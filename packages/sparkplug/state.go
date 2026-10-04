package sparkplug

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// HostState is the Sparkplug 3.0 STATE payload published (retained, QoS 1) by
// a host application on spBv1.0/STATE/{host_id}.
type HostState struct {
	Online    bool   `json:"online"`
	Timestamp uint64 `json:"timestamp"` // ms since the Unix epoch, UTC
}

// ErrState is returned (wrapped) for a malformed STATE payload.
var ErrState = errors.New("sparkplug: invalid STATE payload")

// EncodeState renders the STATE JSON payload.
func EncodeState(s HostState) []byte {
	b, _ := json.Marshal(s) // cannot fail for this type
	return b
}

// DecodeState parses a STATE payload; both fields are required.
func DecodeState(b []byte) (HostState, error) {
	var raw struct {
		Online    *bool   `json:"online"`
		Timestamp *uint64 `json:"timestamp"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&raw); err != nil {
		return HostState{}, fmt.Errorf("%w: %v", ErrState, err)
	}
	if raw.Online == nil || raw.Timestamp == nil {
		return HostState{}, fmt.Errorf("%w: online and timestamp are required", ErrState)
	}
	return HostState{Online: *raw.Online, Timestamp: *raw.Timestamp}, nil
}

// HostTracker applies the edge-node rules for a configured primary host
// (spec 3.0 tck-id-message-flow-edge-node-birth-publish-phid-wait-*): a STATE
// message counts only when its timestamp is ≥ the last accepted one.
type HostTracker struct {
	seen   bool
	online bool
	last   uint64
}

// Observe applies a STATE message and returns (online, changed). Stale
// messages (older timestamp) are ignored and report changed=false.
func (h *HostTracker) Observe(s HostState) (online, changed bool) {
	if h.seen && s.Timestamp < h.last {
		return h.online, false
	}
	changed = !h.seen || h.online != s.Online
	h.seen, h.online, h.last = true, s.Online, s.Timestamp
	return h.online, changed
}

// Online reports the last accepted state.
func (h *HostTracker) Online() bool { return h.seen && h.online }
