package api

// POST /mcp through the real routes and the real Janua token verification
// (a JWKS served by httptest), with sqlmock standing in for PostgreSQL: the
// switch, every auth gate, and the tools running in the token's tenant
// transaction. mcp_pg_test.go repeats the tool calls on PostgreSQL.

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

type mcpRig struct {
	*scopeRig
	mock sqlmock.Sqlmock
}

func newMCPRig(t *testing.T, enabled bool) *mcpRig {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

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

	cfg := &config.Config{}
	cfg.OIDC.Issuer, cfg.OIDC.JWKSURL, cfg.OIDC.Audience = testIssuer, jwks.URL, testAudience
	cfg.MCP = config.MCPConfig{Enabled: enabled, RatePerMinute: 600, Burst: 100}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	RegisterRoutesAll(router, &db.DB{DB: sqlDB}, cfg, log, nil, nil, RoutesDeps{
		APIKeyRepo: repositories.NewAPIKeyRepository(sqlDB),
	})
	return &mcpRig{scopeRig: &scopeRig{router: router, key: key, tenantID: uuid.New(), sqlDB: sqlDB}, mock: mock}
}

func (r *mcpRig) cogsToken(t *testing.T) string {
	return r.machineToken(t, middleware.ScopeCogsRead, nil)
}

func (r *mcpRig) mcp(bearer string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, MCPPath, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	r.router.ServeHTTP(w, req)
	return w
}

func rpc(id int, method string, params any) map[string]any {
	m := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		m["params"] = params
	}
	return m
}

func toolCall(id int, name string, args map[string]any) map[string]any {
	return rpc(id, "tools/call", map[string]any{"name": name, "arguments": args})
}

type mcpReply struct {
	Result struct {
		Tools             []map[string]any `json:"tools"`
		IsError           bool             `json:"isError"`
		Content           []map[string]any `json:"content"`
		StructuredContent json.RawMessage  `json:"structuredContent"`
	} `json:"result"`
	Error *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func readReply(t *testing.T, w *httptest.ResponseRecorder) mcpReply {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var r mcpReply
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &r), w.Body.String())
	return r
}

func TestMCPIsOffByDefault(t *testing.T) {
	router := registeredRouter(t)
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, MCPPath, bytes.NewReader([]byte(`{}`))))
		assert.Equal(t, http.StatusNotFound, w.Code, "%s /mcp with PRAVARA_MCP_ENABLED unset", method)
	}
	r := newMCPRig(t, false)
	assert.Equal(t, http.StatusNotFound, r.mcp(r.cogsToken(t), rpc(1, "ping", nil)).Code)
}

func TestMCPAuthGates(t *testing.T) {
	r := newMCPRig(t, true)
	ping := rpc(1, "ping", nil)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	w := r.mcp("", ping)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, `Bearer realm="pravara-mcp"`, w.Header().Get("WWW-Authenticate"))

	assert.Equal(t, http.StatusUnauthorized, r.mcp("prv_"+uuid.NewString(), ping).Code, "API keys are not accepted")
	assert.Equal(t, http.StatusUnauthorized, r.mcp(r.machineToken(t, middleware.ScopeCogsRead, func(c jwt.MapClaims) {
		c["aud"] = "another-api"
	}), ping).Code, "another audience")
	assert.Equal(t, http.StatusUnauthorized, r.mcp(r.sign(t, jwt.MapClaims(r.baseClaims("service-account:jnc_x")), otherKey), ping).Code,
		"a key outside the JWKS")
	assert.Equal(t, http.StatusUnauthorized, r.mcp(r.machineToken(t, middleware.ScopeCogsRead, func(c jwt.MapClaims) {
		c["exp"] = time.Now().Add(-time.Minute).Unix()
	}), ping).Code, "expired")

	assert.Equal(t, http.StatusForbidden, r.mcp(r.userToken(t, "admin"), ping).Code, "people come in Phase B")
	w = r.mcp(r.machineToken(t, middleware.ScopeRead+" "+middleware.ScopeJobs, nil), ping)
	assert.Equal(t, http.StatusForbidden, w.Code, "pravara-mes scopes do not open the MCP")
	assert.Contains(t, w.Header().Get("WWW-Authenticate"), `scope="pravara-cogs:read"`)
	assert.Equal(t, http.StatusForbidden, r.mcp(r.machineToken(t, middleware.ScopeCogsRead, func(c jwt.MapClaims) {
		delete(c, "tenant_id")
	}), ping).Code, "machine tokens must be org-bound (tenant_id)")

	w = r.mcp(r.cogsToken(t), ping)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, w.Header().Get("WWW-Authenticate"))
	require.NoError(t, r.mock.ExpectationsWereMet(), "the gates and ping touch no database")
}

