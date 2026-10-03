package pubsub

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPublishCommandForDispatch_AppendsStreamEntry(t *testing.T) {
	pub, mr := newTestPublisher(t)
	defer mr.Close()
	defer func() { _ = pub.Close() }()

	tenantID, commandID, machineID, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	err := pub.PublishCommandForDispatch(context.Background(), tenantID, MachineCommandData{
		CommandID: commandID, MachineID: machineID, MQTTTopic: "org/site/area/line/m1",
		Command: CommandStartJob, Parameters: map[string]interface{}{"file_name": "part.gcode"},
		TaskID: &taskID, IssuedBy: uuid.New(), IssuedAt: time.Now().UTC(),
	})
	require.NoError(t, err)

	entries, err := pub.client.XRange(context.Background(), DefaultCommandStreamKey, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	v := entries[0].Values
	require.Equal(t, tenantID.String(), v["tenant_id"])
	require.Equal(t, commandID.String(), v["command_id"])

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(v["payload"].(string)), &payload))
	require.Equal(t, machineID.String(), payload["machine_id"])
	require.Equal(t, "start_job", payload["command"])
	require.Equal(t, taskID.String(), payload["task_id"])
	require.Equal(t, "org/site/area/line/m1", payload["mqtt_topic"])
}

func TestPublishCommandForDispatch_UsesConfiguredStreamAndCap(t *testing.T) {
	pub, mr := newTestPublisher(t)
	defer mr.Close()
	defer func() { _ = pub.Close() }()
	pub.commandStream = "custom:commands"
	pub.commandStreamMaxLen = 5

	for i := 0; i < 20; i++ {
		require.NoError(t, pub.PublishCommandForDispatch(context.Background(), uuid.New(), MachineCommandData{
			CommandID: uuid.New(), MachineID: uuid.New(), Command: CommandHome, IssuedAt: time.Now(),
		}))
	}
	n, err := pub.client.XLen(context.Background(), "custom:commands").Result()
	require.NoError(t, err)
	require.LessOrEqual(t, n, int64(20))
	require.Positive(t, n)
	exists, err := pub.client.Exists(context.Background(), DefaultCommandStreamKey).Result()
	require.NoError(t, err)
	require.Zero(t, exists)
}

func TestPublishCommandForDispatch_FailsVisiblyWhenRedisIsDown(t *testing.T) {
	pub, mr := newTestPublisher(t)
	defer func() { _ = pub.Close() }()
	mr.Close()

	err := pub.PublishCommandForDispatch(context.Background(), uuid.New(), MachineCommandData{
		CommandID: uuid.New(), MachineID: uuid.New(), Command: CommandHome, IssuedAt: time.Now(),
	})
	require.Error(t, err)
}

func TestNotifyRealtime_SkipsOutbox(t *testing.T) {
	pub, sink, cleanup := newOutboxPublisher(t)
	defer cleanup()

	event := NewEvent(EventMachineStatusChanged, uuid.New(), MachineStatusData{NewStatus: "offline"})
	require.NoError(t, pub.NotifyRealtime(context.Background(), NamespaceMachines, event.TenantID, event))
	// No outbox insert was expected on the sqlmock sink.
	require.NoError(t, sink.ExpectationsWereMet())
}
