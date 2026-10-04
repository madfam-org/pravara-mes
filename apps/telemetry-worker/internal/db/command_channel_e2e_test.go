package db

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/command"
)

// The end-to-end command channel test needs a throwaway Redis
// (PRAVARA_TEST_REDIS_URL) and PostgreSQL (PRAVARA_TEST_DATABASE_URL); the
// MQTT broker runs in process. No real machine is involved: a simulated
// device subscribes to its command topic and answers on its ack topic.
const testRedisEnv = "PRAVARA_TEST_REDIS_URL"

func openTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	raw := os.Getenv(testRedisEnv)
	if raw == "" {
		t.Skipf("%s not set; skipping Redis test", testRedisEnv)
	}
	opts, err := redis.ParseURL(raw)
	require.NoError(t, err)
	c := redis.NewClient(opts)
	require.NoError(t, c.Ping(context.Background()).Err())
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func startTestBroker(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	server := mochi.New(&mochi.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	require.NoError(t, server.AddHook(new(auth.AllowHook), nil))
	require.NoError(t, server.AddListener(listeners.NewTCP(listeners.Config{ID: "e2e", Address: addr})))
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })
	return "tcp://" + addr
}

func connectClient(t *testing.T, broker, id string) paho.Client {
	t.Helper()
	c := paho.NewClient(paho.NewClientOptions().AddBroker(broker).SetClientID(id))
	tok := c.Connect()
	require.True(t, tok.WaitTimeout(5*time.Second))
	require.NoError(t, tok.Error())
	t.Cleanup(func() { c.Disconnect(100) })
	return c
}

// enqueueLikeAPI appends a command entry with the pravara-api field layout.
func enqueueLikeAPI(t *testing.T, rc *redis.Client, stream string, tenantID uuid.UUID, cmd command.MachineCommand) {
	t.Helper()
	payload, err := json.Marshal(cmd)
	require.NoError(t, err)
	require.NoError(t, rc.XAdd(context.Background(), &redis.XAddArgs{
		Stream: stream, MaxLen: 1000, Approx: true,
		Values: map[string]interface{}{
			command.StreamFieldTenantID:  tenantID.String(),
			command.StreamFieldCommandID: cmd.CommandID.String(),
			command.StreamFieldPayload:   string(payload),
		},
	}).Err())
}

