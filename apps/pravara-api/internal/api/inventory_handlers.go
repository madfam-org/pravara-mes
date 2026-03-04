package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
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

// InventoryHandler handles inventory-related HTTP requests.
type InventoryHandler struct {
	inventoryRepo    *repositories.InventoryRepository
	inventoryService *services.InventoryService
	publisher        *pubsub.Publisher
	log              *logrus.Logger
	forgeSightSecret string
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
}

// AdjustInventoryRequest represents the request body for adjusting inventory quantity.
type AdjustInventoryRequest struct {
	Quantity        float64    `json:"quantity" binding:"required"`
	TransactionType string    `json:"transaction_type" binding:"required"`
	ReferenceType   *string    `json:"reference_type"`
	ReferenceID     *uuid.UUID `json:"reference_id"`
	Notes           *string    `json:"notes"`
}

// ForgeSightWebhookPayload represents the incoming ForgeSight webhook payload.
type ForgeSightWebhookPayload struct {
	Event string                     `json:"event"`
	Items []ForgeSightWebhookItem    `json:"items"`
}

// ForgeSightWebhookItem represents a single item in the ForgeSight webhook.
type ForgeSightWebhookItem struct {
	ForgeSightID string  `json:"forgesight_id"`
	SKU          string  `json:"sku"`
	Name         string  `json:"name"`
	Category     string  `json:"category"`
	Unit         string  `json:"unit"`
	Quantity     float64 `json:"quantity"`
	UnitCost     *float64 `json:"unit_cost"`
	Currency     string  `json:"currency"`
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

	if err := h.inventoryRepo.CreateItem(c.Request.Context(), item); err != nil {
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

	if err := h.inventoryRepo.UpdateItem(c.Request.Context(), item); err != nil {
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

// ForgeSightWebhook handles incoming webhook events from ForgeSight.
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
	tenantUUID, _ := uuid.Parse(tenantID)

	var processed int
	for _, fsItem := range payload.Items {
		item := &repositories.InventoryItem{
			TenantID:     tenantUUID,
			SKU:          fsItem.SKU,
			Name:         fsItem.Name,
			Category:     fsItem.Category,
			Unit:         fsItem.Unit,
			QuantityOnHand: fsItem.Quantity,
			ForgeSightID: &fsItem.ForgeSightID,
			UnitCost:     fsItem.UnitCost,
			Currency:     fsItem.Currency,
		}

		if item.Currency == "" {
			item.Currency = "MXN"
		}
		if item.Category == "" {
			item.Category = "material"
		}

		if err := h.inventoryRepo.UpsertByForgeSightID(c.Request.Context(), item); err != nil {
			h.log.WithError(err).WithField("forgesight_id", fsItem.ForgeSightID).Error("Failed to upsert inventory from ForgeSight")
			continue
		}
		processed++
	}

	h.log.WithFields(logrus.Fields{
		"event":     payload.Event,
		"received":  len(payload.Items),
		"processed": processed,
	}).Info("ForgeSight webhook processed")

	c.JSON(http.StatusOK, gin.H{
		"message":   "Webhook processed",
		"received":  len(payload.Items),
		"processed": processed,
	})
}
