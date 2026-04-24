package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// WebhookHandler handles external webhook requests.
type WebhookHandler struct {
	orderRepo     *repositories.OrderRepository
	orderItemRepo *repositories.OrderItemRepository
	log           *logrus.Logger
	cotizaSecret  string
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(orderRepo *repositories.OrderRepository, orderItemRepo *repositories.OrderItemRepository, log *logrus.Logger, cotizaSecret string) *WebhookHandler {
	return &WebhookHandler{
		orderRepo:     orderRepo,
		orderItemRepo: orderItemRepo,
		log:           log,
		cotizaSecret:  cotizaSecret,
	}
}

// CotizaWebhookPayload represents the incoming payload from Cotiza.
type CotizaWebhookPayload struct {
	Event     string          `json:"event" binding:"required"`
	Timestamp string          `json:"timestamp"`
	Order     CotizaOrderData `json:"order" binding:"required"`
}

// CotizaOrderData represents the order data from Cotiza.
type CotizaOrderData struct {
	ID            string           `json:"id" binding:"required"`
	CustomerName  string           `json:"customer_name" binding:"required"`
	CustomerEmail string           `json:"customer_email"`
	TotalAmount   float64          `json:"total_amount"`
	Currency      string           `json:"currency"`
	DueDate       *time.Time       `json:"due_date"`
	Priority      int              `json:"priority"`
	Items         []CotizaItemData `json:"items"`
	Metadata      map[string]any   `json:"metadata"`
}

// CotizaItemData represents an order item from Cotiza.
type CotizaItemData struct {
	ID             string         `json:"id"`
	ProductName    string         `json:"product_name" binding:"required"`
	ProductSKU     string         `json:"product_sku"`
	Quantity       int            `json:"quantity" binding:"required"`
	UnitPrice      float64        `json:"unit_price"`
	Specifications map[string]any `json:"specifications"`
	CADFileURL     string         `json:"cad_file_url"`
}

// CotizaWebhook handles incoming webhooks from Cotiza Studio.
// @Summary Receive Cotiza webhook
// @Description Processes order events from Cotiza Studio (created, updated, cancelled)
// @Tags webhooks
// @Accept json
// @Produce json
// @Param X-Cotiza-Signature header string false "HMAC-SHA256 signature for payload verification"
// @Param body body CotizaWebhookPayload true "Webhook payload"
// @Success 200 {object} map[string]string "Webhook processed successfully"
// @Failure 400 {object} map[string]string "Invalid payload"
// @Failure 401 {object} map[string]string "Invalid signature"
// @Failure 500 {object} map[string]string "Processing error"
// @Router /webhooks/cotiza [post]
func (h *WebhookHandler) CotizaWebhook(c *gin.Context) {
	// Verify webhook signature if secret is configured
	if h.cotizaSecret != "" {
		signature := c.GetHeader("X-Cotiza-Signature")
		if signature == "" {
			h.log.Warn("Missing webhook signature")
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Missing webhook signature",
			})
			return
		}

		// Read raw body for signature verification
		body, err := c.GetRawData()
		if err != nil {
			h.log.WithError(err).Error("Failed to read request body")
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "bad_request",
				"message": "Failed to read request body",
			})
			return
		}

		// Verify HMAC signature
		if !h.verifySignature(body, signature) {
			h.log.Warn("Invalid webhook signature")
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "unauthorized",
				"message": "Invalid webhook signature",
			})
			return
		}

		// Re-bind body for parsing
		c.Request.Body = &bodyReader{data: body}
	}

	var payload CotizaWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		h.log.WithError(err).Warn("Invalid webhook payload")
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	h.log.WithFields(logrus.Fields{
		"event":    payload.Event,
		"order_id": payload.Order.ID,
	}).Info("Received Cotiza webhook")

	// Handle different event types
	switch payload.Event {
	case "order.created", "order.confirmed":
		if err := h.handleCotizaOrderCreated(c, &payload); err != nil {
			h.log.WithError(err).Error("Failed to process order creation")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to process order",
			})
			return
		}
	case "order.updated":
		if err := h.handleCotizaOrderUpdated(c, &payload); err != nil {
			h.log.WithError(err).Error("Failed to process order update")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to process order update",
			})
			return
		}
	case "order.cancelled":
		if err := h.handleCotizaOrderCancelled(c, &payload); err != nil {
			h.log.WithError(err).Error("Failed to process order cancellation")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to process order cancellation",
			})
			return
		}
	default:
		h.log.WithField("event", payload.Event).Warn("Unknown webhook event type")
		c.JSON(http.StatusOK, gin.H{
			"message": "Event type not handled",
			"event":   payload.Event,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Webhook processed successfully",
		"order_id": payload.Order.ID,
	})
}

