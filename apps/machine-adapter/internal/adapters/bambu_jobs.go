package adapters

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

// DefaultBambuProjectURLPrefix is where project files uploaded over FTPS are
// addressed in the project_file command. It can be overridden per printer
// (BambuAdapter.ProjectURLPrefix) if a firmware expects another form.
const DefaultBambuProjectURLPrefix = "file:///sdcard/"

// AcceptsMediaType implements JobRunner: Bambu LAN printing takes a sliced 3MF project.
func (a *BambuAdapter) AcceptsMediaType(mt string) bool {
	return mt == MediaType3MF || mt == MediaType3MFMS
}

// UploadFile implements JobRunner via implicit FTPS (port 990, user bblp).
func (a *BambuAdapter) UploadFile(ctx context.Context, name string, r io.Reader, _ int64) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}
	a.mu.RLock()
	host, code := a.host, a.accessCode
	port := 990
	if a.FTPSPort > 0 {
		port = a.FTPSPort
	}
	cfg := a.tlsConfig()
	a.mu.RUnlock()
	return FTPSUpload(ctx, net.JoinHostPort(host, strconv.Itoa(port)), "bblp", code, cfg, name, r)
}

// StartPrint implements JobRunner with the LAN "project_file" command.
func (a *BambuAdapter) StartPrint(_ context.Context, name string) error {
	a.mu.RLock()
	prefix := a.ProjectURLPrefix
	useAMS := len(a.status.Trays) > 0
	a.mu.RUnlock()
	if prefix == "" {
		prefix = DefaultBambuProjectURLPrefix
	}
	return a.publishCommand("project_file", map[string]interface{}{
		"param":          "Metadata/plate_1.gcode",
		"url":            prefix + name,
		"subtask_name":   name,
		"project_id":     "0",
		"profile_id":     "0",
		"task_id":        "0",
		"subtask_id":     "0",
		"md5":            "",
		"timelapse":      false,
		"bed_type":       "auto",
		"bed_levelling":  true,
		"flow_cali":      false,
		"vibration_cali": false,
		"layer_inspect":  false,
		"use_ams":        useAMS,
	})
}

// Snapshot implements SnapshotSource from the latest pushed report.
func (a *BambuAdapter) Snapshot(_ context.Context) (PrinterSnapshot, error) {
	if !a.IsConnected() {
		return PrinterSnapshot{}, fmt.Errorf("not connected")
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	s := a.status
	if s.GcodeState == "" {
		return PrinterSnapshot{}, fmt.Errorf("no status report received yet")
	}
	snap := PrinterSnapshot{
		HotendC:  s.NozzleTemp,
		BedC:     s.BedTemp,
		Progress: clampPercent(float64(s.PrintPercent)),
		JobFile:  s.JobFile,
	}
	switch strings.ToUpper(s.GcodeState) {
	case "PREPARE", "RUNNING", "SLICING":
		snap.Status, snap.JobState = PrinterPrinting, JobStatePrinting
	case "PAUSE":
		snap.Status, snap.JobState = PrinterPaused, JobStatePaused
	case "FINISH":
		snap.Status, snap.JobState = PrinterIdle, JobStateComplete
	case "FAILED":
		snap.Status, snap.JobState = PrinterError, JobStateFailed
	default: // IDLE
		snap.Status, snap.JobState = PrinterIdle, JobStateNone
	}
	snap.Materials = append(snap.Materials, s.Trays...)
	if s.ExternalSpool != nil {
		snap.Materials = append(snap.Materials, *s.ExternalSpool)
	}
	return snap, nil
}
