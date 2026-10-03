package api_test

// Tenant-isolation rig: the real router on a real PostgreSQL, connected as a
// NOSUPERUSER/NOBYPASSRLS application role, with two tenants.
//
// Runs only when PRAVARA_ISOLATION_DB_URL points at a database migrated with
// infra/db/migrate.sh (through 029) and the role from
// docs/operations/database-app-role.md. Skipped otherwise and in -short mode.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/api"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

const (
	rigIssuer   = "https://issuer.test"
	rigAudience = "pravara-api"
	rigKid      = "isolation-test"
)

type tenantFixture struct {
	ID     uuid.UUID
	Slug   string
	UserID uuid.UUID
	Token  string
}

type isolationRig struct {
	t      *testing.T
	pool   *sql.DB
	tdb    *db.TenantDB
	router *gin.Engine
	A, B   tenantFixture
}

func newIsolationRig(t *testing.T) *isolationRig {
	t.Helper()
	if testing.Short() {
		t.Skip("tenant isolation suite needs PostgreSQL; skipped in -short mode")
	}
	url := os.Getenv("PRAVARA_ISOLATION_DB_URL")
	if url == "" {
		t.Skip("PRAVARA_ISOLATION_DB_URL not set")
	}
	gin.SetMode(gin.TestMode)

	pool, err := sql.Open("postgres", url)
	require.NoError(t, err)
	// A small pool forces connection reuse across tenants: the old
	// pool-level SET leaked exactly under these conditions.
	pool.SetMaxOpenConns(4)
	pool.SetMaxIdleConns(4)
	require.NoError(t, pool.Ping())
	t.Cleanup(func() { pool.Close() })

	var super, bypass bool
	require.NoError(t, pool.QueryRow(
		`SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&super, &bypass))
	require.False(t, super, "the isolation suite must run as a non-superuser")
	require.False(t, bypass, "the isolation suite must run as a role without BYPASSRLS")

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": rigKid, "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(jwks.Close)

	log := logrus.New()
	log.SetOutput(io.Discard)
	database := &db.DB{DB: pool}
	tdb := database.Tenant(log)
	cfg := &config.Config{}
	cfg.OIDC.Issuer, cfg.OIDC.JWKSURL, cfg.OIDC.Audience = rigIssuer, jwks.URL, rigAudience

	router := gin.New()
	api.RegisterRoutesAll(router, database, cfg, log, nil, nil, api.RoutesDeps{
		OutboxRepo:  repositories.NewOutboxRepository(tdb),
		WebhookRepo: repositories.NewWebhookRepository(tdb),
		APIKeyRepo:  repositories.NewAPIKeyRepository(tdb),
		FeedRepo:    repositories.NewFeedRepository(tdb),
		StatusDB:    pool,
	})

	r := &isolationRig{t: t, pool: pool, tdb: tdb, router: router}
	r.A = r.newTenant(key, "a")
	r.B = r.newTenant(key, "b")
	return r
}

func (r *isolationRig) newTenant(key *rsa.PrivateKey, label string) tenantFixture {
	r.t.Helper()
	f := tenantFixture{ID: uuid.New(), UserID: uuid.New()}
	f.Slug = fmt.Sprintf("iso-%s-%s", label, f.ID.String()[:8])
	_, err := r.pool.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, $2, $3)`, f.ID, "Isolation "+label, f.Slug)
	require.NoError(r.t, err)
	r.seed(f.ID, `INSERT INTO users (id, tenant_id, email, name, role) VALUES ($1, $2, $3, 'Admin', 'admin')`,
		f.UserID, f.ID, f.Slug+"@example.test")

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": rigIssuer, "aud": rigAudience, "sub": f.UserID.String(),
		"tenant_id": f.ID.String(), "roles": []string{"admin"},
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = rigKid
	f.Token, err = tok.SignedString(key)
	require.NoError(r.t, err)
	return f
}

// seed runs one statement in tenantID's scope, as the application role.
func (r *isolationRig) seed(tenantID uuid.UUID, query string, args ...any) {
	r.t.Helper()
	err := db.RunInTenantTx(context.Background(), r.pool, tenantID.String(), func(ctx context.Context) error {
		_, err := r.tdb.ExecContext(ctx, query, args...)
		return err
	})
	require.NoError(r.t, err, query)
}

// scopedErr runs one statement in tenantID's scope and returns its error.
func (r *isolationRig) scopedErr(tenantID uuid.UUID, query string, args ...any) (int64, error) {
	var n int64
	err := db.RunInTenantTx(context.Background(), r.pool, tenantID.String(), func(ctx context.Context) error {
		res, err := r.tdb.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}

type rigResponse struct {
	Code int
	Body []byte
}

func (r *isolationRig) do(f tenantFixture, method, path string, body any) rigResponse {
	return r.doWith(map[string]string{"Authorization": "Bearer " + f.Token}, method, path, body)
}

func (r *isolationRig) doWith(headers map[string]string, method, path string, body any) rigResponse {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(r.t, err)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.router.ServeHTTP(w, req)
	return rigResponse{Code: w.Code, Body: w.Body.Bytes()}
}

// createID POSTs body as f and returns the created entity's "id".
func (r *isolationRig) createID(f tenantFixture, path string, body any) string {
	r.t.Helper()
	res := r.do(f, http.MethodPost, path, body)
	require.Equalf(r.t, http.StatusCreated, res.Code, "POST %s: %s", path, res.Body)
	var out map[string]any
	require.NoError(r.t, json.Unmarshal(res.Body, &out))
	id, ok := out["id"].(string)
	if !ok {
		if order, isMap := out["order"].(map[string]any); isMap {
			id, ok = order["id"].(string)
		}
	}
	require.Truef(r.t, ok, "POST %s: no id in %s", path, res.Body)
	return id
}
