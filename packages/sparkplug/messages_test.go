package sparkplug

import (
	"errors"
	"testing"

	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

func TestNodeBirthAndDeathShareBdSeq(t *testing.T) {
	aliases := NewAliasTable()
	death, err := NodeDeath(42, t0)
	if err != nil {
		t.Fatal(err)
	}
	if death.Seq != nil {
		t.Fatal("NDEATH must not carry seq")
	}
	birth, err := NodeBirth(42, 0, t0, aliases)
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := BdSeqOf(birth)
	b2, _ := BdSeqOf(death)
	if b1 != 42 || b2 != 42 {
		t.Fatalf("bdSeq birth=%d death=%d", b1, b2)
	}
	if birth.GetSeq() != 0 {
		t.Fatal("NBIRTH seq")
	}
	var rebirth *pb.Payload_Metric
	for _, m := range birth.Metrics {
		if m.GetName() == string(MetricNodeControlRebirth) {
			rebirth = m
		}
	}
	if rebirth == nil || rebirth.GetBooleanValue() || pb.DataType(rebirth.GetDatatype()) != pb.DataType_Boolean {
		t.Fatal("NBIRTH needs 'Node Control/Rebirth' = false (Boolean)")
	}
	// round trip through the wire
	raw, _ := Encode(death)
	back, _ := Decode(raw)
	if v, _ := BdSeqOf(back); v != 42 {
		t.Fatal("bdSeq lost on the wire")
	}
	if _, err := BdSeqOf(&pb.Payload{}); err == nil {
		t.Fatal("missing bdSeq accepted")
	}
}

func deviceMetrics(t *testing.T) []*pb.Payload_Metric {
	t.Helper()
	var out []*pb.Payload_Metric
	for name, v := range map[MetricName]any{
		MetricStateStatus: string(StatusIdle), MetricTempsHotend: 24.5, MaterialSlotLoadedMetric(1): true,
	} {
		m, err := NewContractMetric(name, v, t0)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func TestDeviceBirthDataDeath(t *testing.T) {
	aliases := NewAliasTable()
	if _, err := NodeBirth(1, 0, t0, aliases); err != nil {
		t.Fatal(err)
	}
	var seq SeqCounter
	seq.Reset()
	seq.Next() // NBIRTH used 0
	dbirth, err := DeviceBirth("VORON-01", seq.Next(), t0, aliases, deviceMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	if dbirth.GetSeq() != 1 {
		t.Fatalf("DBIRTH seq = %d", dbirth.GetSeq())
	}
	// aliases are unique across node + devices
	other, err := DeviceBirth("A1-02", seq.Next(), t0, aliases, deviceMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uint64]bool{}
	for _, p := range []*pb.Payload{dbirth, other} {
		for _, m := range p.Metrics {
			if seen[m.GetAlias()] {
				t.Fatalf("alias %d reused", m.GetAlias())
			}
			seen[m.GetAlias()] = true
		}
	}
	hot, _ := NewContractMetric(MetricTempsHotend, 210.0, t0)
	ddata, err := DeviceData("VORON-01", seq.Next(), t0, aliases, []*pb.Payload_Metric{hot})
	if err != nil {
		t.Fatal(err)
	}
	m := ddata.Metrics[0]
	if m.Name != nil || m.Datatype != nil || m.Alias == nil || m.Timestamp == nil {
		t.Fatalf("DDATA metric must be alias-only with a timestamp: %v", m)
	}
	name, dt, ok := aliases.Resolve("VORON-01", m.GetAlias())
	if !ok || name != MetricTempsHotend {
		t.Fatalf("alias resolves to %q", name)
	}
	if v, _ := Value(m, dt); v != 210.0 {
		t.Fatalf("value = %v", v)
	}
	if _, _, ok := aliases.Resolve("A1-02", m.GetAlias()); ok {
		t.Fatal("alias resolved for the wrong device")
	}
	undeclared, _ := NewContractMetric(MetricJobID, "x", t0)
	if _, err := DeviceData("VORON-01", seq.Next(), t0, aliases, []*pb.Payload_Metric{undeclared}); !errors.Is(err, ErrMessage) {
		t.Fatal("DDATA with an undeclared metric accepted")
	}
	dd := DeviceDeath(seq.Next(), t0)
	if dd.Seq == nil || dd.Timestamp == nil || len(dd.Metrics) != 0 {
		t.Fatal("DDEATH shape")
	}
}

func TestHostLearnsAliasesFromBirth(t *testing.T) {
	edge := NewAliasTable()
	dbirth, err := DeviceBirth("VORON-01", 1, t0, edge, deviceMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	host := NewAliasTable()
	if err := host.LearnBirth("VORON-01", dbirth); err != nil {
		t.Fatal(err)
	}
	a, _ := edge.Lookup("VORON-01", MetricStateStatus)
	if n, dt, ok := host.Resolve("VORON-01", a); !ok || n != MetricStateStatus || dt != pb.DataType_String {
		t.Fatalf("host resolve = %q %s %v", n, dt, ok)
	}
	if err := host.Learn("OTHER", "Temps/Bed", a, pb.DataType_Double); err == nil {
		t.Fatal("conflicting alias accepted")
	}
}

func TestValidateBirth(t *testing.T) {
	m, _ := NewContractMetric(MetricStateStatus, "idle", t0)
	p := &pb.Payload{Metrics: []*pb.Payload_Metric{m}}
	if err := ValidateBirth(p); !errors.Is(err, ErrMessage) {
		t.Fatal("birth without timestamp accepted")
	}
	p.Timestamp, p.Seq = m.Timestamp, m.Timestamp // seq 1000 > 255
	if err := ValidateBirth(p); !errors.Is(err, ErrMessage) {
		t.Fatal("seq > 255 accepted")
	}
	if _, err := DeviceBirth("d", 1, t0, NewAliasTable(), []*pb.Payload_Metric{{}}); err == nil {
		t.Fatal("unnamed DBIRTH metric accepted")
	}
}
