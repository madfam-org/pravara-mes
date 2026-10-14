package edge

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/manager"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

// Options adjusts a Node for tests and the simulator.
type Options struct {
	Now func() time.Time
	// ArtifactClient replaces the HTTPS client used to download artifacts.
	ArtifactClient *http.Client
	// Secrets supplies device secrets by device id instead of secret files.
	Secrets map[string]string
	// Connections overrides device connection parameters by device id (simulator mode).
	Connections map[string]manager.ConnectionParams
}

type sessionEvent int

const (
	evRebirth sessionEvent = iota + 1
	evHostOffline
)

type event struct {
	kind   sessionEvent
	client paho.Client
}

// Node is the Sparkplug B edge node of one site box.
type Node struct {
	cfg       Config
	mgr       *manager.Manager
	log       *logrus.Entry
	fetcher   *ArtifactFetcher
	now       func() time.Time
	password  string
	tlsConfig *tls.Config

	aliases *sparkplug.AliasTable
	seq     sparkplug.SeqCounter
	bdSeqs  *bdSeqStore

	pubMu    sync.Mutex // orders seq assignment and publishing; guards the fields below
	client   paho.Client
	bdSeq    uint64
	session  uint64 // id of the born session, 0 when not born
	sessions uint64

	devices []*device
	byID    map[string]*device

	motionCount motionCounters

	hostMu  sync.Mutex
	host    sparkplug.HostTracker
	hostUp  chan struct{}
	events  chan event
	running sync.WaitGroup
}

// NewNode validates cfg and prepares the devices.
func NewNode(cfg Config, reg *registry.Registry, mgr *manager.Manager, log *logrus.Logger, opts Options) (*Node, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	password, err := readSecret(cfg.PasswordFile)
	if err != nil {
		return nil, fmt.Errorf("broker password: %w", err)
	}
	tc, err := buildTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	n := &Node{
		cfg:       cfg,
		mgr:       mgr,
		log:       log.WithFields(logrus.Fields{"component": "edge", "group_id": cfg.GroupID, "edge_node_id": cfg.EdgeNodeID}),
		now:       opts.Now,
		password:  password,
		tlsConfig: tc,
		aliases:   sparkplug.NewAliasTable(),
		bdSeqs:    loadBdSeq(cfg.StateDir),
		byID:      map[string]*device{},
		hostUp:    make(chan struct{}, 1),
		events:    make(chan event, 8),
	}
	if n.now == nil {
		n.now = time.Now
	}
	scratch := ""
	if cfg.StateDir != "" {
		scratch = filepath.Join(cfg.StateDir, "artifacts")
	}
	n.fetcher = NewArtifactFetcher(scratch, cfg.ArtifactMaxBytes, cfg.ArtifactTimeout)
	if opts.ArtifactClient != nil {
		c := *opts.ArtifactClient
		c.CheckRedirect = httpsOnlyRedirects
		n.fetcher.Client = &c
	}
	for _, dc := range cfg.Devices {
		d, err := newDevice(dc, cfg.GroupID, reg, opts, cfg.Motion)
		if err != nil {
			return nil, fmt.Errorf("device %s: %w", dc.DeviceID, err)
		}
		n.devices = append(n.devices, d)
		n.byID[d.id] = d
	}
	return n, nil
}

// Run connects to the broker and keeps the Sparkplug session alive until ctx
// ends, then publishes NDEATH and disconnects.
func (n *Node) Run(ctx context.Context) error {
	for _, d := range n.devices {
		n.running.Add(1)
		go func(d *device) {
			defer n.running.Done()
			n.runDevice(ctx, d)
		}(d)
		if d.motionEnabled() {
			n.running.Add(1)
			go func(d *device) {
				defer n.running.Done()
				n.runMotion(ctx, d)
			}(d)
		}
	}
	defer n.running.Wait()
	for {
		err := n.runSession(ctx)
		if ctx.Err() != nil {
			return nil
		}
		n.log.WithError(err).Warn("Sparkplug session ended; reconnecting")
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(n.cfg.ReconnectDelay):
		}
	}
}

