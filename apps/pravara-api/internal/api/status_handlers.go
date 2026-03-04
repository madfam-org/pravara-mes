package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// StatusHandler handles public and authenticated status endpoints.
type StatusHandler struct {
	db  *sql.DB
	log *logrus.Logger
}

// NewStatusHandler creates a new status handler.
func NewStatusHandler(db *sql.DB, log *logrus.Logger) *StatusHandler {
	return &StatusHandler{db: db, log: log}
}

// ComponentStatus represents the health status of a component.
type ComponentStatus struct {
	Name   string         `json:"name"`
	Status string         `json:"status"`
	Uptime *float64       `json:"uptime_percent,omitempty"`
}

// StatusResponse is the public status endpoint response.
type StatusResponse struct {
	Status     string            `json:"status"`
	Components []ComponentStatus `json:"components"`
	Uptime     UptimeStats       `json:"uptime"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// UptimeStats contains rolling uptime percentages.
type UptimeStats struct {
	Last24h float64 `json:"last_24h"`
	Last7d  float64 `json:"last_7d"`
	Last30d float64 `json:"last_30d"`
}

// Status returns the composite system health (public, no auth).
// GET /status
func (h *StatusHandler) Status(c *gin.Context) {
	components, err := h.getLatestComponentStatus(c)
	if err != nil {
		h.log.WithError(err).Error("Failed to get component status")
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "unknown",
			"message": "Unable to determine system status",
		})
		return
	}

	// Determine overall status
	overallStatus := "operational"
	for _, comp := range components {
		if comp.Status == "outage" {
			overallStatus = "outage"
			break
		}
		if comp.Status == "degraded" {
			overallStatus = "degraded"
		}
	}

	// Compute uptime stats
	uptime := h.computeUptime(c)

	c.JSON(http.StatusOK, StatusResponse{
		Status:     overallStatus,
		Components: components,
		Uptime:     uptime,
		UpdatedAt:  time.Now().UTC(),
	})
}

// StatusHistory returns 90-day uptime history (public, no auth).
// GET /status/history
func (h *StatusHandler) StatusHistory(c *gin.Context) {
	rows, err := h.db.QueryContext(c.Request.Context(),
		`SELECT
			date_trunc('day', checked_at) as day,
			component,
			COUNT(*) as total_checks,
			COUNT(*) FILTER (WHERE status = 'operational') as operational_checks
		FROM health_snapshots
		WHERE checked_at >= NOW() - INTERVAL '90 days'
		GROUP BY day, component
		ORDER BY day DESC, component`,
	)
	if err != nil {
		h.log.WithError(err).Error("Failed to get status history")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}
	defer rows.Close()

	type DayHistory struct {
		Date      time.Time `json:"date"`
		Component string    `json:"component"`
		Uptime    float64   `json:"uptime_percent"`
	}

	var history []DayHistory
	for rows.Next() {
		var d DayHistory
		var total, operational int
		if err := rows.Scan(&d.Date, &d.Component, &total, &operational); err != nil {
			h.log.WithError(err).Error("Failed to scan status history")
			continue
		}
		if total > 0 {
			d.Uptime = float64(operational) / float64(total) * 100
		}
		history = append(history, d)
	}

	c.JSON(http.StatusOK, gin.H{"history": history})
}

// DetailedStatus returns per-tenant component health (authenticated).
// GET /v1/feeds/status/detailed
func (h *StatusHandler) DetailedStatus(c *gin.Context) {
	components, err := h.getLatestComponentStatus(c)
	if err != nil {
		h.log.WithError(err).Error("Failed to get detailed status")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	// Get additional tenant-specific stats
	var machineCount, activeMachines int
	h.db.QueryRowContext(c.Request.Context(),
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE status IN ('running', 'online', 'idle'))
		 FROM machines`,
	).Scan(&machineCount, &activeMachines)

	var pendingTasks int
	h.db.QueryRowContext(c.Request.Context(),
		`SELECT COUNT(*) FROM tasks WHERE status NOT IN ('completed', 'blocked')`,
	).Scan(&pendingTasks)

	c.JSON(http.StatusOK, gin.H{
		"components":      components,
		"machine_count":   machineCount,
		"active_machines": activeMachines,
		"pending_tasks":   pendingTasks,
		"updated_at":      time.Now().UTC(),
	})
}

// Incidents returns recent health state transitions (authenticated).
// GET /v1/feeds/status/incidents
func (h *StatusHandler) Incidents(c *gin.Context) {
	limit := queryInt(c, "limit", 50)

	rows, err := h.db.QueryContext(c.Request.Context(),
		`SELECT event_type, payload, created_at
		FROM event_outbox
		WHERE event_type LIKE 'system.health.%'
		ORDER BY created_at DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		h.log.WithError(err).Error("Failed to get incidents")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}
	defer rows.Close()

	type Incident struct {
		Type       string          `json:"type"`
		Details    json.RawMessage `json:"details"`
		OccurredAt time.Time       `json:"occurred_at"`
	}

	var incidents []Incident
	for rows.Next() {
		var i Incident
		if err := rows.Scan(&i.Type, &i.Details, &i.OccurredAt); err != nil {
			continue
		}
		incidents = append(incidents, i)
	}

	c.JSON(http.StatusOK, gin.H{"incidents": incidents})
}

func (h *StatusHandler) getLatestComponentStatus(c *gin.Context) ([]ComponentStatus, error) {
	rows, err := h.db.QueryContext(c.Request.Context(),
		`SELECT DISTINCT ON (component) component, status
		FROM health_snapshots
		ORDER BY component, checked_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var components []ComponentStatus
	for rows.Next() {
		var cs ComponentStatus
		if err := rows.Scan(&cs.Name, &cs.Status); err != nil {
			return nil, err
		}
		components = append(components, cs)
	}
	return components, rows.Err()
}

func (h *StatusHandler) computeUptime(c *gin.Context) UptimeStats {
	var stats UptimeStats

	computePeriod := func(interval string) float64 {
		var total, operational int
		h.db.QueryRowContext(c.Request.Context(),
			`SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'operational')
			FROM health_snapshots
			WHERE checked_at >= NOW() - $1::interval`,
			interval,
		).Scan(&total, &operational)
		if total == 0 {
			return 100
		}
		return float64(operational) / float64(total) * 100
	}

	stats.Last24h = computePeriod("24 hours")
	stats.Last7d = computePeriod("7 days")
	stats.Last30d = computePeriod("30 days")

	return stats
}
