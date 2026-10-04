package edge

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

func digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// allDeviceMetrics are the MES-1 §1 metric names every DBIRTH must declare
// (for a single-slot printer).
var allDeviceMetrics = []sparkplug.MetricName{
	sparkplug.MetricPropertiesModel, sparkplug.MetricPropertiesFirmware, sparkplug.MetricPropertiesConnectivity,
	sparkplug.CapabilityMaxHotendTempC.Metric(), sparkplug.CapabilityBuildVolumeXMM.Metric(),
	sparkplug.MaterialSlotClassMetric(1), sparkplug.MaterialSlotLoadedMetric(1),
	sparkplug.MetricStateStatus, sparkplug.MetricStateProgress, sparkplug.MetricTempsHotend, sparkplug.MetricTempsBed,
	sparkplug.MetricJobID, sparkplug.MetricJobStatus,
	sparkplug.MetricCommandLastID, sparkplug.MetricCommandStatus, sparkplug.MetricCommandError,
	sparkplug.MetricCommandID, sparkplug.MetricCommandName, sparkplug.MetricCommandTaskID,
	sparkplug.MetricCommandArtifactURL, sparkplug.MetricCommandArtifactSHA256, sparkplug.MetricCommandArtifactMediaType,
}

func TestEdgeBirthDataAndStartJobOnMoonraker(t *testing.T) {
	r := newRig(t, nil)
	r.host.publishState(true, uint64(time.Now().UnixMilli()))
	r.start()
	h := r.host

	nb, i := h.waitFor("NBIRTH", 0, func(m message) bool { return m.topic.Type == sparkplug.NBIRTH })
	if bd, _ := sparkplug.BdSeqOf(nb.payload); bd != 0 || nb.payload.GetSeq() != 0 {
		t.Fatalf("first NBIRTH bdSeq=%d seq=%d", bd, nb.payload.GetSeq())
	}
	if !h.hasValue(nb, sparkplug.MetricNodeControlRebirth, false) {
		t.Fatal("NBIRTH lacks Node Control/Rebirth=false")
	}
	db, _ := h.waitFor("DBIRTH VORON-01", i, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "VORON-01") })
	for _, name := range allDeviceMetrics {
		if _, ok := h.value(db, name); !ok {
			t.Errorf("DBIRTH lacks %s", name)
		}
	}
	for name, want := range map[sparkplug.MetricName]any{
		sparkplug.MetricPropertiesConnectivity:      "moonraker",
		sparkplug.MetricPropertiesModel:             "Voron Design 2.4",
		sparkplug.CapabilityMaxHotendTempC.Metric(): 300.0,
		sparkplug.CapabilityProcess.Metric():        "fff",
		sparkplug.CapabilityFirmware.Metric():       "klipper",
		sparkplug.MaterialSlotClassMetric(1):        "pla",
		sparkplug.MaterialSlotLoadedMetric(1):       true,
		sparkplug.MetricStateStatus:                 "idle",
		sparkplug.MetricTempsHotend:                 24.0,
		sparkplug.MetricCommandArtifactURL:          nil,
		sparkplug.MetricJobStatus:                   nil,
		sparkplug.CapabilityBuildVolumeZMM.Metric(): 340.0,
		sparkplug.CapabilityConnectivity.Metric():   "moonraker",
		sparkplug.MetricCommandStatus:               nil,
		sparkplug.MetricPropertiesFirmware:          "klipper",
		sparkplug.CapabilityMaxBedTempC.Metric():    120.0,
		sparkplug.CapabilityBuildVolumeXMM.Metric(): 350.0,
		sparkplug.CapabilityBuildVolumeYMM.Metric(): 350.0,
		sparkplug.MetricStateProgress:               0.0,
		sparkplug.MetricTempsBed:                    23.0,
		sparkplug.MetricCommandArtifactMediaType:    nil,
		sparkplug.MetricCommandArtifactSHA256:       nil,
		sparkplug.MetricJobID:                       nil,
		sparkplug.MetricCommandError:                nil,
		sparkplug.MetricCommandLastID:               nil,
		sparkplug.MetricCommandTaskID:               nil,
		sparkplug.MetricCommandName:                 nil,
		sparkplug.MetricCommandID:                   nil,
	} {
		if !h.hasValue(db, name, want) {
			v, _ := h.value(db, name)
			t.Errorf("DBIRTH %s = %#v, want %#v", name, v, want)
		}
	}
	h.waitFor("DBIRTH A1-01", i, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "A1-01") })

	// DDATA on change, alias-only.
	mark := len(h.messages())
	r.voron.SetTemps(215.2, 60.1)
	dd, _ := h.waitFor("DDATA hotend", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "VORON-01") && h.hasValue(m, sparkplug.MetricTempsHotend, 215.0)
	})
	for _, m := range dd.payload.Metrics {
		if m.Name != nil || m.Alias == nil {
			t.Fatal("DDATA metrics must be alias-only")
		}
	}
	if !h.hasValue(dd, sparkplug.MetricTempsBed, 60.0) {
		t.Error("bed temperature not reported")
	}

	// start_job: accepted -> running -> done, upload then start, exact bytes.
	gcode := []byte("; pravara test\nG28\nG1 X10 Y10\n")
	url := r.serve("/a.gcode", gcode)
	mark = len(h.messages())
	h.command("VORON-01", sparkplug.DeviceCommand{ID: "cmd-1", Name: sparkplug.CommandStartJob, TaskID: "task-42",
		ArtifactURL: url, ArtifactSHA256: digest(gcode), ArtifactMediaType: "text/x-gcode"})
	for _, st := range []string{"accepted", "running", "done"} {
		st := st
		_, mark = h.waitFor("Command/Status "+st, mark, func(m message) bool {
			return h.is(m, sparkplug.DDATA, "VORON-01") && h.hasValue(m, sparkplug.MetricCommandStatus, st) &&
				h.hasValue(m, sparkplug.MetricCommandLastID, "cmd-1")
		})
	}
	name := artifactFileName("task-42", digest(gcode), "text/x-gcode")
	if got := r.voron.Files()[name]; string(got) != string(gcode) {
		t.Fatalf("printer received %q under %s", got, name)
	}
	calls := strings.Join(r.voron.Calls(), ",")
	if !strings.Contains(calls, "upload "+name+",start "+name) {
		t.Fatalf("expected upload then start, got %s", calls)
	}
	h.waitFor("Job printing", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "VORON-01") && h.hasValue(m, sparkplug.MetricJobStatus, "printing")
	})
	for i := 0; i < 4; i++ {
		r.voron.Tick()
	}
	h.waitFor("Job complete", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "VORON-01") && h.hasValue(m, sparkplug.MetricJobStatus, "complete")
	})
	st := r.node.Status()
	if !st.Born || st.Devices[0].JobID != "task-42" {
		t.Fatalf("status %+v", st)
	}
	h.checkSeq()
}

