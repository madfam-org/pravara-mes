// Package middleware provides HTTP middleware for the PravaraMES API.
package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/observability"
)

// Metrics returns a Gin middleware that records Prometheus metrics for HTTP requests.
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip metrics endpoint to avoid recursive metrics collection
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}

		// Record in-flight request
		observability.HTTPRequestsInFlight.Inc()
		defer observability.HTTPRequestsInFlight.Dec()

		// Record request start time
		start := time.Now()

		// Process request
		c.Next()

		// Use route path template (e.g., "/api/v1/machines/:id") to avoid high cardinality
		// Fall back to raw path if no route is matched
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		// Extract tenant_id from context (set by auth middleware)
		tenantID := c.GetString("tenant_id")
		if tenantID == "" {
			tenantID = "unknown"
		}

		// Record duration
		duration := time.Since(start).Seconds()
		observability.HTTPRequestDuration.WithLabelValues(
			c.Request.Method,
			path,
			tenantID,
		).Observe(duration)

		// Record request count
		status := strconv.Itoa(c.Writer.Status())
		observability.HTTPRequestsTotal.WithLabelValues(
			c.Request.Method,
			path,
			status,
			tenantID,
		).Inc()
	}
}
