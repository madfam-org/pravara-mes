package billing

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

func setupTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return mr, client
}

func TestRedisUsageRecorder_RecordEvent_Success(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	recorder := &RedisUsageRecorder{
		client:       client,
		log:          log,
		eventChannel: make(chan UsageEvent, 100),
	}

	ctx := context.Background()
	event := UsageEvent{
		TenantID:  "tenant-123",
		EventType: UsageEventTelemetry,
		Quantity:  5,
		Timestamp: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}

	err := recorder.RecordEvent(ctx, event)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify event was sent to channel
	select {
	case received := <-recorder.eventChannel:
		if received.TenantID != event.TenantID {
			t.Errorf("TenantID: got %q, want %q", received.TenantID, event.TenantID)
		}
		if received.EventType != event.EventType {
			t.Errorf("EventType: got %q, want %q", received.EventType, event.EventType)
		}
		if received.Quantity != event.Quantity {
			t.Errorf("Quantity: got %d, want %d", received.Quantity, event.Quantity)
		}
		if received.ID == "" {
			t.Error("expected ID to be auto-generated")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in channel")
	}
}

func TestRedisUsageRecorder_RecordBatch_Success(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	recorder := &RedisUsageRecorder{
		client: client,
		log:    log,
	}

	ctx := context.Background()
	timestamp := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	events := []UsageEvent{
		{
			TenantID:  "tenant-123",
			EventType: UsageEventTelemetry,
			Quantity:  5,
			Timestamp: timestamp,
		},
		{
			TenantID:  "tenant-123",
			EventType: UsageEventTelemetry,
			Quantity:  3,
			Timestamp: timestamp,
		},
	}

	err := recorder.RecordBatch(ctx, events)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify counter in Redis
	key := buildKey("tenant-123", timestamp, UsageEventTelemetry)
	val, err := client.Get(ctx, key).Int64()
	if err != nil {
		t.Fatalf("failed to get key from redis: %v", err)
	}

	expectedTotal := int64(8) // 5 + 3
	if val != expectedTotal {
		t.Errorf("counter value: got %d, want %d", val, expectedTotal)
	}

	// Verify TTL is set
	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("failed to get TTL: %v", err)
	}
	if ttl <= 0 {
		t.Error("expected positive TTL")
	}
}

func TestRedisUsageRecorder_RecordBatch_Empty(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	recorder := &RedisUsageRecorder{
		client: client,
		log:    log,
	}

	ctx := context.Background()

	// Empty batch should not error
	err := recorder.RecordBatch(ctx, []UsageEvent{})
	if err != nil {
		t.Errorf("expected no error for empty batch, got: %v", err)
	}

	// Nil batch should not error
	err = recorder.RecordBatch(ctx, nil)
	if err != nil {
		t.Errorf("expected no error for nil batch, got: %v", err)
	}
}

func TestRedisUsageRecorder_ChannelFull_FallbackToRedis(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	// Create recorder with small buffer
	recorder := &RedisUsageRecorder{
		client:       client,
		log:          log,
		eventChannel: make(chan UsageEvent, 1),
	}

	ctx := context.Background()
	timestamp := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	// Fill the channel
	event1 := UsageEvent{
		TenantID:  "tenant-123",
		EventType: UsageEventTelemetry,
		Quantity:  1,
		Timestamp: timestamp,
	}
	recorder.eventChannel <- event1

	// This should trigger fallback to direct Redis write
	event2 := UsageEvent{
		TenantID:  "tenant-456",
		EventType: UsageEventTelemetry,
		Quantity:  10,
		Timestamp: timestamp,
	}

	err := recorder.RecordEvent(ctx, event2)
	if err != nil {
		t.Fatalf("expected no error on fallback, got: %v", err)
	}

	// Verify event2 was written directly to Redis
	key := buildKey("tenant-456", timestamp, UsageEventTelemetry)
	val, err := client.Get(ctx, key).Int64()
	if err != nil {
		t.Fatalf("failed to get key from redis: %v", err)
	}

	if val != 10 {
		t.Errorf("counter value: got %d, want 10", val)
	}
}

