package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/billing"
)

// mockUsageRecorder is a mock implementation of billing.UsageRecorder
type mockUsageRecorder struct {
	mock.Mock
}

func (m *mockUsageRecorder) RecordEvent(ctx context.Context, event billing.UsageEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *mockUsageRecorder) RecordBatch(ctx context.Context, events []billing.UsageEvent) error {
	args := m.Called(ctx, events)
	return args.Error(0)
}

func (m *mockUsageRecorder) GetTenantUsage(ctx context.Context, tenantID string, from, to time.Time) (*billing.TenantUsageSummary, error) {
	args := m.Called(ctx, tenantID, from, to)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*billing.TenantUsageSummary), args.Error(1)
}

func (m *mockUsageRecorder) GetDailyUsage(ctx context.Context, tenantID string, from, to time.Time) ([]billing.DailyUsageSummary, error) {
	args := m.Called(ctx, tenantID, from, to)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]billing.DailyUsageSummary), args.Error(1)
}

func (m *mockUsageRecorder) Close() error {
	args := m.Called()
	return args.Error(0)
}

func TestUsageTracking_SkipsHealthEndpoints(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	mockRecorder := new(mockUsageRecorder)

	router := gin.New()
	router.Use(UsageTracking(mockRecorder, logger))

	// Add health endpoints
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})
	router.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "live"})
	})
	router.GET("/health/ready", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	router.GET("/metrics", func(c *gin.Context) {
		c.String(http.StatusOK, "# HELP metrics")
	})

	healthPaths := []string{"/health", "/health/live", "/health/ready", "/metrics"}

	for _, path := range healthPaths {
		t.Run("Skip_"+path, func(t *testing.T) {
			// Test: Request to health endpoint
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", path, nil)
			router.ServeHTTP(w, req)

			// Assert: Request succeeds
			assert.Equal(t, http.StatusOK, w.Code)

			// Assert: RecordEvent was never called (usage tracking skipped)
			// Wait a bit for any async operations that shouldn't happen
			time.Sleep(50 * time.Millisecond)
			mockRecorder.AssertNotCalled(t, "RecordEvent")
		})
	}
}

func TestUsageTracking_RecordsAPICall(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	mockRecorder := new(mockUsageRecorder)

	// Expect RecordEvent to be called with matching event
	mockRecorder.On("RecordEvent", mock.Anything, mock.MatchedBy(func(event billing.UsageEvent) bool {
		return event.TenantID == "tenant-123" &&
			event.EventType == billing.UsageEventAPICall &&
			event.Quantity == 1 &&
			event.Metadata["method"] == "POST" &&
			event.Metadata["path"] == "/api/v1/machines"
	})).Return(nil)

	router := gin.New()

	// Simulate auth middleware setting tenant ID
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyTenantID), "tenant-123")
		c.Next()
	})

	router.Use(UsageTracking(mockRecorder, logger))

	router.POST("/api/v1/machines", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"id": "machine-456"})
	})

	// Test: Make API request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/machines", nil)
	router.ServeHTTP(w, req)

	// Assert: Request succeeds
	assert.Equal(t, http.StatusCreated, w.Code)

	// Wait for async recording to complete
	time.Sleep(100 * time.Millisecond)

	// Assert: RecordEvent was called
	mockRecorder.AssertExpectations(t)
}

func TestUsageTracking_NoTenantSkipped(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	mockRecorder := new(mockUsageRecorder)

	router := gin.New()
	// No auth middleware - tenant ID not set in context
	router.Use(UsageTracking(mockRecorder, logger))

	router.GET("/public/endpoint", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "public"})
	})

	// Test: Request without tenant context
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/public/endpoint", nil)
	router.ServeHTTP(w, req)

	// Assert: Request succeeds
	assert.Equal(t, http.StatusOK, w.Code)

	// Wait for any potential async operations
	time.Sleep(50 * time.Millisecond)

	// Assert: RecordEvent was never called (no tenant context)
	mockRecorder.AssertNotCalled(t, "RecordEvent")
}

func TestUsageTracking_AsyncRecording(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	mockRecorder := new(mockUsageRecorder)

	// Channel to signal when RecordEvent is called
	recordingDone := make(chan bool, 1)

	// Expect RecordEvent to be called
	mockRecorder.On("RecordEvent", mock.Anything, mock.MatchedBy(func(event billing.UsageEvent) bool {
		// Signal that recording happened
		recordingDone <- true
		return event.TenantID == "tenant-async" &&
			event.EventType == billing.UsageEventAPICall &&
			event.Quantity == 1
	})).Return(nil)

	router := gin.New()

	// Simulate auth middleware setting tenant ID
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyTenantID), "tenant-async")
		c.Next()
	})

	router.Use(UsageTracking(mockRecorder, logger))

	router.GET("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "test"})
	})

	// Test: Make request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/test", nil)

	// Record time before request
	startTime := time.Now()

	// Execute request
	router.ServeHTTP(w, req)

	// Record time after request handler completes
	handlerDuration := time.Since(startTime)

	// Assert: Request completes quickly (not blocked by usage recording)
	assert.Less(t, handlerDuration, 50*time.Millisecond,
		"Request should not be blocked by async usage recording")

	// Assert: Request succeeds
	assert.Equal(t, http.StatusOK, w.Code)

	// Wait for async recording to complete (with timeout)
	select {
	case <-recordingDone:
		// Recording completed successfully
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Async recording did not complete within timeout")
	}

	// Assert: RecordEvent was called asynchronously
	mockRecorder.AssertExpectations(t)
}

func TestUsageTracking_RecordsMetadata(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetOutput(httptest.NewRecorder().Body)

	mockRecorder := new(mockUsageRecorder)

	tests := []struct {
		name       string
		method     string
		path       string
		statusCode int
		tenantID   string
	}{
		{
			name:       "GET request with 200",
			method:     "GET",
			path:       "/api/v1/machines",
			statusCode: http.StatusOK,
			tenantID:   "tenant-get",
		},
		{
			name:       "POST request with 201",
			method:     "POST",
			path:       "/api/v1/orders",
			statusCode: http.StatusCreated,
			tenantID:   "tenant-post",
		},
		{
			name:       "DELETE request with 204",
			method:     "DELETE",
			path:       "/api/v1/machines/123",
			statusCode: http.StatusNoContent,
			tenantID:   "tenant-delete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Expect RecordEvent with specific metadata
			mockRecorder.On("RecordEvent", mock.Anything, mock.MatchedBy(func(event billing.UsageEvent) bool {
				return event.TenantID == tt.tenantID &&
					event.EventType == billing.UsageEventAPICall &&
					event.Quantity == 1 &&
					event.Metadata["method"] == tt.method &&
					event.Metadata["path"] == tt.path
			})).Return(nil).Once()

			router := gin.New()

			// Simulate auth middleware setting tenant ID
			router.Use(func(c *gin.Context) {
				c.Set(string(ContextKeyTenantID), tt.tenantID)
				c.Next()
			})

			router.Use(UsageTracking(mockRecorder, logger))

			// Add handler for the specific method and path
			router.Handle(tt.method, tt.path, func(c *gin.Context) {
				c.Status(tt.statusCode)
			})

			// Test: Make request
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(tt.method, tt.path, nil)
			router.ServeHTTP(w, req)

			// Assert: Status code matches
			assert.Equal(t, tt.statusCode, w.Code)

			// Wait for async recording
			time.Sleep(100 * time.Millisecond)
		})
	}

	// Assert all expectations met
	mockRecorder.AssertExpectations(t)
}
