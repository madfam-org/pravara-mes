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

func TestOutboxRepository_InsertEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewOutboxRepository(db)

	tests := []struct {
		name             string
		tenantID         uuid.UUID
		eventType        string
		channelNamespace string
		payload          json.RawMessage
		mockSetup        func(sqlmock.Sqlmock)
		wantError        bool
	}{
		{
			name:             "insert event successfully",
			tenantID:         uuid.New(),
			eventType:        "order.created",
			channelNamespace: "orders",
			payload:          json.RawMessage(`{"order_id":"abc"}`),
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("INSERT INTO event_outbox").
					WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))
			},
			wantError: false,
		},
		{
			name:             "insert event with empty payload",
			tenantID:         uuid.New(),
			eventType:        "machine.heartbeat",
			channelNamespace: "machines",
			payload:          json.RawMessage(`{}`),
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("INSERT INTO event_outbox").
					WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))
			},
			wantError: false,
		},
		{
			name:             "insert event database error",
			tenantID:         uuid.New(),
			eventType:        "task.completed",
			channelNamespace: "tasks",
			payload:          json.RawMessage(`{"task_id":"xyz"}`),
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("INSERT INTO event_outbox").
					WillReturnError(sql.ErrConnDone)
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			event, err := repo.InsertEvent(context.Background(), tt.tenantID, tt.eventType, tt.channelNamespace, tt.payload)

			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, event)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, event)
				assert.Equal(t, tt.tenantID, event.TenantID)
				assert.Equal(t, tt.eventType, event.EventType)
				assert.Equal(t, tt.channelNamespace, event.ChannelNamespace)
				assert.NotEqual(t, uuid.Nil, event.ID)
				assert.False(t, event.CreatedAt.IsZero())
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestOutboxRepository_GetPendingEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewOutboxRepository(db)

	tests := []struct {
		name      string
		limit     int
		mockSetup func(sqlmock.Sqlmock)
		wantCount int
		wantError bool
	}{
		{
			name:  "returns undelivered events",
			limit: 10,
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
				}).
					AddRow(uuid.New(), uuid.New(), "order.created", "orders", json.RawMessage(`{}`), false, time.Now()).
					AddRow(uuid.New(), uuid.New(), "task.completed", "tasks", json.RawMessage(`{}`), false, time.Now())

				mock.ExpectQuery("SELECT id, tenant_id.*FROM event_outbox.*WHERE delivered = FALSE").
					WithArgs(10).
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantError: false,
		},
		{
			name:  "returns empty when no pending events",
			limit: 5,
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
				})
				mock.ExpectQuery("SELECT id, tenant_id.*FROM event_outbox.*WHERE delivered = FALSE").
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
				mock.ExpectQuery("SELECT id, tenant_id.*FROM event_outbox.*WHERE delivered = FALSE").
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

			events, err := repo.GetPendingEvents(context.Background(), tt.limit)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, events, tt.wantCount)
				for _, e := range events {
					assert.False(t, e.Delivered)
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestOutboxRepository_MarkDelivered(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewOutboxRepository(db)

	tests := []struct {
		name      string
		eventID   uuid.UUID
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantError bool
	}{
		{
			name:    "mark delivered successfully",
			eventID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE event_outbox SET delivered = TRUE WHERE id").
					WithArgs(id).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:    "mark delivered database error",
			eventID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE event_outbox SET delivered = TRUE WHERE id").
					WithArgs(id).
					WillReturnError(sql.ErrConnDone)
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.eventID)

			err := repo.MarkDelivered(context.Background(), tt.eventID)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestOutboxRepository_ListEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewOutboxRepository(db)

	tests := []struct {
		name      string
		filter    OutboxEventFilter
		mockSetup func(sqlmock.Sqlmock)
		wantCount int
		wantTotal int
		wantError bool
	}{
		{
			name:   "list all events with no filter",
			filter: OutboxEventFilter{Limit: 10, Offset: 0},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
				}).
					AddRow(uuid.New(), uuid.New(), "order.created", "orders", json.RawMessage(`{}`), false, time.Now()).
					AddRow(uuid.New(), uuid.New(), "task.completed", "tasks", json.RawMessage(`{}`), true, time.Now())

				mock.ExpectQuery("SELECT id, tenant_id.*FROM event_outbox").
					WithArgs(10).
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantTotal: 2,
			wantError: false,
		},
		{
			name: "filter by event type",
			filter: OutboxEventFilter{
				EventType: func() *string { s := "order.created"; return &s }(),
				Limit:     10,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*AND event_type").
					WithArgs("order.created").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
				}).
					AddRow(uuid.New(), uuid.New(), "order.created", "orders", json.RawMessage(`{}`), false, time.Now())

				mock.ExpectQuery("SELECT id, tenant_id.*AND event_type").
					WithArgs("order.created", 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantTotal: 1,
			wantError: false,
		},
		{
			name: "filter by types glob",
			filter: OutboxEventFilter{
				TypesGlob: func() *string { s := "order.*"; return &s }(),
				Limit:     10,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*AND event_type LIKE").
					WithArgs("order.%").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
				}).
					AddRow(uuid.New(), uuid.New(), "order.created", "orders", json.RawMessage(`{}`), false, time.Now()).
					AddRow(uuid.New(), uuid.New(), "order.updated", "orders", json.RawMessage(`{}`), false, time.Now()).
					AddRow(uuid.New(), uuid.New(), "order.deleted", "orders", json.RawMessage(`{}`), true, time.Now())

				mock.ExpectQuery("SELECT id, tenant_id.*AND event_type LIKE").
					WithArgs("order.%", 10).
					WillReturnRows(rows)
			},
			wantCount: 3,
			wantTotal: 3,
			wantError: false,
		},
		{
			name: "filter by time range",
			filter: OutboxEventFilter{
				Since: func() *time.Time { t := time.Now().Add(-24 * time.Hour); return &t }(),
				Until: func() *time.Time { t := time.Now(); return &t }(),
				Limit: 10,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*AND created_at >=.*AND created_at <=").
					WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
				}).
					AddRow(uuid.New(), uuid.New(), "task.completed", "tasks", json.RawMessage(`{}`), false, time.Now())

				mock.ExpectQuery("SELECT id, tenant_id.*AND created_at >=.*AND created_at <=").
					WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantTotal: 1,
			wantError: false,
		},
		{
			name:   "count query error",
			filter: OutboxEventFilter{Limit: 10},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT").
					WillReturnError(sql.ErrConnDone)
			},
			wantCount: 0,
			wantTotal: 0,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			events, total, err := repo.ListEvents(context.Background(), tt.filter)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, events, tt.wantCount)
				assert.Equal(t, tt.wantTotal, total)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestOutboxRepository_GetEventByID(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewOutboxRepository(db)

	tests := []struct {
		name      string
		eventID   uuid.UUID
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantEvent bool
		wantError bool
	}{
		{
			name:    "event found",
			eventID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
				}).AddRow(id, uuid.New(), "order.created", "orders", json.RawMessage(`{}`), false, time.Now())

				mock.ExpectQuery("SELECT id, tenant_id.*FROM event_outbox WHERE id").
					WithArgs(id).
					WillReturnRows(rows)
			},
			wantEvent: true,
			wantError: false,
		},
		{
			name:    "event not found",
			eventID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectQuery("SELECT id, tenant_id.*FROM event_outbox WHERE id").
					WithArgs(id).
					WillReturnError(sql.ErrNoRows)
			},
			wantEvent: false,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.eventID)

			event, err := repo.GetEventByID(context.Background(), tt.eventID)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.wantEvent {
					require.NotNil(t, event)
					assert.Equal(t, tt.eventID, event.ID)
				} else {
					assert.Nil(t, event)
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestOutboxRepository_PurgeOldEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewOutboxRepository(db)

	mock.ExpectExec("DELETE FROM event_outbox WHERE created_at").
		WithArgs(30).
		WillReturnResult(sqlmock.NewResult(0, 5))

	count, err := repo.PurgeOldEvents(context.Background(), 30)

	assert.NoError(t, err)
	assert.Equal(t, int64(5), count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestReplaceStar(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single star at end",
			input:    "order.*",
			expected: "order.%",
		},
		{
			name:     "multiple stars",
			input:    "*.*",
			expected: "%.%",
		},
		{
			name:     "no stars",
			input:    "order.created",
			expected: "order.created",
		},
		{
			name:     "star only",
			input:    "*",
			expected: "%",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "star at beginning",
			input:    "*.completed",
			expected: "%.completed",
		},
		{
			name:     "consecutive stars",
			input:    "**",
			expected: "%%",
		},
		{
			name:     "star in middle",
			input:    "order.*.completed",
			expected: "order.%.completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceStar(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewOutboxRepository(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewOutboxRepository(db)

	assert.NotNil(t, repo)
}

func TestOutboxEvent_Structure(t *testing.T) {
	id := uuid.New()
	tenantID := uuid.New()
	now := time.Now()

	event := OutboxEvent{
		ID:               id,
		TenantID:         tenantID,
		EventType:        "machine.status_changed",
		ChannelNamespace: "machines",
		Payload:          json.RawMessage(`{"machine_id":"abc"}`),
		Delivered:        false,
		CreatedAt:        now,
	}

	assert.Equal(t, id, event.ID)
	assert.Equal(t, tenantID, event.TenantID)
	assert.Equal(t, "machine.status_changed", event.EventType)
	assert.Equal(t, "machines", event.ChannelNamespace)
	assert.False(t, event.Delivered)
	assert.Equal(t, now, event.CreatedAt)
}

func TestOutboxEventFilter_Structure(t *testing.T) {
	eventType := "order.created"
	glob := "order.*"
	entityID := uuid.New()
	since := time.Now().Add(-1 * time.Hour)
	until := time.Now()

	filter := OutboxEventFilter{
		EventType: &eventType,
		TypesGlob: &glob,
		EntityID:  &entityID,
		Since:     &since,
		Until:     &until,
		Limit:     50,
		Offset:    10,
	}

	assert.Equal(t, &eventType, filter.EventType)
	assert.Equal(t, &glob, filter.TypesGlob)
	assert.Equal(t, &entityID, filter.EntityID)
	assert.Equal(t, 50, filter.Limit)
	assert.Equal(t, 10, filter.Offset)
}

func TestEventTypeCount_Structure(t *testing.T) {
	etc := EventTypeCount{
		EventType: "order.created",
		Count:     42,
	}

	assert.Equal(t, "order.created", etc.EventType)
	assert.Equal(t, 42, etc.Count)
}
