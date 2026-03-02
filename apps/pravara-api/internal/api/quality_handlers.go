package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/billing"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// QualityHandler handles quality management HTTP requests.
type QualityHandler struct {
	certRepo       *repositories.QualityCertificateRepository
	inspectionRepo *repositories.InspectionRepository
	batchLotRepo   *repositories.BatchLotRepository
	log            *logrus.Logger
	usageRecorder  billing.UsageRecorder
}

// NewQualityHandler creates a new quality handler.
func NewQualityHandler(
	certRepo *repositories.QualityCertificateRepository,
	inspectionRepo *repositories.InspectionRepository,
	batchLotRepo *repositories.BatchLotRepository,
	log *logrus.Logger,
) *QualityHandler {
	return &QualityHandler{
		certRepo:       certRepo,
		inspectionRepo: inspectionRepo,
		batchLotRepo:   batchLotRepo,
		log:            log,
	}
}

// SetUsageRecorder sets the usage recorder for billing tracking.
func (h *QualityHandler) SetUsageRecorder(r billing.UsageRecorder) {
	h.usageRecorder = r
}

// =============== Quality Certificates ===============

// CreateCertificateRequest represents the request body for creating a certificate.
type CreateCertificateRequest struct {
	CertificateNumber string                    `json:"certificate_number" binding:"required"`
	Type              types.QualityCertType     `json:"type" binding:"required"`
	Status            types.QualityCertStatus   `json:"status"`
	OrderID           *uuid.UUID                `json:"order_id"`
	TaskID            *uuid.UUID                `json:"task_id"`
	MachineID         *uuid.UUID                `json:"machine_id"`
	BatchLotID        *uuid.UUID                `json:"batch_lot_id"`
	Title             string                    `json:"title" binding:"required"`
	Description       string                    `json:"description"`
	IssuedDate        *time.Time                `json:"issued_date"`
	ExpiryDate        *time.Time                `json:"expiry_date"`
	IssuedBy          *uuid.UUID                `json:"issued_by"`
	DocumentURL       string                    `json:"document_url"`
	Metadata          map[string]any            `json:"metadata"`
}

// UpdateCertificateRequest represents the request body for updating a certificate.
type UpdateCertificateRequest struct {
	Status      types.QualityCertStatus `json:"status"`
	Title       string                  `json:"title"`
	Description string                  `json:"description"`
	IssuedDate  *time.Time              `json:"issued_date"`
	ExpiryDate  *time.Time              `json:"expiry_date"`
	IssuedBy    *uuid.UUID              `json:"issued_by"`
	ApprovedBy  *uuid.UUID              `json:"approved_by"`
	ApprovedAt  *time.Time              `json:"approved_at"`
	DocumentURL string                  `json:"document_url"`
	Metadata    map[string]any          `json:"metadata"`
}

// ListCertificates returns a paginated list of quality certificates.
func (h *QualityHandler) ListCertificates(c *gin.Context) {
	filter := repositories.QualityCertificateFilter{
		Limit:  20,
		Offset: 0,
	}

	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && limit > 0 {
		filter.Limit = limit
	}
	if offset, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && offset >= 0 {
		filter.Offset = offset
	}

	if certType := c.Query("type"); certType != "" {
		t := types.QualityCertType(certType)
		filter.Type = &t
	}

	if status := c.Query("status"); status != "" {
		s := types.QualityCertStatus(status)
		filter.Status = &s
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

	if batchLotID := c.Query("batch_lot_id"); batchLotID != "" {
		if id, err := uuid.Parse(batchLotID); err == nil {
			filter.BatchLotID = &id
		}
	}

	if fromDate := c.Query("from_date"); fromDate != "" {
		if t, err := time.Parse(time.RFC3339, fromDate); err == nil {
			filter.FromDate = &t
		}
	}

	if toDate := c.Query("to_date"); toDate != "" {
		if t, err := time.Parse(time.RFC3339, toDate); err == nil {
			filter.ToDate = &t
		}
	}

	certificates, total, err := h.certRepo.List(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list quality certificates")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve quality certificates",
		})
		return
	}

	c.JSON(http.StatusOK, ListResponse{
		Data:   certificates,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetCertificateByID returns a single quality certificate by ID.
func (h *QualityHandler) GetCertificateByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid certificate ID format",
		})
		return
	}

	certificate, err := h.certRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get quality certificate")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve quality certificate",
		})
		return
	}

	if certificate == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Quality certificate not found",
		})
		return
	}

	c.JSON(http.StatusOK, certificate)
}

