package yantra4d

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleManifest() *Manifest {
	return &Manifest{
		Project: ProjectMeta{
			Name:        "Gridfinity Baseplate",
			Slug:        "gridfinity",
			Version:     "2.1.0",
			Description: map[string]string{"en": "Parametric gridfinity baseplate"},
			Tags:        []string{"storage", "organization"},
			Difficulty:  "beginner",
			Engine:      "openscad",
		},
		Modes: []Mode{
			{ID: "assembled", Label: map[string]string{"en": "Assembled"}},
			{ID: "exploded", Label: map[string]string{"en": "Exploded"}},
		},
		Parameters: []Parameter{
			{
				ID:      "width_units",
				Type:    "int",
				Default: float64(3),
				Label:   map[string]string{"en": "Width (units)"},
				Group:   "dimensions",
			},
			{
				ID:      "depth_units",
				Type:    "int",
				Default: float64(3),
				Label:   map[string]string{"en": "Depth (units)"},
				Group:   "dimensions",
			},
			{
				ID:      "enable_magnets",
				Type:    "bool",
				Default: true,
				Label:   map[string]string{"en": "Magnets"},
				Group:   "features",
			},
		},
		BOM: BOM{
			Hardware: []HardwareItem{
				{
					ID:              "magnet_6x2",
					Label:           map[string]string{"en": "6x2mm Magnets"},
					QuantityFormula: "enable_magnets ? width_units * depth_units * 4 : 0",
					Unit:            "pcs",
					SupplierURL:     "https://example.com/magnets",
				},
			},
		},
		AssemblySteps: []AssemblyStep{
			{
				Step:           1,
				Label:          map[string]string{"en": "Print the baseplate"},
				Notes:          map[string]string{"en": "Use 0.2mm layer height"},
				VisibleParts:   []string{"baseplate"},
				HighlightParts: []string{"baseplate"},
				Camera:         []float64{0, 45, 100},
				CameraTarget:   []float64{0, 0, 0},
				Hardware:       nil,
			},
			{
				Step:           2,
				Label:          map[string]string{"en": "Insert magnets"},
				Notes:          map[string]string{"en": "Press magnets into pockets"},
				VisibleParts:   []string{"baseplate", "magnets"},
				HighlightParts: []string{"magnets"},
				Camera:         []float64{0, 30, 50},
				CameraTarget:   []float64{0, 0, 5},
				Hardware:       []string{"magnet_6x2"},
			},
		},
		Hyperobject: HyperobjectMeta{
			IsHyperobject: true,
			Domain:        "storage",
		},
	}
}

func TestGetManifest_Success(t *testing.T) {
	manifest := sampleManifest()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/projects/gridfinity/manifest", r.URL.Path)
		assert.Equal(t, "Bearer test-jwt", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(manifest)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 10*time.Second)
	result, err := client.GetManifest(context.Background(), "gridfinity", "test-jwt")

	require.NoError(t, err)
	assert.Equal(t, "gridfinity", result.Project.Slug)
	assert.Equal(t, "Gridfinity Baseplate", result.Project.Name)
	assert.Equal(t, "2.1.0", result.Project.Version)
	assert.Equal(t, "openscad", result.Project.Engine)
	assert.Len(t, result.Modes, 2)
	assert.Len(t, result.Parameters, 3)
	assert.Len(t, result.BOM.Hardware, 1)
	assert.Len(t, result.AssemblySteps, 2)
	assert.True(t, result.Hyperobject.IsHyperobject)
}

func TestGetManifest_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"project not found"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 10*time.Second)
	result, err := client.GetManifest(context.Background(), "nonexistent", "test-jwt")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "404")
}

func TestRender_Success(t *testing.T) {
	glbData := []byte("fake-glb-binary-data")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/render", r.URL.Path)
		assert.Equal(t, "gridfinity", r.URL.Query().Get("slug"))
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "Bearer render-jwt", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "glb", body["export_format"])
		assert.NotNil(t, body["params"])

		w.Header().Set("Content-Type", "model/gltf-binary")
		w.Write(glbData)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 10*time.Second)
	data, contentType, err := client.Render(context.Background(), "gridfinity", map[string]interface{}{
		"width_units": 4,
		"depth_units": 3,
	}, "glb", "render-jwt")

	require.NoError(t, err)
	assert.Equal(t, glbData, data)
	assert.Equal(t, "model/gltf-binary", contentType)
}

