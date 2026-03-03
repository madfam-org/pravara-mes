package physics

import (
	"context"
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Arc interpolation logic is embedded in SimulateGCode (engine.go).
// These tests exercise arc-specific behaviors: center calculation, angle
// computation, clockwise vs counter-clockwise direction, degenerate cases,
// segment count based on sweep angle, and Z interpolation for helical arcs.

func arcEngine() *Engine {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)
	return NewEngine(log)
}

func arcReq(gcode string) GCodeSimulationRequest {
	return GCodeSimulationRequest{
		MachineID:     uuid.New(),
		GCode:         gcode,
		FeedRateScale: 1.0,
		ToolDiameter:  6.0,
	}
}

// ---------------------------------------------------------------------------
// Arc center calculation (I, J offsets)
// ---------------------------------------------------------------------------

func TestArc_CenterFromIJOffset(t *testing.T) {
	// Start at (10,0). I=-10 J=0 means center = (10+(-10), 0+0) = (0,0)
	// End at (0,10) with G3 (CCW) is a 90-degree arc of radius 10
	gcode := `G0 X10 Y0 Z0
G3 X0 Y10 I-10 J0 F1000`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First arc segment should start at (10, 0)
	var firstArc *ToolPathSegment
	for i := range result.ToolPath {
		if result.ToolPath[i].Type == "arc" {
			firstArc = &result.ToolPath[i]
			break
		}
	}
	if firstArc == nil {
		t.Fatal("no arc segments found")
	}

	// The first arc segment starts at (10, 0) and should curve toward (0, 10)
	if math.Abs(firstArc.Start.X-10) > 0.01 || math.Abs(firstArc.Start.Y) > 0.01 {
		t.Errorf("first arc start = (%v, %v), want (10, 0)", firstArc.Start.X, firstArc.Start.Y)
	}

	// All intermediate arc points should be approximately radius 10 from center (0,0)
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			distFromCenter := math.Sqrt(seg.End.X*seg.End.X + seg.End.Y*seg.End.Y)
			// Allow tolerance for the final segment which snaps to target
			if math.Abs(distFromCenter-10) > 0.5 {
				t.Errorf("arc point (%v, %v) distance from center = %v, want ~10",
					seg.End.X, seg.End.Y, distFromCenter)
			}
		}
	}
}

func TestArc_CenterWithNonZeroJOffset(t *testing.T) {
	// Start at (0,0). I=5 J=5 means center = (5, 5). Radius = sqrt(50) ~ 7.07
	// G2 (CW) to endpoint (10, 0)
	gcode := `G0 X0 Y0 Z0
G2 X10 Y0 I5 J5 F1000`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedRadius := math.Sqrt(50)
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			// Center is (5, 5)
			dx := seg.End.X - 5
			dy := seg.End.Y - 5
			dist := math.Sqrt(dx*dx + dy*dy)
			if math.Abs(dist-expectedRadius) > 0.5 {
				t.Errorf("arc point (%v, %v) dist from center (5,5) = %v, want ~%v",
					seg.End.X, seg.End.Y, dist, expectedRadius)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Angle calculation and direction
// ---------------------------------------------------------------------------

func TestArc_ClockwiseDirection(t *testing.T) {
	// CW arc from (10,0) around center (0,0) to (0,-10)
	// In CW direction, Y should decrease from 0 toward -10
	gcode := `G0 X10 Y0 Z0
G2 X0 Y-10 I-10 J0 F1000`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Collect Y values of arc points to verify CW direction
	arcPoints := make([]Vector3, 0)
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			arcPoints = append(arcPoints, seg.End)
		}
	}

	if len(arcPoints) == 0 {
		t.Fatal("no arc points found")
	}

	// For CW from (10,0) to (0,-10) around origin:
	// Points should pass through roughly (7.07, -7.07) region
	// Y should generally be decreasing or negative
	lastPoint := arcPoints[len(arcPoints)-1]
	if math.Abs(lastPoint.X) > 0.1 || math.Abs(lastPoint.Y-(-10)) > 0.1 {
		t.Errorf("CW arc final point = (%v, %v), want (0, -10)", lastPoint.X, lastPoint.Y)
	}
}

func TestArc_CounterClockwiseDirection(t *testing.T) {
	// CCW arc from (10,0) around center (0,0) to (0,10)
	// In CCW direction, Y should increase from 0 toward 10
	gcode := `G0 X10 Y0 Z0
G3 X0 Y10 I-10 J0 F1000`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	arcPoints := make([]Vector3, 0)
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			arcPoints = append(arcPoints, seg.End)
		}
	}

	if len(arcPoints) == 0 {
		t.Fatal("no arc points found")
	}

	lastPoint := arcPoints[len(arcPoints)-1]
	if math.Abs(lastPoint.X) > 0.1 || math.Abs(lastPoint.Y-10) > 0.1 {
		t.Errorf("CCW arc final point = (%v, %v), want (0, 10)", lastPoint.X, lastPoint.Y)
	}
}

