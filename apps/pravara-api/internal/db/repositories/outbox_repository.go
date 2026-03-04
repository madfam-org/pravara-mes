package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// OutboxEvent represents an event stored in the outbox.
type OutboxEvent struct {
	ID               uuid.UUID       `json:"id"`
	TenantID         uuid.UUID       `json:"tenant_id"`
	EventType        string          `json:"event_type"`
	ChannelNamespace string          `json:"channel_namespace"`
	Payload          json.RawMessage `json:"payload"`
	Delivered        bool            `json:"delivered"`
	CreatedAt        time.Time       `json:"created_at"`
}

// OutboxEventFilter is used to filter event queries.
type OutboxEventFilter struct {
	EventType *string
	TypesGlob *string
	EntityID  *uuid.UUID
	Since     *time.Time
	Until     *time.Time
	Limit     int
	Offset    int
}

// OutboxRepository handles event outbox database operations.
type OutboxRepository struct {
	db *sql.DB
}

// NewOutboxRepository creates a new outbox repository.
func NewOutboxRepository(db *sql.DB) *OutboxRepository {
	return &OutboxRepository{db: db}
}

// InsertEvent inserts an event into the outbox.
func (r *OutboxRepository) InsertEvent(ctx context.Context, tenantID uuid.UUID, eventType, channelNamespace string, payload json.RawMessage) (*OutboxEvent, error) {
	event := &OutboxEvent{
		ID:               uuid.New(),
		TenantID:         tenantID,
		EventType:        eventType,
		ChannelNamespace: channelNamespace,
		Payload:          payload,
	}

	err := r.db.QueryRowContext(ctx,
		`INSERT INTO event_outbox (id, tenant_id, event_type, channel_namespace, payload)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING created_at`,
		event.ID, event.TenantID, event.EventType, event.ChannelNamespace, event.Payload,
	).Scan(&event.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to insert outbox event: %w", err)
	}

	return event, nil
}

// GetPendingEvents retrieves undelivered events. This bypasses RLS (called by system dispatcher).
func (r *OutboxRepository) GetPendingEvents(ctx context.Context, limit int) ([]OutboxEvent, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, tenant_id, event_type, channel_namespace, payload, delivered, created_at
		 FROM event_outbox
		 WHERE delivered = FALSE
		 ORDER BY created_at ASC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending events: %w", err)
	}
	defer rows.Close()

	return scanOutboxEvents(rows)
}

// MarkDelivered marks an event as delivered.
func (r *OutboxRepository) MarkDelivered(ctx context.Context, eventID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE event_outbox SET delivered = TRUE WHERE id = $1`,
		eventID,
	)
	return err
}

// ListEvents retrieves events with filtering (tenant-scoped via RLS).
func (r *OutboxRepository) ListEvents(ctx context.Context, filter OutboxEventFilter) ([]OutboxEvent, int, error) {
	query := `SELECT id, tenant_id, event_type, channel_namespace, payload, delivered, created_at
		 FROM event_outbox WHERE 1=1`
	countQuery := `SELECT COUNT(*) FROM event_outbox WHERE 1=1`
	args := []interface{}{}
	argIdx := 1

	if filter.EventType != nil {
		clause := fmt.Sprintf(" AND event_type = $%d", argIdx)
		query += clause
		countQuery += clause
		args = append(args, *filter.EventType)
		argIdx++
	}

	if filter.TypesGlob != nil {
		clause := fmt.Sprintf(" AND event_type LIKE $%d", argIdx)
		query += clause
		countQuery += clause
		// Convert glob pattern: order.* -> order.%
		glob := *filter.TypesGlob
		glob = replaceStar(glob)
		args = append(args, glob)
		argIdx++
	}

	if filter.Since != nil {
		clause := fmt.Sprintf(" AND created_at >= $%d", argIdx)
		query += clause
		countQuery += clause
		args = append(args, *filter.Since)
		argIdx++
	}

	if filter.Until != nil {
		clause := fmt.Sprintf(" AND created_at <= $%d", argIdx)
		query += clause
		countQuery += clause
		args = append(args, *filter.Until)
		argIdx++
	}

	// Count total
	var total int
	err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count events: %w", err)
	}

	// Add ordering and pagination
	query += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, filter.Limit)
		argIdx++
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIdx)
		args = append(args, filter.Offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list events: %w", err)
	}
	defer rows.Close()

	events, err := scanOutboxEvents(rows)
	if err != nil {
		return nil, 0, err
	}

	return events, total, nil
}

// GetEventByID retrieves a single event by ID (tenant-scoped via RLS).
func (r *OutboxRepository) GetEventByID(ctx context.Context, id uuid.UUID) (*OutboxEvent, error) {
	var event OutboxEvent
	err := r.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, event_type, channel_namespace, payload, delivered, created_at
		 FROM event_outbox WHERE id = $1`,
		id,
	).Scan(&event.ID, &event.TenantID, &event.EventType, &event.ChannelNamespace,
		&event.Payload, &event.Delivered, &event.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get event: %w", err)
	}
	return &event, nil
}

