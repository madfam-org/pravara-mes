package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestNewStatusHandler(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	handler := NewStatusHandler(nil, log)
	if handler == nil {
		t.Fatal("expected non-nil StatusHandler")
	}
}

func TestStatusHandler_Status_Operational(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	handler := NewStatusHandler(db, log)

	// Mock getLatestComponentStatus query
	componentRows := sqlmock.NewRows([]string{"component", "status"}).
		AddRow("database", "operational").
		AddRow("redis", "operational").
		AddRow("api", "operational")
	mock.ExpectQuery("SELECT DISTINCT ON \\(component\\) component, status FROM health_snapshots").
		WillReturnRows(componentRows)

	// Mock computeUptime queries (3 calls for 24h, 7d, 30d)
	for i := 0; i < 3; i++ {
		uptimeRows := sqlmock.NewRows([]string{"total", "operational"}).
			AddRow(100, 99)
		mock.ExpectQuery("SELECT COUNT\\(\\*\\), COUNT\\(\\*\\) FILTER").
			WillReturnRows(uptimeRows)
	}

	router := gin.New()
	router.GET("/status", handler.Status)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp StatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Status != "operational" {
		t.Errorf("expected overall status 'operational', got %q", resp.Status)
	}
	if len(resp.Components) != 3 {
		t.Errorf("expected 3 components, got %d", len(resp.Components))
	}
	if resp.UpdatedAt.IsZero() {
		t.Error("expected non-zero UpdatedAt timestamp")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled sqlmock expectations: %v", err)
	}
}

func TestStatusHandler_Status_Degraded(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	handler := NewStatusHandler(db, log)

	// One component degraded
	componentRows := sqlmock.NewRows([]string{"component", "status"}).
		AddRow("database", "operational").
		AddRow("redis", "degraded").
		AddRow("api", "operational")
	mock.ExpectQuery("SELECT DISTINCT ON \\(component\\) component, status FROM health_snapshots").
		WillReturnRows(componentRows)

	for i := 0; i < 3; i++ {
		uptimeRows := sqlmock.NewRows([]string{"total", "operational"}).
			AddRow(100, 95)
		mock.ExpectQuery("SELECT COUNT\\(\\*\\), COUNT\\(\\*\\) FILTER").
			WillReturnRows(uptimeRows)
	}

	router := gin.New()
	router.GET("/status", handler.Status)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp StatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Status != "degraded" {
		t.Errorf("expected overall status 'degraded', got %q", resp.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled sqlmock expectations: %v", err)
	}
}

func TestStatusHandler_Status_Outage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	handler := NewStatusHandler(db, log)

	componentRows := sqlmock.NewRows([]string{"component", "status"}).
		AddRow("database", "outage").
		AddRow("redis", "operational")
	mock.ExpectQuery("SELECT DISTINCT ON \\(component\\) component, status FROM health_snapshots").
		WillReturnRows(componentRows)

	for i := 0; i < 3; i++ {
		uptimeRows := sqlmock.NewRows([]string{"total", "operational"}).
			AddRow(100, 50)
		mock.ExpectQuery("SELECT COUNT\\(\\*\\), COUNT\\(\\*\\) FILTER").
			WillReturnRows(uptimeRows)
	}

	router := gin.New()
	router.GET("/status", handler.Status)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp StatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Status != "outage" {
		t.Errorf("expected overall status 'outage', got %q", resp.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled sqlmock expectations: %v", err)
	}
}

func TestStatusHandler_Status_DatabaseError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	handler := NewStatusHandler(db, log)

	// Simulate database error
	mock.ExpectQuery("SELECT DISTINCT ON \\(component\\) component, status FROM health_snapshots").
		WillReturnError(fmt.Errorf("connection refused"))

	router := gin.New()
	router.GET("/status", handler.Status)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 on DB error, got %d", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if body["status"] != "unknown" {
		t.Errorf("expected status 'unknown', got %v", body["status"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled sqlmock expectations: %v", err)
	}
}

func TestStatusHandler_StatusHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	handler := NewStatusHandler(db, log)

	day1 := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
	historyRows := sqlmock.NewRows([]string{"day", "component", "total_checks", "operational_checks"}).
		AddRow(day1, "database", 288, 288).
		AddRow(day1, "redis", 288, 280).
		AddRow(day2, "database", 288, 286)
	mock.ExpectQuery("SELECT").
		WillReturnRows(historyRows)

	router := gin.New()
	router.GET("/status/history", handler.StatusHistory)

	req := httptest.NewRequest(http.MethodGet, "/status/history", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if _, ok := body["history"]; !ok {
		t.Error("response missing 'history' field")
	}

	var history []map[string]interface{}
	if err := json.Unmarshal(body["history"], &history); err != nil {
		t.Fatalf("failed to parse history: %v", err)
	}
	if len(history) != 3 {
		t.Errorf("expected 3 history entries, got %d", len(history))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled sqlmock expectations: %v", err)
	}
}

func TestStatusHandler_StatusHistory_DatabaseError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	handler := NewStatusHandler(db, log)

	mock.ExpectQuery("SELECT").
		WillReturnError(fmt.Errorf("connection refused"))

	router := gin.New()
	router.GET("/status/history", handler.StatusHistory)

	req := httptest.NewRequest(http.MethodGet, "/status/history", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 on DB error, got %d", w.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled sqlmock expectations: %v", err)
	}
}

