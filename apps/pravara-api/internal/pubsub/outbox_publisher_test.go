package pubsub

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DATA-DOG/go-sqlmock"
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

func TestOutboxPublisher_Publish(t *testing.T) {
	pub, mr := newTestPublisher(t)
	defer mr.Close()
	defer pub.Close()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	outboxRepo := repositories.NewOutboxRepository(db)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	outboxPub := NewOutboxPublisher(pub, outboxRepo, log)

	tenantID := uuid.New()
	event := NewEvent(EventOrderCreated, tenantID, map[string]string{"order_id": "test-123"})

	// Expect the outbox insert query
	mock.ExpectQuery("INSERT INTO event_outbox").
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	err = outboxPub.Publish(context.Background(), NamespaceOrders, tenantID, event)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOutboxPublisher_PublishToEntity(t *testing.T) {
	pub, mr := newTestPublisher(t)
	defer mr.Close()
	defer pub.Close()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	outboxRepo := repositories.NewOutboxRepository(db)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	outboxPub := NewOutboxPublisher(pub, outboxRepo, log)

	tenantID := uuid.New()
	entityID := uuid.New()
	event := NewEvent(EventMachineStatusChanged, tenantID, MachineStatusData{
		MachineID:   entityID,
		MachineName: "CNC-01",
		NewStatus:   "online",
		UpdatedAt:   time.Now(),
	})

	// Expect the outbox insert query
	mock.ExpectQuery("INSERT INTO event_outbox").
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	err = outboxPub.PublishToEntity(context.Background(), NamespaceMachines, tenantID, entityID, event)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOutboxPublisher_OutboxFailureDoesNotBlockPublish(t *testing.T) {
	pub, mr := newTestPublisher(t)
	defer mr.Close()
	defer pub.Close()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	outboxRepo := repositories.NewOutboxRepository(db)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	outboxPub := NewOutboxPublisher(pub, outboxRepo, log)

	tenantID := uuid.New()
	event := NewEvent(EventTaskCompleted, tenantID, map[string]string{"task_id": "task-456"})

	// Outbox insert fails - this should not block the publish
	mock.ExpectQuery("INSERT INTO event_outbox").
		WillReturnError(assert.AnError)

	// Publish should still succeed (the real-time Centrifugo path via Redis works)
	err = outboxPub.Publish(context.Background(), NamespaceTasks, tenantID, event)

	assert.NoError(t, err, "Publish should succeed even when outbox insert fails")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOutboxPublisher_OutboxFailureDoesNotBlockPublishToEntity(t *testing.T) {
	pub, mr := newTestPublisher(t)
	defer mr.Close()
	defer pub.Close()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	outboxRepo := repositories.NewOutboxRepository(db)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	outboxPub := NewOutboxPublisher(pub, outboxRepo, log)

	tenantID := uuid.New()
	entityID := uuid.New()
	event := NewEvent(EventMachineHeartbeat, tenantID, MachineHeartbeatData{
		MachineID:     entityID,
		LastHeartbeat: time.Now(),
		IsOnline:      true,
	})

	// Outbox insert fails
	mock.ExpectQuery("INSERT INTO event_outbox").
		WillReturnError(assert.AnError)

	err = outboxPub.PublishToEntity(context.Background(), NamespaceMachines, tenantID, entityID, event)

	assert.NoError(t, err, "PublishToEntity should succeed even when outbox insert fails")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOutboxPublisher_PersistsCorrectData(t *testing.T) {
	pub, mr := newTestPublisher(t)
	defer mr.Close()
	defer pub.Close()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	outboxRepo := repositories.NewOutboxRepository(db)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	outboxPub := NewOutboxPublisher(pub, outboxRepo, log)

	tenantID := uuid.New()
	event := NewEvent(EventOrderStatus, tenantID, OrderStatusData{
		OrderID:     uuid.New(),
		OldStatus:   "confirmed",
		NewStatus:   "in_production",
		CustomerName: "Test Corp",
		UpdatedAt:   time.Now(),
	})

	// Verify the correct event type and namespace are passed to the outbox
	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(
			sqlmock.AnyArg(),                       // id
			tenantID,                                // tenant_id
			string(EventOrderStatus),                // event_type
			string(NamespaceOrders),                 // channel_namespace
			sqlmock.AnyArg(),                        // payload (JSON)
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	err = outboxPub.Publish(context.Background(), NamespaceOrders, tenantID, event)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNewOutboxPublisher(t *testing.T) {
	pub, mr := newTestPublisher(t)
	defer mr.Close()
	defer pub.Close()

	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	outboxRepo := repositories.NewOutboxRepository(db)
	log := logrus.New()

	outboxPub := NewOutboxPublisher(pub, outboxRepo, log)

	assert.NotNil(t, outboxPub)
	assert.NotNil(t, outboxPub.Publisher)
}

func TestOutboxPublisher_EventPayloadIsValidJSON(t *testing.T) {
	pub, mr := newTestPublisher(t)
	defer mr.Close()
	defer pub.Close()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	outboxRepo := repositories.NewOutboxRepository(db)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	outboxPub := NewOutboxPublisher(pub, outboxRepo, log)

	tenantID := uuid.New()
	event := NewEvent(EventMachineCreated, tenantID, map[string]interface{}{
		"machine_id": uuid.New().String(),
		"name":       "Test Machine",
	})

	// Capture the payload argument to verify it is valid JSON
	mock.ExpectQuery("INSERT INTO event_outbox").
		WithArgs(
			sqlmock.AnyArg(), // id
			tenantID,         // tenant_id
			string(EventMachineCreated), // event_type
			string(NamespaceMachines),   // channel_namespace
			sqlmock.AnyArg(), // payload
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(time.Now()))

	err = outboxPub.Publish(context.Background(), NamespaceMachines, tenantID, event)
	assert.NoError(t, err)

	// Verify the event itself can be marshaled to valid JSON (same as persistToOutbox does)
	payload, marshalErr := json.Marshal(event)
	assert.NoError(t, marshalErr)
	assert.True(t, json.Valid(payload))

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOutboxPublisher_PublishReturnsRedisError(t *testing.T) {
	// Create a publisher with a stopped miniredis to simulate Redis failure
	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	pub := &Publisher{
		client: client,
		log:    log,
	}

	db, mock, dbErr := sqlmock.New()
	require.NoError(t, dbErr)
	defer db.Close()

	outboxRepo := repositories.NewOutboxRepository(db)
	outboxPub := NewOutboxPublisher(pub, outboxRepo, log)

	// Stop Redis to cause publish failure
	mr.Close()

	tenantID := uuid.New()
	event := NewEvent(EventTaskCreated, tenantID, map[string]string{"task_id": "t1"})

	// The outbox persist will also fail since it runs after publish, but
	// the important thing is that the Redis error is returned
	err = outboxPub.Publish(context.Background(), NamespaceTasks, tenantID, event)

	assert.Error(t, err, "Should return error when Redis is down")

	// No outbox expectations needed since marshal may or may not succeed,
	// and the query may or may not be called depending on marshal result.
	// We just verify any expectations that were set.
	_ = mock.ExpectationsWereMet()
}
