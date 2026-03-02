package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// RedisUsageRecorder implements UsageRecorder using Redis for event storage.
type RedisUsageRecorder struct {
	client       *redis.Client
	log          *logrus.Logger
	mu           sync.RWMutex
	closed       bool
	eventChannel chan UsageEvent
	wg           sync.WaitGroup
	flushTicker  *time.Ticker
}

// RecorderConfig contains configuration for the RedisUsageRecorder.
type RecorderConfig struct {
	RedisURL     string
	BufferSize   int           // Size of event channel buffer
	FlushInterval time.Duration // Interval for background flush to Dhanam
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
		cfg.BufferSize = 1000 // default buffer size
	}

	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = 5 * time.Minute // default flush interval
	}

	recorder := &RedisUsageRecorder{
		client:       client,
		log:          log,
		eventChannel: make(chan UsageEvent, cfg.BufferSize),
		flushTicker:  time.NewTicker(cfg.FlushInterval),
	}

	// Start background workers
	recorder.wg.Add(2)
	go recorder.eventProcessor()
	go recorder.periodicFlush()

	log.Info("Redis usage recorder initialized")

	return recorder, nil
}

// buildKey creates a Redis key for usage tracking.
// Format: usage:{tenant_id}:{date}:{event_type}
func buildKey(tenantID string, date time.Time, eventType UsageEventType) string {
	dateStr := date.Format("2006-01-02")
	return fmt.Sprintf("usage:%s:%s:%s", tenantID, dateStr, eventType)
}

// buildDayKey creates a Redis key for all usage on a specific day.
// Format: usage:{tenant_id}:{date}
func buildDayKey(tenantID string, date time.Time) string {
	dateStr := date.Format("2006-01-02")
	return fmt.Sprintf("usage:%s:%s", tenantID, dateStr)
}

// RecordEvent records a single usage event asynchronously.
func (r *RedisUsageRecorder) RecordEvent(ctx context.Context, event UsageEvent) error {
	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return fmt.Errorf("recorder is closed")
	}
	r.mu.RUnlock()

	// Assign ID and timestamp if not set
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Non-blocking send to channel
	select {
	case r.eventChannel <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Channel full, log warning and try to record directly
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

	// Use Redis pipeline for atomic batch recording
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
		// Set expiry to 90 days for usage data
		pipe.Expire(ctx, key, 90*24*time.Hour)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		r.log.WithError(err).Error("Failed to record batch events to Redis")
		return fmt.Errorf("failed to record batch: %w", err)
	}

	r.log.WithField("count", len(events)).Debug("Recorded batch events")
	return nil
}

// recordEventToRedis records an event directly to Redis (synchronous).
func (r *RedisUsageRecorder) recordEventToRedis(ctx context.Context, event UsageEvent) error {
	key := buildKey(event.TenantID, event.Timestamp, event.EventType)

	// Atomically increment counter
	if err := r.client.IncrBy(ctx, key, event.Quantity).Err(); err != nil {
		r.log.WithError(err).WithFields(logrus.Fields{
			"tenant_id":  event.TenantID,
			"event_type": event.EventType,
			"quantity":   event.Quantity,
		}).Error("Failed to record event to Redis")
		return fmt.Errorf("failed to increment counter: %w", err)
	}

	// Set expiry to 90 days for usage data
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
				// Channel closed, flush remaining batch
				if len(batch) > 0 {
					_ = r.RecordBatch(ctx, batch)
				}
				return
			}
			batch = append(batch, event)

			// Flush batch if it reaches size limit
			if len(batch) >= 100 {
				if err := r.RecordBatch(ctx, batch); err != nil {
					r.log.WithError(err).Error("Failed to flush batch")
				}
				batch = batch[:0]
			}

		case <-ticker.C:
			// Periodic flush of accumulated events
			if len(batch) > 0 {
				if err := r.RecordBatch(ctx, batch); err != nil {
					r.log.WithError(err).Error("Failed to flush batch")
				}
				batch = batch[:0]
			}
		}
	}
}

// periodicFlush sends aggregated usage data to Dhanam API periodically.
func (r *RedisUsageRecorder) periodicFlush() {
	defer r.wg.Done()

	for range r.flushTicker.C {
		r.mu.RLock()
		if r.closed {
			r.mu.RUnlock()
			return
		}
		r.mu.RUnlock()

		// TODO: Implement actual Dhanam API integration
		// For now, this is a stub that logs the flush event
		r.log.Debug("Periodic flush to Dhanam API (stub)")

		// Future implementation:
		// 1. Scan Redis for usage data from previous day
		// 2. Aggregate by tenant and event type
		// 3. Send to Dhanam API
		// 4. On success, delete or archive Redis keys
	}
}

