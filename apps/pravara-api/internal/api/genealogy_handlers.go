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

// GenealogyHandler handles product genealogy HTTP requests.
type GenealogyHandler struct {
	genealogyRepo    *repositories.GenealogyRepository
	genealogyService *services.GenealogyService
	publisher        *pubsub.Publisher
	log              *logrus.Logger
}

// NewGenealogyHandler creates a new genealogy handler.
func NewGenealogyHandler(genealogyRepo *repositories.GenealogyRepository, log *logrus.Logger) *GenealogyHandler {
	return &GenealogyHandler{
		genealogyRepo: genealogyRepo,
		log:           log,
	}
}

// SetPublisher sets the event publisher for real-time updates.
func (h *GenealogyHandler) SetPublisher(p *pubsub.Publisher) {
	h.publisher = p
}

// SetGenealogyService sets the genealogy service for business logic operations.
func (h *GenealogyHandler) SetGenealogyService(s *services.GenealogyService) {
	h.genealogyService = s
}

// CreateGenealogyRequest represents the request body for creating a genealogy record.
type CreateGenealogyRequest struct {
	ProductDefinitionID *uuid.UUID     `json:"product_definition_id"`
	OrderID             *uuid.UUID     `json:"order_id"`
	OrderItemID         *uuid.UUID     `json:"order_item_id"`
	TaskID              *uuid.UUID     `json:"task_id"`
	MachineID           *uuid.UUID     `json:"machine_id"`
	InspectionID        *uuid.UUID     `json:"inspection_id"`
	CertificateID       *uuid.UUID     `json:"certificate_id"`
	SerialNumber        *string        `json:"serial_number"`
	LotNumber           *string        `json:"lot_number"`
	Status              string         `json:"status"`
	Metadata            map[string]any `json:"metadata"`
}

// UpdateGenealogyRequest represents the request body for updating a genealogy record.
type UpdateGenealogyRequest struct {
	ProductDefinitionID *uuid.UUID     `json:"product_definition_id"`
	OrderID             *uuid.UUID     `json:"order_id"`
	OrderItemID         *uuid.UUID     `json:"order_item_id"`
	TaskID              *uuid.UUID     `json:"task_id"`
	MachineID           *uuid.UUID     `json:"machine_id"`
	InspectionID        *uuid.UUID     `json:"inspection_id"`
	CertificateID       *uuid.UUID     `json:"certificate_id"`
	SerialNumber        *string        `json:"serial_number"`
	LotNumber           *string        `json:"lot_number"`
	Status              string         `json:"status"`
	Metadata            map[string]any `json:"metadata"`
}

// SealGenealogyRequest represents the request body for sealing a genealogy record.
type SealGenealogyRequest struct {
	SealedBy uuid.UUID `json:"sealed_by" binding:"required"`
}

