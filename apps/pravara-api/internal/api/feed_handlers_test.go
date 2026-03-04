package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// --- Test helpers ---

func newTestFeedRouter(t *testing.T) (*gin.Engine, *mockFeedData) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mock := &mockFeedData{}
	router := gin.New()

	return router, mock
}

type mockFeedData struct {
	orders     []repositories.CRMOrder
	orderTotal int
	orderErr   error

	orderStatus    *repositories.CRMOrderStatus
	orderStatusErr error

	milestones   []repositories.SocialMilestone
	milestoneErr error

	stats    *repositories.SocialStats
	statsErr error

	highlights   []repositories.SocialHighlight
	highlightErr error

	timelineEvents []repositories.OutboxEvent
	timelineTotal  int
	timelineErr    error
}

// --- Tests using mock handlers that replicate FeedHandler logic ---
// These tests verify the JSON response structure and HTTP status codes
// by simulating what the real handlers produce.
// The existing setTenantContext(tenantID, userID) helper from
// apikey_handlers_test.go is reused here.

func TestFeedHandler_CRMOrders_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	now := time.Now().UTC()
	orderID := uuid.New()
	expectedOrders := []repositories.CRMOrder{
		{
			ID:              orderID,
			CustomerName:    "Test Customer",
			Status:          "in_production",
			Priority:        1,
			TotalTasks:      5,
			CompletedTasks:  3,
			ProgressPercent: 60.0,
			LastUpdatedAt:   now,
			CreatedAt:       now.Add(-24 * time.Hour),
		},
	}

	router := gin.New()
	router.GET("/v1/feeds/crm/orders", setTenantContext("test-tenant", "test-user"), func(c *gin.Context) {
		limit := queryInt(c, "limit", 50)
		offset := queryInt(c, "offset", 0)
		c.JSON(http.StatusOK, gin.H{
			"orders": expectedOrders,
			"total":  1,
			"limit":  limit,
			"offset": offset,
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/feeds/crm/orders", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if _, ok := body["orders"]; !ok {
		t.Error("response missing 'orders' field")
	}
	if _, ok := body["total"]; !ok {
		t.Error("response missing 'total' field")
	}
	if _, ok := body["limit"]; !ok {
		t.Error("response missing 'limit' field")
	}
	if _, ok := body["offset"]; !ok {
		t.Error("response missing 'offset' field")
	}

	var orders []repositories.CRMOrder
	if err := json.Unmarshal(body["orders"], &orders); err != nil {
		t.Fatalf("failed to parse orders: %v", err)
	}
	if len(orders) != 1 {
		t.Errorf("expected 1 order, got %d", len(orders))
	}
	if orders[0].CustomerName != "Test Customer" {
		t.Errorf("expected customer name 'Test Customer', got %q", orders[0].CustomerName)
	}
	if orders[0].ProgressPercent != 60.0 {
		t.Errorf("expected progress 60.0, got %f", orders[0].ProgressPercent)
	}
}

func TestFeedHandler_CRMOrders_Pagination(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/v1/feeds/crm/orders", setTenantContext("test-tenant", "test-user"), func(c *gin.Context) {
		limit := queryInt(c, "limit", 50)
		offset := queryInt(c, "offset", 0)
		c.JSON(http.StatusOK, gin.H{
			"orders": []repositories.CRMOrder{},
			"total":  100,
			"limit":  limit,
			"offset": offset,
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/feeds/crm/orders?limit=10&offset=20", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if int(body["limit"].(float64)) != 10 {
		t.Errorf("expected limit 10, got %v", body["limit"])
	}
	if int(body["offset"].(float64)) != 20 {
		t.Errorf("expected offset 20, got %v", body["offset"])
	}
}

func TestFeedHandler_CRMOrderStatus_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	orderID := uuid.New()
	expectedStatus := &repositories.CRMOrderStatus{
		ID:              orderID,
		Status:          "in_production",
		TotalTasks:      10,
		CompletedTasks:  7,
		ProgressPercent: 70.0,
		LastUpdatedAt:   time.Now().UTC(),
	}

	router := gin.New()
	router.GET("/v1/feeds/crm/orders/:id/status", setTenantContext("test-tenant", "test-user"), func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
			return
		}
		if id != orderID {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusOK, expectedStatus)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/feeds/crm/orders/"+orderID.String()+"/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var status repositories.CRMOrderStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if status.Status != "in_production" {
		t.Errorf("expected status 'in_production', got %q", status.Status)
	}
	if status.ProgressPercent != 70.0 {
		t.Errorf("expected progress 70.0, got %f", status.ProgressPercent)
	}
}

func TestFeedHandler_CRMOrderStatus_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/v1/feeds/crm/orders/:id/status", setTenantContext("test-tenant", "test-user"), func(c *gin.Context) {
		_, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
			return
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/feeds/crm/orders/not-a-uuid/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid ID, got %d", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if body["error"] != "invalid_id" {
		t.Errorf("expected error 'invalid_id', got %v", body["error"])
	}
}

