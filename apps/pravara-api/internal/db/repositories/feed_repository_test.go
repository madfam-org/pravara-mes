package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeedRepository_GetSocialStats(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewFeedRepository(db)

	tests := []struct {
		name      string
		mockSetup func(sqlmock.Sqlmock)
		wantStats *SocialStats
		wantError bool
	}{
		{
			name: "returns stats successfully",
			mockSetup: func(mock sqlmock.Sqlmock) {
				// Machines running query
				mock.ExpectQuery("SELECT COUNT.*FROM machines WHERE status = 'running'").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

				// Orders completed query
				mock.ExpectQuery("SELECT.*COUNT.*FILTER.*FROM orders WHERE status IN").
					WillReturnRows(sqlmock.NewRows([]string{"day", "week", "month"}).AddRow(3, 15, 42))

				// Average OEE query
				mock.ExpectQuery("SELECT AVG.*FROM oee_snapshots").
					WillReturnRows(sqlmock.NewRows([]string{"avg"}).AddRow(85.5))
			},
			wantStats: &SocialStats{
				MachinesRunning:      5,
				OrdersCompletedDay:   3,
				OrdersCompletedWeek:  15,
				OrdersCompletedMonth: 42,
				AverageOEE:           85.5,
			},
			wantError: false,
		},
		{
			name: "returns stats with zero OEE when no snapshots",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*FROM machines WHERE status = 'running'").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

				mock.ExpectQuery("SELECT.*COUNT.*FILTER.*FROM orders WHERE status IN").
					WillReturnRows(sqlmock.NewRows([]string{"day", "week", "month"}).AddRow(0, 0, 0))

				// No OEE data - NullFloat64 with Valid=false
				mock.ExpectQuery("SELECT AVG.*FROM oee_snapshots").
					WillReturnRows(sqlmock.NewRows([]string{"avg"}).AddRow(nil))
			},
			wantStats: &SocialStats{
				MachinesRunning:      0,
				OrdersCompletedDay:   0,
				OrdersCompletedWeek:  0,
				OrdersCompletedMonth: 0,
				AverageOEE:           0,
			},
			wantError: false,
		},
		{
			name: "machines query error",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*FROM machines WHERE status = 'running'").
					WillReturnError(sql.ErrConnDone)
			},
			wantStats: nil,
			wantError: true,
		},
		{
			name: "orders query error",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*FROM machines WHERE status = 'running'").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

				mock.ExpectQuery("SELECT.*COUNT.*FILTER.*FROM orders WHERE status IN").
					WillReturnError(sql.ErrConnDone)
			},
			wantStats: nil,
			wantError: true,
		},
		{
			name: "OEE query error (non ErrNoRows)",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*FROM machines WHERE status = 'running'").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

				mock.ExpectQuery("SELECT.*COUNT.*FILTER.*FROM orders WHERE status IN").
					WillReturnRows(sqlmock.NewRows([]string{"day", "week", "month"}).AddRow(1, 5, 20))

				mock.ExpectQuery("SELECT AVG.*FROM oee_snapshots").
					WillReturnError(sql.ErrConnDone)
			},
			wantStats: nil,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			stats, err := repo.GetSocialStats(context.Background())

			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, stats)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, stats)
				assert.Equal(t, tt.wantStats.MachinesRunning, stats.MachinesRunning)
				assert.Equal(t, tt.wantStats.OrdersCompletedDay, stats.OrdersCompletedDay)
				assert.Equal(t, tt.wantStats.OrdersCompletedWeek, stats.OrdersCompletedWeek)
				assert.Equal(t, tt.wantStats.OrdersCompletedMonth, stats.OrdersCompletedMonth)
				assert.InDelta(t, tt.wantStats.AverageOEE, stats.AverageOEE, 0.01)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestFeedRepository_GetCRMOrderStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewFeedRepository(db)

	tests := []struct {
		name       string
		orderID    uuid.UUID
		mockSetup  func(sqlmock.Sqlmock, uuid.UUID)
		wantStatus bool
		wantError  bool
	}{
		{
			name:    "order status found",
			orderID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				rows := sqlmock.NewRows([]string{
					"id", "status", "total_tasks", "completed_tasks", "progress_percent", "updated_at",
				}).AddRow(id, "in_production", 10, 6, 60.0, time.Now())

				mock.ExpectQuery("SELECT.*FROM orders o.*WHERE o.id").
					WithArgs(id).
					WillReturnRows(rows)
			},
			wantStatus: true,
			wantError:  false,
		},
		{
			name:    "order not found",
			orderID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectQuery("SELECT.*FROM orders o.*WHERE o.id").
					WithArgs(id).
					WillReturnError(sql.ErrNoRows)
			},
			wantStatus: false,
			wantError:  false,
		},
		{
			name:    "database error",
			orderID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectQuery("SELECT.*FROM orders o.*WHERE o.id").
					WithArgs(id).
					WillReturnError(sql.ErrConnDone)
			},
			wantStatus: false,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.orderID)

			status, err := repo.GetCRMOrderStatus(context.Background(), tt.orderID)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.wantStatus {
					require.NotNil(t, status)
					assert.Equal(t, tt.orderID, status.ID)
					assert.Equal(t, "in_production", status.Status)
					assert.Equal(t, 10, status.TotalTasks)
					assert.Equal(t, 6, status.CompletedTasks)
					assert.InDelta(t, 60.0, status.ProgressPercent, 0.01)
				} else {
					assert.Nil(t, status)
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestFeedRepository_GetSocialMilestones(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewFeedRepository(db)

	tests := []struct {
		name      string
		limit     int
		mockSetup func(sqlmock.Sqlmock)
		wantCount int
		wantError bool
	}{
		{
			name:  "returns milestones",
			limit: 10,
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"event_type", "payload", "created_at"}).
					AddRow("task.completed", json.RawMessage(`{"task_id":"abc"}`), time.Now()).
					AddRow("order.status_changed", json.RawMessage(`{"order_id":"def"}`), time.Now())

				mock.ExpectQuery("SELECT event_type, payload, created_at.*FROM event_outbox").
					WithArgs(10).
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantError: false,
		},
		{
			name:  "empty milestones",
			limit: 5,
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"event_type", "payload", "created_at"})
				mock.ExpectQuery("SELECT event_type, payload, created_at.*FROM event_outbox").
					WithArgs(5).
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantError: false,
		},
		{
			name:  "database error",
			limit: 10,
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT event_type, payload, created_at.*FROM event_outbox").
					WithArgs(10).
					WillReturnError(sql.ErrConnDone)
			},
			wantCount: 0,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			milestones, err := repo.GetSocialMilestones(context.Background(), tt.limit)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, milestones, tt.wantCount)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestNewFeedRepository(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewFeedRepository(db)
	assert.NotNil(t, repo)
}

