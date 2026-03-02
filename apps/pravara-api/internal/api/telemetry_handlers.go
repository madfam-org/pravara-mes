package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// TelemetryHandler handles telemetry-related HTTP requests.
type TelemetryHandler struct {
	repo *repositories.TelemetryRepository
	log  *logrus.Logger
}

// NewTelemetryHandler creates a new telemetry handler.
func NewTelemetryHandler(repo *repositories.TelemetryRepository, log *logrus.Logger) *TelemetryHandler {
	return &TelemetryHandler{
		repo: repo,
		log:  log,
	}
}

// TelemetryQueryResponse represents the response for telemetry queries.
type TelemetryQueryResponse struct {
	Data       []types.Telemetry `json:"data"`
	Count      int               `json:"count"`
	MachineID  string            `json:"machine_id,omitempty"`
	MetricType string            `json:"metric_type,omitempty"`
	FromTime   string            `json:"from_time,omitempty"`
	ToTime     string            `json:"to_time,omitempty"`
}

// BatchTelemetryRequest represents the request body for batch telemetry insert.
type BatchTelemetryRequest struct {
	Records []TelemetryRecord `json:"records" binding:"required,dive"`
}

// TelemetryRecord represents a single telemetry record in a batch.
type TelemetryRecord struct {
	MachineID  string         `json:"machine_id" binding:"required"`
	Timestamp  time.Time      `json:"timestamp"`
	MetricType string         `json:"metric_type" binding:"required"`
	Value      float64        `json:"value" binding:"required"`
	Unit       string         `json:"unit"`
	Metadata   map[string]any `json:"metadata"`
}

// List returns telemetry data with filtering.
// @Summary List telemetry data
// @Description Returns telemetry data with filtering by machine, metric type, and time range
// @Tags telemetry
// @Produce json
// @Param machine_id query string false "Filter by machine ID (UUID)"
// @Param metric_type query string false "Filter by metric type (temperature, pressure, etc.)"
// @Param from_time query string false "Filter from time (RFC3339)"
// @Param to_time query string false "Filter to time (RFC3339)"
// @Param limit query int false "Number of records to return (default 100, max 1000)"
// @Success 200 {object} TelemetryQueryResponse "Telemetry data"
// @Failure 400 {object} map[string]string "Invalid parameter format"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /telemetry [get]
func (h *TelemetryHandler) List(c *gin.Context) {
	filter := repositories.TelemetryFilter{
		Limit: 100, // default
	}

	// Parse machine_id filter
	if machineIDStr := c.Query("machine_id"); machineIDStr != "" {
		machineID, err := uuid.Parse(machineIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_machine_id",
				"message": "Invalid machine ID format",
			})
			return
		}
		filter.MachineID = &machineID
	}

	// Parse metric_type filter
	if metricType := c.Query("metric_type"); metricType != "" {
		filter.MetricType = &metricType
	}

	// Parse time range
	if fromTimeStr := c.Query("from_time"); fromTimeStr != "" {
		fromTime, err := time.Parse(time.RFC3339, fromTimeStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_from_time",
				"message": "Invalid from_time format (use RFC3339)",
			})
			return
		}
		filter.FromTime = &fromTime
	}

	if toTimeStr := c.Query("to_time"); toTimeStr != "" {
		toTime, err := time.Parse(time.RFC3339, toTimeStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "invalid_to_time",
				"message": "Invalid to_time format (use RFC3339)",
			})
			return
		}
		filter.ToTime = &toTime
	}

	// Parse limit
	if limitStr := c.Query("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err == nil && limit > 0 && limit <= 1000 {
			filter.Limit = limit
		}
	}

	telemetry, err := h.repo.List(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list telemetry")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve telemetry data",
		})
		return
	}

	response := TelemetryQueryResponse{
		Data:  telemetry,
		Count: len(telemetry),
	}

	if filter.MachineID != nil {
		response.MachineID = filter.MachineID.String()
	}
	if filter.MetricType != nil {
		response.MetricType = *filter.MetricType
	}
	if filter.FromTime != nil {
		response.FromTime = filter.FromTime.Format(time.RFC3339)
	}
	if filter.ToTime != nil {
		response.ToTime = filter.ToTime.Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, response)
}

