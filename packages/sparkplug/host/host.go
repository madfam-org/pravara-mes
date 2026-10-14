// Package host is pravara's Sparkplug 3.0 primary host application engine
// (MES-1 §1, p5spb §7 "Host application"). The telemetry worker runs it with
// a database-backed Store; tests run it against an in-memory Store.
//
// The engine:
//   - publishes the host STATE (a will "offline" plus a retained "online"
//     with the same timestamp, QoS 1) through Client;
//   - accepts NBIRTH/NDEATH/NDATA/DBIRTH/DDATA/DDEATH only from edge nodes
//     the Store has registered under the topic's group (the group must be the
//     registered tenant; the topic alone is never trusted);
//   - keeps one session per edge node: the alias table learned from births,
//     the NBIRTH bdSeq (an NDEATH with another bdSeq is stale and ignored)
//     and the seq of the last message. A seq gap, an unknown alias or data
//     before a birth ends the session and sends an NCMD Node Control/Rebirth;
//   - hands every device birth, data and death to the Store (live state,
//     command acknowledgement binding, quarantine of unregistered devices);
//   - publishes DCMD (QoS 0, no seq) for commands, only to devices born in
//     the current session, and re-sends the commands the Store returns after
//     a DBIRTH, with the same command id;
//   - ignores NCMD/DCMD it receives: an edge credential's ACL lets it publish
//     those types in its own namespace, but they are never authoritative.
package host

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

// EdgeNode is a registered edge node as the Store resolved it.
type EdgeNode struct {
	ID         string // registry id (opaque to the engine)
	TenantID   string
	Group      string // Sparkplug group_id: the tenant slug
	EdgeNodeID string
}

// DeviceReport is one DBIRTH or DDATA, decoded.
type DeviceReport struct {
	DeviceID string
	// Birth is true for a DBIRTH: Values is the device's full metric set.
	// For DDATA, Values holds only the metrics that changed.
	Birth bool
	BdSeq uint64
	Seq   uint64
	// Timestamp is the payload timestamp set by the edge node.
	Timestamp time.Time
	// ReceivedAt is when the host received the message.
	ReceivedAt time.Time
	// Values maps metric names to decoded values (nil for a null metric).
	Values map[sparkplug.MetricName]any
	// MetricTimes holds each metric's own timestamp when it carries one.
	MetricTimes map[sparkplug.MetricName]time.Time
}

// DeviceOutcome is what the Store did with a DeviceReport.
type DeviceOutcome struct {
	// Registered is false when the device is not a registered machine of
	// this edge node; the Store quarantined it and the engine ignores its
	// data until the next birth.
	Registered bool
	// Resend lists commands for this device that were sent but never
	// acknowledged; after a DBIRTH the engine publishes them again with the
	// same command id (the edge node is idempotent on the id).
	Resend []sparkplug.DeviceCommand
}

// Store persists what the host learns. Every method must scope its work to
// the edge node's tenant.
type Store interface {
	// ResolveEdgeNode returns the active registered edge node whose tenant
	// slug is group, or nil when there is none (unknown or disabled).
	ResolveEdgeNode(ctx context.Context, group, edgeNodeID string) (*EdgeNode, error)
	// NodeBirth records an NBIRTH: the node is online with bdSeq, and its
	// devices are offline until their DBIRTH.
	NodeBirth(ctx context.Context, n EdgeNode, bdSeq uint64, at time.Time) error
	// NodeDeath records an NDEATH: when bdSeq is the node's current one, the
	// node and all its devices go offline. It reports whether it applied.
	NodeDeath(ctx context.Context, n EdgeNode, bdSeq uint64, at time.Time) (bool, error)
	// DeviceReport records a DBIRTH or DDATA.
	DeviceReport(ctx context.Context, n EdgeNode, r DeviceReport) (DeviceOutcome, error)
	// DeviceDeath records a DDEATH.
	DeviceDeath(ctx context.Context, n EdgeNode, deviceID string, at time.Time) error
	// Touch refreshes the liveness of devices that are online in the
	// current session (Sparkplug devices only report changes).
	Touch(ctx context.Context, n EdgeNode, deviceIDs []string, at time.Time) error
}

