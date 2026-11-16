package models

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/visualization-engine/internal/storage"
	"github.com/madfam-org/pravara-mes/apps/visualization-engine/internal/yantra4d"
)

// yantra4dViewerFormat is the export format the engine displays. Yantra4D
// gates export formats by tier: GLB needs its "pro" tier or above, so an
// anonymous caller (guest, STL only) is refused with a 403.
const yantra4dViewerFormat = "glb"

// modelStore is the part of the S3 client the importer uses.
type modelStore interface {
	UploadModel(ctx context.Context, key string, data io.Reader, size int64, contentType string) (string, error)
	GetPresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error)
}

// yantra4dFetcher is the part of the Yantra4D client the importer uses.
type yantra4dFetcher interface {
	GetManifest(ctx context.Context, project, token string) (*yantra4d.Manifest, error)
	Fetch(ctx context.Context, req yantra4d.RenderRequest, part, token string) (*yantra4d.Artifact, error)
}

// Yantra4DImporter imports one part of a configured Yantra4D project: it
// renders the part as GLB, downloads and verifies it (digest and GOC-1
// identity, see package yantra4d), uploads it to S3 and returns a
// MachineModel. Anything Yantra4D does not fully deliver is an error and
// nothing is uploaded.
type Yantra4DImporter struct {
	client yantra4dFetcher
	store  modelStore
	log    *logrus.Logger
}

// NewYantra4DImporter creates a new Yantra4D importer.
func NewYantra4DImporter(client *yantra4d.Client, storageClient *storage.Client, log *logrus.Logger) *Yantra4DImporter {
	imp := &Yantra4DImporter{log: log}
	// Assign only non-nil pointers: a nil *T in an interface is not nil.
	if client != nil {
		imp.client = client
	}
	if storageClient != nil {
		imp.store = storageClient
	}
	return imp
}

// Name returns the importer display name.
func (y *Yantra4DImporter) Name() string {
	return "Yantra4D Hyperobject Importer"
}

// SupportedFormats returns supported identifiers.
func (y *Yantra4DImporter) SupportedFormats() []string {
	return []string{".yantra4d"}
}

// Yantra4DImportRequest holds parameters for a Yantra4D import. Mode and Part
// may be left empty only when the project (or mode) has exactly one.
type Yantra4DImportRequest struct {
	Slug        string                 `json:"slug" binding:"required"`
	Mode        string                 `json:"mode"`
	Part        string                 `json:"part"`
	Params      map[string]interface{} `json:"params"`
	MachineType string                 `json:"machine_type"`
}

// Import imports the single mode and part of the project named by source,
// with default parameters. The caller's token, if any, is in
// opts.Metadata["jwt"] (Yantra4D honours only tokens issued for its own
// audience; any other token is treated as anonymous).
func (y *Yantra4DImporter) Import(ctx context.Context, source string, opts ImportOptions) (*MachineModel, error) {
	return y.ImportFull(ctx, Yantra4DImportRequest{Slug: source}, opts.Metadata["jwt"], opts)
}

