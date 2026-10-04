package sparkplug

import (
	"errors"
	"testing"
)

func TestTopicBuildAndParse(t *testing.T) {
	cases := []struct {
		build func() (string, error)
		want  string
		parse Topic
	}{
		{func() (string, error) { return NodeTopic("acme", NBIRTH, "site-north") },
			"spBv1.0/acme/NBIRTH/site-north", Topic{GroupID: "acme", Type: NBIRTH, EdgeNodeID: "site-north"}},
		{func() (string, error) { return NodeTopic("acme", NCMD, "site-north") },
			"spBv1.0/acme/NCMD/site-north", Topic{GroupID: "acme", Type: NCMD, EdgeNodeID: "site-north"}},
		{func() (string, error) { return DeviceTopic("acme", DDATA, "site-north", "VORON-01") },
			"spBv1.0/acme/DDATA/site-north/VORON-01", Topic{GroupID: "acme", Type: DDATA, EdgeNodeID: "site-north", DeviceID: "VORON-01"}},
		{func() (string, error) { return DeviceTopic("acme", DCMD, "site-north", "A1-02") },
			"spBv1.0/acme/DCMD/site-north/A1-02", Topic{GroupID: "acme", Type: DCMD, EdgeNodeID: "site-north", DeviceID: "A1-02"}},
		{func() (string, error) { return StateTopic(PrimaryHostID) },
			"spBv1.0/STATE/pravara-mes", Topic{Type: STATE, HostID: "pravara-mes"}},
	}
	for _, c := range cases {
		got, err := c.build()
		if err != nil || got != c.want {
			t.Fatalf("build = %q, %v; want %q", got, err, c.want)
		}
		parsed, err := ParseTopic(got)
		if err != nil || parsed != c.parse {
			t.Fatalf("parse(%q) = %+v, %v", got, parsed, err)
		}
	}
	if SiteEdgeNodeID("north") != "site-north" {
		t.Fatal("SiteEdgeNodeID")
	}
}

func TestTopicRejects(t *testing.T) {
	bad := []string{
		"spAv1.0/acme/NBIRTH/e", "spBv1.0/acme/NBIRTH", "spBv1.0/acme/NBIRTH/e/d",
		"spBv1.0/acme/DDATA/e", "spBv1.0/acme/BOGUS/e", "spBv1.0/STATE/a/b", "spBv1.0/STATE/",
		"spBv1.0/acme/DDATA/e/+", "spBv1.0//NBIRTH/e", "spBv1.0/STATE/NBIRTH/e",
	}
	for _, s := range bad {
		if _, err := ParseTopic(s); !errors.Is(err, ErrInvalidTopic) {
			t.Errorf("ParseTopic(%q) err = %v", s, err)
		}
	}
	if _, err := NodeTopic("acme", DDATA, "e"); err == nil {
		t.Error("device type accepted for node topic")
	}
	if _, err := DeviceTopic("acme", NDATA, "e", "d"); err == nil {
		t.Error("node type accepted for device topic")
	}
	for _, id := range []string{"", "a/b", "a+b", "a#", "\xff"} {
		if err := ValidateID("device_id", id); err == nil {
			t.Errorf("ValidateID(%q) accepted", id)
		}
	}
	if _, err := NodeTopic("STATE", NBIRTH, "e"); err == nil {
		t.Error("group STATE accepted")
	}
}

func TestMaterialSlotMetrics(t *testing.T) {
	if MaterialSlotClassMetric(2) != "Materials/Slot2/Class" || MaterialSlotLoadedMetric(5) != "Materials/Slot5/Loaded" {
		t.Fatal("slot metric names")
	}
	slot, field, ok := ParseMaterialSlotMetric("Materials/Slot12/Loaded")
	if !ok || slot != 12 || field != "Loaded" {
		t.Fatalf("parse = %d %q %v", slot, field, ok)
	}
	for _, n := range []MetricName{"Materials/Slot0/Class", "Materials/SlotX/Class", "Materials/Slot1/Color", "Temps/Bed"} {
		if _, _, ok := ParseMaterialSlotMetric(n); ok {
			t.Errorf("%s parsed", n)
		}
	}
	if CapabilityMaxHotendTempC.Metric() != "Capabilities/max_hotend_temp_c" {
		t.Fatal("capability metric name")
	}
	for name, ok := range map[MetricName]bool{
		MetricStateProgress: true, "Capabilities/enclosure": true, "Materials/Slot1/Loaded": true, "Nope": false,
	} {
		if _, got := DatatypeOf(name); got != ok {
			t.Errorf("DatatypeOf(%s) = %v", name, got)
		}
	}
}
