package api

import (
	"encoding/json"
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

// MaintenanceHandler handles maintenance-related HTTP requests.
type MaintenanceHandler struct {
	maintRepo    *repositories.MaintenanceRepository
	maintService *services.MaintenanceService
	publisher    *pubsub.Publisher
	log          *logrus.Logger
}

// NewMaintenanceHandler creates a new maintenance handler.
func NewMaintenanceHandler(maintRepo *repositories.MaintenanceRepository, log *logrus.Logger) *MaintenanceHandler {
	return &MaintenanceHandler{
		maintRepo: maintRepo,
		log:       log,
	}
}

// SetPublisher sets the event publisher for real-time updates.
func (h *MaintenanceHandler) SetPublisher(p *pubsub.Publisher) {
	h.publisher = p
}

// SetMaintenanceService sets the maintenance service for business logic.
func (h *MaintenanceHandler) SetMaintenanceService(s *services.MaintenanceService) {
	h.maintService = s
}

// =============== Request Types ===============

// CreateScheduleRequest represents the request body for creating a maintenance schedule.
type CreateScheduleRequest struct {
	MachineID          uuid.UUID      `json:"machine_id" binding:"required"`
	Name               string         `json:"name" binding:"required"`
	Description        string         `json:"description"`
	TriggerType        string         `json:"trigger_type" binding:"required"`
	Priority           int            `json:"priority"`
	IntervalDays       *int           `json:"interval_days"`
	IntervalHours      *float64       `json:"interval_hours"`
	LastDoneHours      *float64       `json:"last_done_hours"`
	NextDueHours       *float64       `json:"next_due_hours"`
	IntervalCycles     *int           `json:"interval_cycles"`
	LastDoneCycles     *int           `json:"last_done_cycles"`
	NextDueCycles      *int           `json:"next_due_cycles"`
	ConditionMetric    *string        `json:"condition_metric"`
	ConditionThreshold *float64       `json:"condition_threshold"`
	NextDueAt          *time.Time     `json:"next_due_at"`
	AssignedTo         *uuid.UUID     `json:"assigned_to"`
	IsActive           *bool          `json:"is_active"`
	Metadata           map[string]any `json:"metadata"`
}

// UpdateScheduleRequest represents the request body for updating a maintenance schedule.
type UpdateScheduleRequest struct {
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	TriggerType        string         `json:"trigger_type"`
	Priority           int            `json:"priority"`
	IntervalDays       *int           `json:"interval_days"`
	IntervalHours      *float64       `json:"interval_hours"`
	LastDoneHours      *float64       `json:"last_done_hours"`
	NextDueHours       *float64       `json:"next_due_hours"`
	IntervalCycles     *int           `json:"interval_cycles"`
	LastDoneCycles     *int           `json:"last_done_cycles"`
	NextDueCycles      *int           `json:"next_due_cycles"`
	ConditionMetric    *string        `json:"condition_metric"`
	ConditionThreshold *float64       `json:"condition_threshold"`
	NextDueAt          *time.Time     `json:"next_due_at"`
	AssignedTo         *uuid.UUID     `json:"assigned_to"`
	IsActive           *bool          `json:"is_active"`
	Metadata           map[string]any `json:"metadata"`
}

// CreateWorkOrderRequest represents the request body for creating a maintenance work order.
type CreateWorkOrderRequest struct {
	ScheduleID      *uuid.UUID      `json:"schedule_id"`
	MachineID       uuid.UUID       `json:"machine_id" binding:"required"`
	WorkOrderNumber string          `json:"work_order_number" binding:"required"`
	Title           string          `json:"title" binding:"required"`
	Description     string          `json:"description"`
	Priority        int             `json:"priority"`
	AssignedTo      *uuid.UUID      `json:"assigned_to"`
	Checklist       json.RawMessage `json:"checklist"`
	ScheduledAt     *time.Time      `json:"scheduled_at"`
	DueAt           *time.Time      `json:"due_at"`
	Notes           string          `json:"notes"`
	Metadata        map[string]any  `json:"metadata"`
}

// UpdateWorkOrderRequest represents the request body for updating a maintenance work order.
type UpdateWorkOrderRequest struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Status      string          `json:"status"`
	Priority    int             `json:"priority"`
	AssignedTo  *uuid.UUID      `json:"assigned_to"`
	Checklist   json.RawMessage `json:"checklist"`
	ScheduledAt *time.Time      `json:"scheduled_at"`
	DueAt       *time.Time      `json:"due_at"`
	Notes       string          `json:"notes"`
	PartsUsed   json.RawMessage `json:"parts_used"`
	Metadata    map[string]any  `json:"metadata"`
}

