package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

// InventoryRecordsHandler serves RFC 0058 wave 1 (docs/operations/inventory.md):
// the material code registry, unit conversions, conversion gaps, receipts
// and physical counts. Quantities only: no route here reads or writes cost,
// price or currency.
//
// Scope decision (route_scopes.go): the reads accept pravara-mes:read (the
// registry also pravara-cogs:read: the cost ledger references its codes).
// Every write moves stock or changes what is stock, so it is for people and
// wildcard API keys only.
type InventoryRecordsHandler struct {
	materials   *repositories.MaterialRepository
	stock       *repositories.StockRepository
	receipts    *repositories.ReceiptRepository
	consumption *repositories.JobConsumptionRepository
	materialSvc *services.MaterialService
	receiptSvc  *services.ReceiptService
	countSvc    *services.CountService
	log         *logrus.Logger
}

// NewInventoryRecordsHandler creates the handler over the tenant-scoped
// handle q.
func NewInventoryRecordsHandler(q repositories.DBTX, consumption *services.ConsumptionService, log *logrus.Logger) *InventoryRecordsHandler {
	receipts := services.NewReceiptService(q)
	counts := services.NewCountService(q)
	return &InventoryRecordsHandler{
		materials: repositories.NewMaterialRepository(q), stock: repositories.NewStockRepository(q),
		receipts: repositories.NewReceiptRepository(q), consumption: repositories.NewJobConsumptionRepository(q),
		materialSvc: services.NewMaterialService(q, consumption, receipts, counts),
		receiptSvc:  receipts, countSvc: counts, log: log,
	}
}

const (
	recordsDefaultLimit = 100
	recordsMaxLimit     = 1000
	unlinkedMaxRows     = 500
)

// inventoryErrors maps service errors to HTTP status and error code.
var inventoryErrors = []struct {
	err    error
	status int
	code   string
}{
	{services.ErrMaterialInvalid, http.StatusBadRequest, "validation_error"},
	{services.ErrAliasInvalid, http.StatusBadRequest, "validation_error"},
	{services.ErrConversionInvalid, http.StatusBadRequest, "validation_error"},
	{services.ErrReceiptInvalid, http.StatusBadRequest, "validation_error"},
	{services.ErrCountInvalid, http.StatusBadRequest, "validation_error"},
	{services.ErrMaterialNotFound, http.StatusNotFound, "material_not_found"},
	{services.ErrItemNotFound, http.StatusNotFound, "not_found"},
	{services.ErrLotNotFound, http.StatusNotFound, "batch_lot_not_found"},
	{services.ErrCountNotFound, http.StatusNotFound, "count_not_found"},
	{repositories.ErrMaterialCodeTaken, http.StatusConflict, "material_code_taken"},
	{repositories.ErrMaterialAliasTaken, http.StatusConflict, "alias_taken"},
	{repositories.ErrMaterialConversionDup, http.StatusConflict, "conversion_exists"},
	{repositories.ErrMaterialAlreadyStocked, http.StatusConflict, "material_already_stocked"},
	{repositories.ErrOpeningCountExists, http.StatusConflict, "opening_count_exists"},
	{services.ErrConversionBuiltin, http.StatusConflict, "conversion_builtin"},
	{services.ErrMaterialInactive, http.StatusConflict, "material_inactive"},
	{services.ErrMaterialNotStocked, http.StatusConflict, "material_not_stocked"},
	{services.ErrLotOtherItem, http.StatusConflict, "lot_belongs_to_other_item"},
	{services.ErrCountClosed, http.StatusConflict, "count_closed"},
}

// fail answers err: a known error with its status and code, anything else
// as 500 (logged).
func (h *InventoryRecordsHandler) fail(c *gin.Context, err error, what string) {
	for _, e := range inventoryErrors {
		if errors.Is(err, e.err) {
			c.JSON(e.status, gin.H{"error": e.code, "message": err.Error()})
			return
		}
	}
	h.log.WithError(err).Error("Failed to " + what)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to " + what})
}

// actor is the caller's id for *_by columns (no users(id) reference).
func actor(c *gin.Context) *uuid.UUID {
	if a := middleware.ActorUUID(c); a != uuid.Nil {
		return &a
	}
	return nil
}

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, "invalid_id", "id is not a UUID")
		return uuid.Nil, false
	}
	return id, true
}