func TestRedisUsageRecorder_Close_GracefulShutdown(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	recorder := &RedisUsageRecorder{
		client:       client,
		log:          log,
		eventChannel: make(chan UsageEvent, 100),
	}

	// Start the event processor
	recorder.wg.Add(1)
	go recorder.eventProcessor()

	// Send some events
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		event := UsageEvent{
			TenantID:  "tenant-123",
			EventType: UsageEventTelemetry,
			Quantity:  1,
			Timestamp: time.Now(),
		}
		recorder.RecordEvent(ctx, event)
	}

	// Close should wait for events to be processed
	err := recorder.Close()
	if err != nil {
		t.Fatalf("expected no error on close, got: %v", err)
	}

	// Verify recorder is marked as closed
	recorder.mu.RLock()
	if !recorder.closed {
		t.Error("expected recorder to be marked as closed")
	}
	recorder.mu.RUnlock()

	// Subsequent RecordEvent should fail
	event := UsageEvent{
		TenantID:  "tenant-123",
		EventType: UsageEventTelemetry,
		Quantity:  1,
	}
	err = recorder.RecordEvent(ctx, event)
	if err == nil {
		t.Error("expected error when recording after close")
	}
}

func TestBuildKey_Format(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		date      time.Time
		eventType UsageEventType
		expected  string
	}{
		{
			name:      "standard key",
			tenantID:  "tenant-123",
			date:      time.Date(2026, 3, 1, 12, 30, 45, 0, time.UTC),
			eventType: UsageEventTelemetry,
			expected:  "usage:tenant-123:2026-03-01:telemetry_point",
		},
		{
			name:      "different date",
			tenantID:  "tenant-456",
			date:      time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC),
			eventType: UsageEventTelemetry,
			expected:  "usage:tenant-456:2025-12-31:telemetry_point",
		},
		{
			name:      "start of year",
			tenantID:  "acme-corp",
			date:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			eventType: UsageEventTelemetry,
			expected:  "usage:acme-corp:2026-01-01:telemetry_point",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := buildKey(tt.tenantID, tt.date, tt.eventType)
			if key != tt.expected {
				t.Errorf("key: got %q, want %q", key, tt.expected)
			}
		})
	}
}

func TestEventProcessor_BatchFlush(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	recorder := &RedisUsageRecorder{
		client:       client,
		log:          log,
		eventChannel: make(chan UsageEvent, 200),
	}

	// Start processor
	recorder.wg.Add(1)
	go recorder.eventProcessor()

	ctx := context.Background()
	timestamp := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	// Send 100 events to trigger batch flush
	for i := 0; i < 100; i++ {
		event := UsageEvent{
			TenantID:  "tenant-123",
			EventType: UsageEventTelemetry,
			Quantity:  1,
			Timestamp: timestamp,
		}
		recorder.eventChannel <- event
	}

	// Wait a bit for processing
	time.Sleep(200 * time.Millisecond)

	// Verify events were flushed
	key := buildKey("tenant-123", timestamp, UsageEventTelemetry)
	val, err := client.Get(ctx, key).Int64()
	if err != nil {
		t.Fatalf("failed to get key from redis: %v", err)
	}

	if val != 100 {
		t.Errorf("counter value: got %d, want 100", val)
	}

	recorder.Close()
}

func TestEventProcessor_TickerFlush(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	recorder := &RedisUsageRecorder{
		client:       client,
		log:          log,
		eventChannel: make(chan UsageEvent, 200),
	}

	// Start processor
	recorder.wg.Add(1)
	go recorder.eventProcessor()

	ctx := context.Background()
	timestamp := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	// Send small number of events (less than batch size)
	for i := 0; i < 10; i++ {
		event := UsageEvent{
			TenantID:  "tenant-123",
			EventType: UsageEventTelemetry,
			Quantity:  1,
			Timestamp: timestamp,
		}
		recorder.eventChannel <- event
	}

	// Wait for ticker flush (1 second + buffer)
	time.Sleep(1500 * time.Millisecond)

	// Verify events were flushed by ticker
	key := buildKey("tenant-123", timestamp, UsageEventTelemetry)
	val, err := client.Get(ctx, key).Int64()
	if err != nil {
		t.Fatalf("failed to get key from redis: %v", err)
	}

	if val != 10 {
		t.Errorf("counter value: got %d, want 10", val)
	}

	recorder.Close()
}