// GetAggregated returns aggregated telemetry data.
// @Summary Get aggregated telemetry
// @Description Returns aggregated telemetry data (min, max, avg) for a machine and metric
// @Tags telemetry
// @Produce json
// @Param machine_id query string true "Machine ID (UUID)"
// @Param metric_type query string true "Metric type (temperature, pressure, etc.)"
// @Param from_time query string false "From time (RFC3339), defaults to 24h ago"
// @Param to_time query string false "To time (RFC3339), defaults to now"
// @Param interval query string false "Aggregation interval (hour, day), default hour"
// @Success 200 {object} map[string]interface{} "Aggregated telemetry data"
// @Failure 400 {object} map[string]string "Missing required parameters"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /telemetry/aggregated [get]
func (h *TelemetryHandler) GetAggregated(c *gin.Context) {
	machineIDStr := c.Query("machine_id")
	if machineIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "missing_machine_id",
			"message": "machine_id is required",
		})
		return
	}

	machineID, err := uuid.Parse(machineIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_machine_id",
			"message": "Invalid machine ID format",
		})
		return
	}

	metricType := c.Query("metric_type")
	if metricType == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "missing_metric_type",
			"message": "metric_type is required",
		})
		return
	}

	// Default to last 24 hours
	toTime := time.Now()
	fromTime := toTime.Add(-24 * time.Hour)

	if fromTimeStr := c.Query("from_time"); fromTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, fromTimeStr); err == nil {
			fromTime = t
		}
	}
	if toTimeStr := c.Query("to_time"); toTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, toTimeStr); err == nil {
			toTime = t
		}
	}

	interval := c.DefaultQuery("interval", "hour")

	data, err := h.repo.GetAggregated(c.Request.Context(), machineID, metricType, fromTime, toTime, interval)
	if err != nil {
		h.log.WithError(err).Error("Failed to get aggregated telemetry")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve aggregated telemetry",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":        data,
		"machine_id":  machineID.String(),
		"metric_type": metricType,
		"from_time":   fromTime.Format(time.RFC3339),
		"to_time":     toTime.Format(time.RFC3339),
		"interval":    interval,
	})
}

// BatchInsert handles bulk telemetry insertion.
// @Summary Batch insert telemetry
// @Description Inserts multiple telemetry records in a single request (max 1000)
// @Tags telemetry
// @Accept json
// @Produce json
// @Param body body BatchTelemetryRequest true "Batch of telemetry records"
// @Success 201 {object} map[string]interface{} "Insertion confirmation with count"
// @Failure 400 {object} map[string]string "Validation error or empty batch"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /telemetry/batch [post]
func (h *TelemetryHandler) BatchInsert(c *gin.Context) {
	var req BatchTelemetryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	if len(req.Records) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "At least one record is required",
		})
		return
	}

	if len(req.Records) > 1000 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "Maximum 1000 records per batch",
		})
		return
	}

	// Get tenant ID from context
	tenantIDStr, ok := middleware.GetTenantID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "Tenant context not found",
		})
		return
	}
	tenantID, _ := uuid.Parse(tenantIDStr)

	// Convert to telemetry records
	records := make([]types.Telemetry, 0, len(req.Records))
	now := time.Now()

	for _, r := range req.Records {
		machineID, err := uuid.Parse(r.MachineID)
		if err != nil {
			h.log.WithField("machine_id", r.MachineID).Warn("Invalid machine ID in batch")
			continue
		}

		timestamp := r.Timestamp
		if timestamp.IsZero() {
			timestamp = now
		}

		records = append(records, types.Telemetry{
			TenantID:   tenantID,
			MachineID:  machineID,
			Timestamp:  timestamp,
			MetricType: r.MetricType,
			Value:      r.Value,
			Unit:       r.Unit,
			Metadata:   r.Metadata,
		})
	}

	if len(records) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": "No valid records to insert",
		})
		return
	}

	if err := h.repo.CreateBatch(c.Request.Context(), records); err != nil {
		h.log.WithError(err).Error("Failed to batch insert telemetry")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to insert telemetry records",
		})
		return
	}

	h.log.WithField("count", len(records)).Info("Batch telemetry inserted")
	c.JSON(http.StatusCreated, gin.H{
		"message":  "Telemetry records inserted successfully",
		"inserted": len(records),
	})
}

// GetLatest returns the most recent telemetry for a machine.
// @Summary Get latest telemetry
// @Description Returns the most recent telemetry record for a machine and metric type
// @Tags telemetry
// @Produce json
// @Param machine_id query string true "Machine ID (UUID)"
// @Param metric_type query string true "Metric type (temperature, pressure, etc.)"
// @Success 200 {object} types.Telemetry "Latest telemetry record"
// @Failure 400 {object} map[string]string "Missing required parameters"
// @Failure 404 {object} map[string]string "No telemetry found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /telemetry/latest [get]
func (h *TelemetryHandler) GetLatest(c *gin.Context) {
	machineIDStr := c.Query("machine_id")
	if machineIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "missing_machine_id",
			"message": "machine_id is required",
		})
		return
	}

	machineID, err := uuid.Parse(machineIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_machine_id",
			"message": "Invalid machine ID format",
		})
		return
	}

	metricType := c.Query("metric_type")
	if metricType == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "missing_metric_type",
			"message": "metric_type is required",
		})
		return
	}

	telemetry, err := h.repo.GetLatest(c.Request.Context(), machineID, metricType)
	if err != nil {
		h.log.WithError(err).Error("Failed to get latest telemetry")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve latest telemetry",
		})
		return
	}

	if telemetry == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "No telemetry found for this machine and metric type",
		})
		return
	}

	c.JSON(http.StatusOK, telemetry)
}