// ListGenealogy returns a paginated list of genealogy records.
// @Summary List genealogy records
// @Description Returns a paginated list of product genealogy records with optional filtering
// @Tags genealogy
// @Produce json
// @Param limit query int false "Number of results per page" default(20)
// @Param offset query int false "Offset for pagination" default(0)
// @Param product_definition_id query string false "Filter by product definition ID (UUID)"
// @Param order_id query string false "Filter by order ID (UUID)"
// @Param task_id query string false "Filter by task ID (UUID)"
// @Param machine_id query string false "Filter by machine ID (UUID)"
// @Param status query string false "Filter by status (draft, in_progress, completed, sealed)"
// @Param serial_number query string false "Filter by serial number"
// @Param lot_number query string false "Filter by lot number"
// @Success 200 {object} ListResponse "Paginated genealogy list"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /genealogy [get]
func (h *GenealogyHandler) ListGenealogy(c *gin.Context) {
	filter := repositories.GenealogyFilter{
		Limit:  20,
		Offset: 0,
	}

	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && limit > 0 {
		filter.Limit = limit
	}
	if offset, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && offset >= 0 {
		filter.Offset = offset
	}

	if productDefID := c.Query("product_definition_id"); productDefID != "" {
		if id, err := uuid.Parse(productDefID); err == nil {
			filter.ProductDefinitionID = &id
		}
	}

	if orderID := c.Query("order_id"); orderID != "" {
		if id, err := uuid.Parse(orderID); err == nil {
			filter.OrderID = &id
		}
	}

	if taskID := c.Query("task_id"); taskID != "" {
		if id, err := uuid.Parse(taskID); err == nil {
			filter.TaskID = &id
		}
	}

	if machineID := c.Query("machine_id"); machineID != "" {
		if id, err := uuid.Parse(machineID); err == nil {
			filter.MachineID = &id
		}
	}

	if status := c.Query("status"); status != "" {
		filter.Status = &status
	}

	if serialNumber := c.Query("serial_number"); serialNumber != "" {
		filter.SerialNumber = &serialNumber
	}

	if lotNumber := c.Query("lot_number"); lotNumber != "" {
		filter.LotNumber = &lotNumber
	}

	records, total, err := h.genealogyRepo.List(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list genealogy records")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve genealogy records",
		})
		return
	}

	c.JSON(http.StatusOK, ListResponse{
		Data:   records,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetGenealogyByID returns a single genealogy record by ID.
// @Summary Get genealogy record by ID
// @Description Returns a single product genealogy record with all details
// @Tags genealogy
// @Produce json
// @Param id path string true "Genealogy ID (UUID)"
// @Success 200 {object} repositories.ProductGenealogy "Genealogy record details"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Record not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /genealogy/{id} [get]
func (h *GenealogyHandler) GetGenealogyByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid genealogy ID format",
		})
		return
	}

	record, err := h.genealogyRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get genealogy record")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve genealogy record",
		})
		return
	}

	if record == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Genealogy record not found",
		})
		return
	}

	c.JSON(http.StatusOK, record)
}

// CreateGenealogy creates a new genealogy record.
// @Summary Create a genealogy record
// @Description Creates a new product genealogy record
// @Tags genealogy
// @Accept json
// @Produce json
// @Param body body CreateGenealogyRequest true "Genealogy record data"
// @Success 201 {object} repositories.ProductGenealogy "Created genealogy record"
// @Failure 400 {object} map[string]string "Validation error"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /genealogy [post]
func (h *GenealogyHandler) CreateGenealogy(c *gin.Context) {
	var req CreateGenealogyRequest
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

	record := &repositories.ProductGenealogy{
		TenantID:            tenantUUID,
		ProductDefinitionID: req.ProductDefinitionID,
		OrderID:             req.OrderID,
		OrderItemID:         req.OrderItemID,
		TaskID:              req.TaskID,
		MachineID:           req.MachineID,
		InspectionID:        req.InspectionID,
		CertificateID:       req.CertificateID,
		SerialNumber:        req.SerialNumber,
		LotNumber:           req.LotNumber,
		Status:              req.Status,
		Metadata:            req.Metadata,
	}

	if record.Status == "" {
		record.Status = string(repositories.GenealogyStatusDraft)
	}

	if err := h.genealogyRepo.Create(c.Request.Context(), record); err != nil {
		h.log.WithError(err).Error("Failed to create genealogy record")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create genealogy record",
		})
		return
	}

	h.log.WithField("genealogy_id", record.ID).Info("Genealogy record created")
	c.JSON(http.StatusCreated, record)
}

