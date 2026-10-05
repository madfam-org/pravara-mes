package host_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/host"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/host/hosttest"
	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

const (
	group  = "acme"
	edge   = "site-north"
	device = "VORON-01"
)

type sent struct {
	topic    string
	qos      byte
	retained bool
	payload  []byte
}

type fakePub struct {
	mu   sync.Mutex
	msgs []sent
}

func (f *fakePub) Publish(topic string, qos byte, retained bool, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, sent{topic, qos, retained, payload})
	return nil
}

func (f *fakePub) on(prefix string) []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sent
	for _, m := range f.msgs {
		if strings.HasPrefix(m.topic, prefix) {
			out = append(out, m)
		}
	}
	return out
}

// edgeSim builds the messages an edge node would publish.
type edgeSim struct {
	t       *testing.T
	aliases *sparkplug.AliasTable
	seq     uint64
	bdSeq   uint64
}

func newEdgeSim(t *testing.T, bdSeq uint64) *edgeSim {
	return &edgeSim{t: t, aliases: sparkplug.NewAliasTable(), bdSeq: bdSeq}
}

func (s *edgeSim) enc(p *pb.Payload, err error) []byte {
	s.t.Helper()
	if err != nil {
		s.t.Fatal(err)
	}
	b, err := sparkplug.Encode(p)
	if err != nil {
		s.t.Fatal(err)
	}
	return b
}

func (s *edgeSim) nbirth() []byte {
	s.aliases = sparkplug.NewAliasTable()
	s.seq = 0
	return s.enc(sparkplug.NodeBirth(s.bdSeq, 0, time.Now(), s.aliases))
}

func (s *edgeSim) next() uint64 { s.seq = (s.seq + 1) % 256; return s.seq }

