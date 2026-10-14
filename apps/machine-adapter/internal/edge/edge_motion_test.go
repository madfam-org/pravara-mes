package edge

import (
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/adapters"
	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

// observedMotion is the host-side merge of Motion/* values for one device.
type observedMotion struct {
	pos      [4]float64
	known    [4]bool
	velocity float64
	homed    string
	sampled  time.Time // newest metric timestamp in the message
	received time.Time
}

// TestMotionTelemetryFollowsTheGCodePath drives the Moonraker simulator
// along the fixture path and checks that the decoded Motion/* DDATA are the
// simulator's own positions (identity), in path order, at a bounded rate,
// ending exactly at the path's end.
func TestMotionTelemetryFollowsTheGCodePath(t *testing.T) {
	r := newRig(t, func(c *Config) { c.Motion.Enabled = true })
	r.host.publishState(true, uint64(time.Now().UnixMilli()))
	r.start()
	h := r.host

	db, i := h.waitFor("DBIRTH VORON-01", 0, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "VORON-01") })
	for _, name := range sparkplug.MotionMetrics {
		if _, ok := h.value(db, name); !ok {
			t.Errorf("DBIRTH lacks %s", name)
		}
	}
	a1, _ := h.waitFor("DBIRTH A1-01", 0, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "A1-01") })
	if _, ok := h.value(a1, sparkplug.MetricMotionPositionX); ok {
		t.Error("Bambu device declares Motion/* without a motion stream")
	}
	// The subscription's initial result: at rest at the origin, not homed.
	// It arrives as a DDATA, or already in the DBIRTH when it beat the first poll.
	_, i = h.waitFor("initial motion", i, func(m message) bool {
		return (h.is(m, sparkplug.DDATA, "VORON-01") || h.is(m, sparkplug.DBIRTH, "VORON-01")) &&
			h.hasValue(m, sparkplug.MetricMotionPositionX, 0.0) && h.hasValue(m, sparkplug.MetricMotionHomed, "")
	})

	src, err := os.ReadFile("../simulator/testdata/motion-path.gcode")
	if err != nil {
		t.Fatal(err)
	}
	mark := len(h.messages())
	start := time.Now()
	path, err := r.voron.PlayGCode(src)
	if err != nil {
		t.Fatal(err)
	}
	end := path.End()
	h.waitFor("motion at the path end", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "VORON-01") && h.hasValue(m, sparkplug.MetricMotionVelocity, 0.0) &&
			h.hasValue(m, sparkplug.MetricMotionPositionZ, end[2])
	})
	elapsed := time.Since(start)

	// Replay the host's view: merge each motion DDATA into the device state.
	state := observedMotion{}
	var seen []observedMotion
	msgs := h.messages()
	for _, m := range msgs[i:] {
		if !h.is(m, sparkplug.DDATA, "VORON-01") && !h.is(m, sparkplug.DBIRTH, "VORON-01") {
			continue
		}
		touched := false
		for _, mt := range m.payload.Metrics {
			name, dt, ok := h.aliases.Resolve("VORON-01", mt.GetAlias())
			if !ok || !sparkplug.IsMotionMetric(name) {
				continue
			}
			touched = true
			v, err := sparkplug.Value(mt, dt)
			if err != nil {
				t.Fatal(err)
			}
			if v == nil {
				continue
			}
			for k, axis := range sparkplug.MotionPositionMetrics {
				if name == axis {
					state.pos[k], state.known[k] = v.(float64), true
				}
			}
			switch name {
			case sparkplug.MetricMotionVelocity:
				state.velocity = v.(float64)
			case sparkplug.MetricMotionHomed:
				state.homed = v.(string)
			}
			if ts := time.UnixMilli(int64(mt.GetTimestamp())); ts.After(state.sampled) {
				state.sampled = ts
			}
		}
		if touched {
			seen = append(seen, state)
		}
	}
	if len(seen) < 2 {
		t.Fatalf("motion DDATA observed = %d", len(seen))
	}

	// Identity: every observed tuple is one the simulator sent, in order.
	emitted := r.voron.Emissions()
	index := map[[4]float64]int{}
	for k, e := range emitted {
		if _, dup := index[e.Position]; !dup {
			index[e.Position] = k
		}
	}
	param, lastEmission := -1.0, -1
	for k, s := range seen {
		if s.known != [4]bool{true, true, true, true} {
			t.Fatalf("observation %d lacks an axis: %+v", k, s)
		}
		at, ok := index[s.pos]
		if !ok {
			t.Fatalf("observation %d %v is not a position the simulator sent", k, s.pos)
		}
		if at < lastEmission {
			t.Fatalf("observation %d goes back in the simulator's sequence", k)
		}
		lastEmission = at
		if k == 0 {
			continue // the origin, before the path starts
		}
		p, ok := path.Locate(s.pos, param, 1e-6)
		if !ok {
			t.Fatalf("observation %d %v is not on the path after parameter %.3f", k, s.pos, param)
		}
		param = p
	}
	final := seen[len(seen)-1]
	if final.pos != end || final.velocity != 0 || final.homed != "xyz" {
		t.Fatalf("final = %+v, want %v at rest, homed xyz", final, end)
	}

	// Rate: never above one motion DDATA per MinInterval (plus timer slack).
	pathMsgs := len(seen) - 1
	var gaps []time.Duration
	for k := 2; k < len(seen); k++ {
		gaps = append(gaps, seen[k].sampled.Sub(seen[k-1].sampled))
	}
	sort.Slice(gaps, func(a, b int) bool { return gaps[a] < gaps[b] })
	maxRate := float64(path.Duration()+DefaultMotionMinInterval)/float64(DefaultMotionMinInterval) + 1
	if float64(pathMsgs) > maxRate {
		t.Errorf("%d motion DDATA over %v exceeds the %v minimum interval", pathMsgs, path.Duration(), DefaultMotionMinInterval)
	}
	samples := r.node.Status().Motion
	t.Logf("path %v (%d segments); simulator sent %d motion notifications; adapter samples %d; motion DDATA %d (%.2f/s over %v); min sample gap %v, median %v",
		path.Duration(), len(path.Segments), len(emitted), samples.Samples, samples.Published,
		float64(pathMsgs)/elapsed.Seconds(), elapsed.Round(time.Millisecond), gaps[0], gaps[len(gaps)/2])
	h.checkSeq()
}

