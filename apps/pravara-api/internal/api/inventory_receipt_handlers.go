package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

// ReceiptRequest records what physically arrived. Invoice evidence is
// mandatory (ruling I7): invoice_status "invoiced" with cfdi_uuid and
// cfdi_line, or "no_invoice" with neither. No cost, price or currency.
type ReceiptRequest struct {
	MaterialID     *uuid.UUID `json:"material_id" binding:"required"`
	Quantity       *float64   `json:"quantity" binding:"required"`
	Unit           string     `json:"unit" binding:"required"`
	ReceivedAt     *time.Time `json:"received_at"`
	BatchLotID     *uuid.UUID `json:"batch_lot_id"`
	LotNumber      string     `json:"lot_number"`
	InvoiceStatus  string     `json:"invoice_status" binding:"required"`
	CFDIUUID       *uuid.UUID `json:"cfdi_uuid"`
	CFDILine       *int       `json:"cfdi_line"`
	Notes          string     `json:"notes"`
	IdempotencyKey string     `json:"idempotency_key" binding:"required"`
}

// CreateReceipt is POST /v1/inventory/receipts.
// @Summary Record a receipt
// @Description Records a registered, active material that has a stock item. received_at defaults to now. The lot is batch_lot_id (linked to the material's stock item when it has none; 409 lot_belongs_to_other_item otherwise) or lot_number (found, or created for this item). When the unit converts to the stock item's unit (same unit, kg↔g, l↔ml, m↔mm, cm↔mm, or the material's conversion), a 'receipt' movement (reference_type inventory_receipt) raises stock and status is posted; otherwise status is needs_conversion, stock is untouched and a conversion gap is listed. idempotency_key (1-100 characters) is required: a retry with the same key and request returns the first receipt with 200 and replayed true; the same key with another request is 409 idempotency_key_reused. Needs a person with the admin or inventory_manager role (403 otherwise).
// @Tags inventory
// @Accept json
// @Produce json
// @Param body body ReceiptRequest true "Receipt"
// @Success 201 {object} map[string]interface{} "{receipt, stock_movement, replayed}"
// @Success 200 {object} map[string]interface{} "{receipt, stock_movement: null, replayed: true}"
// @Failure 400 {object} map[string]string "validation_error"
// @Failure 404 {object} map[string]string "material_not_found or batch_lot_not_found"
// @Failure 403 {object} map[string]string "forbidden (no inventory role) or human_only"
// @Failure 409 {object} map[string]string "idempotency_key_reused, material_inactive, material_not_stocked or lot_belongs_to_other_item"
// @Security BearerAuth
// @Router /inventory/receipts [post]
func (h *InventoryRecordsHandler) CreateReceipt(c *gin.Context) {
	var req ReceiptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "validation_error", err.Error())
		return
	}
	if len(req.LotNumber) > materialFieldMaxLen || len(req.IdempotencyKey) > materialFieldMaxLen {
		badRequest(c, "validation_error", "lot_number and idempotency_key are at most 100 characters")
		return
	}
	in := services.ReceiptInput{MaterialID: *req.MaterialID, Quantity: *req.Quantity, Unit: req.Unit,
		ReceivedBy: actor(c), BatchLotID: req.BatchLotID, LotNumber: req.LotNumber,
		InvoiceStatus: req.InvoiceStatus, CFDIUUID: req.CFDIUUID, CFDILine: req.CFDILine, Notes: req.Notes,
		IdempotencyKey: req.IdempotencyKey}
	if req.ReceivedAt != nil {
		in.ReceivedAt = *req.ReceivedAt
	}
	res, err := h.receiptSvc.Receive(c.Request.Context(), in)
	if err != nil {
		h.fail(c, err, "record receipt")
		return
	}
	status := http.StatusCreated
	if !res.Created {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"receipt": res.Receipt, "stock_movement": res.Movement, "replayed": !res.Created})
}