func TestDigestMismatchNeverReachesThePrinter(t *testing.T) {
	r := newRig(t, nil)
	r.host.publishState(true, uint64(time.Now().UnixMilli()))
	r.start()
	h := r.host
	_, mark := h.waitFor("DBIRTH", 0, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "VORON-01") })

	served := []byte("G28\n; tampered\n")
	url := r.serve("/b.gcode", served)
	h.command("VORON-01", sparkplug.DeviceCommand{ID: "cmd-2", Name: sparkplug.CommandStartJob, TaskID: "task-7",
		ArtifactURL: url, ArtifactSHA256: digest([]byte("G28\n")), ArtifactMediaType: "text/x-gcode"})
	failed, _ := h.waitFor("Command failed", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "VORON-01") && h.hasValue(m, sparkplug.MetricCommandStatus, "failed")
	})
	if !h.hasValue(failed, sparkplug.MetricCommandError, "artifact digest mismatch") {
		v, _ := h.value(failed, sparkplug.MetricCommandError)
		t.Fatalf("Command/Error = %v", v)
	}
	if !h.hasValue(failed, sparkplug.MetricJobStatus, "failed") {
		t.Error("Job/Status must be failed")
	}
	if len(r.voron.Files()) != 0 {
		t.Fatal("a file reached the printer")
	}
	for _, c := range r.voron.Calls() {
		if strings.HasPrefix(c, "upload") || strings.HasPrefix(c, "start") {
			t.Fatalf("printer saw %q", c)
		}
	}
	entries, _ := osReadDir(r.cfg.StateDir + "/artifacts")
	if len(entries) != 0 {
		t.Fatalf("rejected artifact left on disk: %v", entries)
	}
}

