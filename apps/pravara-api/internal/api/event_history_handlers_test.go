package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

func setupEventHistoryTest(t *testing.T) (*EventHistoryHandler, sqlmock.Sqlmock, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	repo := repositories.NewOutboxRepository(db)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	handler := NewEventHistoryHandler(repo, log)

	router := gin.New()
	return handler, mock, router
}

func TestEventHistoryHandler_ListEvents_DefaultPagination(t *testing.T) {
	handler, mock, router := setupEventHistoryTest(t)

	router.GET("/v1/events", handler.ListEvents)

	eventID := uuid.New()
	tenantID := uuid.New()
	now := time.Now()

	// Count query
	mock.ExpectQuery("SELECT COUNT").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	// Data query
	mock.ExpectQuery("SELECT .+ FROM event_outbox").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
		}).AddRow(
			eventID, tenantID, "order.created", "orders",
			json.RawMessage(`{"order_id":"123"}`), false, now,
		))

	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, float64(1), resp["total"])
	assert.Equal(t, float64(50), resp["limit"])
	assert.Equal(t, float64(0), resp["offset"])

	events, ok := resp["events"].([]interface{})
	require.True(t, ok)
	assert.Len(t, events, 1)

	firstEvent := events[0].(map[string]interface{})
	assert.Equal(t, "order.created", firstEvent["event_type"])
	assert.Equal(t, "orders", firstEvent["channel_namespace"])

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventHistoryHandler_ListEvents_FilterByType(t *testing.T) {
	handler, mock, router := setupEventHistoryTest(t)

	router.GET("/v1/events", handler.ListEvents)

	eventID := uuid.New()
	tenantID := uuid.New()
	now := time.Now()

	// Count query with type filter
	mock.ExpectQuery("SELECT COUNT").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	// Data query with type filter
	mock.ExpectQuery("SELECT .+ FROM event_outbox").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
		}).AddRow(
			eventID, tenantID, "order.created", "orders",
			json.RawMessage(`{"order_id":"123"}`), true, now,
		))

	req := httptest.NewRequest(http.MethodGet, "/v1/events?type=order.created", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(1), resp["total"])

	events := resp["events"].([]interface{})
	assert.Len(t, events, 1)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventHistoryHandler_ListEvents_EmptyResult(t *testing.T) {
	handler, mock, router := setupEventHistoryTest(t)

	router.GET("/v1/events", handler.ListEvents)

	// Count returns 0
	mock.ExpectQuery("SELECT COUNT").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	// Data query returns empty
	mock.ExpectQuery("SELECT .+ FROM event_outbox").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
		}))

	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["total"])
	assert.Nil(t, resp["events"], "events should be null/nil when no results")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventHistoryHandler_GetEventByID_Success(t *testing.T) {
	handler, mock, router := setupEventHistoryTest(t)

	router.GET("/v1/events/:id", handler.GetEventByID)

	eventID := uuid.New()
	tenantID := uuid.New()
	now := time.Now()

	mock.ExpectQuery("SELECT .+ FROM event_outbox WHERE id").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
		}).AddRow(
			eventID, tenantID, "order.updated", "orders",
			json.RawMessage(`{"order_id":"456","status":"confirmed"}`), true, now,
		))

	req := httptest.NewRequest(http.MethodGet, "/v1/events/"+eventID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp repositories.OutboxEvent
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, eventID, resp.ID)
	assert.Equal(t, "order.updated", resp.EventType)
	assert.Equal(t, "orders", resp.ChannelNamespace)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventHistoryHandler_GetEventByID_InvalidUUID(t *testing.T) {
	handler, _, router := setupEventHistoryTest(t)

	router.GET("/v1/events/:id", handler.GetEventByID)

	req := httptest.NewRequest(http.MethodGet, "/v1/events/not-valid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_id", resp["error"])
}

func TestEventHistoryHandler_GetEventByID_NotFound(t *testing.T) {
	handler, mock, router := setupEventHistoryTest(t)

	router.GET("/v1/events/:id", handler.GetEventByID)

	eventID := uuid.New()

	mock.ExpectQuery("SELECT .+ FROM event_outbox WHERE id").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
		})) // no rows -> sql.ErrNoRows on Scan

	req := httptest.NewRequest(http.MethodGet, "/v1/events/"+eventID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "not_found", resp["error"])

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventHistoryHandler_GetEventTypes_Success(t *testing.T) {
	handler, mock, router := setupEventHistoryTest(t)

	router.GET("/v1/events/types", handler.GetEventTypes)

	mock.ExpectQuery("SELECT event_type").
		WillReturnRows(sqlmock.NewRows([]string{"event_type", "count"}).
			AddRow("order.created", 42).
			AddRow("order.updated", 15).
			AddRow("order.deleted", 3))

	req := httptest.NewRequest(http.MethodGet, "/v1/events/types", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	eventTypes, ok := resp["event_types"].([]interface{})
	require.True(t, ok)
	assert.Len(t, eventTypes, 3)

	first := eventTypes[0].(map[string]interface{})
	assert.Equal(t, "order.created", first["event_type"])
	assert.Equal(t, float64(42), first["count"])

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventHistoryHandler_GetEventTypes_Empty(t *testing.T) {
	handler, mock, router := setupEventHistoryTest(t)

	router.GET("/v1/events/types", handler.GetEventTypes)

	mock.ExpectQuery("SELECT event_type").
		WillReturnRows(sqlmock.NewRows([]string{"event_type", "count"}))

	req := httptest.NewRequest(http.MethodGet, "/v1/events/types", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp["event_types"])

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventHistoryHandler_ListEvents_CustomPagination(t *testing.T) {
	handler, mock, router := setupEventHistoryTest(t)

	router.GET("/v1/events", handler.ListEvents)

	// Count query
	mock.ExpectQuery("SELECT COUNT").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))

	// Data query
	mock.ExpectQuery("SELECT .+ FROM event_outbox").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "event_type", "channel_namespace", "payload", "delivered", "created_at",
		}))

	req := httptest.NewRequest(http.MethodGet, "/v1/events?limit=10&offset=20", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(100), resp["total"])
	assert.Equal(t, float64(10), resp["limit"])
	assert.Equal(t, float64(20), resp["offset"])

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestQueryInt_DefaultValues(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		query    string
		key      string
		defVal   int
		expected int
	}{
		{"returns default when key missing", "/test", "limit", 50, 50},
		{"returns parsed value when present", "/test?limit=25", "limit", 50, 25},
		{"returns default for non-numeric value", "/test?limit=abc", "limit", 50, 50},
		{"returns zero when explicitly set", "/test?offset=0", "offset", 10, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			var result int

			router.GET("/test", func(c *gin.Context) {
				result = queryInt(c, tt.key, tt.defVal)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, tt.query, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expected, result)
		})
	}
}
