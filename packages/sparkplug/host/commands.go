package host

import (
	"fmt"
	"time"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

// Target addresses one device of one edge node.
type Target struct {
	Group      string // tenant slug
	EdgeNodeID string
	DeviceID   string // machine code
}

// Delivery reports what SendDeviceCommand did.
type Delivery int

const (
	// Published means the DCMD was handed to the broker connection.
	Published Delivery = iota + 1
	// Deferred means the device is not born in the current session; the
	// command reaches it through the re-send after its next DBIRTH, as long
	// as the ledger still lists it as sent and unacknowledged.
	Deferred
)

// SendDeviceCommand publishes cmd as a DCMD (QoS 0, not retained, no seq) to
// the target device. A device that is not online in the current session gets
// Deferred and nothing is published. Without a broker connection it returns
// ErrNotConnected.
func (e *Engine) SendDeviceCommand(t Target, cmd sparkplug.DeviceCommand) (Delivery, error) {
	if err := cmd.Validate(); err != nil {
		return 0, err
	}
	e.mu.Lock()
	connected := e.pub != nil
	s, ok := e.sessions[nodeKey{t.Group, t.EdgeNodeID}]
	online := ok && s.born && s.ref != nil && s.devices[t.DeviceID] == deviceOnline
	var ref EdgeNode
	if online {
		ref = *s.ref
	}
	e.mu.Unlock()
	if !connected {
		return 0, ErrNotConnected
	}
	if !online {
		e.opts.Observe("command_deferred")
		return Deferred, nil
	}
	e.mu.Lock()
	err := e.publishCommand(ref, t.DeviceID, cmd)
	e.mu.Unlock()
	if err != nil {
		return 0, err
	}
	e.opts.Observe("command_published")
	return Published, nil
}

// publishCommand builds and publishes one DCMD. e.mu must be held.
func (e *Engine) publishCommand(ref EdgeNode, device string, cmd sparkplug.DeviceCommand) error {
	if e.pub == nil {
		return ErrNotConnected
	}
	p, err := sparkplug.BuildDeviceCommand(cmd, e.opts.Now())
	if err != nil {
		return err
	}
	b, err := sparkplug.Encode(p)
	if err != nil {
		return err
	}
	topic, err := sparkplug.DeviceTopic(ref.Group, sparkplug.DCMD, ref.EdgeNodeID, device)
	if err != nil {
		return err
	}
	if err := e.pub.Publish(topic, 0, false, b); err != nil {
		return fmt.Errorf("publish DCMD: %w", err)
	}
	return nil
}

// sessionTimestamp returns a STATE timestamp for a new broker session.
func sessionTimestamp(now time.Time) uint64 { return sparkplug.Millis(now) }
