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
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

func setupAPIKeyTest(t *testing.T) (*APIKeyHandler, sqlmock.Sqlmock, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	repo := repositories.NewAPIKeyRepository(db)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	handler := NewAPIKeyHandler(repo, log)

	router := gin.New()
	return handler, mock, router
}

func setTenantContext(tenantID, userID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyTenantID), tenantID)
		c.Set(string(middleware.ContextKeyUserID), userID)
		c.Next()
	}
}

func TestAPIKeyHandler_Create_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	repo := repositories.NewAPIKeyRepository(db)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	handler := NewAPIKeyHandler(repo, log)
	router := gin.New()

	tenantID := uuid.New().String()
	userID := uuid.New().String()
	router.POST("/v1/api-keys", setTenantContext(tenantID, userID), handler.Create)

	now := time.Now()
	mock.ExpectQuery("INSERT INTO api_keys").
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).
			AddRow(now, now))

	body, _ := json.Marshal(map[string]interface{}{
		"name": "test-key",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Logf("Response body: %s", w.Body.String())
	}
	require.Equal(t, http.StatusCreated, w.Code)

	var resp createAPIKeyResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.NotEmpty(t, resp.Key)
	require.True(t, len(resp.Key) > 4, "key should be longer than prefix")
	assert.Equal(t, "prv_", resp.Key[:4], "key should start with prv_ prefix")
	assert.Equal(t, "test-key", resp.Name)
	assert.Equal(t, resp.Key[:12], resp.KeyPrefix, "key_prefix should be first 12 chars of raw key")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAPIKeyHandler_Create_InvalidBody(t *testing.T) {
	handler, _, router := setupAPIKeyTest(t)
	tenantID := uuid.New().String()
	userID := uuid.New().String()

	router.POST("/v1/api-keys", setTenantContext(tenantID, userID), handler.Create)

	// Missing required "name" field
	body, _ := json.Marshal(map[string]interface{}{
		"scopes": []string{"read:events"},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_request", resp["error"])
}

func TestAPIKeyHandler_Create_NoTenantContext(t *testing.T) {
	handler, _, router := setupAPIKeyTest(t)

	// No tenant context middleware
	router.POST("/v1/api-keys", handler.Create)

	body, _ := json.Marshal(map[string]interface{}{
		"name": "test-key",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPIKeyHandler_List_Success(t *testing.T) {
	handler, mock, router := setupAPIKeyTest(t)
	tenantID := uuid.New().String()
	userID := uuid.New().String()

	router.GET("/v1/api-keys", setTenantContext(tenantID, userID), handler.List)

	keyID := uuid.New()
	tid := uuid.MustParse(tenantID)
	createdBy := uuid.New()
	now := time.Now()

	mock.ExpectQuery("SELECT .+ FROM api_keys").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "name", "key_prefix", "scopes", "rate_limit",
			"is_active", "expires_at", "last_used_at", "created_by", "created_at", "updated_at",
		}).AddRow(
			keyID, tid, "my-key", "prv_abcd1234", pq.Array([]string{"read:events"}), 1000,
			true, nil, nil, createdBy.String(), now, now,
		))

	req := httptest.NewRequest(http.MethodGet, "/v1/api-keys", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	apiKeys, ok := resp["api_keys"].([]interface{})
	require.True(t, ok, "response should contain api_keys array")
	assert.Len(t, apiKeys, 1)

	firstKey := apiKeys[0].(map[string]interface{})
	assert.Equal(t, "my-key", firstKey["name"])
	assert.Equal(t, "prv_abcd1234", firstKey["key_prefix"])
	// Full key hash should NOT be present in JSON (json:"-" tag)
	_, hasKeyHash := firstKey["key_hash"]
	assert.False(t, hasKeyHash, "key_hash should not be exposed in list response")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAPIKeyHandler_Revoke_Success(t *testing.T) {
	handler, mock, router := setupAPIKeyTest(t)
	tenantID := uuid.New().String()
	userID := uuid.New().String()

	router.DELETE("/v1/api-keys/:id", setTenantContext(tenantID, userID), handler.Revoke)

	keyID := uuid.New()
	mock.ExpectExec("UPDATE api_keys SET is_active").
		WithArgs(keyID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodDelete, "/v1/api-keys/"+keyID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "API key revoked", resp["message"])

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAPIKeyHandler_Revoke_InvalidUUID(t *testing.T) {
	handler, _, router := setupAPIKeyTest(t)
	tenantID := uuid.New().String()
	userID := uuid.New().String()

	router.DELETE("/v1/api-keys/:id", setTenantContext(tenantID, userID), handler.Revoke)

	req := httptest.NewRequest(http.MethodDelete, "/v1/api-keys/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_id", resp["error"])
}

func TestAPIKeyHandler_Revoke_NotFound(t *testing.T) {
	handler, mock, router := setupAPIKeyTest(t)
	tenantID := uuid.New().String()
	userID := uuid.New().String()

	router.DELETE("/v1/api-keys/:id", setTenantContext(tenantID, userID), handler.Revoke)

	keyID := uuid.New()
	mock.ExpectExec("UPDATE api_keys SET is_active").
		WithArgs(keyID).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected -> ErrNotFound

	req := httptest.NewRequest(http.MethodDelete, "/v1/api-keys/"+keyID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "not_found", resp["error"])

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAPIKeyHandler_Create_WithCustomScopes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	repo := repositories.NewAPIKeyRepository(db)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	handler := NewAPIKeyHandler(repo, log)
	router := gin.New()

	tenantID := uuid.New().String()
	userID := uuid.New().String()
	router.POST("/v1/api-keys", setTenantContext(tenantID, userID), handler.Create)

	customScopes := []string{"read:orders", "write:orders"}
	rateLimit := 500

	now := time.Now()
	mock.ExpectQuery("INSERT INTO api_keys").
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).
			AddRow(now, now))

	body, _ := json.Marshal(map[string]interface{}{
		"name":            "custom-key",
		"scopes":          customScopes,
		"rate_limit":      rateLimit,
		"expires_in_days": 30,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)

	var resp createAPIKeyResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "custom-key", resp.Name)
	assert.Equal(t, rateLimit, resp.RateLimit)

	assert.NoError(t, mock.ExpectationsWereMet())
}
