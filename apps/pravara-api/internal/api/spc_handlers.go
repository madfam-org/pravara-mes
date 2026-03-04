package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

// SPCHandler handles Statistical Process Control HTTP requests.
type SPCHandler struct {
	spcRepo    *repositories.SPCRepository
	spcService *services.SPCService
	publisher  *pubsub.Publisher
	log        *logrus.Logger
}

// NewSPCHandler creates a new SPC handler.
func NewSPCHandler(spcRepo *repositories.SPCRepository, log *logrus.Logger) *SPCHandler {
	return &SPCHandler{
		spcRepo: spcRepo,
		log:     log,
	}
}

// SetPublisher sets the event publisher for real-time updates.
func (h *SPCHandler) SetPublisher(p *pubsub.Publisher) {
	h.publisher = p
}

// SetSPCService sets the SPC service for computation operations.
func (h *SPCHandler) SetSPCService(s *services.SPCService) {
	h.spcService = s
}

// ComputeLimitsRequest represents the request body for computing SPC limits.
type ComputeLimitsRequest struct {
	MachineID  uuid.UUID `json:"machine_id" binding:"required"`
	MetricType string    `json:"metric_type" binding:"required"`
	SampleDays int       `json:"sample_days"`
}

// AcknowledgeViolationRequest represents the request body for acknowledging a violation.
type AcknowledgeViolationRequest struct {
	Notes *string `json:"notes"`
}

// GetLimits returns control limits for a machine.
func (h *SPCHandler) GetLimits(c *gin.Context) {
	machineIDStr := c.Query("machine_id")
	if machineIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "machine_id query parameter is required",
		})
		return
	}

	machineID, err := uuid.Parse(machineIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "Invalid machine_id format",
		})
		return
	}

	limits, err := h.spcRepo.ListLimits(c.Request.Context(), machineID)
	if err != nil {
		h.log.WithError(err).Error("Failed to list SPC control limits")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve SPC control limits",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":       limits,
		"machine_id": machineID,
	})
}

// ComputeLimits triggers SPC limit computation from telemetry data.
func (h *SPCHandler) ComputeLimits(c *gin.Context) {
	if h.spcService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "SPC service not configured",
		})
		return
	}

	var req ComputeLimitsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	sampleDays := req.SampleDays
	if sampleDays <= 0 {
		sampleDays = 30
	}

	limit, err := h.spcService.ComputeLimits(c.Request.Context(), req.MachineID, req.MetricType, sampleDays)
	if err != nil {
		h.log.WithError(err).Error("Failed to compute SPC limits")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to compute SPC limits: " + err.Error(),
		})
		return
	}

	h.log.WithFields(logrus.Fields{
		"machine_id":  req.MachineID,
		"metric_type": req.MetricType,
		"ucl":         limit.UCL,
		"lcl":         limit.LCL,
	}).Info("SPC limits computed")

	c.JSON(http.StatusOK, limit)
}

// GetChart returns telemetry data formatted for SPC chart rendering.
func (h *SPCHandler) GetChart(c *gin.Context) {
	machineIDStr := c.Query("machine_id")
	metricType := c.Query("metric_type")

	if machineIDStr == "" || metricType == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "machine_id and metric_type query parameters are required",
		})
		return
	}

	machineID, err := uuid.Parse(machineIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "Invalid machine_id format",
		})
		return
	}

	// Get active control limits
	limits, err := h.spcRepo.ListLimits(c.Request.Context(), machineID)
	if err != nil {
		h.log.WithError(err).Error("Failed to get SPC limits for chart")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve SPC limits",
		})
		return
	}

	var activeLimit *repositories.SPCControlLimit
	for i, cl := range limits {
		if cl.MetricType == metricType && cl.IsActive {
			activeLimit = &limits[i]
			break
		}
	}

	// Get recent violations
	violations, err := h.spcRepo.ListViolations(c.Request.Context(), machineID, false)
	if err != nil {
		h.log.WithError(err).Error("Failed to get SPC violations for chart")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve SPC violations",
		})
		return
	}

	// Filter violations to this metric type
	var metricViolations []repositories.SPCViolation
	for _, v := range violations {
		if v.MetricType == metricType {
			metricViolations = append(metricViolations, v)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"machine_id":    machineID,
		"metric_type":   metricType,
		"control_limit": activeLimit,
		"violations":    metricViolations,
	})
}

// GetViolations returns SPC violations with optional filtering.
func (h *SPCHandler) GetViolations(c *gin.Context) {
	machineIDStr := c.Query("machine_id")
	if machineIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "machine_id query parameter is required",
		})
		return
	}

	machineID, err := uuid.Parse(machineIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "Invalid machine_id format",
		})
		return
	}

	unackedOnly := false
	if unacked := c.Query("unacknowledged_only"); unacked != "" {
		unackedOnly, _ = strconv.ParseBool(unacked)
	}

	violations, err := h.spcRepo.ListViolations(c.Request.Context(), machineID, unackedOnly)
	if err != nil {
		h.log.WithError(err).Error("Failed to list SPC violations")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve SPC violations",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":       violations,
		"machine_id": machineID,
		"total":      len(violations),
	})
}

// AcknowledgeViolation marks a violation as acknowledged.
func (h *SPCHandler) AcknowledgeViolation(c *gin.Context) {
	violationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid violation ID format",
		})
		return
	}

	var req AcknowledgeViolationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Allow empty body
		req = AcknowledgeViolationRequest{}
	}

	userID, _ := middleware.GetUserID(c)
	userUUID, _ := uuid.Parse(userID)

	if err := h.spcRepo.AcknowledgeViolation(c.Request.Context(), violationID, userUUID, req.Notes); err != nil {
		if err.Error() == "SPC violation not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "SPC violation not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to acknowledge SPC violation")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to acknowledge SPC violation",
		})
		return
	}

	h.log.WithField("violation_id", violationID).Info("SPC violation acknowledged")
	c.JSON(http.StatusOK, gin.H{
		"message": "SPC violation acknowledged successfully",
	})
}
