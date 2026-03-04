package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// WebhookSubscription represents a registered webhook endpoint.
type WebhookSubscription struct {
	ID         uuid.UUID `json:"id"`
	TenantID   uuid.UUID `json:"tenant_id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	Secret     string    `json:"secret,omitempty"`
	EventTypes []string  `json:"event_types"`
	IsActive   bool      `json:"is_active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// WebhookDelivery represents a delivery attempt for a webhook.
type WebhookDelivery struct {
	ID             uuid.UUID `json:"id"`
	SubscriptionID uuid.UUID `json:"subscription_id"`
	EventID        uuid.UUID `json:"event_id"`
	Status         string    `json:"status"`
	HTTPStatus     *int      `json:"http_status,omitempty"`
	AttemptCount   int       `json:"attempt_count"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
	LastError      *string   `json:"last_error,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// WebhookRepository handles webhook subscription and delivery database operations.
type WebhookRepository struct {
	db *sql.DB
}

// NewWebhookRepository creates a new webhook repository.
func NewWebhookRepository(db *sql.DB) *WebhookRepository {
	return &WebhookRepository{db: db}
}

// CreateSubscription creates a new webhook subscription.
func (r *WebhookRepository) CreateSubscription(ctx context.Context, sub *WebhookSubscription) error {
	if sub.ID == uuid.Nil {
		sub.ID = uuid.New()
	}
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO webhook_subscriptions (id, tenant_id, name, url, secret, event_types, is_active)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING created_at, updated_at`,
		sub.ID, sub.TenantID, sub.Name, sub.URL, sub.Secret, pq.Array(sub.EventTypes), sub.IsActive,
	).Scan(&sub.CreatedAt, &sub.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create webhook subscription: %w", err)
	}
	return nil
}

// GetSubscriptionByID retrieves a subscription by ID (tenant-scoped via RLS).
func (r *WebhookRepository) GetSubscriptionByID(ctx context.Context, id uuid.UUID) (*WebhookSubscription, error) {
	var sub WebhookSubscription
	err := r.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, url, secret, event_types, is_active, created_at, updated_at
		 FROM webhook_subscriptions WHERE id = $1`,
		id,
	).Scan(&sub.ID, &sub.TenantID, &sub.Name, &sub.URL, &sub.Secret,
		pq.Array(&sub.EventTypes), &sub.IsActive, &sub.CreatedAt, &sub.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get webhook subscription: %w", err)
	}
	return &sub, nil
}

// ListSubscriptions returns all subscriptions for the current tenant (via RLS).
func (r *WebhookRepository) ListSubscriptions(ctx context.Context) ([]WebhookSubscription, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, tenant_id, name, url, secret, event_types, is_active, created_at, updated_at
		 FROM webhook_subscriptions
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list webhook subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []WebhookSubscription
	for rows.Next() {
		var sub WebhookSubscription
		if err := rows.Scan(&sub.ID, &sub.TenantID, &sub.Name, &sub.URL, &sub.Secret,
			pq.Array(&sub.EventTypes), &sub.IsActive, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan webhook subscription: %w", err)
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

// UpdateSubscription updates a webhook subscription.
func (r *WebhookRepository) UpdateSubscription(ctx context.Context, sub *WebhookSubscription) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE webhook_subscriptions
		 SET name = $2, url = $3, secret = $4, event_types = $5, is_active = $6
		 WHERE id = $1`,
		sub.ID, sub.Name, sub.URL, sub.Secret, pq.Array(sub.EventTypes), sub.IsActive,
	)
	if err != nil {
		return fmt.Errorf("failed to update webhook subscription: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSubscription deletes a webhook subscription.
func (r *WebhookRepository) DeleteSubscription(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM webhook_subscriptions WHERE id = $1`, id,
	)
	if err != nil {
		return fmt.Errorf("failed to delete webhook subscription: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// GetActiveSubscriptionsForEvent retrieves active subscriptions matching an event type for a tenant.
// This is a system query that bypasses RLS (called by webhook dispatcher).
func (r *WebhookRepository) GetActiveSubscriptionsForEvent(ctx context.Context, tenantID uuid.UUID, eventType string) ([]WebhookSubscription, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, tenant_id, name, url, secret, event_types, is_active, created_at, updated_at
		 FROM webhook_subscriptions
		 WHERE tenant_id = $1 AND is_active = TRUE
		 AND (event_types @> ARRAY[$2]::text[] OR event_types @> ARRAY['*']::text[])`,
		tenantID, eventType,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get active subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []WebhookSubscription
	for rows.Next() {
		var sub WebhookSubscription
		if err := rows.Scan(&sub.ID, &sub.TenantID, &sub.Name, &sub.URL, &sub.Secret,
			pq.Array(&sub.EventTypes), &sub.IsActive, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan webhook subscription: %w", err)
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

// CreateDelivery creates a new delivery record.
func (r *WebhookRepository) CreateDelivery(ctx context.Context, delivery *WebhookDelivery) error {
	if delivery.ID == uuid.Nil {
		delivery.ID = uuid.New()
	}
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO webhook_deliveries (id, subscription_id, event_id, status, next_retry_at)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING created_at, updated_at`,
		delivery.ID, delivery.SubscriptionID, delivery.EventID, delivery.Status, delivery.NextRetryAt,
	).Scan(&delivery.CreatedAt, &delivery.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create webhook delivery: %w", err)
	}
	return nil
}

// UpdateDelivery updates a delivery record.
func (r *WebhookRepository) UpdateDelivery(ctx context.Context, delivery *WebhookDelivery) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE webhook_deliveries
		 SET status = $2, http_status = $3, attempt_count = $4, next_retry_at = $5, last_error = $6
		 WHERE id = $1`,
		delivery.ID, delivery.Status, delivery.HTTPStatus, delivery.AttemptCount,
		delivery.NextRetryAt, delivery.LastError,
	)
	return err
}

// GetPendingDeliveries retrieves deliveries that need to be attempted.
func (r *WebhookRepository) GetPendingDeliveries(ctx context.Context, limit int) ([]WebhookDelivery, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT d.id, d.subscription_id, d.event_id, d.status, d.http_status,
		        d.attempt_count, d.next_retry_at, d.last_error, d.created_at, d.updated_at
		 FROM webhook_deliveries d
		 WHERE d.status IN ('pending', 'failed')
		 AND (d.next_retry_at IS NULL OR d.next_retry_at <= NOW())
		 ORDER BY d.created_at ASC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending deliveries: %w", err)
	}
	defer rows.Close()

	var deliveries []WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		if err := rows.Scan(&d.ID, &d.SubscriptionID, &d.EventID, &d.Status, &d.HTTPStatus,
			&d.AttemptCount, &d.NextRetryAt, &d.LastError, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan webhook delivery: %w", err)
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, rows.Err()
}

// ListDeliveriesBySubscription returns deliveries for a subscription (tenant-scoped via RLS).
func (r *WebhookRepository) ListDeliveriesBySubscription(ctx context.Context, subscriptionID uuid.UUID, limit, offset int) ([]WebhookDelivery, int, error) {
	var total int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM webhook_deliveries WHERE subscription_id = $1`,
		subscriptionID,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count deliveries: %w", err)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, subscription_id, event_id, status, http_status,
		        attempt_count, next_retry_at, last_error, created_at, updated_at
		 FROM webhook_deliveries
		 WHERE subscription_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`,
		subscriptionID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list deliveries: %w", err)
	}
	defer rows.Close()

	var deliveries []WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		if err := rows.Scan(&d.ID, &d.SubscriptionID, &d.EventID, &d.Status, &d.HTTPStatus,
			&d.AttemptCount, &d.NextRetryAt, &d.LastError, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan webhook delivery: %w", err)
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, total, rows.Err()
}
