package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CRMOrder represents an order with task progress for CRM consumers.
type CRMOrder struct {
	ID              uuid.UUID  `json:"id"`
	ExternalID      *string    `json:"external_id,omitempty"`
	CustomerName    string     `json:"customer_name"`
	CustomerEmail   *string    `json:"customer_email,omitempty"`
	Status          string     `json:"status"`
	Priority        int        `json:"priority"`
	DueDate         *time.Time `json:"due_date,omitempty"`
	TotalAmount     *float64   `json:"total_amount,omitempty"`
	Currency        *string    `json:"currency,omitempty"`
	TotalTasks      int        `json:"total_tasks"`
	CompletedTasks  int        `json:"completed_tasks"`
	ProgressPercent float64    `json:"progress_percent"`
	LastUpdatedAt   time.Time  `json:"last_updated_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

// CRMOrderStatus represents a lightweight order status.
type CRMOrderStatus struct {
	ID              uuid.UUID `json:"id"`
	Status          string    `json:"status"`
	TotalTasks      int       `json:"total_tasks"`
	CompletedTasks  int       `json:"completed_tasks"`
	ProgressPercent float64   `json:"progress_percent"`
	LastUpdatedAt   time.Time `json:"last_updated_at"`
}

// SocialMilestone represents a production milestone for social media.
type SocialMilestone struct {
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Data        json.RawMessage `json:"data,omitempty"`
	OccurredAt  time.Time       `json:"occurred_at"`
}

// SocialStats represents production statistics for social media.
type SocialStats struct {
	MachinesRunning      int     `json:"machines_running"`
	OrdersCompletedDay   int     `json:"orders_completed_today"`
	OrdersCompletedWeek  int     `json:"orders_completed_this_week"`
	OrdersCompletedMonth int     `json:"orders_completed_this_month"`
	AverageOEE           float64 `json:"average_oee"`
}

// SocialHighlight represents a curated interesting moment.
type SocialHighlight struct {
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Metric      *float64        `json:"metric,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
	OccurredAt  time.Time       `json:"occurred_at"`
}

// FeedRepository handles optimized feed aggregate queries.
type FeedRepository struct {
	db *sql.DB
}

// NewFeedRepository creates a new feed repository.
func NewFeedRepository(db *sql.DB) *FeedRepository {
	return &FeedRepository{db: db}
}

