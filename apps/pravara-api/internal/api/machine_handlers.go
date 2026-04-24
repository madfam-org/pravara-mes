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

// SendCommandRequest represents the request body for sending a command to a machine.
type SendCommandRequest struct {
	Command    string         `json:"command" binding:"required"`
	Parameters map[string]any `json:"parameters,omitempty"`
	TaskID     *uuid.UUID     `json:"task_id,omitempty"`
	OrderID    *uuid.UUID     `json:"order_id,omitempty"`
}

// List returns a paginated list of machines.
// @Summary List machines
// @Description Returns a paginated list of machines with optional filtering by status and type
// @Tags machines
// @Produce json
// @Param limit query int false "Number of results per page" default(50)
// @Param offset query int false "Offset for pagination" default(0)
// @Param status query string false "Filter by machine status" Enums(online, offline, busy, maintenance, error)
// @Param type query string false "Filter by machine type"
// @Success 200 {object} ListResponse "Paginated machine list"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /machines [get]
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
// @Summary Get machine by ID
// @Description Returns a single machine with all details
// @Tags machines
// @Produce json
// @Param id path string true "Machine ID (UUID)"
// @Success 200 {object} types.Machine "Machine details"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Machine not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /machines/{id} [get]
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
// @Summary Create a new machine
// @Description Creates a new machine in offline status
// @Tags machines
// @Accept json
// @Produce json
// @Param body body CreateMachineRequest true "Machine creation data"
// @Success 201 {object} types.Machine "Created machine"
// @Failure 400 {object} map[string]string "Validation error"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 409 {object} map[string]string "Duplicate machine code"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /machines [post]
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
// @Summary Update a machine
// @Description Updates machine configuration and status
// @Tags machines
// @Accept json
// @Produce json
// @Param id path string true "Machine ID (UUID)"
// @Param body body UpdateMachineRequest true "Machine update data"
// @Success 200 {object} types.Machine "Updated machine"
// @Failure 400 {object} map[string]string "Validation error"
// @Failure 404 {object} map[string]string "Machine not found"
// @Failure 409 {object} map[string]string "Duplicate machine code"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /machines/{id} [put]
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
// @Summary Delete a machine
// @Description Permanently deletes a machine
// @Tags machines
// @Produce json
// @Param id path string true "Machine ID (UUID)"
// @Success 200 {object} map[string]string "Machine deleted successfully"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Machine not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /machines/{id} [delete]
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
// @Summary Get machine telemetry
// @Description Returns telemetry data for a specific machine with optional time range and metric filtering
// @Tags machines
// @Produce json
// @Param id path string true "Machine ID (UUID)"
// @Param limit query int false "Maximum records to return" default(100)
// @Param metric_type query string false "Filter by metric type"
// @Param from query string false "Start time (RFC3339 format)"
// @Param to query string false "End time (RFC3339 format)"
// @Success 200 {object} map[string]interface{} "Telemetry data"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Machine not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /machines/{id}/telemetry [get]
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
// @Summary Record machine heartbeat
// @Description Updates the last heartbeat timestamp for a machine
// @Tags machines
// @Produce json
// @Param id path string true "Machine ID (UUID)"
// @Success 200 {object} map[string]string "Heartbeat recorded"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Machine not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /machines/{id}/heartbeat [post]
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

// validCommands defines the set of allowed machine commands.
var validCommands = map[string]pubsub.MachineCommandType{
	"start_job":      pubsub.CommandStartJob,
	"pause":          pubsub.CommandPause,
	"resume":         pubsub.CommandResume,
	"stop":           pubsub.CommandStop,
	"home":           pubsub.CommandHome,
	"calibrate":      pubsub.CommandCalibrate,
	"emergency_stop": pubsub.CommandEmergency,
	"preheat":        pubsub.CommandPreheat,
	"cooldown":       pubsub.CommandCooldown,
	"load_file":      pubsub.CommandLoadFile,
	"unload_file":    pubsub.CommandUnloadFile,
	"set_origin":     pubsub.CommandSetOrigin,
	"probe":          pubsub.CommandProbe,
}