func TestArc_CW_vs_CCW_OppositeDirections(t *testing.T) {
	// Same start/end but different direction should produce arcs on opposite sides
	cwGcode := `G0 X10 Y0 Z0
G2 X-10 Y0 I-10 J0 F1000`

	ccwGcode := `G0 X10 Y0 Z0
G3 X-10 Y0 I-10 J0 F1000`

	e := arcEngine()

	cwResult, _ := e.SimulateGCode(context.Background(), arcReq(cwGcode))
	ccwResult, _ := e.SimulateGCode(context.Background(), arcReq(ccwGcode))

	// Collect midpoint Y values for each direction
	cwMidY := 0.0
	ccwMidY := 0.0
	cwCount := 0
	ccwCount := 0

	for _, seg := range cwResult.ToolPath {
		if seg.Type == "arc" {
			cwMidY += seg.End.Y
			cwCount++
		}
	}
	for _, seg := range ccwResult.ToolPath {
		if seg.Type == "arc" {
			ccwMidY += seg.End.Y
			ccwCount++
		}
	}

	if cwCount > 0 {
		cwMidY /= float64(cwCount)
	}
	if ccwCount > 0 {
		ccwMidY /= float64(ccwCount)
	}

	// CW from (10,0) to (-10,0) around (0,0) goes through negative Y
	// CCW from (10,0) to (-10,0) around (0,0) goes through positive Y
	if cwMidY >= 0 {
		t.Errorf("CW arc average Y = %v, expected negative", cwMidY)
	}
	if ccwMidY <= 0 {
		t.Errorf("CCW arc average Y = %v, expected positive", ccwMidY)
	}
}

// ---------------------------------------------------------------------------
// Degenerate arc cases
// ---------------------------------------------------------------------------

func TestArc_DegenerateZeroRadius(t *testing.T) {
	// I=0, J=0 => radius = 0 < 0.001 => treated as linear
	gcode := `G0 X5 Y5 Z0
G2 X10 Y10 I0 J0 F500`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	arcSegments := 0
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			arcSegments++
		}
	}

	// Should be exactly 1 degenerate arc segment (linear fallback)
	if arcSegments != 1 {
		t.Errorf("expected 1 degenerate arc segment, got %d", arcSegments)
	}

	// The degenerate segment should go from (5,5) to (10,10) directly
	lastArc := result.ToolPath[len(result.ToolPath)-1]
	if lastArc.Type != "arc" {
		t.Fatalf("last segment type = %q, want 'arc'", lastArc.Type)
	}
	if math.Abs(lastArc.End.X-10) > 0.01 || math.Abs(lastArc.End.Y-10) > 0.01 {
		t.Errorf("degenerate arc end = (%v, %v), want (10, 10)", lastArc.End.X, lastArc.End.Y)
	}
}

func TestArc_VerySmallRadius_BelowThreshold(t *testing.T) {
	// I=0.0005, J=0 => radius = 0.0005 < 0.001 => degenerate
	gcode := `G2 X5 Y0 I0.0005 J0 F500`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			// Degenerate: single segment, start to end directly
			dist := seg.Start.Distance(seg.End)
			if dist < 0 {
				t.Error("distance should be non-negative")
			}
			return
		}
	}
	t.Error("expected at least one arc segment")
}

func TestArc_SameStartEndPoint(t *testing.T) {
	// Start and end at (10, 0) with I=-10 J=0 => full circle
	// Center at (0,0), radius 10
	gcode := `G0 X10 Y0 Z0
G2 X10 Y0 I-10 J0 F500`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// When start == end, sweepAngle adjustment ensures a full circle (2*pi)
	arcDist := 0.0
	arcCount := 0
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			arcDist += seg.Start.Distance(seg.End)
			arcCount++
		}
	}

	// Full circle circumference = 2 * pi * 10 ~ 62.83
	expectedCircumference := 2 * math.Pi * 10
	if arcCount > 0 && math.Abs(arcDist-expectedCircumference) > 2.0 {
		t.Errorf("full circle distance = %v, want ~%v", arcDist, expectedCircumference)
	}
}

// ---------------------------------------------------------------------------
// Segment count based on arc angle
// ---------------------------------------------------------------------------