// CreateCertificate creates a new quality certificate.
func (h *QualityHandler) CreateCertificate(c *gin.Context) {
	var req CreateCertificateRequest
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

	certificate := &types.QualityCertificate{
		TenantID:          tenantUUID,
		CertificateNumber: req.CertificateNumber,
		Type:              req.Type,
		Status:            req.Status,
		OrderID:           req.OrderID,
		TaskID:            req.TaskID,
		MachineID:         req.MachineID,
		BatchLotID:        req.BatchLotID,
		Title:             req.Title,
		Description:       req.Description,
		IssuedDate:        req.IssuedDate,
		ExpiryDate:        req.ExpiryDate,
		IssuedBy:          req.IssuedBy,
		DocumentURL:       req.DocumentURL,
		Metadata:          req.Metadata,
	}

	if certificate.Status == "" {
		certificate.Status = types.QualityCertStatusDraft
	}

	if err := h.certRepo.Create(c.Request.Context(), certificate); err != nil {
		h.log.WithError(err).Error("Failed to create quality certificate")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create quality certificate",
		})
		return
	}

	h.log.WithField("certificate_id", certificate.ID).Info("Quality certificate created")
	c.JSON(http.StatusCreated, certificate)
}

// UpdateCertificate modifies an existing quality certificate.
func (h *QualityHandler) UpdateCertificate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid certificate ID format",
		})
		return
	}

	certificate, err := h.certRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get quality certificate")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve quality certificate",
		})
		return
	}

	if certificate == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Quality certificate not found",
		})
		return
	}

	var req UpdateCertificateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Track if certificate was approved in this update
	wasApproved := certificate.Status != types.QualityCertStatusApproved &&
		req.Status == types.QualityCertStatusApproved

	// Update fields
	if req.Status != "" {
		certificate.Status = req.Status
	}
	if req.Title != "" {
		certificate.Title = req.Title
	}
	if req.Description != "" {
		certificate.Description = req.Description
	}
	if req.IssuedDate != nil {
		certificate.IssuedDate = req.IssuedDate
	}
	if req.ExpiryDate != nil {
		certificate.ExpiryDate = req.ExpiryDate
	}
	if req.IssuedBy != nil {
		certificate.IssuedBy = req.IssuedBy
	}
	if req.ApprovedBy != nil {
		certificate.ApprovedBy = req.ApprovedBy
	}
	if req.ApprovedAt != nil {
		certificate.ApprovedAt = req.ApprovedAt
	}
	if req.DocumentURL != "" {
		certificate.DocumentURL = req.DocumentURL
	}
	if req.Metadata != nil {
		certificate.Metadata = req.Metadata
	}

	if err := h.certRepo.Update(c.Request.Context(), certificate); err != nil {
		h.log.WithError(err).Error("Failed to update quality certificate")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to update quality certificate",
		})
		return
	}

	// Record certificate issuance for billing if it was just approved
	if wasApproved && h.usageRecorder != nil {
		tenantID, _ := middleware.GetTenantID(c)
		go func() {
			event := billing.UsageEvent{
				TenantID:  tenantID,
				EventType: billing.UsageEventCertificate,
				Quantity:  1,
				Metadata: map[string]string{
					"certificate_id":     certificate.ID.String(),
					"certificate_number": certificate.CertificateNumber,
					"type":               string(certificate.Type),
				},
				Timestamp: time.Now(),
			}
			if err := h.usageRecorder.RecordEvent(c.Request.Context(), event); err != nil {
				h.log.WithError(err).Warn("Failed to record certificate issuance usage")
			}
		}()
	}

	h.log.WithField("certificate_id", certificate.ID).Info("Quality certificate updated")
	c.JSON(http.StatusOK, certificate)
}

// DeleteCertificate removes a quality certificate.
func (h *QualityHandler) DeleteCertificate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid certificate ID format",
		})
		return
	}

	if err := h.certRepo.Delete(c.Request.Context(), id); err != nil {
		if err.Error() == "quality certificate not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Quality certificate not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to delete quality certificate")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to delete quality certificate",
		})
		return
	}

	h.log.WithField("certificate_id", id).Info("Quality certificate deleted")
	c.JSON(http.StatusOK, gin.H{
		"message": "Quality certificate deleted successfully",
	})
}

// =============== Inspections ===============

// CreateInspectionRequest represents the request body for creating an inspection.
type CreateInspectionRequest struct {
	InspectionNumber string                   `json:"inspection_number" binding:"required"`
	OrderID          *uuid.UUID               `json:"order_id"`
	TaskID           *uuid.UUID               `json:"task_id"`
	MachineID        *uuid.UUID               `json:"machine_id"`
	Type             string                   `json:"type" binding:"required"`
	ScheduledAt      *time.Time               `json:"scheduled_at"`
	InspectorID      *uuid.UUID               `json:"inspector_id"`
	Notes            string                   `json:"notes"`
	Checklist        []any                    `json:"checklist"`
	Metadata         map[string]any           `json:"metadata"`
}