func TestRender_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"render timeout"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 10*time.Second)
	data, contentType, err := client.Render(context.Background(), "gridfinity", nil, "glb", "jwt")

	assert.Error(t, err)
	assert.Nil(t, data)
	assert.Empty(t, contentType)
	assert.Contains(t, err.Error(), "500")
}

func TestRender_ExplicitContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte("binary"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 10*time.Second)
	_, contentType, err := client.Render(context.Background(), "test", nil, "glb", "jwt")

	require.NoError(t, err)
	assert.Equal(t, "application/octet-stream", contentType)
}

func TestGetMaterial_Success(t *testing.T) {
	mat := MaterialDef{
		Slug:         "pla-basic",
		Name:         "PLA Basic",
		Category:     "filament",
		AMTechnology: "FDM",
		Vendor:       "Generic",
		Thermodynamics: MaterialThermodynamics{
			GlassTransition: 60.0,
			Melting:         180.0,
			YieldStrength:   50.0,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/materials/pla-basic", r.URL.Path)
		assert.Equal(t, "Bearer mat-jwt", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mat)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 10*time.Second)
	result, err := client.GetMaterial(context.Background(), "pla-basic", "mat-jwt")

	require.NoError(t, err)
	assert.Equal(t, "PLA Basic", result.Name)
	assert.Equal(t, "FDM", result.AMTechnology)
	assert.Equal(t, 180.0, result.Thermodynamics.Melting)
}

func TestGetBOM_Success(t *testing.T) {
	bom := BOM{
		Hardware: []HardwareItem{
			{ID: "screw_m3", Label: map[string]string{"en": "M3 Screw"}, QuantityFormula: "4", Unit: "pcs"},
			{ID: "nut_m3", Label: map[string]string{"en": "M3 Nut"}, QuantityFormula: "4", Unit: "pcs"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/projects/gridfinity/bom", r.URL.Path)
		assert.Equal(t, "2", r.URL.Query().Get("width"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(bom)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 10*time.Second)
	result, err := client.GetBOM(context.Background(), "gridfinity", map[string]interface{}{"width": 2}, "jwt")

	require.NoError(t, err)
	assert.Len(t, result.Hardware, 2)
	assert.Equal(t, "screw_m3", result.Hardware[0].ID)
}

func TestGetAssemblySteps_Success(t *testing.T) {
	steps := []AssemblyStep{
		{Step: 1, Label: map[string]string{"en": "Step 1"}, Notes: map[string]string{"en": "First step"}},
		{Step: 2, Label: map[string]string{"en": "Step 2"}, Notes: map[string]string{"en": "Second step"}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/projects/gridfinity/assembly-steps", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(steps)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 10*time.Second)
	result, err := client.GetAssemblySteps(context.Background(), "gridfinity", "jwt")

	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, 1, result[0].Step)
	assert.Equal(t, "Step 1", result[0].Label["en"])
}

func TestGetAssemblySteps_EmptyProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 10*time.Second)
	result, err := client.GetAssemblySteps(context.Background(), "simple", "jwt")

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestNewClient(t *testing.T) {
	client := NewClient("https://yantra4d.example.com", 30*time.Second)
	assert.NotNil(t, client)
	assert.Equal(t, "https://yantra4d.example.com", client.baseURL)
	assert.Equal(t, 30*time.Second, client.httpClient.Timeout)
}

func TestManifestJSONRoundTrip(t *testing.T) {
	original := sampleManifest()

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Manifest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.Project.Slug, decoded.Project.Slug)
	assert.Equal(t, original.Project.Name, decoded.Project.Name)
	assert.Len(t, decoded.Modes, len(original.Modes))
	assert.Len(t, decoded.Parameters, len(original.Parameters))
	assert.Len(t, decoded.BOM.Hardware, len(original.BOM.Hardware))
	assert.Len(t, decoded.AssemblySteps, len(original.AssemblySteps))
	assert.Equal(t, original.Hyperobject.IsHyperobject, decoded.Hyperobject.IsHyperobject)
}