func TestRecorderConfig_DefaultBufferSize(t *testing.T) {
	tests := []struct {
		name             string
		inputBufferSize  int
		expectBufferSize int
	}{
		{
			name:             "zero buffer gets default",
			inputBufferSize:  0,
			expectBufferSize: 1000,
		},
		{
			name:             "custom buffer preserved",
			inputBufferSize:  500,
			expectBufferSize: 500,
		},
		{
			name:             "large buffer preserved",
			inputBufferSize:  10000,
			expectBufferSize: 10000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr, err := miniredis.Run()
			if err != nil {
				t.Fatalf("failed to start miniredis: %v", err)
			}
			defer mr.Close()

			log := logrus.New()
			log.SetLevel(logrus.ErrorLevel)

			cfg := RecorderConfig{
				RedisURL:   "redis://" + mr.Addr(),
				BufferSize: tt.inputBufferSize,
			}

			recorder, err := NewRedisUsageRecorder(cfg, log)
			if err != nil {
				t.Fatalf("failed to create recorder: %v", err)
			}
			defer recorder.Close()

			actualSize := cap(recorder.eventChannel)
			if actualSize != tt.expectBufferSize {
				t.Errorf("buffer size: got %d, want %d", actualSize, tt.expectBufferSize)
			}
		})
	}
}

func TestNewRedisUsageRecorder_InvalidURL(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	cfg := RecorderConfig{
		RedisURL:   "invalid://url",
		BufferSize: 100,
	}

	_, err := NewRedisUsageRecorder(cfg, log)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestNewRedisUsageRecorder_ConnectionFailure(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	cfg := RecorderConfig{
		RedisURL:   "redis://localhost:9999",
		BufferSize: 100,
	}

	_, err := NewRedisUsageRecorder(cfg, log)
	if err == nil {
		t.Error("expected error for failed connection")
	}
}

func TestUsageEvent_IDGeneration(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	recorder := &RedisUsageRecorder{
		client:       client,
		log:          log,
		eventChannel: make(chan UsageEvent, 100),
	}

	ctx := context.Background()

	// Event without ID
	event := UsageEvent{
		TenantID:  "tenant-123",
		EventType: UsageEventTelemetry,
		Quantity:  5,
	}

	err := recorder.RecordEvent(ctx, event)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify ID was auto-generated
	select {
	case received := <-recorder.eventChannel:
		if received.ID == "" {
			t.Error("expected ID to be auto-generated")
		}
		if _, err := uuid.Parse(received.ID); err != nil {
			t.Errorf("expected valid UUID, got: %q", received.ID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in channel")
	}
}

func TestUsageEvent_TimestampGeneration(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	recorder := &RedisUsageRecorder{
		client:       client,
		log:          log,
		eventChannel: make(chan UsageEvent, 100),
	}

	ctx := context.Background()

	// Event without timestamp
	event := UsageEvent{
		TenantID:  "tenant-123",
		EventType: UsageEventTelemetry,
		Quantity:  5,
	}

	before := time.Now()
	err := recorder.RecordEvent(ctx, event)
	after := time.Now()

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify timestamp was auto-generated
	select {
	case received := <-recorder.eventChannel:
		if received.Timestamp.IsZero() {
			t.Error("expected timestamp to be auto-generated")
		}
		if received.Timestamp.Before(before) || received.Timestamp.After(after) {
			t.Errorf("timestamp out of expected range: %v", received.Timestamp)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event in channel")
	}
}
