package adapters

import (
	"time"
)

// Klipper status objects and fields the motion subscription reads.
//
// Sources (Klipper Status Reference, https://www.klipper3d.org/Status_Reference.html):
//   - §toolhead: "position" is the last commanded position of the toolhead in
//     the config file's coordinate system; "homed_axes" is a string holding one
//     or more of "x", "y", "z".
//   - §motion_report: "live_position" is the requested toolhead position
//     interpolated to the current time; "live_velocity" is the requested
//     toolhead velocity in mm/s at the current time.
//
// Moonraker delivers changes through `printer.objects.subscribe` and the
// `notify_status_update` notification, whose params are [status diff,
// eventtime] (https://moonraker.readthedocs.io/en/latest/external_api/printer/
// and .../external_api/jsonrpc_notifications/ §Subscription Updates).
var motionSubscription = map[string]interface{}{
	"toolhead":      []string{"position", "homed_axes"},
	"motion_report": []string{"live_position", "live_velocity"},
}

// MotionSample is the printer's own motion state, unchanged. Position holds
// X, Y, Z, E in mm in the printer's coordinate system; Velocity is the XYZ
// toolhead speed in mm/s. No kinematics are applied.
type MotionSample struct {
	Position    [4]float64
	HasPosition bool
	// Commanded is true when Position comes from toolhead.position because the
	// printer reports no motion_report object.
	Commanded   bool
	Velocity    float64
	HasVelocity bool
	HomedAxes   string
	HasHomed    bool
	// EventTime is Klipper's monotonic eventtime of the update (seconds).
	EventTime float64
	// ReceivedAt is when the adapter received the update (wall clock).
	ReceivedAt time.Time
}

// MotionSource is implemented by adapters that stream live motion. The handler
// runs on the adapter's receive goroutine for every update that touches a
// motion field and must not block. Setting a handler (re)subscribes to the
// motion objects; nil unsubscribes them.
type MotionSource interface {
	SetMotionHandler(func(MotionSample))
}

// motionState accumulates the diffs of the motion subscription.
type motionState struct {
	handler       func(MotionSample)
	live          [4]float64
	liveSeen      bool
	reportMissing bool // the subscribe result had no motion_report.live_position
	commanded     [4]float64
	commandedSeen bool
	velocity      float64
	velocitySeen  bool
	homed         string
	homedSeen     bool
}

// SetMotionHandler implements MotionSource.
func (a *MoonrakerAdapter) SetMotionHandler(h func(MotionSample)) {
	a.mu.Lock()
	a.motion.handler = h
	a.mu.Unlock()
	if a.IsConnected() {
		// A new subscribe request replaces the previous one (Moonraker
		// printer.objects.subscribe), so this adds or drops the motion objects.
		if err := a.wsSubscribe(); err != nil {
			a.log.WithError(err).Debug("motion resubscribe deferred to the next WebSocket connect")
		}
	}
}

// applyMotionLocked folds toolhead/motion_report fields into the motion state
// and reports whether any motion field was present. initial marks the
// subscribe result, which lists every subscribed field. Caller holds a.mu.
func (a *MoonrakerAdapter) applyMotionLocked(status map[string]map[string]interface{}, initial bool) bool {
	touched := false
	m := &a.motion
	if th, ok := status["toolhead"]; ok {
		if v, ok := coord(th["position"]); ok {
			m.commanded, m.commandedSeen, touched = v, true, true
		}
		if h, ok := th["homed_axes"].(string); ok {
			m.homed, m.homedSeen, touched = h, true, true
		}
	}
	mr, hasReport := status["motion_report"]
	if hasReport {
		if v, ok := coord(mr["live_position"]); ok {
			m.live, m.liveSeen, touched = v, true, true
		}
		if v, ok := mr["live_velocity"]; ok {
			if f, err := toFloat64(v); err == nil {
				m.velocity, m.velocitySeen, touched = f, true, true
			}
		}
	}
	if initial {
		m.reportMissing = !m.liveSeen
		if m.reportMissing && m.handler != nil {
			a.log.Warn("printer reports no motion_report.live_position; motion falls back to toolhead.position (commanded)")
		}
	}
	return touched
}

// motionSampleLocked returns the accumulated motion state. Caller holds a.mu.
func (a *MoonrakerAdapter) motionSampleLocked(eventtime float64, at time.Time) MotionSample {
	m := a.motion
	s := MotionSample{
		Velocity: m.velocity, HasVelocity: m.velocitySeen,
		HomedAxes: m.homed, HasHomed: m.homedSeen,
		EventTime: eventtime, ReceivedAt: at,
	}
	switch {
	case m.liveSeen:
		s.Position, s.HasPosition = m.live, true
	case m.reportMissing && m.commandedSeen:
		s.Position, s.HasPosition, s.Commanded = m.commanded, true, true
	}
	return s
}

// coord parses a Klipper coordinate ([x, y, z, e] as a JSON array). The E
// component is 0 when the printer reports only three axes.
func coord(v interface{}) ([4]float64, bool) {
	var out [4]float64
	list, ok := v.([]interface{})
	if !ok || len(list) < 3 {
		return out, false
	}
	for i := 0; i < len(list) && i < 4; i++ {
		f, err := toFloat64(list[i])
		if err != nil {
			return out, false
		}
		out[i] = f
	}
	return out, true
}