// UpdateInspectionRequest represents the request body for updating an inspection.
type UpdateInspectionRequest struct {
	ScheduledAt   *time.Time               `json:"scheduled_at"`
	CompletedAt   *time.Time               `json:"completed_at"`
	InspectorID   *uuid.UUID               `json:"inspector_id"`
	Result        types.InspectionResult   `json:"result"`
	Notes         string                   `json:"notes"`
	Checklist     []any                    `json:"checklist"`
	CertificateID *uuid.UUID               `json:"certificate_id"`
	Metadata      map[string]any           `json:"metadata"`
}

// CompleteInspectionRequest represents the request body for completing an inspection.
type CompleteInspectionRequest struct {
	Result        types.InspectionResult `json:"result" binding:"required"`
	Notes         string                 `json:"notes"`
	Checklist     []any                  `json:"checklist"`
	CertificateID *uuid.UUID             `json:"certificate_id"`
}

// ListInspections returns a paginated list of inspections.
func (h *QualityHandler) ListInspections(c *gin.Context) {
	filter := repositories.InspectionFilter{
		Limit:  20,
		Offset: 0,
	}

	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && limit > 0 {
		filter.Limit = limit
	}
	if offset, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && offset >= 0 {
		filter.Offset = offset
	}

	if inspectionType := c.Query("type"); inspectionType != "" {
		filter.Type = &inspectionType
	}

	if result := c.Query("result"); result != "" {
		r := types.InspectionResult(result)
		filter.Result = &r
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

	if fromDate := c.Query("from_date"); fromDate != "" {
		if t, err := time.Parse(time.RFC3339, fromDate); err == nil {
			filter.FromDate = &t
		}
	}

	if toDate := c.Query("to_date"); toDate != "" {
		if t, err := time.Parse(time.RFC3339, toDate); err == nil {
			filter.ToDate = &t
		}
	}

	inspections, total, err := h.inspectionRepo.List(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list inspections")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve inspections",
		})
		return
	}

	c.JSON(http.StatusOK, ListResponse{
		Data:   inspections,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetInspectionByID returns a single inspection by ID.
func (h *QualityHandler) GetInspectionByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid inspection ID format",
		})
		return
	}

	inspection, err := h.inspectionRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get inspection")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve inspection",
		})
		return
	}

	if inspection == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Inspection not found",
		})
		return
	}

	c.JSON(http.StatusOK, inspection)
}

// CreateInspection creates a new inspection.
func (h *QualityHandler) CreateInspection(c *gin.Context) {
	var req CreateInspectionRequest
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

	inspection := &types.Inspection{
		TenantID:         tenantUUID,
		InspectionNumber: req.InspectionNumber,
		OrderID:          req.OrderID,
		TaskID:           req.TaskID,
		MachineID:        req.MachineID,
		Type:             req.Type,
		ScheduledAt:      req.ScheduledAt,
		InspectorID:      req.InspectorID,
		Result:           types.InspectionResultPending,
		Notes:            req.Notes,
		Checklist:        req.Checklist,
		Metadata:         req.Metadata,
	}

	if err := h.inspectionRepo.Create(c.Request.Context(), inspection); err != nil {
		h.log.WithError(err).Error("Failed to create inspection")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create inspection",
		})
		return
	}

	h.log.WithField("inspection_id", inspection.ID).Info("Inspection created")
	c.JSON(http.StatusCreated, inspection)
}

// UpdateInspection modifies an existing inspection.
func (h *QualityHandler) UpdateInspection(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid inspection ID format",
		})
		return
	}

	inspection, err := h.inspectionRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get inspection")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve inspection",
		})
		return
	}

	if inspection == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Inspection not found",
		})
		return
	}

	var req UpdateInspectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Update fields
	if req.ScheduledAt != nil {
		inspection.ScheduledAt = req.ScheduledAt
	}
	if req.CompletedAt != nil {
		inspection.CompletedAt = req.CompletedAt
	}
	if req.InspectorID != nil {
		inspection.InspectorID = req.InspectorID
	}
	if req.Result != "" {
		inspection.Result = req.Result
	}
	if req.Notes != "" {
		inspection.Notes = req.Notes
	}
	if req.Checklist != nil {
		inspection.Checklist = req.Checklist
	}
	if req.CertificateID != nil {
		inspection.CertificateID = req.CertificateID
	}
	if req.Metadata != nil {
		inspection.Metadata = req.Metadata
	}

	if err := h.inspectionRepo.Update(c.Request.Context(), inspection); err != nil {
		h.log.WithError(err).Error("Failed to update inspection")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to update inspection",
		})
		return
	}

	h.log.WithField("inspection_id", inspection.ID).Info("Inspection updated")
	c.JSON(http.StatusOK, inspection)
}

