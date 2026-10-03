package edge

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/manager"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/simulator"
	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

const (
	testGroup    = "acme"
	testEdge     = "site-north"
	edgeUser     = "edge-site-north"
	edgePassword = "edge-test-password"
	hostUser     = "pravara-host"
)

// aclHook authenticates the edge-node credential and enforces the MES-1 ACL
// on it; the host credential may do anything. Denials are recorded so tests
// can assert the edge node never steps outside its ACL.
type aclHook struct {
	mqtt.HookBase
	rules  []sparkplug.ACLRule
	mu     sync.Mutex
	denied []string
}

func (h *aclHook) ID() string { return "mes1-acl" }

func (h *aclHook) Provides(b byte) bool {
	return bytes.Contains([]byte{mqtt.OnConnectAuthenticate, mqtt.OnACLCheck}, []byte{b})
}

func (h *aclHook) OnConnectAuthenticate(_ *mqtt.Client, pk packets.Packet) bool {
	u, p := string(pk.Connect.Username), string(pk.Connect.Password)
	return (u == edgeUser && p == edgePassword) || u == hostUser
}

func (h *aclHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	if string(cl.Properties.Username) != edgeUser {
		return true
	}
	action := sparkplug.ACLSubscribe
	if write {
		action = sparkplug.ACLPublish
	}
	if sparkplug.Permits(h.rules, action, topic) {
		return true
	}
	h.mu.Lock()
	h.denied = append(h.denied, string(action)+" "+topic)
	h.mu.Unlock()
	return false
}

type testBroker struct {
	server *mqtt.Server
	port   int
	caFile string
	acl    *aclHook
}

func startBroker(t *testing.T) *testBroker {
	t.Helper()
	cert, err := simulator.SelfSigned("broker.test")
	if err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, cert.PEM, 0o600); err != nil {
		t.Fatal(err)
	}
	port, err := simulator.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	rules, err := sparkplug.EdgeNodeACL(testGroup, testEdge)
	if err != nil {
		t.Fatal(err)
	}
	hook := &aclHook{rules: rules}
	s := mqtt.New(&mqtt.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := s.AddHook(hook, nil); err != nil {
		t.Fatal(err)
	}
	l := listeners.NewTCP(listeners.Config{ID: "tls", Address: fmt.Sprintf("127.0.0.1:%d", port),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert.TLS}, MinVersion: tls.VersionTLS12}})
	if err := s.AddListener(l); err != nil {
		t.Fatal(err)
	}
	if err := s.Serve(); err != nil {
		t.Fatal(err)
	}
	b := &testBroker{server: s, port: port, caFile: caFile, acl: hook}
	t.Cleanup(func() {
		_ = s.Close()
		hook.mu.Lock()
		defer hook.mu.Unlock()
		if len(hook.denied) > 0 {
			t.Errorf("edge node attempted operations outside its ACL: %v", hook.denied)
		}
	})
	return b
}

// kick drops the edge node's connection without a DISCONNECT (network loss).
func (b *testBroker) kick(t *testing.T, clientID string) {
	t.Helper()
	cl, ok := b.server.Clients.Get(clientID)
	if !ok {
		t.Fatalf("client %s not connected", clientID)
	}
	cl.Stop(fmt.Errorf("test: connection dropped"))
}

// message is one Sparkplug message seen by the host.
type message struct {
	topic   sparkplug.Topic
	payload *pb.Payload
	state   *sparkplug.HostState
}

// hostClient plays the primary host application: it observes every message
// and publishes STATE, NCMD and DCMD.
type hostClient struct {
	t       *testing.T
	client  paho.Client
	aliases *sparkplug.AliasTable
	mu      sync.Mutex
	msgs    []message
}

func startHost(t *testing.T, b *testBroker) *hostClient {
	t.Helper()
	pool, err := os.ReadFile(b.caFile)
	if err != nil {
		t.Fatal(err)
	}
	tc := &tls.Config{ServerName: "broker.test", MinVersion: tls.VersionTLS12, RootCAs: poolFrom(t, pool)}
	h := &hostClient{t: t, aliases: sparkplug.NewAliasTable()}
	opts := paho.NewClientOptions().AddBroker(fmt.Sprintf("ssl://127.0.0.1:%d", b.port)).
		SetClientID("host-observer").SetUsername(hostUser).SetTLSConfig(tc).SetAutoReconnect(false)
	h.client = paho.NewClient(opts)
	if tok := h.client.Connect(); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("host connect: %v", tok.Error())
	}
	if tok := h.client.Subscribe("spBv1.0/#", 1, h.onMessage); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("host subscribe: %v", tok.Error())
	}
	t.Cleanup(func() { h.client.Disconnect(100) })
	return h
}

