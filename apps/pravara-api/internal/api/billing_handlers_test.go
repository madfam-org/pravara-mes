package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/billing"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

// mockUsageRecorder implements billing.UsageRecorder for testing.
type mockUsageRecorder struct {
	recordEventFunc    func(ctx context.Context, event billing.UsageEvent) error
	recordBatchFunc    func(ctx context.Context, events []billing.UsageEvent) error
	getTenantUsageFunc func(ctx context.Context, tenantID string, from, to time.Time) (*billing.TenantUsageSummary, error)
	getDailyUsageFunc  func(ctx context.Context, tenantID string, from, to time.Time) ([]billing.DailyUsageSummary, error)
	closeFunc          func() error
}

func (m *mockUsageRecorder) RecordEvent(ctx context.Context, event billing.UsageEvent) error {
	if m.recordEventFunc != nil {
		return m.recordEventFunc(ctx, event)
	}
	return nil
}

func (m *mockUsageRecorder) RecordBatch(ctx context.Context, events []billing.UsageEvent) error {
	if m.recordBatchFunc != nil {
		return m.recordBatchFunc(ctx, events)
	}
	return nil
}

func (m *mockUsageRecorder) GetTenantUsage(ctx context.Context, tenantID string, from, to time.Time) (*billing.TenantUsageSummary, error) {
	if m.getTenantUsageFunc != nil {
		return m.getTenantUsageFunc(ctx, tenantID, from, to)
	}
	return &billing.TenantUsageSummary{
		TenantID: tenantID,
		FromDate: from,
		ToDate:   to,
	}, nil
}

func (m *mockUsageRecorder) GetDailyUsage(ctx context.Context, tenantID string, from, to time.Time) ([]billing.DailyUsageSummary, error) {
	if m.getDailyUsageFunc != nil {
		return m.getDailyUsageFunc(ctx, tenantID, from, to)
	}
	return []billing.DailyUsageSummary{}, nil
}

func (m *mockUsageRecorder) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

// setupBillingTestRouter creates a test router with billing handlers.
func setupBillingTestRouter(recorder billing.UsageRecorder, tenantID string) *gin.Engine {
	gin.SetMode(gin.TestMode)

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel) // Suppress logs in tests

	handler := NewBillingHandler(recorder, log)

	router := gin.New()

	// Add middleware to set tenant context if provided
	if tenantID != "" {
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyTenantID), tenantID)
			c.Next()
		})
	}

	// Setup routes similar to actual API
	v1 := router.Group("/v1")
	{
		billing := v1.Group("/billing")
		{
			billing.GET("/usage", handler.GetUsage)
			billing.GET("/usage/daily", handler.GetDailyUsage)
		}

		admin := v1.Group("/admin")
		{
			adminBilling := admin.Group("/billing")
			{
				adminBilling.GET("/tenants/:id/usage", handler.GetTenantUsageAdmin)
			}
		}
	}

	return router
}

// TestGetUsageSummary_Success tests successful usage retrieval.
func TestGetUsageSummary_Success(t *testing.T) {
	expectedTenantID := "tenant-123"
	expectedSummary := &billing.TenantUsageSummary{
		TenantID:        expectedTenantID,
		APICallCount:    1500,
		TelemetryPoints: 50000,
		StorageMB:       2048,
		OrdersCreated:   25,
	}

	mock := &mockUsageRecorder{
		getTenantUsageFunc: func(ctx context.Context, tenantID string, from, to time.Time) (*billing.TenantUsageSummary, error) {
			assert.Equal(t, expectedTenantID, tenantID)
			return expectedSummary, nil
		},
	}

	router := setupBillingTestRouter(mock, expectedTenantID)

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/usage", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response billing.TenantUsageSummary
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, expectedSummary.TenantID, response.TenantID)
	assert.Equal(t, expectedSummary.APICallCount, response.APICallCount)
	assert.Equal(t, expectedSummary.TelemetryPoints, response.TelemetryPoints)
	assert.Equal(t, expectedSummary.StorageMB, response.StorageMB)
	assert.Equal(t, expectedSummary.OrdersCreated, response.OrdersCreated)
}

// TestGetUsageSummary_NoTenant tests error when tenant context is missing.
func TestGetUsageSummary_NoTenant(t *testing.T) {
	mock := &mockUsageRecorder{}
	router := setupBillingTestRouter(mock, "") // No tenant ID

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/usage", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "unauthorized", response["error"])
	assert.Contains(t, response["message"], "Tenant context not found")
}

