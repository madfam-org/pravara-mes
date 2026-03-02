package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestRateLimiter_IPBasedLimiting(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	config := RateLimiterConfig{
		IPRateLimit:     5, // 5 requests per minute (very low for testing)
		TenantRateLimit: 100,
		Burst:           2, // Allow 2 requests in burst
		Enabled:         true,
	}

	router := gin.New()
	router.Use(RateLimiterWithConfig(config, logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test: First burst requests should succeed (up to burst limit)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Request %d should succeed", i+1)
	}

	// Test: Next request should be rate limited (burst exhausted)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code, "Should be rate limited")
	assert.Contains(t, w.Header().Get("Retry-After"), "60", "Should have Retry-After header")
}

func TestRateLimiter_TenantBasedLimiting(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	config := RateLimiterConfig{
		IPRateLimit:     100,
		TenantRateLimit: 5, // 5 requests per minute per tenant (very low for testing)
		Burst:           2, // Allow 2 requests in burst
		Enabled:         true,
	}

	router := gin.New()
	router.Use(RateLimiterWithConfig(config, logger))
	router.GET("/test", func(c *gin.Context) {
		// Simulate tenant ID being set by auth middleware
		c.Set("tenant_id", "tenant-123")
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test: First burst requests should succeed (up to burst limit)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Request %d should succeed", i+1)
	}

	// Test: Next request should be rate limited (burst exhausted)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code, "Should be rate limited by tenant")
	assert.Contains(t, w.Header().Get("Retry-After"), "60", "Should have Retry-After header")
}

func TestRateLimiter_DifferentIPsNotAffected(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	config := RateLimiterConfig{
		IPRateLimit:     5,
		TenantRateLimit: 100,
		Burst:           2,
		Enabled:         true,
	}

	router := gin.New()
	router.Use(RateLimiterWithConfig(config, logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test: Exhaust rate limit for IP1
	for i := 0; i < 4; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		router.ServeHTTP(w, req)
	}

	// Test: IP2 should not be affected
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.2:1234"
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Different IP should not be rate limited")
}

func TestRateLimiter_Disabled(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	config := RateLimiterConfig{
		IPRateLimit:     1, // Very low limit
		TenantRateLimit: 1,
		Burst:           1,
		Enabled:         false, // Disabled
	}

	router := gin.New()
	router.Use(RateLimiterWithConfig(config, logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test: All requests should succeed even with low limits
	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "Request %d should succeed when rate limiting disabled", i+1)
	}
}

func TestRateLimiter_TokenRefill(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	config := RateLimiterConfig{
		IPRateLimit:     60, // 60 per minute = 1 per second
		TenantRateLimit: 100,
		Burst:           2,
		Enabled:         true,
	}

	router := gin.New()
	router.Use(RateLimiterWithConfig(config, logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test: Consume initial burst (2 requests)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "Burst request %d should succeed", i+1)
	}

	// Test: Next request should be rate limited (burst exhausted)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code, "Should be rate limited after burst")

	// Test: Wait for token refill (1 second for 1 token at 60/min rate)
	time.Sleep(1100 * time.Millisecond)

	// Test: Should succeed after token refill
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "Should succeed after token refill")
}

func TestLoadRateLimiterConfigFromEnv(t *testing.T) {
	// Save original env vars
	origEnabled := os.Getenv("RATELIMIT_ENABLED")
	origIPLimit := os.Getenv("RATELIMIT_IP_PER_MINUTE")
	origTenantLimit := os.Getenv("RATELIMIT_TENANT_PER_MINUTE")
	origBurst := os.Getenv("RATELIMIT_BURST")

	// Clean up after test
	defer func() {
		if origEnabled != "" {
			os.Setenv("RATELIMIT_ENABLED", origEnabled)
		} else {
			os.Unsetenv("RATELIMIT_ENABLED")
		}
		if origIPLimit != "" {
			os.Setenv("RATELIMIT_IP_PER_MINUTE", origIPLimit)
		} else {
			os.Unsetenv("RATELIMIT_IP_PER_MINUTE")
		}
		if origTenantLimit != "" {
			os.Setenv("RATELIMIT_TENANT_PER_MINUTE", origTenantLimit)
		} else {
			os.Unsetenv("RATELIMIT_TENANT_PER_MINUTE")
		}
		if origBurst != "" {
			os.Setenv("RATELIMIT_BURST", origBurst)
		} else {
			os.Unsetenv("RATELIMIT_BURST")
		}
	}()

	// Test with custom env vars
	os.Setenv("RATELIMIT_ENABLED", "false")
	os.Setenv("RATELIMIT_IP_PER_MINUTE", "200")
	os.Setenv("RATELIMIT_TENANT_PER_MINUTE", "2000")
	os.Setenv("RATELIMIT_BURST", "50")

	config := LoadRateLimiterConfigFromEnv()

	assert.False(t, config.Enabled, "Should load enabled from env")
	assert.Equal(t, 200, config.IPRateLimit, "Should load IP limit from env")
	assert.Equal(t, 2000, config.TenantRateLimit, "Should load tenant limit from env")
	assert.Equal(t, 50, config.Burst, "Should load burst from env")
}