// ListReceipts is GET /v1/inventory/receipts.
// @Summary List receipts
// @Description Newest received first. from inclusive, to exclusive, on received_at.
// @Tags inventory
// @Produce json
// @Param material_id query string false "Material (UUID)"
// @Param invoice_status query string false "invoiced or no_invoice"
// @Param status query string false "posted or needs_conversion"
// @Param from query string false "received_at >= from (RFC3339)"
// @Param to query string false "received_at < to (RFC3339)"
// @Param limit query int false "Page size (max 1000)" default(100)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} ListResponse{data=[]repositories.Receipt}
// @Failure 400 {object} map[string]string "invalid_material_id, invalid_invoice_status, invalid_status, invalid_from, invalid_to, invalid_limit or invalid_offset"
// @Security BearerAuth
// @Router /inventory/receipts [get]
func (h *InventoryRecordsHandler) ListReceipts(c *gin.Context) {
	limit, offset, ok := page(c)
	if !ok {
		return
	}
	f := repositories.ReceiptFilter{InvoiceStatus: c.Query("invoice_status"), Status: c.Query("status"), Limit: limit, Offset: offset}
	if raw := c.Query("material_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			badRequest(c, "invalid_material_id", "material_id is not a UUID")
			return
		}
		f.MaterialID = &id
	}
	if f.InvoiceStatus != "" && f.InvoiceStatus != repositories.ReceiptInvoiced && f.InvoiceStatus != repositories.ReceiptNoInvoice {
		badRequest(c, "invalid_invoice_status", "invoice_status must be invoiced or no_invoice")
		return
	}
	if f.Status != "" && f.Status != repositories.ReceiptPosted && f.Status != repositories.ReceiptNeedsConversion {
		badRequest(c, "invalid_status", "status must be posted or needs_conversion")
		return
	}
	for _, p := range []struct {
		name string
		dst  **time.Time
	}{{"from", &f.From}, {"to", &f.To}} {
		if raw := c.Query(p.name); raw != "" {
			t, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				badRequest(c, "invalid_"+p.name, p.name+" must be an RFC3339 timestamp")
				return
			}
			*p.dst = &t
		}
	}
	rows, total, err := h.receipts.List(c.Request.Context(), f)
	if err != nil {
		h.fail(c, err, "list receipts")
		return
	}
	c.JSON(http.StatusOK, ListResponse{Data: rows, Total: total, Limit: limit, Offset: offset})
}

// GetReceipt is GET /v1/inventory/receipts/:id.
// @Summary Get a receipt
// @Tags inventory
// @Produce json
// @Param id path string true "Receipt ID (UUID)"
// @Success 200 {object} repositories.Receipt
// @Failure 404 {object} map[string]string "receipt_not_found"
// @Security BearerAuth
// @Router /inventory/receipts/{id} [get]
func (h *InventoryRecordsHandler) GetReceipt(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	rc, err := h.receipts.Get(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err, "get receipt")
		return
	}
	if rc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "receipt_not_found", "message": "Receipt not found"})
		return
	}
	c.JSON(http.StatusOK, rc)
}

// CountRequest opens a physical count.
type CountRequest struct {
	IsOpening bool   `json:"is_opening"`
	Notes     string `json:"notes"`
}

// CreateCount is POST /v1/inventory/counts.
// @Summary Open a physical count
// @Description is_opening marks the tenant's one opening count (ruling I7; 409 opening_count_exists for a second).
// @Tags inventory
// @Accept json
// @Produce json
// @Param body body CountRequest false "Count"
// @Success 201 {object} repositories.Count
// @Failure 409 {object} map[string]string "opening_count_exists"
// @Security BearerAuth
// @Router /inventory/counts [post]
func (h *InventoryRecordsHandler) CreateCount(c *gin.Context) {
	var req CountRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			badRequest(c, "validation_error", err.Error())
			return
		}
	}
	cnt, err := h.countSvc.Create(c.Request.Context(), req.IsOpening, req.Notes, actor(c))
	if err != nil {
		h.fail(c, err, "open count")
		return
	}
	c.JSON(http.StatusCreated, cnt)
}

// ListCounts is GET /v1/inventory/counts.
// @Summary List physical counts
// @Description Newest first, without lines (line_count says how many).
// @Tags inventory
// @Produce json
// @Param status query string false "open or closed"
// @Param limit query int false "Page size (max 1000)" default(100)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} ListResponse{data=[]repositories.Count}
// @Failure 400 {object} map[string]string "invalid_status, invalid_limit or invalid_offset"
// @Security BearerAuth
// @Router /inventory/counts [get]
func (h *InventoryRecordsHandler) ListCounts(c *gin.Context) {
	limit, offset, ok := page(c)
	if !ok {
		return
	}
	status := c.Query("status")
	if status != "" && status != repositories.CountOpen && status != repositories.CountClosed {
		badRequest(c, "invalid_status", "status must be open or closed")
		return
	}
	rows, total, err := h.countSvc.List(c.Request.Context(), status, limit, offset)
	if err != nil {
		h.fail(c, err, "list counts")
		return
	}
	c.JSON(http.StatusOK, ListResponse{Data: rows, Total: total, Limit: limit, Offset: offset})
}

