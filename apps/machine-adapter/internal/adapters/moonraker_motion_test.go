package adapters

import (
	"io"

	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/simulator"
)

func quietLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}

// TestMoonrakerNotificationFormat pins the Moonraker notify_status_update
// shape: params are [status diff, eventtime].
func TestMoonrakerNotificationFormat(t *testing.T) {
	a := NewMoonrakerAdapter(&registry.MachineDefinition{}, quietLogger())
	var got []MotionSample
	a.motion.handler = func(s MotionSample) { got = append(got, s) }

	a.processWSMessage([]byte(`{"jsonrpc":"2.0","id":3,"result":{"eventtime":10.5,"status":{
		"extruder":{"temperature":24.0},
		"toolhead":{"position":[0,0,0,0],"homed_axes":""},
		"motion_report":{"live_position":[0,0,0,0],"live_velocity":0}}}}`))
	a.processWSMessage([]byte(`{"jsonrpc":"2.0","method":"notify_status_update","params":[
		{"extruder":{"temperature":215.5},"motion_report":{"live_position":[12.5,-3.25,0.3,1.0],"live_velocity":100.0}},
		11.75]}`))
	a.processWSMessage([]byte(`{"jsonrpc":"2.0","method":"notify_status_update","params":[{"toolhead":{"homed_axes":"xyz"}},12.0]}`))
	a.processWSMessage([]byte(`{"jsonrpc":"2.0","method":"notify_status_update","params":[{"heater_bed":{"temperature":60.0}},12.25]}`))

	if s := a.GetStatus(); s.ExtruderTemp != 215.5 || s.BedTemp != 60.0 {
		t.Fatalf("temperatures from notifications not applied: %+v", s)
	}
	if len(got) != 3 {
		t.Fatalf("motion samples = %d, want 3 (the bed-only update touches no motion field)", len(got))
	}
	s := got[1]
	if !s.HasPosition || s.Commanded || s.Position != [4]float64{12.5, -3.25, 0.3, 1.0} || s.Velocity != 100 || s.EventTime != 11.75 {
		t.Fatalf("sample = %+v", s)
	}
	if got[2].HomedAxes != "xyz" || got[2].Position != s.Position {
		t.Fatalf("diffs must accumulate: %+v", got[2])
	}
}

// TestMoonrakerMotionFallsBackToCommanded covers a printer without motion_report.
func TestMoonrakerMotionFallsBackToCommanded(t *testing.T) {
	a := NewMoonrakerAdapter(&registry.MachineDefinition{}, quietLogger())
	var got MotionSample
	a.motion.handler = func(s MotionSample) { got = s }
	a.processWSMessage([]byte(`{"jsonrpc":"2.0","id":1,"result":{"eventtime":1,"status":{
		"toolhead":{"position":[1,2,3,4],"homed_axes":"xy"},"motion_report":{}}}}`))
	if !got.HasPosition || !got.Commanded || got.Position != [4]float64{1, 2, 3, 4} || got.HasVelocity {
		t.Fatalf("fallback sample = %+v", got)
	}
}

// TestMoonrakerMotionStreamFromSimulator subscribes through the real
// WebSocket path and checks the rate and values the simulator emits.
func TestMoonrakerMotionStreamFromSimulator(t *testing.T) {
	sim := simulator.NewMoonraker("")
	sim.Start()
	t.Cleanup(sim.Close)
	host, port := sim.HostPort()
	a := NewMoonrakerAdapter(&registry.MachineDefinition{}, quietLogger())
	if err := a.Connect(host, port, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Disconnect() })

	var mu sync.Mutex
	var samples []MotionSample
	a.SetMotionHandler(func(s MotionSample) {
		mu.Lock()
		samples = append(samples, s)
		mu.Unlock()
	})
	// Wait for the motion subscription's initial result.
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(samples)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no motion sample after subscribing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	src := []byte("G28\nG90\nG1 X60 Y0 F6000\n") // 60 mm at 100 mm/s = 0.6 s
	path, err := sim.PlayGCode(src)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(path.Duration() + 4*simulator.DefaultMotionCadence)

	mu.Lock()
	defer mu.Unlock()
	last := samples[len(samples)-1]
	if last.Position != path.End() || last.Velocity != 0 || last.HomedAxes != "xyz" {
		t.Fatalf("last sample = %+v, want end %v at rest, homed xyz", last, path.End())
	}
	emitted := sim.Emissions()
	byPos := map[[4]float64]bool{}
	for _, e := range emitted {
		byPos[e.Position] = true
	}
	moving := 0
	for _, s := range samples {
		if !byPos[s.Position] {
			t.Errorf("adapter saw %v, which the simulator never sent", s.Position)
		}
		if s.Velocity > 0 {
			moving++
		}
	}
	// 0.6 s of motion at one update per 250 ms: 2 or 3 updates in motion.
	if moving < 2 || moving > 3 {
		t.Errorf("updates while moving = %d, want 2..3 at the 250 ms cadence", moving)
	}
}