// CompleteWorkOrderRequest represents the request body for completing a work order.
type CompleteWorkOrderRequest struct {
	Notes string `json:"notes"`
}

// =============== Schedule Endpoints ===============

// ListSchedules returns a paginated list of maintenance schedules.
// @Summary List maintenance schedules
// @Description Returns paginated list of maintenance schedules with optional filters
// @Tags maintenance
// @Produce json
// @Param limit query int false "Number of items per page (default 20)"
// @Param offset query int false "Pagination offset (default 0)"
// @Param machine_id query string false "Filter by machine ID (UUID)"
// @Param trigger_type query string false "Filter by trigger type"
// @Param is_active query string false "Filter by active status (true/false)"
// @Success 200 {object} ListResponse "List of schedules"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /maintenance/schedules [get]
func (h *MaintenanceHandler) ListSchedules(c *gin.Context) {
	filter := repositories.ScheduleFilter{
		Limit:  20,
		Offset: 0,
	}

	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && limit > 0 {
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

	if triggerType := c.Query("trigger_type"); triggerType != "" {
		filter.TriggerType = &triggerType
	}

	if isActiveStr := c.Query("is_active"); isActiveStr != "" {
		if isActive, err := strconv.ParseBool(isActiveStr); err == nil {
			filter.IsActive = &isActive
		}
	}

	schedules, total, err := h.maintRepo.ListSchedules(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list maintenance schedules")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve maintenance schedules",
		})
		return
	}

	c.JSON(http.StatusOK, ListResponse{
		Data:   schedules,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetScheduleByID returns a single maintenance schedule by ID.
// @Summary Get maintenance schedule by ID
// @Description Returns a single maintenance schedule by its unique identifier
// @Tags maintenance
// @Produce json
// @Param id path string true "Schedule ID (UUID)"
// @Success 200 {object} repositories.MaintenanceSchedule "Schedule details"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Schedule not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /maintenance/schedules/{id} [get]
func (h *MaintenanceHandler) GetScheduleByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid schedule ID format",
		})
		return
	}

	schedule, err := h.maintRepo.GetScheduleByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get maintenance schedule")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve maintenance schedule",
		})
		return
	}

	if schedule == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Maintenance schedule not found",
		})
		return
	}

	c.JSON(http.StatusOK, schedule)
}

// CreateSchedule creates a new maintenance schedule.
// @Summary Create maintenance schedule
// @Description Creates a new maintenance schedule for a machine
// @Tags maintenance
// @Accept json
// @Produce json
// @Param body body CreateScheduleRequest true "Schedule data"
// @Success 201 {object} repositories.MaintenanceSchedule "Created schedule"
// @Failure 400 {object} map[string]string "Validation error"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /maintenance/schedules [post]
func (h *MaintenanceHandler) CreateSchedule(c *gin.Context) {
	var req CreateScheduleRequest
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

	schedule := &repositories.MaintenanceSchedule{
		TenantID:           tenantUUID,
		MachineID:          req.MachineID,
		Name:               req.Name,
		Description:        req.Description,
		TriggerType:        req.TriggerType,
		Priority:           req.Priority,
		IntervalDays:       req.IntervalDays,
		IntervalHours:      req.IntervalHours,
		LastDoneHours:      req.LastDoneHours,
		NextDueHours:       req.NextDueHours,
		IntervalCycles:     req.IntervalCycles,
		LastDoneCycles:     req.LastDoneCycles,
		NextDueCycles:      req.NextDueCycles,
		ConditionMetric:    req.ConditionMetric,
		ConditionThreshold: req.ConditionThreshold,
		NextDueAt:          req.NextDueAt,
		AssignedTo:         req.AssignedTo,
		IsActive:           true,
		Metadata:           req.Metadata,
	}

	if req.IsActive != nil {
		schedule.IsActive = *req.IsActive
	}

	if schedule.Priority == 0 {
		schedule.Priority = 5
	}

	if err := h.maintRepo.CreateSchedule(c.Request.Context(), schedule); err != nil {
		h.log.WithError(err).Error("Failed to create maintenance schedule")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create maintenance schedule",
		})
		return
	}

	h.log.WithField("schedule_id", schedule.ID).Info("Maintenance schedule created")
	c.JSON(http.StatusCreated, schedule)
}

