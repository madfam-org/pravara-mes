package models

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// stubImporter is a minimal ModelImporter for testing the registry.
type stubImporter struct {
	name    string
	formats []string
}

func (s *stubImporter) Name() string              { return s.name }
func (s *stubImporter) SupportedFormats() []string { return s.formats }
func (s *stubImporter) Import(_ context.Context, _ string, _ ImportOptions) (*MachineModel, error) {
	return nil, nil
}

func TestNewImporterRegistry(t *testing.T) {
	reg := NewImporterRegistry()
	assert.NotNil(t, reg)
	assert.Empty(t, reg.List())
}

func TestImporterRegistry_RegisterAndGet(t *testing.T) {
	reg := NewImporterRegistry()
	imp := &stubImporter{name: "test-importer", formats: []string{".stl"}}

	reg.Register("test", imp)

	got, err := reg.Get("test")
	assert.NoError(t, err)
	assert.Equal(t, "test-importer", got.Name())
}

func TestImporterRegistry_GetUnknown(t *testing.T) {
	reg := NewImporterRegistry()

	got, err := reg.Get("nonexistent")
	assert.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "not found")
}

func TestImporterRegistry_List(t *testing.T) {
	reg := NewImporterRegistry()
	imp1 := &stubImporter{name: "importer-a", formats: []string{".gltf"}}
	imp2 := &stubImporter{name: "importer-b", formats: []string{".stl"}}

	reg.Register("a", imp1)
	reg.Register("b", imp2)

	list := reg.List()
	assert.Len(t, list, 2)
	assert.Equal(t, imp1, list["a"])
	assert.Equal(t, imp2, list["b"])
}

func TestImporterRegistry_RegisterOverwrite(t *testing.T) {
	reg := NewImporterRegistry()
	imp1 := &stubImporter{name: "original"}
	imp2 := &stubImporter{name: "replacement"}

	reg.Register("key", imp1)
	reg.Register("key", imp2)

	got, err := reg.Get("key")
	assert.NoError(t, err)
	assert.Equal(t, "replacement", got.Name())
}