// TestGetUsageSummary_EmptyUsage tests response for new tenant with no usage.
func TestGetUsageSummary_EmptyUsage(t *testing.T) {
	expectedTenantID := "new-tenant-456"

	mock := &mockUsageRecorder{
		getTenantUsageFunc: func(ctx context.Context, tenantID string, from, to time.Time) (*billing.TenantUsageSummary, error) {
			return &billing.TenantUsageSummary{
				TenantID:        tenantID,
				FromDate:        from,
				ToDate:          to,
				APICallCount:    0,
				TelemetryPoints: 0,
				StorageMB:       0,
				OrdersCreated:   0,
			}, nil
		},
	}

	router := setupBillingTestRouter(mock, expectedTenantID)

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/usage", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response billing.TenantUsageSummary
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, expectedTenantID, response.TenantID)
	assert.Equal(t, int64(0), response.APICallCount)
	assert.Equal(t, int64(0), response.TelemetryPoints)
	assert.Equal(t, int64(0), response.StorageMB)
}

// TestGetUsageSummary_WithDateRange tests usage query with date parameters.
func TestGetUsageSummary_WithDateRange(t *testing.T) {
	expectedTenantID := "tenant-789"
	expectedFrom := "2024-01-01"
	expectedTo := "2024-01-31"

	mock := &mockUsageRecorder{
		getTenantUsageFunc: func(ctx context.Context, tenantID string, from, to time.Time) (*billing.TenantUsageSummary, error) {
			assert.Equal(t, expectedTenantID, tenantID)
			assert.Equal(t, expectedFrom, from.Format("2006-01-02"))
			assert.Equal(t, expectedTo, to.Format("2006-01-02"))
			return &billing.TenantUsageSummary{
				TenantID:     tenantID,
				FromDate:     from,
				ToDate:       to,
				APICallCount: 500,
			}, nil
		},
	}

	router := setupBillingTestRouter(mock, expectedTenantID)

	url := fmt.Sprintf("/v1/billing/usage?from=%s&to=%s", expectedFrom, expectedTo)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestGetUsageSummary_InvalidDateFormat tests error handling for bad date format.
func TestGetUsageSummary_InvalidDateFormat(t *testing.T) {
	mock := &mockUsageRecorder{}
	router := setupBillingTestRouter(mock, "tenant-123")

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/usage?from=invalid-date", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "invalid_date", response["error"])
}

// TestGetUsageSummary_InvalidDateRange tests error when 'to' is before 'from'.
func TestGetUsageSummary_InvalidDateRange(t *testing.T) {
	mock := &mockUsageRecorder{}
	router := setupBillingTestRouter(mock, "tenant-123")

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/usage?from=2024-01-31&to=2024-01-01", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "invalid_range", response["error"])
	assert.Contains(t, response["message"], "must be after")
}

// TestGetUsageSummary_ExceedsMaxRange tests validation of 90-day limit.
func TestGetUsageSummary_ExceedsMaxRange(t *testing.T) {
	mock := &mockUsageRecorder{}
	router := setupBillingTestRouter(mock, "tenant-123")

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/usage?from=2024-01-01&to=2024-05-01", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "invalid_range", response["error"])
	assert.Contains(t, response["message"], "90 days")
}

// TestGetDailyUsage_Success tests successful daily usage retrieval.
func TestGetDailyUsage_Success(t *testing.T) {
	expectedTenantID := "tenant-123"
	expectedDaily := []billing.DailyUsageSummary{
		{Date: "2024-01-01", APICallCount: 100, TelemetryPoints: 5000},
		{Date: "2024-01-02", APICallCount: 150, TelemetryPoints: 6000},
		{Date: "2024-01-03", APICallCount: 120, TelemetryPoints: 5500},
	}

	mock := &mockUsageRecorder{
		getDailyUsageFunc: func(ctx context.Context, tenantID string, from, to time.Time) ([]billing.DailyUsageSummary, error) {
			assert.Equal(t, expectedTenantID, tenantID)
			return expectedDaily, nil
		},
	}

	router := setupBillingTestRouter(mock, expectedTenantID)

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/usage/daily", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, expectedTenantID, response["tenant_id"])

	dailyUsage, ok := response["daily_usage"].([]interface{})
	require.True(t, ok)
	assert.Len(t, dailyUsage, 3)
}

// TestGetDailyUsage_WithDateRange tests daily usage with date parameters.
func TestGetDailyUsage_WithDateRange(t *testing.T) {
	expectedTenantID := "tenant-456"
	expectedFrom := "2024-02-01"
	expectedTo := "2024-02-07"

	mock := &mockUsageRecorder{
		getDailyUsageFunc: func(ctx context.Context, tenantID string, from, to time.Time) ([]billing.DailyUsageSummary, error) {
			assert.Equal(t, expectedFrom, from.Format("2006-01-02"))
			assert.Equal(t, expectedTo, to.Format("2006-01-02"))

			// Return 7 days of data
			var dailyData []billing.DailyUsageSummary
			for i := 0; i < 7; i++ {
				date := from.AddDate(0, 0, i)
				dailyData = append(dailyData, billing.DailyUsageSummary{
					Date:         date.Format("2006-01-02"),
					APICallCount: int64(100 + i*10),
				})
			}
			return dailyData, nil
		},
	}

	router := setupBillingTestRouter(mock, expectedTenantID)

	url := fmt.Sprintf("/v1/billing/usage/daily?from=%s&to=%s", expectedFrom, expectedTo)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, expectedFrom, response["from_date"])
	assert.Equal(t, expectedTo, response["to_date"])

	dailyUsage, ok := response["daily_usage"].([]interface{})
	require.True(t, ok)
	assert.Len(t, dailyUsage, 7)
}

