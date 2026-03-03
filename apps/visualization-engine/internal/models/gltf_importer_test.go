package models

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGLTFImporter_Name(t *testing.T) {
	imp := NewGLTFImporter("/tmp")
	assert.Equal(t, "GLTF/STL File Importer", imp.Name())
}

func TestGLTFImporter_SupportedFormats(t *testing.T) {
	imp := NewGLTFImporter("/tmp")
	formats := imp.SupportedFormats()

	assert.ElementsMatch(t, []string{".gltf", ".glb", ".stl"}, formats)
}

func TestGLTFImporter_Import_NonExistentFile(t *testing.T) {
	imp := NewGLTFImporter("/tmp")
	ctx := context.Background()

	_, err := imp.Import(ctx, "/tmp/does_not_exist.glb", ImportOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot access file")
}

func TestGLTFImporter_Import_UnsupportedFormat(t *testing.T) {
	imp := NewGLTFImporter("/tmp")
	ctx := context.Background()

	_, err := imp.Import(ctx, "/tmp/model.obj", ImportOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestGLTFImporter_Import_GLBFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test_model.glb")
	err := os.WriteFile(filePath, []byte("fake-glb-content"), 0644)
	assert.NoError(t, err)

	imp := NewGLTFImporter(tmpDir)
	ctx := context.Background()

	model, err := imp.Import(ctx, filePath, ImportOptions{
		Name:        "Test Machine",
		MachineType: "3d_printer_fdm",
		Scale:       2.0,
	})

	assert.NoError(t, err)
	assert.NotNil(t, model)
	assert.Equal(t, "Test Machine", model.Name)
	assert.Equal(t, "3d_printer_fdm", model.MachineType)
	assert.Equal(t, 2.0, model.Scale)
	assert.Equal(t, filePath, model.ModelURL)
	assert.False(t, model.ID.String() == "00000000-0000-0000-0000-000000000000")
}

func TestGLTFImporter_Import_STLFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "part.stl")
	err := os.WriteFile(filePath, []byte("fake-stl-content"), 0644)
	assert.NoError(t, err)

	imp := NewGLTFImporter(tmpDir)
	ctx := context.Background()

	model, err := imp.Import(ctx, filePath, ImportOptions{
		MachineType: "cnc_3axis",
	})

	assert.NoError(t, err)
	assert.NotNil(t, model)
	assert.Equal(t, "part", model.Name, "should default name to filename without extension")
	assert.Equal(t, 1.0, model.Scale, "should default scale to 1.0")
}

func TestGLTFImporter_Import_GTLFFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "scene.gltf")
	err := os.WriteFile(filePath, []byte(`{"asset":{"version":"2.0"}}`), 0644)
	assert.NoError(t, err)

	imp := NewGLTFImporter(tmpDir)
	ctx := context.Background()

	model, err := imp.Import(ctx, filePath, ImportOptions{
		Name: "My Scene",
	})

	assert.NoError(t, err)
	assert.NotNil(t, model)
	assert.Equal(t, "My Scene", model.Name)
}