// DeleteInspection removes an inspection.
func (h *QualityHandler) DeleteInspection(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid inspection ID format",
		})
		return
	}

	if err := h.inspectionRepo.Delete(c.Request.Context(), id); err != nil {
		if err.Error() == "inspection not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Inspection not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to delete inspection")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to delete inspection",
		})
		return
	}

	h.log.WithField("inspection_id", id).Info("Inspection deleted")
	c.JSON(http.StatusOK, gin.H{
		"message": "Inspection deleted successfully",
	})
}

// CompleteInspection marks an inspection as complete with a result.
func (h *QualityHandler) CompleteInspection(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid inspection ID format",
		})
		return
	}

	inspection, err := h.inspectionRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get inspection")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve inspection",
		})
		return
	}

	if inspection == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Inspection not found",
		})
		return
	}

	var req CompleteInspectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Mark as complete
	now := time.Now()
	inspection.CompletedAt = &now
	inspection.Result = req.Result
	if req.Notes != "" {
		inspection.Notes = req.Notes
	}
	if req.Checklist != nil {
		inspection.Checklist = req.Checklist
	}
	if req.CertificateID != nil {
		inspection.CertificateID = req.CertificateID
	}

	if err := h.inspectionRepo.Update(c.Request.Context(), inspection); err != nil {
		h.log.WithError(err).Error("Failed to complete inspection")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to complete inspection",
		})
		return
	}

	h.log.WithFields(logrus.Fields{
		"inspection_id": inspection.ID,
		"result":        inspection.Result,
	}).Info("Inspection completed")
	c.JSON(http.StatusOK, inspection)
}

// =============== Batch Lots ===============

// CreateBatchLotRequest represents the request body for creating a batch lot.
type CreateBatchLotRequest struct {
	LotNumber         string         `json:"lot_number" binding:"required"`
	ProductName       string         `json:"product_name" binding:"required"`
	ProductCode       string         `json:"product_code"`
	Quantity          float64        `json:"quantity" binding:"required"`
	Unit              string         `json:"unit" binding:"required"`
	ManufacturedDate  *time.Time     `json:"manufactured_date"`
	ExpiryDate        *time.Time     `json:"expiry_date"`
	ReceivedDate      *time.Time     `json:"received_date"`
	SupplierName      string         `json:"supplier_name"`
	SupplierLotNumber string         `json:"supplier_lot_number"`
	PurchaseOrder     string         `json:"purchase_order"`
	Status            string         `json:"status"`
	OrderID           *uuid.UUID     `json:"order_id"`
	Metadata          map[string]any `json:"metadata"`
}

// UpdateBatchLotRequest represents the request body for updating a batch lot.
type UpdateBatchLotRequest struct {
	ProductName       string         `json:"product_name"`
	ProductCode       string         `json:"product_code"`
	Quantity          float64        `json:"quantity"`
	Unit              string         `json:"unit"`
	ManufacturedDate  *time.Time     `json:"manufactured_date"`
	ExpiryDate        *time.Time     `json:"expiry_date"`
	ReceivedDate      *time.Time     `json:"received_date"`
	SupplierName      string         `json:"supplier_name"`
	SupplierLotNumber string         `json:"supplier_lot_number"`
	PurchaseOrder     string         `json:"purchase_order"`
	Status            string         `json:"status"`
	Metadata          map[string]any `json:"metadata"`
}

