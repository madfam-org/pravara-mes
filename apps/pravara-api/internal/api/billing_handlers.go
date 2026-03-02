package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/billing"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

// BillingHandler handles billing and usage tracking HTTP requests.
type BillingHandler struct {
	recorder billing.UsageRecorder
	log      *logrus.Logger
}

// NewBillingHandler creates a new billing handler.
func NewBillingHandler(recorder billing.UsageRecorder, log *logrus.Logger) *BillingHandler {
	return &BillingHandler{
		recorder: recorder,
		log:      log,
	}
}

// GetUsage returns the current tenant's usage summary for a specified period.
// GET /v1/billing/usage?from=2024-01-01&to=2024-01-31
func (h *BillingHandler) GetUsage(c *gin.Context) {
	tenantID, ok := middleware.GetTenantID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "Tenant context not found",
		})
		return
	}

	// Parse date parameters
	fromStr := c.DefaultQuery("from", "")
	toStr := c.DefaultQuery("to", "")

	var from, to time.Time
	var err error

	if fromStr == "" {
		// Default to current month
		now := time.Now()
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	} else {
		from, err = time.Parse("2006-01-02", fromStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_date",
				"message": "Invalid 'from' date format. Use YYYY-MM-DD",
			})
			return
		}
	}

	if toStr == "" {
		// Default to current date
		to = time.Now()
	} else {
		to, err = time.Parse("2006-01-02", toStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_date",
				"message": "Invalid 'to' date format. Use YYYY-MM-DD",
			})
			return
		}
	}

	// Validate date range
	if to.Before(from) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_range",
			"message": "'to' date must be after 'from' date",
		})
		return
	}

	// Limit range to 90 days
	if to.Sub(from) > 90*24*time.Hour {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_range",
			"message": "Date range cannot exceed 90 days",
		})
		return
	}

	summary, err := h.recorder.GetTenantUsage(c.Request.Context(), tenantID, from, to)
	if err != nil {
		h.log.WithError(err).WithField("tenant_id", tenantID).Error("Failed to get tenant usage")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve usage data",
		})
		return
	}

	c.JSON(http.StatusOK, summary)
}

// GetDailyUsage returns daily breakdown of usage for the current tenant.
// GET /v1/billing/usage/daily?from=2024-01-01&to=2024-01-31
func (h *BillingHandler) GetDailyUsage(c *gin.Context) {
	tenantID, ok := middleware.GetTenantID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "Tenant context not found",
		})
		return
	}

	// Parse date parameters
	fromStr := c.DefaultQuery("from", "")
	toStr := c.DefaultQuery("to", "")

	var from, to time.Time
	var err error

	if fromStr == "" {
		// Default to current month
		now := time.Now()
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	} else {
		from, err = time.Parse("2006-01-02", fromStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_date",
				"message": "Invalid 'from' date format. Use YYYY-MM-DD",
			})
			return
		}
	}

	if toStr == "" {
		// Default to current date
		to = time.Now()
	} else {
		to, err = time.Parse("2006-01-02", toStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_date",
				"message": "Invalid 'to' date format. Use YYYY-MM-DD",
			})
			return
		}
	}

	// Validate date range
	if to.Before(from) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_range",
			"message": "'to' date must be after 'from' date",
		})
		return
	}

	// Limit range to 90 days
	if to.Sub(from) > 90*24*time.Hour {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_range",
			"message": "Date range cannot exceed 90 days",
		})
		return
	}

	dailyUsage, err := h.recorder.GetDailyUsage(c.Request.Context(), tenantID, from, to)
	if err != nil {
		h.log.WithError(err).WithField("tenant_id", tenantID).Error("Failed to get daily usage")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve daily usage data",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tenant_id":   tenantID,
		"from_date":   from.Format("2006-01-02"),
		"to_date":     to.Format("2006-01-02"),
		"daily_usage": dailyUsage,
	})
}

// GetTenantUsageAdmin returns usage summary for a specific tenant (admin only).
// GET /v1/admin/billing/tenants/:id/usage?from=2024-01-01&to=2024-01-31
func (h *BillingHandler) GetTenantUsageAdmin(c *gin.Context) {
	targetTenantID := c.Param("id")
	if targetTenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_request",
			"message": "Tenant ID is required",
		})
		return
	}

	// Parse date parameters
	fromStr := c.DefaultQuery("from", "")
	toStr := c.DefaultQuery("to", "")

	var from, to time.Time
	var err error

	if fromStr == "" {
		// Default to current month
		now := time.Now()
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	} else {
		from, err = time.Parse("2006-01-02", fromStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_date",
				"message": "Invalid 'from' date format. Use YYYY-MM-DD",
			})
			return
		}
	}

	if toStr == "" {
		// Default to current date
		to = time.Now()
	} else {
		to, err = time.Parse("2006-01-02", toStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_date",
				"message": "Invalid 'to' date format. Use YYYY-MM-DD",
			})
			return
		}
	}

	// Validate date range
	if to.Before(from) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_range",
			"message": "'to' date must be after 'from' date",
		})
		return
	}

	// Limit range to 365 days for admin
	if to.Sub(from) > 365*24*time.Hour {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_range",
			"message": "Date range cannot exceed 365 days",
		})
		return
	}

	summary, err := h.recorder.GetTenantUsage(c.Request.Context(), targetTenantID, from, to)
	if err != nil {
		h.log.WithError(err).WithField("tenant_id", targetTenantID).Error("Failed to get tenant usage (admin)")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve usage data",
		})
		return
	}

	c.JSON(http.StatusOK, summary)
}
