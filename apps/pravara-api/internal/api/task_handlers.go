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
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// TaskHandler handles task-related HTTP requests (Kanban board operations).
type TaskHandler struct {
	repo       *repositories.TaskRepository
	log        *logrus.Logger
	publisher  *pubsub.Publisher
	automation *services.AutomationService
}

// SetPublisher sets the event publisher for real-time updates.
func (h *TaskHandler) SetPublisher(p *pubsub.Publisher) {
	h.publisher = p
}

// SetAutomation sets the automation service for machine-task integration.
func (h *TaskHandler) SetAutomation(a *services.AutomationService) {
	h.automation = a
}

// NewTaskHandler creates a new task handler.
func NewTaskHandler(repo *repositories.TaskRepository, log *logrus.Logger) *TaskHandler {
	return &TaskHandler{
		repo: repo,
		log:  log,
	}
}

// CreateTaskRequest represents the request body for creating a task.
type CreateTaskRequest struct {
	OrderID          *uuid.UUID     `json:"order_id"`
	OrderItemID      *uuid.UUID     `json:"order_item_id"`
	MachineID        *uuid.UUID     `json:"machine_id"`
	AssignedUserID   *uuid.UUID     `json:"assigned_user_id"`
	Title            string         `json:"title" binding:"required"`
	Description      string         `json:"description"`
	Priority         int            `json:"priority"`
	EstimatedMinutes int            `json:"estimated_minutes"`
	Metadata         map[string]any `json:"metadata"`
}

// UpdateTaskRequest represents the request body for updating a task.
type UpdateTaskRequest struct {
	OrderID          *uuid.UUID     `json:"order_id"`
	OrderItemID      *uuid.UUID     `json:"order_item_id"`
	MachineID        *uuid.UUID     `json:"machine_id"`
	AssignedUserID   *uuid.UUID     `json:"assigned_user_id"`
	Title            string         `json:"title"`
	Description      string         `json:"description"`
	Status           string         `json:"status"`
	Priority         int            `json:"priority"`
	EstimatedMinutes int            `json:"estimated_minutes"`
	ActualMinutes    int            `json:"actual_minutes"`
	Metadata         map[string]any `json:"metadata"`
}

// MoveTaskRequest represents the request body for moving a task on the Kanban board.
type MoveTaskRequest struct {
	Status   string `json:"status" binding:"required"`
	Position int    `json:"position" binding:"required,min=1"`
}

// AssignTaskRequest represents the request body for assigning a task.
type AssignTaskRequest struct {
	UserID    *uuid.UUID `json:"user_id"`
	MachineID *uuid.UUID `json:"machine_id"`
}

// List returns a paginated list of tasks.
func (h *TaskHandler) List(c *gin.Context) {
	filter := repositories.TaskFilter{
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
		s := types.TaskStatus(status)
		filter.Status = &s
	}

	if machineID := c.Query("machine_id"); machineID != "" {
		if id, err := uuid.Parse(machineID); err == nil {
			filter.MachineID = &id
		}
	}

	if orderID := c.Query("order_id"); orderID != "" {
		if id, err := uuid.Parse(orderID); err == nil {
			filter.OrderID = &id
		}
	}

	if userID := c.Query("user_id"); userID != "" {
		if id, err := uuid.Parse(userID); err == nil {
			filter.UserID = &id
		}
	}

	tasks, total, err := h.repo.List(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list tasks")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve tasks",
		})
		return
	}

	c.JSON(http.StatusOK, ListResponse{
		Data:   tasks,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetByID returns a single task by ID.
func (h *TaskHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid task ID format",
		})
		return
	}

	task, err := h.repo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get task")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve task",
		})
		return
	}

	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Task not found",
		})
		return
	}

	c.JSON(http.StatusOK, task)
}

// Create creates a new task.
func (h *TaskHandler) Create(c *gin.Context) {
	var req CreateTaskRequest
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

	task := &types.Task{
		TenantID:         tenantUUID,
		OrderID:          req.OrderID,
		OrderItemID:      req.OrderItemID,
		MachineID:        req.MachineID,
		AssignedUserID:   req.AssignedUserID,
		Title:            req.Title,
		Description:      req.Description,
		Status:           types.TaskStatusBacklog,
		Priority:         req.Priority,
		EstimatedMinutes: req.EstimatedMinutes,
		Metadata:         req.Metadata,
	}

	if task.Priority == 0 {
		task.Priority = 5
	}

	if err := h.repo.Create(c.Request.Context(), task); err != nil {
		h.log.WithError(err).Error("Failed to create task")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create task",
		})
		return
	}

	h.log.WithField("task_id", task.ID).Info("Task created")
	c.JSON(http.StatusCreated, task)
}

