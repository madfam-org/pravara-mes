package api

import (
	"errors"
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
	refs       repositories.DBTX
	log        *logrus.Logger
	publisher  *pubsub.Publisher
	automation *services.AutomationService
	rollup     *services.OrderRollupService
}

// SetPublisher sets the event publisher for real-time updates.
func (h *TaskHandler) SetPublisher(p *pubsub.Publisher) {
	h.publisher = p
}

// SetAutomation sets the automation service for machine-task integration.
func (h *TaskHandler) SetAutomation(a *services.AutomationService) {
	h.automation = a
}

// SetRollup sets the order roll-up service so task status changes advance
// the parent order (first task started -> in_progress; all tasks completed
// -> completed).
func (h *TaskHandler) SetRollup(r *services.OrderRollupService) {
	h.rollup = r
}

// NewTaskHandler creates a new task handler. refs is used to check that the
// order, order item, machine and user a request references belong to the
// caller's tenant.
func NewTaskHandler(repo *repositories.TaskRepository, refs repositories.DBTX, log *logrus.Logger) *TaskHandler {
	return &TaskHandler{
		repo: repo,
		refs: refs,
		log:  log,
	}
}

// taskRefs lists the rows a task create or update references.
func taskRefs(orderID, orderItemID, machineID, userID *uuid.UUID) []tenantRef {
	return []tenantRef{
		optRef(repositories.RefOrders, orderID),
		optRef(repositories.RefOrderItems, orderItemID),
		optRef(repositories.RefMachines, machineID),
		optRef(repositories.RefUsers, userID),
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
	// Status is the status the task enters.
	Status string `json:"status" binding:"required"`
	// FromStatus, when present, is the status the caller says the task
	// leaves. A task in any other status is not moved: 409 status_conflict
	// with its current status (RFC 0064 §4.1, ruling PV-5). Absent, the move
	// applies whatever the task's status is, as it always has.
	FromStatus string `json:"from_status,omitempty"`
	// Position is the 1-based board position in the target column. Absent,
	// the task goes to the end of that column.
	Position *int `json:"position,omitempty" binding:"omitempty,min=1"`
}

// TaskMoveResponse is the body of a successful POST /v1/tasks/:id/move.
type TaskMoveResponse struct {
	Message        string           `json:"message"`
	Status         types.TaskStatus `json:"status"`
	KanbanPosition int              `json:"kanban_position"`
	PreviousStatus types.TaskStatus `json:"previous_status"`
}

// TaskStatusConflictResponse is the 409 of POST /v1/tasks/:id/move when the
// task is not in the request's from_status. Nothing was changed.
type TaskStatusConflictResponse struct {
	Error         string           `json:"error"`
	Message       string           `json:"message"`
	CurrentStatus types.TaskStatus `json:"current_status"`
}

// TaskBoardBusyResponse is the 503 of POST /v1/tasks/:id/move when another
// move held the board or the task for longer than
// repositories.TaskBoardLockTimeout. Nothing was changed; retry after
// Retry-After seconds.
type TaskBoardBusyResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// TaskBoardBusyRetryAfterSeconds is the Retry-After of that 503.
const TaskBoardBusyRetryAfterSeconds = 1

// validTaskStatuses are the six board columns (migration 001).
var validTaskStatuses = map[types.TaskStatus]bool{
	types.TaskStatusBacklog:      true,
	types.TaskStatusQueued:       true,
	types.TaskStatusInProgress:   true,
	types.TaskStatusQualityCheck: true,
	types.TaskStatusCompleted:    true,
	types.TaskStatusBlocked:      true,
}

// AssignTaskRequest represents the request body for assigning a task.
type AssignTaskRequest struct {
	UserID    *uuid.UUID `json:"user_id"`
	MachineID *uuid.UUID `json:"machine_id"`
}

// List returns a paginated list of tasks.
// @Summary List tasks
// @Description Returns a paginated list of tasks with optional filtering by status, machine, order, or user
// @Tags tasks
// @Produce json
// @Param limit query int false "Number of results per page" default(50)
// @Param offset query int false "Offset for pagination" default(0)
// @Param status query string false "Filter by task status" Enums(backlog, queued, in_progress, quality_check, completed, blocked)
// @Param machine_id query string false "Filter by machine ID (UUID)"
// @Param order_id query string false "Filter by order ID (UUID)"
// @Param user_id query string false "Filter by assigned user ID (UUID)"
// @Success 200 {object} ListResponse "Paginated task list"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /tasks [get]
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

	if isTerminalOnly(c) {
		tasks = terminalTasks(tasks)
	}
	c.JSON(http.StatusOK, TaskListResponse{
		Data:   tasks,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetByID returns a single task by ID.
// @Summary Get task by ID
// @Description Returns a single task with all details
// @Tags tasks
// @Produce json
// @Param id path string true "Task ID (UUID)"
// @Success 200 {object} types.Task "Task details"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Task not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /tasks/{id} [get]
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

	if isTerminalOnly(c) {
		narrowed := terminalTask(*task)
		task = &narrowed
	}
	c.JSON(http.StatusOK, task)
}

// Create creates a new task.
// @Summary Create a new task
// @Description Creates a new task in backlog status
// @Tags tasks
// @Accept json
// @Produce json
// @Param body body CreateTaskRequest true "Task creation data"
// @Success 201 {object} types.Task "Created task"
// @Failure 400 {object} map[string]string "Validation error"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 404 {object} map[string]string "Referenced order, order item, machine or user not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /tasks [post]
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

	if !requireTenantRefs(c, h.refs, h.log, taskRefs(req.OrderID, req.OrderItemID, req.MachineID, req.AssignedUserID)...) {
		return
	}

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

	// Publish task.created through the outbox-backed publisher.
	if h.publisher != nil {
		userUUID := middleware.ActorUUID(c)
		metadata := map[string]any{}
		if task.OrderID != nil {
			metadata["order_id"] = task.OrderID.String()
		}
		if task.MachineID != nil {
			metadata["machine_id"] = task.MachineID.String()
		}
		if err := h.publisher.PublishEntityCreated(c.Request.Context(), pubsub.NamespaceTasks, task.TenantID, pubsub.EventTaskCreated, pubsub.EntityCreatedData{
			EntityID:   task.ID,
			EntityType: "task",
			Name:       task.Title,
			CreatedBy:  userUUID,
			CreatedAt:  time.Now().UTC(),
			Metadata:   metadata,
		}); err != nil {
			h.log.WithError(err).Warn("Failed to publish task.created event")
		}
	}

	h.log.WithField("task_id", task.ID).Info("Task created")
	c.JSON(http.StatusCreated, task)
}

// Update modifies an existing task.
// @Summary Update a task
// @Description Updates task fields including status transitions
// @Tags tasks
// @Accept json
// @Produce json
// @Param id path string true "Task ID (UUID)"
// @Param body body UpdateTaskRequest true "Task update data"
// @Success 200 {object} types.Task "Updated task"
// @Failure 400 {object} map[string]string "Validation error"
// @Failure 404 {object} map[string]string "Task, or a referenced row, not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /tasks/{id} [put]
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

	if !requireTenantRefs(c, h.refs, h.log, taskRefs(req.OrderID, req.OrderItemID, req.MachineID, req.AssignedUserID)...) {
		return
	}

	oldStatus := task.Status

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

	if task.Status != oldStatus {
		// Publish task.updated so the status transition is durable/visible.
		if h.publisher != nil {
			userUUID := middleware.ActorUUID(c)
			if err := h.publisher.PublishEntityUpdated(c.Request.Context(), pubsub.NamespaceTasks, task.TenantID, pubsub.EventTaskUpdated, pubsub.EntityUpdatedData{
				EntityID:      task.ID,
				EntityType:    "task",
				Name:          task.Title,
				ChangedFields: []string{"status"},
				UpdatedBy:     userUUID,
				UpdatedAt:     time.Now().UTC(),
				Metadata: map[string]any{
					"old_status": string(oldStatus),
					"new_status": string(task.Status),
				},
			}); err != nil {
				h.log.WithError(err).Warn("Failed to publish task.updated event")
			}
		}

		// Roll the parent order's status up from its tasks.
		if h.rollup != nil {
			h.rollup.OnTaskStatusChanged(c.Request.Context(), task, task.Status)
		}
	}

	h.log.WithField("task_id", task.ID).Info("Task updated")
	c.JSON(http.StatusOK, task)
}