func metric(t *testing.T, name sparkplug.MetricName, v any) *pb.Payload_Metric {
	t.Helper()
	m, err := sparkplug.NewContractMetric(name, v, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (s *edgeSim) dbirth(dev string, status string) []byte {
	ms := []*pb.Payload_Metric{
		metric(s.t, sparkplug.MetricStateStatus, status),
		metric(s.t, sparkplug.MetricTempsHotend, 21.5),
		metric(s.t, sparkplug.MaterialSlotClassMetric(1), "pla"),
	}
	return s.enc(sparkplug.DeviceBirth(dev, s.next(), time.Now(), s.aliases, ms))
}

func (s *edgeSim) ddata(dev string, name sparkplug.MetricName, v any) []byte {
	return s.enc(sparkplug.DeviceData(dev, s.next(), time.Now(), s.aliases, []*pb.Payload_Metric{metric(s.t, name, v)}))
}

func newEngine(t *testing.T) (*host.Engine, *hosttest.MemStore, *fakePub, *[]string) {
	t.Helper()
	store := hosttest.New()
	store.RegisterNode("tenant-a", group, edge)
	store.RegisterDevice(group, edge, device)
	var mu sync.Mutex
	events := &[]string{}
	e := host.New(host.Options{
		Store:  store,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Observe: func(ev string) {
			mu.Lock()
			*events = append(*events, ev)
			mu.Unlock()
		},
	})
	pub := &fakePub{}
	host.AttachForTest(e, pub, 1000)
	return e, store, pub, events
}

func topic(mt sparkplug.MessageType, dev string) string {
	if dev == "" {
		s, _ := sparkplug.NodeTopic(group, mt, edge)
		return s
	}
	s, _ := sparkplug.DeviceTopic(group, mt, edge, dev)
	return s
}

func has(events []string, ev string) bool {
	for _, e := range events {
		if e == ev {
			return true
		}
	}
	return false
}

func TestBirthDataAndAliases(t *testing.T) {
	e, store, _, _ := newEngine(t)
	ctx := context.Background()
	sim := newEdgeSim(t, 3)
	e.HandleMessage(ctx, topic(sparkplug.NBIRTH, ""), sim.nbirth(), time.Now())
	e.HandleMessage(ctx, topic(sparkplug.DBIRTH, device), sim.dbirth(device, "idle"), time.Now())
	e.HandleMessage(ctx, topic(sparkplug.DDATA, device), sim.ddata(device, sparkplug.MetricTempsHotend, 210.0), time.Now())

	k := hosttest.DeviceKey(group, edge, device)
	if !store.Online(k) {
		t.Fatal("device not online after DBIRTH")
	}
	live := store.Snapshot(k)
	if live[sparkplug.MetricTempsHotend] != 210.0 || live[sparkplug.MetricStateStatus] != "idle" ||
		live[sparkplug.MaterialSlotClassMetric(1)] != "pla" {
		t.Fatalf("live state %v", live)
	}
	reports := store.ReportsFor(k)
	if len(reports) != 2 || !reports[0].Birth || reports[1].Birth || reports[1].BdSeq != 3 {
		t.Fatalf("reports %+v", reports)
	}
}

func TestSeqGapRequestsRebirthAndDropsUntilNBIRTH(t *testing.T) {
	e, store, pub, events := newEngine(t)
	ctx := context.Background()
	sim := newEdgeSim(t, 0)
	e.HandleMessage(ctx, topic(sparkplug.NBIRTH, ""), sim.nbirth(), time.Now())
	e.HandleMessage(ctx, topic(sparkplug.DBIRTH, device), sim.dbirth(device, "idle"), time.Now())
	sim.next() // a message is lost
	e.HandleMessage(ctx, topic(sparkplug.DDATA, device), sim.ddata(device, sparkplug.MetricTempsHotend, 50.0), time.Now())

	ncmd := pub.on(topic(sparkplug.NCMD, ""))
	if len(ncmd) != 1 || ncmd[0].qos != 0 || ncmd[0].retained {
		t.Fatalf("expected one rebirth NCMD, got %d", len(ncmd))
	}
	p, err := sparkplug.Decode(ncmd[0].payload)
	if err != nil || !sparkplug.ParseRebirthRequest(p, nil) || p.Seq != nil {
		t.Fatalf("NCMD is not a rebirth request without seq: %v", err)
	}
	if !has(*events, "seq_gap") {
		t.Fatal("seq gap not observed")
	}
	// Data after the gap is not applied, and a second request is throttled.
	e.HandleMessage(ctx, topic(sparkplug.DDATA, device), sim.ddata(device, sparkplug.MetricTempsHotend, 60.0), time.Now())
	if v := store.Snapshot(hosttest.DeviceKey(group, edge, device))[sparkplug.MetricTempsHotend]; v != 21.5 {
		t.Fatalf("data after a gap was applied: %v", v)
	}
	if len(pub.on(topic(sparkplug.NCMD, ""))) != 1 || !has(*events, "rebirth_suppressed") {
		t.Fatal("rebirth requests are not throttled")
	}
	// Rebirth restores the session.
	e.HandleMessage(ctx, topic(sparkplug.NBIRTH, ""), sim.nbirth(), time.Now())
	e.HandleMessage(ctx, topic(sparkplug.DBIRTH, device), sim.dbirth(device, "printing"), time.Now())
	if v := store.Snapshot(hosttest.DeviceKey(group, edge, device))[sparkplug.MetricStateStatus]; v != "printing" {
		t.Fatalf("rebirth not applied: %v", v)
	}
}

func TestDataBeforeBirthRequestsRebirth(t *testing.T) {
	e, _, pub, _ := newEngine(t)
	sim := newEdgeSim(t, 0)
	sim.nbirth()
	sim.dbirth(device, "idle")
	e.HandleMessage(context.Background(), topic(sparkplug.DDATA, device), sim.ddata(device, sparkplug.MetricTempsHotend, 1.0), time.Now())
	if len(pub.on(topic(sparkplug.NCMD, ""))) != 1 {
		t.Fatal("no rebirth request for data before NBIRTH")
	}
}

func TestDeathsAndStaleNDEATH(t *testing.T) {
	e, store, _, events := newEngine(t)
	ctx := context.Background()
	sim := newEdgeSim(t, 7)
	e.HandleMessage(ctx, topic(sparkplug.NBIRTH, ""), sim.nbirth(), time.Now())
	e.HandleMessage(ctx, topic(sparkplug.DBIRTH, device), sim.dbirth(device, "idle"), time.Now())
	k := hosttest.DeviceKey(group, edge, device)

	e.HandleMessage(ctx, topic(sparkplug.DDEATH, device), sim.enc(sparkplug.DeviceDeath(sim.next(), time.Now()), nil), time.Now())
	if store.Online(k) {
		t.Fatal("DDEATH did not take the device offline")
	}
	e.HandleMessage(ctx, topic(sparkplug.DBIRTH, device), sim.dbirth(device, "idle"), time.Now())

	// An NDEATH from an older connection (other bdSeq) is ignored.
	e.HandleMessage(ctx, topic(sparkplug.NDEATH, ""), sim.enc(sparkplug.NodeDeath(6, time.Now())), time.Now())
	if !store.Online(k) || !has(*events, "stale_ndeath") {
		t.Fatal("stale NDEATH was applied")
	}
	e.HandleMessage(ctx, topic(sparkplug.NDEATH, ""), sim.enc(sparkplug.NodeDeath(7, time.Now())), time.Now())
	if store.Online(k) || store.NodeIsOnline(group+"/"+edge) {
		t.Fatal("NDEATH did not take the node and its devices offline")
	}
}

func TestUnregisteredDeviceIsQuarantined(t *testing.T) {
	e, store, _, _ := newEngine(t)
	ctx := context.Background()
	sim := newEdgeSim(t, 0)
	e.HandleMessage(ctx, topic(sparkplug.NBIRTH, ""), sim.nbirth(), time.Now())
	e.HandleMessage(ctx, topic(sparkplug.DBIRTH, "ROGUE"), sim.dbirth("ROGUE", "idle"), time.Now())
	e.HandleMessage(ctx, topic(sparkplug.DDATA, "ROGUE"), sim.ddata("ROGUE", sparkplug.MetricTempsHotend, 99.0), time.Now())
	k := hosttest.DeviceKey(group, edge, "ROGUE")
	if store.QuarantineCount(k) != 1 || store.Online(k) || len(store.ReportsFor(k)) != 0 {
		t.Fatal("unregistered device was trusted")
	}
}

func TestUnregisteredEdgeNodeIsIgnored(t *testing.T) {
	e, store, pub, events := newEngine(t)
	ctx := context.Background()
	sim := newEdgeSim(t, 0)
	other, _ := sparkplug.NodeTopic("other-tenant", sparkplug.NBIRTH, edge)
	e.HandleMessage(ctx, other, sim.nbirth(), time.Now())
	if !has(*events, "unknown_edge_node") || len(store.EventLog()) != 0 || len(pub.on("spBv1.0/other-tenant")) != 0 {
		t.Fatal("NBIRTH under an unregistered group was accepted")
	}
}

func TestCommandsAreSentOnlyToBornDevicesAndResentAfterDBIRTH(t *testing.T) {
	e, store, pub, _ := newEngine(t)
	ctx := context.Background()
	target := host.Target{Group: group, EdgeNodeID: edge, DeviceID: device}
	cmd := sparkplug.DeviceCommand{ID: "cmd-1", Name: sparkplug.CommandPause}

	if d, err := e.SendDeviceCommand(target, cmd); err != nil || d != host.Deferred {
		t.Fatalf("command to an unborn device: %v %v", d, err)
	}
	store.QueueResend(group, edge, device, cmd)
	sim := newEdgeSim(t, 0)
	e.HandleMessage(ctx, topic(sparkplug.NBIRTH, ""), sim.nbirth(), time.Now())
	e.HandleMessage(ctx, topic(sparkplug.DBIRTH, device), sim.dbirth(device, "idle"), time.Now())
	dcmd := pub.on(topic(sparkplug.DCMD, device))
	if len(dcmd) != 1 || dcmd[0].qos != 0 || dcmd[0].retained {
		t.Fatalf("expected the deferred command re-sent once after DBIRTH, got %d", len(dcmd))
	}
	p, _ := sparkplug.Decode(dcmd[0].payload)
	got, err := sparkplug.ParseDeviceCommand(p, nil)
	if err != nil || got.ID != "cmd-1" || p.Seq != nil {
		t.Fatalf("re-sent DCMD %+v %v", got, err)
	}

	if d, err := e.SendDeviceCommand(target, sparkplug.DeviceCommand{ID: "cmd-2", Name: sparkplug.CommandResume}); err != nil || d != host.Published {
		t.Fatalf("command to a born device: %v %v", d, err)
	}
	if len(pub.on(topic(sparkplug.DCMD, device))) != 2 {
		t.Fatal("DCMD not published")
	}
	if _, err := e.SendDeviceCommand(target, sparkplug.DeviceCommand{ID: "x", Name: "explode"}); err == nil {
		t.Fatal("invalid command accepted")
	}
}

func TestReceivedCommandsAreNeverAuthoritative(t *testing.T) {
	e, store, _, events := newEngine(t)
	e.HandleMessage(context.Background(), topic(sparkplug.DCMD, device), []byte{1, 2, 3}, time.Now())
	e.HandleMessage(context.Background(), topic(sparkplug.NCMD, ""), []byte{1}, time.Now())
	if len(store.EventLog()) != 0 || !has(*events, "command_topic_ignored") {
		t.Fatal("received NCMD/DCMD were processed")
	}
}

func TestOwnOfflineStateIsAnsweredWithOnline(t *testing.T) {
	e, _, pub, _ := newEngine(t) // session STATE timestamp 1000
	st, _ := sparkplug.StateTopic(sparkplug.PrimaryHostID)
	e.HandleMessage(context.Background(), st, sparkplug.EncodeState(sparkplug.HostState{Online: false, Timestamp: 900}), time.Now())
	msgs := pub.on(st)
	if len(msgs) != 1 || msgs[0].qos != 1 || !msgs[0].retained {
		t.Fatalf("expected one retained QoS1 online STATE, got %d", len(msgs))
	}
	s, _ := sparkplug.DecodeState(msgs[0].payload)
	if !s.Online || s.Timestamp != 1000 {
		t.Fatalf("STATE %+v", s)
	}
	// A newer offline (another session of this host) is not contradicted.
	e.HandleMessage(context.Background(), st, sparkplug.EncodeState(sparkplug.HostState{Online: false, Timestamp: 2000}), time.Now())
	if len(pub.on(st)) != 1 {
		t.Fatal("newer offline STATE was contradicted")
	}
}

func TestTickRefreshesOnlineDevices(t *testing.T) {
	e, store, _, _ := newEngine(t)
	ctx := context.Background()
	sim := newEdgeSim(t, 0)
	e.HandleMessage(ctx, topic(sparkplug.NBIRTH, ""), sim.nbirth(), time.Now())
	e.HandleMessage(ctx, topic(sparkplug.DBIRTH, device), sim.dbirth(device, "idle"), time.Now())
	e.Tick(ctx)
	if got := store.TouchedDevices(); len(got) != 1 || got[0] != hosttest.DeviceKey(group, edge, device) {
		t.Fatalf("touched %v", got)
	}
}

func TestSendWithoutConnection(t *testing.T) {
	e := host.New(host.Options{Store: hosttest.New()})
	if _, err := e.SendDeviceCommand(host.Target{Group: group, EdgeNodeID: edge, DeviceID: device},
		sparkplug.DeviceCommand{ID: "c", Name: sparkplug.CommandPause}); err != host.ErrNotConnected {
		t.Fatalf("got %v", err)
	}
}
