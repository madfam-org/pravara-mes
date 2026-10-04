package api

// End-to-end scope enforcement: real routes, real API-key and JWT middleware,
// a real JWKS (served by httptest; Janua is never called) and PostgreSQL.
//
// Opt-in: set PRAVARA_TEST_DATABASE_URL to a server where the role may CREATE
// DATABASE. The test creates a scratch database, applies every *.up.sql
// migration, and drops the database at the end. Without the variable it skips.

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

const (
	testIssuer   = "https://issuer.test"
	testAudience = "pravara-api"
	testKid      = "test-key-1"
)

type scopeRig struct {
	router   *gin.Engine
	key      *rsa.PrivateKey
	tenantID uuid.UUID
	sqlDB    *sql.DB
}

func newScratchDatabase(t *testing.T) *sql.DB {
	t.Helper()
	base := os.Getenv("PRAVARA_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("PRAVARA_TEST_DATABASE_URL not set; skipping PostgreSQL scope-enforcement test")
	}
	admin, err := sql.Open("postgres", base)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })

	name := fmt.Sprintf("pravara_scope_%d", time.Now().UnixNano())
	_, err = admin.Exec("CREATE DATABASE " + pq.QuoteIdentifier(name))
	require.NoError(t, err)

	u, err := url.Parse(base)
	require.NoError(t, err)
	u.Path = "/" + name
	scratch, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = scratch.Close()
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + pq.QuoteIdentifier(name) + " WITH (FORCE)")
	})

	files, err := filepath.Glob("../db/migrations/*.up.sql")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	sort.Strings(files)
	// infra/db/migrate.sh creates the tracking table first; 029 relies on it.
	_, err = scratch.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`)
	require.NoError(t, err)
	for _, f := range files {
		body, err := os.ReadFile(f)
		require.NoError(t, err)
		_, err = scratch.Exec(string(body))
		require.NoError(t, err, "migration %s", filepath.Base(f))
	}
	return scratch
}

func newScopeRig(t *testing.T) *scopeRig {
	t.Helper()
	sqlDB := newScratchDatabase(t)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": testKid, "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(jwks.Close)

	tenantID := uuid.New()
	_, err = sqlDB.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Scope test', $2)`, tenantID, "scope-"+tenantID.String()[:8])
	require.NoError(t, err)

	cfg := &config.Config{}
	cfg.OIDC.Issuer = testIssuer
	cfg.OIDC.JWKSURL = jwks.URL
	cfg.OIDC.Audience = testAudience

	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	RegisterRoutesAll(router, &db.DB{DB: sqlDB}, cfg, log, nil, nil, RoutesDeps{
		APIKeyRepo: repositories.NewAPIKeyRepository(sqlDB),
		OutboxRepo: repositories.NewOutboxRepository(sqlDB),
	})
	return &scopeRig{router: router, key: key, tenantID: tenantID, sqlDB: sqlDB}
}

func (r *scopeRig) sign(t *testing.T, claims jwt.MapClaims, key *rsa.PrivateKey) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = testKid
	s, err := tok.SignedString(key)
	require.NoError(t, err)
	return s
}

func (r *scopeRig) baseClaims(sub string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss": testIssuer, "aud": testAudience, "sub": sub,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"tenant_id": r.tenantID.String(),
	}
}

func (r *scopeRig) machineToken(t *testing.T, scope string, mutate func(jwt.MapClaims)) string {
	c := r.baseClaims("service-account:jnc_scope_test")
	c["client_id"] = "jnc_scope_test"
	c["token_use"] = "client_credentials"
	c["actor_type"] = "service_account"
	c["roles"] = []string{"service_account"}
	c["scope"] = scope
	if mutate != nil {
		mutate(c)
	}
	return r.sign(t, c, r.key)
}

func (r *scopeRig) userToken(t *testing.T, roles ...string) string {
	c := r.baseClaims(uuid.NewString())
	c["email"] = "person@example.test"
	c["roles"] = roles
	return r.sign(t, c, r.key)
}

func (r *scopeRig) apiKey(t *testing.T, scopes ...string) string {
	t.Helper()
	raw := "prv_" + strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	sum := sha256.Sum256([]byte(raw))
	_, err := r.sqlDB.Exec(`INSERT INTO api_keys (tenant_id, name, key_hash, key_prefix, scopes) VALUES ($1, $2, $3, $4, $5)`,
		r.tenantID, "scope-test", fmt.Sprintf("%x", sum), raw[:12], pq.Array(scopes))
	require.NoError(t, err)
	return raw
}

func (r *scopeRig) do(method, path, bearer string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	r.router.ServeHTTP(w, req)
	return w
}

