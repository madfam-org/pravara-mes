package physics

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

func newTestEngine() *Engine {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)
	return NewEngine(log)
}

func defaultReq(gcode string) GCodeSimulationRequest {
	return GCodeSimulationRequest{
		MachineID:     uuid.New(),
		GCode:         gcode,
		FeedRateScale: 1.0,
		ToolDiameter:  6.0,
		Material: MaterialProps{
			Type:     "aluminum",
			Hardness: 0.5,
			Density:  2700,
		},
	}
}

// ---------------------------------------------------------------------------
// Vector3 unit tests
// ---------------------------------------------------------------------------

func TestVector3_Add(t *testing.T) {
	tests := []struct {
		name string
		a, b Vector3
		want Vector3
	}{
		{"zero vectors", Vector3{}, Vector3{}, Vector3{}},
		{"positive", Vector3{1, 2, 3}, Vector3{4, 5, 6}, Vector3{5, 7, 9}},
		{"negative", Vector3{1, 2, 3}, Vector3{-1, -2, -3}, Vector3{0, 0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Add(tt.b)
			if got != tt.want {
				t.Errorf("Add() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVector3_Subtract(t *testing.T) {
	got := Vector3{5, 7, 9}.Subtract(Vector3{1, 2, 3})
	want := Vector3{4, 5, 6}
	if got != want {
		t.Errorf("Subtract() = %v, want %v", got, want)
	}
}

func TestVector3_Magnitude(t *testing.T) {
	tests := []struct {
		name string
		v    Vector3
		want float64
	}{
		{"zero", Vector3{}, 0},
		{"unit x", Vector3{1, 0, 0}, 1},
		{"3-4-5", Vector3{3, 4, 0}, 5},
		{"3d", Vector3{1, 2, 2}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.v.Magnitude()
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("Magnitude() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVector3_Normalize(t *testing.T) {
	t.Run("zero vector returns zero", func(t *testing.T) {
		got := Vector3{}.Normalize()
		if got != (Vector3{}) {
			t.Errorf("Normalize(zero) = %v, want zero", got)
		}
	})

	t.Run("unit vector unchanged", func(t *testing.T) {
		n := Vector3{3, 4, 0}.Normalize()
		mag := n.Magnitude()
		if math.Abs(mag-1.0) > 1e-9 {
			t.Errorf("Normalize magnitude = %v, want 1.0", mag)
		}
	})
}

func TestVector3_Distance(t *testing.T) {
	d := Vector3{0, 0, 0}.Distance(Vector3{3, 4, 0})
	if math.Abs(d-5.0) > 1e-9 {
		t.Errorf("Distance() = %v, want 5.0", d)
	}
}

// ---------------------------------------------------------------------------
// BoundingBox tests
// ---------------------------------------------------------------------------

func TestBoundingBox_Contains(t *testing.T) {
	bb := BoundingBox{
		Min: Vector3{0, 0, 0},
		Max: Vector3{10, 10, 10},
	}
	tests := []struct {
		name string
		pt   Vector3
		want bool
	}{
		{"inside", Vector3{5, 5, 5}, true},
		{"on min edge", Vector3{0, 0, 0}, true},
		{"on max edge", Vector3{10, 10, 10}, true},
		{"outside x", Vector3{11, 5, 5}, false},
		{"outside negative", Vector3{-1, 5, 5}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bb.Contains(tt.pt); got != tt.want {
				t.Errorf("Contains(%v) = %v, want %v", tt.pt, got, tt.want)
			}
		})
	}
}

func TestBoundingBox_Intersects(t *testing.T) {
	a := BoundingBox{Min: Vector3{0, 0, 0}, Max: Vector3{5, 5, 5}}
	tests := []struct {
		name string
		b    BoundingBox
		want bool
	}{
		{"overlap", BoundingBox{Min: Vector3{3, 3, 3}, Max: Vector3{8, 8, 8}}, true},
		{"touching edge", BoundingBox{Min: Vector3{5, 5, 5}, Max: Vector3{10, 10, 10}}, true},
		{"no overlap", BoundingBox{Min: Vector3{6, 6, 6}, Max: Vector3{10, 10, 10}}, false},
		{"contained", BoundingBox{Min: Vector3{1, 1, 1}, Max: Vector3{2, 2, 2}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := a.Intersects(tt.b); got != tt.want {
				t.Errorf("Intersects() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SimulateGCode tests
// ---------------------------------------------------------------------------

func TestSimulateGCode_EmptyGCode(t *testing.T) {
	e := newTestEngine()
	req := defaultReq("")
	result, err := e.SimulateGCode(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolPath) != 0 {
		t.Errorf("expected 0 segments for empty gcode, got %d", len(result.ToolPath))
	}
	if result.Distance != 0 {
		t.Errorf("expected 0 distance, got %v", result.Distance)
	}
}

func TestSimulateGCode_CommentsAndBlankLines(t *testing.T) {
	gcode := `; This is a comment
(another comment)

; more comments`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolPath) != 0 {
		t.Errorf("expected 0 segments for comment-only gcode, got %d", len(result.ToolPath))
	}
}

func TestSimulateGCode_G0_RapidMove(t *testing.T) {
	gcode := "G0 X10 Y20 Z5"
	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolPath) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(result.ToolPath))
	}

	seg := result.ToolPath[0]
	if seg.Type != "rapid" {
		t.Errorf("expected type 'rapid', got %q", seg.Type)
	}
	if seg.IsCutting {
		t.Error("rapid move should not be cutting")
	}
	if seg.End.X != 10 || seg.End.Y != 20 || seg.End.Z != 5 {
		t.Errorf("unexpected end position: %v", seg.End)
	}
	if seg.FeedRate != 10000 {
		t.Errorf("rapid feed rate should be 10000, got %v", seg.FeedRate)
	}
	if result.RapidTime <= 0 {
		t.Error("rapid time should be > 0")
	}
	expectedDist := Vector3{0, 0, 0}.Distance(Vector3{10, 20, 5})
	if math.Abs(result.Distance-expectedDist) > 1e-6 {
		t.Errorf("distance = %v, want %v", result.Distance, expectedDist)
	}
}

func TestSimulateGCode_G1_LinearCut(t *testing.T) {
	gcode := `M3 S10000
G1 X50 Y0 Z-2 F500`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 1 linear segment (M3 produces no segment)
	if len(result.ToolPath) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(result.ToolPath))
	}

	seg := result.ToolPath[0]
	if seg.Type != "linear" {
		t.Errorf("expected type 'linear', got %q", seg.Type)
	}
	// Spindle is on (10000) and Z < 0, so IsCutting should be true
	if !seg.IsCutting {
		t.Error("should be cutting (spindle on, Z < 0)")
	}
	if seg.FeedRate != 500 {
		t.Errorf("feed rate = %v, want 500", seg.FeedRate)
	}
	if result.CuttingTime <= 0 {
		t.Error("cutting time should be > 0")
	}
	if result.MaterialRemoved <= 0 {
		t.Error("material removed should be > 0 for a cutting move")
	}
}

func TestSimulateGCode_G1_NotCuttingAboveZ0(t *testing.T) {
	gcode := `M3 S10000
G1 X50 Y0 Z2 F500`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	seg := result.ToolPath[0]
	if seg.IsCutting {
		t.Error("should NOT be cutting when Z >= 0")
	}
}

func TestSimulateGCode_G1_NotCuttingSpindleOff(t *testing.T) {
	gcode := "G1 X50 Y0 Z-2 F500"
	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	seg := result.ToolPath[0]
	if seg.IsCutting {
		t.Error("should NOT be cutting when spindle speed is 0")
	}
}

func TestSimulateGCode_FeedRateScale(t *testing.T) {
	gcode := "G1 X10 Y0 Z0 F1000"
	req := defaultReq(gcode)
	req.FeedRateScale = 2.0

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	seg := result.ToolPath[0]
	// FeedRate should be 1000 * 2.0 = 2000
	if math.Abs(seg.FeedRate-2000) > 1e-6 {
		t.Errorf("feed rate = %v, want 2000 (1000 * scale 2.0)", seg.FeedRate)
	}
}

func TestSimulateGCode_G2_ClockwiseArc_QuarterCircle(t *testing.T) {
	// Start at (10,0,0), arc center at (0,0,0), end at (0,-10,0)
	// I=-10 (offset from start X to center X), J=0
	// This is a 90-degree clockwise arc with radius 10
	gcode := `G0 X10 Y0 Z0
G2 X0 Y-10 I-10 J0 F500`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have rapid + multiple arc segments
	if len(result.ToolPath) < 2 {
		t.Fatalf("expected at least 2 segments, got %d", len(result.ToolPath))
	}

	// Verify arc segments exist
	arcCount := 0
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			arcCount++
		}
	}
	if arcCount == 0 {
		t.Error("expected arc segments in tool path")
	}

	// Last segment should end near (0, -10, 0)
	lastSeg := result.ToolPath[len(result.ToolPath)-1]
	if math.Abs(lastSeg.End.X-0) > 0.01 || math.Abs(lastSeg.End.Y-(-10)) > 0.01 {
		t.Errorf("arc end position = (%v, %v), want (0, -10)", lastSeg.End.X, lastSeg.End.Y)
	}

	// Arc length should be approximately pi/2 * 10 = 15.708
	// Total distance includes the initial rapid, so check arc-only distance
	arcDist := 0.0
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			arcDist += seg.Start.Distance(seg.End)
		}
	}
	expectedArcLen := math.Pi / 2 * 10
	if math.Abs(arcDist-expectedArcLen) > 0.5 {
		t.Errorf("arc distance = %v, want ~%v", arcDist, expectedArcLen)
	}
}

func TestSimulateGCode_G3_CounterClockwiseArc_QuarterCircle(t *testing.T) {
	// Start at (10,0,0), arc center at (0,0,0), end at (0,10,0)
	// CCW quarter circle
	gcode := `G0 X10 Y0 Z0
G3 X0 Y10 I-10 J0 F500`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	arcCount := 0
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			arcCount++
		}
	}
	if arcCount == 0 {
		t.Error("expected arc segments")
	}

	lastSeg := result.ToolPath[len(result.ToolPath)-1]
	if math.Abs(lastSeg.End.X-0) > 0.01 || math.Abs(lastSeg.End.Y-10) > 0.01 {
		t.Errorf("arc end = (%v, %v), want (0, 10)", lastSeg.End.X, lastSeg.End.Y)
	}
}

func TestSimulateGCode_G2_HalfCircle(t *testing.T) {
	// Start at (10,0,0), center at (0,0,0), end at (-10,0,0)
	// 180-degree clockwise arc
	gcode := `G0 X10 Y0 Z0
G2 X-10 Y0 I-10 J0 F500`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lastSeg := result.ToolPath[len(result.ToolPath)-1]
	if math.Abs(lastSeg.End.X-(-10)) > 0.01 || math.Abs(lastSeg.End.Y) > 0.01 {
		t.Errorf("half circle end = (%v, %v), want (-10, 0)", lastSeg.End.X, lastSeg.End.Y)
	}

	// Arc distance should be pi * 10 ~ 31.4
	arcDist := 0.0
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			arcDist += seg.Start.Distance(seg.End)
		}
	}
	expectedArcLen := math.Pi * 10
	if math.Abs(arcDist-expectedArcLen) > 1.0 {
		t.Errorf("half circle distance = %v, want ~%v", arcDist, expectedArcLen)
	}
}

