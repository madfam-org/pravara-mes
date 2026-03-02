package billing

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRecorder(t *testing.T) (*RedisUsageRecorder, *miniredis.Miniredis) {
	// Start embedded Redis server for testing
	mr, err := miniredis.Run()
	require.NoError(t, err)

	log := logrus.New()
	log.SetLevel(logrus.FatalLevel) // Suppress logs in tests

	recorder, err := NewRedisUsageRecorder(RecorderConfig{
		RedisURL:      "redis://" + mr.Addr(),
		BufferSize:    100,
		FlushInterval: 1 * time.Second,
	}, log)
	require.NoError(t, err)

	return recorder, mr
}

func TestRecordEvent(t *testing.T) {
	recorder, mr := setupTestRecorder(t)
	defer mr.Close()
	defer recorder.Close()

	ctx := context.Background()
	event := UsageEvent{
		TenantID:  "tenant-123",
		EventType: UsageEventAPICall,
		Quantity:  1,
		Timestamp: time.Now(),
	}

	err := recorder.RecordEvent(ctx, event)
	assert.NoError(t, err)

	// Wait for background processing
	time.Sleep(2 * time.Second)

	// Verify event was recorded in Redis
	key := buildKey(event.TenantID, event.Timestamp, event.EventType)
	val, err := mr.Get(key)
	assert.NoError(t, err)
	assert.Equal(t, "1", val)
}

func TestRecordBatch(t *testing.T) {
	recorder, mr := setupTestRecorder(t)
	defer mr.Close()
	defer recorder.Close()

	ctx := context.Background()
	now := time.Now()

	events := []UsageEvent{
		{
			TenantID:  "tenant-123",
			EventType: UsageEventAPICall,
			Quantity:  5,
			Timestamp: now,
		},
		{
			TenantID:  "tenant-123",
			EventType: UsageEventTelemetry,
			Quantity:  100,
			Timestamp: now,
		},
		{
			TenantID:  "tenant-456",
			EventType: UsageEventAPICall,
			Quantity:  3,
			Timestamp: now,
		},
	}

	err := recorder.RecordBatch(ctx, events)
	assert.NoError(t, err)

	// Verify all events recorded
	key1 := buildKey("tenant-123", now, UsageEventAPICall)
	val1, _ := mr.Get(key1)
	assert.Equal(t, "5", val1)

	key2 := buildKey("tenant-123", now, UsageEventTelemetry)
	val2, _ := mr.Get(key2)
	assert.Equal(t, "100", val2)

	key3 := buildKey("tenant-456", now, UsageEventAPICall)
	val3, _ := mr.Get(key3)
	assert.Equal(t, "3", val3)
}

func TestGetTenantUsage(t *testing.T) {
	recorder, mr := setupTestRecorder(t)
	defer mr.Close()
	defer recorder.Close()

	ctx := context.Background()
	tenantID := "tenant-123"
	now := time.Now()

	// Record some events
	events := []UsageEvent{
		{TenantID: tenantID, EventType: UsageEventAPICall, Quantity: 10, Timestamp: now},
		{TenantID: tenantID, EventType: UsageEventTelemetry, Quantity: 500, Timestamp: now},
		{TenantID: tenantID, EventType: UsageEventOrder, Quantity: 2, Timestamp: now},
	}

	err := recorder.RecordBatch(ctx, events)
	require.NoError(t, err)

	// Get usage summary
	from := now.AddDate(0, 0, -1)
	to := now.AddDate(0, 0, 1)

	summary, err := recorder.GetTenantUsage(ctx, tenantID, from, to)
	require.NoError(t, err)
	assert.Equal(t, tenantID, summary.TenantID)
	assert.Equal(t, int64(10), summary.APICallCount)
	assert.Equal(t, int64(500), summary.TelemetryPoints)
	assert.Equal(t, int64(2), summary.OrdersCreated)
}

