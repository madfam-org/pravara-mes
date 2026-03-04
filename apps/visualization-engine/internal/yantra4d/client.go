// Package yantra4d provides an HTTP client for the Yantra4D parametric design platform.
package yantra4d

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client communicates with the Yantra4D Flask API.
// It uses the caller's JWT directly since both systems share the Janua issuer.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a Yantra4D API client.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// --- Manifest structs mirroring Yantra4D's project.json ---

// Manifest is the top-level project manifest from Yantra4D.
type Manifest struct {
	Project           ProjectMeta        `json:"project"`
	Modes             []Mode             `json:"modes"`
	Parts             []Part             `json:"parts"`
	Parameters        []Parameter        `json:"parameters"`
	Presets           []Preset           `json:"presets"`
	BOM               BOM                `json:"bom"`
	AssemblySteps     []AssemblyStep     `json:"assembly_steps"`
	EstimateConstants EstimateConstants  `json:"estimate_constants"`
	Hyperobject       HyperobjectMeta    `json:"hyperobject"`
	Verification      VerificationStages `json:"verification"`
	Materials         []MaterialDef      `json:"materials"`
	ExportFormats     []string           `json:"export_formats"`
	CameraViews       []CameraView       `json:"camera_views"`
}

// ProjectMeta holds project-level metadata.
type ProjectMeta struct {
	Name        string            `json:"name"`
	Slug        string            `json:"slug"`
	Version     string            `json:"version"`
	Description map[string]string `json:"description"`
	Tags        []string          `json:"tags"`
	Difficulty  string            `json:"difficulty"`
	Engine      string            `json:"engine"`
}

// HyperobjectMeta describes the hyperobject classification.
type HyperobjectMeta struct {
	IsHyperobject     bool     `json:"is_hyperobject"`
	Domain            string   `json:"domain"`
	CDGInterfaces     []string `json:"cdg_interfaces"`
	MaterialAwareness bool     `json:"material_awareness"`
	SocietalBenefit   string   `json:"societal_benefit"`
	CommonsLicense    string   `json:"commons_license"`
}

// Mode represents a rendering/export mode (e.g. "assembled", "exploded").
type Mode struct {
	ID    string            `json:"id"`
	Label map[string]string `json:"label"`
}

// Part represents a model part.
type Part struct {
	ID    string            `json:"id"`
	Label map[string]string `json:"label"`
	Group string            `json:"group"`
}

// Parameter is a configurable hyperobject parameter.
type Parameter struct {
	ID             string            `json:"id"`
	Type           string            `json:"type"`
	Default        interface{}       `json:"default"`
	Min            *float64          `json:"min,omitempty"`
	Max            *float64          `json:"max,omitempty"`
	Step           *float64          `json:"step,omitempty"`
	Options        []string          `json:"options,omitempty"`
	Label          map[string]string `json:"label"`
	Group          string            `json:"group"`
	VisibleInModes []string          `json:"visible_in_modes"`
}

// Preset is a named parameter configuration.
type Preset struct {
	ID     string                 `json:"id"`
	Label  map[string]string      `json:"label"`
	Values map[string]interface{} `json:"values"`
}

// BOM is the bill of materials section.
type BOM struct {
	Hardware []HardwareItem `json:"hardware"`
}

// HardwareItem is a single BOM line with a quantity formula.
type HardwareItem struct {
	ID              string            `json:"id"`
	Label           map[string]string `json:"label"`
	QuantityFormula string            `json:"quantity_formula"`
	Unit            string            `json:"unit"`
	SupplierURL     string            `json:"supplier_url"`
}

// AssemblyStep is an ordered assembly instruction.
type AssemblyStep struct {
	Step           int               `json:"step"`
	Label          map[string]string `json:"label"`
	Notes          map[string]string `json:"notes"`
	VisibleParts   []string          `json:"visible_parts"`
	HighlightParts []string          `json:"highlight_parts"`
	Camera         []float64         `json:"camera"`
	CameraTarget   []float64         `json:"camera_target"`
	Hardware       []string          `json:"hardware"`
}

// MaterialDef describes a compatible material.
type MaterialDef struct {
	Slug             string                `json:"slug"`
	Name             string                `json:"name"`
	Category         string                `json:"category"`
	AMTechnology     string                `json:"am_technology"`
	Vendor           string                `json:"vendor"`
	AMCompensations  AMCompensations       `json:"am_compensations"`
	Thermodynamics   MaterialThermodynamics `json:"thermodynamics"`
}

