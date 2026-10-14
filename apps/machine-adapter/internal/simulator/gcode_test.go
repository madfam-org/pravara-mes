package simulator

import (
	"math"
	"os"
	"testing"
	"time"
)

func loadFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/motion-path.gcode")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestParseGCodePathFixture(t *testing.T) {
	p, err := ParseGCodePath(loadFixture(t), [4]float64{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Segments) != 9 {
		t.Fatalf("segments = %d, want 9", len(p.Segments))
	}
	if len(p.Homing) != 1 || p.Homing[0].Axes != "xyz" || p.Homing[0].At != 0 {
		t.Fatalf("homing = %+v", p.Homing)
	}
	if len(p.Skipped) != 1 || p.Skipped[0] != "M400" {
		t.Fatalf("skipped = %v", p.Skipped)
	}
	// Relative E accumulates: 4 × 2.0 − 0.8.
	if end := p.End(); end[0] != 150 || end[1] != 150 || end[2] != 5 || !near(end[3], 7.2) {
		t.Fatalf("end = %v", end)
	}
	travel := p.Segments[1] // X150 Y150 at F12000 = 200 mm/s
	if !near(travel.Speed, 200) {
		t.Fatalf("travel speed = %v", travel.Speed)
	}
	wantDur := time.Duration(math.Hypot(150, 150) / 200 * float64(time.Second))
	if travel.Duration != wantDur {
		t.Fatalf("travel duration = %v, want %v", travel.Duration, wantDur)
	}
	retract := p.Segments[7] // extruder-only: F applies to E, XYZ speed 0
	if d := retract.Duration - 20*time.Millisecond; retract.Speed != 0 || d > time.Microsecond || d < -time.Microsecond {
		t.Fatalf("retract = %+v", retract)
	}
	if d := p.Duration(); d < 4800*time.Millisecond || d > 5000*time.Millisecond {
		t.Fatalf("duration = %v", d)
	}
}

func TestGCodePathInterpolates(t *testing.T) {
	p, err := ParseGCodePath([]byte("G90\nG1 X100 F6000\nG1 Y50\n"), [4]float64{10, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	// 90 mm at 100 mm/s: halfway at 0.45 s.
	pos, speed, cmd := p.At(450 * time.Millisecond)
	if !near(pos[0], 55) || pos[1] != 0 || speed != 100 || cmd != [4]float64{100, 0, 0, 0} {
		t.Fatalf("at 0.45 s: %v %v %v", pos, speed, cmd)
	}
	pos, speed, _ = p.At(10 * time.Second)
	if pos != [4]float64{100, 50, 0, 0} || speed != 0 {
		t.Fatalf("after end: %v %v", pos, speed)
	}
	if pos, _, _ := p.At(0); pos != p.Start {
		t.Fatalf("at 0: %v", pos)
	}
	// Locate follows the path in order and rejects off-path points.
	a, ok := p.Locate([4]float64{55, 0, 0, 0}, 0, 1e-9)
	if !ok || !near(a, 0.5) {
		t.Fatalf("locate mid = %v %v", a, ok)
	}
	if b, ok := p.Locate([4]float64{100, 25, 0, 0}, a, 1e-9); !ok || !near(b, 1.5) {
		t.Fatalf("locate second = %v %v", b, ok)
	}
	if _, ok := p.Locate([4]float64{55, 1, 0, 0}, 0, 1e-6); ok {
		t.Fatal("off-path point located")
	}
}

func TestGCodePathRejects(t *testing.T) {
	for _, src := range []string{"G1 X10\n", "G1 X10 F0\n", "G1 Xabc F100\n"} {
		if _, err := ParseGCodePath([]byte(src), [4]float64{}); err == nil {
			t.Errorf("%q accepted", src)
		}
	}
	p, err := ParseGCodePath([]byte("G91\nG1 X5 F600\nG1 X5\nG28 X Y\nG90\nG92 E0\n"), [4]float64{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if p.End() != [4]float64{11, 2, 3, 4} || p.HomedAt(time.Hour) != "xy" || p.HomedAt(0) != "" {
		t.Fatalf("relative/homing: %v %q", p.End(), p.HomedAt(time.Hour))
	}
}
