package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
)

var vizEngineURL = getVizEngineURL()

func getVizEngineURL() string {
	url := os.Getenv("VIZENGINE_URL")
	if url == "" {
		return "http://localhost:4502"
	}
	return url
}

// handleGetActiveLayout returns the most recently updated layout for the tenant.
func handleGetActiveLayout(database *db.DB, log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, exists := c.Get("tenant_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id not found"})
			return
		}

		query := `
			SELECT id, tenant_id, name, description, floor_plan,
			       machine_positions, camera_presets, lighting_config,
			       grid_settings, zones, waypoints, created_at, updated_at
			FROM factory_layouts
			WHERE tenant_id = $1
			ORDER BY updated_at DESC
			LIMIT 1
		`

		var layout struct {
			ID               string          `json:"id"`
			TenantID         string          `json:"tenant_id"`
			Name             string          `json:"name"`
			Description      *string         `json:"description"`
			FloorPlan        json.RawMessage `json:"floor_plan"`
			MachinePositions json.RawMessage `json:"machine_positions"`
			CameraPresets    json.RawMessage `json:"camera_presets"`
			LightingConfig   json.RawMessage `json:"lighting_config"`
			GridSettings     json.RawMessage `json:"grid_settings"`
			Zones            json.RawMessage `json:"zones"`
			Waypoints        json.RawMessage `json:"waypoints"`
			CreatedAt        string          `json:"created_at"`
			UpdatedAt        string          `json:"updated_at"`
		}

		err := database.DB.QueryRow(query, tenantID).Scan(
			&layout.ID, &layout.TenantID, &layout.Name, &layout.Description,
			&layout.FloorPlan, &layout.MachinePositions, &layout.CameraPresets,
			&layout.LightingConfig, &layout.GridSettings, &layout.Zones,
			&layout.Waypoints, &layout.CreatedAt, &layout.UpdatedAt,
		)
		if err != nil {
			log.WithError(err).Debug("No active layout found")
			c.JSON(http.StatusNotFound, gin.H{"error": "no layout found"})
			return
		}

		c.JSON(http.StatusOK, layout)
	}
}

// Proxy helpers for viz-engine routes

func handleProxyLayouts(log *logrus.Logger) gin.HandlerFunc {
	return proxyToVizEngine("/v1/layouts", log)
}

func handleProxyLayout(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		proxyToVizEngine(fmt.Sprintf("/v1/layouts/%s", c.Param("id")), log)(c)
	}
}

func handleProxyLayoutUpdate(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		proxyToVizEngineMutate("PUT", fmt.Sprintf("/v1/layouts/%s", c.Param("id")), log)(c)
	}
}

func handleProxyModels(log *logrus.Logger) gin.HandlerFunc {
	return proxyToVizEngine("/v1/models", log)
}

func handleProxyModelUpload(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Forward multipart form to viz-engine
		url := vizEngineURL + "/v1/models/upload"

		req, err := http.NewRequestWithContext(c.Request.Context(), "POST", url, c.Request.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create proxy request"})
			return
		}
		req.Header.Set("Content-Type", c.GetHeader("Content-Type"))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.WithError(err).Error("Failed to proxy model upload to viz-engine")
			c.JSON(http.StatusBadGateway, gin.H{"error": "viz-engine unavailable"})
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
}

func proxyToVizEngine(path string, log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		url := vizEngineURL + path

		resp, err := http.Get(url)
		if err != nil {
			log.WithError(err).WithField("path", path).Error("Failed to proxy to viz-engine")
			c.JSON(http.StatusBadGateway, gin.H{"error": "viz-engine unavailable"})
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
}

func proxyToVizEngineMutate(method, path string, log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		url := vizEngineURL + path

		req, err := http.NewRequestWithContext(c.Request.Context(), method, url, c.Request.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create proxy request"})
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.WithError(err).WithField("path", path).Error("Failed to proxy to viz-engine")
			c.JSON(http.StatusBadGateway, gin.H{"error": "viz-engine unavailable"})
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
}
