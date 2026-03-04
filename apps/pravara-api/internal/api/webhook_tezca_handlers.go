package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

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
// Tezca wraps event data inside a "data" subkey.
type TezcaWebhookPayload struct {
	Event     string                 `json:"event" binding:"required"`
	Timestamp string                 `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
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

		// Tezca sends "sha256=<hex>" — strip the prefix before comparing
		sig := strings.TrimPrefix(signature, "sha256=")

		mac := hmac.New(sha256.New, []byte(h.secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(sig)) {
			h.log.Warn("Invalid Tezca webhook signature")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
			return
		}

		c.Request.Body = &bodyReader{data: body}
	}

	var payload TezcaWebhookPayload
	if err := json.NewDecoder(c.Request.Body).Decode(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if payload.Event == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "event is required"})
		return
	}

	lawID, _ := payload.Data["law_id"].(string)
	domain, _ := payload.Data["domain"].(string)
	h.log.WithFields(logrus.Fields{
		"event":  payload.Event,
		"law_id": lawID,
		"domain": domain,
	}).Info("Tezca webhook received")

	// Extensibility: add handlers per event type
	// e.g. invalidate NOM caches, trigger compliance re-check

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
