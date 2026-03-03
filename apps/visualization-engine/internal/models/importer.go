package models

import (
	"context"
	"fmt"
)

// ImportOptions configures a model import operation.
type ImportOptions struct {
	Name        string            `json:"name"`
	MachineType string            `json:"machine_type"`
	Scale       float64           `json:"scale"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ModelImporter defines the interface for importing 3D models from various sources.
type ModelImporter interface {
	// Name returns the importer's display name.
	Name() string

	// SupportedFormats returns the file extensions this importer handles.
	SupportedFormats() []string

	// Import fetches/converts a model from the given source and returns a MachineModel record.
	Import(ctx context.Context, source string, opts ImportOptions) (*MachineModel, error)
}

// ImporterRegistry manages available model importers.
type ImporterRegistry struct {
	importers map[string]ModelImporter
}

// NewImporterRegistry creates a new importer registry.
func NewImporterRegistry() *ImporterRegistry {
	return &ImporterRegistry{
		importers: make(map[string]ModelImporter),
	}
}

// Register adds an importer to the registry.
func (r *ImporterRegistry) Register(id string, importer ModelImporter) {
	r.importers[id] = importer
}

// Get returns an importer by ID.
func (r *ImporterRegistry) Get(id string) (ModelImporter, error) {
	imp, ok := r.importers[id]
	if !ok {
		return nil, fmt.Errorf("importer %q not found", id)
	}
	return imp, nil
}

// List returns all registered importers.
func (r *ImporterRegistry) List() map[string]ModelImporter {
	return r.importers
}
