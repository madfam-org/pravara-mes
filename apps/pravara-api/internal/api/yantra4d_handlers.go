package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

// Yantra4DHandler handles hyperobject import endpoints.
type Yantra4DHandler struct {
	mapper       *services.HyperobjectMapper
	vizEngineURL string
	yantra4dURL  string
	log          *logrus.Logger
}

// NewYantra4DHandler creates a new Yantra4D handler.
func NewYantra4DHandler(mapper *services.HyperobjectMapper, vizEngineURL, yantra4dURL string, log *logrus.Logger) *Yantra4DHandler {
	return &Yantra4DHandler{
		mapper:       mapper,
		vizEngineURL: vizEngineURL,
		yantra4dURL:  yantra4dURL,
		log:          log,
	}
}

// ImportHyperobjectRequest is the JSON body for POST /v1/import/yantra4d.
type ImportHyperobjectRequest struct {
	Slug        string                 `json:"slug" binding:"required"`
	Mode        string                 `json:"mode"`
	Parameters  map[string]interface{} `json:"parameters"`
	MachineType string                 `json:"machine_type"`
}

// ImportHyperobject performs a full Yantra4D import:
// 1. Calls viz-engine to render + upload GLB
// 2. Fetches manifest from Yantra4D
// 3. Maps manifest to MES domain objects
func (h *Yantra4DHandler) ImportHyperobject(c *gin.Context) {
	var req ImportHyperobjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	tenantID, ok := middleware.GetTenantID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "Tenant context not found",
		})
		return
	}

	jwt := extractBearerToken(c)
	if jwt == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "Bearer token required",
		})
		return
	}

	h.log.WithFields(logrus.Fields{
		"slug":      req.Slug,
		"tenant_id": tenantID,
	}).Info("Starting Yantra4D import")

	// Step 1: Call viz-engine to render GLB and get model URL
	modelURL, err := h.renderViaVizEngine(c, req.Slug, req.Parameters, req.MachineType, jwt)
	if err != nil {
		h.log.WithError(err).Error("Failed to render model via viz-engine")
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "viz_engine_error",
			"message": fmt.Sprintf("Failed to render 3D model: %v", err),
		})
		return
	}

	// Step 2: Fetch manifest from Yantra4D for domain mapping
	manifest, err := h.fetchManifest(c, req.Slug, jwt)
	if err != nil {
		h.log.WithError(err).Error("Failed to fetch Yantra4D manifest")
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "yantra4d_error",
			"message": fmt.Sprintf("Failed to fetch manifest: %v", err),
		})
		return
	}

	// Step 3: Map to MES domain objects
	result, err := h.mapper.Import(c.Request.Context(), services.HyperobjectImportRequest{
		TenantID:    uuid.MustParse(tenantID),
		Manifest:    manifest,
		Params:      req.Parameters,
		Mode:        req.Mode,
		GLBModelURL: modelURL,
	})
	if err != nil {
		h.log.WithError(err).Error("Failed to map hyperobject to MES domain")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "mapping_error",
			"message": fmt.Sprintf("Failed to create domain objects: %v", err),
		})
		return
	}

	h.log.WithFields(logrus.Fields{
		"product_id": result.ProductDefinition.ID,
		"bom_count":  len(result.BOMItems),
	}).Info("Yantra4D import completed")

	c.JSON(http.StatusCreated, gin.H{
		"product_definition": result.ProductDefinition,
		"bom_items":          result.BOMItems,
		"work_instruction":   result.WorkInstruction,
		"model_url":          modelURL,
	})
}

// PreviewImport fetches the manifest and returns a preview of what would be created.
func (h *Yantra4DHandler) PreviewImport(c *gin.Context) {
	slug := c.Query("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "slug query parameter is required",
		})
		return
	}

	jwt := extractBearerToken(c)
	if jwt == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "Bearer token required",
		})
		return
	}

	manifest, err := h.fetchManifest(c, slug, jwt)
	if err != nil {
		h.log.WithError(err).Error("Failed to fetch manifest for preview")
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "yantra4d_error",
			"message": fmt.Sprintf("Failed to fetch manifest: %v", err),
		})
		return
	}

	mode := ""
	if len(manifest.Modes) > 0 {
		mode = manifest.Modes[0].ID
	}

	c.JSON(http.StatusOK, gin.H{
		"manifest": manifest,
		"preview": gin.H{
			"sku":         fmt.Sprintf("Y4D-%s-%s", manifest.Project.Slug, mode),
			"name":        manifest.Project.Name,
			"version":     manifest.Project.Version,
			"category":    inferCategoryFromEngine(manifest.Project.Engine),
			"description": manifest.Project.Description["en"],
			"bom_count":   len(manifest.BOM.Hardware),
			"step_count":  len(manifest.AssemblySteps),
			"modes":       manifest.Modes,
			"parameters":  manifest.Parameters,
		},
	})
}

// renderViaVizEngine calls the viz-engine to render a GLB and returns the model URL.
func (h *Yantra4DHandler) renderViaVizEngine(c *gin.Context, slug string, params map[string]interface{}, machineType, jwt string) (string, error) {
	bodyJSON, err := json.Marshal(map[string]interface{}{
		"slug":         slug,
		"params":       params,
		"machine_type": machineType,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/models/import/yantra4d", h.vizEngineURL)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("viz-engine request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("viz-engine returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ModelURL string `json:"model_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode viz-engine response: %w", err)
	}

	return result.ModelURL, nil
}

// fetchManifest calls Yantra4D directly to get the project manifest.
func (h *Yantra4DHandler) fetchManifest(c *gin.Context, slug, jwt string) (*services.Yantra4DManifest, error) {
	url := fmt.Sprintf("%s/api/projects/%s/manifest", h.yantra4dURL, slug)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("manifest request returned %d: %s", resp.StatusCode, string(body))
	}

	var manifest services.Yantra4DManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &manifest, nil
}

// extractBearerToken extracts the JWT from the Authorization header.
func extractBearerToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		return auth[7:]
	}
	return ""
}

// inferCategoryFromEngine maps Yantra4D engine to a PravaraMES product category.
func inferCategoryFromEngine(engine string) string {
	switch engine {
	case "openscad", "scad":
		return "3d_print"
	case "cadquery", "cq":
		return "cnc_part"
	case "freecad":
		return "cnc_part"
	default:
		return "3d_print"
	}
}