// Move changes a task's status and/or position on the Kanban board.
// @Summary Move task on Kanban board
// @Description Changes task status and position, triggers automation for machine jobs. from_status (optional) is the status the task leaves: when the task is in any other status nothing changes and the answer is 409 status_conflict with current_status. position (optional, at least 1) is the place in the target column; without it the task goes to the end of that column.
// @Tags tasks
// @Accept json
// @Produce json
// @Param id path string true "Task ID (UUID)"
// @Param body body MoveTaskRequest true "Move request: target status, optional from_status and position"
// @Success 200 {object} TaskMoveResponse "Task moved"
// @Failure 400 {object} map[string]string "validation_error, invalid_status or invalid_from_status"
// @Failure 404 {object} map[string]string "Task not found"
// @Failure 409 {object} TaskStatusConflictResponse "status_conflict: the task is not in from_status"
// @Failure 503 {object} TaskBoardBusyResponse "task_board_busy: another move held the board for more than 5 s; retry"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /tasks/{id}/move [post]
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
	if !validTaskStatuses[newStatus] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_status",
			"message": "Invalid task status",
		})
		return
	}
	var expected *types.TaskStatus
	if req.FromStatus != "" {
		from := types.TaskStatus(req.FromStatus)
		if !validTaskStatuses[from] {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_from_status",
				"message": "from_status is not a task status",
			})
			return
		}
		expected = &from
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

	move, err := h.repo.MoveTaskExpecting(c.Request.Context(), id, newStatus, req.Position, expected)
	if err != nil {
		var conflict *repositories.TaskStatusConflictError
		switch {
		case errors.Is(err, repositories.ErrTaskNotFound):
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Task not found",
			})
		case errors.Is(err, repositories.ErrTaskBoardBusy):
			c.Header("Retry-After", strconv.Itoa(TaskBoardBusyRetryAfterSeconds))
			c.JSON(http.StatusServiceUnavailable, TaskBoardBusyResponse{
				Error:   "task_board_busy",
				Message: "Another move of this board is still running; retry",
			})
		case errors.As(err, &conflict):
			c.JSON(http.StatusConflict, TaskStatusConflictResponse{
				Error:         "status_conflict",
				Message:       "The task is not in from_status; nothing was changed",
				CurrentStatus: conflict.Current,
			})
		default:
			h.log.WithError(err).Error("Failed to move task")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to move task",
			})
		}
		return
	}
	// The status the move left, read under the row lock.
	oldStatus := move.OldStatus

	// Trigger automation if status changed
	if oldStatus != newStatus && h.automation != nil {
		// Get user ID for automation tracking (issued_by references users;
		// machine callers are recorded as NULL there).
		userUUID := middleware.ActorUserUUID(c)

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

	// Roll the parent order's status up from its tasks (first task started
	// -> in_progress; all tasks completed -> completed). Best-effort.
	if oldStatus != newStatus && h.rollup != nil {
		task.Status = newStatus
		h.rollup.OnTaskStatusChanged(c.Request.Context(), task, newStatus)
	}

	// Publish task move event for real-time updates
	if h.publisher != nil {
		userUUID := middleware.ActorUUID(c)
		h.publisher.PublishTaskMove(c.Request.Context(), task.TenantID, pubsub.TaskMoveData{
			TaskID:      task.ID,
			TaskTitle:   task.Title,
			OldStatus:   string(oldStatus),
			NewStatus:   string(newStatus),
			OldPosition: move.OldPosition,
			NewPosition: move.NewPosition,
			MovedBy:     userUUID,
			MovedAt:     time.Now().UTC(),
		})
	}

	h.log.WithFields(logrus.Fields{
		"task_id":  id,
		"status":   newStatus,
		"position": move.NewPosition,
	}).Info("Task moved")

	c.JSON(http.StatusOK, TaskMoveResponse{
		Message:        "Task moved successfully",
		Status:         move.NewStatus,
		KanbanPosition: move.NewPosition,
		PreviousStatus: move.OldStatus,
	})
}