func TestStatusHandler_Status_NoAuthRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	handler := NewStatusHandler(db, log)

	// Mock queries - the status endpoint should work without any auth middleware
	componentRows := sqlmock.NewRows([]string{"component", "status"}).
		AddRow("api", "operational")
	mock.ExpectQuery("SELECT DISTINCT ON \\(component\\) component, status FROM health_snapshots").
		WillReturnRows(componentRows)

	for i := 0; i < 3; i++ {
		uptimeRows := sqlmock.NewRows([]string{"total", "operational"}).
			AddRow(100, 100)
		mock.ExpectQuery("SELECT COUNT\\(\\*\\), COUNT\\(\\*\\) FILTER").
			WillReturnRows(uptimeRows)
	}

	// Register without any auth middleware
	router := gin.New()
	router.GET("/status", handler.Status)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	// No Authorization header
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should succeed without auth
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 without auth, got %d: %s", w.Code, w.Body.String())
	}

	var resp StatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Status != "operational" {
		t.Errorf("expected 'operational', got %q", resp.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled sqlmock expectations: %v", err)
	}
}

func TestStatusHandler_UptimeStats_Calculation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	handler := NewStatusHandler(db, log)

	componentRows := sqlmock.NewRows([]string{"component", "status"}).
		AddRow("api", "operational")
	mock.ExpectQuery("SELECT DISTINCT ON \\(component\\) component, status FROM health_snapshots").
		WillReturnRows(componentRows)

	// 24h: 99/100 = 99%
	mock.ExpectQuery("SELECT COUNT\\(\\*\\), COUNT\\(\\*\\) FILTER").
		WillReturnRows(sqlmock.NewRows([]string{"total", "operational"}).AddRow(100, 99))
	// 7d: 690/700 = 98.57%
	mock.ExpectQuery("SELECT COUNT\\(\\*\\), COUNT\\(\\*\\) FILTER").
		WillReturnRows(sqlmock.NewRows([]string{"total", "operational"}).AddRow(700, 690))
	// 30d: 2900/3000 = 96.67%
	mock.ExpectQuery("SELECT COUNT\\(\\*\\), COUNT\\(\\*\\) FILTER").
		WillReturnRows(sqlmock.NewRows([]string{"total", "operational"}).AddRow(3000, 2900))

	router := gin.New()
	router.GET("/status", handler.Status)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp StatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Uptime.Last24h != 99.0 {
		t.Errorf("expected 24h uptime 99.0, got %f", resp.Uptime.Last24h)
	}

	expectedWeek := float64(690) / float64(700) * 100
	if resp.Uptime.Last7d != expectedWeek {
		t.Errorf("expected 7d uptime %f, got %f", expectedWeek, resp.Uptime.Last7d)
	}

	expectedMonth := float64(2900) / float64(3000) * 100
	if resp.Uptime.Last30d != expectedMonth {
		t.Errorf("expected 30d uptime %f, got %f", expectedMonth, resp.Uptime.Last30d)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled sqlmock expectations: %v", err)
	}
}

func TestComponentStatus_JSONSerialization(t *testing.T) {
	uptime := 99.5
	cs := ComponentStatus{
		Name:   "database",
		Status: "operational",
		Uptime: &uptime,
	}

	data, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("failed to marshal ComponentStatus: %v", err)
	}

	var decoded ComponentStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ComponentStatus: %v", err)
	}

	if decoded.Name != cs.Name {
		t.Errorf("Name mismatch: got %q, want %q", decoded.Name, cs.Name)
	}
	if decoded.Status != cs.Status {
		t.Errorf("Status mismatch: got %q, want %q", decoded.Status, cs.Status)
	}
	if decoded.Uptime == nil || *decoded.Uptime != *cs.Uptime {
		t.Errorf("Uptime mismatch: got %v, want %v", decoded.Uptime, cs.Uptime)
	}
}

func TestUptimeStats_JSONSerialization(t *testing.T) {
	stats := UptimeStats{
		Last24h: 99.9,
		Last7d:  99.5,
		Last30d: 99.0,
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("failed to marshal UptimeStats: %v", err)
	}

	var decoded UptimeStats
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal UptimeStats: %v", err)
	}

	if decoded.Last24h != stats.Last24h {
		t.Errorf("Last24h mismatch: got %f, want %f", decoded.Last24h, stats.Last24h)
	}
	if decoded.Last7d != stats.Last7d {
		t.Errorf("Last7d mismatch: got %f, want %f", decoded.Last7d, stats.Last7d)
	}
	if decoded.Last30d != stats.Last30d {
		t.Errorf("Last30d mismatch: got %f, want %f", decoded.Last30d, stats.Last30d)
	}
}
