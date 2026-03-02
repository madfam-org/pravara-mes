package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/billing"
)

// UsageTracking creates middleware that tracks API usage per tenant for billing.
func UsageTracking(recorder billing.UsageRecorder, log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip usage tracking for health endpoints
		if c.Request.URL.Path == "/health" ||
			c.Request.URL.Path == "/health/live" ||
			c.Request.URL.Path == "/health/ready" ||
			c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}

		// Process request first
		c.Next()

		// Get tenant ID from context (set by auth middleware)
		tenantID, ok := GetTenantID(c)
		if !ok {
			// No tenant context, skip tracking (e.g., unauthenticated endpoints)
			return
		}

		// Record API call usage event asynchronously
		event := billing.UsageEvent{
			TenantID:  tenantID,
			EventType: billing.UsageEventAPICall,
			Quantity:  1,
			Metadata: map[string]string{
				"method":      c.Request.Method,
				"path":        c.Request.URL.Path,
				"status_code": string(rune(c.Writer.Status())),
			},
			Timestamp: time.Now(),
		}

		// Record event in background (non-blocking)
		go func() {
			ctx := c.Request.Context()
			if err := recorder.RecordEvent(ctx, event); err != nil {
				log.WithError(err).WithFields(logrus.Fields{
					"tenant_id":  tenantID,
					"event_type": event.EventType,
				}).Warn("Failed to record usage event")
			}
		}()
	}
}
