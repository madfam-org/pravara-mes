package api

// Rig for the fabrication-dispatch end-to-end tests: a scratch PostgreSQL
// database migrated from scratch (001→032), the API connected as the
// NOSUPERUSER NOBYPASSRLS application role pravara_app, miniredis for the
// durable command stream, a JWKS for person and machine tokens, and httptest
// fakes of Janua, yantra4d, fabrication-prep and asset-shells that follow
// their real API shapes (internal/dispatchfakes). Opt-in with
// PRAVARA_TEST_DATABASE_URL; skipped otherwise.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatch"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatchfakes"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
)

// machineLiveStateMirror creates migration 031's machine_live_state (owned by
// the Sparkplug host lane) when this branch runs without it. Same columns
// and policies; skipped once 031 is present.
const machineLiveStateMirror = `
CREATE TABLE IF NOT EXISTS machine_live_state (
    machine_id UUID PRIMARY KEY REFERENCES machines(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    edge_node_id TEXT NOT NULL,
    online BOOLEAN NOT NULL DEFAULT FALSE,
    state_status TEXT CHECK (state_status IN ('idle', 'printing', 'paused', 'error', 'offline')),
    progress DOUBLE PRECISION, hotend_temp_c DOUBLE PRECISION, bed_temp_c DOUBLE PRECISION,
    material_slots JSONB NOT NULL DEFAULT '[]'::jsonb,
    capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
    properties JSONB NOT NULL DEFAULT '{}'::jsonb,
    job_id TEXT, job_status TEXT, command_last_id TEXT, command_status TEXT, command_error TEXT,
    bdseq BIGINT, born_at TIMESTAMPTZ, died_at TIMESTAMPTZ, reported_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'machine_live_state') THEN
    ALTER TABLE machine_live_state ENABLE ROW LEVEL SECURITY;
    ALTER TABLE machine_live_state FORCE ROW LEVEL SECURITY;
    CREATE POLICY tenant_isolation ON machine_live_state FOR ALL
      USING (tenant_id = app_current_tenant_id()) WITH CHECK (tenant_id = app_current_tenant_id());
  END IF;
END $$;`

const treeSHA = "9de820d73e02774a8237748189ef4c3171de989d0009c2ca9020d9e6301349d7"

type dispatchRig struct {
	*scopeRig
	admin    *sql.DB // scratch database, migration owner
	app      *sql.DB // pravara_app
	svc      *dispatch.Service
	redis    *miniredis.Miniredis
	janua    *dispatchfakes.Janua
	yantra   *dispatchfakes.Yantra4D
	fabprep  *dispatchfakes.FabricationPrep
	shells   *dispatchfakes.AssetShells
	typeEnv  map[string]any
	typeID   string
	cfg      *config.Config
	tenantID uuid.UUID
}