// UpdateGenealogy modifies an existing genealogy record.
// @Summary Update a genealogy record
// @Description Updates genealogy record fields
// @Tags genealogy
// @Accept json
// @Produce json
// @Param id path string true "Genealogy ID (UUID)"
// @Param body body UpdateGenealogyRequest true "Genealogy record update data"
// @Success 200 {object} repositories.ProductGenealogy "Updated genealogy record"
// @Failure 400 {object} map[string]string "Validation error"
// @Failure 404 {object} map[string]string "Record not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /genealogy/{id} [put]
func (h *GenealogyHandler) UpdateGenealogy(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid genealogy ID format",
		})
		return
	}

	record, err := h.genealogyRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get genealogy record")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve genealogy record",
		})
		return
	}

	if record == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Genealogy record not found",
		})
		return
	}

	// Prevent updates to sealed records
	if record.Status == string(repositories.GenealogyStatusSealed) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "sealed_record",
			"message": "Cannot update a sealed genealogy record",
		})
		return
	}

	var req UpdateGenealogyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Update fields
	if req.ProductDefinitionID != nil {
		record.ProductDefinitionID = req.ProductDefinitionID
	}
	if req.OrderID != nil {
		record.OrderID = req.OrderID
	}
	if req.OrderItemID != nil {
		record.OrderItemID = req.OrderItemID
	}
	if req.TaskID != nil {
		record.TaskID = req.TaskID
	}
	if req.MachineID != nil {
		record.MachineID = req.MachineID
	}
	if req.InspectionID != nil {
		record.InspectionID = req.InspectionID
	}
	if req.CertificateID != nil {
		record.CertificateID = req.CertificateID
	}
	if req.SerialNumber != nil {
		record.SerialNumber = req.SerialNumber
	}
	if req.LotNumber != nil {
		record.LotNumber = req.LotNumber
	}
	if req.Status != "" {
		record.Status = req.Status
	}
	if req.Metadata != nil {
		record.Metadata = req.Metadata
	}

	if err := h.genealogyRepo.Update(c.Request.Context(), record); err != nil {
		h.log.WithError(err).Error("Failed to update genealogy record")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to update genealogy record",
		})
		return
	}

	h.log.WithField("genealogy_id", record.ID).Info("Genealogy record updated")
	c.JSON(http.StatusOK, record)
}

// SealGenealogy seals a genealogy record, making it immutable.
// @Summary Seal a genealogy record
// @Description Computes a cryptographic hash and seals the genealogy record, making it immutable
// @Tags genealogy
// @Accept json
// @Produce json
// @Param id path string true "Genealogy ID (UUID)"
// @Param body body SealGenealogyRequest true "Seal request with sealed_by user ID"
// @Success 200 {object} map[string]string "Seal confirmation"
// @Failure 400 {object} map[string]string "Invalid request or already sealed"
// @Failure 404 {object} map[string]string "Record not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /genealogy/{id}/seal [post]
func (h *GenealogyHandler) SealGenealogy(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid genealogy ID format",
		})
		return
	}

	var req SealGenealogyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	if h.genealogyService == nil {
		h.log.Error("Genealogy service not configured")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Genealogy service not configured",
		})
		return
	}

	if err := h.genealogyService.SealRecord(c.Request.Context(), id, req.SealedBy); err != nil {
		if err.Error() == "genealogy record not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Genealogy record not found",
			})
			return
		}
		if err.Error() == "genealogy record is already sealed" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "already_sealed",
				"message": "Genealogy record is already sealed",
			})
			return
		}
		h.log.WithError(err).Error("Failed to seal genealogy record")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to seal genealogy record",
		})
		return
	}

	h.log.WithFields(logrus.Fields{
		"genealogy_id": id,
		"sealed_by":    req.SealedBy,
	}).Info("Genealogy record sealed")
	c.JSON(http.StatusOK, gin.H{
		"message": "Genealogy record sealed successfully",
	})
}

// GetGenealogyTree returns the full genealogy tree for a record.
// @Summary Get genealogy tree
// @Description Returns the complete genealogy tree including product definition, BOM, and material consumption
// @Tags genealogy
// @Produce json
// @Param id path string true "Genealogy ID (UUID)"
// @Success 200 {object} repositories.GenealogyTree "Genealogy tree"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Record not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /genealogy/{id}/tree [get]
func (h *GenealogyHandler) GetGenealogyTree(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid genealogy ID format",
		})
		return
	}

	tree, err := h.genealogyRepo.GetTree(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get genealogy tree")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve genealogy tree",
		})
		return
	}

	if tree == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Genealogy record not found",
		})
		return
	}

	c.JSON(http.StatusOK, tree)
}
