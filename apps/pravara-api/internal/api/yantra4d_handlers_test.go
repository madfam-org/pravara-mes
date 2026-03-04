package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newTestYantra4DHandler() (*Yantra4DHandler, *httptest.Server, *httptest.Server) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	// Mock viz-engine server
	vizEngine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models/import/yantra4d" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{
				"model_url": "https://s3.example.com/models/test.glb",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	// Mock Yantra4D server
	yantra4d := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/projects/gridfinity/manifest" {
			manifest := services.Yantra4DManifest{}
			manifest.Project.Name = "Gridfinity Baseplate"
			manifest.Project.Slug = "gridfinity"
			manifest.Project.Version = "2.1.0"
			manifest.Project.Description = map[string]string{"en": "Parametric gridfinity baseplate"}
			manifest.Project.Tags = []string{"storage"}
			manifest.Project.Engine = "openscad"
			manifest.Modes = []struct {
				ID    string            `json:"id"`
				Label map[string]string `json:"label"`
			}{
				{ID: "assembled", Label: map[string]string{"en": "Assembled"}},
			}
			manifest.Parameters = []struct {
				ID      string      `json:"id"`
				Type    string      `json:"type"`
				Default interface{} `json:"default"`
				Label   map[string]string `json:"label"`
				Group   string      `json:"group"`
			}{
				{ID: "width_units", Type: "int", Default: float64(3), Label: map[string]string{"en": "Width"}, Group: "dimensions"},
			}
			manifest.BOM.Hardware = []struct {
				ID              string            `json:"id"`
				Label           map[string]string `json:"label"`
				QuantityFormula string            `json:"quantity_formula"`
				Unit            string            `json:"unit"`
				SupplierURL     string            `json:"supplier_url"`
			}{
				{ID: "magnet_6x2", Label: map[string]string{"en": "Magnets"}, QuantityFormula: "4", Unit: "pcs"},
			}
			manifest.Hyperobject.IsHyperobject = true
			manifest.Hyperobject.Domain = "storage"

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(manifest)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	handler := NewYantra4DHandler(nil, vizEngine.URL, yantra4d.URL, log)
	return handler, vizEngine, yantra4d
}

func TestPreviewImport_Success(t *testing.T) {
	handler, vizEngine, yantra4d := newTestYantra4DHandler()
	defer vizEngine.Close()
	defer yantra4d.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/v1/import/yantra4d/preview?slug=gridfinity", nil)
	c.Request.Header.Set("Authorization", "Bearer test-jwt-token")

	handler.PreviewImport(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	// Check preview section
	preview, ok := resp["preview"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Y4D-gridfinity-assembled", preview["sku"])
	assert.Equal(t, "Gridfinity Baseplate", preview["name"])
	assert.Equal(t, "2.1.0", preview["version"])
	assert.Equal(t, "3d_print", preview["category"])
	assert.Equal(t, float64(1), preview["bom_count"])

	// Check manifest is returned
	_, hasManifest := resp["manifest"]
	assert.True(t, hasManifest)
}

func TestPreviewImport_MissingSlug(t *testing.T) {
	handler, vizEngine, yantra4d := newTestYantra4DHandler()
	defer vizEngine.Close()
	defer yantra4d.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/v1/import/yantra4d/preview", nil)
	c.Request.Header.Set("Authorization", "Bearer test-jwt")

	handler.PreviewImport(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "validation_error", resp["error"])
}

func TestPreviewImport_MissingToken(t *testing.T) {
	handler, vizEngine, yantra4d := newTestYantra4DHandler()
	defer vizEngine.Close()
	defer yantra4d.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/v1/import/yantra4d/preview?slug=gridfinity", nil)
	// No Authorization header

	handler.PreviewImport(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPreviewImport_Yantra4DNotFound(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	yantra4d := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"project not found"}`))
	}))
	defer yantra4d.Close()

	handler := NewYantra4DHandler(nil, "http://unused", yantra4d.URL, log)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/v1/import/yantra4d/preview?slug=nonexistent", nil)
	c.Request.Header.Set("Authorization", "Bearer test-jwt")

	handler.PreviewImport(c)

	assert.Equal(t, http.StatusBadGateway, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "yantra4d_error", resp["error"])
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{"valid token", "Bearer my-jwt-token", "my-jwt-token"},
		{"empty header", "", ""},
		{"no bearer prefix", "my-jwt-token", ""},
		{"bearer lowercase", "bearer my-jwt-token", ""},
		{"just Bearer", "Bearer ", ""},
		{"Bearer with long token", "Bearer eyJhbGciOiJSUzI1NiJ9.payload.sig", "eyJhbGciOiJSUzI1NiJ9.payload.sig"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				c.Request.Header.Set("Authorization", tc.header)
			}

			result := extractBearerToken(c)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestInferCategoryFromEngine(t *testing.T) {
	tests := []struct {
		engine   string
		expected string
	}{
		{"openscad", "3d_print"},
		{"scad", "3d_print"},
		{"cadquery", "cnc_part"},
		{"cq", "cnc_part"},
		{"freecad", "cnc_part"},
		{"unknown", "3d_print"},
		{"", "3d_print"},
	}

	for _, tc := range tests {
		t.Run(tc.engine, func(t *testing.T) {
			result := inferCategoryFromEngine(tc.engine)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestInferCategoryFromEngine_MatchesServiceVersion(t *testing.T) {
	// Verify the handler's local inferCategoryFromEngine matches
	// the exported services.InferCategoryFromEngine for consistency
	engines := []string{"openscad", "scad", "cadquery", "cq", "freecad", "unknown", ""}
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			handlerResult := inferCategoryFromEngine(engine)
			serviceResult := services.InferCategoryFromEngine(engine)
			assert.Equal(t, serviceResult, handlerResult,
				"handler and service should produce the same category for engine %q", engine)
		})
	}
}
