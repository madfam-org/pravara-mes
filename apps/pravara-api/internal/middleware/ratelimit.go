// Package middleware provides HTTP middleware for the PravaraMES API.
package middleware

import (
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

// RateLimiterConfig holds configuration for rate limiting middleware.
type RateLimiterConfig struct {
	// IPRateLimit is the maximum requests per minute per IP address
	IPRateLimit int
	// TenantRateLimit is the maximum requests per minute per tenant
	TenantRateLimit int
	// Burst allows for temporary bursts above the rate limit
	Burst int
	// Enabled toggles rate limiting on/off
	Enabled bool
}

// DefaultRateLimiterConfig returns sensible defaults for rate limiting.
func DefaultRateLimiterConfig() RateLimiterConfig {
	return RateLimiterConfig{
		IPRateLimit:     100,  // 100 requests per minute per IP
		TenantRateLimit: 1000, // 1000 requests per minute per tenant
		Burst:           20,   // Allow bursts of 20 requests
		Enabled:         true,
	}
}

// LoadRateLimiterConfigFromEnv loads rate limiter configuration from environment variables.
func LoadRateLimiterConfigFromEnv() RateLimiterConfig {
	cfg := DefaultRateLimiterConfig()

	if val := os.Getenv("RATELIMIT_ENABLED"); val != "" {
		if enabled, err := strconv.ParseBool(val); err == nil {
			cfg.Enabled = enabled
		}
	}

	if val := os.Getenv("RATELIMIT_IP_PER_MINUTE"); val != "" {
		if limit, err := strconv.Atoi(val); err == nil && limit > 0 {
			cfg.IPRateLimit = limit
		}
	}

	if val := os.Getenv("RATELIMIT_TENANT_PER_MINUTE"); val != "" {
		if limit, err := strconv.Atoi(val); err == nil && limit > 0 {
			cfg.TenantRateLimit = limit
		}
	}

	if val := os.Getenv("RATELIMIT_BURST"); val != "" {
		if burst, err := strconv.Atoi(val); err == nil && burst > 0 {
			cfg.Burst = burst
		}
	}

	return cfg
}

// rateLimiter holds per-IP and per-tenant rate limiters using token bucket algorithm.
type rateLimiter struct {
	config RateLimiterConfig
	logger *logrus.Logger

	// Per-IP rate limiters
	ipLimiters map[string]*rate.Limiter
	ipMu       sync.RWMutex

	// Per-tenant rate limiters
	tenantLimiters map[string]*rate.Limiter
	tenantMu       sync.RWMutex

	// Cleanup ticker
	cleanupTicker *time.Ticker
	done          chan bool
}

// newRateLimiter creates a new rate limiter instance.
func newRateLimiter(config RateLimiterConfig, logger *logrus.Logger) *rateLimiter {
	rl := &rateLimiter{
		config:         config,
		logger:         logger,
		ipLimiters:     make(map[string]*rate.Limiter),
		tenantLimiters: make(map[string]*rate.Limiter),
		cleanupTicker:  time.NewTicker(5 * time.Minute),
		done:           make(chan bool),
	}

	// Start cleanup goroutine to remove stale limiters
	go rl.cleanupLoop()

	return rl
}

// cleanupLoop periodically removes inactive rate limiters to prevent memory leaks.
func (rl *rateLimiter) cleanupLoop() {
	for {
		select {
		case <-rl.cleanupTicker.C:
			rl.cleanup()
		case <-rl.done:
			return
		}
	}
}

// cleanup removes rate limiters that haven't been used recently.
func (rl *rateLimiter) cleanup() {
	// Clean up IP limiters
	rl.ipMu.Lock()
	for ip, limiter := range rl.ipLimiters {
		// If limiter has full tokens (not used recently), remove it
		if limiter.Tokens() >= float64(rl.config.Burst) {
			delete(rl.ipLimiters, ip)
		}
	}
	rl.ipMu.Unlock()

	// Clean up tenant limiters
	rl.tenantMu.Lock()
	for tenant, limiter := range rl.tenantLimiters {
		if limiter.Tokens() >= float64(rl.config.Burst) {
			delete(rl.tenantLimiters, tenant)
		}
	}
	rl.tenantMu.Unlock()
}

// getIPLimiter returns the rate limiter for a specific IP address.
func (rl *rateLimiter) getIPLimiter(ip string) *rate.Limiter {
	rl.ipMu.Lock()
	defer rl.ipMu.Unlock()

	limiter, exists := rl.ipLimiters[ip]
	if !exists {
		// Create new limiter: rate per second = rate per minute / 60
		r := rate.Limit(float64(rl.config.IPRateLimit) / 60.0)
		limiter = rate.NewLimiter(r, rl.config.Burst)
		rl.ipLimiters[ip] = limiter
	}

	return limiter
}

// getTenantLimiter returns the rate limiter for a specific tenant.
func (rl *rateLimiter) getTenantLimiter(tenantID string) *rate.Limiter {
	rl.tenantMu.Lock()
	defer rl.tenantMu.Unlock()

	limiter, exists := rl.tenantLimiters[tenantID]
	if !exists {
		// Create new limiter: rate per second = rate per minute / 60
		r := rate.Limit(float64(rl.config.TenantRateLimit) / 60.0)
		limiter = rate.NewLimiter(r, rl.config.Burst)
		rl.tenantLimiters[tenantID] = limiter
	}

	return limiter
}

// stop stops the cleanup goroutine.
func (rl *rateLimiter) stop() {
	rl.cleanupTicker.Stop()
	close(rl.done)
}

// RateLimiter returns a Gin middleware that implements per-IP and per-tenant rate limiting.
// It uses the token bucket algorithm via golang.org/x/time/rate.
//
// Rate limiting is applied at two levels:
// 1. Per-IP: Limits requests from a single IP address (default: 100 req/min)
// 2. Per-Tenant: Limits requests for a specific tenant from JWT claims (default: 1000 req/min)
//
// Configuration is loaded from environment variables:
// - RATELIMIT_ENABLED: Enable/disable rate limiting (default: true)
// - RATELIMIT_IP_PER_MINUTE: Max requests per minute per IP (default: 100)
// - RATELIMIT_TENANT_PER_MINUTE: Max requests per minute per tenant (default: 1000)
// - RATELIMIT_BURST: Burst size for token bucket (default: 20)
//
// When rate limit is exceeded, responds with 429 Too Many Requests and Retry-After header.
func RateLimiter(logger *logrus.Logger) gin.HandlerFunc {
	config := LoadRateLimiterConfigFromEnv()

	// If rate limiting is disabled, return a no-op middleware
	if !config.Enabled {
		logger.Info("Rate limiting is disabled")
		return func(c *gin.Context) {
			c.Next()
		}
	}

	logger.WithFields(logrus.Fields{
		"ip_limit":     config.IPRateLimit,
		"tenant_limit": config.TenantRateLimit,
		"burst":        config.Burst,
	}).Info("Rate limiting enabled")

	rl := newRateLimiter(config, logger)

	return func(c *gin.Context) {
		// Get client IP
		clientIP := c.ClientIP()

		// Check IP-based rate limit
		ipLimiter := rl.getIPLimiter(clientIP)
		if !ipLimiter.Allow() {
			logger.WithFields(logrus.Fields{
				"ip":     clientIP,
				"path":   c.Request.URL.Path,
				"method": c.Request.Method,
			}).Warn("IP rate limit exceeded")

			c.Header("Retry-After", "60") // Suggest retry after 60 seconds
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":               "rate_limit_exceeded",
				"message":             "Too many requests from this IP address. Please try again later.",
				"retry_after_seconds": 60,
			})
			c.Abort()
			return
		}

		// Check tenant-based rate limit if user is authenticated
		// Tenant ID is extracted from JWT claims set by the Auth middleware
		tenantID, exists := c.Get("tenant_id")
		if exists {
			tenantIDStr, ok := tenantID.(string)
			if ok && tenantIDStr != "" {
				tenantLimiter := rl.getTenantLimiter(tenantIDStr)
				if !tenantLimiter.Allow() {
					logger.WithFields(logrus.Fields{
						"tenant_id": tenantIDStr,
						"ip":        clientIP,
						"path":      c.Request.URL.Path,
						"method":    c.Request.Method,
					}).Warn("Tenant rate limit exceeded")

					c.Header("Retry-After", "60")
					c.JSON(http.StatusTooManyRequests, gin.H{
						"error":               "rate_limit_exceeded",
						"message":             "Too many requests for this tenant. Please try again later.",
						"retry_after_seconds": 60,
					})
					c.Abort()
					return
				}
			}
		}

		// Rate limit check passed, continue to next handler
		c.Next()
	}
}

