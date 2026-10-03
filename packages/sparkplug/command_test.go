package sparkplug

import (
	"errors"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

const sha = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

func startJob() DeviceCommand {
	return DeviceCommand{ID: "cmd-1", Name: CommandStartJob, TaskID: "task-9",
		ArtifactURL: "https://prep.example.test/a.gcode", ArtifactSHA256: sha, ArtifactMediaType: "text/x-gcode"}
}

func TestDeviceCommandRoundTripByName(t *testing.T) {
	p, err := BuildDeviceCommand(startJob(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if p.Seq != nil {
		t.Fatal("DCMD must not carry seq")
	}
	raw, _ := Encode(p)
	back, _ := Decode(raw)
	got, err := ParseDeviceCommand(back, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := startJob()
	want.Timestamp = 1000
	if got != want {
		t.Fatalf("got %+v", got)
	}
}

func TestDeviceCommandByAlias(t *testing.T) {
	aliases := NewAliasTable()
	var birth []*pb.Payload_Metric
	for _, n := range CommandInputMetrics {
		m, _ := NewContractMetric(n, "", t0)
		birth = append(birth, m)
	}
	if _, err := DeviceBirth("VORON-01", 1, t0, aliases, birth); err != nil {
		t.Fatal(err)
	}
	p := &pb.Payload{Timestamp: proto.Uint64(1000)}
	for n, v := range map[MetricName]string{MetricCommandID: "c2", MetricCommandName: "PAUSE"} {
		a, _ := aliases.Lookup("VORON-01", n)
		p.Metrics = append(p.Metrics, &pb.Payload_Metric{Alias: proto.Uint64(a), Value: &pb.Payload_Metric_StringValue{StringValue: v}})
	}
	if _, err := ParseDeviceCommand(p, aliases.ResolverFor("VORON-01")); !errors.Is(err, ErrCommand) {
		t.Fatal("upper-case command name accepted")
	}
	for _, m := range p.Metrics {
		if m.GetStringValue() == "PAUSE" {
			m.Value = &pb.Payload_Metric_StringValue{StringValue: "pause"}
		}
	}
	got, err := ParseDeviceCommand(p, aliases.ResolverFor("VORON-01"))
	if err != nil || got.Name != CommandPause || got.ID != "c2" {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err := ParseDeviceCommand(p, aliases.ResolverFor("A1-02")); !errors.Is(err, ErrCommand) {
		t.Fatal("aliases of another device accepted")
	}
}

func TestDeviceCommandValidation(t *testing.T) {
	mutate := []func(*DeviceCommand){
		func(c *DeviceCommand) { c.ID = "" },
		func(c *DeviceCommand) { c.Name = "home" },
		func(c *DeviceCommand) { c.ArtifactURL = "http://prep.example.test/a.gcode" },
		func(c *DeviceCommand) { c.ArtifactURL = "https:///nohost" },
		func(c *DeviceCommand) { c.ArtifactSHA256 = sha[:63] },
		func(c *DeviceCommand) { c.ArtifactSHA256 = strings.Repeat("z", 64) },
		func(c *DeviceCommand) { c.ArtifactMediaType = "" },
	}
	for i, f := range mutate {
		c := startJob()
		f(&c)
		if err := c.Validate(); !errors.Is(err, ErrCommand) {
			t.Errorf("case %d accepted: %+v", i, c)
		}
	}
	if err := (DeviceCommand{ID: "x", Name: CommandCancel}).Validate(); err != nil {
		t.Fatal("cancel needs no artifact")
	}
	p, _ := BuildDeviceCommand(DeviceCommand{ID: "x", Name: CommandCancel}, t0)
	p.Seq = proto.Uint64(3)
	if _, err := ParseDeviceCommand(p, nil); !errors.Is(err, ErrCommand) {
		t.Fatal("DCMD with seq accepted")
	}
	extra, _ := NewMetric("Command/Unknown", pb.DataType_String, "x", t0)
	p.Seq = nil
	p.Metrics = append(p.Metrics, extra)
	if _, err := ParseDeviceCommand(p, nil); !errors.Is(err, ErrCommand) {
		t.Fatal("unknown command metric accepted")
	}
	num, _ := NewMetric(MetricCommandID, pb.DataType_Int64, int64(1), t0)
	if _, err := ParseDeviceCommand(&pb.Payload{Metrics: []*pb.Payload_Metric{num}}, nil); !errors.Is(err, ErrCommand) {
		t.Fatal("non-string command metric accepted")
	}
}

func TestRebirthRequest(t *testing.T) {
	yes, _ := NewMetric(MetricNodeControlRebirth, pb.DataType_Boolean, true, t0)
	no, _ := NewMetric(MetricNodeControlRebirth, pb.DataType_Boolean, false, t0)
	if !ParseRebirthRequest(&pb.Payload{Metrics: []*pb.Payload_Metric{yes}}, nil) {
		t.Fatal("rebirth not detected")
	}
	if ParseRebirthRequest(&pb.Payload{Metrics: []*pb.Payload_Metric{no}}, nil) {
		t.Fatal("false rebirth detected")
	}
	aliases := NewAliasTable()
	birth, _ := NodeBirth(0, 0, t0, aliases)
	_ = birth
	a, _ := aliases.Lookup("", MetricNodeControlRebirth)
	byAlias := &pb.Payload{Metrics: []*pb.Payload_Metric{{Alias: proto.Uint64(a), Value: &pb.Payload_Metric_BooleanValue{BooleanValue: true}}}}
	if !ParseRebirthRequest(byAlias, aliases.ResolverFor("")) {
		t.Fatal("rebirth by alias not detected")
	}
}
