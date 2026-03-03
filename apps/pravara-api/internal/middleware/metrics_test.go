package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/observability"
)

// getGaugeValue retrieves the current value of a Gauge metric
func getGaugeValue(gauge prometheus.Gauge) float64 {
	var metric dto.Metric
	if err := gauge.Write(&metric); err != nil {
		return 0
	}
	return metric.Gauge.GetValue()
}

// getMetricFamilies retrieves all metric families from the default registry
func getMetricFamilies() map[string]*dto.MetricFamily {
	families, _ := prometheus.DefaultGatherer.Gather()
	result := make(map[string]*dto.MetricFamily)
	for _, family := range families {
		result[family.GetName()] = family
	}
	return result
}

// findHistogramSampleCount finds the sample count for a histogram with specific labels
func findHistogramSampleCount(familyName string, labelMatcher map[string]string) uint64 {
	families := getMetricFamilies()
	family, ok := families[familyName]
	if !ok {
		return 0
	}

	for _, metric := range family.GetMetric() {
		if labelsMatch(metric.GetLabel(), labelMatcher) {
			if metric.Histogram != nil {
				return metric.Histogram.GetSampleCount()
			}
		}
	}
	return 0
}

// findCounterValue finds the value for a counter with specific labels
func findCounterValue(familyName string, labelMatcher map[string]string) float64 {
	families := getMetricFamilies()
	family, ok := families[familyName]
	if !ok {
		return 0
	}

	for _, metric := range family.GetMetric() {
		if labelsMatch(metric.GetLabel(), labelMatcher) {
			if metric.Counter != nil {
				return metric.Counter.GetValue()
			}
		}
	}
	return 0
}

// labelsMatch checks if metric labels match the given matcher
func labelsMatch(labels []*dto.LabelPair, matcher map[string]string) bool {
	if len(labels) < len(matcher) {
		return false
	}

	labelMap := make(map[string]string)
	for _, label := range labels {
		labelMap[label.GetName()] = label.GetValue()
	}

	for key, value := range matcher {
		if labelMap[key] != value {
			return false
		}
	}
	return true
}

func TestMetrics_SkipsMetricsEndpoint(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	// Record initial counter value
	initialCount := findCounterValue("pravara_api_http_requests_total", map[string]string{
		"method":    "GET",
		"path":      "/metrics",
		"status":    "200",
		"tenant_id": "unknown",
	})

	router := gin.New()
	router.Use(Metrics())
	router.GET("/metrics", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "metrics"})
	})

	// Test: Request to /metrics endpoint
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/metrics", nil)
	router.ServeHTTP(w, req)

	// Assert: Request succeeds
	assert.Equal(t, http.StatusOK, w.Code)

	// Assert: Metrics were not incremented (no recursive collection)
	afterCount := findCounterValue("pravara_api_http_requests_total", map[string]string{
		"method":    "GET",
		"path":      "/metrics",
		"status":    "200",
		"tenant_id": "unknown",
	})
	assert.Equal(t, initialCount, afterCount, "Metrics endpoint should not record its own metrics")
}

func TestMetrics_RecordsRequestDuration(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Metrics())
	router.GET("/test", func(c *gin.Context) {
		// Simulate some processing time
		time.Sleep(10 * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Record initial histogram count
	initialCount := findHistogramSampleCount("pravara_api_http_request_duration_seconds", map[string]string{
		"method":    "GET",
		"path":      "/test",
		"tenant_id": "unknown",
	})

	// Test: Make request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	// Assert: Request succeeds
	assert.Equal(t, http.StatusOK, w.Code)

	// Assert: Duration was recorded
	afterCount := findHistogramSampleCount("pravara_api_http_request_duration_seconds", map[string]string{
		"method":    "GET",
		"path":      "/test",
		"tenant_id": "unknown",
	})
	assert.Greater(t, afterCount, initialCount, "Request duration should be recorded")
}

func TestMetrics_RecordsRequestCount(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Metrics())
	router.GET("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})
	router.POST("/api/create", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"id": "123"})
	})

	tests := []struct {
		name         string
		method       string
		path         string
		expectedCode int
	}{
		{
			name:         "GET request with 200",
			method:       "GET",
			path:         "/api/test",
			expectedCode: http.StatusOK,
		},
		{
			name:         "POST request with 201",
			method:       "POST",
			path:         "/api/create",
			expectedCode: http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Record initial counter value (status is numeric string, not status text)
			statusStr := strconv.Itoa(tt.expectedCode)
			initialCount := findCounterValue("pravara_api_http_requests_total", map[string]string{
				"method":    tt.method,
				"path":      tt.path,
				"status":    statusStr,
				"tenant_id": "unknown",
			})

			// Test: Make request
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(tt.method, tt.path, nil)
			router.ServeHTTP(w, req)

			// Assert: Request succeeds with expected status
			assert.Equal(t, tt.expectedCode, w.Code)

			// Assert: Counter was incremented
			afterCount := findCounterValue("pravara_api_http_requests_total", map[string]string{
				"method":    tt.method,
				"path":      tt.path,
				"status":    statusStr,
				"tenant_id": "unknown",
			})
			assert.Greater(t, afterCount, initialCount, "Request counter should be incremented")
		})
	}
}

func TestMetrics_UsesRoutePath(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Metrics())
	router.GET("/api/v1/machines/:id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
	})

	// Record initial counts for route template path
	initialCountTemplate := findHistogramSampleCount("pravara_api_http_request_duration_seconds", map[string]string{
		"method":    "GET",
		"path":      "/api/v1/machines/:id",
		"tenant_id": "unknown",
	})

	// Test: Make requests with different IDs
	ids := []string{"machine-1", "machine-2", "machine-3"}
	for _, id := range ids {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/v1/machines/"+id, nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Assert: All requests recorded under template path (not individual paths)
	afterCountTemplate := findHistogramSampleCount("pravara_api_http_request_duration_seconds", map[string]string{
		"method":    "GET",
		"path":      "/api/v1/machines/:id",
		"tenant_id": "unknown",
	})
	assert.Equal(t, initialCountTemplate+uint64(len(ids)), afterCountTemplate,
		"Should use route template path to avoid high cardinality")
}

func TestMetrics_InflightTracking(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)

	// Channel to control request processing
	processingStarted := make(chan bool)
	continueProcessing := make(chan bool)

	router := gin.New()
	router.Use(Metrics())
	router.GET("/slow", func(c *gin.Context) {
		processingStarted <- true
		<-continueProcessing
		c.JSON(http.StatusOK, gin.H{"message": "done"})
	})

	// Record initial in-flight count
	initialInflight := getGaugeValue(observability.HTTPRequestsInFlight)

	// Start request in background
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/slow", nil)

	go router.ServeHTTP(w, req)

	// Wait for request to start processing
	<-processingStarted

	// Assert: In-flight count increased
	duringInflight := getGaugeValue(observability.HTTPRequestsInFlight)
	assert.Greater(t, duringInflight, initialInflight, "In-flight requests should increase during processing")

	// Allow request to complete
	continueProcessing <- true

	// Give some time for request to complete
	time.Sleep(50 * time.Millisecond)

	// Assert: In-flight count returned to initial value
	afterInflight := getGaugeValue(observability.HTTPRequestsInFlight)
	assert.Equal(t, initialInflight, afterInflight, "In-flight requests should decrease after completion")
}