// Publisher sends one MQTT message. It must not wait for broker
// acknowledgements (it is called from the message handler).
type Publisher interface {
	Publish(topic string, qos byte, retained bool, payload []byte) error
}

// Options configures an Engine.
type Options struct {
	// HostID is the host application id (default sparkplug.PrimaryHostID).
	HostID string
	Store  Store
	Logger *slog.Logger
	// Observe receives one event name per notable occurrence (metrics).
	Observe func(event string)
	// RebirthInterval is the minimum time between two rebirth requests to
	// the same edge node (default 5s).
	RebirthInterval time.Duration
	// Now is the clock (default time.Now).
	Now func() time.Time
}

// Errors returned by SendDeviceCommand.
var (
	ErrNotConnected  = errors.New("sparkplug host: not connected to the broker")
	ErrDeviceOffline = errors.New("sparkplug host: device is not online in the current session")
)

// Engine is the primary host application state machine.
type Engine struct {
	opts Options

	mu       sync.Mutex
	pub      Publisher
	stateTS  uint64
	sessions map[nodeKey]*session
}

type nodeKey struct{ group, edge string }

type deviceState int

const (
	deviceOnline deviceState = iota + 1
	deviceQuarantined
)

type session struct {
	ref         *EdgeNode
	born        bool
	bdSeq       uint64
	seq         uint64
	aliases     *sparkplug.AliasTable
	devices     map[string]deviceState
	lastRebirth time.Time
	lastLookup  time.Time // last registry lookup for an unregistered node
}

