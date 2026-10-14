package api

import (
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
)

func TestDispatchRoutesAreRegisteredAndDispatchIsHumanOnly(t *testing.T) {
	routes := registeredV1Routes(t)
	for _, key := range []string{"POST /v1/match", "POST /v1/dispatches", "GET /v1/dispatches", "GET /v1/dispatches/:id"} {
		assert.True(t, routes[key], key)
	}
	_, listed := MachineRouteScopes()["POST /v1/dispatches"]
	assert.False(t, listed, "dispatch ends in start_job: no machine scope may reach it")
}

// With dispatch disabled (the default), POST /v1/dispatches answers 503 with
// the reason before touching the database.
func TestDispatchDisabledAnswers503(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": testKid, "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	defer jwks.Close()
	cfg := &config.Config{}
	cfg.OIDC.Issuer, cfg.OIDC.JWKSURL, cfg.OIDC.Audience = testIssuer, jwks.URL, testAudience
	sqlDB, err := sql.Open("postgres", "postgres://unused@127.0.0.1:1/unused?sslmode=disable")
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	RegisterRoutesAll(router, &db.DB{DB: sqlDB}, cfg, log, nil, nil, RoutesDeps{})
	rig := &scopeRig{router: router, key: key, tenantID: uuid.New()}

	w := rig.do(http.MethodPost, "/v1/dispatches", rig.userToken(t, "operator"), map[string]any{"task_id": uuid.New()})
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "DISPATCH_ENABLED")
}
