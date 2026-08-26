package services

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

func rollupTask(tenantID uuid.UUID, orderID *uuid.UUID) *types.Task {
	return &types.Task{
		ID:       uuid.New(),
		TenantID: tenantID,
		OrderID:  orderID,
		Title:    "Print bracket",
	}
}

func TestRollup_FirstTaskStartMarksOrderInProgress(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	pub, cleanup := newMiniredisPublisher(t)
	defer cleanup()
	pub.EnableOutbox(repositories.NewOutboxRepository(db))

	tenantID := uuid.New()
	orderID := uuid.New()

	// Guarded UPDATE fires and reports the previous status.
	mock.ExpectQuery(`SET status = 'in_progress'`).
		WithArgs(orderID).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("received"))

	// The transition is published as order.status_changed (durable via outbox).
	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(sqlmock.AnyArg(), tenantID, string(pubsub.EventOrderStatus), string(pubsub.NamespaceOrders), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	svc := NewOrderRollupService(repositories.NewOrderRepository(db), pub, quietLog())
	svc.OnTaskStatusChanged(context.Background(), rollupTask(tenantID, &orderID), types.TaskStatusInProgress)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRollup_AllTasksCompleteCompletesOrder(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	pub, cleanup := newMiniredisPublisher(t)
	defer cleanup()
	pub.EnableOutbox(repositories.NewOutboxRepository(db))

	tenantID := uuid.New()
	orderID := uuid.New()

	mock.ExpectQuery(`SET status = 'completed'`).
		WithArgs(orderID).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("in_progress"))

	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(sqlmock.AnyArg(), tenantID, string(pubsub.EventOrderStatus), string(pubsub.NamespaceOrders), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	svc := NewOrderRollupService(repositories.NewOrderRepository(db), pub, quietLog())
	svc.OnTaskStatusChanged(context.Background(), rollupTask(tenantID, &orderID), types.TaskStatusCompleted)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRollup_OpenTasksRemainNoTransition(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tenantID := uuid.New()
	orderID := uuid.New()

	// The guarded UPDATE matches no row (open tasks remain) — no event follows.
	mock.ExpectQuery(`SET status = 'completed'`).
		WithArgs(orderID).
		WillReturnError(sql.ErrNoRows)

	svc := NewOrderRollupService(repositories.NewOrderRepository(db), nil, quietLog())
	svc.OnTaskStatusChanged(context.Background(), rollupTask(tenantID, &orderID), types.TaskStatusCompleted)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRollup_QualityCheckAlsoMarksInProgress(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tenantID := uuid.New()
	orderID := uuid.New()

	// Machine acks advance tasks to quality_check; the order must still be
	// treated as in production.
	mock.ExpectQuery(`SET status = 'in_progress'`).
		WithArgs(orderID).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("received"))

	svc := NewOrderRollupService(repositories.NewOrderRepository(db), nil, quietLog())
	svc.OnTaskStatusChanged(context.Background(), rollupTask(tenantID, &orderID), types.TaskStatusQualityCheck)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRollup_TaskWithoutOrderIsIgnored(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// No SQL expectations: standalone tasks trigger no roll-up.
	svc := NewOrderRollupService(repositories.NewOrderRepository(db), nil, quietLog())
	svc.OnTaskStatusChanged(context.Background(), rollupTask(uuid.New(), nil), types.TaskStatusCompleted)
	svc.OnTaskStatusChanged(context.Background(), nil, types.TaskStatusCompleted)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRollup_BacklogMoveTriggersNothing(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	orderID := uuid.New()
	svc := NewOrderRollupService(repositories.NewOrderRepository(db), nil, quietLog())
	svc.OnTaskStatusChanged(context.Background(), rollupTask(uuid.New(), &orderID), types.TaskStatusBacklog)
	svc.OnTaskStatusChanged(context.Background(), rollupTask(uuid.New(), &orderID), types.TaskStatusBlocked)

	assert.NoError(t, mock.ExpectationsWereMet())
}