// UpdateSchedule modifies an existing maintenance schedule.
// @Summary Update maintenance schedule
// @Description Modifies an existing maintenance schedule
// @Tags maintenance
// @Accept json
// @Produce json
// @Param id path string true "Schedule ID (UUID)"
// @Param body body UpdateScheduleRequest true "Updated schedule data"
// @Success 200 {object} repositories.MaintenanceSchedule "Updated schedule"
// @Failure 400 {object} map[string]string "Invalid ID or validation error"
// @Failure 404 {object} map[string]string "Schedule not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /maintenance/schedules/{id} [put]
func (h *MaintenanceHandler) UpdateSchedule(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid schedule ID format",
		})
		return
	}

	schedule, err := h.maintRepo.GetScheduleByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get maintenance schedule")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve maintenance schedule",
		})
		return
	}

	if schedule == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Maintenance schedule not found",
		})
		return
	}

	var req UpdateScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Update fields
	if req.Name != "" {
		schedule.Name = req.Name
	}
	if req.Description != "" {
		schedule.Description = req.Description
	}
	if req.TriggerType != "" {
		schedule.TriggerType = req.TriggerType
	}
	if req.Priority > 0 {
		schedule.Priority = req.Priority
	}
	if req.IntervalDays != nil {
		schedule.IntervalDays = req.IntervalDays
	}
	if req.IntervalHours != nil {
		schedule.IntervalHours = req.IntervalHours
	}
	if req.LastDoneHours != nil {
		schedule.LastDoneHours = req.LastDoneHours
	}
	if req.NextDueHours != nil {
		schedule.NextDueHours = req.NextDueHours
	}
	if req.IntervalCycles != nil {
		schedule.IntervalCycles = req.IntervalCycles
	}
	if req.LastDoneCycles != nil {
		schedule.LastDoneCycles = req.LastDoneCycles
	}
	if req.NextDueCycles != nil {
		schedule.NextDueCycles = req.NextDueCycles
	}
	if req.ConditionMetric != nil {
		schedule.ConditionMetric = req.ConditionMetric
	}
	if req.ConditionThreshold != nil {
		schedule.ConditionThreshold = req.ConditionThreshold
	}
	if req.NextDueAt != nil {
		schedule.NextDueAt = req.NextDueAt
	}
	if req.AssignedTo != nil {
		schedule.AssignedTo = req.AssignedTo
	}
	if req.IsActive != nil {
		schedule.IsActive = *req.IsActive
	}
	if req.Metadata != nil {
		schedule.Metadata = req.Metadata
	}

	if err := h.maintRepo.UpdateSchedule(c.Request.Context(), schedule); err != nil {
		h.log.WithError(err).Error("Failed to update maintenance schedule")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to update maintenance schedule",
		})
		return
	}

	h.log.WithField("schedule_id", schedule.ID).Info("Maintenance schedule updated")
	c.JSON(http.StatusOK, schedule)
}

// DeleteSchedule removes a maintenance schedule.
// @Summary Delete maintenance schedule
// @Description Permanently removes a maintenance schedule
// @Tags maintenance
// @Produce json
// @Param id path string true "Schedule ID (UUID)"
// @Success 200 {object} map[string]string "Deletion confirmation"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Schedule not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /maintenance/schedules/{id} [delete]
func (h *MaintenanceHandler) DeleteSchedule(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid schedule ID format",
		})
		return
	}

	if err := h.maintRepo.DeleteSchedule(c.Request.Context(), id); err != nil {
		if err.Error() == "maintenance schedule not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Maintenance schedule not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to delete maintenance schedule")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to delete maintenance schedule",
		})
		return
	}

	h.log.WithField("schedule_id", id).Info("Maintenance schedule deleted")
	c.JSON(http.StatusOK, gin.H{
		"message": "Maintenance schedule deleted successfully",
	})
}

// =============== Work Order Endpoints ===============

