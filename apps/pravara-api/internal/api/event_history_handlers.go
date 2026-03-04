package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// EventHistoryHandler handles event history endpoints.
type EventHistoryHandler struct {
	outboxRepo *repositories.OutboxRepository
	log        *logrus.Logger
}

// NewEventHistoryHandler creates a new event history handler.
func NewEventHistoryHandler(outboxRepo *repositories.OutboxRepository, log *logrus.Logger) *EventHistoryHandler {
	return &EventHistoryHandler{outboxRepo: outboxRepo, log: log}
}

// ListEvents returns paginated, filterable event history.
func (h *EventHistoryHandler) ListEvents(c *gin.Context) {
	filter := repositories.OutboxEventFilter{
		Limit:  queryInt(c, "limit", 50),
		Offset: queryInt(c, "offset", 0),
	}

	if t := c.Query("type"); t != "" {
		filter.EventType = &t
	}
	if g := c.Query("types"); g != "" {
		filter.TypesGlob = &g
	}
	if s := c.Query("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			filter.Since = &t
		}
	}
	if u := c.Query("until"); u != "" {
		if t, err := time.Parse(time.RFC3339, u); err == nil {
			filter.Until = &t
		}
	}

	events, total, err := h.outboxRepo.ListEvents(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list events")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to list events"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"total":  total,
		"limit":  filter.Limit,
		"offset": filter.Offset,
	})
}

// GetEventByID returns a single event.
func (h *EventHistoryHandler) GetEventByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}

	event, err := h.outboxRepo.GetEventByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get event")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}
	if event == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "Event not found"})
		return
	}

	c.JSON(http.StatusOK, event)
}

// GetEventTypes returns distinct event types with counts.
func (h *EventHistoryHandler) GetEventTypes(c *gin.Context) {
	types, err := h.outboxRepo.GetEventTypes(c.Request.Context())
	if err != nil {
		h.log.WithError(err).Error("Failed to get event types")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to get event types"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"event_types": types})
}

// queryInt extracts an integer query parameter with a default value.
func queryInt(c *gin.Context, key string, defaultVal int) int {
	s := c.Query(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}
