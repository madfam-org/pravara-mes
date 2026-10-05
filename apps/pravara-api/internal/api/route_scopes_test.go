package api

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
)

// registeredV1Routes registers every route (all optional deps wired) and
// returns the /v1 route keys.
func registeredV1Routes(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for _, r := range registeredRouter(t).Routes() {
		if strings.HasPrefix(r.Path, "/v1/") {
			keys[middleware.RouteKey(r.Method, r.Path)] = true
		}
	}
	return keys
}

// registeredRouter registers every route with all optional deps wired.
func registeredRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	cfg := &config.Config{}
	sqlDB, err := sql.Open("postgres", "postgres://unused@127.0.0.1:1/unused?sslmode=disable")
	require.NoError(t, err) // sql.Open does not connect
	t.Cleanup(func() { _ = sqlDB.Close() })
	database := &db.DB{DB: sqlDB}
	deps := RoutesDeps{
		OutboxRepo:  &repositories.OutboxRepository{},
		WebhookRepo: &repositories.WebhookRepository{},
		APIKeyRepo:  &repositories.APIKeyRepository{},
		FeedRepo:    &repositories.FeedRepository{},
	}
	deps.RedisClient = redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}) // never dialled
	t.Cleanup(func() { _ = deps.RedisClient.Close() })
	deps.StatusDB = sqlDB
	RegisterRoutesAll(router, database, cfg, log, &pubsub.Publisher{}, nil, deps)
	return router
}

func TestMachineRouteScopesMatchRegisteredRoutes(t *testing.T) {
	routes := registeredV1Routes(t)
	require.NotEmpty(t, routes)
	for key := range MachineRouteScopes() {
		assert.True(t, routes[key], "matrix entry %q matches no registered route", key)
	}
}

func TestMachineRouteScopesUseOnlyKnownScopes(t *testing.T) {
	known := map[string]bool{}
	for _, s := range middleware.MachineScopes {
		known[s] = true
	}
	for key, req := range MachineRouteScopes() {
		require.NotEmpty(t, req.AnyOf, key)
		for _, s := range req.AnyOf {
			assert.True(t, known[s], "%s: unknown machine scope %q", key, s)
		}
	}
}

func TestMachineRouteScopesKeepCommandAndAdminRoutesHumanOnly(t *testing.T) {
	policy := MachineRouteScopes()
	for _, key := range []string{
		"POST /v1/machines/:id/command",
		"DELETE /v1/machines/:id",
		"POST /v1/api-keys",
		"GET /v1/admin/billing/tenants/:id/usage",
		"DELETE /v1/orders/:id",
	} {
		_, listed := policy[key]
		assert.False(t, listed, "%s must not accept machine credentials", key)
	}
}

// TestNonJWTRoutesAreExactlyTheUnauthenticatedRoutes sends every registered
// /v1 route a request without credentials: anything that does not answer 401
// must be listed in NonJWTRoutes (public), and every public NonJWTRoutes entry
// must be registered. Internal entries must be served only by the internal
// router.
func TestNonJWTRoutesAreExactlyTheUnauthenticatedRoutes(t *testing.T) {
	router := registeredRouter(t)
	listed := NonJWTRoutes()
	seen := map[string]bool{}
	for _, r := range router.Routes() {
		if !strings.HasPrefix(r.Path, "/v1/") {
			continue
		}
		key := middleware.RouteKey(r.Method, r.Path)
		seen[key] = true
		if entry, ok := listed[key]; ok {
			assert.Equal(t, "public", entry.Listener, "%s is internal but registered on the public router", key)
			continue
		}
		path := strings.NewReplacer(":id", "00000000-0000-0000-0000-000000000001", ":wiId", "00000000-0000-0000-0000-000000000002",
			":itemId", "00000000-0000-0000-0000-000000000003").Replace(r.Path)
		req := httptest.NewRequest(r.Method, path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s answered without credentials; list it in NonJWTRoutes or put it behind auth", key)
	}

	internal := NewInternalRouter(&db.DB{}, &config.Config{}, logrus.New())
	internalRoutes := map[string]bool{}
	for _, r := range internal.Routes() {
		internalRoutes[middleware.RouteKey(r.Method, r.Path)] = true
	}
	for key, entry := range listed {
		switch entry.Listener {
		case "public":
			assert.True(t, seen[key], "NonJWTRoutes entry %q matches no public route", key)
		case "internal":
			assert.True(t, internalRoutes[key], "NonJWTRoutes entry %q is not served by the internal router", key)
			assert.False(t, seen[key], "%s must not be reachable on the public router", key)
		default:
			t.Errorf("%s: unknown listener %q", key, entry.Listener)
		}
		assert.NotEmpty(t, entry.Guard, key)
		_, inMatrix := MachineRouteScopes()[key]
		assert.False(t, inMatrix, "%s takes no JWT; it cannot be in the machine scope matrix", key)
	}
}

func TestHumanOnlyRoutesAreRegisteredAndNotInTheMachineMatrix(t *testing.T) {
	routes := registeredV1Routes(t)
	for _, key := range HumanOnlyRoutes() {
		assert.True(t, routes[key], "human-only route %q is not registered", key)
		_, listed := MachineRouteScopes()[key]
		assert.False(t, listed, "%s must not accept machine credentials", key)
	}
}

func TestInternalBrokerRoutesRefuseWithoutTheKey(t *testing.T) {
	cfg := &config.Config{}
	cfg.Edge.MQTTAuthInternalKey = "internal-test-key"
	internal := NewInternalRouter(&db.DB{}, cfg, logrus.New())
	for _, path := range []string{"/v1/mqtt/auth", "/v1/mqtt/acl"} {
		w := httptest.NewRecorder()
		internal.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"username":"u"}`)))
		assert.Equal(t, http.StatusUnauthorized, w.Code, path)
	}
	unset := NewInternalRouter(&db.DB{}, &config.Config{}, logrus.New())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/mqtt/auth", strings.NewReader(`{"username":"u"}`))
	req.Header.Set("X-Pravara-Internal-Key", "")
	unset.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