// GetTenantUsage retrieves aggregated usage for a tenant within a time range.
func (r *RedisUsageRecorder) GetTenantUsage(ctx context.Context, tenantID string, from, to time.Time) (*TenantUsageSummary, error) {
	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return nil, fmt.Errorf("recorder is closed")
	}
	r.mu.RUnlock()

	summary := &TenantUsageSummary{
		TenantID: tenantID,
		FromDate: from,
		ToDate:   to,
		Period:   fmt.Sprintf("%s to %s", from.Format("2006-01-02"), to.Format("2006-01-02")),
	}

	// Aggregate usage across date range
	for date := from; !date.After(to); date = date.AddDate(0, 0, 1) {
		// Get all event types for this day
		if err := r.aggregateDayUsage(ctx, tenantID, date, summary); err != nil {
			r.log.WithError(err).WithField("date", date).Warn("Failed to aggregate day usage")
			// Continue aggregating other days
		}
	}

	return summary, nil
}

// aggregateDayUsage aggregates usage for a single day into the summary.
func (r *RedisUsageRecorder) aggregateDayUsage(ctx context.Context, tenantID string, date time.Time, summary *TenantUsageSummary) error {
	eventTypes := []UsageEventType{
		UsageEventAPICall,
		UsageEventTelemetry,
		UsageEventStorage,
		UsageEventWebSocket,
		UsageEventMachine,
		UsageEventOrder,
		UsageEventCertificate,
	}

	for _, eventType := range eventTypes {
		key := buildKey(tenantID, date, eventType)
		val, err := r.client.Get(ctx, key).Result()
		if err == redis.Nil {
			// Key doesn't exist, skip
			continue
		} else if err != nil {
			return fmt.Errorf("failed to get key %s: %w", key, err)
		}

		count, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			r.log.WithError(err).WithField("key", key).Warn("Failed to parse usage count")
			continue
		}

		// Add to summary
		switch eventType {
		case UsageEventAPICall:
			summary.APICallCount += count
		case UsageEventTelemetry:
			summary.TelemetryPoints += count
		case UsageEventStorage:
			summary.StorageMB += count
		case UsageEventWebSocket:
			summary.WebSocketMinutes += count
		case UsageEventMachine:
			summary.ActiveMachines += count
		case UsageEventOrder:
			summary.OrdersCreated += count
		case UsageEventCertificate:
			summary.Certificates += count
		}
	}

	return nil
}

// GetDailyUsage retrieves daily breakdown of usage for a tenant.
func (r *RedisUsageRecorder) GetDailyUsage(ctx context.Context, tenantID string, from, to time.Time) ([]DailyUsageSummary, error) {
	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return nil, fmt.Errorf("recorder is closed")
	}
	r.mu.RUnlock()

	var dailySummaries []DailyUsageSummary

	for date := from; !date.After(to); date = date.AddDate(0, 0, 1) {
		daily := DailyUsageSummary{
			Date: date.Format("2006-01-02"),
		}

		eventTypes := []UsageEventType{
			UsageEventAPICall,
			UsageEventTelemetry,
			UsageEventStorage,
			UsageEventWebSocket,
			UsageEventMachine,
			UsageEventOrder,
			UsageEventCertificate,
		}

		for _, eventType := range eventTypes {
			key := buildKey(tenantID, date, eventType)
			val, err := r.client.Get(ctx, key).Result()
			if err == redis.Nil {
				continue
			} else if err != nil {
				r.log.WithError(err).WithField("key", key).Warn("Failed to get daily usage")
				continue
			}

			count, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				r.log.WithError(err).WithField("key", key).Warn("Failed to parse daily count")
				continue
			}

			switch eventType {
			case UsageEventAPICall:
				daily.APICallCount = count
			case UsageEventTelemetry:
				daily.TelemetryPoints = count
			case UsageEventStorage:
				daily.StorageMB = count
			case UsageEventWebSocket:
				daily.WebSocketMinutes = count
			case UsageEventMachine:
				daily.ActiveMachines = count
			case UsageEventOrder:
				daily.OrdersCreated = count
			case UsageEventCertificate:
				daily.Certificates = count
			}
		}

		dailySummaries = append(dailySummaries, daily)
	}

	return dailySummaries, nil
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

	r.log.Info("Closing usage recorder")

	// Stop background workers
	r.flushTicker.Stop()
	close(r.eventChannel)
	r.wg.Wait()

	// Close Redis connection
	return r.client.Close()
}

// DhanamClient is a stub for future Dhanam API integration.
type DhanamClient struct {
	apiURL string
	apiKey string
	log    *logrus.Logger
}

// DhanamUsageReport represents usage data sent to Dhanam.
type DhanamUsageReport struct {
	TenantID  string                       `json:"tenant_id"`
	Date      string                       `json:"date"`
	UsageData map[UsageEventType]int64     `json:"usage_data"`
	Metadata  map[string]string            `json:"metadata,omitempty"`
}

// SendUsageReport sends usage data to Dhanam API (stub implementation).
func (dc *DhanamClient) SendUsageReport(ctx context.Context, report DhanamUsageReport) error {
	// TODO: Implement actual HTTP call to Dhanam API
	// For now, just log the report
	data, _ := json.Marshal(report)
	dc.log.WithField("report", string(data)).Debug("Would send usage report to Dhanam")

	// Future implementation:
	// 1. Prepare HTTP request with report data
	// 2. Add authentication headers (API key)
	// 3. Send POST request to Dhanam billing endpoint
	// 4. Handle response and errors
	// 5. Return success/failure

	return nil
}
