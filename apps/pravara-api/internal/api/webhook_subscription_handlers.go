package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

// WebhookSubscriptionHandler handles webhook subscription CRUD endpoints.
type WebhookSubscriptionHandler struct {
	repo *repositories.WebhookRepository
	log  *logrus.Logger
}

// NewWebhookSubscriptionHandler creates a new webhook subscription handler.
func NewWebhookSubscriptionHandler(repo *repositories.WebhookRepository, log *logrus.Logger) *WebhookSubscriptionHandler {
	return &WebhookSubscriptionHandler{repo: repo, log: log}
}

type createWebhookSubscriptionRequest struct {
	Name       string   `json:"name" binding:"required"`
	URL        string   `json:"url" binding:"required,url"`
	Secret     string   `json:"secret" binding:"required"`
	EventTypes []string `json:"event_types" binding:"required,min=1"`
}

type updateWebhookSubscriptionRequest struct {
	Name       *string  `json:"name,omitempty"`
	URL        *string  `json:"url,omitempty"`
	Secret     *string  `json:"secret,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`
	IsActive   *bool    `json:"is_active,omitempty"`
}

// Create creates a new webhook subscription.
func (h *WebhookSubscriptionHandler) Create(c *gin.Context) {
	var req createWebhookSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
		return
	}

	tenantID, ok := middleware.GetTenantID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	tid, err := uuid.Parse(tenantID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_tenant_id"})
		return
	}

	sub := &repositories.WebhookSubscription{
		TenantID:   tid,
		Name:       req.Name,
		URL:        req.URL,
		Secret:     req.Secret,
		EventTypes: req.EventTypes,
		IsActive:   true,
	}

	if err := h.repo.CreateSubscription(c.Request.Context(), sub); err != nil {
		h.log.WithError(err).Error("Failed to create webhook subscription")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to create subscription"})
		return
	}

	c.JSON(http.StatusCreated, sub)
}

// List returns all webhook subscriptions for the current tenant.
func (h *WebhookSubscriptionHandler) List(c *gin.Context) {
	subs, err := h.repo.ListSubscriptions(c.Request.Context())
	if err != nil {
		h.log.WithError(err).Error("Failed to list webhook subscriptions")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to list subscriptions"})
		return
	}

	// Redact secrets in list response
	for i := range subs {
		subs[i].Secret = "***"
	}

	c.JSON(http.StatusOK, gin.H{"subscriptions": subs})
}

// GetByID returns a single webhook subscription.
func (h *WebhookSubscriptionHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}

	sub, err := h.repo.GetSubscriptionByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get webhook subscription")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}
	if sub == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "Subscription not found"})
		return
	}

	sub.Secret = "***"
	c.JSON(http.StatusOK, sub)
}

// Update patches a webhook subscription.
func (h *WebhookSubscriptionHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}

	var req updateWebhookSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
		return
	}

	sub, err := h.repo.GetSubscriptionByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get webhook subscription for update")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}
	if sub == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "Subscription not found"})
		return
	}

	if req.Name != nil {
		sub.Name = *req.Name
	}
	if req.URL != nil {
		sub.URL = *req.URL
	}
	if req.Secret != nil {
		sub.Secret = *req.Secret
	}
	if req.EventTypes != nil {
		sub.EventTypes = req.EventTypes
	}
	if req.IsActive != nil {
		sub.IsActive = *req.IsActive
	}

	if err := h.repo.UpdateSubscription(c.Request.Context(), sub); err != nil {
		h.log.WithError(err).Error("Failed to update webhook subscription")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to update subscription"})
		return
	}

	sub.Secret = "***"
	c.JSON(http.StatusOK, sub)
}

// Delete removes a webhook subscription.
func (h *WebhookSubscriptionHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}

	if err := h.repo.DeleteSubscription(c.Request.Context(), id); err != nil {
		if err == repositories.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "Subscription not found"})
			return
		}
		h.log.WithError(err).Error("Failed to delete webhook subscription")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to delete subscription"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Subscription deleted"})
}

// ListDeliveries returns delivery history for a subscription.
func (h *WebhookSubscriptionHandler) ListDeliveries(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}

	limit := queryInt(c, "limit", 50)
	offset := queryInt(c, "offset", 0)

	deliveries, total, err := h.repo.ListDeliveriesBySubscription(c.Request.Context(), id, limit, offset)
	if err != nil {
		h.log.WithError(err).Error("Failed to list webhook deliveries")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to list deliveries"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deliveries": deliveries,
		"total":      total,
		"limit":      limit,
		"offset":     offset,
	})
}
