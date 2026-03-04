package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// TezcaWebhookHandler handles Tezca law-change webhook events.
type TezcaWebhookHandler struct {
	log    *logrus.Logger
	secret string
}

// NewTezcaWebhookHandler creates a new Tezca webhook handler.
func NewTezcaWebhookHandler(log *logrus.Logger, secret string) *TezcaWebhookHandler {
	return &TezcaWebhookHandler{log: log, secret: secret}
}

// TezcaWebhookPayload represents a Tezca webhook event.
type TezcaWebhookPayload struct {
	Event  string `json:"event" binding:"required"`
	LawID  string `json:"law_id"`
	Domain string `json:"domain"`
}

// HandleWebhook processes POST /webhooks/tezca.
// Events: law.updated, version.created, law.created
func (h *TezcaWebhookHandler) HandleWebhook(c *gin.Context) {
	if h.secret != "" {
		signature := c.GetHeader("X-Tezca-Signature")
		if signature == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing signature"})
			return
		}

		body, err := c.GetRawData()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
			return
		}

		mac := hmac.New(sha256.New, []byte(h.secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(signature)) {
			h.log.Warn("Invalid Tezca webhook signature")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}

		c.Request.Body = &bodyReader{data: body}
	}

	var payload TezcaWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.log.WithFields(logrus.Fields{
		"event":  payload.Event,
		"law_id": payload.LawID,
		"domain": payload.Domain,
	}).Info("Tezca webhook received")

	// Extensibility: add handlers per event type
	// e.g. invalidate NOM caches, trigger compliance re-check

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
