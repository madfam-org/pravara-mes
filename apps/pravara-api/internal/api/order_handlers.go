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
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// OrderHandler handles order-related HTTP requests.
type OrderHandler struct {
	repo          *repositories.OrderRepository
	orderItemRepo *repositories.OrderItemRepository
	log           *logrus.Logger
	publisher     *pubsub.Publisher
	usageRecorder billing.UsageRecorder
}

// SetPublisher sets the event publisher for real-time updates.
func (h *OrderHandler) SetPublisher(p *pubsub.Publisher) {
	h.publisher = p
}

// SetUsageRecorder sets the usage recorder for billing tracking.
func (h *OrderHandler) SetUsageRecorder(r billing.UsageRecorder) {
	h.usageRecorder = r
}

// NewOrderHandler creates a new order handler.
func NewOrderHandler(repo *repositories.OrderRepository, orderItemRepo *repositories.OrderItemRepository, log *logrus.Logger) *OrderHandler {
	return &OrderHandler{
		repo:         repo,
		orderItemRepo: orderItemRepo,
		log:          log,
	}
}

// CreateOrderRequest represents the request body for creating an order.
type CreateOrderRequest struct {
	ExternalID    string         `json:"external_id"`
	CustomerName  string         `json:"customer_name" binding:"required"`
	CustomerEmail string         `json:"customer_email"`
	Priority      int            `json:"priority"`
	DueDate       *time.Time     `json:"due_date"`
	TotalAmount   float64        `json:"total_amount"`
	Currency      string         `json:"currency"`
	Metadata      map[string]any `json:"metadata"`
}

// UpdateOrderRequest represents the request body for updating an order.
type UpdateOrderRequest struct {
	CustomerName  string         `json:"customer_name"`
	CustomerEmail string         `json:"customer_email"`
	Status        string         `json:"status"`
	Priority      int            `json:"priority"`
	DueDate       *time.Time     `json:"due_date"`
	TotalAmount   float64        `json:"total_amount"`
	Currency      string         `json:"currency"`
	Metadata      map[string]any `json:"metadata"`
}

