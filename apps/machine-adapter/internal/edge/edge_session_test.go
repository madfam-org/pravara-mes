package edge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

func TestBambuStartJobMaterialsAndMediaType(t *testing.T) {
	r := newRig(t, nil)
	r.host.publishState(true, uint64(time.Now().UnixMilli()))
	r.start()
	h := r.host
	db, mark := h.waitFor("DBIRTH A1-01", 0, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "A1-01") })
	// AMS Lite: 4 trays + the external spool = 5 slots.
	want := map[sparkplug.MetricName]any{
		sparkplug.MetricPropertiesConnectivity: "bambu_lan_mqtt",
		sparkplug.MaterialSlotClassMetric(1):   "pla", sparkplug.MaterialSlotLoadedMetric(1): true,
		sparkplug.MaterialSlotClassMetric(2): "petg", sparkplug.MaterialSlotLoadedMetric(2): true,
		sparkplug.MaterialSlotClassMetric(3): nil, sparkplug.MaterialSlotLoadedMetric(3): false,
		sparkplug.MaterialSlotClassMetric(4): nil, sparkplug.MaterialSlotLoadedMetric(4): true, // TPU-AMS: loaded, no class
		sparkplug.MaterialSlotClassMetric(5): "abs", sparkplug.MaterialSlotLoadedMetric(5): true,
		sparkplug.CapabilityMaterialSlots.Metric(): int64(4),
		sparkplug.MetricStateStatus:                "idle",
	}
	for name, v := range want {
		if !h.hasValue(db, name, v) {
			got, _ := h.value(db, name)
			t.Errorf("A1 DBIRTH %s = %#v, want %#v", name, got, v)
		}
	}
	if _, ok := h.value(db, sparkplug.MaterialSlotClassMetric(6)); ok {
		t.Error("unexpected slot 6")
	}

	// G-code is refused before any download: Bambu LAN printing takes a 3MF.
	gcode := []byte("G28\n")
	h.command("A1-01", sparkplug.DeviceCommand{ID: "b0", Name: sparkplug.CommandStartJob,
		ArtifactURL: r.serve("/x.gcode", gcode), ArtifactSHA256: digest(gcode), ArtifactMediaType: "text/x-gcode"})
	refused, _ := h.waitFor("media type refused", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "A1-01") && h.hasValue(m, sparkplug.MetricCommandLastID, "b0") &&
			h.hasValue(m, sparkplug.MetricCommandStatus, "failed")
	})
	if v, _ := h.value(refused, sparkplug.MetricCommandError); !strings.Contains(v.(string), "media type") {
		t.Fatalf("Command/Error = %v", v)
	}

	project := []byte("PK\x03\x04 simulated 3mf project")
	h.command("A1-01", sparkplug.DeviceCommand{ID: "b1", Name: sparkplug.CommandStartJob, TaskID: "task-a1",
		ArtifactURL: r.serve("/p.3mf", project), ArtifactSHA256: digest(project), ArtifactMediaType: "model/3mf"})
	_, mark = h.waitFor("bambu done", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "A1-01") && h.hasValue(m, sparkplug.MetricCommandLastID, "b1") &&
			h.hasValue(m, sparkplug.MetricCommandStatus, "done")
	})
	name := artifactFileName("task-a1", digest(project), "model/3mf")
	if got := r.bambu.Files()[name]; string(got) != string(project) {
		t.Fatalf("FTPS upload %s = %q", name, got)
	}
	pf := r.bambu.LastProjectFile()
	if pf["url"] != "file:///sdcard/"+name || pf["param"] != "Metadata/plate_1.gcode" || pf["use_ams"] != true {
		t.Fatalf("project_file = %v", pf)
	}
	_, mark = h.waitFor("bambu printing", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "A1-01") && h.hasValue(m, sparkplug.MetricJobStatus, "printing")
	})
	r.bambu.SetTemps(219.7, 65)
	h.waitFor("bambu temps", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "A1-01") && h.hasValue(m, sparkplug.MetricTempsHotend, 219.5)
	})
	for i := 0; i < 4; i++ {
		r.bambu.Tick()
	}
	h.waitFor("bambu complete", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "A1-01") && h.hasValue(m, sparkplug.MetricJobStatus, "complete")
	})
	h.checkSeq()
}

func TestNodeDeathIsTheWillAndBdSeqAdvances(t *testing.T) {
	r := newRig(t, nil)
	r.host.publishState(true, uint64(time.Now().UnixMilli()))
	r.start()
	h := r.host
	_, mark := h.waitFor("DBIRTH", 0, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "A1-01") })

	r.broker.kick(t, r.cfg.ClientID) // network loss: the broker publishes the will
	death, mark := h.waitFor("NDEATH will", mark, func(m message) bool { return m.topic.Type == sparkplug.NDEATH })
	if bd, err := sparkplug.BdSeqOf(death.payload); err != nil || bd != 0 {
		t.Fatalf("will bdSeq = %d, %v", bd, err)
	}
	birth, mark := h.waitFor("NBIRTH after reconnect", mark, func(m message) bool { return m.topic.Type == sparkplug.NBIRTH })
	if bd, _ := sparkplug.BdSeqOf(birth.payload); bd != 1 || birth.payload.GetSeq() != 0 {
		t.Fatalf("second NBIRTH bdSeq=%d seq=%d", bd, birth.payload.GetSeq())
	}
	h.waitFor("DBIRTH after reconnect", mark, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "VORON-01") })
	if b, _ := os.ReadFile(filepath.Join(r.cfg.StateDir, "bdseq")); strings.TrimSpace(string(b)) != "1" {
		t.Fatalf("persisted bdSeq = %q", b)
	}

	// Intentional shutdown publishes NDEATH with the session's bdSeq.
	mark = len(h.messages())
	r.stop()
	death, _ = h.waitFor("NDEATH on stop", mark, func(m message) bool { return m.topic.Type == sparkplug.NDEATH })
	if bd, _ := sparkplug.BdSeqOf(death.payload); bd != 1 {
		t.Fatalf("shutdown NDEATH bdSeq = %d", bd)
	}
	h.checkSeq()
}