func TestMCPToolsListIsTheReadOnlyCatalog(t *testing.T) {
	r := newMCPRig(t, true)
	reply := readReply(t, r.mcp(r.cogsToken(t), rpc(1, "tools/list", nil)))
	var names []string
	for _, tool := range reply.Result.Tools {
		names = append(names, tool["name"].(string))
		assert.Equal(t, true, tool["annotations"].(map[string]any)["readOnlyHint"], tool["name"])
		assert.Equal(t, false, tool["annotations"].(map[string]any)["destructiveHint"], tool["name"])
		assert.Equal(t, false, tool["inputSchema"].(map[string]any)["additionalProperties"], tool["name"])
	}
	assert.Equal(t, []string{"mes_job_consumption", "mes_inventory_levels"}, names)
	rfcNames := map[string]string{"mes_job_consumption": "mes.job_consumption", "mes_inventory_levels": "mes.inventory_levels"}
	for _, tool := range reply.Result.Tools {
		name := tool["name"].(string)
		assert.Regexp(t, `^[a-zA-Z0-9_-]{1,64}$`, name, "Claude rejects any other tool name")
		assert.True(t, strings.HasPrefix(tool["title"].(string), rfcNames[name]+" "), "%s title carries the RFC 0051 name", name)
	}
}

var consumptionCols = []string{"id", "tenant_id", "job_kind", "job_id", "dispatch_id", "print_job_id", "task_id",
	"order_id", "machine_id", "kind", "material_code", "material_lot", "quantity", "unit", "basis", "source", "note",
	"recorded_by", "occurred_at", "created_at", "engagement_slug", "registry_code"}