func TestScopeEnforcementAgainstPostgres(t *testing.T) {
	rig := newScopeRig(t)
	order := map[string]any{"customer_name": "Scope test", "external_id": "scope-1"}

	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	cases := []struct {
		name   string
		method string
		path   string
		bearer string
		body   any
		want   int
	}{
		// Machine tokens (Janua client_credentials).
		{"machine jobs: order intake", http.MethodPost, "/v1/orders", rig.machineToken(t, "pravara-mes:jobs", nil), order, http.StatusCreated},
		{"machine jobs: read back orders", http.MethodGet, "/v1/orders", rig.machineToken(t, "pravara-mes:jobs", nil), nil, http.StatusOK},
		{"machine read: no intake", http.MethodPost, "/v1/orders", rig.machineToken(t, "pravara-mes:read", nil), order, http.StatusForbidden},
		{"machine without scope", http.MethodPost, "/v1/orders", rig.machineToken(t, "", nil), order, http.StatusForbidden},
		{"machine with unreserved wildcard", http.MethodPost, "/v1/orders", rig.machineToken(t, "*", nil), order, http.StatusForbidden},
		{"machine read: events", http.MethodGet, "/v1/events", rig.machineToken(t, "pravara-mes:read", nil), nil, http.StatusOK},
		{"machine legacy name on events", http.MethodGet, "/v1/events", rig.machineToken(t, "read:events", nil), nil, http.StatusForbidden},
		{"machine nodes: machine registry", http.MethodGet, "/v1/machines", rig.machineToken(t, "pravara-mes:nodes", nil), nil, http.StatusOK},
		{"machine all scopes: unlisted route", http.MethodGet, "/v1/tasks", rig.machineToken(t, "pravara-mes:jobs pravara-mes:nodes pravara-mes:passports pravara-mes:read", nil), nil, http.StatusForbidden},
		{"machine on admin route", http.MethodGet, "/v1/api-keys", rig.machineToken(t, "pravara-mes:read", nil), nil, http.StatusForbidden},
		{"machine scope as JSON array", http.MethodPost, "/v1/orders", rig.machineToken(t, "", func(c jwt.MapClaims) { c["scope"] = []string{"pravara-mes:jobs"} }), order, http.StatusCreated},
		{"machine token_use only (partial markers)", http.MethodGet, "/v1/tasks", rig.machineToken(t, "pravara-mes:read", func(c jwt.MapClaims) { delete(c, "actor_type"); c["sub"] = "x" }), nil, http.StatusForbidden},
		{"machine wrong audience", http.MethodPost, "/v1/orders", rig.machineToken(t, "pravara-mes:jobs", func(c jwt.MapClaims) { c["aud"] = "asset-shells-api" }), order, http.StatusUnauthorized},
		{"machine missing tenant_id", http.MethodPost, "/v1/orders", rig.machineToken(t, "pravara-mes:jobs", func(c jwt.MapClaims) { delete(c, "tenant_id") }), order, http.StatusForbidden},
		{"machine expired", http.MethodPost, "/v1/orders", rig.machineToken(t, "pravara-mes:jobs", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() }), order, http.StatusUnauthorized},
		{"machine foreign signing key", http.MethodPost, "/v1/orders", rig.sign(t, jwt.MapClaims{"iss": testIssuer, "aud": testAudience, "sub": "service-account:x", "token_use": "client_credentials", "scope": "pravara-mes:jobs", "tenant_id": rig.tenantID.String(), "exp": time.Now().Add(time.Hour).Unix()}, otherKey), order, http.StatusUnauthorized},

		// People (Janua human tokens): roles, not scopes.
		{"user: unlisted route", http.MethodGet, "/v1/tasks", rig.userToken(t, "operator"), nil, http.StatusOK},
		{"user: order intake without scopes", http.MethodPost, "/v1/orders", rig.userToken(t, "operator"), order, http.StatusCreated},
		{"user: events without scopes", http.MethodGet, "/v1/events", rig.userToken(t), nil, http.StatusOK},
		{"user non-admin: admin route", http.MethodGet, "/v1/api-keys", rig.userToken(t, "operator"), nil, http.StatusForbidden},
		{"user admin: admin route", http.MethodGet, "/v1/api-keys", rig.userToken(t, "admin"), nil, http.StatusOK},
		{"user wrong audience", http.MethodGet, "/v1/tasks", rig.sign(t, func() jwt.MapClaims { c := rig.baseClaims(uuid.NewString()); c["aud"] = "yantra4d-api"; return c }(), rig.key), nil, http.StatusUnauthorized},

		// API keys.
		{"api key jobs: order intake", http.MethodPost, "/v1/orders", rig.apiKey(t, "pravara-mes:jobs"), order, http.StatusCreated},
		{"api key legacy read:events", http.MethodGet, "/v1/events", rig.apiKey(t, "read:events"), nil, http.StatusOK},
		{"api key legacy read:events: no intake", http.MethodPost, "/v1/orders", rig.apiKey(t, "read:events"), order, http.StatusForbidden},
		{"api key read: unlisted route", http.MethodGet, "/v1/tasks", rig.apiKey(t, "pravara-mes:read"), nil, http.StatusForbidden},
		{"api key wildcard: unlisted route", http.MethodGet, "/v1/tasks", rig.apiKey(t, "*"), nil, http.StatusOK},
		{"no credentials", http.MethodGet, "/v1/orders", "", nil, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := rig.do(tc.method, tc.path, tc.bearer, tc.body)
			assert.Equal(t, tc.want, w.Code, "body: %s", w.Body.String())
		})
	}
}