func TestCommandChannelE2E_RestartRedeliveryAckAndCompletion(t *testing.T) {
	db := openTestDB(t)
	rc := openTestRedis(t)
	broker := startTestBroker(t)
	f := newFixture(t, db)
	ctx := context.Background()

	stream := "test:e2e:" + uuid.NewString()
	t.Cleanup(func() { rc.Del(context.Background(), stream) })
	require.NoError(t, rc.XGroupCreateMkStream(ctx, stream, "workers", "0").Err())

	suffix := f.tenantID.String()[:8]
	topic := f.topic("site/area/line/sim-" + suffix)
	machineID := f.machine(t, "sim-"+suffix, topic, "online")
	orderID := f.order(t, "received")
	taskID := f.task(t, &orderID, machineID)
	commandID := f.command(t, machineID, &taskID, "start_job", "pending")

	enqueueLikeAPI(t, rc, stream, f.tenantID, command.MachineCommand{
		CommandID: commandID, MachineID: machineID, MQTTTopic: topic, Command: "start_job",
		TaskID: &taskID, IssuedBy: uuid.New(), IssuedAt: time.Now().UTC(),
	})

	// A worker reads the entry and dies before publishing or acknowledging.
	got, err := rc.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: "workers", Consumer: "crashed", Streams: []string{stream, ">"}, Count: 1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, got[0].Messages, 1)

	// Simulated device: receives on {topic}/cmd, reports completion on {topic}/ack.
	device := connectClient(t, broker, "device-"+suffix)
	var mu sync.Mutex
	var received []string
	sub := device.Subscribe(topic+"/cmd", 1, func(c paho.Client, m paho.Message) {
		var p command.MQTTCommandPayload
		if json.Unmarshal(m.Payload(), &p) != nil {
			return
		}
		mu.Lock()
		received = append(received, p.CommandID)
		mu.Unlock()
		ack, _ := json.Marshal(command.CommandAck{CommandID: p.CommandID, Success: true, JobCompleted: true, Timestamp: time.Now().UTC()})
		c.Publish(topic+"/ack", 1, false, ack)
	})
	require.True(t, sub.WaitTimeout(5*time.Second))
	require.NoError(t, sub.Error())

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	worker := connectClient(t, broker, "worker-"+suffix)
	ledger := NewCommandLedgerWithScope(db, TxTenantScope{DB: db})

	var completions []command.JobCompletion
	acks := command.NewAckHandler(worker, nil, log, "+/+/+/+/+/#")
	acks.SetLedger(ledger)
	acks.SetCompletionHook(command.JobCompletionHookFunc(func(_ context.Context, c command.JobCompletion) error {
		mu.Lock()
		completions = append(completions, c)
		mu.Unlock()
		return nil
	}))
	require.NoError(t, acks.Start(ctx))
	defer acks.Stop()

	// The restarted worker reclaims the idle entry and dispatches it.
	d := command.NewDispatcher(rc, command.NewPahoPublisher(worker), ledger, command.DispatcherConfig{
		StreamKey: stream, Group: "workers", Consumer: "restarted",
		MaxAttempts: 3, RetryIdle: 100 * time.Millisecond, AckTimeout: time.Minute, ReadBlock: 50 * time.Millisecond,
	}, log)
	time.Sleep(150 * time.Millisecond)
	require.NoError(t, d.Start(ctx))
	defer d.Stop()

	require.Eventually(t, func() bool {
		s, _, _ := f.commandStatus(t, commandID)
		return s == "completed"
	}, 10*time.Second, 50*time.Millisecond)

	mu.Lock()
	require.Equal(t, []string{commandID.String()}, received, "delivered exactly once to the device")
	require.Len(t, completions, 1)
	mu.Unlock()

	require.Equal(t, "quality_check", f.scalar(t, `SELECT status::text FROM tasks WHERE id = $1`, taskID))
	require.Equal(t, "in_progress", f.scalar(t, `SELECT status::text FROM orders WHERE id = $1`, orderID))
	_, attempts, _ := f.commandStatus(t, commandID)
	require.Equal(t, 1, attempts)
	events := f.outboxEvents(t)
	require.Len(t, events[EventTaskJobCompleted], 1)
	require.Len(t, events[EventOrderStatusChanged], 1)

	pending, err := rc.XPending(ctx, stream, "workers").Result()
	require.NoError(t, err)
	require.Zero(t, pending.Count)
}

func TestCommandChannelE2E_PublishFailureIsWrittenBack(t *testing.T) {
	db := openTestDB(t)
	rc := openTestRedis(t)
	f := newFixture(t, db)
	ctx := context.Background()

	stream := "test:e2e:" + uuid.NewString()
	t.Cleanup(func() { rc.Del(context.Background(), stream) })

	machineID := f.machine(t, "unreachable", "org/site/area/line/unreachable", "online")
	taskID := f.task(t, nil, machineID)
	commandID := f.command(t, machineID, &taskID, "start_job", "pending")
	enqueueLikeAPI(t, rc, stream, f.tenantID, command.MachineCommand{
		CommandID: commandID, MachineID: machineID, Command: "start_job", IssuedAt: time.Now().UTC(),
	})

	// A broker that never comes up: the paho client is not connected.
	offline := paho.NewClient(paho.NewClientOptions().AddBroker("tcp://127.0.0.1:" + strconv.Itoa(1)))
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	d := command.NewDispatcher(rc, command.NewPahoPublisher(offline), NewCommandLedgerWithScope(db, TxTenantScope{DB: db}), command.DispatcherConfig{
		StreamKey: stream, Group: "workers", Consumer: "w1",
		MaxAttempts: 2, RetryIdle: 100 * time.Millisecond, AckTimeout: time.Minute, ReadBlock: 50 * time.Millisecond,
	}, log)
	require.NoError(t, d.Start(ctx))
	defer d.Stop()

	require.Eventually(t, func() bool {
		s, _, _ := f.commandStatus(t, commandID)
		return s == "failed"
	}, 10*time.Second, 50*time.Millisecond)

	_, attempts, msg := f.commandStatus(t, commandID)
	require.Equal(t, 2, attempts)
	require.Contains(t, msg, "not connected")
	events := f.outboxEvents(t)
	require.Len(t, events[EventMachineCommandFailed], 1)
	require.Len(t, events[EventTaskJobFailed], 1)
	require.Equal(t, fmt.Sprint(2), fmt.Sprint(events[EventMachineCommandFailed][0]["attempts"]))
}
