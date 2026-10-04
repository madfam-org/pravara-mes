package adapters

import (
	"context"
	"io"
)

// Printer status values reported in PrinterSnapshot.Status. They are the
// MES-1 State/Status values.
const (
	PrinterIdle     = "idle"
	PrinterPrinting = "printing"
	PrinterPaused   = "paused"
	PrinterError    = "error"
	PrinterOffline  = "offline"
)

// JobState is the printer's view of the current or last print job.
type JobState string

// Job states derived from the printer's own state machine.
const (
	JobStateNone      JobState = ""
	JobStatePrinting  JobState = "printing"
	JobStatePaused    JobState = "paused"
	JobStateComplete  JobState = "complete"
	JobStateCancelled JobState = "cancelled"
	JobStateFailed    JobState = "failed"
)

// LoadedMaterial is one material slot as the printer reports it. Vendor is
// the printer's own material string (Moonraker filament_type / Spoolman
// material, Bambu AMS tray_type); mapping to a material class is done by the
// caller.
type LoadedMaterial struct {
	Vendor string
	Loaded bool
}

// PrinterSnapshot is a point-in-time view of a printer used by the edge node.
type PrinterSnapshot struct {
	Status    string  // PrinterIdle, PrinterPrinting, ...
	Progress  float64 // 0..100
	HotendC   float64
	BedC      float64
	JobState  JobState
	JobFile   string // file name of the current/last job as the printer reports it
	Firmware  string // firmware version string when the printer reports one
	Materials []LoadedMaterial
}

// SnapshotSource is implemented by adapters that can report a PrinterSnapshot.
// An error means the printer is unreachable.
type SnapshotSource interface {
	Snapshot(ctx context.Context) (PrinterSnapshot, error)
}

// JobRunner is implemented by adapters that can receive a print file and start it.
type JobRunner interface {
	// AcceptsMediaType reports whether the printer can run an artifact of this media type.
	AcceptsMediaType(mediaType string) bool
	// UploadFile stores the artifact on the printer under name.
	UploadFile(ctx context.Context, name string, r io.Reader, size int64) error
	// StartPrint starts printing a previously uploaded file.
	StartPrint(ctx context.Context, name string) error
}

// Media types the edge node dispatches (MES-1 slice targets).
const (
	MediaTypeGCode = "text/x-gcode"
	MediaType3MF   = "model/3mf"
	MediaType3MFMS = "application/vnd.ms-package.3dmanufacturing-3dmodel+xml"
)