// ImportFull performs the full import pipeline.
func (y *Yantra4DImporter) ImportFull(ctx context.Context, req Yantra4DImportRequest, token string, opts ImportOptions) (*MachineModel, error) {
	if !y.IsConfigured() {
		return nil, fmt.Errorf("yantra4d importer not configured (needs the Yantra4D client and S3 storage)")
	}
	log := y.log.WithField("slug", req.Slug)
	log.Info("Starting Yantra4D import")

	manifest, err := y.client.GetManifest(ctx, req.Slug, token)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	mode, part, err := resolveModeAndPart(manifest, req.Mode, req.Part)
	if err != nil {
		return nil, err
	}

	artifact, err := y.client.Fetch(ctx, yantra4d.RenderRequest{
		Project: req.Slug, Mode: mode, Parameters: req.Params, ExportFormat: yantra4dViewerFormat,
	}, part, token)
	if err != nil {
		return nil, fmt.Errorf("render %s/%s/%s: %w", req.Slug, mode, part, err)
	}
	log.WithFields(logrus.Fields{
		"mode": mode, "part": part, "size_bytes": len(artifact.Data),
		"sha256": artifact.SHA256, "instance_id": artifact.InstanceID,
	}).Info("Yantra4D part rendered and verified")

	key := fmt.Sprintf("models/yantra4d/%s/%s.glb", req.Slug, artifact.SHA256)
	if _, err := y.store.UploadModel(ctx, key, bytes.NewReader(artifact.Data), int64(len(artifact.Data)), artifact.MediaType); err != nil {
		return nil, fmt.Errorf("upload GLB to S3: %w", err)
	}
	modelURL, err := y.store.GetPresignedURL(ctx, key, 24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("generate presigned URL: %w", err)
	}

	name := opts.Name
	if name == "" {
		name = manifest.Project.Name
	}
	machineType := opts.MachineType
	if machineType == "" {
		machineType = req.MachineType
	}
	if machineType == "" {
		engine := manifest.Project.Engine
		if m, ok := manifest.ModeByID(mode); ok && m.Engine != "" {
			engine = m.Engine
		}
		machineType = inferMachineType(engine)
	}
	scale := opts.Scale
	if scale == 0 {
		scale = 1.0
	}

	// The bounding box is left empty: Yantra4D's manifest no longer carries
	// part dimensions (verification.geometry.dimensions is gone), and the
	// engine does not measure the GLB.
	now := time.Now()
	model := &MachineModel{
		ID:          uuid.New(),
		MachineType: machineType,
		Name:        name,
		ModelURL:    modelURL,
		Scale:       scale,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	log.WithFields(logrus.Fields{"model_id": model.ID, "machine_type": machineType}).Info("Yantra4D import complete")
	return model, nil
}

// resolveModeAndPart picks the mode and part to import. An empty mode is
// accepted only when the project has exactly one mode, an empty part only
// when the mode has exactly one part: Yantra4D itself would otherwise render
// its first mode silently, and the engine must not guess.
func resolveModeAndPart(m *yantra4d.Manifest, mode, part string) (string, string, error) {
	if mode == "" {
		if len(m.Modes) != 1 {
			return "", "", fmt.Errorf("project %q has %d modes; name the mode to import", m.Project.Slug, len(m.Modes))
		}
		mode = m.Modes[0].ID
	}
	md, ok := m.ModeByID(mode)
	if !ok {
		return "", "", fmt.Errorf("project %q has no mode %q", m.Project.Slug, mode)
	}
	if part == "" {
		if len(md.Parts) != 1 {
			return "", "", fmt.Errorf("mode %q renders %d parts; name the part to import", mode, len(md.Parts))
		}
		return mode, md.Parts[0], nil
	}
	for _, p := range md.Parts {
		if p == part {
			return mode, part, nil
		}
	}
	return "", "", fmt.Errorf("mode %q of project %q renders no part %q", mode, m.Project.Slug, part)
}

// GetManifest exposes manifest fetching for preview endpoints.
func (y *Yantra4DImporter) GetManifest(ctx context.Context, slug, token string) (*yantra4d.Manifest, error) {
	if y.client == nil {
		return nil, fmt.Errorf("yantra4d client not configured")
	}
	return y.client.GetManifest(ctx, slug, token)
}

// IsConfigured returns true if the importer has all dependencies.
func (y *Yantra4DImporter) IsConfigured() bool {
	return y.client != nil && y.store != nil
}

// inferMachineType maps a Yantra4D engine to a MES machine type.
func inferMachineType(engine string) string {
	switch engine {
	case "openscad", "scad":
		return "3d_printer_fdm"
	case "cadquery", "cq":
		return "cnc_3axis"
	case "freecad":
		return "cnc_3axis"
	default:
		return "3d_printer_fdm"
	}
}