func (h *hostClient) onMessage(_ paho.Client, m paho.Message) {
	topic, err := sparkplug.ParseTopic(m.Topic())
	if err != nil {
		h.t.Errorf("unparseable topic %q", m.Topic())
		return
	}
	msg := message{topic: topic}
	if topic.Type == sparkplug.STATE {
		s, err := sparkplug.DecodeState(m.Payload())
		if err != nil {
			h.t.Errorf("bad STATE: %v", err)
			return
		}
		msg.state = &s
	} else {
		p, err := sparkplug.Decode(m.Payload())
		if err != nil {
			h.t.Errorf("bad payload on %s: %v", m.Topic(), err)
			return
		}
		msg.payload = p
		switch topic.Type {
		case sparkplug.NBIRTH:
			_ = h.aliases.LearnBirth("", p)
		case sparkplug.DBIRTH:
			if err := h.aliases.LearnBirth(topic.DeviceID, p); err != nil {
				h.t.Errorf("DBIRTH aliases: %v", err)
			}
		}
	}
	h.mu.Lock()
	h.msgs = append(h.msgs, msg)
	h.mu.Unlock()
}

func (h *hostClient) messages() []message {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]message(nil), h.msgs...)
}

// waitFor returns the first message at or after index from that satisfies pred.
func (h *hostClient) waitFor(desc string, from int, pred func(message) bool) (message, int) {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		msgs := h.messages()
		for i := from; i < len(msgs); i++ {
			if pred(msgs[i]) {
				return msgs[i], i
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s; saw %d messages", desc, len(h.messages()))
	return message{}, 0
}

// value returns the named metric of a message (resolving aliases), and whether present.
func (h *hostClient) value(m message, name sparkplug.MetricName) (any, bool) {
	if m.payload == nil {
		return nil, false
	}
	for _, mt := range m.payload.Metrics {
		n, dt := sparkplug.MetricName(mt.GetName()), pb.DataType(mt.GetDatatype())
		if mt.Name == nil {
			var ok bool
			n, dt, ok = h.aliases.Resolve(m.topic.DeviceID, mt.GetAlias())
			if !ok {
				continue
			}
		}
		if n == name {
			v, err := sparkplug.Value(mt, dt)
			if err != nil {
				h.t.Errorf("value %s: %v", name, err)
			}
			return v, true
		}
	}
	return nil, false
}

func (h *hostClient) is(m message, mt sparkplug.MessageType, device string) bool {
	return m.topic.Type == mt && m.topic.DeviceID == device
}

func (h *hostClient) hasValue(m message, name sparkplug.MetricName, want any) bool {
	v, ok := h.value(m, name)
	return ok && v == want
}

func (h *hostClient) publishState(online bool, ts uint64) {
	topic, _ := sparkplug.StateTopic(sparkplug.PrimaryHostID)
	tok := h.client.Publish(topic, 1, true, sparkplug.EncodeState(sparkplug.HostState{Online: online, Timestamp: ts}))
	tok.WaitTimeout(5 * time.Second)
}

func (h *hostClient) command(device string, cmd sparkplug.DeviceCommand) {
	h.t.Helper()
	p, err := sparkplug.BuildDeviceCommand(cmd, time.Now())
	if err != nil {
		// still send invalid commands built by hand to exercise rejection
		p = &pb.Payload{}
		for name, v := range map[sparkplug.MetricName]string{sparkplug.MetricCommandID: cmd.ID, sparkplug.MetricCommandName: string(cmd.Name)} {
			m, _ := sparkplug.NewMetric(name, pb.DataType_String, v, time.Now())
			p.Metrics = append(p.Metrics, m)
		}
	}
	b, _ := sparkplug.Encode(p)
	topic, _ := sparkplug.DeviceTopic(testGroup, sparkplug.DCMD, testEdge, device)
	h.client.Publish(topic, 0, false, b).WaitTimeout(5 * time.Second)
}

func (h *hostClient) rebirth() {
	m, _ := sparkplug.NewMetric(sparkplug.MetricNodeControlRebirth, pb.DataType_Boolean, true, time.Now())
	b, _ := sparkplug.Encode(&pb.Payload{Timestamp: m.Timestamp, Metrics: []*pb.Payload_Metric{m}})
	topic, _ := sparkplug.NodeTopic(testGroup, sparkplug.NCMD, testEdge)
	h.client.Publish(topic, 0, false, b).WaitTimeout(5 * time.Second)
}

// checkSeq asserts that every message of the edge node after an NBIRTH carries
// the next sequence number (wrapping at 255) and NDEATH carries none.
func (h *hostClient) checkSeq() {
	h.t.Helper()
	var prev uint64
	born := false
	for _, m := range h.messages() {
		switch m.topic.Type {
		case sparkplug.STATE, sparkplug.DCMD, sparkplug.NCMD:
			continue
		case sparkplug.NDEATH:
			if m.payload.Seq != nil {
				h.t.Error("NDEATH carries a seq")
			}
			born = false
			continue
		case sparkplug.NBIRTH:
			if m.payload.GetSeq() > 255 {
				h.t.Errorf("NBIRTH seq %d", m.payload.GetSeq())
			}
			prev, born = m.payload.GetSeq(), true
			continue
		}
		if !born {
			h.t.Errorf("%s before NBIRTH", m.topic)
			continue
		}
		if !sparkplug.SeqFollows(prev, m.payload.GetSeq()) {
			h.t.Errorf("%s seq %d does not follow %d", m.topic, m.payload.GetSeq(), prev)
		}
		prev = m.payload.GetSeq()
	}
}

func poolFrom(t *testing.T, pemBytes []byte) *x509.CertPool {
	t.Helper()
	p := x509.NewCertPool()
	if !p.AppendCertsFromPEM(pemBytes) {
		t.Fatal("bad CA PEM")
	}
	return p
}

// rig is a running edge node with simulators, broker and host.
type rig struct {
	t        *testing.T
	broker   *testBroker
	host     *hostClient
	voron    *simulator.Moonraker
	bambu    *simulator.Bambu
	files    *httptest.Server
	artMu    sync.Mutex
	artifact map[string][]byte
	node     *Node
	cfg      Config
	cancel   context.CancelFunc
	done     chan struct{}
}

func newRig(t *testing.T, mutate func(*Config)) *rig {
	t.Helper()
	r := &rig{t: t, artifact: map[string][]byte{}}
	r.broker = startBroker(t)
	r.host = startHost(t, r.broker)

	r.voron = simulator.NewMoonraker("moonraker-key")
	r.voron.Start()
	t.Cleanup(r.voron.Close)
	r.voron.SetSpoolMaterial("PLA")
	r.bambu = simulator.NewBambu("01S00SIM", "lan-code")
	if err := r.bambu.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.bambu.Close)
	r.bambu.SetTrays("PLA", "PETG", "", "TPU-AMS")
	r.bambu.SetExternalSpool("ABS")

	r.files = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.artMu.Lock()
		b, ok := r.artifact[req.URL.Path]
		r.artMu.Unlock()
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(r.files.Close)

	dir := t.TempDir()
	pw := filepath.Join(dir, "mqtt-password")
	if err := os.WriteFile(pw, []byte(edgePassword+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	vhost, vport := r.voron.HostPort()
	mqttPort, ftpsPort := r.bambu.Ports()
	r.cfg = Config{
		Enabled: true, GroupID: testGroup, EdgeNodeID: testEdge,
		BrokerURL: fmt.Sprintf("ssl://127.0.0.1:%d", r.broker.port), TLSCAFile: r.broker.caFile, TLSServerName: "broker.test",
		ClientID: "edge-site-north", Username: edgeUser, PasswordFile: pw, StateDir: filepath.Join(dir, "state"),
		PollInterval: 100 * time.Millisecond, OfflineAfter: 2, ReconnectDelay: 200 * time.Millisecond,
		Devices: []DeviceConfig{
			{DeviceID: "VORON-01", Definition: "voron_2_4", Host: vhost, Port: vport},
			{DeviceID: "A1-01", Definition: "bambu_a1", Host: r.bambu.Host(), Port: mqttPort, FTPSPort: ftpsPort,
				Serial: "01S00SIM", TLSPinSHA256: r.bambu.CertSHA256()},
		},
	}
	if mutate != nil {
		mutate(&r.cfg)
	}
	log := logrus.New()
	log.SetOutput(io.Discard)
	reg := registry.NewRegistry()
	node, err := NewNode(r.cfg, reg, manager.NewManager(reg, nil, log), log, Options{
		ArtifactClient: r.files.Client(),
		Secrets:        map[string]string{"VORON-01": "moonraker-key", "A1-01": "lan-code"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.node = node
	return r
}

func (r *rig) start() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel, r.done = cancel, make(chan struct{})
	go func() {
		defer close(r.done)
		_ = r.node.Run(ctx)
	}()
	r.t.Cleanup(r.stop)
}

func (r *rig) stop() {
	if r.cancel == nil {
		return
	}
	r.cancel()
	select {
	case <-r.done:
	case <-time.After(10 * time.Second):
		r.t.Error("edge node did not stop")
	}
	r.cancel = nil
}

// serve publishes an artifact on the local HTTPS server and returns its URL.
func (r *rig) serve(path string, body []byte) string {
	r.artMu.Lock()
	r.artifact[path] = body
	r.artMu.Unlock()
	return r.files.URL + path
}