// GetCount is GET /v1/inventory/counts/:id.
// @Summary Get a physical count with its lines
// @Tags inventory
// @Produce json
// @Param id path string true "Count ID (UUID)"
// @Success 200 {object} repositories.Count
// @Failure 404 {object} map[string]string "count_not_found"
// @Security BearerAuth
// @Router /inventory/counts/{id} [get]
func (h *InventoryRecordsHandler) GetCount(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	cnt, err := h.countSvc.Get(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err, "get count")
		return
	}
	c.JSON(http.StatusOK, cnt)
}

// CountLineRequest records one counted material.
type CountLineRequest struct {
	MaterialID      *uuid.UUID `json:"material_id" binding:"required"`
	CountedQuantity *float64   `json:"counted_quantity" binding:"required"`
	Unit            string     `json:"unit" binding:"required"`
	CountedAt       *time.Time `json:"counted_at"`
}

// RecordCountLine is POST /v1/inventory/counts/:id/lines.
// @Summary Record a counted material
// @Description One line per material: recording the same material again replaces its quantity, unit and counted_at while the count is open. counted_at (default now) is when the material was counted; closing compares with the stock on hand at that moment.
// @Tags inventory
// @Accept json
// @Produce json
// @Param id path string true "Count ID (UUID)"
// @Param body body CountLineRequest true "Counted material"
// @Success 201 {object} repositories.CountLine
// @Failure 400 {object} map[string]string "validation_error"
// @Failure 404 {object} map[string]string "count_not_found or material_not_found"
// @Failure 409 {object} map[string]string "count_closed"
// @Security BearerAuth
// @Router /inventory/counts/{id}/lines [post]
func (h *InventoryRecordsHandler) RecordCountLine(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req CountLineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "validation_error", err.Error())
		return
	}
	in := services.LineInput{MaterialID: *req.MaterialID, CountedQuantity: *req.CountedQuantity, Unit: req.Unit, CountedBy: actor(c)}
	if req.CountedAt != nil {
		in.CountedAt = *req.CountedAt
	}
	l, err := h.countSvc.RecordLine(c.Request.Context(), id, in)
	if err != nil {
		h.fail(c, err, "record count line")
		return
	}
	c.JSON(http.StatusCreated, l)
}

// CloseCount is POST /v1/inventory/counts/:id/close.
// @Summary Close a physical count
// @Description For each line: result matched (no difference), adjusted (an 'adjustment' movement of counted minus expected, reference_type inventory_count, reference_id the line), needs_conversion (no conversion to the stock item's unit: no movement, a conversion gap is listed and posted once the conversion exists) or not_stocked (the material has no stock item). Count differences are never folded into a job. Idempotent: closing a closed count returns it with already_closed true and moves nothing. An older count never closes over a newer one: when another closed count has a line for the same material counted later, 409 newer_count_exists lists the materials. Each applied line reports movements_since_counted and quantity_since_counted (movements between counted_at and the close). Needs the admin or inventory_manager role.
// @Tags inventory
// @Produce json
// @Param id path string true "Count ID (UUID)"
// @Success 200 {object} map[string]interface{} "{count, already_closed}"
// @Failure 404 {object} map[string]string "count_not_found"
// @Failure 409 {object} map[string]interface{} "newer_count_exists {error, message, materials}"
// @Security BearerAuth
// @Router /inventory/counts/{id}/close [post]
func (h *InventoryRecordsHandler) CloseCount(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	res, err := h.countSvc.Close(c.Request.Context(), id, actor(c))
	var newer *services.NewerCountError
	if errors.As(err, &newer) {
		c.JSON(http.StatusConflict, gin.H{"error": "newer_count_exists", "message": err.Error(), "materials": newer.Materials})
		return
	}
	if err != nil {
		h.fail(c, err, "close count")
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": res.Count, "already_closed": res.AlreadyClosed})
}