// TestMotionDecisionRules covers the deadband, rest and whole-sample rules
// without a broker.
func TestMotionDecisionRules(t *testing.T) {
	cfg := MotionConfig{Enabled: true}
	c := Config{Motion: cfg}
	c.ApplyDefaults()
	cfg = c.Motion
	d := &device{values: map[sparkplug.MetricName]any{}, stamps: map[sparkplug.MetricName]time.Time{}, motion: &motionState{}}
	d.declareMotionLocked()
	at := time.UnixMilli(1_760_000_000_000)
	sample := func(x, y, v float64, homed string) adapters.MotionSample {
		return adapters.MotionSample{Position: [4]float64{x, y, 0, 0}, HasPosition: true, Velocity: v, HasVelocity: true,
			HomedAxes: homed, HasHomed: true, ReceivedAt: at}
	}
	if got := d.applyMotionLocked(sample(0, 0, 0, ""), cfg); len(got) != 6 {
		t.Fatalf("first sample publishes every metric, got %v", got)
	}
	if got := d.applyMotionLocked(sample(0.01, 0, 50, ""), cfg); len(got) != 2 {
		// the velocity change passes, so the sub-deadband X goes with it
		t.Fatalf("velocity change: %v", got)
	}
	if got := d.applyMotionLocked(sample(0.02, 0, 50.5, ""), cfg); got != nil {
		t.Fatalf("sub-deadband change while moving published: %v", got)
	}
	if got := d.applyMotionLocked(sample(0.07, 0.001, 50.5, ""), cfg); len(got) != 3 {
		t.Fatalf("X over the deadband publishes the whole sample: %v", got)
	}
	if d.values[sparkplug.MetricMotionPositionY] != 0.001 || d.values[sparkplug.MetricMotionVelocity] != 50.5 {
		t.Fatalf("whole sample not stored: %v", d.values)
	}
	if got := d.applyMotionLocked(sample(0.0701, 0.001, 0, ""), cfg); len(got) != 2 {
		t.Fatalf("at rest every difference publishes: %v", got)
	}
	if got := d.applyMotionLocked(sample(0.0701, 0.001, 0, "xyz"), cfg); len(got) != 1 || got[0] != sparkplug.MetricMotionHomed {
		t.Fatalf("homed change: %v", got)
	}
	if d.stamps[sparkplug.MetricMotionHomed] != at {
		t.Fatal("metric timestamp is not the sample time")
	}
	bad := Config{GroupID: "acme", EdgeNodeID: "site-north", BrokerURL: "ssl://broker.example.test:8883",
		Devices: []DeviceConfig{{DeviceID: "VORON-01", Protocol: "moonraker", Host: "192.0.2.10"}},
		Motion:  MotionConfig{PositionDeadbandMM: -1}}
	bad.ApplyDefaults()
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "deadband") {
		t.Fatalf("negative deadband: %v", err)
	}
	bad.Motion.PositionDeadbandMM = 0.05
	if err := bad.Validate(); err != nil {
		t.Fatalf("valid motion config refused: %v", err)
	}
}
