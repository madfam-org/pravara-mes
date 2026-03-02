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
func (h *HealthHandler) Health(c *gin.Context) {
	response := HealthResponse{
		Status:     "healthy",
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Uptime:     time.Since(startTime).String(),
		Version:    "0.1.0",
		Components: make(map[string]ComponentHealth),
	}

	// Check database
	dbHealth := ComponentHealth{Status: "healthy"}
	if err := h.db.Health(); err != nil {
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
func (h *HealthHandler) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "alive",
	})
}

// Readiness returns a readiness probe response.
// This is used by Kubernetes to determine if the container can receive traffic.
func (h *HealthHandler) Readiness(c *gin.Context) {
	// Check if we can connect to the database
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