// GetCRMOrders returns active orders with task progress percentage.
func (r *FeedRepository) GetCRMOrders(ctx context.Context, limit, offset int) ([]CRMOrder, int, error) {
	var total int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orders WHERE status NOT IN ('cancelled', 'delivered')`,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count CRM orders: %w", err)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT
			o.id, o.external_id, o.customer_name, o.customer_email,
			o.status, o.priority, o.due_date, o.total_amount, o.currency,
			COALESCE(t.total_tasks, 0) as total_tasks,
			COALESCE(t.completed_tasks, 0) as completed_tasks,
			CASE WHEN COALESCE(t.total_tasks, 0) > 0
				THEN ROUND(COALESCE(t.completed_tasks, 0)::numeric / t.total_tasks * 100, 1)
				ELSE 0
			END as progress_percent,
			o.updated_at as last_updated_at,
			o.created_at
		FROM orders o
		LEFT JOIN (
			SELECT order_id,
				COUNT(*) as total_tasks,
				COUNT(*) FILTER (WHERE status = 'completed') as completed_tasks
			FROM tasks
			GROUP BY order_id
		) t ON t.order_id = o.id
		WHERE o.status NOT IN ('cancelled', 'delivered')
		ORDER BY o.priority DESC, o.due_date ASC NULLS LAST
		LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get CRM orders: %w", err)
	}
	defer rows.Close()

	var orders []CRMOrder
	for rows.Next() {
		var o CRMOrder
		var externalID, customerEmail, currency sql.NullString
		var dueDate sql.NullTime
		var totalAmount sql.NullFloat64

		if err := rows.Scan(
			&o.ID, &externalID, &o.CustomerName, &customerEmail,
			&o.Status, &o.Priority, &dueDate, &totalAmount, &currency,
			&o.TotalTasks, &o.CompletedTasks, &o.ProgressPercent,
			&o.LastUpdatedAt, &o.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan CRM order: %w", err)
		}

		if externalID.Valid {
			o.ExternalID = &externalID.String
		}
		if customerEmail.Valid {
			o.CustomerEmail = &customerEmail.String
		}
		if dueDate.Valid {
			o.DueDate = &dueDate.Time
		}
		if totalAmount.Valid {
			o.TotalAmount = &totalAmount.Float64
		}
		if currency.Valid {
			o.Currency = &currency.String
		}

		orders = append(orders, o)
	}

	return orders, total, rows.Err()
}

// GetCRMOrderStatus returns lightweight status for a single order.
func (r *FeedRepository) GetCRMOrderStatus(ctx context.Context, orderID uuid.UUID) (*CRMOrderStatus, error) {
	var s CRMOrderStatus
	err := r.db.QueryRowContext(ctx,
		`SELECT
			o.id, o.status,
			COALESCE(t.total_tasks, 0),
			COALESCE(t.completed_tasks, 0),
			CASE WHEN COALESCE(t.total_tasks, 0) > 0
				THEN ROUND(COALESCE(t.completed_tasks, 0)::numeric / t.total_tasks * 100, 1)
				ELSE 0
			END,
			o.updated_at
		FROM orders o
		LEFT JOIN (
			SELECT order_id,
				COUNT(*) as total_tasks,
				COUNT(*) FILTER (WHERE status = 'completed') as completed_tasks
			FROM tasks
			GROUP BY order_id
		) t ON t.order_id = o.id
		WHERE o.id = $1`,
		orderID,
	).Scan(&s.ID, &s.Status, &s.TotalTasks, &s.CompletedTasks, &s.ProgressPercent, &s.LastUpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get CRM order status: %w", err)
	}
	return &s, nil
}

// GetSocialMilestones returns recent production milestones.
func (r *FeedRepository) GetSocialMilestones(ctx context.Context, limit int) ([]SocialMilestone, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT event_type, payload, created_at
		FROM event_outbox
		WHERE event_type IN (
			'task.completed', 'order.status_changed',
			'genealogy.sealed', 'product.imported_from_yantra4d'
		)
		ORDER BY created_at DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get social milestones: %w", err)
	}
	defer rows.Close()

	var milestones []SocialMilestone
	for rows.Next() {
		var eventType string
		var payload json.RawMessage
		var occurredAt time.Time
		if err := rows.Scan(&eventType, &payload, &occurredAt); err != nil {
			return nil, fmt.Errorf("failed to scan milestone: %w", err)
		}

		milestone := SocialMilestone{
			Type:       eventType,
			Title:      formatMilestoneTitle(eventType),
			Data:       payload,
			OccurredAt: occurredAt,
		}
		milestones = append(milestones, milestone)
	}
	return milestones, rows.Err()
}

// GetSocialStats returns production statistics.
func (r *FeedRepository) GetSocialStats(ctx context.Context) (*SocialStats, error) {
	var stats SocialStats

	// Machines currently running
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM machines WHERE status = 'running'`,
	).Scan(&stats.MachinesRunning)
	if err != nil {
		return nil, fmt.Errorf("failed to count running machines: %w", err)
	}

	// Orders completed today/week/month
	err = r.db.QueryRowContext(ctx,
		`SELECT
			COUNT(*) FILTER (WHERE updated_at >= CURRENT_DATE),
			COUNT(*) FILTER (WHERE updated_at >= date_trunc('week', CURRENT_DATE)),
			COUNT(*) FILTER (WHERE updated_at >= date_trunc('month', CURRENT_DATE))
		FROM orders WHERE status IN ('delivered', 'shipped', 'ready')`,
	).Scan(&stats.OrdersCompletedDay, &stats.OrdersCompletedWeek, &stats.OrdersCompletedMonth)
	if err != nil {
		return nil, fmt.Errorf("failed to count completed orders: %w", err)
	}

	// Average OEE from latest snapshots
	var avgOEE sql.NullFloat64
	err = r.db.QueryRowContext(ctx,
		`SELECT AVG(oee) FROM oee_snapshots
		 WHERE snapshot_date = CURRENT_DATE`,
	).Scan(&avgOEE)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get average OEE: %w", err)
	}
	if avgOEE.Valid {
		stats.AverageOEE = avgOEE.Float64
	}

	return &stats, nil
}

// GetSocialHighlights returns curated interesting production moments.
func (r *FeedRepository) GetSocialHighlights(ctx context.Context, limit int) ([]SocialHighlight, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT event_type, payload, created_at
		FROM event_outbox
		WHERE event_type IN (
			'analytics.oee_updated', 'order.status_changed',
			'genealogy.sealed', 'task.completed'
		)
		ORDER BY created_at DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get social highlights: %w", err)
	}
	defer rows.Close()

	var highlights []SocialHighlight
	for rows.Next() {
		var eventType string
		var payload json.RawMessage
		var occurredAt time.Time
		if err := rows.Scan(&eventType, &payload, &occurredAt); err != nil {
			return nil, fmt.Errorf("failed to scan highlight: %w", err)
		}

		highlight := SocialHighlight{
			Type:        eventType,
			Title:       formatHighlightTitle(eventType),
			Description: formatHighlightDescription(eventType),
			Data:        payload,
			OccurredAt:  occurredAt,
		}
		highlights = append(highlights, highlight)
	}
	return highlights, rows.Err()
}

func formatMilestoneTitle(eventType string) string {
	switch eventType {
	case "task.completed":
		return "Task Completed"
	case "order.status_changed":
		return "Order Status Update"
	case "genealogy.sealed":
		return "Genealogy Sealed"
	case "product.imported_from_yantra4d":
		return "Product Imported"
	default:
		return eventType
	}
}

func formatHighlightTitle(eventType string) string {
	switch eventType {
	case "analytics.oee_updated":
		return "OEE Update"
	case "order.status_changed":
		return "Order Milestone"
	case "genealogy.sealed":
		return "Product Genealogy Sealed"
	case "task.completed":
		return "Production Task Complete"
	default:
		return eventType
	}
}

func formatHighlightDescription(eventType string) string {
	switch eventType {
	case "analytics.oee_updated":
		return "Machine efficiency metrics updated"
	case "order.status_changed":
		return "An order reached a new milestone"
	case "genealogy.sealed":
		return "Complete product traceability record sealed"
	case "task.completed":
		return "A production task was completed"
	default:
		return ""
	}
}
