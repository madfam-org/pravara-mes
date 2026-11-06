package api

// The native MCP on PostgreSQL, with the API connected as pravara_app
// (NOSUPERUSER NOBYPASSRLS): each machine token sees only its own tenant's
// consumption and stock, engagement slugs come from the order, and
// inventory levels carry no cost. Opt-in with PRAVARA_TEST_DATABASE_URL.

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

func TestMCPToolsOnPostgresAreTenantScoped(t *testing.T) {
	admin := newScratchDatabase(t)
	app := openAsApp(t, admin)

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
	cfg.MCP = config.MCPConfig{Enabled: true, RatePerMinute: 600, Burst: 100}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	RegisterRoutesAll(router, &db.DB{DB: app}, cfg, log, nil, nil, RoutesDeps{})

	type tenant struct {
		id   uuid.UUID
		slug string
		rig  *mcpRig
	}
	seed := func(name, slug, sku string, qty float64) tenant {
		tn := tenant{id: uuid.New(), slug: slug}
		_, err := admin.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, $2, $3)`, tn.id, name, "mcp-"+tn.id.String()[:8])
		require.NoError(t, err)
		var order uuid.UUID
		require.NoError(t, admin.QueryRow(`INSERT INTO orders (tenant_id, customer_name, engagement_slug) VALUES ($1, 'MCP test', $2)
			RETURNING id`, tn.id, slug).Scan(&order))
		_, err = admin.Exec(`INSERT INTO job_consumption (tenant_id, job_kind, job_id, order_id, kind, material_code, quantity, unit,
			basis, source, recorded_by, occurred_at) VALUES ($1, 'task', $2, $3, 'material', $4, $5, 'g', 'actual', 'operator', $6,
			'2026-10-15T12:00:00Z')`, tn.id, uuid.New(), order, sku, qty, uuid.New())
		require.NoError(t, err)
		_, err = admin.Exec(`INSERT INTO inventory_items (tenant_id, sku, name, unit, quantity_on_hand, quantity_reserved, unit_cost, currency)
			VALUES ($1, $2, 'Filament', 'g', 1000, 50, 321.00, 'MXN')`, tn.id, sku)
		require.NoError(t, err)
		tn.rig = &mcpRig{scopeRig: &scopeRig{router: router, key: key, tenantID: tn.id, sqlDB: admin}}
		return tn
	}
	a := seed("Tenant A", "eng-alpha", "FIL-A", 12.5)
	b := seed("Tenant B", "eng-beta", "FIL-B", 7)

	for _, tc := range []struct {
		tn         tenant
		sku, other string
		qty        float64
	}{{a, "FIL-A", "FIL-B", 12.5}, {b, "FIL-B", "FIL-A", 7}} {
		token := tc.tn.rig.machineToken(t, middleware.ScopeCogsRead, func(c jwt.MapClaims) { c["client_id"] = "jnc_selva_cogs" })

		reply := readReply(t, tc.tn.rig.mcp(token, toolCall(1, MCPToolJobConsumption,
			map[string]any{"from": "2026-10-01T00:00:00Z", "to": "2026-11-01T00:00:00Z"})))
		require.False(t, reply.Result.IsError, "%+v", reply.Result.Content)
		var rows mcpPage[mcpConsumptionRow]
		require.NoError(t, json.Unmarshal(reply.Result.StructuredContent, &rows))
		require.Len(t, rows.Data, 1, "only the token's tenant")
		assert.Equal(t, []any{tc.sku, tc.qty, tc.tn.slug}, []any{rows.Data[0].MaterialCode, rows.Data[0].Quantity, rows.Data[0].EngagementSlug})
		assert.NotContains(t, string(reply.Result.StructuredContent), "recorded_by")

		reply = readReply(t, tc.tn.rig.mcp(token, toolCall(2, MCPToolJobConsumption,
			map[string]any{"from": "2026-10-01T00:00:00Z", "to": "2026-11-01T00:00:00Z", "engagement_slug": "eng-alpha"})))
		require.NoError(t, json.Unmarshal(reply.Result.StructuredContent, &rows))
		assert.Equal(t, tc.tn.slug == "eng-alpha", len(rows.Data) == 1, "another tenant's engagement matches nothing")

		reply = readReply(t, tc.tn.rig.mcp(token, toolCall(3, MCPToolInventoryLevels, nil)))
		require.False(t, reply.Result.IsError, "%+v", reply.Result.Content)
		var levels mcpPage[mcpInventoryLevel]
		require.NoError(t, json.Unmarshal(reply.Result.StructuredContent, &levels))
		require.Len(t, levels.Data, 1)
		assert.Equal(t, []any{tc.sku, 1000.0, 50.0, 950.0, ""},
			[]any{levels.Data[0].SKU, levels.Data[0].QuantityOnHand, levels.Data[0].QuantityReserved, levels.Data[0].QuantityAvailable,
				levels.Data[0].Category}, "a NULL category reads as empty")
		assert.NotContains(t, string(reply.Result.StructuredContent), "321")

		reply = readReply(t, tc.tn.rig.mcp(token, toolCall(4, MCPToolInventoryLevels, map[string]any{"material_code": tc.other})))
		require.NoError(t, json.Unmarshal(reply.Result.StructuredContent, &levels))
		assert.Empty(t, levels.Data, "the other tenant's SKU is invisible")
	}
}
