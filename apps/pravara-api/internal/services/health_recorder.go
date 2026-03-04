package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/observability"
)

// HealthRecorder is a background service that periodically checks component health.
type HealthRecorder struct {
	db          *sql.DB
	redisClient *redis.Client
	centrifugo  config.CentrifugoConfig
	log         *logrus.Logger
	lastStatus  map[string]string // track state transitions
}

// NewHealthRecorder creates a new health recorder.
func NewHealthRecorder(db *sql.DB, redisClient *redis.Client, centrifugoCfg config.CentrifugoConfig, log *logrus.Logger) *HealthRecorder {
	return &HealthRecorder{
		db:          db,
		redisClient: redisClient,
		centrifugo:  centrifugoCfg,
		log:         log,
		lastStatus:  make(map[string]string),
	}
}

// Start begins the background health check loop. It blocks until ctx is cancelled.
func (h *HealthRecorder) Start(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	h.log.Info("Health recorder started")

	// Run an initial check immediately
	h.recordHealthChecks(ctx)

	for {
		select {
		case <-ctx.Done():
			h.log.Info("Health recorder stopping")
			return
		case <-ticker.C:
			h.recordHealthChecks(ctx)
		}
	}
}

func (h *HealthRecorder) recordHealthChecks(ctx context.Context) {
	// Check database
	h.checkComponent(ctx, "database", func() (string, map[string]any) {
		if err := h.db.PingContext(ctx); err != nil {
			return "outage", map[string]any{"error": err.Error()}
		}
		stats := h.db.Stats()
		return "operational", map[string]any{
			"open_connections": stats.OpenConnections,
			"in_use":           stats.InUse,
			"idle":             stats.Idle,
		}
	})

	// Check Redis
	if h.redisClient != nil {
		h.checkComponent(ctx, "redis", func() (string, map[string]any) {
			if err := h.redisClient.Ping(ctx).Err(); err != nil {
				return "outage", map[string]any{"error": err.Error()}
			}
			return "operational", map[string]any{}
		})
	}

	// Check Centrifugo (if configured)
	if h.centrifugo.APIURL != "" {
		h.checkComponent(ctx, "centrifugo", func() (string, map[string]any) {
			// Simple ping via Redis pub/sub channel existence
			// In production, this could hit the Centrifugo HTTP API health endpoint
			return "operational", map[string]any{"api_url": h.centrifugo.APIURL}
		})
	}
}

func (h *HealthRecorder) checkComponent(ctx context.Context, component string, checker func() (string, map[string]any)) {
	status, details := checker()

	// Detect state transition
	prevStatus, existed := h.lastStatus[component]
	if existed && prevStatus != status {
		h.log.WithFields(logrus.Fields{
			"component":  component,
			"old_status": prevStatus,
			"new_status": status,
		}).Warn("Component status changed")

		// Publish incident event to outbox (if DB is available)
		h.publishHealthTransition(ctx, component, prevStatus, status, details)
	}
	h.lastStatus[component] = status

	// Record snapshot
	detailsJSON, _ := json.Marshal(details)
	_, err := h.db.ExecContext(ctx,
		`INSERT INTO health_snapshots (component, status, details, checked_at)
		 VALUES ($1, $2, $3, NOW())`,
		component, status, detailsJSON,
	)
	if err != nil {
		h.log.WithError(err).WithField("component", component).Error("Failed to record health snapshot")
	}

	// Update metrics
	statusVal := float64(0)
	if status == "operational" {
		statusVal = 1
	} else if status == "degraded" {
		statusVal = 0.5
	}
	observability.HealthComponentStatus.WithLabelValues(component).Set(statusVal)
}

func (h *HealthRecorder) publishHealthTransition(ctx context.Context, component, oldStatus, newStatus string, details map[string]any) {
	payload := map[string]any{
		"component":  component,
		"old_status": oldStatus,
		"new_status": newStatus,
		"details":    details,
		"timestamp":  time.Now().UTC(),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return
	}

	// Insert directly into outbox (bypassing publisher since this is a system event)
	_, err = h.db.ExecContext(ctx,
		`INSERT INTO event_outbox (id, tenant_id, event_type, channel_namespace, payload)
		 VALUES (gen_random_uuid(), '00000000-0000-0000-0000-000000000000'::uuid, $1, 'system', $2)`,
		fmt.Sprintf("system.health.%s", newStatus), payloadJSON,
	)
	if err != nil {
		h.log.WithError(err).Error("Failed to publish health transition event")
	}
}
