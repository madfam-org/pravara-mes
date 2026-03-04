package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/observability"
)

// SSEHandler handles Server-Sent Events streaming.
type SSEHandler struct {
	redisClient *redis.Client
	outboxRepo  *repositories.OutboxRepository
	cfg         config.SSEConfig
	log         *logrus.Logger
}

// NewSSEHandler creates a new SSE handler.
func NewSSEHandler(redisClient *redis.Client, outboxRepo *repositories.OutboxRepository, cfg config.SSEConfig, log *logrus.Logger) *SSEHandler {
	return &SSEHandler{
		redisClient: redisClient,
		outboxRepo:  outboxRepo,
		cfg:         cfg,
		log:         log,
	}
}

// Stream handles the SSE streaming endpoint.
// GET /v1/events/stream?types=order.*,task.completed&since=<timestamp>
func (h *SSEHandler) Stream(c *gin.Context) {
	tenantID, ok := middleware.GetTenantID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// Parse requested event type filters
	typesParam := c.Query("types")
	var typeFilters []string
	if typesParam != "" {
		typeFilters = strings.Split(typesParam, ",")
	}

	// Track active SSE connections
	observability.SSEConnectionsActive.Inc()
	defer observability.SSEConnectionsActive.Dec()

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // Disable nginx buffering

	// Replay missed events if Last-Event-ID is provided
	lastEventID := c.GetHeader("Last-Event-ID")
	if lastEventID == "" {
		// Also check since parameter
		if since := c.Query("since"); since != "" {
			h.replayEvents(c, tenantID, since, typeFilters)
		}
	} else {
		h.replayFromEventID(c, tenantID, lastEventID, typeFilters)
	}

	// Subscribe to Redis pub/sub for this tenant's events
	ctx := c.Request.Context()
	channelPattern := fmt.Sprintf("pravara.events.%s", tenantID)
	pubsub := h.redisClient.Subscribe(ctx, channelPattern)
	defer pubsub.Close()

	ch := pubsub.Channel()

	keepaliveSeconds := h.cfg.KeepaliveSeconds
	if keepaliveSeconds == 0 {
		keepaliveSeconds = 30
	}
	keepaliveTicker := time.NewTicker(time.Duration(keepaliveSeconds) * time.Second)
	defer keepaliveTicker.Stop()

	c.Stream(func(w io.Writer) bool {
		select {
		case <-ctx.Done():
			return false
		case msg, ok := <-ch:
			if !ok {
				return false
			}
			// Parse the event type from the message for filtering
			if h.matchesFilter(msg.Payload, typeFilters) {
				// Write SSE event
				fmt.Fprintf(w, "data: %s\n\n", msg.Payload)
			}
			return true
		case <-keepaliveTicker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			return true
		}
	})
}

// replayEvents replays events from the outbox since a given timestamp.
func (h *SSEHandler) replayEvents(c *gin.Context, tenantID string, since string, typeFilters []string) {
	sinceTime, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return
	}

	filter := repositories.OutboxEventFilter{
		Since: &sinceTime,
		Limit: 1000,
	}

	events, _, err := h.outboxRepo.ListEvents(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to replay events")
		return
	}

	for _, event := range events {
		if h.matchesFilterByType(event.EventType, typeFilters) {
			fmt.Fprintf(c.Writer, "event: %s\ndata: %s\nid: %s\n\n",
				event.EventType, string(event.Payload), event.ID.String())
		}
	}
	c.Writer.Flush()
}

// replayFromEventID replays events after a specific event ID.
func (h *SSEHandler) replayFromEventID(c *gin.Context, tenantID string, lastEventID string, typeFilters []string) {
	eventID, err := uuid.Parse(lastEventID)
	if err != nil {
		return
	}

	// Get the timestamp of the last event
	lastEvent, err := h.outboxRepo.GetEventByID(c.Request.Context(), eventID)
	if err != nil || lastEvent == nil {
		return
	}

	// Replay events after this timestamp
	since := lastEvent.CreatedAt.Add(time.Millisecond)
	filter := repositories.OutboxEventFilter{
		Since: &since,
		Limit: 1000,
	}

	events, _, err := h.outboxRepo.ListEvents(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to replay events from ID")
		return
	}

	for _, event := range events {
		if h.matchesFilterByType(event.EventType, typeFilters) {
			fmt.Fprintf(c.Writer, "event: %s\ndata: %s\nid: %s\n\n",
				event.EventType, string(event.Payload), event.ID.String())
		}
	}
	c.Writer.Flush()
}

// matchesFilter checks if a raw message matches the type filters.
// If no filters are specified, all events match.
func (h *SSEHandler) matchesFilter(payload string, typeFilters []string) bool {
	if len(typeFilters) == 0 {
		return true
	}
	// Quick check: look for event type in payload
	for _, filter := range typeFilters {
		if strings.Contains(filter, "*") {
			// Glob match: "order.*" matches any event with "order." prefix
			prefix := strings.TrimSuffix(filter, "*")
			if strings.Contains(payload, prefix) {
				return true
			}
		} else if strings.Contains(payload, filter) {
			return true
		}
	}
	return false
}

// matchesFilterByType checks if an event type matches the filters.
func (h *SSEHandler) matchesFilterByType(eventType string, typeFilters []string) bool {
	if len(typeFilters) == 0 {
		return true
	}
	for _, filter := range typeFilters {
		if strings.Contains(filter, "*") {
			prefix := strings.TrimSuffix(filter, "*")
			if strings.HasPrefix(eventType, prefix) {
				return true
			}
		} else if eventType == filter {
			return true
		}
	}
	return false
}