// SendCommand sends a control command to a machine.
// The command is published via Redis for the telemetry-worker to dispatch via MQTT.
// @Summary Send command to machine
// @Description Dispatches a control command to a machine via MQTT
// @Tags machines
// @Accept json
// @Produce json
// @Param id path string true "Machine ID (UUID)"
// @Param body body SendCommandRequest true "Command to send"
// @Success 202 {object} map[string]interface{} "Command dispatched"
// @Failure 400 {object} map[string]string "Invalid command or machine not configured"
// @Failure 404 {object} map[string]string "Machine not found"
// @Failure 409 {object} map[string]string "Machine in error state"
// @Failure 500 {object} map[string]string "Dispatch failed"
// @Security BearerAuth
// @Router /machines/{id}/command [post]
func (h *MachineHandler) SendCommand(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid machine ID format",
		})
		return
	}

	var req SendCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Validate command type
	cmdType, valid := validCommands[req.Command]
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_command",
			"message": "Unknown command type. Valid commands: start_job, pause, resume, stop, home, calibrate, emergency_stop, preheat, cooldown, load_file, unload_file, set_origin, probe",
		})
		return
	}

	// Get machine to verify it exists and get MQTT topic
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

	// Check if machine has MQTT topic configured
	if machine.MQTTTopic == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "no_mqtt_topic",
			"message": "Machine does not have an MQTT topic configured for command dispatch",
		})
		return
	}

	// Check if machine is online (optional - allow commands to offline machines for recovery)
	if machine.Status == types.MachineStatusError {
		// Only allow emergency_stop for machines in error state
		if cmdType != pubsub.CommandEmergency {
			c.JSON(http.StatusConflict, gin.H{
				"error":   "machine_error",
				"message": "Machine is in error state. Only emergency_stop command is allowed.",
			})
			return
		}
	}

	// Get user ID from context
	userID, _ := middleware.GetUserID(c)
	userUUID, _ := uuid.Parse(userID)

	// Generate command ID
	commandID := uuid.New()

	// Build command data
	commandData := pubsub.MachineCommandData{
		CommandID:   commandID,
		MachineID:   machine.ID,
		MachineName: machine.Name,
		MQTTTopic:   machine.MQTTTopic,
		Command:     cmdType,
		Parameters:  req.Parameters,
		TaskID:      req.TaskID,
		OrderID:     req.OrderID,
		IssuedBy:    userUUID,
		IssuedAt:    time.Now().UTC(),
	}

	// Publish command event for telemetry-worker to dispatch via MQTT
	if h.publisher != nil {
		// Publish to Centrifugo for UI real-time updates
		if err := h.publisher.PublishMachineCommand(c.Request.Context(), machine.TenantID, commandData); err != nil {
			h.log.WithError(err).WithFields(logrus.Fields{
				"machine_id": machine.ID,
				"command":    req.Command,
			}).Error("Failed to publish machine command to Centrifugo")
			// Continue - Centrifugo publish is not critical for command dispatch
		}

		// Publish to command dispatch channel for telemetry-worker
		if err := h.publisher.PublishCommandForDispatch(c.Request.Context(), machine.TenantID, commandData); err != nil {
			h.log.WithError(err).WithFields(logrus.Fields{
				"machine_id": machine.ID,
				"command":    req.Command,
			}).Error("Failed to publish command to dispatch channel")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "dispatch_error",
				"message": "Failed to dispatch command to machine",
			})
			return
		}
	} else {
		h.log.Warn("Publisher not configured - command will not be dispatched")
	}

	h.log.WithFields(logrus.Fields{
		"machine_id": machine.ID,
		"command_id": commandID,
		"command":    req.Command,
		"issued_by":  userID,
	}).Info("Machine command issued")

	c.JSON(http.StatusAccepted, gin.H{
		"command_id": commandID,
		"machine_id": machine.ID,
		"command":    req.Command,
		"status":     "dispatched",
		"message":    "Command dispatched to machine",
	})
}