// TestGetDailyUsage_InvalidDates tests error handling for invalid date format.
func TestGetDailyUsage_InvalidDates(t *testing.T) {
	mock := &mockUsageRecorder{}

	tests := []struct {
		name        string
		queryString string
	}{
		{
			name:        "invalid from date",
			queryString: "?from=not-a-date&to=2024-01-31",
		},
		{
			name:        "invalid to date",
			queryString: "?from=2024-01-01&to=not-a-date",
		},
		{
			name:        "wrong date format",
			queryString: "?from=01/01/2024&to=01/31/2024",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := setupBillingTestRouter(mock, "tenant-123")

			req := httptest.NewRequest(http.MethodGet, "/v1/billing/usage/daily"+tt.queryString, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)

			var response map[string]string
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, "invalid_date", response["error"])
		})
	}
}

// TestGetDailyUsage_NoTenant tests error when tenant context is missing.
func TestGetDailyUsage_NoTenant(t *testing.T) {
	mock := &mockUsageRecorder{}
	router := setupBillingTestRouter(mock, "") // No tenant ID

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/usage/daily", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "unauthorized", response["error"])
}

// TestAdminGetTenantUsage_Success tests admin can view any tenant's usage.
func TestAdminGetTenantUsage_Success(t *testing.T) {
	targetTenantID := "target-tenant-789"
	expectedSummary := &billing.TenantUsageSummary{
		TenantID:        targetTenantID,
		APICallCount:    2500,
		TelemetryPoints: 75000,
		StorageMB:       5120,
	}

	mock := &mockUsageRecorder{
		getTenantUsageFunc: func(ctx context.Context, tenantID string, from, to time.Time) (*billing.TenantUsageSummary, error) {
			assert.Equal(t, targetTenantID, tenantID)
			return expectedSummary, nil
		},
	}

	router := setupBillingTestRouter(mock, "") // Admin endpoint doesn't need tenant context

	url := fmt.Sprintf("/v1/admin/billing/tenants/%s/usage", targetTenantID)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response billing.TenantUsageSummary
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, expectedSummary.TenantID, response.TenantID)
	assert.Equal(t, expectedSummary.APICallCount, response.APICallCount)
	assert.Equal(t, expectedSummary.TelemetryPoints, response.TelemetryPoints)
}