// page reads limit (1-1000, default 100) and offset (>= 0).
func page(c *gin.Context) (int, int, bool) {
	limit, offset := recordsDefaultLimit, 0
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > recordsMaxLimit {
			badRequest(c, "invalid_limit", "limit must be 1-1000")
			return 0, 0, false
		}
		limit = n
	}
	if raw := c.Query("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			badRequest(c, "invalid_offset", "offset must be >= 0")
			return 0, 0, false
		}
		offset = n
	}
	return limit, offset, true
}

// MaterialRequest creates a registry entry.
type MaterialRequest struct {
	Code     string `json:"code" binding:"required"`
	Name     string `json:"name" binding:"required"`
	Category string `json:"category" binding:"required"`
	BaseUnit string `json:"base_unit" binding:"required"`
	Active   *bool  `json:"active"`
	Notes    string `json:"notes"`
}

// MaterialPatchRequest changes a registry entry; the code never changes.
type MaterialPatchRequest struct {
	Name     *string `json:"name"`
	Category *string `json:"category"`
	BaseUnit *string `json:"base_unit"`
	Active   *bool   `json:"active"`
	Notes    *string `json:"notes"`
}

// MaterialDetail is a registry entry with its aliases and conversions.
type MaterialDetail struct {
	repositories.Material
	Aliases     []repositories.MaterialAlias  `json:"aliases"`
	Conversions []repositories.UnitConversion `json:"conversions"`
}

// ListMaterials is GET /v1/inventory/materials.
// @Summary List the material code registry
// @Description Registry entries ordered by code, each with its stock item (inventory_item_id, stock_sku, stock_unit, quantity_on_hand) when it has one. Quantities only.
// @Tags inventory
// @Produce json
// @Param active query bool false "Only active (true) or inactive (false) entries"
// @Param category query string false "filament, resin, sheet, spare_part, consumable, packaging or other"
// @Param search query string false "Code or name contains (case-insensitive)"
// @Param limit query int false "Page size (max 1000)" default(100)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} ListResponse{data=[]repositories.Material}
// @Failure 400 {object} map[string]string "invalid_active, invalid_category, invalid_limit or invalid_offset"
// @Security BearerAuth
// @Router /inventory/materials [get]
func (h *InventoryRecordsHandler) ListMaterials(c *gin.Context) {
	limit, offset, ok := page(c)
	if !ok {
		return
	}
	f := repositories.MaterialFilter{Category: c.Query("category"), Search: c.Query("search"), Limit: limit, Offset: offset}
	if raw := c.Query("active"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			badRequest(c, "invalid_active", "active must be true or false")
			return
		}
		f.Active = &v
	}
	if f.Category != "" && !repositories.MaterialCategories[f.Category] {
		badRequest(c, "invalid_category", "category must be one of filament, resin, sheet, spare_part, consumable, packaging, other")
		return
	}
	rows, total, err := h.materials.List(c.Request.Context(), f)
	if err != nil {
		h.fail(c, err, "list materials")
		return
	}
	c.JSON(http.StatusOK, ListResponse{Data: rows, Total: total, Limit: limit, Offset: offset})
}

// CreateMaterial is POST /v1/inventory/materials.
// @Summary Register a material code
// @Description Codes are 1-64 letters, digits, '.', '_' or '-' (starting with a letter or digit), unique per tenant ignoring case, and never change. A code equal to an alias or a linked item's SKU is refused. Link a stock item with PATCH /v1/inventory/{id} (material_id).
// @Tags inventory
// @Accept json
// @Produce json
// @Param body body MaterialRequest true "Material"
// @Success 201 {object} repositories.Material
// @Failure 400 {object} map[string]string "validation_error"
// @Failure 409 {object} map[string]string "material_code_taken"
// @Security BearerAuth
// @Router /inventory/materials [post]
func (h *InventoryRecordsHandler) CreateMaterial(c *gin.Context) {
	var req MaterialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "validation_error", err.Error())
		return
	}
	m := &repositories.Material{Code: req.Code, Name: req.Name, Category: req.Category, BaseUnit: req.BaseUnit,
		Active: req.Active == nil || *req.Active, Notes: req.Notes, CreatedBy: actor(c)}
	if err := h.materialSvc.Create(c.Request.Context(), m); err != nil {
		h.fail(c, err, "register material")
		return
	}
	c.JSON(http.StatusCreated, m)
}