func TestBdSeqContinuesAcrossRestartAndWraps(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bdseq"), []byte("255\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := loadBdSeq(dir)
	if v, _ := s.Next(); v != 0 {
		t.Fatalf("after 255 got %d", v)
	}
	if v, _ := s.Next(); v != 1 {
		t.Fatalf("got %d", v)
	}
	if v, _ := loadBdSeq(dir).Next(); v != 2 {
		t.Fatalf("restart got %d", v)
	}
	if v, _ := loadBdSeq("").Next(); v != 0 {
		t.Fatal("memory store starts at 0")
	}
}

func TestPrimaryHostStateGatesBirths(t *testing.T) {
	r := newRig(t, nil)
	r.start()
	h := r.host
	time.Sleep(500 * time.Millisecond)
	for _, m := range h.messages() {
		if m.topic.Type == sparkplug.NBIRTH {
			t.Fatal("NBIRTH before the primary host was online")
		}
	}
	t0 := uint64(time.Now().UnixMilli())
	h.publishState(true, t0)
	_, mark := h.waitFor("NBIRTH", 0, func(m message) bool { return m.topic.Type == sparkplug.NBIRTH })

	// A stale offline (older timestamp, e.g. a late will of an old host session) is ignored.
	h.publishState(false, t0-1000)
	time.Sleep(300 * time.Millisecond)
	for _, m := range h.messages()[mark:] {
		if m.topic.Type == sparkplug.NDEATH {
			t.Fatal("stale offline STATE ended the session")
		}
	}
	// A current offline ends the session: NDEATH, reconnect, wait again.
	h.publishState(false, t0+1)
	_, mark = h.waitFor("NDEATH on host offline", mark, func(m message) bool { return m.topic.Type == sparkplug.NDEATH })
	time.Sleep(500 * time.Millisecond)
	for _, m := range h.messages()[mark:] {
		if m.topic.Type == sparkplug.NBIRTH {
			t.Fatal("NBIRTH while the host is offline")
		}
	}
	h.publishState(true, t0+2)
	birth, _ := h.waitFor("NBIRTH after host returns", mark, func(m message) bool { return m.topic.Type == sparkplug.NBIRTH })
	if bd, _ := sparkplug.BdSeqOf(birth.payload); bd != 1 {
		t.Fatalf("bdSeq after host offline = %d", bd)
	}
	h.checkSeq()
}

func TestRebirthRequestRepeatsBirthsWithSameBdSeq(t *testing.T) {
	r := newRig(t, nil)
	r.host.publishState(true, uint64(time.Now().UnixMilli()))
	r.start()
	h := r.host
	_, mark := h.waitFor("DBIRTH", 0, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "A1-01") })
	h.rebirth()
	nb, mark := h.waitFor("NBIRTH on rebirth", mark, func(m message) bool { return m.topic.Type == sparkplug.NBIRTH })
	if bd, _ := sparkplug.BdSeqOf(nb.payload); bd != 0 || nb.payload.GetSeq() != 0 {
		t.Fatalf("rebirth NBIRTH bdSeq=%d seq=%d", bd, nb.payload.GetSeq())
	}
	h.waitFor("DBIRTH VORON on rebirth", mark, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "VORON-01") })
	h.waitFor("DBIRTH A1 on rebirth", mark, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "A1-01") })
	h.checkSeq()
}

func TestDeviceLossPublishesDDEATHAndRecoveryRebirths(t *testing.T) {
	r := newRig(t, nil)
	r.host.publishState(true, uint64(time.Now().UnixMilli()))
	r.start()
	h := r.host
	_, mark := h.waitFor("DBIRTH", 0, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "VORON-01") })
	r.voron.SetDown(true)
	dd, mark := h.waitFor("DDEATH", mark, func(m message) bool { return h.is(m, sparkplug.DDEATH, "VORON-01") })
	if dd.payload.Seq == nil || len(dd.payload.Metrics) != 0 {
		t.Fatal("DDEATH shape")
	}
	// The command path refuses while the device is dead.
	h.command("VORON-01", sparkplug.DeviceCommand{ID: "off-1", Name: sparkplug.CommandPause})
	time.Sleep(300 * time.Millisecond)
	for _, m := range h.messages()[mark:] {
		if h.is(m, sparkplug.DDATA, "VORON-01") {
			t.Fatal("DDATA for a dead device")
		}
	}
	r.voron.SetDown(false)
	h.waitFor("DBIRTH after recovery", mark, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "VORON-01") })
	h.checkSeq()
}
