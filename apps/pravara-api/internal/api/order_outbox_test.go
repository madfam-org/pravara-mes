package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
)

// TestOrderHandler_UpdateStatusWritesOutboxEvent is the end-to-end unit
// proof of the outbox fix at the handler level: PATCH /orders/:id with a
// status change publishes order.status_changed through the outbox-backed
// publisher, so a row lands in event_outbox where the webhook dispatcher,
// /v1/events history, and CRM feeds can see it. It also exercises the
// status-vocabulary reconciliation: the legacy alias "confirmed" is
// normalized to the canonical DB enum value "validated" before the write.
func TestOrderHandler_UpdateStatusWritesOutboxEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	publisher, err := pubsub.NewPublisher(pubsub.PublisherConfig{RedisURL: "redis://" + mr.Addr()}, log)
	require.NoError(t, err)
	defer publisher.Close()
	publisher.EnableOutbox(repositories.NewOutboxRepository(db))

	handler := NewOrderHandler(repositories.NewOrderRepository(db), repositories.NewOrderItemRepository(db), log)
	handler.SetPublisher(publisher)

	orderID := uuid.New()
	tenantID := uuid.New()
	now := time.Now()

	// 1) handler loads the order (status: received)
	mock.ExpectQuery("SELECT id, tenant_id.*FROM orders WHERE id").
		WithArgs(orderID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "external_id", "customer_name", "customer_email",
			"status", "priority", "due_date", "total_amount", "currency",
			"shipping_address", "metadata", "created_at", "updated_at",
		}).AddRow(
			orderID, tenantID, "EXT-77", "Acme MX", "ops@acme.mx",
			"received", 5, now, 100.0, "MXN", nil, []byte("{}"), now, now,
		))

	// 2) the UPDATE persists the NORMALIZED status (validated, not confirmed)
	mock.ExpectQuery("UPDATE orders SET").
		WithArgs(
			orderID, "Acme MX", sqlmock.AnyArg(), "validated",
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	// 3) order.status_changed lands in event_outbox
	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(
			sqlmock.AnyArg(), tenantID,
			string(pubsub.EventOrderStatus), string(pubsub.NamespaceOrders),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(now))

	router := gin.New()
	router.PATCH("/orders/:id", handler.Update)

	body, _ := json.Marshal(UpdateOrderRequest{Status: "confirmed"}) // legacy alias
	req := httptest.NewRequest(http.MethodPatch, "/orders/"+orderID.String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), `"status":"validated"`, "alias must be normalized to the canonical enum")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestOrderHandler_UpdateRejectsUnknownStatus documents the new validation:
// values outside the canonical enum and its aliases are rejected with 400
// instead of reaching Postgres and failing with an opaque 500.
func TestOrderHandler_UpdateRejectsUnknownStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	handler := NewOrderHandler(repositories.NewOrderRepository(db), repositories.NewOrderItemRepository(db), log)

	orderID := uuid.New()
	now := time.Now()

	mock.ExpectQuery("SELECT id, tenant_id.*FROM orders WHERE id").
		WithArgs(orderID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "external_id", "customer_name", "customer_email",
			"status", "priority", "due_date", "total_amount", "currency",
			"shipping_address", "metadata", "created_at", "updated_at",
		}).AddRow(
			orderID, uuid.New(), "EXT-78", "Acme MX", "ops@acme.mx",
			"received", 5, now, 100.0, "MXN", nil, []byte("{}"), now, now,
		))

	router := gin.New()
	router.PATCH("/orders/:id", handler.Update)

	body, _ := json.Marshal(UpdateOrderRequest{Status: "warp_speed"})
	req := httptest.NewRequest(http.MethodPatch, "/orders/"+orderID.String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_status")
	assert.NoError(t, mock.ExpectationsWereMet())
}