func TestSocialStats_Structure(t *testing.T) {
	stats := SocialStats{
		MachinesRunning:      8,
		OrdersCompletedDay:   5,
		OrdersCompletedWeek:  23,
		OrdersCompletedMonth: 87,
		AverageOEE:           91.3,
	}

	assert.Equal(t, 8, stats.MachinesRunning)
	assert.Equal(t, 5, stats.OrdersCompletedDay)
	assert.Equal(t, 23, stats.OrdersCompletedWeek)
	assert.Equal(t, 87, stats.OrdersCompletedMonth)
	assert.InDelta(t, 91.3, stats.AverageOEE, 0.01)
}

func TestCRMOrder_Structure(t *testing.T) {
	id := uuid.New()
	now := time.Now()
	externalID := "ORD-2026-001"
	email := "customer@example.com"
	dueDate := now.Add(7 * 24 * time.Hour)
	amount := 1500.50
	currency := "USD"

	order := CRMOrder{
		ID:              id,
		ExternalID:      &externalID,
		CustomerName:    "Acme Corp",
		CustomerEmail:   &email,
		Status:          "in_production",
		Priority:        3,
		DueDate:         &dueDate,
		TotalAmount:     &amount,
		Currency:        &currency,
		TotalTasks:      10,
		CompletedTasks:  6,
		ProgressPercent: 60.0,
		LastUpdatedAt:   now,
		CreatedAt:       now,
	}

	assert.Equal(t, id, order.ID)
	assert.Equal(t, "Acme Corp", order.CustomerName)
	assert.Equal(t, "in_production", order.Status)
	assert.Equal(t, 3, order.Priority)
	assert.Equal(t, 10, order.TotalTasks)
	assert.Equal(t, 6, order.CompletedTasks)
	assert.InDelta(t, 60.0, order.ProgressPercent, 0.01)
	assert.NotNil(t, order.ExternalID)
	assert.NotNil(t, order.CustomerEmail)
	assert.NotNil(t, order.DueDate)
	assert.NotNil(t, order.TotalAmount)
	assert.NotNil(t, order.Currency)
}