func (n *Node) runSession(ctx context.Context) error {
	bd, err := n.bdSeqs.Next()
	if err != nil {
		n.log.WithError(err).Warn("could not persist bdSeq")
	}
	client, lost, err := n.dial(bd)
	if err != nil {
		return err
	}
	n.pubMu.Lock()
	n.client, n.bdSeq = client, bd
	n.pubMu.Unlock()
	defer func() {
		n.pubMu.Lock()
		n.client, n.session = nil, 0
		n.pubMu.Unlock()
	}()

	if err := n.subscribe(client); err != nil {
		n.closeSession(client)
		return err
	}
	n.log.WithField("bdSeq", bd).Info("connected to broker")

	for n.cfg.WaitsForPrimaryHost() && !n.hostOnline() {
		select {
		case <-ctx.Done():
			n.closeSession(client)
			return nil
		case err := <-lost:
			return fmt.Errorf("connection lost: %w", err)
		case <-n.hostUp:
		case <-n.events: // nothing to rebirth or end before the first birth
		}
	}
	if err := n.birth(); err != nil {
		n.closeSession(client)
		return err
	}

	for {
		select {
		case <-ctx.Done():
			n.closeSession(client)
			return nil
		case err := <-lost:
			return fmt.Errorf("connection lost: %w", err)
		case ev := <-n.events:
			if ev.client != client {
				continue // stale event from an earlier session
			}
			switch ev.kind {
			case evRebirth:
				if err := n.birth(); err != nil {
					n.log.WithError(err).Warn("rebirth failed")
				}
			case evHostOffline:
				n.closeSession(client)
				return errHostOffline
			}
		}
	}
}

// closeSession publishes NDEATH (spec: before an intentional disconnect) and disconnects.
func (n *Node) closeSession(client paho.Client) {
	n.pubMu.Lock()
	defer n.pubMu.Unlock()
	n.session = 0
	if death, err := sparkplug.NodeDeath(n.bdSeq, n.now()); err == nil {
		topic, _ := sparkplug.NodeTopic(n.cfg.GroupID, sparkplug.NDEATH, n.cfg.EdgeNodeID)
		if b, err := sparkplug.Encode(death); err == nil {
			tok := client.Publish(topic, 1, false, b)
			tok.WaitTimeout(5 * time.Second)
		}
	}
	client.Disconnect(250)
}

// birth publishes NBIRTH (seq 0, same bdSeq as the will) followed by a DBIRTH
// for every online device, all under one lock so no DATA interleaves.
func (n *Node) birth() error {
	n.pubMu.Lock()
	defer n.pubMu.Unlock()
	if n.client == nil {
		return fmt.Errorf("not connected")
	}
	now := n.now()
	n.seq.Reset()
	nb, err := sparkplug.NodeBirth(n.bdSeq, n.seq.Next(), now, n.aliases)
	if err != nil {
		return err
	}
	topic, _ := sparkplug.NodeTopic(n.cfg.GroupID, sparkplug.NBIRTH, n.cfg.EdgeNodeID)
	if err := n.publishLocked(topic, nb); err != nil {
		return err
	}
	n.sessions++
	n.session = n.sessions
	for _, d := range n.devices {
		if err := n.publishDeviceBirthLocked(d, now); err != nil {
			n.log.WithError(err).WithField("device_id", d.id).Warn("DBIRTH failed")
		}
	}
	return nil
}

// publishLocked encodes and publishes with QoS 0, retain false (spec for
// NBIRTH/DBIRTH/DDATA/DDEATH). Caller holds pubMu.
func (n *Node) publishLocked(topic string, p *pb.Payload) error {
	b, err := sparkplug.Encode(p)
	if err != nil {
		return err
	}
	tok := n.client.Publish(topic, 0, false, b)
	if !tok.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("publish %s timed out", topic)
	}
	return tok.Error()
}

// publishDeviceBirthLocked sends a DBIRTH if the device is online. Caller holds pubMu.
func (n *Node) publishDeviceBirthLocked(d *device, now time.Time) error {
	d.mu.Lock()
	if !d.online {
		d.mu.Unlock()
		return nil
	}
	metrics, err := d.birthMetricsLocked(now)
	if err == nil {
		d.bornIn = n.session
	}
	d.mu.Unlock()
	if err != nil {
		return err
	}
	p, err := sparkplug.DeviceBirth(d.id, n.seq.Next(), now, n.aliases, metrics)
	if err != nil {
		return err
	}
	topic, _ := sparkplug.DeviceTopic(n.cfg.GroupID, sparkplug.DBIRTH, n.cfg.EdgeNodeID, d.id)
	return n.publishLocked(topic, p)
}

// publishDevice sends the changed metrics as DDATA, or a DBIRTH if the device
// has not been born in the current session. No-op while the node is not born.
func (n *Node) publishDevice(d *device, changed []sparkplug.MetricName) {
	n.pubMu.Lock()
	defer n.pubMu.Unlock()
	if n.session == 0 || n.client == nil {
		return
	}
	now := n.now()
	d.mu.Lock()
	online, bornIn := d.online, d.bornIn
	if !online {
		d.mu.Unlock()
		return
	}
	if bornIn != n.session {
		d.mu.Unlock()
		if err := n.publishDeviceBirthLocked(d, now); err != nil {
			n.log.WithError(err).WithField("device_id", d.id).Warn("DBIRTH failed")
		}
		return
	}
	metrics, err := d.metricsLocked(changed, now)
	d.mu.Unlock()
	if err != nil || len(metrics) == 0 {
		if err != nil {
			n.log.WithError(err).WithField("device_id", d.id).Warn("DDATA build failed")
		}
		return
	}
	p, err := sparkplug.DeviceData(d.id, n.seq.Next(), now, n.aliases, metrics)
	if err != nil {
		n.log.WithError(err).WithField("device_id", d.id).Warn("DDATA build failed")
		return
	}
	topic, _ := sparkplug.DeviceTopic(n.cfg.GroupID, sparkplug.DDATA, n.cfg.EdgeNodeID, d.id)
	if err := n.publishLocked(topic, p); err != nil {
		n.log.WithError(err).WithField("device_id", d.id).Warn("DDATA publish failed")
	}
}

