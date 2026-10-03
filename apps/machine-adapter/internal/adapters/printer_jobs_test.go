package adapters

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/simulator"
)

func quietLog() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}

func TestMoonrakerStates(t *testing.T) {
	cases := []struct {
		klippy, print string
		status        string
		job           JobState
	}{
		{"ready", "standby", PrinterIdle, JobStateNone},
		{"ready", "printing", PrinterPrinting, JobStatePrinting},
		{"ready", "paused", PrinterPaused, JobStatePaused},
		{"ready", "complete", PrinterIdle, JobStateComplete},
		{"ready", "cancelled", PrinterIdle, JobStateCancelled},
		{"ready", "error", PrinterError, JobStateFailed},
		{"startup", "printing", PrinterOffline, JobStateNone},
		{"shutdown", "printing", PrinterError, JobStateNone},
	}
	for _, c := range cases {
		s, j := moonrakerStates(c.klippy, c.print)
		if s != c.status || j != c.job {
			t.Errorf("%s/%s = %s/%s", c.klippy, c.print, s, j)
		}
	}
}

func TestMoonrakerMaterialFallsBackToFileMetadata(t *testing.T) {
	sim := simulator.NewMoonraker("")
	sim.Start()
	defer sim.Close()
	sim.SetFileFilament("PETG;PETG")
	a := NewMoonrakerAdapter(registry.Voron24Definition(), quietLog())
	host, port := sim.HostPort()
	if err := a.Connect(host, port, ""); err != nil {
		t.Fatal(err)
	}
	defer a.Disconnect()
	ctx := context.Background()

	snap, err := a.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Materials) != 1 || snap.Materials[0].Loaded {
		t.Fatalf("no spool and no file: %+v", snap.Materials)
	}
	if err := a.UploadFile(ctx, "part.gcode", bytes.NewReader([]byte("G28\n")), 4); err != nil {
		t.Fatal(err)
	}
	if err := a.StartPrint(ctx, "part.gcode"); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.materials = nil // drop the cache
	a.mu.Unlock()
	snap, err = a.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != PrinterPrinting || snap.JobFile != "part.gcode" || snap.Materials[0].Vendor != "PETG" || !snap.Materials[0].Loaded {
		t.Fatalf("snapshot %+v", snap)
	}
	if !a.AcceptsMediaType(MediaTypeGCode) || a.AcceptsMediaType(MediaType3MF) {
		t.Error("Moonraker media types")
	}
	if err := a.StartPrint(ctx, "missing.gcode"); err == nil {
		t.Error("start of a missing file succeeded")
	}
}

func TestBambuIncrementalReportsKeepUnsentFields(t *testing.T) {
	a := NewBambuAdapter(registry.BambuA1Definition(), quietLog())
	n, b := 210.0, 60.0
	a.processReport(&bambuPrintReport{GcodeState: "RUNNING", NozzleTemper: &n, BedTemper: &b})
	pct := 40
	a.processReport(&bambuPrintReport{MCPercent: &pct}) // P1/A1 delta: temps absent
	s := a.GetStatus()
	if s.NozzleTemp != 210 || s.BedTemp != 60 || s.PrintPercent != 40 || s.GcodeState != "RUNNING" {
		t.Fatalf("status after delta = %+v", s)
	}
}

func TestFTPSUploadRejectsBadNames(t *testing.T) {
	for _, n := range []string{"", "a/b.3mf", "..\\x", "a\r\nDELE x"} {
		if err := FTPSUpload(context.Background(), "127.0.0.1:1", "u", "p", nil, n, bytes.NewReader(nil)); err == nil {
			t.Errorf("name %q accepted", n)
		}
	}
}

func TestBambuTLSPin(t *testing.T) {
	sim := simulator.NewBambu("SER1", "code")
	if err := sim.Start(); err != nil {
		t.Fatal(err)
	}
	defer sim.Close()
	mqttPort, _ := sim.Ports()
	a := NewBambuAdapter(registry.BambuA1Definition(), quietLog())
	a.MQTTPort = mqttPort
	a.TLSPinSHA256 = "00" + sim.CertSHA256()[2:]
	if err := a.Connect(sim.Host(), "code", "SER1"); err == nil {
		a.Disconnect()
		t.Fatal("connected despite a wrong certificate pin")
	}
	b := NewBambuAdapter(registry.BambuA1Definition(), quietLog())
	b.MQTTPort = mqttPort
	b.TLSPinSHA256 = sim.CertSHA256()
	if err := b.Connect(sim.Host(), "wrong", "SER1"); err == nil {
		b.Disconnect()
		t.Fatal("connected with a wrong access code")
	}
	c := NewBambuAdapter(registry.BambuA1Definition(), quietLog())
	c.MQTTPort = mqttPort
	c.TLSPinSHA256 = sim.CertSHA256()
	if err := c.Connect(sim.Host(), "code", "SER1"); err != nil {
		t.Fatalf("pinned connect: %v", err)
	}
	c.Disconnect()
}
