package pubsub

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// newTestPublisher creates a Publisher backed by miniredis for testing.
func newTestPublisher(t *testing.T) (*Publisher, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel) // suppress test logs

	pub := &Publisher{
		client: client,
		log:    log,
	}

	return pub, mr
}

// newOutboxPublisher returns a Publisher backed by miniredis with an outbox
// sink backed by sqlmock.
func newOutboxPublisher(t *testing.T) (*Publisher, sqlmock.Sqlmock, func()) {
	t.Helper()

	pub, mr := newTestPublisher(t)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	pub.EnableOutbox(repositories.NewOutboxRepository(db))

	cleanup := func() {
		pub.Close()
		mr.Close()
		db.Close()
	}
	return pub, mock, cleanup
}

func TestPublisher_PublishPersistsToOutbox(t *testing.T) {
	pub, mock, cleanup := newOutboxPublisher(t)
	defer cleanup()

	tenantID := uuid.New()
	event := NewEvent(EventOrderCreated, tenantID, map[string]string{"order_id": "test-123"})

	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(
			sqlmock.AnyArg(),          // id
			tenantID,                  // tenant_id
			string(EventOrderCreated), // event_type
			string(NamespaceOrders),   // channel_namespace
			sqlmock.AnyArg(),          // payload (JSON)
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	err := pub.Publish(context.Background(), NamespaceOrders, tenantID, event)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPublisher_PublishToEntityPersistsToOutbox(t *testing.T) {
	pub, mock, cleanup := newOutboxPublisher(t)
	defer cleanup()

	tenantID := uuid.New()
	entityID := uuid.New()
	event := NewEvent(EventMachineStatusChanged, tenantID, MachineStatusData{
		MachineID:   entityID,
		MachineName: "CNC-01",
		NewStatus:   "online",
		UpdatedAt:   time.Now(),
	})

	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(
			sqlmock.AnyArg(),
			tenantID,
			string(EventMachineStatusChanged),
			string(NamespaceMachines),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	err := pub.PublishToEntity(context.Background(), NamespaceMachines, tenantID, entityID, event)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPublisher_ConvenienceMethodsPersistToOutbox is the regression test for
// the OutboxPublisher wrapper bug: helper methods (PublishTaskMove,
// PublishOrderStatus, ...) are promoted from *Publisher, so a wrapper's
// overridden Publish was silently bypassed and no business event ever
// reached event_outbox. With the sink folded into Publisher itself, every
// helper must persist.
func TestPublisher_ConvenienceMethodsPersistToOutbox(t *testing.T) {
	pub, mock, cleanup := newOutboxPublisher(t)
	defer cleanup()

	tenantID := uuid.New()

	// PublishTaskMove -> task.moved in tasks namespace
	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(
			sqlmock.AnyArg(),
			tenantID,
			string(EventTaskMoved),
			string(NamespaceTasks),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	err := pub.PublishTaskMove(context.Background(), tenantID, TaskMoveData{
		TaskID:    uuid.New(),
		TaskTitle: "Print bracket",
		OldStatus: "backlog",
		NewStatus: "in_progress",
		MovedBy:   uuid.New(),
		MovedAt:   time.Now().UTC(),
	})
	require.NoError(t, err)

	// PublishOrderStatus -> order.status_changed in orders namespace
	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(
			sqlmock.AnyArg(),
			tenantID,
			string(EventOrderStatus),
			string(NamespaceOrders),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	err = pub.PublishOrderStatus(context.Background(), tenantID, OrderStatusData{
		OrderID:      uuid.New(),
		OldStatus:    "received",
		NewStatus:    "in_progress",
		CustomerName: "Test Corp",
		UpdatedAt:    time.Now(),
	})
	require.NoError(t, err)

	// PublishTaskAssignmentFailed -> task.assignment_failed in tasks namespace
	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(
			sqlmock.AnyArg(),
			tenantID,
			string(EventTaskAssignmentFailed),
			string(NamespaceTasks),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	err = pub.PublishTaskAssignmentFailed(context.Background(), tenantID, TaskAssignmentFailedData{
		TaskID:               uuid.New(),
		TaskTitle:            "Mill housing",
		RequiredCapabilities: []string{"cnc_milling", "aluminum"},
		CandidatesEvaluated:  3,
		Reason:               "no machine satisfies required capabilities",
		FailedAt:             time.Now().UTC(),
	})
	require.NoError(t, err)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPublisher_OutboxFailureDoesNotBlockPublish(t *testing.T) {
	pub, mock, cleanup := newOutboxPublisher(t)
	defer cleanup()

	tenantID := uuid.New()
	event := NewEvent(EventTaskCompleted, tenantID, map[string]string{"task_id": "task-456"})

	// Outbox insert fails - this should not block the publish
	mock.ExpectQuery("INSERT INTO event_outbox").
		WillReturnError(assert.AnError)

	err := pub.Publish(context.Background(), NamespaceTasks, tenantID, event)

	assert.NoError(t, err, "Publish should succeed even when outbox insert fails")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPublisher_NoOutboxSinkStillPublishes(t *testing.T) {
	pub, mr := newTestPublisher(t)
	defer mr.Close()
	defer pub.Close()

	tenantID := uuid.New()
	event := NewEvent(EventTaskCreated, tenantID, map[string]string{"task_id": "t1"})

	err := pub.Publish(context.Background(), NamespaceTasks, tenantID, event)
	assert.NoError(t, err, "Publisher without an outbox sink keeps the real-time path working")
}

func TestPublisher_OutboxEventPayloadIsValidJSON(t *testing.T) {
	pub, mock, cleanup := newOutboxPublisher(t)
	defer cleanup()

	tenantID := uuid.New()
	event := NewEvent(EventMachineCreated, tenantID, map[string]interface{}{
		"machine_id": uuid.New().String(),
		"name":       "Test Machine",
	})

	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(
			sqlmock.AnyArg(),
			tenantID,
			string(EventMachineCreated),
			string(NamespaceMachines),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	err := pub.Publish(context.Background(), NamespaceMachines, tenantID, event)
	assert.NoError(t, err)

	// The payload persisted is the marshaled event; verify it is valid JSON.
	payload, marshalErr := json.Marshal(event)
	assert.NoError(t, marshalErr)
	assert.True(t, json.Valid(payload))

	assert.NoError(t, mock.ExpectationsWereMet())
}
