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
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// MachineHandler handles machine-related HTTP requests.
type MachineHandler struct {
	repo          *repositories.MachineRepository
	telemetryRepo *repositories.TelemetryRepository
	log           *logrus.Logger
	publisher     *pubsub.Publisher
}

// SetPublisher sets the event publisher for real-time updates.
func (h *MachineHandler) SetPublisher(p *pubsub.Publisher) {
	h.publisher = p
}

// NewMachineHandler creates a new machine handler.
func NewMachineHandler(repo *repositories.MachineRepository, telemetryRepo *repositories.TelemetryRepository, log *logrus.Logger) *MachineHandler {
	return &MachineHandler{
		repo:          repo,
		telemetryRepo: telemetryRepo,
		log:           log,
	}
}

// CreateMachineRequest represents the request body for creating a machine.
type CreateMachineRequest struct {
	Name           string         `json:"name" binding:"required"`
	Code           string         `json:"code" binding:"required"`
	Type           string         `json:"type" binding:"required"`
	Description    string         `json:"description"`
	MQTTTopic      string         `json:"mqtt_topic"`
	Location       string         `json:"location"`
	Specifications map[string]any `json:"specifications"`
	Metadata       map[string]any `json:"metadata"`
}

// UpdateMachineRequest represents the request body for updating a machine.
type UpdateMachineRequest struct {
	Name           string         `json:"name"`
	Code           string         `json:"code"`
	Type           string         `json:"type"`
	Description    string         `json:"description"`
	Status         string         `json:"status"`
	MQTTTopic      string         `json:"mqtt_topic"`
	Location       string         `json:"location"`
	Specifications map[string]any `json:"specifications"`
	Metadata       map[string]any `json:"metadata"`
}

// List returns a paginated list of machines.
func (h *MachineHandler) List(c *gin.Context) {
	filter := repositories.MachineFilter{
		Limit:  50,
		Offset: 0,
	}

	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "50")); err == nil && limit > 0 {
		filter.Limit = limit
	}
	if offset, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && offset >= 0 {
		filter.Offset = offset
	}

	if status := c.Query("status"); status != "" {
		s := types.MachineStatus(status)
		filter.Status = &s
	}

	if machineType := c.Query("type"); machineType != "" {
		filter.Type = &machineType
	}

	machines, total, err := h.repo.List(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list machines")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve machines",
		})
		return
	}

	c.JSON(http.StatusOK, ListResponse{
		Data:   machines,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetByID returns a single machine by ID.
func (h *MachineHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid machine ID format",
		})
		return
	}

	machine, err := h.repo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get machine")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve machine",
		})
		return
	}

	if machine == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Machine not found",
		})
		return
	}

	c.JSON(http.StatusOK, machine)
}

// Create creates a new machine.
func (h *MachineHandler) Create(c *gin.Context) {
	var req CreateMachineRequest
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

	// Check if code already exists
	existing, _ := h.repo.GetByCode(c.Request.Context(), req.Code)
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{
			"error":   "duplicate_code",
			"message": "Machine with this code already exists",
		})
		return
	}

	machine := &types.Machine{
		TenantID:       tenantUUID,
		Name:           req.Name,
		Code:           req.Code,
		Type:           req.Type,
		Description:    req.Description,
		Status:         types.MachineStatusOffline,
		MQTTTopic:      req.MQTTTopic,
		Location:       req.Location,
		Specifications: req.Specifications,
		Metadata:       req.Metadata,
	}

	if err := h.repo.Create(c.Request.Context(), machine); err != nil {
		h.log.WithError(err).Error("Failed to create machine")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create machine",
		})
		return
	}

	h.log.WithField("machine_id", machine.ID).Info("Machine created")
	c.JSON(http.StatusCreated, machine)
}

// Update modifies an existing machine.
func (h *MachineHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid machine ID format",
		})
		return
	}

	machine, err := h.repo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get machine")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve machine",
		})
		return
	}

	if machine == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Machine not found",
		})
		return
	}

	var req UpdateMachineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Update fields
	if req.Name != "" {
		machine.Name = req.Name
	}
	if req.Code != "" {
		// Check if new code conflicts with existing
		existing, _ := h.repo.GetByCode(c.Request.Context(), req.Code)
		if existing != nil && existing.ID != machine.ID {
			c.JSON(http.StatusConflict, gin.H{
				"error":   "duplicate_code",
				"message": "Machine with this code already exists",
			})
			return
		}
		machine.Code = req.Code
	}
	if req.Type != "" {
		machine.Type = req.Type
	}
	if req.Description != "" {
		machine.Description = req.Description
	}
	if req.Status != "" {
		machine.Status = types.MachineStatus(req.Status)
	}
	if req.MQTTTopic != "" {
		machine.MQTTTopic = req.MQTTTopic
	}
	if req.Location != "" {
		machine.Location = req.Location
	}
	if req.Specifications != nil {
		machine.Specifications = req.Specifications
	}
	if req.Metadata != nil {
		machine.Metadata = req.Metadata
	}

	if err := h.repo.Update(c.Request.Context(), machine); err != nil {
		h.log.WithError(err).Error("Failed to update machine")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to update machine",
		})
		return
	}

	h.log.WithField("machine_id", machine.ID).Info("Machine updated")
	c.JSON(http.StatusOK, machine)
}

// Delete removes a machine.
func (h *MachineHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid machine ID format",
		})
		return
	}

	if err := h.repo.Delete(c.Request.Context(), id); err != nil {
		if err.Error() == "machine not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Machine not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to delete machine")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to delete machine",
		})
		return
	}

	h.log.WithField("machine_id", id).Info("Machine deleted")
	c.JSON(http.StatusOK, gin.H{
		"message": "Machine deleted successfully",
	})
}

// GetTelemetry returns telemetry data for a machine.
func (h *MachineHandler) GetTelemetry(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid machine ID format",
		})
		return
	}

	// Verify machine exists
	machine, err := h.repo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get machine")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve machine",
		})
		return
	}

	if machine == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Machine not found",
		})
		return
	}

	// Parse query parameters
	filter := repositories.TelemetryFilter{
		MachineID: &id,
		Limit:     100,
	}

	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "100")); err == nil && limit > 0 {
		filter.Limit = limit
	}

	if metricType := c.Query("metric_type"); metricType != "" {
		filter.MetricType = &metricType
	}

	if fromTime := c.Query("from"); fromTime != "" {
		if t, err := time.Parse(time.RFC3339, fromTime); err == nil {
			filter.FromTime = &t
		}
	}

	if toTime := c.Query("to"); toTime != "" {
		if t, err := time.Parse(time.RFC3339, toTime); err == nil {
			filter.ToTime = &t
		}
	}

	telemetry, err := h.telemetryRepo.List(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to get telemetry")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve telemetry",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"machine_id": id,
		"data":       telemetry,
	})
}

// Heartbeat handles machine heartbeat updates.
func (h *MachineHandler) Heartbeat(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid machine ID format",
		})
		return
	}

	if err := h.repo.UpdateHeartbeat(c.Request.Context(), id); err != nil {
		if err.Error() == "machine not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Machine not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to update heartbeat")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to update heartbeat",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Heartbeat recorded",
	})
}