func TestSimulateGCode_G2_HelicalArc_ZChange(t *testing.T) {
	// Helical: arc with Z change
	gcode := `G0 X10 Y0 Z0
G2 X0 Y-10 Z-5 I-10 J0 F500`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lastSeg := result.ToolPath[len(result.ToolPath)-1]
	if math.Abs(lastSeg.End.Z-(-5)) > 0.01 {
		t.Errorf("helical Z end = %v, want -5", lastSeg.End.Z)
	}

	// Verify Z interpolation: intermediate segments should have Z between 0 and -5
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			if seg.End.Z > 0.01 || seg.End.Z < -5.01 {
				t.Errorf("intermediate arc Z = %v, should be between 0 and -5", seg.End.Z)
			}
		}
	}
}

func TestSimulateGCode_DegenerateArc_VerySmallRadius(t *testing.T) {
	// I and J near zero => degenerate arc treated as linear
	gcode := `G0 X0 Y0 Z0
G2 X1 Y0 I0.0001 J0 F500`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still produce a segment (treated as linear-like arc)
	arcCount := 0
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			arcCount++
		}
	}
	// With radius < 0.001, it should be a single degenerate arc segment
	if arcCount != 1 {
		t.Errorf("expected 1 degenerate arc segment, got %d", arcCount)
	}
}

