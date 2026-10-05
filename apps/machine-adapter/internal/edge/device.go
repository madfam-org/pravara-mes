package edge

import (
	"context"
	"fmt"
	"math"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/adapters"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/manager"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

// jobState tracks the job started by the last start_job on a device.
type jobState struct {
	id              string
	file            string // name the artifact was stored under on the printer
	status          sparkplug.JobStatus
	cancelRequested bool
	startedAt       time.Time
}

func (j jobState) active() bool { return j.id != "" && !j.status.IsTerminal() }

// device is one printer behind the edge node.
type device struct {
	id    string
	spec  manager.MachineSpec
	slots int

	mu            sync.Mutex
	connected     bool
	online        bool
	failures      int
	bornIn        uint64
	values        map[sparkplug.MetricName]any       // every device metric, nil = null
	stamps        map[sparkplug.MetricName]time.Time // per-metric source time (motion); else publish time
	motion        *motionState                       // nil = no Motion/* metrics
	job           jobState
	lastCommandID string
	startInFlight bool
	wake          chan struct{}
}

func newDevice(dc DeviceConfig, tenant string, reg *registry.Registry, opts Options, motion MotionConfig) (*device, error) {
	var def *registry.MachineDefinition
	if dc.Definition != "" {
		d, ok := reg.GetDefinition(dc.Definition)
		if !ok {
			return nil, fmt.Errorf("unknown definition %q", dc.Definition)
		}
		def = d
	}
	protocol := dc.Protocol
	if protocol == "" {
		protocol = string(def.Protocol)
	}
	conn, err := connectivityFor(protocol)
	if err != nil {
		return nil, err
	}
	secret, ok := opts.Secrets[dc.DeviceID]
	if !ok {
		if secret, err = readSecret(dc.SecretFile); err != nil {
			return nil, err
		}
	}
	caps, err := deriveCapabilities(def, protocol, dc.Capabilities)
	if err != nil {
		return nil, err
	}
	params := manager.ConnectionParams{
		Host: dc.Host, Port: dc.Port, Secret: secret, Serial: dc.Serial,
		TLSPinSHA256: dc.TLSPinSHA256, FTPSPort: dc.FTPSPort,
	}
	if o, ok := opts.Connections[dc.DeviceID]; ok {
		params = o
	}

	slots := dc.MaterialSlots
	if slots <= 0 {
		if n, ok := caps[sparkplug.CapabilityMaterialSlots].(int64); ok && n > 0 {
			slots = int(n)
			if conn == sparkplug.ConnectivityBambuLANMQTT {
				slots++ // the external spool holder follows the AMS trays
			}
		} else {
			slots = 1
		}
	}

	model := dc.Model
	if model == "" && def != nil {
		model = def.Manufacturer + " " + def.Model
	}
	firmware := dc.Firmware
	if firmware == "" {
		firmware = firmwareFor(protocol)
	}

	d := &device{
		id: dc.DeviceID,
		spec: manager.MachineSpec{
			MachineID: dc.DeviceID, DefinitionID: dc.Definition, Protocol: protocol, TenantID: tenant, Conn: params,
		},
		slots:  slots,
		values: map[sparkplug.MetricName]any{},
		stamps: map[sparkplug.MetricName]time.Time{},
		wake:   make(chan struct{}, 1),
	}
	if motion.Enabled && conn == sparkplug.ConnectivityMoonraker {
		d.motion = &motionState{wake: make(chan struct{}, 1)}
		d.declareMotionLocked()
	}
	v := d.values
	v[sparkplug.MetricPropertiesModel] = model
	v[sparkplug.MetricPropertiesFirmware] = firmware
	v[sparkplug.MetricPropertiesConnectivity] = string(conn)
	for k, val := range caps {
		v[k.Metric()] = val
	}
	for i := 1; i <= slots; i++ {
		v[sparkplug.MaterialSlotClassMetric(i)] = nil
		v[sparkplug.MaterialSlotLoadedMetric(i)] = false
	}
	v[sparkplug.MetricStateStatus] = string(sparkplug.StatusOffline)
	v[sparkplug.MetricStateProgress] = 0.0
	v[sparkplug.MetricTempsHotend] = 0.0
	v[sparkplug.MetricTempsBed] = 0.0
	for _, name := range []sparkplug.MetricName{
		sparkplug.MetricJobID, sparkplug.MetricJobStatus,
		sparkplug.MetricCommandLastID, sparkplug.MetricCommandStatus, sparkplug.MetricCommandError,
	} {
		v[name] = nil
	}
	for _, name := range sparkplug.CommandInputMetrics {
		v[name] = nil
	}
	return d, nil
}

// birthMetricsLocked returns every metric of the device. Caller holds d.mu.
func (d *device) birthMetricsLocked(now time.Time) ([]*pb.Payload_Metric, error) {
	names := make([]sparkplug.MetricName, 0, len(d.values))
	for name := range d.values {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return d.metricsLocked(names, now)
}

// metricsLocked builds metrics for names from the current values. Caller holds d.mu.
func (d *device) metricsLocked(names []sparkplug.MetricName, now time.Time) ([]*pb.Payload_Metric, error) {
	out := make([]*pb.Payload_Metric, 0, len(names))
	for _, name := range names {
		ts := now
		if st, ok := d.stamps[name]; ok {
			ts = st
		}
		m, err := sparkplug.NewContractMetric(name, d.values[name], ts)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// setLocked stores a value and records the name in changed when it differs.
func (d *device) setLocked(name sparkplug.MetricName, v any, changed *[]sparkplug.MetricName) {
	if equalValue(d.values[name], v) {
		return
	}
	d.values[name] = v
	*changed = append(*changed, name)
}

func equalValue(a, b any) bool {
	fa, okA := a.([]float64)
	fb, okB := b.([]float64)
	if okA || okB {
		if !okA || !okB || len(fa) != len(fb) {
			return false
		}
		for i := range fa {
			if fa[i] != fb[i] {
				return false
			}
		}
		return true
	}
	return a == b
}

// applySnapshotLocked folds a printer snapshot into the metrics. Caller holds d.mu.
func (d *device) applySnapshotLocked(s adapters.PrinterSnapshot, now time.Time, startTimeout time.Duration) []sparkplug.MetricName {
	var changed []sparkplug.MetricName
	status := s.Status
	if status == "" {
		status = string(sparkplug.StatusIdle)
	}
	d.setLocked(sparkplug.MetricStateStatus, status, &changed)
	d.setLocked(sparkplug.MetricStateProgress, math.Round(s.Progress*10)/10, &changed)
	d.setLocked(sparkplug.MetricTempsHotend, math.Round(s.HotendC*2)/2, &changed)
	d.setLocked(sparkplug.MetricTempsBed, math.Round(s.BedC*2)/2, &changed)
	if s.Firmware != "" {
		d.setLocked(sparkplug.MetricPropertiesFirmware, s.Firmware, &changed)
	}
	for i := 1; i <= d.slots; i++ {
		var class any
		loaded := false
		if i <= len(s.Materials) {
			m := s.Materials[i-1]
			loaded = m.Loaded
			if c := MaterialClass(m.Vendor); loaded && c != "" {
				class = c
			}
		}
		d.setLocked(sparkplug.MaterialSlotClassMetric(i), class, &changed)
		d.setLocked(sparkplug.MaterialSlotLoadedMetric(i), loaded, &changed)
	}
	d.trackJobLocked(s, now, startTimeout, &changed)
	return changed
}

// trackJobLocked advances Job/Status from what the printer reports about our file.
func (d *device) trackJobLocked(s adapters.PrinterSnapshot, now time.Time, startTimeout time.Duration, changed *[]sparkplug.MetricName) {
	j := &d.job
	if !j.active() || j.file == "" {
		return
	}
	if !fileMatches(s.JobFile, j.file) {
		if j.status == sparkplug.JobQueued && !j.startedAt.IsZero() && now.Sub(j.startedAt) > startTimeout {
			j.status = sparkplug.JobFailed
			d.setLocked(sparkplug.MetricJobStatus, string(j.status), changed)
		}
		return
	}
	next := j.status
	switch s.JobState {
	case adapters.JobStatePrinting, adapters.JobStatePaused:
		next = sparkplug.JobPrinting
	case adapters.JobStateComplete:
		next = sparkplug.JobComplete
	case adapters.JobStateCancelled:
		next = sparkplug.JobCancelled
	case adapters.JobStateFailed:
		next = sparkplug.JobFailed
		if j.cancelRequested {
			next = sparkplug.JobCancelled
		}
	case adapters.JobStateNone:
		if j.status == sparkplug.JobPrinting && j.cancelRequested {
			next = sparkplug.JobCancelled
		}
	}
	if next != j.status {
		j.status = next
		d.setLocked(sparkplug.MetricJobStatus, string(next), changed)
	}
}

// fileMatches compares the printer's job file with the stored artifact name.
func fileMatches(reported, ours string) bool {
	if reported == "" {
		return false
	}
	return reported == ours || path.Base(reported) == ours
}

func (d *device) poke() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// runDevice connects the printer through the manager (which wires its
// executor), polls it and turns changes into DBIRTH/DDATA/DDEATH.
func (n *Node) runDevice(ctx context.Context, d *device) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = n.mgr.DisconnectMachine(d.id)
			return
		case <-timer.C:
		case <-d.wake:
		}
		n.pollDevice(ctx, d)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(n.cfg.PollInterval)
	}
}

func (n *Node) pollDevice(ctx context.Context, d *device) {
	d.mu.Lock()
	connected := d.connected
	d.mu.Unlock()
	if !connected {
		if _, err := n.mgr.ConnectMachine(ctx, d.spec); err != nil {
			n.log.WithError(err).WithField("device_id", d.id).Debug("printer not reachable")
			return
		}
		d.mu.Lock()
		d.connected, d.failures = true, 0
		d.mu.Unlock()
		n.attachMotion(d)
	}
	exec, ok := n.mgr.Executor(d.id)
	src, isSource := exec.(adapters.SnapshotSource)
	if !ok || !isSource {
		n.log.WithField("device_id", d.id).Error("connected adapter cannot report status")
		return
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	snap, err := src.Snapshot(pctx)
	cancel()
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		d.mu.Lock()
		d.failures++
		lost := d.failures >= n.cfg.OfflineAfter
		wasOnline := d.online
		if lost {
			d.online, d.connected, d.failures = false, false, 0
			d.values[sparkplug.MetricStateStatus] = string(sparkplug.StatusOffline)
		}
		d.mu.Unlock()
		if lost {
			n.log.WithError(err).WithField("device_id", d.id).Warn("printer lost")
			if wasOnline {
				n.publishDeviceDeath(d)
			}
			_ = n.mgr.DisconnectMachine(d.id)
		}
		return
	}
	d.mu.Lock()
	d.failures = 0
	wasOnline := d.online
	changed := d.applySnapshotLocked(snap, n.now(), n.cfg.JobStartTimeout)
	d.online = true
	d.mu.Unlock()
	if !wasOnline {
		n.log.WithField("device_id", d.id).Info("printer online")
		n.publishDevice(d, nil) // DBIRTH
		return
	}
	if len(changed) > 0 {
		n.publishDevice(d, changed)
	}
}
