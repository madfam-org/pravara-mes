package dlq

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

func setupTestDLQ(t *testing.T) (*DLQ, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	dlq := NewDLQ(client, log, "test:dlq", 100)

	return dlq, mr
}

func createTestBatch(count int) []types.Telemetry {
	batch := make([]types.Telemetry, count)
	for i := 0; i < count; i++ {
		batch[i] = types.Telemetry{
			ID:         uuid.New(),
			TenantID:   uuid.New(),
			MachineID:  uuid.New(),
			Timestamp:  time.Now(),
			MetricType: "temperature",
			Value:      25.5,
			Unit:       "celsius",
		}
	}
	return batch
}

func TestNewDLQ(t *testing.T) {
	t.Run("with default key", func(t *testing.T) {
		mr, err := miniredis.Run()
		require.NoError(t, err)
		defer mr.Close()

		client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		log := logrus.New()

		dlq := NewDLQ(client, log, "", 0)

		assert.Equal(t, DefaultDLQKey, dlq.key)
		assert.Equal(t, 1000, dlq.maxItems)
	})

	t.Run("with custom key and max items", func(t *testing.T) {
		mr, err := miniredis.Run()
		require.NoError(t, err)
		defer mr.Close()

		client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		log := logrus.New()

		dlq := NewDLQ(client, log, "custom:key", 500)

		assert.Equal(t, "custom:key", dlq.key)
		assert.Equal(t, 500, dlq.maxItems)
	})
}

func TestDLQ_Push(t *testing.T) {
	dlq, mr := setupTestDLQ(t)
	defer mr.Close()

	ctx := context.Background()
	batch := createTestBatch(5)

	err := dlq.Push(ctx, batch, "test error message")
	require.NoError(t, err)

	length, err := dlq.Length(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), length)
}

func TestDLQ_Pop(t *testing.T) {
	dlq, mr := setupTestDLQ(t)
	defer mr.Close()

	ctx := context.Background()

	t.Run("pop from empty queue", func(t *testing.T) {
		item, err := dlq.Pop(ctx)
		require.NoError(t, err)
		assert.Nil(t, item)
	})

	t.Run("pop from non-empty queue", func(t *testing.T) {
		batch := createTestBatch(3)
		err := dlq.Push(ctx, batch, "error 1")
		require.NoError(t, err)

		item, err := dlq.Pop(ctx)
		require.NoError(t, err)
		require.NotNil(t, item)

		assert.Equal(t, "error 1", item.Error)
		assert.Equal(t, 3, len(item.Batch))
		assert.Equal(t, 0, item.RetryCount)
		assert.NotEmpty(t, item.ID)

		// Queue should be empty now
		length, err := dlq.Length(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), length)
	})
}

func TestDLQ_Peek(t *testing.T) {
	dlq, mr := setupTestDLQ(t)
	defer mr.Close()

	ctx := context.Background()

	t.Run("peek empty queue", func(t *testing.T) {
		item, err := dlq.Peek(ctx)
		require.NoError(t, err)
		assert.Nil(t, item)
	})

	t.Run("peek non-empty queue", func(t *testing.T) {
		batch := createTestBatch(2)
		err := dlq.Push(ctx, batch, "peek test")
		require.NoError(t, err)

		// Peek should return item without removing
		item, err := dlq.Peek(ctx)
		require.NoError(t, err)
		require.NotNil(t, item)
		assert.Equal(t, "peek test", item.Error)

		// Item should still be in queue
		length, err := dlq.Length(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), length)
	})
}

func TestDLQ_UpdateRetryCount(t *testing.T) {
	dlq, mr := setupTestDLQ(t)
	defer mr.Close()

	ctx := context.Background()
	batch := createTestBatch(1)

	err := dlq.Push(ctx, batch, "retry test")
	require.NoError(t, err)

	item, err := dlq.Pop(ctx)
	require.NoError(t, err)
	require.NotNil(t, item)

	assert.Equal(t, 0, item.RetryCount)
	assert.Nil(t, item.LastRetryAt)

	// Update retry count and re-add
	err = dlq.UpdateRetryCount(ctx, item)
	require.NoError(t, err)

	// Pop again and verify
	item2, err := dlq.Pop(ctx)
	require.NoError(t, err)
	require.NotNil(t, item2)

	assert.Equal(t, 1, item2.RetryCount)
	assert.NotNil(t, item2.LastRetryAt)
}

