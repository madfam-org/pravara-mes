package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
)

type fakeSchema struct{ status db.SchemaStatus }

func (f fakeSchema) Status(context.Context) db.SchemaStatus { return f.status }

// healthRouter serves the health endpoints over a database that answers
// pings, with the given schema state.
func healthRouter(t *testing.T, schema schemaChecker, enforce bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	sqlDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	mock.MatchExpectationsInOrder(false)
	for i := 0; i < 4; i++ {
		mock.ExpectPing()
	}

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	handler := NewHealthHandler(&db.DB{DB: sqlDB}, log)
	if schema != nil {
		handler.WithSchemaGuard(schema, enforce)
	}
	router := gin.New()
	router.GET("/health", handler.Health)
	router.GET("/health/live", handler.Liveness)
	router.GET("/health/ready", handler.Readiness)
	return router
}

func getJSON(t *testing.T, router *gin.Engine, path string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return w.Code, body
}

var (
	schemaCurrent    = db.SchemaStatus{}
	schemaBehind     = db.SchemaStatus{Missing: []string{"035_inventory", "036_corrections"}}
	schemaUnverified = db.SchemaStatus{Err: errors.New("read schema_migrations: permission denied")}
)

func TestReadinessWithSchemaGuard(t *testing.T) {
	cases := []struct {
		name       string
		schema     schemaChecker
		enforce    bool
		wantCode   int
		wantReason string
	}{
		{"no guard", nil, false, http.StatusOK, ""},
		{"current, enforced", fakeSchema{schemaCurrent}, true, http.StatusOK, ""},
		{"behind, enforced", fakeSchema{schemaBehind}, true, http.StatusServiceUnavailable, "schema_behind"},
		{"unverified, enforced", fakeSchema{schemaUnverified}, true, http.StatusServiceUnavailable, "schema_unverified"},
		{"behind, warn only", fakeSchema{schemaBehind}, false, http.StatusOK, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := getJSON(t, healthRouter(t, tc.schema, tc.enforce), "/health/ready")
			assert.Equal(t, tc.wantCode, code)
			if tc.wantReason == "" {
				assert.Equal(t, "ready", body["status"])
				return
			}
			assert.Equal(t, "not_ready", body["status"])
			assert.Equal(t, tc.wantReason, body["reason"])
		})
	}
}

// Liveness never depends on the schema: a pod that is waiting for a
// migration must stay alive so it can become ready when it is applied.
func TestLivenessIgnoresTheSchema(t *testing.T) {
	code, body := getJSON(t, healthRouter(t, fakeSchema{schemaBehind}, true), "/health/live")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "alive", body["status"])
}

func TestHealthReportsTheSchema(t *testing.T) {
	t.Run("behind and enforced is degraded", func(t *testing.T) {
		code, body := getJSON(t, healthRouter(t, fakeSchema{schemaBehind}, true), "/health")
		assert.Equal(t, http.StatusServiceUnavailable, code)
		assert.Equal(t, "degraded", body["status"])
		schema := body["components"].(map[string]any)["schema"].(map[string]any)
		assert.Equal(t, "behind", schema["status"])
		details := schema["details"].(map[string]any)
		assert.Equal(t, "schema_behind", details["reason"])
		assert.EqualValues(t, 2, details["missing_migrations"])
	})

	t.Run("behind in warn mode is reported but not degraded", func(t *testing.T) {
		code, body := getJSON(t, healthRouter(t, fakeSchema{schemaBehind}, false), "/health")
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, "ok", body["status"])
		schema := body["components"].(map[string]any)["schema"].(map[string]any)
		assert.Equal(t, "behind", schema["status"])
	})

	t.Run("current", func(t *testing.T) {
		code, body := getJSON(t, healthRouter(t, fakeSchema{schemaCurrent}, true), "/health")
		assert.Equal(t, http.StatusOK, code)
		schema := body["components"].(map[string]any)["schema"].(map[string]any)
		assert.Equal(t, "healthy", schema["status"])
	})

	t.Run("no guard, no schema component", func(t *testing.T) {
		_, body := getJSON(t, healthRouter(t, nil, false), "/health")
		_, present := body["components"].(map[string]any)["schema"]
		assert.False(t, present)
	})
}

// The public endpoints say how many migrations are missing, never which.
func TestHealthEndpointsDoNotNameMigrations(t *testing.T) {
	router := healthRouter(t, fakeSchema{schemaBehind}, true)
	for _, path := range []string{"/health", "/health/ready"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		assert.NotContains(t, w.Body.String(), "035_inventory", path)
		assert.NotContains(t, w.Body.String(), "permission denied", path)
	}
}