func TestSimulateGCode_ZeroLengthMove(t *testing.T) {
	// Move to same position
	gcode := "G0 X0 Y0 Z0"
	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Distance != 0 {
		t.Errorf("distance = %v, want 0 for zero-length move", result.Distance)
	}
}

func TestSimulateGCode_UnknownCommands(t *testing.T) {
	gcode := `G99 X10 Y20
M99
G0 X5 Y5`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only G0 should produce a segment; unknown commands are silently ignored
	if len(result.ToolPath) != 1 {
		t.Errorf("expected 1 segment (from G0), got %d", len(result.ToolPath))
	}
}

func TestSimulateGCode_AbsoluteVsRelativeMode(t *testing.T) {
	gcode := `G90
G0 X10 Y10 Z0
G91
G0 X5 Y5 Z0`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolPath) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.ToolPath))
	}

	// First move: absolute to (10,10,0)
	if result.ToolPath[0].End.X != 10 || result.ToolPath[0].End.Y != 10 {
		t.Errorf("first move end = %v, want (10,10,0)", result.ToolPath[0].End)
	}

	// Second move: relative +5,+5 from (10,10,0) => (15,15,0)
	if result.ToolPath[1].End.X != 15 || result.ToolPath[1].End.Y != 15 {
		t.Errorf("second move end = %v, want (15,15,0)", result.ToolPath[1].End)
	}
}

