package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateModelFile_ValidExtensions(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{"gltf file", "model.gltf"},
		{"glb file", "machine.glb"},
		{"stl file", "part.stl"},
		{"nested path gltf", "/uploads/models/scene.gltf"},
		{"nested path glb", "assets/printer.glb"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			contentType, err := ValidateModelFile(tc.filename)
			assert.NoError(t, err)
			assert.NotEmpty(t, contentType)
		})
	}
}

func TestValidateModelFile_InvalidExtensions(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{"exe file", "malware.exe"},
		{"txt file", "readme.txt"},
		{"obj file", "model.obj"},
		{"png file", "image.png"},
		{"no extension", "filename"},
		{"empty string", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			contentType, err := ValidateModelFile(tc.filename)
			assert.Error(t, err)
			assert.Empty(t, contentType)
			assert.Contains(t, err.Error(), "unsupported file type")
		})
	}
}

func TestAllowedModelExtensions(t *testing.T) {
	expected := map[string]string{
		".gltf": "model/gltf+json",
		".glb":  "model/gltf-binary",
		".stl":  "model/stl",
	}

	assert.Len(t, AllowedModelExtensions, len(expected))

	for ext, contentType := range expected {
		t.Run(ext, func(t *testing.T) {
			got, ok := AllowedModelExtensions[ext]
			assert.True(t, ok, "extension %s should exist", ext)
			assert.Equal(t, contentType, got)
		})
	}
}
