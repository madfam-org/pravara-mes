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

// inventoryScopeRouter serves the /v1/inventory route patterns behind the real
// machine route table, with caller standing in for authentication.
func inventoryScopeRouter(caller middleware.Caller) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyCaller), caller)
		c.Next()
	})
	router.Use(middleware.EnforceRouteScopes(MachineRouteScopes(), nil))
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	inventory := router.Group("/v1/inventory")
	inventory.GET("", ok)
	inventory.POST("", ok)
	inventory.GET("/low-stock", ok)
	inventory.GET("/:id", ok)
	inventory.PATCH("/:id", ok)
	inventory.POST("/:id/adjust", ok)
	return router
}

// TestMachineInventoryAccessIsReadOnly: machine callers holding
// pravara-mes:read may list and get inventory items; no machine scope opens an
// inventory write.
func TestMachineInventoryAccessIsReadOnly(t *testing.T) {
	routes := registeredV1Routes(t)
	for _, key := range []string{
		"GET /v1/inventory", "POST /v1/inventory", "GET /v1/inventory/low-stock",
		"GET /v1/inventory/:id", "PATCH /v1/inventory/:id", "POST /v1/inventory/:id/adjust",
	} {
		require.True(t, routes[key], "%s is not registered; update inventoryScopeRouter", key)
	}

	serve := func(caller middleware.Caller, method, path string) int {
		w := httptest.NewRecorder()
		inventoryScopeRouter(caller).ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w.Code
	}
	machine := func(scopes ...string) middleware.Caller {
		return middleware.Caller{Kind: middleware.CallerServiceAccount, ID: "jnc_inventory_test", Scopes: scopes}
	}
	apiKey := func(scopes ...string) middleware.Caller {
		return middleware.Caller{Kind: middleware.CallerAPIKey, ID: "00000000-0000-0000-0000-00000000000a", Scopes: scopes}
	}
	var namedKeyScopes []string // every API-key scope except the wildcard
	for _, s := range middleware.APIKeyScopes {
		if s != middleware.ScopeWildcard {
			namedKeyScopes = append(namedKeyScopes, s)
		}
	}

	const item = "/v1/inventory/00000000-0000-0000-0000-000000000001"
	reads := []string{"/v1/inventory?search=SKU-1&limit=100", item}
	writes := []struct{ method, path string }{
		{http.MethodPost, "/v1/inventory"},
		{http.MethodPatch, item},
		{http.MethodPost, item + "/adjust"},
	}

	for _, caller := range []middleware.Caller{machine(middleware.ScopeRead), apiKey(middleware.ScopeRead)} {
		for _, path := range reads {
			assert.Equal(t, http.StatusOK, serve(caller, http.MethodGet, path), "%s with pravara-mes:read: GET %s", caller.Kind, path)
		}
	}
	for _, caller := range []middleware.Caller{
		machine(middleware.ScopeRead), machine(middleware.MachineScopes...),
		apiKey(middleware.ScopeRead), apiKey(namedKeyScopes...),
	} {
		for _, wr := range writes {
			assert.Equal(t, http.StatusForbidden, serve(caller, wr.method, wr.path), "%s %v: %s %s", caller.Kind, caller.Scopes, wr.method, wr.path)
		}
	}
	// Only pravara-mes:read opens these reads, and only on these two routes.
	for _, caller := range []middleware.Caller{
		machine(), machine(middleware.ScopeJobs), machine(middleware.ScopeNodes), machine(middleware.ScopePassports),
	} {
		assert.Equal(t, http.StatusForbidden, serve(caller, http.MethodGet, "/v1/inventory"), "%v: GET /v1/inventory", caller.Scopes)
	}
	assert.Equal(t, http.StatusForbidden, serve(machine(middleware.ScopeRead), http.MethodGet, "/v1/inventory/low-stock"))
}
