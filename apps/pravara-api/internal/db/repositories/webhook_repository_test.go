package repositories

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookRepository_CreateSubscription(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)

	tests := []struct {
		name      string
		sub       *WebhookSubscription
		mockSetup func(sqlmock.Sqlmock)
		wantError bool
	}{
		{
			name: "create subscription successfully",
			sub: &WebhookSubscription{
				TenantID:   uuid.New(),
				Name:       "Order Events",
				URL:        "https://example.com/webhook",
				Secret:     "whsec_test123",
				EventTypes: []string{"order.created", "order.updated"},
				IsActive:   true,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())
				mock.ExpectQuery("INSERT INTO webhook_subscriptions").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create subscription with existing ID",
			sub: &WebhookSubscription{
				ID:         uuid.New(),
				TenantID:   uuid.New(),
				Name:       "Task Events",
				URL:        "https://example.com/tasks",
				EventTypes: []string{"task.*"},
				IsActive:   true,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())
				mock.ExpectQuery("INSERT INTO webhook_subscriptions").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create subscription with nil ID generates new ID",
			sub: &WebhookSubscription{
				ID:         uuid.Nil,
				TenantID:   uuid.New(),
				Name:       "All Events",
				URL:        "https://example.com/all",
				EventTypes: []string{"*"},
				IsActive:   true,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())
				mock.ExpectQuery("INSERT INTO webhook_subscriptions").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create subscription database error",
			sub: &WebhookSubscription{
				TenantID:   uuid.New(),
				Name:       "Failing",
				URL:        "https://example.com/fail",
				EventTypes: []string{"order.created"},
				IsActive:   true,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("INSERT INTO webhook_subscriptions").
					WillReturnError(sql.ErrConnDone)
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalID := tt.sub.ID
			tt.mockSetup(mock)

			err := repo.CreateSubscription(context.Background(), tt.sub)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if originalID == uuid.Nil {
					assert.NotEqual(t, uuid.Nil, tt.sub.ID, "ID should be generated when nil")
				}
				assert.False(t, tt.sub.CreatedAt.IsZero())
				assert.False(t, tt.sub.UpdatedAt.IsZero())
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestWebhookRepository_GetActiveSubscriptionsForEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)

	tests := []struct {
		name      string
		tenantID  uuid.UUID
		eventType string
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantCount int
		wantError bool
	}{
		{
			name:      "returns matching subscriptions",
			tenantID:  uuid.New(),
			eventType: "order.created",
			mockSetup: func(mock sqlmock.Sqlmock, tenantID uuid.UUID) {
				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "name", "url", "secret", "event_types", "is_active", "created_at", "updated_at",
				}).
					AddRow(uuid.New(), tenantID, "Order Hook", "https://example.com/hook1",
						"secret1", pq.Array([]string{"order.created", "order.updated"}), true, time.Now(), time.Now()).
					AddRow(uuid.New(), tenantID, "All Hook", "https://example.com/hook2",
						"secret2", pq.Array([]string{"*"}), true, time.Now(), time.Now())

				mock.ExpectQuery("SELECT id, tenant_id.*FROM webhook_subscriptions.*WHERE tenant_id.*AND is_active = TRUE").
					WithArgs(tenantID, "order.created").
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantError: false,
		},
		{
			name:      "returns empty when no matching subscriptions",
			tenantID:  uuid.New(),
			eventType: "machine.heartbeat",
			mockSetup: func(mock sqlmock.Sqlmock, tenantID uuid.UUID) {
				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "name", "url", "secret", "event_types", "is_active", "created_at", "updated_at",
				})
				mock.ExpectQuery("SELECT id, tenant_id.*FROM webhook_subscriptions.*WHERE tenant_id.*AND is_active = TRUE").
					WithArgs(tenantID, "machine.heartbeat").
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantError: false,
		},
		{
			name:      "database error",
			tenantID:  uuid.New(),
			eventType: "order.created",
			mockSetup: func(mock sqlmock.Sqlmock, tenantID uuid.UUID) {
				mock.ExpectQuery("SELECT id, tenant_id.*FROM webhook_subscriptions.*WHERE tenant_id.*AND is_active = TRUE").
					WithArgs(tenantID, "order.created").
					WillReturnError(sql.ErrConnDone)
			},
			wantCount: 0,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.tenantID)

			subs, err := repo.GetActiveSubscriptionsForEvent(context.Background(), tt.tenantID, tt.eventType)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, subs, tt.wantCount)
				for _, sub := range subs {
					assert.True(t, sub.IsActive)
					assert.Equal(t, tt.tenantID, sub.TenantID)
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestWebhookRepository_CreateDelivery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)

	tests := []struct {
		name      string
		delivery  *WebhookDelivery
		mockSetup func(sqlmock.Sqlmock)
		wantError bool
	}{
		{
			name: "create delivery successfully",
			delivery: &WebhookDelivery{
				SubscriptionID: uuid.New(),
				EventID:        uuid.New(),
				Status:         "pending",
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())
				mock.ExpectQuery("INSERT INTO webhook_deliveries").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create delivery with nil ID generates new ID",
			delivery: &WebhookDelivery{
				ID:             uuid.Nil,
				SubscriptionID: uuid.New(),
				EventID:        uuid.New(),
				Status:         "pending",
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())
				mock.ExpectQuery("INSERT INTO webhook_deliveries").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create delivery with next_retry_at",
			delivery: &WebhookDelivery{
				SubscriptionID: uuid.New(),
				EventID:        uuid.New(),
				Status:         "pending",
				NextRetryAt:    func() *time.Time { t := time.Now().Add(5 * time.Minute); return &t }(),
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())
				mock.ExpectQuery("INSERT INTO webhook_deliveries").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create delivery database error",
			delivery: &WebhookDelivery{
				SubscriptionID: uuid.New(),
				EventID:        uuid.New(),
				Status:         "pending",
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("INSERT INTO webhook_deliveries").
					WillReturnError(sql.ErrConnDone)
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalID := tt.delivery.ID
			tt.mockSetup(mock)

			err := repo.CreateDelivery(context.Background(), tt.delivery)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if originalID == uuid.Nil {
					assert.NotEqual(t, uuid.Nil, tt.delivery.ID, "ID should be generated when nil")
				}
				assert.False(t, tt.delivery.CreatedAt.IsZero())
				assert.False(t, tt.delivery.UpdatedAt.IsZero())
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestWebhookRepository_UpdateDelivery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)

	tests := []struct {
		name      string
		delivery  *WebhookDelivery
		mockSetup func(sqlmock.Sqlmock)
		wantError bool
	}{
		{
			name: "update delivery to delivered",
			delivery: &WebhookDelivery{
				ID:           uuid.New(),
				Status:       "delivered",
				HTTPStatus:   func() *int { s := 200; return &s }(),
				AttemptCount: 1,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("UPDATE webhook_deliveries").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name: "update delivery to failed with error",
			delivery: &WebhookDelivery{
				ID:           uuid.New(),
				Status:       "failed",
				HTTPStatus:   func() *int { s := 500; return &s }(),
				AttemptCount: 3,
				LastError:    func() *string { s := "internal server error"; return &s }(),
				NextRetryAt:  func() *time.Time { t := time.Now().Add(30 * time.Minute); return &t }(),
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("UPDATE webhook_deliveries").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name: "update delivery database error",
			delivery: &WebhookDelivery{
				ID:     uuid.New(),
				Status: "delivered",
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("UPDATE webhook_deliveries").
					WillReturnError(sql.ErrConnDone)
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			err := repo.UpdateDelivery(context.Background(), tt.delivery)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestWebhookRepository_GetSubscriptionByID(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)

	tests := []struct {
		name      string
		subID     uuid.UUID
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantSub   bool
		wantErr   bool
	}{
		{
			name:  "subscription found",
			subID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "name", "url", "secret", "event_types", "is_active", "created_at", "updated_at",
				}).AddRow(id, uuid.New(), "My Hook", "https://example.com/hook",
					"secret", pq.Array([]string{"order.created"}), true, time.Now(), time.Now())

				mock.ExpectQuery("SELECT id, tenant_id.*FROM webhook_subscriptions WHERE id").
					WithArgs(id).
					WillReturnRows(rows)
			},
			wantSub: true,
			wantErr: false,
		},
		{
			name:  "subscription not found",
			subID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectQuery("SELECT id, tenant_id.*FROM webhook_subscriptions WHERE id").
					WithArgs(id).
					WillReturnError(sql.ErrNoRows)
			},
			wantSub: false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.subID)

			sub, err := repo.GetSubscriptionByID(context.Background(), tt.subID)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.wantSub {
					require.NotNil(t, sub)
					assert.Equal(t, tt.subID, sub.ID)
				} else {
					assert.Nil(t, sub)
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestWebhookRepository_DeleteSubscription(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)

	tests := []struct {
		name      string
		subID     uuid.UUID
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantError bool
	}{
		{
			name:  "delete subscription successfully",
			subID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("DELETE FROM webhook_subscriptions WHERE id").
					WithArgs(id).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:  "delete subscription not found",
			subID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("DELETE FROM webhook_subscriptions WHERE id").
					WithArgs(id).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.subID)

			err := repo.DeleteSubscription(context.Background(), tt.subID)

			if tt.wantError {
				assert.Error(t, err)
				assert.Equal(t, ErrNotFound, err)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestNewWebhookRepository(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	assert.NotNil(t, repo)
}

func TestWebhookSubscription_Structure(t *testing.T) {
	id := uuid.New()
	tenantID := uuid.New()
	now := time.Now()

	sub := WebhookSubscription{
		ID:         id,
		TenantID:   tenantID,
		Name:       "Test Webhook",
		URL:        "https://example.com/webhook",
		Secret:     "whsec_test",
		EventTypes: []string{"order.created", "task.completed"},
		IsActive:   true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	assert.Equal(t, id, sub.ID)
	assert.Equal(t, tenantID, sub.TenantID)
	assert.Equal(t, "Test Webhook", sub.Name)
	assert.Equal(t, "https://example.com/webhook", sub.URL)
	assert.Len(t, sub.EventTypes, 2)
	assert.True(t, sub.IsActive)
}

func TestWebhookDelivery_Structure(t *testing.T) {
	id := uuid.New()
	subID := uuid.New()
	eventID := uuid.New()
	httpStatus := 200
	now := time.Now()

	delivery := WebhookDelivery{
		ID:             id,
		SubscriptionID: subID,
		EventID:        eventID,
		Status:         "delivered",
		HTTPStatus:     &httpStatus,
		AttemptCount:   1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	assert.Equal(t, id, delivery.ID)
	assert.Equal(t, subID, delivery.SubscriptionID)
	assert.Equal(t, eventID, delivery.EventID)
	assert.Equal(t, "delivered", delivery.Status)
	assert.Equal(t, 200, *delivery.HTTPStatus)
	assert.Equal(t, 1, delivery.AttemptCount)
	assert.Nil(t, delivery.NextRetryAt)
	assert.Nil(t, delivery.LastError)
}