func (h *WebhookHandler) handleCotizaOrderCreated(c *gin.Context, payload *CotizaWebhookPayload) error {
	ctx := c.Request.Context()

	// Check if order already exists (idempotency)
	existing, err := h.orderRepo.GetByExternalID(ctx, payload.Order.ID)
	if err != nil {
		return err
	}
	if existing != nil {
		h.log.WithField("external_id", payload.Order.ID).Info("Order already exists, skipping creation")
		return nil
	}

	// Get tenant ID from context (set by webhook auth middleware)
	tenantIDStr, _ := c.Get("tenant_id")
	tenantID, _ := uuid.Parse(tenantIDStr.(string))

	// Create order
	order := &types.Order{
		TenantID:      tenantID,
		ExternalID:    payload.Order.ID,
		CustomerName:  payload.Order.CustomerName,
		CustomerEmail: payload.Order.CustomerEmail,
		Status:        types.OrderStatusReceived,
		Priority:      payload.Order.Priority,
		DueDate:       payload.Order.DueDate,
		TotalAmount:   payload.Order.TotalAmount,
		Currency:      payload.Order.Currency,
		Metadata:      payload.Order.Metadata,
	}

	if order.Priority == 0 {
		order.Priority = 5
	}
	if order.Currency == "" {
		order.Currency = "MXN"
	}

	if err := h.orderRepo.Create(ctx, order); err != nil {
		return err
	}

	h.log.WithFields(logrus.Fields{
		"order_id":    order.ID,
		"external_id": order.ExternalID,
	}).Info("Order created from Cotiza webhook")

	// Create order items
	for _, item := range payload.Order.Items {
		orderItem := &types.OrderItem{
			OrderID:        order.ID,
			ProductName:    item.ProductName,
			ProductSKU:     item.ProductSKU,
			Quantity:       item.Quantity,
			UnitPrice:      item.UnitPrice,
			Specifications: item.Specifications,
			CADFileURL:     item.CADFileURL,
		}

		if err := h.orderItemRepo.Create(ctx, orderItem); err != nil {
			h.log.WithError(err).WithField("product_name", item.ProductName).Warn("Failed to create order item")
			// Continue with other items
		}
	}

	return nil
}

func (h *WebhookHandler) handleCotizaOrderUpdated(c *gin.Context, payload *CotizaWebhookPayload) error {
	ctx := c.Request.Context()

	// Find existing order
	order, err := h.orderRepo.GetByExternalID(ctx, payload.Order.ID)
	if err != nil {
		return err
	}
	if order == nil {
		h.log.WithField("external_id", payload.Order.ID).Warn("Order not found for update")
		return nil
	}

	// Update order fields
	order.CustomerName = payload.Order.CustomerName
	order.CustomerEmail = payload.Order.CustomerEmail
	order.TotalAmount = payload.Order.TotalAmount
	order.Currency = payload.Order.Currency
	order.DueDate = payload.Order.DueDate
	if payload.Order.Priority > 0 {
		order.Priority = payload.Order.Priority
	}
	if payload.Order.Metadata != nil {
		order.Metadata = payload.Order.Metadata
	}

	return h.orderRepo.Update(ctx, order)
}

func (h *WebhookHandler) handleCotizaOrderCancelled(c *gin.Context, payload *CotizaWebhookPayload) error {
	ctx := c.Request.Context()

	// Find existing order
	order, err := h.orderRepo.GetByExternalID(ctx, payload.Order.ID)
	if err != nil {
		return err
	}
	if order == nil {
		h.log.WithField("external_id", payload.Order.ID).Warn("Order not found for cancellation")
		return nil
	}

	return h.orderRepo.UpdateStatus(ctx, order.ID, types.OrderStatusCancelled)
}

func (h *WebhookHandler) verifySignature(body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(h.cotizaSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}

// bodyReader implements io.ReadCloser for re-reading request body
type bodyReader struct {
	data []byte
	pos  int
}

func (b *bodyReader) Read(p []byte) (n int, err error) {
	if b.pos >= len(b.data) {
		return 0, nil
	}
	n = copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}

func (b *bodyReader) Close() error {
	return nil
}
