package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// TestWebhookDispatcher_PicksUpOutboxEvent proves the delivery half of the
// outbox fix: an undelivered event_outbox row (exactly what the
// outbox-enabled Publisher inserts) is picked up by the dispatcher, signed,
// POSTed to the subscription URL, recorded as a delivery, and marked
// delivered.
func TestWebhookDispatcher_PicksUpOutboxEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tenantID := uuid.New()
	eventID := uuid.New()
	subID := uuid.New()
	secret := "test-webhook-secret"
	payload := []byte(`{"type":"order.created","data":{"entity_id":"abc"}}`)

	// Receiving endpoint asserts payload + HMAC signature.
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, string(payload), string(body))

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := fmt.Sprintf("sha256=%x", mac.Sum(nil))
		assert.Equal(t, expected, r.Header.Get("X-Pravara-Signature"), "delivery must be HMAC-signed")
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		received <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	now := time.Now()

	// 1) dispatcher polls pending outbox events
	mock.ExpectQuery("FROM event_outbox").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
		}).AddRow(eventID, tenantID, "order.created", "orders", payload, false, now))

	// 2) matching active subscription for the event type
	mock.ExpectQuery("FROM webhook_subscriptions").
		WithArgs(tenantID, "order.created").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "name", "url", "secret", "event_types", "is_active", "created_at", "updated_at",
		}).AddRow(subID, tenantID, "crm-sync", server.URL, secret, "{order.created}", true, now, now))

	// 3) delivery record is created
	mock.ExpectQuery("INSERT INTO webhook_deliveries").
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

	// 4) delivery is updated after the successful POST
	mock.ExpectExec("UPDATE webhook_deliveries").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// 5) the outbox event is marked delivered
	mock.ExpectExec("UPDATE event_outbox SET delivered = TRUE").
		WillReturnResult(sqlmock.NewResult(0, 1))

	dispatcher := NewWebhookDispatcher(
		repositories.NewOutboxRepository(db),
		repositories.NewWebhookRepository(db),
		config.WebhooksConfig{DispatchInterval: 1, MaxRetries: 3},
		quietLog(),
	)

	dispatcher.dispatchPendingEvents(context.Background())

	select {
	case <-received:
	default:
		t.Fatal("webhook endpoint was never called")
	}

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestWebhookDispatcher_NoSubscriptionsStillMarksDelivered documents the
// existing sweep semantics: events with no subscribers are marked delivered
// so they do not pile up as pending.
func TestWebhookDispatcher_NoSubscriptionsStillMarksDelivered(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tenantID := uuid.New()
	eventID := uuid.New()
	now := time.Now()

	mock.ExpectQuery("FROM event_outbox").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
		}).AddRow(eventID, tenantID, "task.created", "tasks", []byte(`{}`), false, now))

	mock.ExpectQuery("FROM webhook_subscriptions").
		WithArgs(tenantID, "task.created").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "name", "url", "secret", "event_types", "is_active", "created_at", "updated_at",
		}))

	mock.ExpectExec("UPDATE event_outbox SET delivered = TRUE").
		WillReturnResult(sqlmock.NewResult(0, 1))

	dispatcher := NewWebhookDispatcher(
		repositories.NewOutboxRepository(db),
		repositories.NewWebhookRepository(db),
		config.WebhooksConfig{},
		quietLog(),
	)

	dispatcher.dispatchPendingEvents(context.Background())

	assert.NoError(t, mock.ExpectationsWereMet())
}
