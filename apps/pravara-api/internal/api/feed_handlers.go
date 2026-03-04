package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// FeedHandler handles CRM and social media feed endpoints.
type FeedHandler struct {
	feedRepo   *repositories.FeedRepository
	outboxRepo *repositories.OutboxRepository
	log        *logrus.Logger
}

// NewFeedHandler creates a new feed handler.
func NewFeedHandler(feedRepo *repositories.FeedRepository, outboxRepo *repositories.OutboxRepository, log *logrus.Logger) *FeedHandler {
	return &FeedHandler{feedRepo: feedRepo, outboxRepo: outboxRepo, log: log}
}

// --- CRM Feed Endpoints ---

// CRMOrders returns active orders with task progress.
func (h *FeedHandler) CRMOrders(c *gin.Context) {
	limit := queryInt(c, "limit", 50)
	offset := queryInt(c, "offset", 0)

	orders, total, err := h.feedRepo.GetCRMOrders(c.Request.Context(), limit, offset)
	if err != nil {
		h.log.WithError(err).Error("Failed to get CRM orders")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to get orders"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"orders": orders,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// CRMOrderTimeline returns chronological events for an order.
func (h *FeedHandler) CRMOrderTimeline(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}

	limit := queryInt(c, "limit", 100)
	offset := queryInt(c, "offset", 0)

	events, total, err := h.outboxRepo.GetEventsByEntityFromPayload(c.Request.Context(), id, limit, offset)
	if err != nil {
		h.log.WithError(err).Error("Failed to get order timeline")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to get timeline"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// CRMOrderStatus returns lightweight current status for an order.
func (h *FeedHandler) CRMOrderStatus(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}

	status, err := h.feedRepo.GetCRMOrderStatus(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get order status")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to get order status"})
		return
	}
	if status == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "Order not found"})
		return
	}

	c.JSON(http.StatusOK, status)
}

// --- Social Media Feed Endpoints ---

// SocialMilestones returns recent production milestones.
func (h *FeedHandler) SocialMilestones(c *gin.Context) {
	limit := queryInt(c, "limit", 20)

	milestones, err := h.feedRepo.GetSocialMilestones(c.Request.Context(), limit)
	if err != nil {
		h.log.WithError(err).Error("Failed to get social milestones")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to get milestones"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"milestones": milestones})
}

// SocialStats returns production statistics.
func (h *FeedHandler) SocialStats(c *gin.Context) {
	stats, err := h.feedRepo.GetSocialStats(c.Request.Context())
	if err != nil {
		h.log.WithError(err).Error("Failed to get social stats")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to get stats"})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// SocialHighlights returns curated interesting production moments.
func (h *FeedHandler) SocialHighlights(c *gin.Context) {
	limit := queryInt(c, "limit", 10)

	highlights, err := h.feedRepo.GetSocialHighlights(c.Request.Context(), limit)
	if err != nil {
		h.log.WithError(err).Error("Failed to get social highlights")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to get highlights"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"highlights": highlights})
}
