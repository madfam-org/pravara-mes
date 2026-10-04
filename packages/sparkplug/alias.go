package sparkplug

import (
	"fmt"
	"sync"

	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

// AliasTable maps metric names to aliases for one edge node. Aliases are
// unique across the edge node's entire metric set, devices included (spec 3.0,
// tck-id-payloads-alias-uniqueness). The edge node assigns them with Assign;
// a host learns them from births with Learn. Safe for concurrent use.
type AliasTable struct {
	mu      sync.RWMutex
	next    uint64
	byName  map[aliasKey]aliasEntry
	byAlias map[uint64]aliasEntry
}

type aliasKey struct {
	device string // "" for node-level metrics
	name   MetricName
}

type aliasEntry struct {
	key      aliasKey
	alias    uint64
	datatype pb.DataType
}

// NewAliasTable returns an empty table; the first assigned alias is 1.
func NewAliasTable() *AliasTable {
	return &AliasTable{next: 1, byName: map[aliasKey]aliasEntry{}, byAlias: map[uint64]aliasEntry{}}
}

// Assign returns the alias for (device, name), allocating one if needed.
// Use device "" for node-level metrics.
func (t *AliasTable) Assign(device string, name MetricName, dt pb.DataType) uint64 {
	k := aliasKey{device, name}
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.byName[k]; ok {
		return e.alias
	}
	e := aliasEntry{key: k, alias: t.next, datatype: dt}
	t.next++
	t.byName[k] = e
	t.byAlias[e.alias] = e
	return e.alias
}

// Learn records an alias seen in a birth certificate (host side). It rejects
// an alias already bound to a different metric.
func (t *AliasTable) Learn(device string, name MetricName, alias uint64, dt pb.DataType) error {
	k := aliasKey{device, name}
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.byAlias[alias]; ok && e.key != k {
		return fmt.Errorf("sparkplug: alias %d already bound to %s/%s", alias, e.key.device, e.key.name)
	}
	if old, ok := t.byName[k]; ok && old.alias != alias {
		delete(t.byAlias, old.alias)
	}
	e := aliasEntry{key: k, alias: alias, datatype: dt}
	t.byName[k] = e
	t.byAlias[alias] = e
	if alias >= t.next {
		t.next = alias + 1
	}
	return nil
}

// LearnBirth records every named+aliased metric of a birth payload.
func (t *AliasTable) LearnBirth(device string, p *pb.Payload) error {
	for _, m := range p.GetMetrics() {
		if m.Name == nil || m.Alias == nil {
			continue
		}
		if err := t.Learn(device, MetricName(m.GetName()), m.GetAlias(), pb.DataType(m.GetDatatype())); err != nil {
			return err
		}
	}
	return nil
}

// Lookup returns the alias for (device, name).
func (t *AliasTable) Lookup(device string, name MetricName) (uint64, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	e, ok := t.byName[aliasKey{device, name}]
	return e.alias, ok
}

// Resolve returns the metric an alias denotes, provided it belongs to device.
func (t *AliasTable) Resolve(device string, alias uint64) (MetricName, pb.DataType, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	e, ok := t.byAlias[alias]
	if !ok || e.key.device != device {
		return "", pb.DataType_Unknown, false
	}
	return e.key.name, e.datatype, true
}

// ResolverFor returns a resolver bound to one device, for ParseDeviceCommand.
func (t *AliasTable) ResolverFor(device string) AliasResolver {
	return func(alias uint64) (MetricName, pb.DataType, bool) { return t.Resolve(device, alias) }
}

// AliasResolver maps an alias to its metric name and datatype.
type AliasResolver func(alias uint64) (MetricName, pb.DataType, bool)
