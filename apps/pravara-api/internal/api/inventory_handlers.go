package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/observability"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

// InventoryHandler handles inventory-related HTTP requests.
type InventoryHandler struct {
	inventoryRepo    *repositories.InventoryRepository
	inventoryService *services.InventoryService
	publisher        *pubsub.Publisher
	log              *logrus.Logger
	forgeSightSecret string
	materials        *repositories.MaterialRepository
}

// SetMaterialRepository lets item writes link a material registry entry
// (material_id).
func (h *InventoryHandler) SetMaterialRepository(m *repositories.MaterialRepository) {
	h.materials = m
}

// NewInventoryHandler creates a new inventory handler.
func NewInventoryHandler(inventoryRepo *repositories.InventoryRepository, log *logrus.Logger) *InventoryHandler {
	return &InventoryHandler{
		inventoryRepo: inventoryRepo,
		log:           log,
	}
}

// SetPublisher sets the event publisher for real-time updates.
func (h *InventoryHandler) SetPublisher(p *pubsub.Publisher) {
	h.publisher = p
}

// SetInventoryService sets the inventory service for business logic.
func (h *InventoryHandler) SetInventoryService(s *services.InventoryService) {
	h.inventoryService = s
}

// =============== Request Types ===============

// CreateInventoryItemRequest represents the request body for creating an inventory item.
type CreateInventoryItemRequest struct {
	SKU             string         `json:"sku" binding:"required"`
	Name            string         `json:"name" binding:"required"`
	Category        string         `json:"category"`
	Description     string         `json:"description"`
	Unit            string         `json:"unit" binding:"required"`
	QuantityOnHand  float64        `json:"quantity_on_hand"`
	ReorderPoint    float64        `json:"reorder_point"`
	ReorderQuantity float64        `json:"reorder_quantity"`
	ForgeSightID    *string        `json:"forgesight_id"`
	UnitCost        *float64       `json:"unit_cost"`
	Currency        string         `json:"currency"`
	Metadata        map[string]any `json:"metadata"`
	// MaterialID links the item to a material registry entry (one item
	// per material; docs/operations/inventory.md).
	MaterialID *uuid.UUID `json:"material_id"`
}

// UpdateInventoryItemRequest represents the request body for updating an inventory item.
type UpdateInventoryItemRequest struct {
	SKU             string         `json:"sku"`
	Name            string         `json:"name"`
	Category        string         `json:"category"`
	Description     string         `json:"description"`
	Unit            string         `json:"unit"`
	ReorderPoint    *float64       `json:"reorder_point"`
	ReorderQuantity *float64       `json:"reorder_quantity"`
	ForgeSightID    *string        `json:"forgesight_id"`
	UnitCost        *float64       `json:"unit_cost"`
	Currency        string         `json:"currency"`
	Metadata        map[string]any `json:"metadata"`
	// MaterialID links the item to a material registry entry; "" unlinks
	// it; absent leaves it unchanged.
	MaterialID *string `json:"material_id"`
}

// AdjustInventoryRequest represents the request body for adjusting inventory quantity.
type AdjustInventoryRequest struct {
	Quantity        float64    `json:"quantity" binding:"required"`
	TransactionType string     `json:"transaction_type" binding:"required"`
	ReferenceType   *string    `json:"reference_type"`
	ReferenceID     *uuid.UUID `json:"reference_id"`
	Notes           *string    `json:"notes"`
}

// ForgeSightWebhookPayload represents the incoming ForgeSight webhook payload.
type ForgeSightWebhookPayload struct {
	Event string                  `json:"event"`
	Items []ForgeSightWebhookItem `json:"items"`
}

