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

// eventsDefaultLimit is GET /v1/events' page size when none is given; the
// largest is recordsMaxLimit, as on the other paged reads.
const eventsDefaultLimit = 50

// ListEvents returns paginated, filterable event history. limit is 1-1000
// (default 50) and offset at least 0; anything else is 400.
func (h *EventHistoryHandler) ListEvents(c *gin.Context) {
	filter := repositories.OutboxEventFilter{Limit: eventsDefaultLimit}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > recordsMaxLimit {
			badRequest(c, "invalid_limit", "limit must be 1-1000")
			return
		}
		filter.Limit = n
	}
	if raw := c.Query("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			badRequest(c, "invalid_offset", "offset must be >= 0")
			return
		}
		filter.Offset = n
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

	if isTerminalOnly(c) {
		events = terminalEvents(events)
	}
	c.JSON(http.StatusOK, EventListResponse{Events: events, Total: total, Limit: filter.Limit, Offset: filter.Offset})
}

// EventListResponse is the body of GET /v1/events.
type EventListResponse struct {
	Events []repositories.OutboxEvent `json:"events"`
	Total  int                        `json:"total"`
	Limit  int                        `json:"limit"`
	Offset int                        `json:"offset"`
}

// EventTypesResponse is the body of GET /v1/events/types.
type EventTypesResponse struct {
	EventTypes []repositories.EventTypeCount `json:"event_types"`
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

	if isTerminalOnly(c) {
		narrowed := terminalEvent(*event)
		event = &narrowed
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

	c.JSON(http.StatusOK, EventTypesResponse{EventTypes: types})
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