// GetEventTypes returns distinct event types with counts (tenant-scoped via RLS).
func (r *OutboxRepository) GetEventTypes(ctx context.Context) ([]EventTypeCount, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT event_type, COUNT(*) as count
		 FROM event_outbox
		 GROUP BY event_type
		 ORDER BY count DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get event types: %w", err)
	}
	defer rows.Close()

	var result []EventTypeCount
	for rows.Next() {
		var etc EventTypeCount
		if err := rows.Scan(&etc.EventType, &etc.Count); err != nil {
			return nil, fmt.Errorf("failed to scan event type count: %w", err)
		}
		result = append(result, etc)
	}
	return result, rows.Err()
}

// PurgeOldEvents deletes events older than the specified number of days.
func (r *OutboxRepository) PurgeOldEvents(ctx context.Context, olderThanDays int) (int64, error) {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM event_outbox WHERE created_at < NOW() - INTERVAL '1 day' * $1 AND delivered = TRUE`,
		olderThanDays,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to purge old events: %w", err)
	}
	return result.RowsAffected()
}

// GetEventsByEntityFromPayload retrieves events that reference a specific entity in their payload.
func (r *OutboxRepository) GetEventsByEntityFromPayload(ctx context.Context, entityID uuid.UUID, limit, offset int) ([]OutboxEvent, int, error) {
	idStr := entityID.String()
	query := `SELECT id, tenant_id, event_type, channel_namespace, payload, delivered, created_at
		 FROM event_outbox
		 WHERE payload::text LIKE '%' || $1 || '%'
		 ORDER BY created_at DESC`
	countQuery := `SELECT COUNT(*) FROM event_outbox WHERE payload::text LIKE '%' || $1 || '%'`

	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, idStr).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count entity events: %w", err)
	}

	args := []interface{}{idStr}
	argIdx := 2
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, limit)
		argIdx++
	}
	if offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIdx)
		args = append(args, offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get entity events: %w", err)
	}
	defer rows.Close()

	events, err := scanOutboxEvents(rows)
	if err != nil {
		return nil, 0, err
	}
	return events, total, nil
}

// EventTypeCount holds a type and its count.
type EventTypeCount struct {
	EventType string `json:"event_type"`
	Count     int    `json:"count"`
}

func scanOutboxEvents(rows *sql.Rows) ([]OutboxEvent, error) {
	var events []OutboxEvent
	for rows.Next() {
		var e OutboxEvent
		if err := rows.Scan(&e.ID, &e.TenantID, &e.EventType, &e.ChannelNamespace,
			&e.Payload, &e.Delivered, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan outbox event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// replaceStar converts glob-style * to SQL LIKE %.
func replaceStar(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '*' {
			result = append(result, '%')
		} else {
			result = append(result, s[i])
		}
	}
	return string(result)
}