// New returns an Engine.
func New(opts Options) *Engine {
	if opts.HostID == "" {
		opts.HostID = sparkplug.PrimaryHostID
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Observe == nil {
		opts.Observe = func(string) {}
	}
	if opts.RebirthInterval <= 0 {
		opts.RebirthInterval = 5 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Engine{opts: opts, sessions: map[nodeKey]*session{}}
}

// HostID returns the host application id.
func (e *Engine) HostID() string { return e.opts.HostID }

// attach binds the engine to a connected publisher for one broker session
// with STATE timestamp ts. Every edge-node session from an earlier broker
// session is forgotten: edge nodes rebirth when the host comes back online.
func (e *Engine) attach(pub Publisher, ts uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pub, e.stateTS = pub, ts
	e.sessions = map[nodeKey]*session{}
}

// detach drops the publisher when the broker session ends.
func (e *Engine) detach() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pub = nil
	for _, s := range e.sessions {
		s.born = false
	}
}

func (e *Engine) session(group, edge string) *session {
	k := nodeKey{group, edge}
	s, ok := e.sessions[k]
	if !ok {
		s = &session{devices: map[string]deviceState{}}
		e.sessions[k] = s
	}
	return s
}

// HandleMessage processes one message received on topic.
func (e *Engine) HandleMessage(ctx context.Context, topic string, payload []byte, receivedAt time.Time) {
	t, err := sparkplug.ParseTopic(topic)
	if err != nil {
		e.opts.Observe("invalid_topic")
		return
	}
	switch t.Type {
	case sparkplug.STATE:
		e.onState(t, payload)
		return
	case sparkplug.NCMD, sparkplug.DCMD:
		e.opts.Observe("command_topic_ignored")
		return
	}
	p, err := sparkplug.Decode(payload)
	if err != nil {
		e.opts.Observe("undecodable")
		e.opts.Logger.Warn("undecodable Sparkplug payload", "topic", topic, "error", err)
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.session(t.GroupID, t.EdgeNodeID)
	log := e.opts.Logger.With("group", t.GroupID, "edge_node_id", t.EdgeNodeID, "type", string(t.Type))

	switch t.Type {
	case sparkplug.NBIRTH:
		e.onNodeBirth(ctx, log, s, t, p)
		return
	case sparkplug.NDEATH:
		e.onNodeDeath(ctx, log, s, t, p)
		return
	}

	if !s.born {
		e.requestRebirth(ctx, log, s, t, "message before NBIRTH")
		return
	}
	if p.Seq == nil || !sparkplug.SeqFollows(s.seq, p.GetSeq()) {
		e.opts.Observe("seq_gap")
		log.Warn("sequence gap", "expected", (s.seq+1)%256, "got", p.GetSeq())
		s.born = false
		e.requestRebirth(ctx, log, s, t, "sequence gap")
		return
	}
	s.seq = p.GetSeq()

	switch t.Type {
	case sparkplug.NDATA:
		e.opts.Observe("ndata")
	case sparkplug.DBIRTH:
		e.onDeviceBirth(ctx, log, s, t, p, receivedAt)
	case sparkplug.DDATA:
		e.onDeviceData(ctx, log, s, t, p, receivedAt)
	case sparkplug.DDEATH:
		e.onDeviceDeath(ctx, log, s, t, p)
	}
}

func (e *Engine) onNodeBirth(ctx context.Context, log *slog.Logger, s *session, t sparkplug.Topic, p *pb.Payload) {
	ref, err := e.opts.Store.ResolveEdgeNode(ctx, t.GroupID, t.EdgeNodeID)
	if err != nil {
		log.Error("edge node lookup failed", "error", err)
		s.born = false
		return
	}
	if ref == nil {
		e.opts.Observe("unknown_edge_node")
		log.Warn("NBIRTH from an edge node that is not registered under this group")
		s.ref, s.born = nil, false
		return
	}
	s.ref = ref
	bdSeq, err := sparkplug.BdSeqOf(p)
	if err == nil {
		err = sparkplug.ValidateBirth(p)
	}
	aliases := sparkplug.NewAliasTable()
	if err == nil {
		err = aliases.LearnBirth("", p)
	}
	if err != nil {
		log.Warn("invalid NBIRTH", "error", err)
		s.born = false
		e.requestRebirth(ctx, log, s, t, "invalid NBIRTH")
		return
	}
	if err := e.opts.Store.NodeBirth(ctx, *ref, bdSeq, payloadTime(p, e.opts.Now())); err != nil {
		log.Error("recording NBIRTH failed", "error", err)
		s.born = false
		return
	}
	s.born, s.bdSeq, s.seq, s.aliases = true, bdSeq, p.GetSeq(), aliases
	s.devices = map[string]deviceState{}
	e.opts.Observe("nbirth")
	log.Info("edge node born", "bdseq", bdSeq)
}

func (e *Engine) onNodeDeath(ctx context.Context, log *slog.Logger, s *session, t sparkplug.Topic, p *pb.Payload) {
	bdSeq, err := sparkplug.BdSeqOf(p)
	if err != nil {
		log.Warn("NDEATH without bdSeq ignored", "error", err)
		return
	}
	if s.born && bdSeq != s.bdSeq {
		e.opts.Observe("stale_ndeath")
		log.Info("stale NDEATH ignored", "bdseq", bdSeq, "current", s.bdSeq)
		return
	}
	ref := s.ref
	if ref == nil {
		if ref, err = e.opts.Store.ResolveEdgeNode(ctx, t.GroupID, t.EdgeNodeID); err != nil || ref == nil {
			if err != nil {
				log.Error("edge node lookup failed", "error", err)
			}
			return
		}
	}
	applied, err := e.opts.Store.NodeDeath(ctx, *ref, bdSeq, payloadTime(p, e.opts.Now()))
	if err != nil {
		log.Error("recording NDEATH failed", "error", err)
		return
	}
	s.born = false
	s.devices = map[string]deviceState{}
	if applied {
		e.opts.Observe("ndeath")
		log.Info("edge node died", "bdseq", bdSeq)
	} else {
		e.opts.Observe("stale_ndeath")
	}
}

func (e *Engine) onDeviceBirth(ctx context.Context, log *slog.Logger, s *session, t sparkplug.Topic, p *pb.Payload, receivedAt time.Time) {
	log = log.With("device_id", t.DeviceID)
	if err := sparkplug.ValidateBirth(p); err != nil {
		log.Warn("invalid DBIRTH", "error", err)
		e.endSession(ctx, log, s, t, "invalid DBIRTH")
		return
	}
	if err := s.aliases.LearnBirth(t.DeviceID, p); err != nil {
		log.Warn("DBIRTH alias conflict", "error", err)
		e.endSession(ctx, log, s, t, "alias conflict")
		return
	}
	values, times, err := decodeMetrics(p, t.DeviceID, s.aliases)
	if err != nil {
		log.Warn("undecodable DBIRTH metric", "error", err)
		e.endSession(ctx, log, s, t, "undecodable DBIRTH")
		return
	}
	out, err := e.opts.Store.DeviceReport(ctx, *s.ref, DeviceReport{
		DeviceID: t.DeviceID, Birth: true, BdSeq: s.bdSeq, Seq: p.GetSeq(),
		Timestamp: payloadTime(p, receivedAt), ReceivedAt: receivedAt, Values: values, MetricTimes: times,
	})
	if err != nil {
		log.Error("recording DBIRTH failed", "error", err)
		delete(s.devices, t.DeviceID)
		return
	}
	if !out.Registered {
		s.devices[t.DeviceID] = deviceQuarantined
		e.opts.Observe("quarantined")
		log.Warn("DBIRTH for an unregistered device quarantined")
		return
	}
	s.devices[t.DeviceID] = deviceOnline
	e.opts.Observe("dbirth")
	for _, cmd := range out.Resend {
		if err := e.publishCommand(*s.ref, t.DeviceID, cmd); err != nil {
			log.Warn("command re-send after DBIRTH failed", "command_id", cmd.ID, "error", err)
			continue
		}
		e.opts.Observe("command_resent")
		log.Info("command re-sent after DBIRTH", "command_id", cmd.ID)
	}
}

func (e *Engine) onDeviceData(ctx context.Context, log *slog.Logger, s *session, t sparkplug.Topic, p *pb.Payload, receivedAt time.Time) {
	log = log.With("device_id", t.DeviceID)
	switch s.devices[t.DeviceID] {
	case deviceQuarantined:
		e.opts.Observe("quarantined_data_ignored")
		return
	case deviceOnline:
	default:
		e.endSession(ctx, log, s, t, "DDATA before DBIRTH")
		return
	}
	values, times, err := decodeMetrics(p, t.DeviceID, s.aliases)
	if err != nil {
		log.Warn("DDATA cannot be decoded", "error", err)
		e.endSession(ctx, log, s, t, "unknown alias")
		return
	}
	if _, err := e.opts.Store.DeviceReport(ctx, *s.ref, DeviceReport{
		DeviceID: t.DeviceID, BdSeq: s.bdSeq, Seq: p.GetSeq(),
		Timestamp: payloadTime(p, receivedAt), ReceivedAt: receivedAt, Values: values, MetricTimes: times,
	}); err != nil {
		log.Error("recording DDATA failed", "error", err)
		return
	}
	e.opts.Observe("ddata")
}

func (e *Engine) onDeviceDeath(ctx context.Context, log *slog.Logger, s *session, t sparkplug.Topic, p *pb.Payload) {
	state := s.devices[t.DeviceID]
	delete(s.devices, t.DeviceID)
	if state == deviceQuarantined {
		return
	}
	if err := e.opts.Store.DeviceDeath(ctx, *s.ref, t.DeviceID, payloadTime(p, e.opts.Now())); err != nil {
		log.Error("recording DDEATH failed", "device_id", t.DeviceID, "error", err)
		return
	}
	e.opts.Observe("ddeath")
	log.Info("device died", "device_id", t.DeviceID)
}

// endSession forgets the node's session and asks for a rebirth.
func (e *Engine) endSession(ctx context.Context, log *slog.Logger, s *session, t sparkplug.Topic, reason string) {
	s.born = false
	e.requestRebirth(ctx, log, s, t, reason)
}

// requestRebirth sends NCMD Node Control/Rebirth = true to a registered edge
// node, at most once per RebirthInterval.
func (e *Engine) requestRebirth(ctx context.Context, log *slog.Logger, s *session, t sparkplug.Topic, reason string) {
	now := e.opts.Now()
	if s.ref == nil {
		// Unregistered nodes are looked up again at most once per interval,
		// so their traffic cannot turn into a lookup per message.
		if !s.lastLookup.IsZero() && now.Sub(s.lastLookup) < e.opts.RebirthInterval {
			return
		}
		s.lastLookup = now
		ref, err := e.opts.Store.ResolveEdgeNode(ctx, t.GroupID, t.EdgeNodeID)
		if err != nil {
			log.Error("edge node lookup failed", "error", err)
			return
		}
		if ref == nil {
			e.opts.Observe("unknown_edge_node")
			return
		}
		s.ref = ref
	}
	if !s.lastRebirth.IsZero() && now.Sub(s.lastRebirth) < e.opts.RebirthInterval {
		e.opts.Observe("rebirth_suppressed")
		return
	}
	if e.pub == nil {
		return
	}
	m, err := sparkplug.NewMetric(sparkplug.MetricNodeControlRebirth, pb.DataType_Boolean, true, now)
	if err != nil {
		return
	}
	b, err := sparkplug.Encode(&pb.Payload{Timestamp: proto.Uint64(sparkplug.Millis(now)), Metrics: []*pb.Payload_Metric{m}})
	if err != nil {
		return
	}
	topic, err := sparkplug.NodeTopic(s.ref.Group, sparkplug.NCMD, s.ref.EdgeNodeID)
	if err != nil {
		return
	}
	if err := e.pub.Publish(topic, 0, false, b); err != nil {
		log.Warn("rebirth request failed", "error", err)
		return
	}
	s.lastRebirth = now
	e.opts.Observe("rebirth_requested")
	log.Info("rebirth requested", "reason", reason)
}

// onState handles STATE messages. The host's own STATE showing offline while
// this session is connected (for example a stale retained will) is answered
// with a fresh online STATE carrying the session timestamp.
func (e *Engine) onState(t sparkplug.Topic, payload []byte) {
	if t.HostID != e.opts.HostID {
		return
	}
	st, err := sparkplug.DecodeState(payload)
	if err != nil || st.Online {
		return
	}
	e.mu.Lock()
	pub, ts := e.pub, e.stateTS
	e.mu.Unlock()
	if pub == nil || st.Timestamp > ts {
		return
	}
	topic, _ := sparkplug.StateTopic(e.opts.HostID)
	if err := pub.Publish(topic, 1, true, sparkplug.EncodeState(sparkplug.HostState{Online: true, Timestamp: ts})); err != nil {
		e.opts.Logger.Warn("re-publishing online STATE failed", "error", err)
		return
	}
	e.opts.Observe("state_reasserted")
}

// Tick refreshes the liveness of every device online in a born session.
func (e *Engine) Tick(ctx context.Context) {
	type touch struct {
		ref     EdgeNode
		devices []string
	}
	var work []touch
	e.mu.Lock()
	for _, s := range e.sessions {
		if !s.born || s.ref == nil {
			continue
		}
		var ds []string
		for d, st := range s.devices {
			if st == deviceOnline {
				ds = append(ds, d)
			}
		}
		if len(ds) > 0 {
			work = append(work, touch{*s.ref, ds})
		}
	}
	e.mu.Unlock()
	now := e.opts.Now()
	for _, w := range work {
		if err := e.opts.Store.Touch(ctx, w.ref, w.devices, now); err != nil {
			e.opts.Logger.Warn("device liveness refresh failed", "edge_node_id", w.ref.EdgeNodeID, "error", err)
		}
	}
}

// decodeMetrics resolves names (via aliases for alias-only metrics) and
// decodes values.
func decodeMetrics(p *pb.Payload, device string, aliases *sparkplug.AliasTable) (map[sparkplug.MetricName]any, map[sparkplug.MetricName]time.Time, error) {
	values := map[sparkplug.MetricName]any{}
	times := map[sparkplug.MetricName]time.Time{}
	for _, m := range p.GetMetrics() {
		name := sparkplug.MetricName(m.GetName())
		dt := pb.DataType(m.GetDatatype())
		if m.Name == nil {
			if m.Alias == nil {
				return nil, nil, errors.New("metric without name or alias")
			}
			var learned pb.DataType
			var ok bool
			name, learned, ok = aliases.Resolve(device, m.GetAlias())
			if !ok {
				return nil, nil, errors.New("unknown alias")
			}
			if m.Datatype == nil {
				dt = learned
			}
		}
		v, err := sparkplug.Value(m, dt)
		if err != nil {
			return nil, nil, err
		}
		values[name] = v
		if m.Timestamp != nil {
			times[name] = time.UnixMilli(int64(m.GetTimestamp())).UTC()
		}
	}
	return values, times, nil
}

func payloadTime(p *pb.Payload, fallback time.Time) time.Time {
	if p.Timestamp == nil {
		return fallback.UTC()
	}
	return time.UnixMilli(int64(p.GetTimestamp())).UTC()
}
