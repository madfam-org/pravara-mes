package edge

import (
	"context"
	"math"
	"sync/atomic"
	"time"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/adapters"
	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

// Motion telemetry (MES-1 §1 Motion/*). The edge node forwards Klipper's own
// values unchanged: motion_report.live_position → Motion/Position/X|Y|Z|E,
// motion_report.live_velocity → Motion/Velocity, toolhead.homed_axes →
// Motion/Homed. It does no kinematics or geometry; it only decides when a
// change is worth a DDATA (MotionConfig) and stamps each metric with the time
// the edge received it.

// motionState is a device's motion bookkeeping, guarded by device.mu.
type motionState struct {
	latest   adapters.MotionSample
	pending  bool
	lastSent time.Time
	wake     chan struct{}
}

// MotionCounters reports motion telemetry volumes since start.
type MotionCounters struct {
	Samples   uint64 `json:"samples"`   // printer updates touching a motion field
	Published uint64 `json:"published"` // motion DDATA sent
}

type motionCounters struct {
	samples, published atomic.Uint64
}

func (c *motionCounters) snapshot() MotionCounters {
	return MotionCounters{Samples: c.samples.Load(), Published: c.published.Load()}
}

// motionEnabled reports whether the device reports Motion/*.
func (d *device) motionEnabled() bool { return d.motion != nil }

// declareMotionLocked adds the motion metrics to the DBIRTH set, null until
// the printer reports them. Caller holds d.mu (or owns d).
func (d *device) declareMotionLocked() {
	for _, name := range sparkplug.MotionMetrics {
		d.values[name] = nil
	}
}

// onMotion records a printer update; it runs on the adapter's receive goroutine.
func (n *Node) onMotion(d *device, s adapters.MotionSample) {
	n.motionCount.samples.Add(1)
	d.mu.Lock()
	d.motion.latest, d.motion.pending = s, true
	d.mu.Unlock()
	select {
	case d.motion.wake <- struct{}{}:
	default:
	}
}

// attachMotion subscribes the connected adapter's motion stream to the device.
func (n *Node) attachMotion(d *device) {
	if !d.motionEnabled() {
		return
	}
	exec, ok := n.mgr.Executor(d.id)
	if !ok {
		return
	}
	src, ok := exec.(adapters.MotionSource)
	if !ok {
		n.log.WithField("device_id", d.id).Warn("adapter has no motion stream; Motion/* stays null")
		return
	}
	src.SetMotionHandler(func(s adapters.MotionSample) { n.onMotion(d, s) })
}

// runMotion publishes motion changes of one device, at most once per
// MinInterval, flushing the latest value when the interval ends.
func (n *Node) runMotion(ctx context.Context, d *device) {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.motion.wake:
		case <-timer.C:
		}
		changed, wait := n.motionDue(d, n.now())
		if wait > 0 {
			timer.Reset(wait)
			continue
		}
		if len(changed) > 0 {
			n.publishDevice(d, changed)
			n.motionCount.published.Add(1)
		}
	}
}

// motionDue folds the latest sample into the device metrics when the minimum
// interval has passed and returns the changed names, or how long to wait.
func (n *Node) motionDue(d *device, now time.Time) ([]sparkplug.MetricName, time.Duration) {
	cfg := n.cfg.Motion
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.motion
	if !m.pending {
		return nil, 0
	}
	if !m.lastSent.IsZero() {
		if elapsed := now.Sub(m.lastSent); elapsed < cfg.MinInterval {
			return nil, cfg.MinInterval - elapsed
		}
	}
	d.motion.pending = false
	changed := d.applyMotionLocked(m.latest, cfg)
	if len(changed) > 0 {
		d.motion.lastSent = now
	}
	return changed, 0
}

// applyMotionLocked decides whether the sample is worth a DDATA and, if it
// is, stores every field that differs from the last published value, so the
// published state is always one whole printer sample (never axes mixed from
// different samples). A sample passes when the homed axes change, when any
// axis moved at least PositionDeadbandMM, when the velocity changed by at
// least VelocityDeadbandMMS, or, at rest (velocity 0), when anything differs:
// the last published value therefore equals the printer's exactly. Caller
// holds d.mu.
func (d *device) applyMotionLocked(s adapters.MotionSample, cfg MotionConfig) []sparkplug.MetricName {
	type field struct {
		name sparkplug.MetricName
		v    any
	}
	var diff []field
	pass := false
	moving := s.HasVelocity && s.Velocity != 0
	if s.HasPosition {
		for i, name := range sparkplug.MotionPositionMetrics {
			v := s.Position[i]
			prev, ok := d.values[name].(float64)
			if ok && prev == v {
				continue
			}
			diff = append(diff, field{name, v})
			if !ok || math.Abs(v-prev) >= cfg.PositionDeadbandMM {
				pass = true
			}
		}
	}
	if s.HasVelocity {
		prev, ok := d.values[sparkplug.MetricMotionVelocity].(float64)
		if !ok || prev != s.Velocity {
			diff = append(diff, field{sparkplug.MetricMotionVelocity, s.Velocity})
			if !ok || math.Abs(s.Velocity-prev) >= cfg.VelocityDeadbandMMS {
				pass = true
			}
		}
	}
	if s.HasHomed {
		if prev, ok := d.values[sparkplug.MetricMotionHomed].(string); !ok || prev != s.HomedAxes {
			diff = append(diff, field{sparkplug.MetricMotionHomed, s.HomedAxes})
			pass = true
		}
	}
	if len(diff) == 0 || (!pass && moving) {
		return nil
	}
	changed := make([]sparkplug.MetricName, 0, len(diff))
	for _, f := range diff {
		d.values[f.name] = f.v
		d.stamps[f.name] = s.ReceivedAt
		changed = append(changed, f.name)
	}
	return changed
}