func openAsApp(t *testing.T, admin *sql.DB) *sql.DB {
	t.Helper()
	var dbName string
	require.NoError(t, admin.QueryRow(`SELECT current_database()`).Scan(&dbName))
	for _, stmt := range []string{
		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pravara_app') THEN
			CREATE ROLE pravara_app LOGIN NOSUPERUSER NOBYPASSRLS; END IF; END $$`,
	} {
		_, err := admin.Exec(stmt)
		require.NoError(t, err)
	}
	grants, err := os.ReadFile("../../../../infra/db/roles/pravara_app_grants.sql")
	require.NoError(t, err)
	_, err = admin.Exec(string(grants))
	require.NoError(t, err)
	var super, bypass bool
	require.NoError(t, admin.QueryRow(`SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'pravara_app'`).Scan(&super, &bypass))
	require.False(t, super)
	require.False(t, bypass)

	u, err := url.Parse(os.Getenv("PRAVARA_TEST_DATABASE_URL"))
	require.NoError(t, err)
	u.User, u.Path = url.User("pravara_app"), "/"+dbName
	app, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Close() })
	require.NoError(t, app.Ping())
	return app
}

func loadTypeEnv(t *testing.T) map[string]any {
	raw, err := os.ReadFile("../integrations/assetshells/testdata/motor-soft-mount.requirements.aas.json")
	require.NoError(t, err)
	var env map[string]any
	require.NoError(t, json.Unmarshal(raw, &env))
	return env
}

func newDispatchRig(t *testing.T) *dispatchRig {
	t.Helper()
	admin := newScratchDatabase(t)
	_, err := admin.Exec(machineLiveStateMirror)
	require.NoError(t, err)
	app := openAsApp(t, admin)

	// The migrations seed the MADFAM tenant (slug madfam), as in production.
	r := &dispatchRig{admin: admin, app: app}
	require.NoError(t, admin.QueryRow(`INSERT INTO tenants (id, name, slug) VALUES (gen_random_uuid(), 'MADFAM', 'madfam')
		ON CONFLICT (slug) DO UPDATE SET slug = EXCLUDED.slug RETURNING id`).Scan(&r.tenantID))

	r.typeEnv = loadTypeEnv(t)
	r.typeID = r.typeEnv["assetAdministrationShells"].([]any)[0].(map[string]any)["id"].(string)
	r.janua = dispatchfakes.NewJanua(t,
		dispatchfakes.Client{ID: "jnc_y4d", Secret: "jns_y4d", Scope: machineclients.Yantra4DScope, Audience: machineclients.Yantra4DAudience},
		dispatchfakes.Client{ID: "jnc_fab", Secret: "jns_fab", Scope: machineclients.FabricationPrepScope, Audience: machineclients.FabricationPrepAudience},
		dispatchfakes.Client{ID: "jnc_ash", Secret: "jns_ash", Scope: machineclients.AssetShellsScope, Audience: machineclients.AssetShellsAudience})
	r.yantra = dispatchfakes.NewYantra4D(t, "motor-soft-mount", "assembled", "body", treeSHA)
	r.fabprep = dispatchfakes.NewFabricationPrep(t)
	r.shells = dispatchfakes.NewAssetShells(t, r.typeEnv)
	r.redis = miniredis.RunT(t)

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

	t.Setenv("ASSET_SHELLS_PUBLISHER_MADFAM_ECOSYSTEM_CLIENT_ID", "jnc_ash")
	t.Setenv("ASSET_SHELLS_PUBLISHER_MADFAM_ECOSYSTEM_CLIENT_SECRET", "jns_ash")
	cfg := &config.Config{}
	cfg.OIDC.Issuer, cfg.OIDC.JWKSURL, cfg.OIDC.Audience = testIssuer, jwks.URL, testAudience
	cfg.Dispatch = config.DispatchConfig{Enabled: true, RenderFormat: "3mf", ReservationTTLSeconds: 900,
		CommandHoldSeconds: 3600, MaxAttempts: 5, PollIntervalSeconds: 1, PassportMaxAttempts: 5,
		YantraAPIURL: r.yantra.Server.URL, FabricationPrepAPIURL: r.fabprep.Server.URL, AssetShellsAPIURL: r.shells.Server.URL}
	cfg.MachineClients = config.MachineClientsConfig{TokenURL: r.janua.TokenURL(),
		Yantra4DClientID: "jnc_y4d", Yantra4DClientSecret: "jns_y4d",
		FabricationPrepClientID: "jnc_fab", FabricationPrepClientSecret: "jns_fab",
		AssetShellsPublisherTenants: "madfam=ASSET_SHELLS_PUBLISHER_MADFAM_ECOSYSTEM"}
	r.cfg = cfg

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	if testing.Verbose() {
		log.SetLevel(logrus.InfoLevel)
	}
	publisher, err := pubsub.NewPublisher(pubsub.PublisherConfig{RedisURL: "redis://" + r.redis.Addr()}, log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = publisher.Close() })
	database := &db.DB{DB: app}
	r.svc, err = BuildDispatchService(cfg, database, publisher, log)
	require.NoError(t, err)
	require.Empty(t, r.svc.Unavailable())

	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutesAll(router, database, cfg, log, publisher, nil, RoutesDeps{
		APIKeyRepo: repositories.NewAPIKeyRepository(database.Tenant(log)),
		Dispatch:   r.svc,
	})
	r.scopeRig = &scopeRig{router: router, key: key, tenantID: r.tenantID, sqlDB: admin}
	return r
}

// seedMachine registers a Sparkplug machine and its live state.
func (r *dispatchRig) seedMachine(t *testing.T, code, status, class string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := r.admin.Exec(`INSERT INTO machines (id, tenant_id, name, code, type, status, specifications, metadata, mqtt_topic)
		VALUES ($1, $2, $3, $3, '3d_printer', 'online', $4, $5, $6)`, id, r.tenantID, code,
		`{"fabrication_capabilities": {"process": "fff", "build_volume_x_mm": 350, "build_volume_y_mm": 350,
		  "build_volume_z_mm": 340, "nozzle_diameters_mm": [0.4], "max_hotend_temp_c": 300, "max_bed_temp_c": 120}}`,
		`{"fabrication_prep": {"printer_profile": "klipper-corexy-350-0.4@1", "target": "klipper_gcode"},
		  "material_lots": {"1": "LOT-`+strings.ToUpper(class)+`-0042"}}`, "madfam/site-lab/"+code)
	require.NoError(t, err)
	_, err = r.admin.Exec(`INSERT INTO machine_live_state (machine_id, tenant_id, edge_node_id, online, state_status,
		material_slots, capabilities, born_at, reported_at) VALUES ($1, $2, 'site-lab', true, $3, $4, '{"max_hotend_temp_c": 300}', NOW(), NOW())`,
		id, r.tenantID, status, `[{"slot": 1, "class": "`+class+`", "loaded": true}]`)
	require.NoError(t, err)
	return id
}

// seedTask creates product → order → item → task, returning the task id.
func (r *dispatchRig) seedTask(t *testing.T, itemSpecs string) uuid.UUID {
	t.Helper()
	var product, order, item, task uuid.UUID
	sku := "Y4D-motor-soft-mount-assembled-" + uuid.NewString()[:6]
	require.NoError(t, r.admin.QueryRow(`INSERT INTO product_definitions (tenant_id, sku, name, version, parametric_specs, metadata)
		VALUES ($1, $2, 'Motor soft mount', '1.0', '{"size": {"value": 40, "type": "slider"}}',
		'{"source": "yantra4d", "slug": "motor-soft-mount", "mode": "assembled", "part": "body"}') RETURNING id`,
		r.tenantID, sku).Scan(&product))
	require.NoError(t, r.admin.QueryRow(`INSERT INTO orders (tenant_id, customer_name) VALUES ($1, 'Dispatch test') RETURNING id`,
		r.tenantID).Scan(&order))
	require.NoError(t, r.admin.QueryRow(`INSERT INTO order_items (order_id, product_name, product_sku, specifications)
		VALUES ($1, 'Motor soft mount', $2, $3) RETURNING id`, order, sku, itemSpecs).Scan(&item))
	require.NoError(t, r.admin.QueryRow(`INSERT INTO tasks (tenant_id, order_id, order_item_id, title, status)
		VALUES ($1, $2, $3, 'Print motor soft mount', 'queued') RETURNING id`, r.tenantID, order, item).Scan(&task))
	return task
}

// runUntil runs the dispatch runner until the dispatch reaches status (or
// fails the test after timeout).
func (r *dispatchRig) runUntil(t *testing.T, id uuid.UUID, status string) *repositories.DispatchJob {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		r.svc.RunOnce(context.Background())
		d := r.dispatch(t, id)
		if d.Status == status {
			return d
		}
		if d.Status == repositories.DispatchFailed && status != repositories.DispatchFailed {
			t.Fatalf("dispatch failed: %s: %s", d.ErrorCode, d.ErrorMessage)
		}
		if time.Now().After(deadline) {
			t.Fatalf("dispatch stuck in %s (want %s): %s %s", d.Status, status, d.ErrorCode, d.ErrorMessage)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// dispatch reads a dispatch as pravara_app in the tenant's scope.
func (r *dispatchRig) dispatch(t *testing.T, id uuid.UUID) *repositories.DispatchJob {
	t.Helper()
	var d *repositories.DispatchJob
	require.NoError(t, db.RunInTenantTx(context.Background(), r.app, r.tenantID.String(), func(ctx context.Context) error {
		var err error
		d, err = repositories.NewDispatchRepository(db.NewTenantDB(r.app, nil)).Get(ctx, id)
		return err
	}))
	require.NotNil(t, d)
	return d
}