// ListResponse represents a paginated list response.
type ListResponse struct {
	Data   interface{} `json:"data"`
	Total  int         `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}

// List returns a paginated list of orders.
func (h *OrderHandler) List(c *gin.Context) {
	// Parse query parameters
	filter := repositories.OrderFilter{
		Limit:  20, // default
		Offset: 0,
	}

	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && limit > 0 {
		filter.Limit = limit
	}
	if offset, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && offset >= 0 {
		filter.Offset = offset
	}

	if status := c.Query("status"); status != "" {
		s := types.OrderStatus(status)
		filter.Status = &s
	}

	if priority, err := strconv.Atoi(c.Query("priority")); err == nil {
		filter.Priority = &priority
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

	orders, total, err := h.repo.List(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list orders")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve orders",
		})
		return
	}

	c.JSON(http.StatusOK, ListResponse{
		Data:   orders,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetByID returns a single order by ID.
func (h *OrderHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid order ID format",
		})
		return
	}

	order, err := h.repo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get order")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve order",
		})
		return
	}

	if order == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Order not found",
		})
		return
	}

	c.JSON(http.StatusOK, order)
}

// Create creates a new order.
func (h *OrderHandler) Create(c *gin.Context) {
	var req CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Get tenant ID from context
	tenantID, ok := middleware.GetTenantID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "Tenant context not found",
		})
		return
	}

	tenantUUID, _ := uuid.Parse(tenantID)

	order := &types.Order{
		TenantID:      tenantUUID,
		ExternalID:    req.ExternalID,
		CustomerName:  req.CustomerName,
		CustomerEmail: req.CustomerEmail,
		Status:        types.OrderStatusReceived,
		Priority:      req.Priority,
		DueDate:       req.DueDate,
		TotalAmount:   req.TotalAmount,
		Currency:      req.Currency,
		Metadata:      req.Metadata,
	}

	if order.Priority == 0 {
		order.Priority = 5 // default priority
	}
	if order.Currency == "" {
		order.Currency = "MXN"
	}

	if err := h.repo.Create(c.Request.Context(), order); err != nil {
		h.log.WithError(err).Error("Failed to create order")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create order",
		})
		return
	}

	// Record order creation for billing
	if h.usageRecorder != nil {
		go func() {
			event := billing.UsageEvent{
				TenantID:  tenantID,
				EventType: billing.UsageEventOrder,
				Quantity:  1,
				Metadata: map[string]string{
					"order_id": order.ID.String(),
				},
				Timestamp: time.Now(),
			}
			if err := h.usageRecorder.RecordEvent(c.Request.Context(), event); err != nil {
				h.log.WithError(err).Warn("Failed to record order creation usage")
			}
		}()
	}

	h.log.WithField("order_id", order.ID).Info("Order created")
	c.JSON(http.StatusCreated, order)
}

// Update modifies an existing order.
func (h *OrderHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid order ID format",
		})
		return
	}

	// Get existing order
	order, err := h.repo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get order")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve order",
		})
		return
	}

	if order == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Order not found",
		})
		return
	}

	var req UpdateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Update fields
	if req.CustomerName != "" {
		order.CustomerName = req.CustomerName
	}
	if req.CustomerEmail != "" {
		order.CustomerEmail = req.CustomerEmail
	}
	if req.Status != "" {
		order.Status = types.OrderStatus(req.Status)
	}
	if req.Priority > 0 {
		order.Priority = req.Priority
	}
	if req.DueDate != nil {
		order.DueDate = req.DueDate
	}
	if req.TotalAmount > 0 {
		order.TotalAmount = req.TotalAmount
	}
	if req.Currency != "" {
		order.Currency = req.Currency
	}
	if req.Metadata != nil {
		order.Metadata = req.Metadata
	}

	if err := h.repo.Update(c.Request.Context(), order); err != nil {
		h.log.WithError(err).Error("Failed to update order")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to update order",
		})
		return
	}

	h.log.WithField("order_id", order.ID).Info("Order updated")
	c.JSON(http.StatusOK, order)
}

// Delete cancels an order.
func (h *OrderHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid order ID format",
		})
		return
	}

	if err := h.repo.Delete(c.Request.Context(), id); err != nil {
		if err.Error() == "order not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Order not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to delete order")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to delete order",
		})
		return
	}

	h.log.WithField("order_id", id).Info("Order cancelled")
	c.JSON(http.StatusOK, gin.H{
		"message": "Order cancelled successfully",
	})
}

// CreateOrderItemRequest represents the request body for creating an order item.
type CreateOrderItemRequest struct {
	ProductName    string         `json:"product_name" binding:"required"`
	ProductSKU     string         `json:"product_sku"`
	Quantity       int            `json:"quantity" binding:"required,min=1"`
	UnitPrice      float64        `json:"unit_price"`
	Specifications map[string]any `json:"specifications"`
	CADFileURL     string         `json:"cad_file_url"`
}

// ListItems returns all items for an order.
func (h *OrderHandler) ListItems(c *gin.Context) {
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid order ID format",
		})
		return
	}

	// Verify order exists
	order, err := h.repo.GetByID(c.Request.Context(), orderID)
	if err != nil {
		h.log.WithError(err).Error("Failed to get order")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve order",
		})
		return
	}
	if order == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Order not found",
		})
		return
	}

	items, err := h.orderItemRepo.List(c.Request.Context(), orderID)
	if err != nil {
		h.log.WithError(err).Error("Failed to list order items")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve order items",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"order_id": orderID,
		"items":    items,
		"count":    len(items),
	})
}

// AddItem adds an item to an order.
func (h *OrderHandler) AddItem(c *gin.Context) {
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid order ID format",
		})
		return
	}

	// Verify order exists
	order, err := h.repo.GetByID(c.Request.Context(), orderID)
	if err != nil {
		h.log.WithError(err).Error("Failed to get order")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve order",
		})
		return
	}
	if order == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Order not found",
		})
		return
	}

	var req CreateOrderItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	item := &types.OrderItem{
		OrderID:        orderID,
		ProductName:    req.ProductName,
		ProductSKU:     req.ProductSKU,
		Quantity:       req.Quantity,
		UnitPrice:      req.UnitPrice,
		Specifications: req.Specifications,
		CADFileURL:     req.CADFileURL,
	}

	if err := h.orderItemRepo.Create(c.Request.Context(), item); err != nil {
		h.log.WithError(err).Error("Failed to create order item")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create order item",
		})
		return
	}

	h.log.WithFields(logrus.Fields{
		"order_id": orderID,
		"item_id":  item.ID,
	}).Info("Order item created")
	c.JSON(http.StatusCreated, item)
}