// ListWorkOrders returns a paginated list of maintenance work orders.
// @Summary List maintenance work orders
// @Description Returns paginated list of work orders with optional filters
// @Tags maintenance
// @Produce json
// @Param limit query int false "Number of items per page (default 20)"
// @Param offset query int false "Pagination offset (default 0)"
// @Param machine_id query string false "Filter by machine ID (UUID)"
// @Param schedule_id query string false "Filter by schedule ID (UUID)"
// @Param status query string false "Filter by status"
// @Param assigned_to query string false "Filter by assigned user ID (UUID)"
// @Success 200 {object} ListResponse "List of work orders"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /maintenance/work-orders [get]
func (h *MaintenanceHandler) ListWorkOrders(c *gin.Context) {
	filter := repositories.WorkOrderFilter{
		Limit:  20,
		Offset: 0,
	}

	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && limit > 0 {
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

	if scheduleID := c.Query("schedule_id"); scheduleID != "" {
		if id, err := uuid.Parse(scheduleID); err == nil {
			filter.ScheduleID = &id
		}
	}

	if status := c.Query("status"); status != "" {
		filter.Status = &status
	}

	if assignedTo := c.Query("assigned_to"); assignedTo != "" {
		if id, err := uuid.Parse(assignedTo); err == nil {
			filter.AssignedTo = &id
		}
	}

	workOrders, total, err := h.maintRepo.ListWorkOrders(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list maintenance work orders")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve maintenance work orders",
		})
		return
	}

	c.JSON(http.StatusOK, ListResponse{
		Data:   workOrders,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetWorkOrderByID returns a single maintenance work order by ID.
// @Summary Get maintenance work order by ID
// @Description Returns a single work order by its unique identifier
// @Tags maintenance
// @Produce json
// @Param id path string true "Work Order ID (UUID)"
// @Success 200 {object} repositories.MaintenanceWorkOrder "Work order details"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Work order not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /maintenance/work-orders/{id} [get]
func (h *MaintenanceHandler) GetWorkOrderByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid work order ID format",
		})
		return
	}

	wo, err := h.maintRepo.GetWorkOrderByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get maintenance work order")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve maintenance work order",
		})
		return
	}

	if wo == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Maintenance work order not found",
		})
		return
	}

	c.JSON(http.StatusOK, wo)
}

// CreateWorkOrder creates a new maintenance work order.
// @Summary Create maintenance work order
// @Description Creates a new maintenance work order
// @Tags maintenance
// @Accept json
// @Produce json
// @Param body body CreateWorkOrderRequest true "Work order data"
// @Success 201 {object} repositories.MaintenanceWorkOrder "Created work order"
// @Failure 400 {object} map[string]string "Validation error"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /maintenance/work-orders [post]
func (h *MaintenanceHandler) CreateWorkOrder(c *gin.Context) {
	var req CreateWorkOrderRequest
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

	wo := &repositories.MaintenanceWorkOrder{
		TenantID:        tenantUUID,
		ScheduleID:      req.ScheduleID,
		MachineID:       req.MachineID,
		WorkOrderNumber: req.WorkOrderNumber,
		Title:           req.Title,
		Description:     req.Description,
		Status:          "open",
		Priority:        req.Priority,
		AssignedTo:      req.AssignedTo,
		Checklist:       req.Checklist,
		ScheduledAt:     req.ScheduledAt,
		DueAt:           req.DueAt,
		Notes:           req.Notes,
		Metadata:        req.Metadata,
	}

	if wo.Priority == 0 {
		wo.Priority = 5
	}

	if err := h.maintRepo.CreateWorkOrder(c.Request.Context(), wo); err != nil {
		h.log.WithError(err).Error("Failed to create maintenance work order")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create maintenance work order",
		})
		return
	}

	h.log.WithField("work_order_id", wo.ID).Info("Maintenance work order created")
	c.JSON(http.StatusCreated, wo)
}

