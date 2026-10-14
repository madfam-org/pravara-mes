package edge

// End to end: pravara's Sparkplug primary host (packages/sparkplug/host, the
// engine the telemetry worker runs) and this edge node, on the in-process TLS
// broker. The broker authenticates and authorizes with brokerauth, the code
// behind pravara-api's /v1/mqtt/auth and /v1/mqtt/acl, against a credential
// the edge node enrolled for itself. Covered: enrollment without a
// person-held secret, STATE, births, DDATA, a DCMD round trip, rebirth after
// a sequence gap, a command re-sent after DBIRTH, deaths, quarantine of an
// unregistered device and the ACL refusing cross-tenant topics.

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	"github.com/madfam-org/pravara-mes/packages/sparkplug/brokerauth"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/host"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/host/hosttest"
)

const (
	e2eHostUser   = "pravara-mes-host" // the host's own credential (EMQX built-in database)
	e2eOtherGroup = "globex"
)

// memCredentials is the edge-node credential registry (edge_nodes).
type memCredentials struct {
	mu    sync.Mutex
	creds map[string]*brokerauth.Credential
}

func (m *memCredentials) LookupCredential(_ context.Context, u string) (*brokerauth.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.creds[u]
	if !ok {
		return nil, nil
	}
	cc := *c
	return &cc, nil
}

func (m *memCredentials) put(c brokerauth.Credential) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creds[c.Username] = &c
}

// fakeEnrollmentAPI serves pravara-api's enrollment routes (same shapes):
// POST /v1/edge/enrollments and GET /v1/edge/enrollments/{id}. approve()
// plays the tenant admin approving the user code.
type fakeEnrollmentAPI struct {
	srv   *httptest.Server
	creds *memCredentials
	mu    sync.Mutex
	rows  map[string]*brokerauth.Credential
	state map[string]string
	codes map[string]string // user code -> enrollment id
	posts int
}

func newFakeEnrollmentAPI(creds *memCredentials) *fakeEnrollmentAPI {
	f := &fakeEnrollmentAPI{creds: creds, rows: map[string]*brokerauth.Credential{}, state: map[string]string{}, codes: map[string]string{}}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/edge/enrollments":
			var req struct{ GroupID, EdgeNodeID, Password string }
			_ = json.NewDecoder(r.Body).Decode(&struct {
				GroupID    *string `json:"group_id"`
				EdgeNodeID *string `json:"edge_node_id"`
				Password   *string `json:"password"`
			}{&req.GroupID, &req.EdgeNodeID, &req.Password})
			hash, err := brokerauth.HashPassword(req.Password, 4)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.posts++
			id, code := fmt.Sprintf("00000000-0000-4000-8000-%012d", f.posts), fmt.Sprintf("BCDF-GH%02d", f.posts)
			f.rows[id] = &brokerauth.Credential{Username: sparkplug.EdgeNodeUsername(req.GroupID, req.EdgeNodeID),
				Group: req.GroupID, EdgeNodeID: req.EdgeNodeID, PasswordHash: hash}
			f.state[id], f.codes[code] = "pending", id
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"enrollment_id": id, "status": "pending", "user_code": code,
				"expires_at": time.Now().Add(15 * time.Minute), "poll_interval_seconds": 1})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/edge/enrollments/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/edge/enrollments/")
			st, ok := f.state[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"enrollment_id": id, "status": st})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return f
}

func (f *fakeEnrollmentAPI) approve(code string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.codes[code]
	if !ok || f.state[id] != "pending" {
		return false
	}
	f.state[id] = "approved"
	f.creds.put(*f.rows[id])
	return true
}

// e2eHook is the broker's HTTP auth/ACL as EMQX would apply it: edge-node
// credentials are decided by brokerauth; usernames it ignores fall through to
// the next authenticator, which knows only the host credential.
type e2eHook struct {
	mqtt.HookBase
	authz    *brokerauth.Authorizer
	mu       sync.Mutex
	denied   []string
	dropData atomic.Int32 // drop the next n DDATA publishes of edge nodes
}

func (h *e2eHook) ID() string { return "mes1-http-auth" }

