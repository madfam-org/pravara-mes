package sparkplug

import (
	"testing"
	"time"

	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

// TestMotionMetricContract pins the MES-1 §1 motion names and datatypes
// (Phase 7 amendment); a change here is a breaking change for consumers.
func TestMotionMetricContract(t *testing.T) {
	want := map[MetricName]pb.DataType{
		"Motion/Position/X": pb.DataType_Double,
		"Motion/Position/Y": pb.DataType_Double,
		"Motion/Position/Z": pb.DataType_Double,
		"Motion/Position/E": pb.DataType_Double,
		"Motion/Homed":      pb.DataType_String,
		"Motion/Velocity":   pb.DataType_Double,
	}
	if len(MotionMetrics) != len(want) {
		t.Fatalf("MotionMetrics has %d names, want %d", len(MotionMetrics), len(want))
	}
	for _, name := range MotionMetrics {
		dt, ok := DatatypeOf(name)
		if !ok || dt != want[name] {
			t.Errorf("DatatypeOf(%s) = %v %v, want %v", name, dt, ok, want[name])
		}
		if !IsMotionMetric(name) {
			t.Errorf("IsMotionMetric(%s) = false", name)
		}
	}
	for i, axis := range []string{"X", "Y", "Z", "E"} {
		if MotionPositionMetrics[i] != MetricName("Motion/Position/"+axis) {
			t.Errorf("axis %d = %s", i, MotionPositionMetrics[i])
		}
	}
	for _, n := range []MetricName{"Motion/Position/W", "Motion/", MetricStateStatus} {
		if IsMotionMetric(n) {
			t.Errorf("IsMotionMetric(%s) = true", n)
		}
	}
	// Raw doubles survive the wire unchanged, and null is allowed before the first sample.
	ts := time.UnixMilli(1_760_000_000_123)
	m, err := NewContractMetric(MetricMotionPositionX, 123.456789012345, ts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode(&pb.Payload{Metrics: []*pb.Payload_Metric{m}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := Value(p.Metrics[0], pb.DataType_Double); err != nil || v != 123.456789012345 {
		t.Fatalf("round trip = %v %v", v, err)
	}
	if n, err := NewContractMetric(MetricMotionVelocity, nil, ts); err != nil || !n.GetIsNull() {
		t.Fatalf("null velocity = %v %v", n, err)
	}
}
