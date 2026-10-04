package api

import (
	"database/sql"
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

	keys := map[string]bool{}
	for _, r := range router.Routes() {
		if strings.HasPrefix(r.Path, "/v1/") {
			keys[middleware.RouteKey(r.Method, r.Path)] = true
		}
	}
	return keys
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
