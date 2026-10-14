package mqtt

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/config"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// recordingStore is a TelemetryStore that records what ingest touched.
type recordingStore struct {
	mu         sync.Mutex
	machine    *types.Machine
	heartbeats int
	records    []types.Telemetry
}

func (s *recordingStore) CreateBatch(_ context.Context, records []types.Telemetry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, records...)
	return nil
}

func (s *recordingStore) ResolveMachine(_ context.Context, _, code string) (*types.Machine, error) {
	if code != s.machine.Code {
		return nil, nil
	}
	return s.machine, nil
}

func (s *recordingStore) UpdateMachineHeartbeat(context.Context, uuid.UUID, uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeats++
	return nil
}

func (s *recordingStore) snapshot() (heartbeats int, metrics []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.records {
		metrics = append(metrics, r.MetricType)
	}
	return s.heartbeats, metrics
}

func TestControlChannel(t *testing.T) {
	cases := map[string]string{
		"org/site/area/line/m1/cmd":          "cmd",
		"org/site/area/line/m1/ack":          "ack",
		"org/site/area/line/m1/temperature":  "",
		"org/site/area/line/m1/cmd/extra":    "",
		"org/site/area/line/cmd":             "",
		"org/site/area/line/m1/acknowledged": "",
	}
	for topic, want := range cases {
		require.Equal(t, want, controlChannel(topic), topic)
	}
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

// startBroker runs an in-process MQTT broker for the duration of the test.
func startBroker(t *testing.T) (host string, port int) {
	t.Helper()
	addr := freeTCPAddr(t)
	server := mochi.New(&mochi.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	require.NoError(t, server.AddHook(new(auth.AllowHook), nil))
	require.NoError(t, server.AddListener(listeners.NewTCP(listeners.Config{ID: "test", Address: addr})))
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })

	h, p, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	_, err = fmt.Sscanf(p, "%d", &port)
	require.NoError(t, err)
	return h, port
}

// TestTelemetryIngest_SkipsCommandChannelOverRealBroker publishes telemetry,
// a command and an ack for the same machine through a real broker and checks
// that only the telemetry reading is ingested or refreshes liveness.
func TestTelemetryIngest_SkipsCommandChannelOverRealBroker(t *testing.T) {
	host, port := startBroker(t)

	cfg := &config.Config{
		MQTT: config.MQTTConfig{
			Broker: host, Port: port, ClientID: "ingest-under-test",
			TopicRoot: "+/+/+/+/+/#", QoS: 1, CleanStart: true,
		},
		Worker: config.WorkerConfig{BatchSize: 1, BatchTimeout: 20, NumWorkers: 1},
	}
	store := &recordingStore{machine: &types.Machine{ID: uuid.New(), TenantID: uuid.New(), Code: "m1"}}
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	h := NewHandler(cfg, store, log)
	require.NoError(t, h.Connect())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, h.Start(ctx))
	defer h.Stop()

	opts := paho.NewClientOptions().AddBroker(fmt.Sprintf("tcp://%s:%d", host, port)).SetClientID("device-sim")
	device := paho.NewClient(opts)
	tok := device.Connect()
	require.True(t, tok.WaitTimeout(5*time.Second))
	require.NoError(t, tok.Error())
	defer device.Disconnect(100)

	// Give the handler's subscription a moment to register.
	require.Eventually(t, func() bool { return h.GetMQTTClient().IsConnectionOpen() }, 2*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)

	publish := func(topic, payload string) {
		tk := device.Publish(topic, 1, false, payload)
		require.True(t, tk.WaitTimeout(5*time.Second))
		require.NoError(t, tk.Error())
	}
	publish("org/site/area/line/m1/cmd", `{"command_id":"`+uuid.NewString()+`","command":"start_job"}`)
	publish("org/site/area/line/m1/ack", `{"command_id":"`+uuid.NewString()+`","success":true}`)
	// Sparkplug topics share the wildcard; they belong to the primary host.
	publish("spBv1.0/org/DDATA/site/m1", `{"metric_type":"temperature","value":1,"unit":"C"}`)
	publish("spBv1.0/org/DDATA/site/m1/extra", `{"metric_type":"temperature","value":2,"unit":"C"}`)
	publish("org/site/area/line/m1/temperature", `{"metric_type":"temperature","value":210.5,"unit":"C"}`)

	require.Eventually(t, func() bool {
		_, metrics := store.snapshot()
		return len(metrics) == 1
	}, 5*time.Second, 20*time.Millisecond)
	time.Sleep(200 * time.Millisecond)

	heartbeats, metrics := store.snapshot()
	require.Equal(t, []string{"temperature"}, metrics)
	require.Equal(t, 1, heartbeats, "only the telemetry reading may refresh liveness")
}