// Update modifies an existing task.
func (h *TaskHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid task ID format",
		})
		return
	}

	task, err := h.repo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get task")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve task",
		})
		return
	}

	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Task not found",
		})
		return
	}

	var req UpdateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Update fields
	if req.OrderID != nil {
		task.OrderID = req.OrderID
	}
	if req.OrderItemID != nil {
		task.OrderItemID = req.OrderItemID
	}
	if req.MachineID != nil {
		task.MachineID = req.MachineID
	}
	if req.AssignedUserID != nil {
		task.AssignedUserID = req.AssignedUserID
	}
	if req.Title != "" {
		task.Title = req.Title
	}
	if req.Description != "" {
		task.Description = req.Description
	}
	if req.Status != "" {
		newStatus := types.TaskStatus(req.Status)
		// Track status transitions
		if task.Status != newStatus {
			if newStatus == types.TaskStatusInProgress && task.StartedAt == nil {
				now := time.Now()
				task.StartedAt = &now
			}
			if newStatus == types.TaskStatusCompleted && task.CompletedAt == nil {
				now := time.Now()
				task.CompletedAt = &now
			}
		}
		task.Status = newStatus
	}
	if req.Priority > 0 {
		task.Priority = req.Priority
	}
	if req.EstimatedMinutes > 0 {
		task.EstimatedMinutes = req.EstimatedMinutes
	}
	if req.ActualMinutes > 0 {
		task.ActualMinutes = req.ActualMinutes
	}
	if req.Metadata != nil {
		task.Metadata = req.Metadata
	}

	if err := h.repo.Update(c.Request.Context(), task); err != nil {
		h.log.WithError(err).Error("Failed to update task")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to update task",
		})
		return
	}

	h.log.WithField("task_id", task.ID).Info("Task updated")
	c.JSON(http.StatusOK, task)
}

// Move changes a task's status and/or position on the Kanban board.
func (h *TaskHandler) Move(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid task ID format",
		})
		return
	}

	var req MoveTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	newStatus := types.TaskStatus(req.Status)

	// Validate status
	validStatuses := map[types.TaskStatus]bool{
		types.TaskStatusBacklog:      true,
		types.TaskStatusQueued:       true,
		types.TaskStatusInProgress:   true,
		types.TaskStatusQualityCheck: true,
		types.TaskStatusCompleted:    true,
		types.TaskStatusBlocked:      true,
	}

	if !validStatuses[newStatus] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_status",
			"message": "Invalid task status",
		})
		return
	}

	// Get task before move to capture old status for automation
	task, err := h.repo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get task for move")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve task",
		})
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Task not found",
		})
		return
	}

	oldStatus := task.Status

	if err := h.repo.MoveTask(c.Request.Context(), id, newStatus, req.Position); err != nil {
		if err.Error() == "task not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Task not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to move task")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to move task",
		})
		return
	}

	// Trigger automation if status changed
	if oldStatus != newStatus && h.automation != nil {
		// Get user ID for automation tracking
		userID, _ := middleware.GetUserID(c)
		userUUID, _ := uuid.Parse(userID)

		// Update task status for automation
		task.Status = newStatus

		// Trigger automation (non-blocking - errors are logged)
		if err := h.automation.OnTaskStatusChange(c.Request.Context(), task, oldStatus, newStatus, userUUID); err != nil {
			h.log.WithError(err).WithFields(logrus.Fields{
				"task_id":    id,
				"old_status": oldStatus,
				"new_status": newStatus,
			}).Warn("Automation failed for task move")
			// Don't fail the request - the move succeeded, automation is best-effort
		}
	}

	// Publish task move event for real-time updates
	if h.publisher != nil {
		userID, _ := middleware.GetUserID(c)
		userUUID, _ := uuid.Parse(userID)
		h.publisher.PublishTaskMove(c.Request.Context(), task.TenantID, pubsub.TaskMoveData{
			TaskID:      task.ID,
			TaskTitle:   task.Title,
			OldStatus:   string(oldStatus),
			NewStatus:   string(newStatus),
			OldPosition: task.KanbanPosition,
			NewPosition: req.Position,
			MovedBy:     userUUID,
			MovedAt:     time.Now().UTC(),
		})
	}

	h.log.WithFields(logrus.Fields{
		"task_id":  id,
		"status":   newStatus,
		"position": req.Position,
	}).Info("Task moved")

	c.JSON(http.StatusOK, gin.H{
		"message": "Task moved successfully",
	})
}

// Assign assigns a task to a user and/or machine.
func (h *TaskHandler) Assign(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid task ID format",
		})
		return
	}

	var req AssignTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	if err := h.repo.AssignTask(c.Request.Context(), id, req.UserID, req.MachineID); err != nil {
		if err.Error() == "task not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Task not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to assign task")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to assign task",
		})
		return
	}

	h.log.WithFields(logrus.Fields{
		"task_id":    id,
		"user_id":    req.UserID,
		"machine_id": req.MachineID,
	}).Info("Task assigned")

	c.JSON(http.StatusOK, gin.H{
		"message": "Task assigned successfully",
	})
}

// Delete removes a task.
func (h *TaskHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid task ID format",
		})
		return
	}

	if err := h.repo.Delete(c.Request.Context(), id); err != nil {
		if err.Error() == "task not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Task not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to delete task")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to delete task",
		})
		return
	}

	h.log.WithField("task_id", id).Info("Task deleted")
	c.JSON(http.StatusOK, gin.H{
		"message": "Task deleted successfully",
	})
}

// GetKanbanBoard returns all tasks grouped by status for the Kanban board view.
func (h *TaskHandler) GetKanbanBoard(c *gin.Context) {
	board, err := h.repo.GetKanbanBoard(c.Request.Context())
	if err != nil {
		h.log.WithError(err).Error("Failed to get Kanban board")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve Kanban board",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"columns": board,
	})
}
