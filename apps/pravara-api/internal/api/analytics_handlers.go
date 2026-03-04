package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

// AnalyticsHandler handles analytics and OEE HTTP requests.
type AnalyticsHandler struct {
	oeeRepo    *repositories.OEERepository
	oeeService *services.OEEService
	publisher  *pubsub.Publisher
	log        *logrus.Logger
}

// NewAnalyticsHandler creates a new analytics handler.
func NewAnalyticsHandler(oeeRepo *repositories.OEERepository, log *logrus.Logger) *AnalyticsHandler {
	return &AnalyticsHandler{
		oeeRepo: oeeRepo,
		log:     log,
	}
}

// SetPublisher sets the event publisher for real-time updates.
func (h *AnalyticsHandler) SetPublisher(p *pubsub.Publisher) {
	h.publisher = p
}

// SetOEEService sets the OEE service for computation operations.
func (h *AnalyticsHandler) SetOEEService(s *services.OEEService) {
	h.oeeService = s
}

// ComputeOEERequest represents the request body for triggering OEE computation.
type ComputeOEERequest struct {
	MachineID *uuid.UUID `json:"machine_id"`
	Date      string     `json:"date" binding:"required"`
}

// GetOEE returns a paginated list of OEE snapshots with optional filtering.
// @Summary Get OEE data
// @Description Returns paginated OEE snapshots with optional machine and date range filters
// @Tags analytics
// @Produce json
// @Param machine_id query string false "Filter by machine ID (UUID)"
// @Param from query string false "Start date (RFC3339)"
// @Param to query string false "End date (RFC3339)"
// @Param interval query string false "Aggregation interval: day, week, month" default(day)
// @Param limit query int false "Number of results per page" default(50)
// @Param offset query int false "Offset for pagination" default(0)
// @Success 200 {object} ListResponse "Paginated OEE snapshot list"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /analytics/oee [get]
func (h *AnalyticsHandler) GetOEE(c *gin.Context) {
	filter := repositories.OEEFilter{
		Limit:  50,
		Offset: 0,
	}

	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "50")); err == nil && limit > 0 {
		filter.Limit = limit
	}
	if offset, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && offset >= 0 {
		filter.Offset = offset
	}

	if machineID := c.Query("machine_id"); machineID != "" {
		if id, err := uuid.Parse(machineID); err == nil {
			filter.MachineID = &id
		}
	}

	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			filter.From = &t
		}
	}

	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			filter.To = &t
		}
	}

	if interval := c.Query("interval"); interval != "" {
		filter.Interval = interval
	}

	snapshots, total, err := h.oeeRepo.List(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list OEE snapshots")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve OEE data",
		})
		return
	}

	c.JSON(http.StatusOK, ListResponse{
		Data:   snapshots,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetOEESummary returns fleet-wide aggregated OEE data.
// @Summary Get fleet OEE summary
// @Description Returns aggregated OEE metrics across all machines for a date range
// @Tags analytics
// @Produce json
// @Param from query string true "Start date (RFC3339)"
// @Param to query string true "End date (RFC3339)"
// @Success 200 {object} map[string]interface{} "Fleet OEE summary"
// @Failure 400 {object} map[string]string "Missing required parameters"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /analytics/oee/summary [get]
func (h *AnalyticsHandler) GetOEESummary(c *gin.Context) {
	fromStr := c.Query("from")
	toStr := c.Query("to")

	if fromStr == "" || toStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "Both 'from' and 'to' query parameters are required",
		})
		return
	}

	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "Invalid 'from' date format, expected RFC3339",
		})
		return
	}

	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "Invalid 'to' date format, expected RFC3339",
		})
		return
	}

	summary, err := h.oeeRepo.GetFleetSummary(c.Request.Context(), from, to)
	if err != nil {
		h.log.WithError(err).Error("Failed to get fleet OEE summary")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve fleet OEE summary",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": summary,
		"from": from,
		"to":   to,
	})
}

// ComputeOEE triggers OEE computation for a machine on a specific date.
// @Summary Compute OEE
// @Description Admin trigger to compute OEE for a specific machine and date, or all machines if machine_id is omitted
// @Tags analytics
// @Accept json
// @Produce json
// @Param body body ComputeOEERequest true "Computation request"
// @Success 200 {object} interface{} "Computed OEE snapshot(s)"
// @Failure 400 {object} map[string]string "Validation error"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /analytics/oee/compute [post]
func (h *AnalyticsHandler) ComputeOEE(c *gin.Context) {
	if h.oeeService == nil {
		h.log.Error("OEE service not configured")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "OEE service not configured",
		})
		return
	}

	var req ComputeOEERequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	tenantID, ok := middleware.GetTenantID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "Tenant context not found",
		})
		return
	}

	tenantUUID, _ := uuid.Parse(tenantID)

	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "Invalid date format, expected YYYY-MM-DD",
		})
		return
	}

	if req.MachineID != nil {
		// Compute for a specific machine
		snapshot, err := h.oeeService.ComputeDaily(c.Request.Context(), tenantUUID, *req.MachineID, date)
		if err != nil {
			h.log.WithError(err).Error("Failed to compute OEE")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to compute OEE",
			})
			return
		}

		h.log.WithFields(logrus.Fields{
			"machine_id": req.MachineID,
			"date":       req.Date,
			"oee":        snapshot.OEE,
		}).Info("OEE computed")

		c.JSON(http.StatusOK, snapshot)
		return
	}

	// Compute for all machines
	snapshots, err := h.oeeService.ComputeAllMachines(c.Request.Context(), tenantUUID, date)
	if err != nil {
		h.log.WithError(err).Error("Failed to compute OEE for all machines")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to compute OEE for all machines",
		})
		return
	}

	h.log.WithFields(logrus.Fields{
		"date":           req.Date,
		"machines_count": len(snapshots),
	}).Info("OEE computed for all machines")

	c.JSON(http.StatusOK, gin.H{
		"data":  snapshots,
		"count": len(snapshots),
	})
}