// Assign assigns a task to a user and/or machine.
// @Summary Assign task to user/machine
// @Description Assigns task to a user and/or machine for processing
// @Tags tasks
// @Accept json
// @Produce json
// @Param id path string true "Task ID (UUID)"
// @Param body body AssignTaskRequest true "Assignment data"
// @Success 200 {object} map[string]string "Task assigned successfully"
// @Failure 400 {object} map[string]string "Validation error"
// @Failure 404 {object} map[string]string "Task, or a referenced row, not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /tasks/{id}/assign [post]
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

	if !requireTenantRefs(c, h.refs, h.log,
		optRef(repositories.RefUsers, req.UserID),
		optRef(repositories.RefMachines, req.MachineID)) {
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
// @Summary Delete a task
// @Description Permanently deletes a task
// @Tags tasks
// @Produce json
// @Param id path string true "Task ID (UUID)"
// @Success 200 {object} map[string]string "Task deleted successfully"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Task not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /tasks/{id} [delete]
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
// @Summary Get Kanban board
// @Description Returns all tasks grouped by status columns for the Kanban board UI
// @Tags tasks
// @Produce json
// @Success 200 {object} map[string]interface{} "Kanban board columns with tasks"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /tasks/board [get]
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

	if isTerminalOnly(c) {
		for status, tasks := range board {
			board[status] = terminalTasks(tasks)
		}
	}
	c.JSON(http.StatusOK, KanbanBoardResponse{Columns: board})
}

// TaskListResponse is the body of GET /v1/tasks (ListResponse's JSON, typed).
type TaskListResponse struct {
	Data   []types.Task `json:"data"`
	Total  int          `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

// KanbanBoardResponse is the body of GET /v1/tasks/board: the tenant's
// tasks keyed by status.
type KanbanBoardResponse struct {
	Columns map[types.TaskStatus][]types.Task `json:"columns"`
}