// publishDeviceDeath sends DDEATH for a device born in the current session.
func (n *Node) publishDeviceDeath(d *device) {
	n.pubMu.Lock()
	defer n.pubMu.Unlock()
	d.mu.Lock()
	born := n.session != 0 && d.bornIn == n.session
	d.bornIn = 0
	d.mu.Unlock()
	if !born || n.client == nil {
		return
	}
	topic, _ := sparkplug.DeviceTopic(n.cfg.GroupID, sparkplug.DDEATH, n.cfg.EdgeNodeID, d.id)
	if err := n.publishLocked(topic, sparkplug.DeviceDeath(n.seq.Next(), n.now())); err != nil {
		n.log.WithError(err).WithField("device_id", d.id).Warn("DDEATH publish failed")
	}
}

func (n *Node) post(kind sessionEvent, client paho.Client) {
	select {
	case n.events <- event{kind: kind, client: client}:
	default:
		n.log.Warn("session event queue full; dropping event")
	}
}

func (n *Node) hostOnline() bool {
	n.hostMu.Lock()
	defer n.hostMu.Unlock()
	return n.host.Online()
}

// onSTATE applies the primary host's STATE (stale timestamps ignored).
func (n *Node) onSTATE(client paho.Client, msg paho.Message) {
	t, err := sparkplug.ParseTopic(msg.Topic())
	if err != nil || t.HostID != n.cfg.PrimaryHostID {
		return
	}
	s, err := sparkplug.DecodeState(msg.Payload())
	if err != nil {
		n.log.WithError(err).Warn("ignoring malformed STATE")
		return
	}
	n.hostMu.Lock()
	online, changed := n.host.Observe(s)
	n.hostMu.Unlock()
	if !changed {
		return
	}
	n.log.WithField("online", online).Info("primary host state changed")
	if online {
		select {
		case n.hostUp <- struct{}{}:
		default:
		}
		return
	}
	n.post(evHostOffline, client)
}

// onNCMD handles node commands; only 'Node Control/Rebirth' is supported.
func (n *Node) onNCMD(client paho.Client, msg paho.Message) {
	p, err := sparkplug.Decode(msg.Payload())
	if err != nil {
		n.log.WithError(err).Warn("ignoring malformed NCMD")
		return
	}
	if sparkplug.ParseRebirthRequest(p, n.aliases.ResolverFor("")) {
		n.log.Info("rebirth requested")
		n.post(evRebirth, client)
	}
}

// onDCMD routes a device command.
func (n *Node) onDCMD(_ paho.Client, msg paho.Message) {
	t, err := sparkplug.ParseTopic(msg.Topic())
	if err != nil || t.Type != sparkplug.DCMD || t.GroupID != n.cfg.GroupID || t.EdgeNodeID != n.cfg.EdgeNodeID {
		return
	}
	d, ok := n.byID[t.DeviceID]
	if !ok {
		n.log.WithField("device_id", t.DeviceID).Warn("DCMD for an unknown device")
		return
	}
	n.handleCommand(d, msg.Payload())
}

// Status is a read-only snapshot for the local health endpoint.
type Status struct {
	Connected bool           `json:"connected"`
	Born      bool           `json:"born"`
	BdSeq     uint64         `json:"bd_seq"`
	Devices   []DeviceStatus `json:"devices"`
	Motion    MotionCounters `json:"motion"`
}

// DeviceStatus is one device's local view.
type DeviceStatus struct {
	DeviceID string `json:"device_id"`
	Online   bool   `json:"online"`
	State    string `json:"state,omitempty"`
	JobID    string `json:"job_id,omitempty"`
	JobState string `json:"job_status,omitempty"`
}

// Status reports the node's session and device state.
func (n *Node) Status() Status {
	n.pubMu.Lock()
	s := Status{Connected: n.client != nil, Born: n.session != 0, BdSeq: n.bdSeq, Motion: n.motionCount.snapshot()}
	n.pubMu.Unlock()
	for _, d := range n.devices {
		d.mu.Lock()
		ds := DeviceStatus{DeviceID: d.id, Online: d.online, JobID: d.job.id, JobState: string(d.job.status)}
		if v, ok := d.values[sparkplug.MetricStateStatus].(string); ok {
			ds.State = v
		}
		d.mu.Unlock()
		s.Devices = append(s.Devices, ds)
	}
	return s
}