// AMCompensations holds additive manufacturing compensation values.
type AMCompensations struct {
	Shrinkage   float64 `json:"shrinkage"`
	Clearances  float64 `json:"clearances"`
	MinFeatures float64 `json:"min_features"`
}

// MaterialThermodynamics holds thermal material properties.
type MaterialThermodynamics struct {
	GlassTransition float64 `json:"glass_transition"`
	Melting         float64 `json:"melting"`
	YieldStrength   float64 `json:"yield_strength"`
}

// VerificationStages holds geometry and printability checks.
type VerificationStages struct {
	Geometry     GeometryChecks     `json:"geometry"`
	Printability PrintabilityChecks `json:"printability"`
}

// GeometryChecks are pre-fabrication geometry validations.
type GeometryChecks struct {
	Watertight bool     `json:"watertight"`
	BodyCount  int      `json:"body_count"`
	Dimensions [3]float64 `json:"dimensions"`
	FacetCount int      `json:"facet_count"`
}

// PrintabilityChecks are AM-specific validations.
type PrintabilityChecks struct {
	ThinWall       float64 `json:"thin_wall"`
	Overhang       float64 `json:"overhang"`
	MinFeatureSize float64 `json:"min_feature_size"`
}

// EstimateConstants holds time estimation coefficients.
type EstimateConstants struct {
	BaseTime       float64 `json:"base_time"`
	PerUnit        float64 `json:"per_unit"`
	PerPart        float64 `json:"per_part"`
	FNFactor       float64 `json:"fn_factor"`
	WasmMultiplier float64 `json:"wasm_multiplier"`
}

// CameraView is a named camera preset.
type CameraView struct {
	ID     string    `json:"id"`
	Label  string    `json:"label"`
	Pos    []float64 `json:"pos"`
	Target []float64 `json:"target"`
}

// --- API methods ---

// GetManifest fetches the full project manifest.
func (c *Client) GetManifest(ctx context.Context, slug, jwt string) (*Manifest, error) {
	url := fmt.Sprintf("%s/api/projects/%s/manifest", c.baseURL, slug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET manifest returned %d: %s", resp.StatusCode, string(body))
	}

	var m Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &m, nil
}

// Render requests a GLB export from Yantra4D.
// Returns the raw binary data and content type.
func (c *Client) Render(ctx context.Context, slug string, params map[string]interface{}, format, jwt string) ([]byte, string, error) {
	body := map[string]interface{}{
		"params":        params,
		"export_format": format,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("marshal render request: %w", err)
	}

	url := fmt.Sprintf("%s/api/render", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")

	// Add slug as query param
	q := req.URL.Query()
	q.Set("slug", slug)
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("POST render: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("POST render returned %d: %s", resp.StatusCode, string(errBody))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read render response: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "model/gltf-binary"
	}

	return data, contentType, nil
}

// GetMaterial fetches a material definition by slug.
func (c *Client) GetMaterial(ctx context.Context, slug, jwt string) (*MaterialDef, error) {
	url := fmt.Sprintf("%s/api/materials/%s", c.baseURL, slug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET material: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET material returned %d: %s", resp.StatusCode, string(body))
	}

	var m MaterialDef
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode material: %w", err)
	}
	return &m, nil
}

// GetBOM fetches the bill of materials for a project with given parameters.
func (c *Client) GetBOM(ctx context.Context, slug string, params map[string]interface{}, jwt string) (*BOM, error) {
	url := fmt.Sprintf("%s/api/projects/%s/bom", c.baseURL, slug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)

	// Add params as query string
	if len(params) > 0 {
		q := req.URL.Query()
		for k, v := range params {
			q.Set(k, fmt.Sprintf("%v", v))
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET BOM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET BOM returned %d: %s", resp.StatusCode, string(body))
	}

	var b BOM
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		return nil, fmt.Errorf("decode BOM: %w", err)
	}
	return &b, nil
}

// GetAssemblySteps fetches assembly instructions for a project.
func (c *Client) GetAssemblySteps(ctx context.Context, slug, jwt string) ([]AssemblyStep, error) {
	url := fmt.Sprintf("%s/api/projects/%s/assembly-steps", c.baseURL, slug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET assembly steps: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET assembly steps returned %d: %s", resp.StatusCode, string(body))
	}

	var steps []AssemblyStep
	if err := json.NewDecoder(resp.Body).Decode(&steps); err != nil {
		return nil, fmt.Errorf("decode assembly steps: %w", err)
	}
	return steps, nil
}