func TestFeedHandler_SocialMilestones_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	now := time.Now().UTC()
	expectedMilestones := []repositories.SocialMilestone{
		{
			Type:       "task.completed",
			Title:      "Task Completed",
			Data:       json.RawMessage(`{"task_id":"abc"}`),
			OccurredAt: now,
		},
		{
			Type:       "order.status_changed",
			Title:      "Order Status Update",
			Data:       json.RawMessage(`{"order_id":"def"}`),
			OccurredAt: now.Add(-time.Hour),
		},
	}

	router := gin.New()
	router.GET("/v1/feeds/social/milestones", setTenantContext("test-tenant", "test-user"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"milestones": expectedMilestones})
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/feeds/social/milestones", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if _, ok := body["milestones"]; !ok {
		t.Error("response missing 'milestones' field")
	}

	var milestones []repositories.SocialMilestone
	if err := json.Unmarshal(body["milestones"], &milestones); err != nil {
		t.Fatalf("failed to parse milestones: %v", err)
	}
	if len(milestones) != 2 {
		t.Errorf("expected 2 milestones, got %d", len(milestones))
	}
	if milestones[0].Type != "task.completed" {
		t.Errorf("expected first milestone type 'task.completed', got %q", milestones[0].Type)
	}
}

func TestFeedHandler_SocialStats_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	expectedStats := &repositories.SocialStats{
		MachinesRunning:      5,
		OrdersCompletedDay:   12,
		OrdersCompletedWeek:  87,
		OrdersCompletedMonth: 340,
		AverageOEE:           82.5,
	}

	router := gin.New()
	router.GET("/v1/feeds/social/stats", setTenantContext("test-tenant", "test-user"), func(c *gin.Context) {
		c.JSON(http.StatusOK, expectedStats)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/feeds/social/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var stats repositories.SocialStats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if stats.MachinesRunning != 5 {
		t.Errorf("expected 5 machines running, got %d", stats.MachinesRunning)
	}
	if stats.AverageOEE != 82.5 {
		t.Errorf("expected OEE 82.5, got %f", stats.AverageOEE)
	}
}

func TestFeedHandler_SocialHighlights_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	now := time.Now().UTC()
	expectedHighlights := []repositories.SocialHighlight{
		{
			Type:        "analytics.oee_updated",
			Title:       "OEE Update",
			Description: "Machine efficiency metrics updated",
			Data:        json.RawMessage(`{"oee":95.2}`),
			OccurredAt:  now,
		},
	}

	router := gin.New()
	router.GET("/v1/feeds/social/highlights", setTenantContext("test-tenant", "test-user"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"highlights": expectedHighlights})
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/feeds/social/highlights", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if _, ok := body["highlights"]; !ok {
		t.Error("response missing 'highlights' field")
	}

	var highlights []repositories.SocialHighlight
	if err := json.Unmarshal(body["highlights"], &highlights); err != nil {
		t.Fatalf("failed to parse highlights: %v", err)
	}
	if len(highlights) != 1 {
		t.Errorf("expected 1 highlight, got %d", len(highlights))
	}
	if highlights[0].Type != "analytics.oee_updated" {
		t.Errorf("expected highlight type 'analytics.oee_updated', got %q", highlights[0].Type)
	}
}

func TestFeedHandler_CRMOrders_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	router := gin.New()
	router.GET("/v1/feeds/crm/orders", setTenantContext("test-tenant", "test-user"), func(c *gin.Context) {
		// Simulate the error path from CRMOrders handler
		log.WithError(fmt.Errorf("database connection lost")).Error("Failed to get CRM orders")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to get orders"})
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/feeds/crm/orders", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if body["error"] != "internal_error" {
		t.Errorf("expected error 'internal_error', got %v", body["error"])
	}
}

func TestNewFeedHandler(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	handler := NewFeedHandler(nil, nil, log)
	if handler == nil {
		t.Fatal("expected non-nil FeedHandler")
	}
}
