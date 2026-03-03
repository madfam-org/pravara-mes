package models

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GLTFImporter handles direct GLTF/GLB file imports and STL conversion.
type GLTFImporter struct {
	storageDir string // Local temp directory for processing
}

// NewGLTFImporter creates a new GLTF/STL importer.
func NewGLTFImporter(storageDir string) *GLTFImporter {
	return &GLTFImporter{storageDir: storageDir}
}

// Name returns the importer display name.
func (g *GLTFImporter) Name() string {
	return "GLTF/STL File Importer"
}

// SupportedFormats returns supported file extensions.
func (g *GLTFImporter) SupportedFormats() []string {
	return []string{".gltf", ".glb", ".stl"}
}

// Import processes a GLTF/GLB/STL file from a local path or URL.
// For GLTF/GLB files, this is a passthrough with metadata extraction.
// For STL files, basic metadata is extracted (full conversion requires external tooling).
func (g *GLTFImporter) Import(ctx context.Context, source string, opts ImportOptions) (*MachineModel, error) {
	ext := strings.ToLower(filepath.Ext(source))

	switch ext {
	case ".gltf", ".glb":
		return g.importGLTF(ctx, source, opts)
	case ".stl":
		return g.importSTL(ctx, source, opts)
	default:
		return nil, fmt.Errorf("unsupported format: %s", ext)
	}
}

func (g *GLTFImporter) importGLTF(ctx context.Context, source string, opts ImportOptions) (*MachineModel, error) {
	// Verify file exists
	info, err := os.Stat(source)
	if err != nil {
		return nil, fmt.Errorf("cannot access file %s: %w", source, err)
	}

	name := opts.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	}

	scale := opts.Scale
	if scale == 0 {
		scale = 1.0
	}

	model := &MachineModel{
		ID:          uuid.New(),
		MachineType: opts.MachineType,
		Name:        name,
		ModelURL:    source, // Will be replaced with S3 URL after upload
		Scale:       scale,
		BoundingBox: BoundingBox{}, // Requires GLTF parsing for accurate values
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	_ = info // File info available for size validation if needed

	return model, nil
}

func (g *GLTFImporter) importSTL(ctx context.Context, source string, opts ImportOptions) (*MachineModel, error) {
	// STL files can be loaded directly by Three.js STLLoader on the frontend.
	// For a full pipeline, we'd convert STL→GLTF here using an external tool.
	// For now, we create a model record pointing to the STL file directly.

	_, err := os.Stat(source)
	if err != nil {
		return nil, fmt.Errorf("cannot access file %s: %w", source, err)
	}

	name := opts.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	}

	scale := opts.Scale
	if scale == 0 {
		scale = 1.0
	}

	model := &MachineModel{
		ID:          uuid.New(),
		MachineType: opts.MachineType,
		Name:        name,
		ModelURL:    source,
		Scale:       scale,
		BoundingBox: BoundingBox{},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	return model, nil
}