func TestGetDailyUsage(t *testing.T) {
	recorder, mr := setupTestRecorder(t)
	defer mr.Close()
	defer recorder.Close()

	ctx := context.Background()
	tenantID := "tenant-123"

	// Record events for 3 days
	day1 := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC)
	day3 := time.Date(2024, 3, 3, 0, 0, 0, 0, time.UTC)

	events := []UsageEvent{
		{TenantID: tenantID, EventType: UsageEventAPICall, Quantity: 10, Timestamp: day1},
		{TenantID: tenantID, EventType: UsageEventAPICall, Quantity: 20, Timestamp: day2},
		{TenantID: tenantID, EventType: UsageEventAPICall, Quantity: 30, Timestamp: day3},
	}

	err := recorder.RecordBatch(ctx, events)
	require.NoError(t, err)

	// Get daily breakdown
	dailyUsage, err := recorder.GetDailyUsage(ctx, tenantID, day1, day3)
	require.NoError(t, err)
	require.Len(t, dailyUsage, 3)

	assert.Equal(t, "2024-03-01", dailyUsage[0].Date)
	assert.Equal(t, int64(10), dailyUsage[0].APICallCount)

	assert.Equal(t, "2024-03-02", dailyUsage[1].Date)
	assert.Equal(t, int64(20), dailyUsage[1].APICallCount)

	assert.Equal(t, "2024-03-03", dailyUsage[2].Date)
	assert.Equal(t, int64(30), dailyUsage[2].APICallCount)
}

func TestKeyExpiry(t *testing.T) {
	recorder, mr := setupTestRecorder(t)
	defer mr.Close()
	defer recorder.Close()

	ctx := context.Background()
	event := UsageEvent{
		TenantID:  "tenant-123",
		EventType: UsageEventAPICall,
		Quantity:  1,
		Timestamp: time.Now(),
	}

	err := recorder.RecordBatch(ctx, []UsageEvent{event})
	require.NoError(t, err)

	// Check TTL is set (should be 90 days)
	key := buildKey(event.TenantID, event.Timestamp, event.EventType)
	ttl := mr.TTL(key)

	// TTL should be approximately 90 days (allow some margin)
	expectedTTL := 90 * 24 * time.Hour
	assert.True(t, ttl > expectedTTL-time.Minute && ttl <= expectedTTL)
}

func TestConcurrentRecording(t *testing.T) {
	recorder, mr := setupTestRecorder(t)
	defer mr.Close()
	defer recorder.Close()

	ctx := context.Background()
	tenantID := "tenant-concurrent"
	now := time.Now()

	// Record events concurrently
	concurrency := 10
	eventsPerGoroutine := 5

	done := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			for j := 0; j < eventsPerGoroutine; j++ {
				event := UsageEvent{
					TenantID:  tenantID,
					EventType: UsageEventAPICall,
					Quantity:  1,
					Timestamp: now,
				}
				_ = recorder.RecordEvent(ctx, event)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < concurrency; i++ {
		<-done
	}

	// Wait for background processing
	time.Sleep(3 * time.Second)

	// Verify total count
	summary, err := recorder.GetTenantUsage(ctx, tenantID, now.AddDate(0, 0, -1), now.AddDate(0, 0, 1))
	require.NoError(t, err)
	assert.Equal(t, int64(concurrency*eventsPerGoroutine), summary.APICallCount)
}

func TestBuildKey(t *testing.T) {
	tenantID := "550e8400-e29b-41d4-a716-446655440000"
	date := time.Date(2024, 3, 15, 14, 30, 0, 0, time.UTC)
	eventType := UsageEventAPICall

	key := buildKey(tenantID, date, eventType)
	expected := "usage:550e8400-e29b-41d4-a716-446655440000:2024-03-15:api_call"

	assert.Equal(t, expected, key)
}

func TestRecorderClose(t *testing.T) {
	recorder, mr := setupTestRecorder(t)
	defer mr.Close()

	err := recorder.Close()
	assert.NoError(t, err)

	// Attempting to record after close should fail
	ctx := context.Background()
	event := UsageEvent{
		TenantID:  "tenant-123",
		EventType: UsageEventAPICall,
		Quantity:  1,
	}

	err = recorder.RecordEvent(ctx, event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "recorder is closed")
}