// ForgeSightWebhookItem represents a single item in the ForgeSight webhook.
// Quantity and UnitCost are market reference values: they are logged and
// never written to inventory (see ForgeSightWebhook).
type ForgeSightWebhookItem struct {
	ForgeSightID string   `json:"forgesight_id"`
	SKU          string   `json:"sku"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Unit         string   `json:"unit"`
	Quantity     float64  `json:"quantity"`
	UnitCost     *float64 `json:"unit_cost"`
	Currency     string   `json:"currency"`
}

// =============== Endpoints ===============

// ListItems returns a paginated list of inventory items.
func (h *InventoryHandler) ListItems(c *gin.Context) {
	filter := repositories.InventoryFilter{
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

	if search := c.Query("search"); search != "" {
		filter.Search = &search
	}

	items, total, err := h.inventoryRepo.ListItems(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list inventory items")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve inventory items",
		})
		return
	}

	c.JSON(http.StatusOK, ListResponse{
		Data:   items,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetItemByID returns a single inventory item by ID.
func (h *InventoryHandler) GetItemByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid inventory item ID format",
		})
		return
	}

	item, err := h.inventoryRepo.GetItemByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get inventory item")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve inventory item",
		})
		return
	}

	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Inventory item not found",
		})
		return
	}

	c.JSON(http.StatusOK, item)
}

// CreateItem creates a new inventory item.
func (h *InventoryHandler) CreateItem(c *gin.Context) {
	var req CreateInventoryItemRequest
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

	currency := req.Currency
	if currency == "" {
		currency = "MXN"
	}

	category := req.Category
	if category == "" {
		category = "material"
	}

	item := &repositories.InventoryItem{
		TenantID:        tenantUUID,
		SKU:             req.SKU,
		Name:            req.Name,
		Category:        category,
		Description:     req.Description,
		Unit:            req.Unit,
		QuantityOnHand:  req.QuantityOnHand,
		ReorderPoint:    req.ReorderPoint,
		ReorderQuantity: req.ReorderQuantity,
		ForgeSightID:    req.ForgeSightID,
		UnitCost:        req.UnitCost,
		Currency:        currency,
		Metadata:        req.Metadata,
	}
	if req.MaterialID != nil {
		if !h.materialExists(c, *req.MaterialID) {
			return
		}
		item.MaterialID = req.MaterialID
	}

	if err := h.inventoryRepo.CreateItem(c.Request.Context(), item); errors.Is(err, repositories.ErrMaterialAlreadyStocked) {
		c.JSON(http.StatusConflict, gin.H{"error": "material_already_stocked", "message": err.Error()})
		return
	} else if err != nil {
		h.log.WithError(err).Error("Failed to create inventory item")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create inventory item",
		})
		return
	}

	h.log.WithField("item_id", item.ID).Info("Inventory item created")
	c.JSON(http.StatusCreated, item)
}

// UpdateItem modifies an existing inventory item.
func (h *InventoryHandler) UpdateItem(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid inventory item ID format",
		})
		return
	}

	item, err := h.inventoryRepo.GetItemByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get inventory item")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve inventory item",
		})
		return
	}

	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Inventory item not found",
		})
		return
	}

	var req UpdateInventoryItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	if req.SKU != "" {
		item.SKU = req.SKU
	}
	if req.Name != "" {
		item.Name = req.Name
	}
	if req.Category != "" {
		item.Category = req.Category
	}
	if req.Description != "" {
		item.Description = req.Description
	}
	if req.Unit != "" {
		item.Unit = req.Unit
	}
	if req.ReorderPoint != nil {
		item.ReorderPoint = *req.ReorderPoint
	}
	if req.ReorderQuantity != nil {
		item.ReorderQuantity = *req.ReorderQuantity
	}
	if req.ForgeSightID != nil {
		item.ForgeSightID = req.ForgeSightID
	}
	if req.UnitCost != nil {
		item.UnitCost = req.UnitCost
	}
	if req.Currency != "" {
		item.Currency = req.Currency
	}
	if req.Metadata != nil {
		item.Metadata = req.Metadata
	}
	if req.MaterialID != nil {
		item.MaterialID = nil
		if *req.MaterialID != "" {
			mid, err := uuid.Parse(*req.MaterialID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "validation_error", "message": "material_id is not a UUID"})
				return
			}
			if !h.materialExists(c, mid) {
				return
			}
			item.MaterialID = &mid
		}
	}

	if err := h.inventoryRepo.UpdateItem(c.Request.Context(), item); errors.Is(err, repositories.ErrMaterialAlreadyStocked) {
		c.JSON(http.StatusConflict, gin.H{"error": "material_already_stocked", "message": err.Error()})
		return
	} else if err != nil {
		h.log.WithError(err).Error("Failed to update inventory item")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to update inventory item",
		})
		return
	}

	h.log.WithField("item_id", item.ID).Info("Inventory item updated")
	c.JSON(http.StatusOK, item)
}

// AdjustItem adjusts inventory quantity and creates a transaction record.
func (h *InventoryHandler) AdjustItem(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid inventory item ID format",
		})
		return
	}

	var req AdjustInventoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Validate transaction type
	validTypes := map[string]bool{
		"receipt": true, "consumption": true, "adjustment": true,
		"reservation": true, "release": true,
	}
	if !validTypes[req.TransactionType] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "Invalid transaction_type. Must be one of: receipt, consumption, adjustment, reservation, release",
		})
		return
	}

	userID, _ := middleware.GetUserID(c)
	var userUUID *uuid.UUID
	if uid, err := uuid.Parse(userID); err == nil {
		userUUID = &uid
	}

	if h.inventoryService != nil {
		tenantID, _ := middleware.GetTenantID(c)
		tenantUUID, _ := uuid.Parse(tenantID)

		if err := h.inventoryService.AdjustQuantity(c.Request.Context(), tenantUUID, id, req.Quantity, req.TransactionType, req.ReferenceType, req.ReferenceID, userUUID, req.Notes); err != nil {
			if err.Error() == "inventory item not found" {
				c.JSON(http.StatusNotFound, gin.H{
					"error":   "not_found",
					"message": "Inventory item not found",
				})
				return
			}
			h.log.WithError(err).Error("Failed to adjust inventory")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to adjust inventory",
			})
			return
		}
	} else {
		if err := h.inventoryRepo.AdjustQuantity(c.Request.Context(), id, req.Quantity, req.TransactionType, req.ReferenceType, req.ReferenceID, userUUID, req.Notes); err != nil {
			if err.Error() == "inventory item not found" {
				c.JSON(http.StatusNotFound, gin.H{
					"error":   "not_found",
					"message": "Inventory item not found",
				})
				return
			}
			h.log.WithError(err).Error("Failed to adjust inventory")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to adjust inventory",
			})
			return
		}
	}

	h.log.WithFields(logrus.Fields{
		"item_id":  id,
		"quantity": req.Quantity,
		"type":     req.TransactionType,
	}).Info("Inventory adjusted")

	c.JSON(http.StatusOK, gin.H{
		"message": "Inventory adjusted successfully",
	})
}

// GetLowStock returns all inventory items below their reorder point.
func (h *InventoryHandler) GetLowStock(c *gin.Context) {
	items, err := h.inventoryRepo.GetLowStock(c.Request.Context())
	if err != nil {
		h.log.WithError(err).Error("Failed to get low stock items")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve low stock items",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  items,
		"total": len(items),
	})
}

// ForgeSightWebhook accepts ForgeSight events as reference data only.
//
// ForgeSight is market intelligence: its prices are benchmarks, not this
// tenant's cost, and its quantities are not this shop's stock. Valuing
// consumption belongs to the cost ledger (Dhanam), and stock moves only
// through inventory transactions (people, or job consumption). So the
// webhook still authenticates, verifies the signature and parses the payload,
// then records each item as a reference (one log line and a counter) and
// never writes inventory: unit_cost, quantity_on_hand and every other
// inventory column stay as they are, and no item is created.
//
// The response keeps its shape ({message, received, processed}) so senders
// keep working; processed counts the items accepted as reference.
func (h *InventoryHandler) ForgeSightWebhook(c *gin.Context) {
	// Read body for HMAC verification
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "bad_request",
			"message": "Failed to read request body",
		})
		return
	}

	// Verify HMAC signature if secret is configured
	if h.forgeSightSecret != "" {
		signature := c.GetHeader("X-ForgeSight-Signature")
		if signature == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Missing webhook signature",
			})
			return
		}

		mac := hmac.New(sha256.New, []byte(h.forgeSightSecret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(signature), []byte(expected)) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Invalid webhook signature",
			})
			return
		}
	}

	var payload ForgeSightWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "bad_request",
			"message": "Invalid JSON payload",
		})
		return
	}

	tenantID, _ := middleware.GetTenantID(c)

	var accepted int
	for _, fsItem := range payload.Items {
		if fsItem.ForgeSightID == "" {
			observability.ForgeSightReferenceItemsTotal.WithLabelValues(forgeSightItemIgnored).Inc()
			continue
		}
		accepted++
		observability.ForgeSightReferenceItemsTotal.WithLabelValues(forgeSightItemReference).Inc()

		fields := logrus.Fields{
			"tenant_id":          tenantID,
			"event":              payload.Event,
			"forgesight_id":      fsItem.ForgeSightID,
			"sku":                fsItem.SKU,
			"benchmark_quantity": fsItem.Quantity,
			"benchmark_unit":     fsItem.Unit,
			"benchmark_currency": fsItem.Currency,
		}
		if fsItem.UnitCost != nil {
			fields["benchmark_unit_cost"] = *fsItem.UnitCost
		}
		if accepted <= forgeSightItemLogLimit {
			h.log.WithFields(fields).Info("ForgeSight reference item (inventory unchanged)")
		}
	}

	h.log.WithFields(logrus.Fields{
		"tenant_id":            tenantID,
		"event":                payload.Event,
		"received":             len(payload.Items),
		"processed":            accepted,
		"ignored":              len(payload.Items) - accepted,
		"item_lines_truncated": accepted > forgeSightItemLogLimit,
	}).Info("ForgeSight webhook received as reference only; inventory cost and stock unchanged")

	c.JSON(http.StatusOK, gin.H{
		"message":        "Webhook received; ForgeSight data is reference only and does not change inventory cost or stock",
		"received":       len(payload.Items),
		"processed":      accepted,
		"reference_only": true,
	})
}

// forgeSightItemLogLimit caps the per-item reference log lines of one call;
// the counter still counts every item.
const forgeSightItemLogLimit = 100

// Outcome labels of observability.ForgeSightReferenceItemsTotal.
const (
	forgeSightItemReference = "reference"
	forgeSightItemIgnored   = "ignored_missing_id"
)
