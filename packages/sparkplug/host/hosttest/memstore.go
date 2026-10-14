// Package hosttest provides an in-memory host.Store for tests of the
// Sparkplug primary host. It records everything it is told and keeps the
// latest metric values per device, so tests can assert on what a database
// store would have persisted.
package hosttest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/host"
)

// MemStore is a concurrency-safe in-memory host.Store.
type MemStore struct {
	mu sync.Mutex

	nodes   map[string]host.EdgeNode // "group/edge"
	devices map[string]bool          // registered "group/edge/device"
	pending map[string][]sparkplug.DeviceCommand

	// NodeOnline and DeviceOnline are the current liveness.
	NodeOnline   map[string]bool
	DeviceOnline map[string]bool
	// LastBdSeq is the bdSeq of each node's latest NBIRTH.
	LastBdSeq map[string]uint64
	// Live is the merged latest metric values per registered device.
	Live map[string]map[sparkplug.MetricName]any
	// Quarantined counts DBIRTHs of unregistered devices.
	Quarantined map[string]int
	// Reports are all device reports of registered devices, in order.
	Reports []Report
	// Events is a log of "NBIRTH g/e", "NDEATH g/e", "DDEATH g/e/d", ...
	Events []string
	// Touched counts liveness refreshes per device.
	Touched map[string]int
}

// Report is a recorded device report.
type Report struct {
	Key    string
	Report host.DeviceReport
}

// New returns an empty store.
func New() *MemStore {
	return &MemStore{
		nodes: map[string]host.EdgeNode{}, devices: map[string]bool{}, pending: map[string][]sparkplug.DeviceCommand{},
		NodeOnline: map[string]bool{}, DeviceOnline: map[string]bool{}, LastBdSeq: map[string]uint64{},
		Live: map[string]map[sparkplug.MetricName]any{}, Quarantined: map[string]int{}, Touched: map[string]int{},
	}
}

func nodeKey(group, edge string) string { return group + "/" + edge }

// DeviceKey is the key used for per-device maps: "group/edge/device".
func DeviceKey(group, edge, device string) string { return group + "/" + edge + "/" + device }

// RegisterNode registers an edge node under tenant group.
func (s *MemStore) RegisterNode(tenantID, group, edge string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[nodeKey(group, edge)] = host.EdgeNode{ID: "node-" + edge, TenantID: tenantID, Group: group, EdgeNodeID: edge}
}

// UnregisterNode removes (or disables) an edge node.
func (s *MemStore) UnregisterNode(group, edge string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.nodes, nodeKey(group, edge))
}

// RegisterDevice registers a machine code under an edge node.
func (s *MemStore) RegisterDevice(group, edge, device string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices[DeviceKey(group, edge, device)] = true
}

// QueueResend makes the next DBIRTH of the device return cmd for re-sending
// (a command that was sent but never acknowledged).
func (s *MemStore) QueueResend(group, edge, device string, cmd sparkplug.DeviceCommand) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := DeviceKey(group, edge, device)
	s.pending[k] = append(s.pending[k], cmd)
}

// ResolveEdgeNode implements host.Store.
func (s *MemStore) ResolveEdgeNode(_ context.Context, group, edge string) (*host.EdgeNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[nodeKey(group, edge)]
	if !ok {
		return nil, nil
	}
	return &n, nil
}

// NodeBirth implements host.Store.
func (s *MemStore) NodeBirth(_ context.Context, n host.EdgeNode, bdSeq uint64, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := nodeKey(n.Group, n.EdgeNodeID)
	s.NodeOnline[k] = true
	s.LastBdSeq[k] = bdSeq
	for dk := range s.DeviceOnline {
		if len(dk) > len(k) && dk[:len(k)+1] == k+"/" {
			s.DeviceOnline[dk] = false
		}
	}
	s.Events = append(s.Events, "NBIRTH "+k)
	return nil
}

// NodeDeath implements host.Store.
func (s *MemStore) NodeDeath(_ context.Context, n host.EdgeNode, bdSeq uint64, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := nodeKey(n.Group, n.EdgeNodeID)
	if last, ok := s.LastBdSeq[k]; !ok || last != bdSeq {
		return false, nil
	}
	s.NodeOnline[k] = false
	for dk := range s.DeviceOnline {
		if len(dk) > len(k) && dk[:len(k)+1] == k+"/" {
			s.DeviceOnline[dk] = false
		}
	}
	s.Events = append(s.Events, "NDEATH "+k)
	return true, nil
}

// DeviceReport implements host.Store.
func (s *MemStore) DeviceReport(_ context.Context, n host.EdgeNode, r host.DeviceReport) (host.DeviceOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := DeviceKey(n.Group, n.EdgeNodeID, r.DeviceID)
	if !s.devices[k] {
		if r.Birth {
			s.Quarantined[k]++
			s.Events = append(s.Events, "QUARANTINE "+k)
		}
		return host.DeviceOutcome{}, nil
	}
	if r.Birth || s.Live[k] == nil {
		s.Live[k] = map[sparkplug.MetricName]any{}
	}
	for name, v := range r.Values {
		s.Live[k][name] = v
	}
	s.Reports = append(s.Reports, Report{Key: k, Report: r})
	out := host.DeviceOutcome{Registered: true}
	if r.Birth {
		s.DeviceOnline[k] = true
		s.Events = append(s.Events, "DBIRTH "+k)
		out.Resend = s.pending[k]
		delete(s.pending, k)
	}
	return out, nil
}

// DeviceDeath implements host.Store.
func (s *MemStore) DeviceDeath(_ context.Context, n host.EdgeNode, device string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := DeviceKey(n.Group, n.EdgeNodeID, device)
	s.DeviceOnline[k] = false
	s.Events = append(s.Events, "DDEATH "+k)
	return nil
}

// Touch implements host.Store.
func (s *MemStore) Touch(_ context.Context, n host.EdgeNode, devices []string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range devices {
		s.Touched[DeviceKey(n.Group, n.EdgeNodeID, d)]++
	}
	return nil
}

// Snapshot returns a copy of the live values of one device.
func (s *MemStore) Snapshot(key string) map[sparkplug.MetricName]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[sparkplug.MetricName]any{}
	for k, v := range s.Live[key] {
		out[k] = v
	}
	return out
}

// Online reports a device's liveness.
func (s *MemStore) Online(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.DeviceOnline[key]
}

// NodeIsOnline reports a node's liveness ("group/edge").
func (s *MemStore) NodeIsOnline(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.NodeOnline[key]
}

// EventLog returns a copy of the event log.
func (s *MemStore) EventLog() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.Events...)
}

// QuarantineCount returns how many DBIRTHs of an unregistered device were quarantined.
func (s *MemStore) QuarantineCount(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Quarantined[key]
}

// ReportsFor returns the recorded reports of one device.
func (s *MemStore) ReportsFor(key string) []host.DeviceReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []host.DeviceReport
	for _, r := range s.Reports {
		if r.Key == key {
			out = append(out, r.Report)
		}
	}
	return out
}

// TouchedDevices lists devices refreshed at least once, sorted.
func (s *MemStore) TouchedDevices() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.Touched {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var _ host.Store = (*MemStore)(nil)