func TestSimulateGCode_SpindleOnOff(t *testing.T) {
	gcode := `M3 S12000
G1 X10 Y0 Z-1 F500
M5
G1 X20 Y0 Z-1 F500`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.ToolPath) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.ToolPath))
	}

	if !result.ToolPath[0].IsCutting {
		t.Error("first segment should be cutting (spindle on, Z < 0)")
	}
	if result.ToolPath[0].SpindleSpeed != 12000 {
		t.Errorf("spindle speed = %v, want 12000", result.ToolPath[0].SpindleSpeed)
	}

	if result.ToolPath[1].IsCutting {
		t.Error("second segment should NOT be cutting (spindle off)")
	}
	if result.ToolPath[1].SpindleSpeed != 0 {
		t.Errorf("spindle speed after M5 = %v, want 0", result.ToolPath[1].SpindleSpeed)
	}
}

func TestSimulateGCode_BoundingBox(t *testing.T) {
	gcode := `G0 X-5 Y-10 Z0
G0 X20 Y30 Z15`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bb := result.BoundingBox
	if bb.Min.X != -5 || bb.Min.Y != -10 {
		t.Errorf("bounding box min = (%v, %v), want (-5, -10)", bb.Min.X, bb.Min.Y)
	}
	if bb.Max.X != 20 || bb.Max.Y != 30 || bb.Max.Z != 15 {
		t.Errorf("bounding box max = (%v, %v, %v), want (20, 30, 15)", bb.Max.X, bb.Max.Y, bb.Max.Z)
	}
}

func TestSimulateGCode_CycleTime(t *testing.T) {
	gcode := `G0 X100 Y0 Z0
M3 S10000
G1 X200 Y0 Z-1 F1000`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.CycleTime <= 0 {
		t.Error("cycle time should be > 0")
	}
	if result.CycleTime != result.CuttingTime+result.RapidTime {
		t.Errorf("cycle time (%v) != cutting (%v) + rapid (%v)",
			result.CycleTime, result.CuttingTime, result.RapidTime)
	}
}

func TestSimulateGCode_WorkpieceCollisionDetection(t *testing.T) {
	gcode := "G0 X100 Y100 Z100"
	req := defaultReq(gcode)
	req.WorkpieceSize = BoundingBox{
		Min: Vector3{0, 0, 0},
		Max: Vector3{50, 50, 50},
	}

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Collisions) == 0 {
		t.Error("expected collision when moving outside workpiece bounds")
	}
	if result.Collisions[0].Severity != "warning" {
		t.Errorf("collision severity = %q, want 'warning'", result.Collisions[0].Severity)
	}
}