// ListBatchLots returns a paginated list of batch lots.
func (h *QualityHandler) ListBatchLots(c *gin.Context) {
	filter := repositories.BatchLotFilter{
		Limit:  20,
		Offset: 0,
	}

	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && limit > 0 {
		filter.Limit = limit
	}
	if offset, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && offset >= 0 {
		filter.Offset = offset
	}

	if status := c.Query("status"); status != "" {
		filter.Status = &status
	}

	if orderID := c.Query("order_id"); orderID != "" {
		if id, err := uuid.Parse(orderID); err == nil {
			filter.OrderID = &id
		}
	}

	if productCode := c.Query("product_code"); productCode != "" {
		filter.ProductCode = &productCode
	}

	if fromDate := c.Query("from_date"); fromDate != "" {
		if t, err := time.Parse(time.RFC3339, fromDate); err == nil {
			filter.FromDate = &t
		}
	}

	if toDate := c.Query("to_date"); toDate != "" {
		if t, err := time.Parse(time.RFC3339, toDate); err == nil {
			filter.ToDate = &t
		}
	}

	batchLots, total, err := h.batchLotRepo.List(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list batch lots")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve batch lots",
		})
		return
	}

	c.JSON(http.StatusOK, ListResponse{
		Data:   batchLots,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetBatchLotByID returns a single batch lot by ID.
func (h *QualityHandler) GetBatchLotByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid batch lot ID format",
		})
		return
	}

	batchLot, err := h.batchLotRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get batch lot")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve batch lot",
		})
		return
	}

	if batchLot == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Batch lot not found",
		})
		return
	}

	c.JSON(http.StatusOK, batchLot)
}

// CreateBatchLot creates a new batch lot.
func (h *QualityHandler) CreateBatchLot(c *gin.Context) {
	var req CreateBatchLotRequest
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

	batchLot := &types.BatchLot{
		TenantID:          tenantUUID,
		LotNumber:         req.LotNumber,
		ProductName:       req.ProductName,
		ProductCode:       req.ProductCode,
		Quantity:          req.Quantity,
		Unit:              req.Unit,
		ManufacturedDate:  req.ManufacturedDate,
		ExpiryDate:        req.ExpiryDate,
		ReceivedDate:      req.ReceivedDate,
		SupplierName:      req.SupplierName,
		SupplierLotNumber: req.SupplierLotNumber,
		PurchaseOrder:     req.PurchaseOrder,
		Status:            req.Status,
		OrderID:           req.OrderID,
		Metadata:          req.Metadata,
	}

	if batchLot.Status == "" {
		batchLot.Status = "active"
	}

	if err := h.batchLotRepo.Create(c.Request.Context(), batchLot); err != nil {
		h.log.WithError(err).Error("Failed to create batch lot")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create batch lot",
		})
		return
	}

	h.log.WithField("batch_lot_id", batchLot.ID).Info("Batch lot created")
	c.JSON(http.StatusCreated, batchLot)
}

// UpdateBatchLot modifies an existing batch lot.
func (h *QualityHandler) UpdateBatchLot(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid batch lot ID format",
		})
		return
	}

	batchLot, err := h.batchLotRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get batch lot")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve batch lot",
		})
		return
	}

	if batchLot == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Batch lot not found",
		})
		return
	}

	var req UpdateBatchLotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Update fields
	if req.ProductName != "" {
		batchLot.ProductName = req.ProductName
	}
	if req.ProductCode != "" {
		batchLot.ProductCode = req.ProductCode
	}
	if req.Quantity > 0 {
		batchLot.Quantity = req.Quantity
	}
	if req.Unit != "" {
		batchLot.Unit = req.Unit
	}
	if req.ManufacturedDate != nil {
		batchLot.ManufacturedDate = req.ManufacturedDate
	}
	if req.ExpiryDate != nil {
		batchLot.ExpiryDate = req.ExpiryDate
	}
	if req.ReceivedDate != nil {
		batchLot.ReceivedDate = req.ReceivedDate
	}
	if req.SupplierName != "" {
		batchLot.SupplierName = req.SupplierName
	}
	if req.SupplierLotNumber != "" {
		batchLot.SupplierLotNumber = req.SupplierLotNumber
	}
	if req.PurchaseOrder != "" {
		batchLot.PurchaseOrder = req.PurchaseOrder
	}
	if req.Status != "" {
		batchLot.Status = req.Status
	}
	if req.Metadata != nil {
		batchLot.Metadata = req.Metadata
	}

	if err := h.batchLotRepo.Update(c.Request.Context(), batchLot); err != nil {
		h.log.WithError(err).Error("Failed to update batch lot")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to update batch lot",
		})
		return
	}

	h.log.WithField("batch_lot_id", batchLot.ID).Info("Batch lot updated")
	c.JSON(http.StatusOK, batchLot)
}

// DeleteBatchLot removes a batch lot.
func (h *QualityHandler) DeleteBatchLot(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid batch lot ID format",
		})
		return
	}

	if err := h.batchLotRepo.Delete(c.Request.Context(), id); err != nil {
		if err.Error() == "batch lot not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Batch lot not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to delete batch lot")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to delete batch lot",
		})
		return
	}

	h.log.WithField("batch_lot_id", id).Info("Batch lot deleted")
	c.JSON(http.StatusOK, gin.H{
		"message": "Batch lot deleted successfully",
	})
}