// UpdateWorkOrder modifies an existing maintenance work order.
// @Summary Update maintenance work order
// @Description Modifies an existing maintenance work order
// @Tags maintenance
// @Accept json
// @Produce json
// @Param id path string true "Work Order ID (UUID)"
// @Param body body UpdateWorkOrderRequest true "Updated work order data"
// @Success 200 {object} repositories.MaintenanceWorkOrder "Updated work order"
// @Failure 400 {object} map[string]string "Invalid ID or validation error"
// @Failure 404 {object} map[string]string "Work order not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /maintenance/work-orders/{id} [put]
func (h *MaintenanceHandler) UpdateWorkOrder(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid work order ID format",
		})
		return
	}

	wo, err := h.maintRepo.GetWorkOrderByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get maintenance work order")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve maintenance work order",
		})
		return
	}

	if wo == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Maintenance work order not found",
		})
		return
	}

	var req UpdateWorkOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Update fields
	if req.Title != "" {
		wo.Title = req.Title
	}
	if req.Description != "" {
		wo.Description = req.Description
	}
	if req.Status != "" {
		wo.Status = req.Status
		if req.Status == "in_progress" && wo.StartedAt == nil {
			now := time.Now()
			wo.StartedAt = &now
		}
	}
	if req.Priority > 0 {
		wo.Priority = req.Priority
	}
	if req.AssignedTo != nil {
		wo.AssignedTo = req.AssignedTo
	}
	if req.Checklist != nil {
		wo.Checklist = req.Checklist
	}
	if req.ScheduledAt != nil {
		wo.ScheduledAt = req.ScheduledAt
	}
	if req.DueAt != nil {
		wo.DueAt = req.DueAt
	}
	if req.Notes != "" {
		wo.Notes = req.Notes
	}
	if req.PartsUsed != nil {
		wo.PartsUsed = req.PartsUsed
	}
	if req.Metadata != nil {
		wo.Metadata = req.Metadata
	}

	if err := h.maintRepo.UpdateWorkOrder(c.Request.Context(), wo); err != nil {
		h.log.WithError(err).Error("Failed to update maintenance work order")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to update maintenance work order",
		})
		return
	}

	h.log.WithField("work_order_id", wo.ID).Info("Maintenance work order updated")
	c.JSON(http.StatusOK, wo)
}

// CompleteWorkOrder marks a work order as completed.
// @Summary Complete maintenance work order
// @Description Marks a work order as completed, advances the associated schedule
// @Tags maintenance
// @Accept json
// @Produce json
// @Param id path string true "Work Order ID (UUID)"
// @Param body body CompleteWorkOrderRequest true "Completion data"
// @Success 200 {object} map[string]string "Completion confirmation"
// @Failure 400 {object} map[string]string "Invalid ID or validation error"
// @Failure 404 {object} map[string]string "Work order not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /maintenance/work-orders/{id}/complete [post]
func (h *MaintenanceHandler) CompleteWorkOrder(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid work order ID format",
		})
		return
	}

	var req CompleteWorkOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	if h.maintService != nil {
		if err := h.maintService.CompleteWorkOrder(c.Request.Context(), id, req.Notes); err != nil {
			if err.Error() == "work order not found" {
				c.JSON(http.StatusNotFound, gin.H{
					"error":   "not_found",
					"message": "Maintenance work order not found",
				})
				return
			}
			h.log.WithError(err).Error("Failed to complete maintenance work order")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to complete maintenance work order",
			})
			return
		}
	} else {
		// Fallback to repo-level completion without schedule advancement
		if err := h.maintRepo.CompleteWorkOrder(c.Request.Context(), id, req.Notes); err != nil {
			if err.Error() == "maintenance work order not found" {
				c.JSON(http.StatusNotFound, gin.H{
					"error":   "not_found",
					"message": "Maintenance work order not found",
				})
				return
			}
			h.log.WithError(err).Error("Failed to complete maintenance work order")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to complete maintenance work order",
			})
			return
		}
	}

	h.log.WithField("work_order_id", id).Info("Maintenance work order completed")
	c.JSON(http.StatusOK, gin.H{
		"message": "Maintenance work order completed successfully",
	})
}

// GetMachineMaintenance returns all work orders for a specific machine.
// @Summary Get machine maintenance
// @Description Returns all maintenance work orders for a specific machine
// @Tags maintenance
// @Produce json
// @Param id path string true "Machine ID (UUID)"
// @Success 200 {object} map[string]interface{} "Machine maintenance data"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /machines/{id}/maintenance [get]
func (h *MaintenanceHandler) GetMachineMaintenance(c *gin.Context) {
	machineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid machine ID format",
		})
		return
	}

	workOrders, err := h.maintRepo.GetByMachine(c.Request.Context(), machineID)
	if err != nil {
		h.log.WithError(err).Error("Failed to get machine maintenance")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve machine maintenance data",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":       workOrders,
		"machine_id": machineID,
		"total":      len(workOrders),
	})
}