func (r *mcpRig) expectTenantTx() {
	r.mock.ExpectBegin()
	r.mock.ExpectExec(regexp.QuoteMeta("SELECT set_config('app.current_tenant_id', $1, true)")).
		WithArgs(r.tenantID.String()).WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestMCPJobConsumptionRunsTheHTTPReadInTheTokensTenant(t *testing.T) {
	r := newMCPRig(t, true)
	from, to := "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z"
	occurred := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	rowID, jobID, orderID, reporter := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	r.expectTenantTx() // the tenant comes from the token, exactly as for GET /v1/jobs/consumption
	r.mock.ExpectQuery(`SELECT count\(\*\) FROM job_consumption c LEFT JOIN orders o`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "eng-alpha").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	r.mock.ExpectQuery(`FROM job_consumption c LEFT JOIN orders o .* ORDER BY c.occurred_at, c.id LIMIT`).
		WillReturnRows(sqlmock.NewRows(consumptionCols).AddRow(rowID, r.tenantID, "task", jobID, nil, nil, nil,
			orderID, nil, "material", "FIL-PLA-BLK", "", 12.5, "g", "actual", "operator", "", reporter, occurred, occurred, "eng-alpha", "FIL-PLA-BLK"))
	r.mock.ExpectCommit()

	reply := readReply(t, r.mcp(r.cogsToken(t), toolCall(1, MCPToolJobConsumption,
		map[string]any{"from": from, "to": to, "engagement_slug": "eng-alpha", "limit": 50})))
	require.False(t, reply.Result.IsError, "%+v", reply.Result.Content)
	var page struct {
		Data                 []map[string]any `json:"data"`
		Total, Limit, Offset int
	}
	require.NoError(t, json.Unmarshal(reply.Result.StructuredContent, &page))
	assert.Equal(t, []int{1, 50, 0}, []int{page.Total, page.Limit, page.Offset})
	require.Len(t, page.Data, 1)
	row := page.Data[0]
	assert.Equal(t, "eng-alpha", row["engagement_slug"], "pravara-cogs:read reads slugs over HTTP too")
	assert.Equal(t, 12.5, row["quantity"])
	assert.NotContains(t, row, "recorded_by", "no person ids in MCP outputs")
	assert.NotContains(t, row, "tenant_id")
	require.NoError(t, r.mock.ExpectationsWereMet())
}

func TestMCPJobConsumptionArgumentErrorsTouchNoDatabase(t *testing.T) {
	r := newMCPRig(t, true)
	token := r.cogsToken(t)
	for name, args := range map[string]map[string]any{
		"missing to":     {"from": "2026-10-01T00:00:00Z"},
		"bad from":       {"from": "yesterday", "to": "2026-11-01T00:00:00Z"},
		"bad slug":       {"from": "2026-10-01T00:00:00Z", "to": "2026-11-01T00:00:00Z", "engagement_slug": "Bad Slug"},
		"bad task":       {"from": "2026-10-01T00:00:00Z", "to": "2026-11-01T00:00:00Z", "task_id": "x"},
		"limit too big":  {"from": "2026-10-01T00:00:00Z", "to": "2026-11-01T00:00:00Z", "limit": 5000},
		"limit as text":  {"from": "2026-10-01T00:00:00Z", "to": "2026-11-01T00:00:00Z", "limit": "10"},
		"unknown arg":    {"from": "2026-10-01T00:00:00Z", "to": "2026-11-01T00:00:00Z", "unit_cost": 1},
		"from as number": {"from": 1, "to": "2026-11-01T00:00:00Z"},
	} {
		reply := readReply(t, r.mcp(token, toolCall(1, MCPToolJobConsumption, args)))
		assert.True(t, reply.Result.IsError, name)
	}
	require.NoError(t, r.mock.ExpectationsWereMet())
}

var inventoryCols = []string{"id", "tenant_id", "sku", "name", "category", "description", "unit", "quantity_on_hand",
	"quantity_reserved", "quantity_available", "reorder_point", "reorder_quantity", "forgesight_id", "unit_cost",
	"currency", "metadata", "created_at", "updated_at", "material_id", "material_code"}

func TestMCPInventoryLevelsAreQuantitiesOnly(t *testing.T) {
	r := newMCPRig(t, true)
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	// Fixed ids: a random UUID can contain "250" and trip the unit-cost check below.
	ids := map[string]uuid.UUID{"FIL-PLA": uuid.MustParse("6d1f3c3e-7a44-4b0e-9c1d-2e8f4a6b7c90"),
		"FIL-PLA-XL": uuid.MustParse("8e3a1d7f-4c6b-4f19-a0d3-5b7c9e1f3a64")}
	item := func(sku string) []driver.Value {
		return []driver.Value{ids[sku], r.tenantID, sku, "PLA black 1 kg", nil, "secret note", "g", 1000.0, 100.0, 900.0, 200.0,
			500.0, "fs-1", 250.0, "MXN", []byte(`{"supplier":"someone"}`), now, now, nil, nil}
	}

	// By material code: the HTTP search, then the exact SKU.
	r.expectTenantTx()
	r.mock.ExpectQuery(`SELECT COUNT\(\*\) FROM inventory_items`).WithArgs("%FIL-PLA%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	r.mock.ExpectQuery(`FROM inventory_items .* ORDER BY name ASC$`).WithArgs("%FIL-PLA%").
		WillReturnRows(sqlmock.NewRows(inventoryCols).AddRow(item("FIL-PLA")...).AddRow(item("FIL-PLA-XL")...))
	r.mock.ExpectCommit()

	reply := readReply(t, r.mcp(r.cogsToken(t), toolCall(1, MCPToolInventoryLevels, map[string]any{"material_code": "FIL-PLA"})))
	require.False(t, reply.Result.IsError, "%+v", reply.Result.Content)
	for _, forbidden := range []string{"unit_cost", "currency", "forgesight_id", "metadata", "description", "250", "secret note", "someone"} {
		assert.NotContains(t, string(reply.Result.StructuredContent), forbidden)
		assert.NotContains(t, fmt.Sprint(reply.Result.Content), forbidden)
	}
	var page struct {
		Data  []mcpInventoryLevel `json:"data"`
		Total int                 `json:"total"`
	}
	require.NoError(t, json.Unmarshal(reply.Result.StructuredContent, &page))
	require.Len(t, page.Data, 1, "exact SKU match")
	assert.Equal(t, 1, page.Total)
	assert.Equal(t, mcpInventoryLevel{ID: page.Data[0].ID, SKU: "FIL-PLA", Name: "PLA black 1 kg", Unit: "g",
		QuantityOnHand: 1000, QuantityReserved: 100, QuantityAvailable: 900, ReorderPoint: 200, UpdatedAt: now}, page.Data[0])

	// Unfiltered: paginated like GET /v1/inventory.
	r.expectTenantTx()
	r.mock.ExpectQuery(`SELECT COUNT\(\*\) FROM inventory_items`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
	r.mock.ExpectQuery(`ORDER BY name ASC LIMIT \$1 OFFSET \$2`).WithArgs(2, 4).
		WillReturnRows(sqlmock.NewRows(inventoryCols).AddRow(item("A")...))
	r.mock.ExpectCommit()
	reply = readReply(t, r.mcp(r.cogsToken(t), toolCall(2, MCPToolInventoryLevels, map[string]any{"limit": 2, "offset": 4})))
	require.False(t, reply.Result.IsError)
	assert.Equal(t, 7, mustPage(t, reply).Total)

	for _, args := range []map[string]any{{"material_code": ""}, {"limit": 0}, {"offset": -1}, {"sku": "x"}} {
		assert.True(t, readReply(t, r.mcp(r.cogsToken(t), toolCall(3, MCPToolInventoryLevels, args))).Result.IsError, "%v", args)
	}
	require.NoError(t, r.mock.ExpectationsWereMet())
}

func mustPage(t *testing.T, reply mcpReply) (p struct {
	Total int `json:"total"`
}) {
	t.Helper()
	require.NoError(t, json.Unmarshal(reply.Result.StructuredContent, &p))
	return p
}