func TestCRMOrderStatus_Structure(t *testing.T) {
	id := uuid.New()
	now := time.Now()

	status := CRMOrderStatus{
		ID:              id,
		Status:          "shipped",
		TotalTasks:      5,
		CompletedTasks:  5,
		ProgressPercent: 100.0,
		LastUpdatedAt:   now,
	}

	assert.Equal(t, id, status.ID)
	assert.Equal(t, "shipped", status.Status)
	assert.Equal(t, 5, status.TotalTasks)
	assert.Equal(t, 5, status.CompletedTasks)
	assert.InDelta(t, 100.0, status.ProgressPercent, 0.01)
}

func TestSocialMilestone_Structure(t *testing.T) {
	now := time.Now()

	milestone := SocialMilestone{
		Type:        "task.completed",
		Title:       "Task Completed",
		Description: "A production task was completed",
		Data:        json.RawMessage(`{"task_id":"abc"}`),
		OccurredAt:  now,
	}

	assert.Equal(t, "task.completed", milestone.Type)
	assert.Equal(t, "Task Completed", milestone.Title)
	assert.NotEmpty(t, milestone.Description)
	assert.Equal(t, now, milestone.OccurredAt)
}

func TestSocialHighlight_Structure(t *testing.T) {
	now := time.Now()
	metric := 95.2

	highlight := SocialHighlight{
		Type:        "analytics.oee_updated",
		Title:       "OEE Update",
		Description: "Machine efficiency metrics updated",
		Metric:      &metric,
		Data:        json.RawMessage(`{"machine_id":"xyz"}`),
		OccurredAt:  now,
	}

	assert.Equal(t, "analytics.oee_updated", highlight.Type)
	assert.Equal(t, "OEE Update", highlight.Title)
	assert.NotNil(t, highlight.Metric)
	assert.InDelta(t, 95.2, *highlight.Metric, 0.01)
}

func TestFormatMilestoneTitle(t *testing.T) {
	tests := []struct {
		eventType string
		expected  string
	}{
		{"task.completed", "Task Completed"},
		{"order.status_changed", "Order Status Update"},
		{"genealogy.sealed", "Genealogy Sealed"},
		{"product.imported_from_yantra4d", "Product Imported"},
		{"unknown.event", "unknown.event"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			result := formatMilestoneTitle(tt.eventType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatHighlightTitle(t *testing.T) {
	tests := []struct {
		eventType string
		expected  string
	}{
		{"analytics.oee_updated", "OEE Update"},
		{"order.status_changed", "Order Milestone"},
		{"genealogy.sealed", "Product Genealogy Sealed"},
		{"task.completed", "Production Task Complete"},
		{"unknown.event", "unknown.event"},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			result := formatHighlightTitle(tt.eventType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatHighlightDescription(t *testing.T) {
	tests := []struct {
		eventType string
		expected  string
	}{
		{"analytics.oee_updated", "Machine efficiency metrics updated"},
		{"order.status_changed", "An order reached a new milestone"},
		{"genealogy.sealed", "Complete product traceability record sealed"},
		{"task.completed", "A production task was completed"},
		{"unknown.event", ""},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			result := formatHighlightDescription(tt.eventType)
			assert.Equal(t, tt.expected, result)
		})
	}
}