func (h *e2eHook) Provides(b byte) bool {
	return bytes.Contains([]byte{mqtt.OnConnectAuthenticate, mqtt.OnACLCheck, mqtt.OnPublish}, []byte{b})
}

func (h *e2eHook) OnConnectAuthenticate(_ *mqtt.Client, pk packets.Packet) bool {
	d, err := h.authz.Authenticate(context.Background(), string(pk.Connect.Username), string(pk.Connect.Password))
	return err == nil && (d == brokerauth.Allow || (d == brokerauth.Ignore && string(pk.Connect.Username) == e2eHostUser))
}

func (h *e2eHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	user := string(cl.Properties.Username)
	action := sparkplug.ACLSubscribe
	if write {
		action = sparkplug.ACLPublish
	}
	d, err := h.authz.Authorize(context.Background(), user, action, topic)
	if err == nil && (d == brokerauth.Allow || (d == brokerauth.Ignore && user == e2eHostUser)) {
		return true
	}
	h.mu.Lock()
	h.denied = append(h.denied, user+" "+string(action)+" "+topic)
	h.mu.Unlock()
	return false
}

func (h *e2eHook) OnPublish(cl *mqtt.Client, pk packets.Packet) (packets.Packet, error) {
	if string(cl.Properties.Username) != e2eHostUser && strings.Contains(pk.TopicName, "/DDATA/") && h.dropData.Load() > 0 {
		h.dropData.Add(-1)
		return pk, packets.ErrRejectPacket
	}
	return pk, nil
}

func (h *e2eHook) deniedList() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.denied...)
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func count(events []string, ev string) int {
	n := 0
	for _, e := range events {
		if e == ev {
			n++
		}
	}
	return n
}

