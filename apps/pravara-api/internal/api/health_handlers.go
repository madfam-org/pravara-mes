package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
)

var startTime = time.Now()

// HealthHandler handles health check endpoints.
type HealthHandler struct {
	db  *db.DB
	log *logrus.Logger
}

// NewHealthHandler creates a new health handler.
func NewHealthHandler(db *db.DB, log *logrus.Logger) *HealthHandler {
	return &HealthHandler{
		db:  db,
		log: log,
	}
}

// HealthResponse represents the full health check response.
type HealthResponse struct {
	Status     string                   `json:"status"`
	Service    string                   `json:"service"`
	Timestamp  string                   `json:"timestamp"`
	Uptime     string                   `json:"uptime"`
	Version    string                   `json:"version"`
	Components map[string]ComponentHealth `json:"components"`
}

// ComponentHealth represents the health of a single component.
type ComponentHealth struct {
	Status  string         `json:"status"`
	Details map[string]any `json:"details,omitempty"`
}

// Health performs a comprehensive health check.
// @Summary Comprehensive health check
// @Description Returns detailed health status including database connection and runtime metrics
// @Tags health
// @Produce json
// @Success 200 {object} HealthResponse "System healthy"
// @Success 503 {object} HealthResponse "System degraded"
// @Router /health [get]
func (h *HealthHandler) Health(c *gin.Context) {
	response := HealthResponse{
		Status:     "ok",
		Service:    "pravara-mes",
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Uptime:     time.Since(startTime).String(),
		Version:    "0.1.0",
		Components: make(map[string]ComponentHealth),
	}

	// Check database
	dbHealth := ComponentHealth{Status: "healthy"}
	if h.db == nil {
		dbHealth.Status = "unhealthy"
		dbHealth.Details = map[string]any{"error": "database not configured"}
		response.Status = "degraded"
	} else if err := h.db.Health(); err != nil {
		dbHealth.Status = "unhealthy"
		dbHealth.Details = map[string]any{"error": err.Error()}
		response.Status = "degraded"
	} else {
		stats := h.db.Stats()
		dbHealth.Details = map[string]any{
			"open_connections": stats.OpenConnections,
			"in_use":           stats.InUse,
			"idle":             stats.Idle,
		}
	}
	response.Components["database"] = dbHealth

	// Check runtime
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	response.Components["runtime"] = ComponentHealth{
		Status: "healthy",
		Details: map[string]any{
			"goroutines":   runtime.NumGoroutine(),
			"heap_alloc":   memStats.HeapAlloc,
			"heap_sys":     memStats.HeapSys,
			"gc_cycles":    memStats.NumGC,
		},
	}

	statusCode := http.StatusOK
	if response.Status == "degraded" {
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, response)
}

// Liveness returns a simple liveness probe response.
// This is used by Kubernetes to determine if the container should be restarted.
// @Summary Kubernetes liveness probe
// @Description Returns alive status for container health check
// @Tags health
// @Produce json
// @Success 200 {object} map[string]string "alive"
// @Router /health/live [get]
func (h *HealthHandler) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "alive",
	})
}

// Readiness returns a readiness probe response.
// This is used by Kubernetes to determine if the container can receive traffic.
// @Summary Kubernetes readiness probe
// @Description Returns ready status when database connection is healthy
// @Tags health
// @Produce json
// @Success 200 {object} map[string]string "ready"
// @Failure 503 {object} map[string]string "not ready"
// @Router /health/ready [get]
func (h *HealthHandler) Readiness(c *gin.Context) {
	// Check if we can connect to the database
	if h.db == nil {
		h.log.Warn("Readiness check failed: database not configured")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"reason": "database_not_configured",
		})
		return
	}
	if err := h.db.Health(); err != nil {
		h.log.WithError(err).Warn("Readiness check failed: database unhealthy")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"reason": "database_unavailable",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
	})
}
