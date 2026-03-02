// Package billing provides usage tracking for the telemetry worker.
package billing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// UsageEventType defines billable events.
type UsageEventType string

const (
	// UsageEventTelemetry tracks telemetry data points ingested.
	UsageEventTelemetry UsageEventType = "telemetry_point"
)

// UsageEvent represents a billable event.
type UsageEvent struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id"`
	EventType UsageEventType    `json:"event_type"`
	Quantity  int64             `json:"quantity"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// UsageRecorder interface for recording usage events.
type UsageRecorder interface {
	RecordEvent(ctx context.Context, event UsageEvent) error
	RecordBatch(ctx context.Context, events []UsageEvent) error
	Close() error
}

// RedisUsageRecorder implements UsageRecorder using Redis for event storage.
type RedisUsageRecorder struct {
	client       *redis.Client
	log          *logrus.Logger
	mu           sync.RWMutex
	closed       bool
	eventChannel chan UsageEvent
	wg           sync.WaitGroup
}

// RecorderConfig contains configuration for the RedisUsageRecorder.
type RecorderConfig struct {
	RedisURL   string
	BufferSize int
}

// NewRedisUsageRecorder creates a new Redis-based usage recorder.
func NewRedisUsageRecorder(cfg RecorderConfig, log *logrus.Logger) (*RedisUsageRecorder, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}

	client := redis.NewClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	if cfg.BufferSize == 0 {
		cfg.BufferSize = 1000
	}

	recorder := &RedisUsageRecorder{
		client:       client,
		log:          log,
		eventChannel: make(chan UsageEvent, cfg.BufferSize),
	}

	// Start background worker
	recorder.wg.Add(1)
	go recorder.eventProcessor()

	log.Info("Redis usage recorder initialized for telemetry worker")

	return recorder, nil
}

// buildKey creates a Redis key for usage tracking.
// Format: usage:{tenant_id}:{date}:{event_type}
func buildKey(tenantID string, date time.Time, eventType UsageEventType) string {
	dateStr := date.Format("2006-01-02")
	return fmt.Sprintf("usage:%s:%s:%s", tenantID, dateStr, eventType)
}

// RecordEvent records a single usage event asynchronously.
func (r *RedisUsageRecorder) RecordEvent(ctx context.Context, event UsageEvent) error {
	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return fmt.Errorf("recorder is closed")
	}
	r.mu.RUnlock()

	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	select {
	case r.eventChannel <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		r.log.Warn("Event channel full, recording directly to Redis")
		return r.recordEventToRedis(ctx, event)
	}
}

// RecordBatch records multiple usage events atomically.
func (r *RedisUsageRecorder) RecordBatch(ctx context.Context, events []UsageEvent) error {
	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return fmt.Errorf("recorder is closed")
	}
	r.mu.RUnlock()

	if len(events) == 0 {
		return nil
	}

	pipe := r.client.Pipeline()

	for _, event := range events {
		if event.ID == "" {
			event.ID = uuid.New().String()
		}
		if event.Timestamp.IsZero() {
			event.Timestamp = time.Now()
		}

		key := buildKey(event.TenantID, event.Timestamp, event.EventType)
		pipe.IncrBy(ctx, key, event.Quantity)
		pipe.Expire(ctx, key, 90*24*time.Hour)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		r.log.WithError(err).Error("Failed to record batch events to Redis")
		return fmt.Errorf("failed to record batch: %w", err)
	}

	r.log.WithField("count", len(events)).Debug("Recorded batch telemetry events")
	return nil
}

// recordEventToRedis records an event directly to Redis (synchronous).
func (r *RedisUsageRecorder) recordEventToRedis(ctx context.Context, event UsageEvent) error {
	key := buildKey(event.TenantID, event.Timestamp, event.EventType)

	if err := r.client.IncrBy(ctx, key, event.Quantity).Err(); err != nil {
		r.log.WithError(err).WithFields(logrus.Fields{
			"tenant_id":  event.TenantID,
			"event_type": event.EventType,
			"quantity":   event.Quantity,
		}).Error("Failed to record event to Redis")
		return fmt.Errorf("failed to increment counter: %w", err)
	}

	if err := r.client.Expire(ctx, key, 90*24*time.Hour).Err(); err != nil {
		r.log.WithError(err).Warn("Failed to set expiry on usage key")
	}

	return nil
}

// eventProcessor processes events from the channel in the background.
func (r *RedisUsageRecorder) eventProcessor() {
	defer r.wg.Done()

	ctx := context.Background()
	batch := make([]UsageEvent, 0, 100)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case event, ok := <-r.eventChannel:
			if !ok {
				if len(batch) > 0 {
					_ = r.RecordBatch(ctx, batch)
				}
				return
			}
			batch = append(batch, event)

			if len(batch) >= 100 {
				if err := r.RecordBatch(ctx, batch); err != nil {
					r.log.WithError(err).Error("Failed to flush batch")
				}
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				if err := r.RecordBatch(ctx, batch); err != nil {
					r.log.WithError(err).Error("Failed to flush batch")
				}
				batch = batch[:0]
			}
		}
	}
}

// Close closes the recorder and releases resources.
func (r *RedisUsageRecorder) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()

	r.log.Info("Closing telemetry usage recorder")

	close(r.eventChannel)
	r.wg.Wait()

	return r.client.Close()
}
