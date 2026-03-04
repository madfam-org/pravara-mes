package api

import (
	"encoding/json"
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

// WorkInstructionHandler handles work instruction HTTP requests.
type WorkInstructionHandler struct {
	wiRepo    *repositories.WorkInstructionRepository
	wiService *services.WorkInstructionService
	publisher *pubsub.Publisher
	log       *logrus.Logger
}

// NewWorkInstructionHandler creates a new work instruction handler.
func NewWorkInstructionHandler(wiRepo *repositories.WorkInstructionRepository, log *logrus.Logger) *WorkInstructionHandler {
	return &WorkInstructionHandler{
		wiRepo: wiRepo,
		log:    log,
	}
}

// SetPublisher sets the event publisher for real-time updates.
func (h *WorkInstructionHandler) SetPublisher(p *pubsub.Publisher) {
	h.publisher = p
}

// SetWIService sets the work instruction service.
func (h *WorkInstructionHandler) SetWIService(s *services.WorkInstructionService) {
	h.wiService = s
}

// CreateWorkInstructionRequest represents the request body for creating a work instruction.
type CreateWorkInstructionRequest struct {
	Title               string          `json:"title" binding:"required"`
	Version             string          `json:"version" binding:"required"`
	Category            string          `json:"category" binding:"required"` // setup, operation, safety, maintenance
	Description         string          `json:"description"`
	ProductDefinitionID *uuid.UUID      `json:"product_definition_id"`
	MachineType         *string         `json:"machine_type"`
	Steps               json.RawMessage `json:"steps"`
	ToolsRequired       json.RawMessage `json:"tools_required"`
	PPERequired         json.RawMessage `json:"ppe_required"`
	IsActive            *bool           `json:"is_active"`
	Metadata            map[string]any  `json:"metadata"`
}

// UpdateWorkInstructionRequest represents the request body for updating a work instruction.
type UpdateWorkInstructionRequest struct {
	Title               string          `json:"title"`
	Version             string          `json:"version"`
	Category            string          `json:"category"`
	Description         string          `json:"description"`
	ProductDefinitionID *uuid.UUID      `json:"product_definition_id"`
	MachineType         *string         `json:"machine_type"`
	Steps               json.RawMessage `json:"steps"`
	ToolsRequired       json.RawMessage `json:"tools_required"`
	PPERequired         json.RawMessage `json:"ppe_required"`
	IsActive            *bool           `json:"is_active"`
	Metadata            map[string]any  `json:"metadata"`
}

// AttachWorkInstructionRequest represents the request body for attaching a WI to a task.
type AttachWorkInstructionRequest struct {
	WorkInstructionID uuid.UUID `json:"work_instruction_id" binding:"required"`
}

// AcknowledgeStepRequest represents the request body for acknowledging a WI step.
type AcknowledgeStepRequest struct {
	StepNumber int `json:"step_number" binding:"required"`
}

// ListWorkInstructions returns a paginated list of work instructions.
func (h *WorkInstructionHandler) ListWorkInstructions(c *gin.Context) {
	filter := repositories.WorkInstructionFilter{
		Limit:  20,
		Offset: 0,
	}

	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && limit > 0 {
		filter.Limit = limit
	}
	if offset, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && offset >= 0 {
		filter.Offset = offset
	}

	if category := c.Query("category"); category != "" {
		filter.Category = &category
	}

	if productDefID := c.Query("product_definition_id"); productDefID != "" {
		if id, err := uuid.Parse(productDefID); err == nil {
			filter.ProductDefinitionID = &id
		}
	}

	if machineType := c.Query("machine_type"); machineType != "" {
		filter.MachineType = &machineType
	}

	if isActive := c.Query("is_active"); isActive != "" {
		active := isActive == "true"
		filter.IsActive = &active
	}

	instructions, total, err := h.wiRepo.List(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list work instructions")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve work instructions",
		})
		return
	}

	c.JSON(http.StatusOK, ListResponse{
		Data:   instructions,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetWorkInstructionByID returns a single work instruction by ID.
func (h *WorkInstructionHandler) GetWorkInstructionByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid work instruction ID format",
		})
		return
	}

	wi, err := h.wiRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get work instruction")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve work instruction",
		})
		return
	}

	if wi == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Work instruction not found",
		})
		return
	}

	c.JSON(http.StatusOK, wi)
}

// CreateWorkInstruction creates a new work instruction.
func (h *WorkInstructionHandler) CreateWorkInstruction(c *gin.Context) {
	var req CreateWorkInstructionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Validate category
	switch req.Category {
	case "setup", "operation", "safety", "maintenance":
		// valid
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "Category must be one of: setup, operation, safety, maintenance",
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

	wi := &repositories.WorkInstruction{
		TenantID:            tenantUUID,
		Title:               req.Title,
		Version:             req.Version,
		Category:            req.Category,
		Description:         req.Description,
		ProductDefinitionID: req.ProductDefinitionID,
		MachineType:         req.MachineType,
		Steps:               req.Steps,
		ToolsRequired:       req.ToolsRequired,
		PPERequired:         req.PPERequired,
		IsActive:            true,
		Metadata:            req.Metadata,
	}

	if req.IsActive != nil {
		wi.IsActive = *req.IsActive
	}

	if err := h.wiRepo.Create(c.Request.Context(), wi); err != nil {
		h.log.WithError(err).Error("Failed to create work instruction")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create work instruction",
		})
		return
	}

	h.log.WithField("work_instruction_id", wi.ID).Info("Work instruction created")
	c.JSON(http.StatusCreated, wi)
}

