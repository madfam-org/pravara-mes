package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

func setupWebhookTest(t *testing.T) (*WebhookSubscriptionHandler, sqlmock.Sqlmock, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	repo := repositories.NewWebhookRepository(db)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	handler := NewWebhookSubscriptionHandler(repo, log)

	router := gin.New()
	return handler, mock, router
}

func TestWebhookHandler_Create_Success(t *testing.T) {
	handler, mock, router := setupWebhookTest(t)
	tenantID := uuid.New().String()

	router.POST("/v1/webhooks/subscriptions", setTenantContext(tenantID, ""), handler.Create)

	now := time.Now()
	mock.ExpectQuery("INSERT INTO webhook_subscriptions").
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).
			AddRow(now, now))

	body, _ := json.Marshal(map[string]interface{}{
		"name":        "my-webhook",
		"url":         "https://example.com/webhook",
		"secret":      "super-secret",
		"event_types": []string{"order.created", "order.updated"},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/subscriptions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var resp repositories.WebhookSubscription
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "my-webhook", resp.Name)
	assert.Equal(t, "https://example.com/webhook", resp.URL)
	assert.True(t, resp.IsActive)
	assert.Equal(t, []string{"order.created", "order.updated"}, resp.EventTypes)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookHandler_Create_InvalidBody(t *testing.T) {
	handler, _, router := setupWebhookTest(t)
	tenantID := uuid.New().String()

	router.POST("/v1/webhooks/subscriptions", setTenantContext(tenantID, ""), handler.Create)

	tests := []struct {
		name string
		body map[string]interface{}
	}{
		{"missing name", map[string]interface{}{"url": "https://example.com/webhook", "secret": "secret", "event_types": []string{"order.created"}}},
		{"missing url", map[string]interface{}{"name": "test", "secret": "secret", "event_types": []string{"order.created"}}},
		{"missing secret", map[string]interface{}{"name": "test", "url": "https://example.com/webhook", "event_types": []string{"order.created"}}},
		{"missing event_types", map[string]interface{}{"name": "test", "url": "https://example.com/webhook", "secret": "secret"}},
		{"empty event_types", map[string]interface{}{"name": "test", "url": "https://example.com/webhook", "secret": "secret", "event_types": []string{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/subscriptions", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			var resp map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, "invalid_request", resp["error"])
		})
	}
}

func TestWebhookHandler_List_SecretsRedacted(t *testing.T) {
	handler, mock, router := setupWebhookTest(t)
	tenantID := uuid.New().String()

	router.GET("/v1/webhooks/subscriptions", setTenantContext(tenantID, ""), handler.List)

	subID1 := uuid.New()
	subID2 := uuid.New()
	tid := uuid.MustParse(tenantID)
	now := time.Now()

	mock.ExpectQuery("SELECT .+ FROM webhook_subscriptions").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "name", "url", "secret", "event_types",
			"is_active", "created_at", "updated_at",
		}).
			AddRow(subID1, tid, "hook-1", "https://a.com/hook", "real-secret-1",
				pq.Array([]string{"order.created"}), true, now, now).
			AddRow(subID2, tid, "hook-2", "https://b.com/hook", "real-secret-2",
				pq.Array([]string{"order.updated"}), true, now, now),
		)

	req := httptest.NewRequest(http.MethodGet, "/v1/webhooks/subscriptions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	subs, ok := resp["subscriptions"].([]interface{})
	require.True(t, ok)
	assert.Len(t, subs, 2)

	for _, sub := range subs {
		s := sub.(map[string]interface{})
		assert.Equal(t, "***", s["secret"], "secret should be redacted in list response")
	}

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookHandler_GetByID_Success(t *testing.T) {
	handler, mock, router := setupWebhookTest(t)
	tenantID := uuid.New().String()

	router.GET("/v1/webhooks/subscriptions/:id", setTenantContext(tenantID, ""), handler.GetByID)

	subID := uuid.New()
	tid := uuid.MustParse(tenantID)
	now := time.Now()

	mock.ExpectQuery("SELECT .+ FROM webhook_subscriptions WHERE id").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "name", "url", "secret", "event_types",
			"is_active", "created_at", "updated_at",
		}).AddRow(
			subID, tid, "hook-1", "https://a.com/hook", "real-secret",
			pq.Array([]string{"order.created"}), true, now, now,
		))

	req := httptest.NewRequest(http.MethodGet, "/v1/webhooks/subscriptions/"+subID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "hook-1", resp["name"])
	assert.Equal(t, "***", resp["secret"], "secret should be redacted")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookHandler_GetByID_InvalidUUID(t *testing.T) {
	handler, _, router := setupWebhookTest(t)
	tenantID := uuid.New().String()

	router.GET("/v1/webhooks/subscriptions/:id", setTenantContext(tenantID, ""), handler.GetByID)

	req := httptest.NewRequest(http.MethodGet, "/v1/webhooks/subscriptions/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWebhookHandler_GetByID_NotFound(t *testing.T) {
	handler, mock, router := setupWebhookTest(t)
	tenantID := uuid.New().String()

	router.GET("/v1/webhooks/subscriptions/:id", setTenantContext(tenantID, ""), handler.GetByID)

	subID := uuid.New()

	mock.ExpectQuery("SELECT .+ FROM webhook_subscriptions WHERE id").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "name", "url", "secret", "event_types",
			"is_active", "created_at", "updated_at",
		}))

	req := httptest.NewRequest(http.MethodGet, "/v1/webhooks/subscriptions/"+subID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookHandler_Update_Success(t *testing.T) {
	handler, mock, router := setupWebhookTest(t)
	tenantID := uuid.New().String()

	router.PATCH("/v1/webhooks/subscriptions/:id", setTenantContext(tenantID, ""), handler.Update)

	subID := uuid.New()
	tid := uuid.MustParse(tenantID)
	now := time.Now()

	// GetSubscriptionByID
	mock.ExpectQuery("SELECT .+ FROM webhook_subscriptions WHERE id").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "name", "url", "secret", "event_types",
			"is_active", "created_at", "updated_at",
		}).AddRow(
			subID, tid, "old-name", "https://old.com/hook", "old-secret",
			pq.Array([]string{"order.created"}), true, now, now,
		))

	// UpdateSubscription
	mock.ExpectExec("UPDATE webhook_subscriptions").
		WillReturnResult(sqlmock.NewResult(0, 1))

	body, _ := json.Marshal(map[string]interface{}{
		"name": "new-name",
	})
	req := httptest.NewRequest(http.MethodPatch, "/v1/webhooks/subscriptions/"+subID.String(), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "new-name", resp["name"])
	assert.Equal(t, "***", resp["secret"], "secret should be redacted")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookHandler_Delete_Success(t *testing.T) {
	handler, mock, router := setupWebhookTest(t)
	tenantID := uuid.New().String()

	router.DELETE("/v1/webhooks/subscriptions/:id", setTenantContext(tenantID, ""), handler.Delete)

	subID := uuid.New()

	mock.ExpectExec("DELETE FROM webhook_subscriptions").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodDelete, "/v1/webhooks/subscriptions/"+subID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookHandler_Delete_NotFound(t *testing.T) {
	handler, mock, router := setupWebhookTest(t)
	tenantID := uuid.New().String()

	router.DELETE("/v1/webhooks/subscriptions/:id", setTenantContext(tenantID, ""), handler.Delete)

	subID := uuid.New()

	mock.ExpectExec("DELETE FROM webhook_subscriptions").
		WillReturnResult(sqlmock.NewResult(0, 0))

	req := httptest.NewRequest(http.MethodDelete, "/v1/webhooks/subscriptions/"+subID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookHandler_Delete_InvalidUUID(t *testing.T) {
	handler, _, router := setupWebhookTest(t)
	tenantID := uuid.New().String()

	router.DELETE("/v1/webhooks/subscriptions/:id", setTenantContext(tenantID, ""), handler.Delete)

	req := httptest.NewRequest(http.MethodDelete, "/v1/webhooks/subscriptions/bad-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWebhookHandler_Create_NoTenantContext(t *testing.T) {
	handler, _, router := setupWebhookTest(t)

	router.POST("/v1/webhooks/subscriptions", handler.Create)

	body, _ := json.Marshal(map[string]interface{}{
		"name":        "test",
		"url":         "https://example.com/hook",
		"secret":      "s3cret",
		"event_types": []string{"order.created"},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/subscriptions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