func TestDLQ_Length(t *testing.T) {
	dlq, mr := setupTestDLQ(t)
	defer mr.Close()

	ctx := context.Background()

	length, err := dlq.Length(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), length)

	// Add items
	for i := 0; i < 5; i++ {
		err := dlq.Push(ctx, createTestBatch(1), "error")
		require.NoError(t, err)
	}

	length, err = dlq.Length(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), length)
}

func TestDLQ_List(t *testing.T) {
	dlq, mr := setupTestDLQ(t)
	defer mr.Close()

	ctx := context.Background()

	// Add multiple items
	for i := 0; i < 5; i++ {
		err := dlq.Push(ctx, createTestBatch(1), "error")
		require.NoError(t, err)
	}

	t.Run("list with default limit", func(t *testing.T) {
		items, err := dlq.List(ctx, 0)
		require.NoError(t, err)
		assert.Equal(t, 5, len(items))
	})

	t.Run("list with custom limit", func(t *testing.T) {
		items, err := dlq.List(ctx, 3)
		require.NoError(t, err)
		assert.Equal(t, 3, len(items))
	})
}

func TestDLQ_Clear(t *testing.T) {
	dlq, mr := setupTestDLQ(t)
	defer mr.Close()

	ctx := context.Background()

	// Add items
	for i := 0; i < 3; i++ {
		err := dlq.Push(ctx, createTestBatch(1), "error")
		require.NoError(t, err)
	}

	length, err := dlq.Length(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), length)

	// Clear
	err = dlq.Clear(ctx)
	require.NoError(t, err)

	length, err = dlq.Length(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), length)
}

func TestDLQ_GetStats(t *testing.T) {
	dlq, mr := setupTestDLQ(t)
	defer mr.Close()

	ctx := context.Background()

	t.Run("empty queue stats", func(t *testing.T) {
		stats, err := dlq.GetStats(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), stats.Length)
	})

	t.Run("non-empty queue stats", func(t *testing.T) {
		// Add items with delay to get different timestamps
		err := dlq.Push(ctx, createTestBatch(1), "oldest")
		require.NoError(t, err)

		time.Sleep(10 * time.Millisecond)

		err = dlq.Push(ctx, createTestBatch(1), "newest")
		require.NoError(t, err)

		stats, err := dlq.GetStats(ctx)
		require.NoError(t, err)

		assert.Equal(t, int64(2), stats.Length)
		assert.False(t, stats.OldestItem.IsZero())
		assert.False(t, stats.NewestItem.IsZero())
		assert.True(t, stats.NewestItem.After(stats.OldestItem) || stats.NewestItem.Equal(stats.OldestItem))
	})
}

func TestDLQ_MaxItems(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	// Create DLQ with small max items
	dlq := NewDLQ(client, log, "test:dlq", 3)

	ctx := context.Background()

	// Add more items than max
	for i := 0; i < 5; i++ {
		err := dlq.Push(ctx, createTestBatch(1), "error")
		require.NoError(t, err)
	}

	// Should be trimmed to max items
	length, err := dlq.Length(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), length)
}

func TestDLQ_FIFO_Order(t *testing.T) {
	dlq, mr := setupTestDLQ(t)
	defer mr.Close()

	ctx := context.Background()

	// Push items in order
	for i := 1; i <= 3; i++ {
		err := dlq.Push(ctx, createTestBatch(1), "error_"+string(rune('0'+i)))
		require.NoError(t, err)
	}

	// Pop should return in FIFO order
	item1, err := dlq.Pop(ctx)
	require.NoError(t, err)
	assert.Equal(t, "error_1", item1.Error)

	item2, err := dlq.Pop(ctx)
	require.NoError(t, err)
	assert.Equal(t, "error_2", item2.Error)

	item3, err := dlq.Pop(ctx)
	require.NoError(t, err)
	assert.Equal(t, "error_3", item3.Error)
}