// UpdateWorkInstruction modifies an existing work instruction.
func (h *WorkInstructionHandler) UpdateWorkInstruction(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid work instruction ID format",
		})
		return
	}

	wi, err := h.wiRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get work instruction")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve work instruction",
		})
		return
	}

	if wi == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Work instruction not found",
		})
		return
	}

	var req UpdateWorkInstructionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Update fields
	if req.Title != "" {
		wi.Title = req.Title
	}
	if req.Version != "" {
		wi.Version = req.Version
	}
	if req.Category != "" {
		switch req.Category {
		case "setup", "operation", "safety", "maintenance":
			wi.Category = req.Category
		default:
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "validation_error",
				"message": "Category must be one of: setup, operation, safety, maintenance",
			})
			return
		}
	}
	if req.Description != "" {
		wi.Description = req.Description
	}
	if req.ProductDefinitionID != nil {
		wi.ProductDefinitionID = req.ProductDefinitionID
	}
	if req.MachineType != nil {
		wi.MachineType = req.MachineType
	}
	if req.Steps != nil {
		wi.Steps = req.Steps
	}
	if req.ToolsRequired != nil {
		wi.ToolsRequired = req.ToolsRequired
	}
	if req.PPERequired != nil {
		wi.PPERequired = req.PPERequired
	}
	if req.IsActive != nil {
		wi.IsActive = *req.IsActive
	}
	if req.Metadata != nil {
		wi.Metadata = req.Metadata
	}

	if err := h.wiRepo.Update(c.Request.Context(), wi); err != nil {
		h.log.WithError(err).Error("Failed to update work instruction")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to update work instruction",
		})
		return
	}

	h.log.WithField("work_instruction_id", wi.ID).Info("Work instruction updated")
	c.JSON(http.StatusOK, wi)
}

// DeleteWorkInstruction removes a work instruction.
func (h *WorkInstructionHandler) DeleteWorkInstruction(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid work instruction ID format",
		})
		return
	}

	if err := h.wiRepo.Delete(c.Request.Context(), id); err != nil {
		if err.Error() == "work instruction not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Work instruction not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to delete work instruction")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to delete work instruction",
		})
		return
	}

	h.log.WithField("work_instruction_id", id).Info("Work instruction deleted")
	c.JSON(http.StatusOK, gin.H{
		"message": "Work instruction deleted successfully",
	})
}

// AttachToTask attaches a work instruction to a task.
// POST /v1/tasks/:id/work-instructions
func (h *WorkInstructionHandler) AttachToTask(c *gin.Context) {
	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid task ID format",
		})
		return
	}

	var req AttachWorkInstructionRequest
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

	// Verify work instruction exists
	wi, err := h.wiRepo.GetByID(c.Request.Context(), req.WorkInstructionID)
	if err != nil {
		h.log.WithError(err).Error("Failed to get work instruction")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to verify work instruction",
		})
		return
	}

	if wi == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Work instruction not found",
		})
		return
	}

	twi := &repositories.TaskWorkInstruction{
		TenantID:             tenantUUID,
		TaskID:               taskID,
		WorkInstructionID:    req.WorkInstructionID,
		StepAcknowledgements: json.RawMessage("{}"),
		AllAcknowledged:      false,
	}

	if err := h.wiRepo.AttachToTask(c.Request.Context(), twi); err != nil {
		h.log.WithError(err).Error("Failed to attach work instruction to task")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to attach work instruction to task",
		})
		return
	}

	h.log.WithFields(logrus.Fields{
		"task_id":             taskID,
		"work_instruction_id": req.WorkInstructionID,
	}).Info("Work instruction attached to task")

	c.JSON(http.StatusCreated, twi)
}

// GetTaskWorkInstructions returns work instructions associated with a task.
// GET /v1/tasks/:id/work-instructions
func (h *WorkInstructionHandler) GetTaskWorkInstructions(c *gin.Context) {
	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid task ID format",
		})
		return
	}

	taskWIs, err := h.wiRepo.GetForTask(c.Request.Context(), taskID)
	if err != nil {
		h.log.WithError(err).Error("Failed to get task work instructions")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve task work instructions",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task_id": taskID,
		"data":    taskWIs,
	})
}

// AcknowledgeStep records that an operator acknowledged a specific step.
// POST /v1/tasks/:id/work-instructions/:wiId/acknowledge
func (h *WorkInstructionHandler) AcknowledgeStep(c *gin.Context) {
	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid task ID format",
		})
		return
	}

	wiID, err := uuid.Parse(c.Param("wiId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid work instruction ID format",
		})
		return
	}

	var req AcknowledgeStepRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	userIDStr, _ := middleware.GetUserID(c)
	userID, _ := uuid.Parse(userIDStr)

	if h.wiService != nil {
		if err := h.wiService.AcknowledgeStep(c.Request.Context(), taskID, wiID, req.StepNumber, userID); err != nil {
			if err.Error() == "task work instruction not found" {
				c.JSON(http.StatusNotFound, gin.H{
					"error":   "not_found",
					"message": "Task work instruction not found",
				})
				return
			}
			h.log.WithError(err).Error("Failed to acknowledge step")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to acknowledge step",
			})
			return
		}
	} else {
		// Fallback to direct repo call if service not configured
		if err := h.wiRepo.AcknowledgeStep(c.Request.Context(), taskID, wiID, req.StepNumber, userID); err != nil {
			if err.Error() == "task work instruction not found" {
				c.JSON(http.StatusNotFound, gin.H{
					"error":   "not_found",
					"message": "Task work instruction not found",
				})
				return
			}
			h.log.WithError(err).Error("Failed to acknowledge step")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to acknowledge step",
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "Step acknowledged successfully",
		"task_id":     taskID,
		"wi_id":       wiID,
		"step_number": req.StepNumber,
	})
}
