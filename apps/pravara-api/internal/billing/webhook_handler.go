package billing

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// WebhookHandler handles incoming Dhanam webhook requests.
type WebhookHandler struct {
	repo   *InvoiceRepository
	secret string
	log    *logrus.Logger
}

// NewWebhookHandler creates a new Dhanam webhook handler.
func NewWebhookHandler(repo *InvoiceRepository, secret string, log *logrus.Logger) *WebhookHandler {
	return &WebhookHandler{
		repo:   repo,
		secret: secret,
		log:    log,
	}
}

// HandleWebhook processes incoming Dhanam webhook events.
func (h *WebhookHandler) HandleWebhook(c *gin.Context) {
	// Read request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.log.WithError(err).Error("Failed to read webhook body")
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	// Validate HMAC-SHA256 signature
	signature := c.GetHeader("X-Dhanam-Signature")
	if signature == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing signature"})
		return
	}

	if !ValidateSignature(body, signature, h.secret) {
		h.log.Warn("Invalid webhook signature received")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
		return
	}

	// Parse payload
	payload, err := ParseWebhookPayload(body)
	if err != nil {
		h.log.WithError(err).Error("Failed to parse webhook payload")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Convert to invoice
	invoice, err := payload.ToInvoice()
	if err != nil {
		h.log.WithError(err).Error("Failed to convert webhook to invoice")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Upsert invoice in database
	if err := h.repo.Upsert(c.Request.Context(), invoice); err != nil {
		h.log.WithError(err).Error("Failed to save invoice")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process webhook"})
		return
	}

	h.log.WithFields(logrus.Fields{
		"event":      payload.Event,
		"invoice_id": invoice.DhanamID,
		"tenant_id":  invoice.TenantID,
	}).Info("Dhanam webhook processed")

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