func TestPrimaryHostAndEdgeNodeEndToEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	// --- broker (TLS) with the HTTP auth/ACL decisions
	creds := &memCredentials{creds: map[string]*brokerauth.Credential{}}
	hook := &e2eHook{authz: &brokerauth.Authorizer{Store: creds}}
	cert, err := simulator.SelfSigned("broker.test")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, cert.PEM, 0o600); err != nil {
		t.Fatal(err)
	}
	port, err := simulator.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	server := mqtt.New(&mqtt.Options{Logger: quiet})
	if err := server.AddHook(hook, nil); err != nil {
		t.Fatal(err)
	}
	if err := server.AddListener(listeners.NewTCP(listeners.Config{ID: "tls", Address: fmt.Sprintf("127.0.0.1:%d", port),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert.TLS}, MinVersion: tls.VersionTLS12}})); err != nil {
		t.Fatal(err)
	}
	if err := server.Serve(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	brokerURL := fmt.Sprintf("ssl://127.0.0.1:%d", port)
	pool, _ := os.ReadFile(caFile)
	tlsCfg := &tls.Config{ServerName: "broker.test", MinVersion: tls.VersionTLS12, RootCAs: poolFrom(t, pool)}

	// --- 1. enrollment: the box generates and registers its own credential
	api := newFakeEnrollmentAPI(creds)
	t.Cleanup(api.srv.Close)
	stateDir := filepath.Join(dir, "state")
	pwFile := filepath.Join(stateDir, "mqtt_password")
	voron := simulator.NewMoonraker("moonraker-key")
	voron.Start()
	t.Cleanup(voron.Close)
	voron.SetSpoolMaterial("PLA")
	rogue := simulator.NewMoonraker("")
	rogue.Start()
	t.Cleanup(rogue.Close)
	vh, vp := voron.HostPort()
	rh, rp := rogue.HostPort()
	cfg := Config{
		Enabled: true, GroupID: testGroup, EdgeNodeID: testEdge, BrokerURL: brokerURL, TLSCAFile: caFile,
		TLSServerName: "broker.test", ClientID: "edge-e2e", PasswordFile: pwFile, StateDir: stateDir,
		EnrollmentURL: api.srv.URL, PollInterval: 100 * time.Millisecond, OfflineAfter: 2, ReconnectDelay: 200 * time.Millisecond,
		Devices: []DeviceConfig{
			{DeviceID: "VORON-01", Definition: "voron_2_4", Host: vh, Port: vp},
			{DeviceID: "ROGUE-01", Definition: "voron_2_4", Host: rh, Port: rp},
		},
	}
	var out bytes.Buffer
	enrolled := make(chan error, 1)
	go func() {
		enrolled <- Enroll(ctx, cfg, EnrollOptions{Client: api.srv.Client(), Out: &out, PollInterval: 50 * time.Millisecond})
	}()
	eventually(t, "the box to register", func() bool { api.mu.Lock(); defer api.mu.Unlock(); return api.posts == 1 })
	if d, _ := hook.authz.Authenticate(ctx, sparkplug.EdgeNodeUsername(testGroup, testEdge), "anything-at-all-anything-at-all-00"); d != brokerauth.Ignore {
		t.Fatalf("a pending enrollment must not authenticate: %s", d)
	}
	if !api.approve("BCDF-GH01") {
		t.Fatal("approval failed")
	}
	if err := <-enrolled; err != nil {
		t.Fatalf("enrollment: %v", err)
	}
	password, err := readSecret(pwFile)
	if err != nil || len(password) != 43 {
		t.Fatalf("generated credential: %v (len %d)", err, len(password))
	}
	if strings.Contains(out.String(), password) || !strings.Contains(out.String(), "BCDF-GH01") {
		t.Fatalf("enrollment output must show the user code and never the credential: %q", out.String())
	}
	if fi, _ := os.Stat(pwFile); fi.Mode().Perm() != 0o600 {
		t.Fatalf("credential file mode %v", fi.Mode().Perm())
	}

	// --- 2. the primary host, with an in-memory registry
	store := hosttest.New()
	store.RegisterNode("tenant-acme", testGroup, testEdge)
	store.RegisterDevice(testGroup, testEdge, "VORON-01") // ROGUE-01 is not registered
	var evMu sync.Mutex
	var events []string
	engine := host.New(host.Options{Store: store, Logger: quiet, RebirthInterval: 200 * time.Millisecond,
		Observe: func(e string) { evMu.Lock(); events = append(events, e); evMu.Unlock() }})
	seen := func() []string { evMu.Lock(); defer evMu.Unlock(); return append([]string(nil), events...) }

	// An observer of STATE (a user the host-side authenticator knows).
	stateTopic, _ := sparkplug.StateTopic(sparkplug.PrimaryHostID)
	var stMu sync.Mutex
	var states []sparkplug.HostState
	obs := paho.NewClient(paho.NewClientOptions().AddBroker(brokerURL).SetClientID("state-observer").
		SetUsername(e2eHostUser).SetTLSConfig(tlsCfg).SetAutoReconnect(false))
	if tok := obs.Connect(); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("observer connect: %v", tok.Error())
	}
	t.Cleanup(func() { obs.Disconnect(50) })
	obs.Subscribe(stateTopic, 1, func(_ paho.Client, m paho.Message) {
		if s, err := sparkplug.DecodeState(m.Payload()); err == nil {
			stMu.Lock()
			states = append(states, s)
			stMu.Unlock()
		}
	}).WaitTimeout(5 * time.Second)

	hostCtx, stopHost := context.WithCancel(ctx)
	hostDone := make(chan struct{})
	go func() {
		defer close(hostDone)
		_ = engine.Run(hostCtx, host.ClientConfig{BrokerURL: brokerURL, ClientID: "pravara-mes-host", Username: e2eHostUser,
			Password: "host-test-password", TLSConfig: tlsCfg, TickInterval: 200 * time.Millisecond})
	}()
	eventually(t, "online STATE", func() bool { stMu.Lock(); defer stMu.Unlock(); return len(states) > 0 && states[len(states)-1].Online })
	stMu.Lock()
	onlineTS := states[len(states)-1].Timestamp
	stMu.Unlock()

	// --- 3. the edge node with its enrolled credential
	log := logrus.New()
	log.SetOutput(io.Discard)
	reg := registry.NewRegistry()
	files := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("; e2e\nG28\n"))
	}))
	t.Cleanup(files.Close)
	node, err := NewNode(cfg, reg, manager.NewManager(reg, nil, log), log, Options{
		ArtifactClient: files.Client(), Secrets: map[string]string{"VORON-01": "moonraker-key"}})
	if err != nil {
		t.Fatal(err)
	}
	nodeCtx, stopNode := context.WithCancel(ctx)
	nodeDone := make(chan struct{})
	go func() { defer close(nodeDone); _ = node.Run(nodeCtx) }()

	voronKey := hosttest.DeviceKey(testGroup, testEdge, "VORON-01")
	rogueKey := hosttest.DeviceKey(testGroup, testEdge, "ROGUE-01")
	eventually(t, "NBIRTH and DBIRTH", func() bool { return store.NodeIsOnline(testGroup+"/"+testEdge) && store.Online(voronKey) })
	eventually(t, "quarantine of the unregistered device", func() bool { return store.QuarantineCount(rogueKey) >= 1 })
	if store.Online(rogueKey) {
		t.Fatal("unregistered device trusted")
	}
	live := store.Snapshot(voronKey)
	if live[sparkplug.MetricStateStatus] != "idle" || live[sparkplug.MaterialSlotClassMetric(1)] != "pla" ||
		live[sparkplug.CapabilityMaxHotendTempC.Metric()] == nil {
		t.Fatalf("DBIRTH live state %v", live)
	}

	// --- 4. DDATA reaches the live state
	voron.SetTemps(215.2, 60.1)
	eventually(t, "DDATA temperature", func() bool { return store.Snapshot(voronKey)[sparkplug.MetricTempsHotend] == 215.0 })

	// --- 5. DCMD round trip: start_job -> accepted/running/done -> Job complete
	gcode := []byte("; e2e\nG28\n")
	target := host.Target{Group: testGroup, EdgeNodeID: testEdge, DeviceID: "VORON-01"}
	d, err := engine.SendDeviceCommand(target, sparkplug.DeviceCommand{ID: "cmd-e2e-1", Name: sparkplug.CommandStartJob, TaskID: "task-e2e",
		ArtifactURL: files.URL + "/part.gcode", ArtifactSHA256: digest(gcode), ArtifactMediaType: "text/x-gcode"})
	if err != nil || d != host.Published {
		t.Fatalf("DCMD: %v %v", d, err)
	}
	eventually(t, "Command done", func() bool {
		s := store.Snapshot(voronKey)
		return s[sparkplug.MetricCommandLastID] == "cmd-e2e-1" && s[sparkplug.MetricCommandStatus] == "done"
	})
	eventually(t, "Job printing", func() bool { return store.Snapshot(voronKey)[sparkplug.MetricJobStatus] == "printing" })
	for i := 0; i < 4; i++ {
		voron.Tick()
	}
	eventually(t, "Job complete", func() bool {
		s := store.Snapshot(voronKey)
		return s[sparkplug.MetricJobStatus] == "complete" && s[sparkplug.MetricJobID] == "task-e2e"
	})

	// --- 6. rebirth after a sequence gap (one DDATA lost in transit)
	births := count(store.EventLog(), "NBIRTH "+testGroup+"/"+testEdge)
	hook.dropData.Store(1)
	voron.SetTemps(180.0, 50.0)
	eventually(t, "the dropped DDATA", func() bool { return hook.dropData.Load() == 0 })
	voron.SetTemps(170.0, 45.0)
	eventually(t, "rebirth after the gap", func() bool {
		return count(seen(), "rebirth_requested") >= 1 && count(store.EventLog(), "NBIRTH "+testGroup+"/"+testEdge) > births
	})
	eventually(t, "state after the rebirth", func() bool {
		return store.Online(voronKey) && store.Snapshot(voronKey)[sparkplug.MetricTempsHotend] == 170.0
	})

	// --- 7. ACL: the edge credential cannot reach another tenant's namespace
	spy := paho.NewClient(paho.NewClientOptions().AddBroker(brokerURL).SetClientID("edge-e2e-spy").
		SetUsername(sparkplug.EdgeNodeUsername(testGroup, testEdge)).SetPassword(password).SetTLSConfig(tlsCfg).SetAutoReconnect(false))
	if tok := spy.Connect(); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("spy connect: %v", tok.Error())
	}
	foreignDCMD := sparkplug.Namespace + "/" + e2eOtherGroup + "/DCMD/site-south/+"
	sub := spy.Subscribe(foreignDCMD, 1, func(paho.Client, paho.Message) { t.Error("cross-tenant DCMD delivered") })
	sub.WaitTimeout(5 * time.Second)
	if st, ok := sub.(*paho.SubscribeToken); ok {
		if code := st.Result()[foreignDCMD]; code < 0x80 {
			t.Fatalf("cross-tenant subscription granted (code %#x)", code)
		}
	}
	foreignBirth, _ := sparkplug.NodeTopic(e2eOtherGroup, sparkplug.NBIRTH, testEdge)
	spy.Publish(foreignBirth, 0, false, []byte{0}).WaitTimeout(5 * time.Second)
	spy.Disconnect(50)
	eventually(t, "ACL denials", func() bool {
		var sub, pub bool
		for _, dn := range hook.deniedList() {
			sub = sub || strings.HasSuffix(dn, "subscribe "+foreignDCMD)
			pub = pub || strings.HasSuffix(dn, "publish "+foreignBirth)
		}
		return sub && pub
	})
	for _, e := range store.EventLog() {
		if strings.Contains(e, e2eOtherGroup) {
			t.Fatalf("cross-tenant message reached the host: %s", e)
		}
	}
	bad := paho.NewClient(paho.NewClientOptions().AddBroker(brokerURL).SetClientID("edge-e2e-bad").
		SetUsername(sparkplug.EdgeNodeUsername(testGroup, testEdge)).SetPassword("not-the-enrolled-credential-xxxxxxxxxx").
		SetTLSConfig(tlsCfg).SetAutoReconnect(false).SetConnectRetry(false))
	if tok := bad.Connect(); tok.WaitTimeout(5*time.Second) && tok.Error() == nil {
		t.Fatal("a wrong password was accepted")
	}

	// --- 8. deaths: DDEATH on device loss; a command for the dead device is
	// deferred and re-sent after its DBIRTH with the same id
	voron.SetDown(true)
	eventually(t, "DDEATH", func() bool { return !store.Online(voronKey) })
	d, err = engine.SendDeviceCommand(target, sparkplug.DeviceCommand{ID: "cmd-e2e-2", Name: sparkplug.CommandPause})
	if err != nil || d != host.Deferred {
		t.Fatalf("command to a dead device: %v %v", d, err)
	}
	store.QueueResend(testGroup, testEdge, "VORON-01", sparkplug.DeviceCommand{ID: "cmd-e2e-2", Name: sparkplug.CommandPause})
	voron.SetDown(false)
	eventually(t, "re-sent command applied after DBIRTH", func() bool {
		return store.Online(voronKey) && store.Snapshot(voronKey)[sparkplug.MetricCommandLastID] == "cmd-e2e-2"
	})
	if count(seen(), "command_resent") < 1 {
		t.Fatal("re-send not observed")
	}

	// NDEATH: the connection drops; the broker publishes the will.
	cl, ok := server.Clients.Get("edge-e2e")
	if !ok {
		t.Fatal("edge client not connected")
	}
	cl.Stop(fmt.Errorf("test: network loss"))
	eventually(t, "NDEATH", func() bool { return count(store.EventLog(), "NDEATH "+testGroup+"/"+testEdge) >= 1 })
	if store.Online(voronKey) {
		t.Fatal("NDEATH left the device online")
	}
	eventually(t, "reconnect and NBIRTH", func() bool { return store.NodeIsOnline(testGroup+"/"+testEdge) && store.Online(voronKey) })
	if len(store.TouchedDevices()) == 0 {
		t.Fatal("device liveness was never refreshed")
	}

	// --- 9. shutdown: edge NDEATH, then host offline STATE with its session timestamp
	stopNode()
	<-nodeDone
	stopHost()
	<-hostDone
	eventually(t, "offline STATE", func() bool { stMu.Lock(); defer stMu.Unlock(); return !states[len(states)-1].Online })
	stMu.Lock()
	last := states[len(states)-1]
	stMu.Unlock()
	if last.Timestamp != onlineTS {
		t.Fatalf("offline STATE timestamp %d != online %d", last.Timestamp, onlineTS)
	}
	for _, dn := range hook.deniedList() {
		if !strings.Contains(dn, e2eOtherGroup) {
			t.Errorf("the edge node or host stepped outside the ACL: %s", dn)
		}
	}
}
