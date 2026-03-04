package models

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/visualization-engine/internal/storage"
	"github.com/madfam-org/pravara-mes/apps/visualization-engine/internal/yantra4d"
)

// Yantra4DImporter imports configured hyperobjects from the Yantra4D platform.
// It renders a GLB via the Yantra4D API, uploads to S3, and returns a MachineModel.
type Yantra4DImporter struct {
	client        *yantra4d.Client
	storageClient *storage.Client
	log           *logrus.Logger
}

// NewYantra4DImporter creates a new Yantra4D importer.
func NewYantra4DImporter(client *yantra4d.Client, storageClient *storage.Client, log *logrus.Logger) *Yantra4DImporter {
	return &Yantra4DImporter{
		client:        client,
		storageClient: storageClient,
		log:           log,
	}
}

// Name returns the importer display name.
func (y *Yantra4DImporter) Name() string {
	return "Yantra4D Hyperobject Importer"
}

// SupportedFormats returns supported identifiers.
func (y *Yantra4DImporter) SupportedFormats() []string {
	return []string{".yantra4d"}
}

// Yantra4DImportRequest holds parameters for a Yantra4D import.
type Yantra4DImportRequest struct {
	Slug        string                 `json:"slug" binding:"required"`
	Params      map[string]interface{} `json:"params"`
	MachineType string                 `json:"machine_type"`
}

// Import fetches a configured hyperobject from Yantra4D, renders it to GLB,
// uploads to S3, and returns a MachineModel ready for persistence.
//
// The source parameter is the project slug.
// The JWT must be available in opts.Metadata["jwt"].
func (y *Yantra4DImporter) Import(ctx context.Context, source string, opts ImportOptions) (*MachineModel, error) {
	jwt := opts.Metadata["jwt"]
	if jwt == "" {
		return nil, fmt.Errorf("yantra4d import requires JWT in metadata")
	}

	params := make(map[string]interface{})
	if raw, ok := opts.Metadata["params"]; ok && raw != "" {
		// Params passed as serialized metadata — the handler provides typed params directly via ImportFull
		_ = raw
	}

	return y.ImportFull(ctx, source, params, jwt, opts)
}

// ImportFull performs the full import pipeline with typed parameters.
func (y *Yantra4DImporter) ImportFull(ctx context.Context, slug string, params map[string]interface{}, jwt string, opts ImportOptions) (*MachineModel, error) {
	y.log.WithField("slug", slug).Info("Starting Yantra4D import")

	// 1. Fetch manifest
	manifest, err := y.client.GetManifest(ctx, slug, jwt)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}

	y.log.WithFields(logrus.Fields{
		"project": manifest.Project.Name,
		"version": manifest.Project.Version,
		"engine":  manifest.Project.Engine,
	}).Info("Manifest fetched")

	// 2. Render GLB
	glbData, contentType, err := y.client.Render(ctx, slug, params, "glb", jwt)
	if err != nil {
		return nil, fmt.Errorf("render GLB: %w", err)
	}

	y.log.WithField("size_bytes", len(glbData)).Info("GLB rendered")

	// 3. Upload to S3
	if y.storageClient == nil {
		return nil, fmt.Errorf("S3 storage not configured")
	}

	key := fmt.Sprintf("models/yantra4d/%s_%d.glb", slug, time.Now().UnixNano())
	_, err = y.storageClient.UploadModel(ctx, key, bytes.NewReader(glbData), int64(len(glbData)), contentType)
	if err != nil {
		return nil, fmt.Errorf("upload GLB to S3: %w", err)
	}

	// 4. Get presigned download URL
	modelURL, err := y.storageClient.GetPresignedURL(ctx, key, 24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("generate presigned URL: %w", err)
	}

	// 5. Build MachineModel from manifest dimensions
	name := opts.Name
	if name == "" {
		name = manifest.Project.Name
	}

	machineType := opts.MachineType
	if machineType == "" {
		machineType = inferMachineType(manifest.Project.Engine)
	}

	scale := opts.Scale
	if scale == 0 {
		scale = 1.0
	}

	bbox := BoundingBox{}
	dims := manifest.Verification.Geometry.Dimensions
	if dims[0] > 0 || dims[1] > 0 || dims[2] > 0 {
		bbox = BoundingBox{
			Min: Vector3{X: 0, Y: 0, Z: 0},
			Max: Vector3{X: dims[0], Y: dims[1], Z: dims[2]},
		}
	}

	model := &MachineModel{
		ID:          uuid.New(),
		MachineType: machineType,
		Name:        name,
		ModelURL:    modelURL,
		Scale:       scale,
		BoundingBox: bbox,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	y.log.WithFields(logrus.Fields{
		"model_id":     model.ID,
		"model_url":    modelURL,
		"machine_type": machineType,
	}).Info("Yantra4D import complete")

	return model, nil
}

// GetManifest exposes manifest fetching for preview endpoints.
func (y *Yantra4DImporter) GetManifest(ctx context.Context, slug, jwt string) (*yantra4d.Manifest, error) {
	return y.client.GetManifest(ctx, slug, jwt)
}

// IsConfigured returns true if the importer has all dependencies.
func (y *Yantra4DImporter) IsConfigured() bool {
	return y.client != nil && y.storageClient != nil
}

// inferMachineType maps Yantra4D engine to a MES machine type.
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