func TestPauseResumeCancelGoThroughTheExecutor(t *testing.T) {
	r := newRig(t, nil)
	r.host.publishState(true, uint64(time.Now().UnixMilli()))
	r.start()
	h := r.host
	_, mark := h.waitFor("DBIRTH", 0, func(m message) bool { return h.is(m, sparkplug.DBIRTH, "VORON-01") })
	gcode := []byte("G28\n")
	h.command("VORON-01", sparkplug.DeviceCommand{ID: "s1", Name: sparkplug.CommandStartJob,
		ArtifactURL: r.serve("/c.gcode", gcode), ArtifactSHA256: digest(gcode), ArtifactMediaType: "text/x-gcode"})
	_, mark = h.waitFor("printing", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "VORON-01") && h.hasValue(m, sparkplug.MetricJobStatus, "printing")
	})

	// A second start_job while printing is refused.
	h.command("VORON-01", sparkplug.DeviceCommand{ID: "s2", Name: sparkplug.CommandStartJob,
		ArtifactURL: r.serve("/d.gcode", gcode), ArtifactSHA256: digest(gcode), ArtifactMediaType: "text/x-gcode"})
	busy, _ := h.waitFor("busy", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "VORON-01") && h.hasValue(m, sparkplug.MetricCommandLastID, "s2") &&
			h.hasValue(m, sparkplug.MetricCommandStatus, "failed")
	})
	if !h.hasValue(busy, sparkplug.MetricCommandError, "device busy") {
		t.Fatal("expected device busy")
	}

	steps := []struct {
		id    string
		name  sparkplug.CommandName
		state string
	}{{"p1", sparkplug.CommandPause, "paused"}, {"r1", sparkplug.CommandResume, "printing"}, {"x1", sparkplug.CommandCancel, "cancelled"}}
	for _, s := range steps {
		h.command("VORON-01", sparkplug.DeviceCommand{ID: s.id, Name: s.name})
		id := s.id
		_, mark = h.waitFor(string(s.name)+" done", mark, func(m message) bool {
			return h.is(m, sparkplug.DDATA, "VORON-01") && h.hasValue(m, sparkplug.MetricCommandLastID, id) &&
				h.hasValue(m, sparkplug.MetricCommandStatus, "done")
		})
		if st, _ := r.voron.State(); st != s.state {
			t.Fatalf("after %s the printer is %s", s.name, st)
		}
	}
	h.waitFor("Job cancelled", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "VORON-01") && h.hasValue(m, sparkplug.MetricJobStatus, "cancelled")
	})

	// A repeated command id is not executed twice.
	calls := len(r.voron.Calls())
	h.command("VORON-01", sparkplug.DeviceCommand{ID: "x1", Name: sparkplug.CommandCancel})
	h.waitFor("duplicate republished", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "VORON-01") && h.hasValue(m, sparkplug.MetricCommandLastID, "x1")
	})
	time.Sleep(200 * time.Millisecond)
	if len(r.voron.Calls()) != calls {
		t.Fatal("duplicate command id executed again")
	}

	// An invalid command with an id is reported as failed.
	h.command("VORON-01", sparkplug.DeviceCommand{ID: "bad-1", Name: "home"})
	h.waitFor("invalid command failed", mark, func(m message) bool {
		return h.is(m, sparkplug.DDATA, "VORON-01") && h.hasValue(m, sparkplug.MetricCommandLastID, "bad-1") &&
			h.hasValue(m, sparkplug.MetricCommandStatus, "failed")
	})
	h.checkSeq()
}