// GetMaterial is GET /v1/inventory/materials/:id.
// @Summary Get a registry entry with its aliases and conversions
// @Tags inventory
// @Produce json
// @Param id path string true "Material ID (UUID)"
// @Success 200 {object} MaterialDetail
// @Failure 404 {object} map[string]string "material_not_found"
// @Security BearerAuth
// @Router /inventory/materials/{id} [get]
func (h *InventoryRecordsHandler) GetMaterial(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	m, err := h.materials.Get(ctx, id)
	if err == nil && m == nil {
		err = services.ErrMaterialNotFound
	}
	if err != nil {
		h.fail(c, err, "get material")
		return
	}
	d := MaterialDetail{Material: *m}
	if d.Aliases, err = h.materials.Aliases(ctx, id); err == nil {
		d.Conversions, err = h.materials.Conversions(ctx, id)
	}
	if err != nil {
		h.fail(c, err, "get material")
		return
	}
	c.JSON(http.StatusOK, d)
}

// UpdateMaterial is PATCH /v1/inventory/materials/:id.
// @Summary Change a registry entry (name, category, base unit, active, notes)
// @Description The code never changes. An inactive material takes no new receipts; history keeps resolving to it.
// @Tags inventory
// @Accept json
// @Produce json
// @Param id path string true "Material ID (UUID)"
// @Param body body MaterialPatchRequest true "Fields to change"
// @Success 200 {object} repositories.Material
// @Failure 400 {object} map[string]string "validation_error"
// @Failure 404 {object} map[string]string "material_not_found"
// @Security BearerAuth
// @Router /inventory/materials/{id} [patch]
func (h *InventoryRecordsHandler) UpdateMaterial(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req MaterialPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "validation_error", err.Error())
		return
	}
	m, err := h.materialSvc.Update(c.Request.Context(), id, repositories.MaterialPatch{Name: req.Name,
		Category: req.Category, BaseUnit: req.BaseUnit, Active: req.Active, Notes: req.Notes})
	if err != nil {
		h.fail(c, err, "update material")
		return
	}
	c.JSON(http.StatusOK, m)
}

// AliasRequest adds an alias.
type AliasRequest struct {
	Alias string `json:"alias" binding:"required"`
}

// AddAlias is POST /v1/inventory/materials/:id/aliases.
// @Summary Name a material by another code
// @Description For a code consumption already uses (a print profile's material type, a lot's product code). Consumption recorded from then on resolves to the material; stock already relieved, or not, is not re-settled. The alias must not already name a material (code, alias or linked SKU).
// @Tags inventory
// @Accept json
// @Produce json
// @Param id path string true "Material ID (UUID)"
// @Param body body AliasRequest true "Alias (1-100 characters)"
// @Success 201 {object} repositories.MaterialAlias
// @Failure 400 {object} map[string]string "validation_error"
// @Failure 404 {object} map[string]string "material_not_found"
// @Failure 409 {object} map[string]string "alias_taken"
// @Security BearerAuth
// @Router /inventory/materials/{id}/aliases [post]
func (h *InventoryRecordsHandler) AddAlias(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req AliasRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "validation_error", err.Error())
		return
	}
	a, err := h.materialSvc.AddAlias(c.Request.Context(), id, req.Alias, actor(c))
	if err != nil {
		h.fail(c, err, "add alias")
		return
	}
	c.JSON(http.StatusCreated, a)
}

// ConversionRequest adds a unit conversion: 1 from_unit = factor to_unit.
type ConversionRequest struct {
	FromUnit string   `json:"from_unit" binding:"required"`
	ToUnit   string   `json:"to_unit" binding:"required"`
	Factor   *float64 `json:"factor" binding:"required"`
	Note     string   `json:"note"`
}

