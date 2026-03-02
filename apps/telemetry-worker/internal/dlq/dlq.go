// Package dlq provides a Dead-Letter Queue implementation for failed telemetry batches.
package dlq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

const (
	// DefaultDLQKey is the default Redis key for the dead-letter queue
	DefaultDLQKey = "pravara:telemetry:dlq"
	// DefaultMaxRetries is the default number of retry attempts for DLQ items
	DefaultMaxRetries = 5
	// DefaultRetryInterval is the default time between retry attempts
	DefaultRetryInterval = 5 * time.Minute
)

// DLQItem represents a failed batch in the dead-letter queue
type DLQItem struct {
	ID          string            `json:"id"`
	Batch       []types.Telemetry `json:"batch"`
	Error       string            `json:"error"`
	RetryCount  int               `json:"retry_count"`
	CreatedAt   time.Time         `json:"created_at"`
	LastRetryAt *time.Time        `json:"last_retry_at,omitempty"`
}

// DLQ represents a Dead-Letter Queue backed by Redis
type DLQ struct {
	client   *redis.Client
	log      *logrus.Logger
	key      string
	maxItems int
	mu       sync.Mutex
}

// DLQConfig holds configuration for the dead-letter queue
type DLQConfig struct {
	RedisURL    string
	Key         string
	MaxItems    int
	MaxRetries  int
	RetryPeriod time.Duration
}

// NewDLQ creates a new Dead-Letter Queue instance
func NewDLQ(client *redis.Client, log *logrus.Logger, key string, maxItems int) *DLQ {
	if key == "" {
		key = DefaultDLQKey
	}
	if maxItems <= 0 {
		maxItems = 1000 // Default to 1000 items max
	}
	return &DLQ{
		client:   client,
		log:      log,
		key:      key,
		maxItems: maxItems,
	}
}

// Push adds a failed batch to the dead-letter queue
func (d *DLQ) Push(ctx context.Context, batch []types.Telemetry, errMsg string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	item := DLQItem{
		ID:         fmt.Sprintf("dlq_%d", time.Now().UnixNano()),
		Batch:      batch,
		Error:      errMsg,
		RetryCount: 0,
		CreatedAt:  time.Now(),
	}

	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal DLQ item: %w", err)
	}

	// Add to the list
	if err := d.client.RPush(ctx, d.key, data).Err(); err != nil {
		return fmt.Errorf("failed to push to DLQ: %w", err)
	}

	// Trim to max items (keep only the most recent)
	if err := d.client.LTrim(ctx, d.key, -int64(d.maxItems), -1).Err(); err != nil {
		d.log.WithError(err).Warn("Failed to trim DLQ")
	}

	d.log.WithFields(logrus.Fields{
		"item_id":     item.ID,
		"batch_count": len(batch),
		"error":       errMsg,
	}).Info("Added batch to dead-letter queue")

	return nil
}

// Pop retrieves and removes the oldest item from the dead-letter queue
func (d *DLQ) Pop(ctx context.Context) (*DLQItem, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.client.LPop(ctx, d.key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Queue is empty
		}
		return nil, fmt.Errorf("failed to pop from DLQ: %w", err)
	}

	var item DLQItem
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, fmt.Errorf("failed to unmarshal DLQ item: %w", err)
	}

	return &item, nil
}

// Peek retrieves but does not remove the oldest item from the dead-letter queue
func (d *DLQ) Peek(ctx context.Context) (*DLQItem, error) {
	data, err := d.client.LIndex(ctx, d.key, 0).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Queue is empty
		}
		return nil, fmt.Errorf("failed to peek DLQ: %w", err)
	}

	var item DLQItem
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, fmt.Errorf("failed to unmarshal DLQ item: %w", err)
	}

	return &item, nil
}

// UpdateRetryCount updates the retry count for an item and re-adds it to the queue
func (d *DLQ) UpdateRetryCount(ctx context.Context, item *DLQItem) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	item.RetryCount++
	item.LastRetryAt = &now

	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal DLQ item: %w", err)
	}

	// Re-add to the end of the queue for retry later
	if err := d.client.RPush(ctx, d.key, data).Err(); err != nil {
		return fmt.Errorf("failed to re-push DLQ item: %w", err)
	}

	return nil
}

// Length returns the number of items in the dead-letter queue
func (d *DLQ) Length(ctx context.Context) (int64, error) {
	return d.client.LLen(ctx, d.key).Result()
}

// List returns all items in the dead-letter queue (for admin/monitoring)
func (d *DLQ) List(ctx context.Context, limit int64) ([]DLQItem, error) {
	if limit <= 0 {
		limit = 100
	}

	data, err := d.client.LRange(ctx, d.key, 0, limit-1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list DLQ items: %w", err)
	}

	items := make([]DLQItem, 0, len(data))
	for _, d := range data {
		var item DLQItem
		if err := json.Unmarshal([]byte(d), &item); err != nil {
			continue // Skip malformed items
		}
		items = append(items, item)
	}

	return items, nil
}

// Clear removes all items from the dead-letter queue
func (d *DLQ) Clear(ctx context.Context) error {
	return d.client.Del(ctx, d.key).Err()
}

// Stats returns statistics about the dead-letter queue
type DLQStats struct {
	Length      int64     `json:"length"`
	OldestItem  time.Time `json:"oldest_item,omitempty"`
	NewestItem  time.Time `json:"newest_item,omitempty"`
	TotalFailed int       `json:"total_failed"`
}

// GetStats returns statistics about the dead-letter queue
func (d *DLQ) GetStats(ctx context.Context) (*DLQStats, error) {
	length, err := d.Length(ctx)
	if err != nil {
		return nil, err
	}

	stats := &DLQStats{
		Length: length,
	}

	if length > 0 {
		// Get oldest item (first in list)
		oldest, err := d.Peek(ctx)
		if err == nil && oldest != nil {
			stats.OldestItem = oldest.CreatedAt
		}

		// Get newest item (last in list)
		data, err := d.client.LIndex(ctx, d.key, -1).Bytes()
		if err == nil {
			var newest DLQItem
			if json.Unmarshal(data, &newest) == nil {
				stats.NewestItem = newest.CreatedAt
			}
		}
	}

	return stats, nil
}