// TestAdminGetTenantUsage_WithDateRange tests admin endpoint with date parameters.
func TestAdminGetTenantUsage_WithDateRange(t *testing.T) {
	targetTenantID := "target-tenant-123"
	expectedFrom := "2024-01-01"
	expectedTo := "2024-03-31"

	mock := &mockUsageRecorder{
		getTenantUsageFunc: func(ctx context.Context, tenantID string, from, to time.Time) (*billing.TenantUsageSummary, error) {
			assert.Equal(t, targetTenantID, tenantID)
			assert.Equal(t, expectedFrom, from.Format("2006-01-02"))
			assert.Equal(t, expectedTo, to.Format("2006-01-02"))
			return &billing.TenantUsageSummary{
				TenantID: tenantID,
				FromDate: from,
				ToDate:   to,
			}, nil
		},
	}

	router := setupBillingTestRouter(mock, "")

	url := fmt.Sprintf("/v1/admin/billing/tenants/%s/usage?from=%s&to=%s",
		targetTenantID, expectedFrom, expectedTo)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAdminGetTenantUsage_InvalidTenantID tests error for missing tenant ID.
func TestAdminGetTenantUsage_InvalidTenantID(t *testing.T) {
	mock := &mockUsageRecorder{}
	router := setupBillingTestRouter(mock, "")

	// Empty tenant ID in path
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/billing/tenants//usage", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Handler validates and returns 400 for empty tenant ID
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "invalid_request", response["error"])
}

// TestAdminGetTenantUsage_MaxRange tests 365-day limit for admin endpoint.
func TestAdminGetTenantUsage_MaxRange(t *testing.T) {
	mock := &mockUsageRecorder{}
	router := setupBillingTestRouter(mock, "")

	// Request exceeding 365 days
	req := httptest.NewRequest(http.MethodGet,
		"/v1/admin/billing/tenants/tenant-123/usage?from=2023-01-01&to=2024-06-01", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "invalid_range", response["error"])
	assert.Contains(t, response["message"], "365 days")
}

// TestAdminGetTenantUsage_InvalidDates tests admin endpoint date validation.
func TestAdminGetTenantUsage_InvalidDates(t *testing.T) {
	mock := &mockUsageRecorder{}
	router := setupBillingTestRouter(mock, "")

	req := httptest.NewRequest(http.MethodGet,
		"/v1/admin/billing/tenants/tenant-123/usage?from=invalid&to=2024-01-31", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "invalid_date", response["error"])
}

// TestBillingHandler_RecorderError tests error handling when recorder fails.
func TestBillingHandler_RecorderError(t *testing.T) {
	expectedError := fmt.Errorf("redis connection failed")

	mock := &mockUsageRecorder{
		getTenantUsageFunc: func(ctx context.Context, tenantID string, from, to time.Time) (*billing.TenantUsageSummary, error) {
			return nil, expectedError
		},
	}

	router := setupBillingTestRouter(mock, "tenant-123")

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/usage", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "internal_error", response["error"])
	assert.Contains(t, response["message"], "Failed to retrieve usage data")
}

// TestBillingHandler_DailyUsageRecorderError tests error handling in daily endpoint.
func TestBillingHandler_DailyUsageRecorderError(t *testing.T) {
	expectedError := fmt.Errorf("redis timeout")

	mock := &mockUsageRecorder{
		getDailyUsageFunc: func(ctx context.Context, tenantID string, from, to time.Time) ([]billing.DailyUsageSummary, error) {
			return nil, expectedError
		},
	}

	router := setupBillingTestRouter(mock, "tenant-123")

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/usage/daily", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "internal_error", response["error"])
	assert.Contains(t, response["message"], "Failed to retrieve daily usage data")
}

// TestBillingHandler_DefaultDateRange tests default date parameters.
func TestBillingHandler_DefaultDateRange(t *testing.T) {
	expectedTenantID := "tenant-default"
	now := time.Now()
	expectedMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	mock := &mockUsageRecorder{
		getTenantUsageFunc: func(ctx context.Context, tenantID string, from, to time.Time) (*billing.TenantUsageSummary, error) {
			// Verify default dates: current month start to now
			assert.Equal(t, expectedMonth.Year(), from.Year())
			assert.Equal(t, expectedMonth.Month(), from.Month())
			assert.Equal(t, 1, from.Day())

			// 'to' should be roughly current time
			assert.Equal(t, now.Year(), to.Year())
			assert.Equal(t, now.Month(), to.Month())
			assert.Equal(t, now.Day(), to.Day())

			return &billing.TenantUsageSummary{
				TenantID: tenantID,
				FromDate: from,
				ToDate:   to,
			}, nil
		},
	}

	router := setupBillingTestRouter(mock, expectedTenantID)

	// Request without date parameters - should use defaults
	req := httptest.NewRequest(http.MethodGet, "/v1/billing/usage", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