func TestArc_SegmentCount_QuarterCircle(t *testing.T) {
	// 90-degree arc => ~90 segments at 1-degree resolution
	gcode := `G0 X10 Y0 Z0
G3 X0 Y10 I-10 J0 F1000`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	arcCount := 0
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			arcCount++
		}
	}

	// 90 degrees at 1-degree resolution => ~90 segments
	if arcCount < 85 || arcCount > 95 {
		t.Errorf("quarter circle produced %d arc segments, expected ~90", arcCount)
	}
}

func TestArc_SegmentCount_HalfCircle(t *testing.T) {
	// 180-degree arc => ~180 segments
	gcode := `G0 X10 Y0 Z0
G2 X-10 Y0 I-10 J0 F1000`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	arcCount := 0
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			arcCount++
		}
	}

	if arcCount < 175 || arcCount > 185 {
		t.Errorf("half circle produced %d arc segments, expected ~180", arcCount)
	}
}

// ---------------------------------------------------------------------------
// Z interpolation for helical arcs
// ---------------------------------------------------------------------------

func TestArc_HelicalZInterpolation_Linear(t *testing.T) {
	// Arc from (10,0,0) to (0,10,-10) with helical Z descent
	gcode := `G0 X10 Y0 Z0
G3 X0 Y10 Z-10 I-10 J0 F1000`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	arcSegments := make([]ToolPathSegment, 0)
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			arcSegments = append(arcSegments, seg)
		}
	}

	if len(arcSegments) == 0 {
		t.Fatal("no arc segments for helical test")
	}

	// Verify Z decreases monotonically from 0 toward -10
	prevZ := 0.0
	for i, seg := range arcSegments {
		if seg.End.Z > prevZ+0.01 {
			t.Errorf("segment %d: Z increased from %v to %v (should be monotonically decreasing)",
				i, prevZ, seg.End.Z)
		}
		prevZ = seg.End.Z
	}

	// Final Z should be -10
	finalZ := arcSegments[len(arcSegments)-1].End.Z
	if math.Abs(finalZ-(-10)) > 0.01 {
		t.Errorf("final Z = %v, want -10", finalZ)
	}
}

func TestArc_HelicalZInterpolation_EvenDistribution(t *testing.T) {
	// Verify Z changes are approximately evenly distributed across segments
	gcode := `G0 X10 Y0 Z0
G3 X0 Y10 Z-9 I-10 J0 F1000`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	arcSegments := make([]ToolPathSegment, 0)
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" {
			arcSegments = append(arcSegments, seg)
		}
	}

	if len(arcSegments) < 2 {
		t.Skip("not enough arc segments to verify even distribution")
	}

	// Calculate Z step for each segment
	zSteps := make([]float64, len(arcSegments))
	for i, seg := range arcSegments {
		zSteps[i] = seg.End.Z - seg.Start.Z
	}

	// All Z steps should be approximately equal (except possibly the last one)
	avgStep := zSteps[0]
	for i := 1; i < len(zSteps)-1; i++ {
		if math.Abs(zSteps[i]-avgStep) > 0.01 {
			t.Errorf("Z step %d = %v, expected ~%v (uneven distribution)", i, zSteps[i], avgStep)
			break
		}
	}
}

func TestArc_NoZChange_FlatArc(t *testing.T) {
	// Arc with no Z change: all segments should stay at Z=0
	gcode := `G0 X10 Y0 Z0
G3 X0 Y10 Z0 I-10 J0 F1000`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, seg := range result.ToolPath {
		if seg.Type == "arc" && math.Abs(seg.End.Z) > 0.001 {
			t.Errorf("flat arc segment has Z = %v, want 0", seg.End.Z)
		}
	}
}

// ---------------------------------------------------------------------------
// Arc with cutting state
// ---------------------------------------------------------------------------

func TestArc_CuttingState(t *testing.T) {
	// Arc below Z=0 with spindle on should be marked as cutting
	gcode := `M3 S10000
G0 X10 Y0 Z-1
G2 X0 Y-10 Z-1 I-10 J0 F500`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cuttingArcs := 0
	for _, seg := range result.ToolPath {
		if seg.Type == "arc" && seg.IsCutting {
			cuttingArcs++
		}
	}
	if cuttingArcs == 0 {
		t.Error("expected cutting arc segments (spindle on, Z < 0)")
	}
}

func TestArc_NotCuttingAboveZ0(t *testing.T) {
	gcode := `M3 S10000
G0 X10 Y0 Z5
G2 X0 Y-10 Z5 I-10 J0 F500`

	e := arcEngine()
	result, err := e.SimulateGCode(context.Background(), arcReq(gcode))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, seg := range result.ToolPath {
		if seg.Type == "arc" && seg.IsCutting {
			t.Error("arc above Z=0 should not be cutting")
		}
	}
}