// RateLimiterWithConfig returns a Gin middleware with custom rate limiter configuration.
func RateLimiterWithConfig(config RateLimiterConfig, logger *logrus.Logger) gin.HandlerFunc {
	if !config.Enabled {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	logger.WithFields(logrus.Fields{
		"ip_limit":     config.IPRateLimit,
		"tenant_limit": config.TenantRateLimit,
		"burst":        config.Burst,
	}).Info("Rate limiting enabled with custom config")

	rl := newRateLimiter(config, logger)

	return func(c *gin.Context) {
		clientIP := c.ClientIP()

		// Check IP-based rate limit
		ipLimiter := rl.getIPLimiter(clientIP)
		if !ipLimiter.Allow() {
			logger.WithFields(logrus.Fields{
				"ip":     clientIP,
				"path":   c.Request.URL.Path,
				"method": c.Request.Method,
			}).Warn("IP rate limit exceeded")

			c.Header("Retry-After", "60")
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":               "rate_limit_exceeded",
				"message":             "Too many requests from this IP address. Please try again later.",
				"retry_after_seconds": 60,
			})
			c.Abort()
			return
		}

		// Check tenant-based rate limit
		tenantID, exists := c.Get("tenant_id")
		if exists {
			tenantIDStr, ok := tenantID.(string)
			if ok && tenantIDStr != "" {
				tenantLimiter := rl.getTenantLimiter(tenantIDStr)
				if !tenantLimiter.Allow() {
					logger.WithFields(logrus.Fields{
						"tenant_id": tenantIDStr,
						"ip":        clientIP,
						"path":      c.Request.URL.Path,
						"method":    c.Request.Method,
					}).Warn("Tenant rate limit exceeded")

					c.Header("Retry-After", "60")
					c.JSON(http.StatusTooManyRequests, gin.H{
						"error":               "rate_limit_exceeded",
						"message":             "Too many requests for this tenant. Please try again later.",
						"retry_after_seconds": 60,
					})
					c.Abort()
					return
				}
			}
		}

		c.Next()
	}
}