func TestSimulateGCode_MultipleSegments_TotalDistance(t *testing.T) {
	gcode := `G0 X10 Y0 Z0
G0 X10 Y10 Z0
G0 X0 Y10 Z0`

	e := newTestEngine()
	result, err := e.SimulateGCode(context.Background(), defaultReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDist := 10.0 + 10.0 + 10.0
	if math.Abs(result.Distance-expectedDist) > 1e-6 {
		t.Errorf("total distance = %v, want %v", result.Distance, expectedDist)
	}
}

// ---------------------------------------------------------------------------
// CheckCollisions tests
// ---------------------------------------------------------------------------

func TestCheckCollisions_NoObstacles(t *testing.T) {
	e := newTestEngine()
	req := CollisionCheckRequest{
		MachineID: uuid.New(),
		ToolPath: []Vector3{
			{0, 0, 0}, {10, 0, 0}, {10, 10, 0},
		},
		Obstacles: []BoundingBox{},
		ToolSize: BoundingBox{
			Min: Vector3{-1, -1, -1},
			Max: Vector3{1, 1, 1},
		},
	}

	result, err := e.CheckCollisions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Safe {
		t.Error("expected safe = true with no obstacles")
	}
	if len(result.Collisions) != 0 {
		t.Errorf("expected 0 collisions, got %d", len(result.Collisions))
	}
}

func TestCheckCollisions_WithObstacle(t *testing.T) {
	e := newTestEngine()
	req := CollisionCheckRequest{
		MachineID: uuid.New(),
		ToolPath: []Vector3{
			{0, 0, 0}, {5, 5, 0},
		},
		Obstacles: []BoundingBox{
			{Min: Vector3{4, 4, -1}, Max: Vector3{6, 6, 1}},
		},
		ToolSize: BoundingBox{
			Min: Vector3{-0.5, -0.5, -0.5},
			Max: Vector3{0.5, 0.5, 0.5},
		},
	}

	result, err := e.CheckCollisions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Safe {
		t.Error("expected safe = false when tool intersects obstacle")
	}
	if len(result.Collisions) == 0 {
		t.Error("expected at least 1 collision")
	}
}

func TestCheckCollisions_SinglePoint(t *testing.T) {
	e := newTestEngine()
	req := CollisionCheckRequest{
		MachineID: uuid.New(),
		ToolPath:  []Vector3{{0, 0, 0}},
		Obstacles: []BoundingBox{
			{Min: Vector3{-1, -1, -1}, Max: Vector3{1, 1, 1}},
		},
		ToolSize: BoundingBox{Min: Vector3{-0.5, -0.5, -0.5}, Max: Vector3{0.5, 0.5, 0.5}},
	}

	result, err := e.CheckCollisions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Single point => no segments (loop doesn't execute)
	if len(result.Collisions) != 0 {
		t.Errorf("single point should produce no collision checks, got %d", len(result.Collisions))
	}
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestUpdatePosition_Absolute(t *testing.T) {
	current := Vector3{5, 5, 5}
	params := map[string]float64{"X": 10, "Y": 20}
	got := updatePosition(current, params, true)
	if got.X != 10 || got.Y != 20 || got.Z != 5 {
		t.Errorf("absolute updatePosition = %v, want (10, 20, 5)", got)
	}
}

func TestUpdatePosition_Relative(t *testing.T) {
	current := Vector3{5, 5, 5}
	params := map[string]float64{"X": 10, "Z": -3}
	got := updatePosition(current, params, false)
	if got.X != 15 || got.Y != 5 || got.Z != 2 {
		t.Errorf("relative updatePosition = %v, want (15, 5, 2)", got)
	}
}

func TestCalculateDuration(t *testing.T) {
	start := Vector3{0, 0, 0}
	end := Vector3{100, 0, 0}
	feedRate := 1000.0 // mm/min

	d := calculateDuration(start, end, feedRate)
	// 100mm at 1000mm/min = 0.1 min = 6 seconds
	expected := time.Duration(0.1 * float64(time.Minute))
	if d != expected {
		t.Errorf("calculateDuration = %v, want %v", d, expected)
	}
}

func TestCalculatePathLength(t *testing.T) {
	path := []Vector3{
		{0, 0, 0},
		{3, 4, 0},
		{3, 4, 12},
	}
	got := calculatePathLength(path)
	want := 5.0 + 12.0
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("calculatePathLength = %v, want %v", got, want)
	}
}

func TestCalculatePathLength_Empty(t *testing.T) {
	got := calculatePathLength([]Vector3{})
	if got != 0 {
		t.Errorf("calculatePathLength([]) = %v, want 0", got)
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		input   string
		want    float64
		wantErr bool
	}{
		{"123.45", 123.45, false},
		{"-10", -10, false},
		{"0", 0, false},
		{"abc", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseFloat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseFloat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("parseFloat(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