// AddConversion is POST /v1/inventory/materials/:id/conversions.
// @Summary Add a unit conversion for a material
// @Description 1 from_unit = factor to_unit (for example 1 spool = 1000 g). kg↔g, l↔ml, m↔mm and cm↔mm are built in (409 conversion_builtin). One factor per unit pair in either direction (409 conversion_exists); conversions never change. In the same transaction, every quantity of this material waiting for a conversion (job consumption relief, receipts, count lines) is posted when the new conversion makes it convertible: gaps_resolved and stock_movements say what moved.
// @Tags inventory
// @Accept json
// @Produce json
// @Param id path string true "Material ID (UUID)"
// @Param body body ConversionRequest true "Conversion"
// @Success 201 {object} map[string]interface{} "{conversion, gaps_resolved, stock_movements}"
// @Failure 400 {object} map[string]string "validation_error"
// @Failure 404 {object} map[string]string "material_not_found"
// @Failure 409 {object} map[string]string "conversion_exists or conversion_builtin"
// @Security BearerAuth
// @Router /inventory/materials/{id}/conversions [post]
func (h *InventoryRecordsHandler) AddConversion(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req ConversionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "validation_error", err.Error())
		return
	}
	res, err := h.materialSvc.AddConversion(c.Request.Context(), &repositories.UnitConversion{MaterialID: id,
		FromUnit: req.FromUnit, ToUnit: req.ToUnit, Factor: *req.Factor, Note: req.Note, CreatedBy: actor(c)})
	if err != nil {
		h.fail(c, err, "add conversion")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"conversion": res.Conversion, "gaps_resolved": res.GapsResolved,
		"stock_movements": res.Movements})
}

// Unlinked is GET /v1/inventory/materials/unlinked.
// @Summary What is not in the registry yet
// @Description Stock items with no registry entry (with the reason the migration left them: sku_is_not_a_valid_code, unit_is_not_a_valid_unit, sku_differs_from_another_only_in_case, code_registered_for_another_material, not_linked), batch lots with no stock item, and consumption material codes that resolve to no registry entry. Nothing here is guessed: a person registers, links or aliases each one. At most 500 of each.
// @Tags inventory
// @Produce json
// @Success 200 {object} map[string]interface{} "{items, lots, consumption_codes}"
// @Security BearerAuth
// @Router /inventory/materials/unlinked [get]
func (h *InventoryRecordsHandler) Unlinked(c *gin.Context) {
	ctx := c.Request.Context()
	items, err := h.materials.UnlinkedItems(ctx, unlinkedMaxRows)
	if err != nil {
		h.fail(c, err, "list unlinked items")
		return
	}
	lots, err := h.materials.UnlinkedLots(ctx, unlinkedMaxRows)
	if err != nil {
		h.fail(c, err, "list unlinked lots")
		return
	}
	cov, err := h.consumption.Coverage(ctx, nil, nil, unlinkedMaxRows)
	if err != nil {
		h.fail(c, err, "list unresolved consumption codes")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "lots": lots, "consumption_codes": cov.Unresolved})
}

// ListConversionGaps is GET /v1/inventory/conversion-gaps.
// @Summary Quantities waiting for a unit conversion
// @Description A job consumption, receipt or count line in a unit with no conversion to its stock item's unit is recorded but moves no stock ("needs conversion"). Adding the conversion (POST /v1/inventory/materials/{id}/conversions) posts it and marks the gap resolved; a newer fact of the same job marks an older gap superseded.
// @Tags inventory
// @Produce json
// @Param status query string false "open (default), resolved, superseded or all"
// @Param limit query int false "Page size (max 1000)" default(100)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} ListResponse{data=[]repositories.ConversionGap}
// @Failure 400 {object} map[string]string "invalid_status, invalid_limit or invalid_offset"
// @Security BearerAuth
// @Router /inventory/conversion-gaps [get]
func (h *InventoryRecordsHandler) ListConversionGaps(c *gin.Context) {
	limit, offset, ok := page(c)
	if !ok {
		return
	}
	status := c.DefaultQuery("status", repositories.GapOpen)
	switch status {
	case repositories.GapOpen, repositories.GapResolved, repositories.GapSuperseded, "all":
	default:
		badRequest(c, "invalid_status", "status must be open, resolved, superseded or all")
		return
	}
	rows, total, err := h.stock.ListGaps(c.Request.Context(), repositories.GapFilter{Status: status, Limit: limit, Offset: offset})
	if err != nil {
		h.fail(c, err, "list conversion gaps")
		return
	}
	c.JSON(http.StatusOK, ListResponse{Data: rows, Total: total, Limit: limit, Offset: offset})
}

// materialExists answers 404 material_not_found (and returns false) unless
// id is a registry entry of the tenant.
func (h *InventoryHandler) materialExists(c *gin.Context, id uuid.UUID) bool {
	if h.materials == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable", "message": "material registry not configured"})
		return false
	}
	m, err := h.materials.Get(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to read material")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to read material"})
		return false
	}
	if m == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "material_not_found", "message": "Material not found"})
		return false
	}
	return true
}
