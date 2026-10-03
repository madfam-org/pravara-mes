package mqtt

// Ingest tenant-isolation test on a real PostgreSQL as the application role
// (NOSUPERUSER, NOBYPASSRLS, FORCE RLS). Runs only when
// PRAVARA_ISOLATION_DB_URL is set; skipped in -short mode.

import (
	"context"
	"database/sql"
	"io"
	"net/url"
	"os"
	"strconv"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/config"
	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/db"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

func inTenant(t *testing.T, pool *sql.DB, tenantID uuid.UUID, fn func(tx *sql.Tx)) {
	t.Helper()
	tx, err := pool.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(`SELECT set_config('app.current_tenant_id', $1, true)`, tenantID.String())
	require.NoError(t, err)
	fn(tx)
	require.NoError(t, tx.Commit())
}

func TestIngest_TenantFromTopic_RealDB(t *testing.T) {
	if testing.Short() {
		t.Skip("needs PostgreSQL; skipped in -short mode")
	}
	raw := os.Getenv("PRAVARA_ISOLATION_DB_URL")
	if raw == "" {
		t.Skip("PRAVARA_ISOLATION_DB_URL not set")
	}
	u, err := url.Parse(raw)
	require.NoError(t, err)
	port, _ := strconv.Atoi(u.Port())
	pw, _ := u.User.Password()
	if pw == "" {
		pw = "unused" // DSN() emits keyword form; an empty value would swallow the next key
	}
	dbCfg := config.DatabaseConfig{Host: u.Hostname(), Port: port, User: u.User.Username(),
		Password: pw, Name: u.Path[1:], SSLMode: "disable"}

	pool, err := sql.Open("postgres", raw)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	var super, bypass bool
	require.NoError(t, pool.QueryRow(`SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&super, &bypass))
	require.False(t, super || bypass, "must run as a role subject to RLS")

	// Two tenants, each with a machine whose code is "M1".
	type tenant struct {
		id, machine uuid.UUID
		slug        string
	}
	mk := func(label string) tenant {
		tn := tenant{id: uuid.New(), machine: uuid.New()}
		tn.slug = "ingest-" + label + "-" + tn.id.String()[:8]
		_, err := pool.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, $2, $2)`, tn.id, tn.slug)
		require.NoError(t, err)
		inTenant(t, pool, tn.id, func(tx *sql.Tx) {
			_, err := tx.Exec(`INSERT INTO machines (id, tenant_id, name, code, type) VALUES ($1, $2, 'shared code', 'M1', 'cnc')`, tn.machine, tn.id)
			require.NoError(t, err)
		})
		return tn
	}
	a, b := mk("a"), mk("b")

	store, err := db.NewStore(&dbCfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	log := logrus.New()
	log.SetOutput(io.Discard)
	h := NewHandler(&config.Config{Worker: config.WorkerConfig{BatchSize: 1000, NumWorkers: 1}}, store, log)

	ctx := context.Background()
	send := func(topic string) {
		h.processMessage(ctx, &TelemetryMessage{Topic: topic,
			Payload: TelemetryPayload{MetricType: "temperature", Value: 21.5}})
	}

	send(a.slug + "/site/area/line/M1/temperature")
	send(a.id.String() + "/site/area/line/M1/temperature") // tenant UUID form
	send("unknown-tenant/site/area/line/M1/temperature")
	send(a.slug + "/site/area/line/NOPE/temperature")

	h.batchMu.Lock()
	batch := append([]types.Telemetry(nil), h.batch...)
	h.batchMu.Unlock()
	require.Len(t, batch, 2, "unknown tenant and unknown code must be dropped")
	for _, rec := range batch {
		assert.Equal(t, a.id, rec.TenantID)
		assert.Equal(t, a.machine, rec.MachineID, "code M1 must resolve to tenant A's machine only")
	}

	// B's machine with the same code was not touched by A's messages.
	inTenant(t, pool, b.id, func(tx *sql.Tx) {
		var hb sql.NullTime
		require.NoError(t, tx.QueryRow(`SELECT last_heartbeat FROM machines WHERE id = $1`, b.machine).Scan(&hb))
		assert.False(t, hb.Valid, "B's machine must not receive A's heartbeat")
	})

	send(b.slug + "/site/area/line/M1/temperature")
	h.batchMu.Lock()
	batch = append([]types.Telemetry(nil), h.batch...)
	h.batch = h.batch[:0]
	h.batchMu.Unlock()
	require.Len(t, batch, 3)
	assert.Equal(t, b.id, batch[2].TenantID)
	assert.Equal(t, b.machine, batch[2].MachineID)

	require.NoError(t, store.CreateBatch(ctx, batch))
	require.NoError(t, store.CreateBatch(ctx, batch), "retries must be idempotent")

	count := func(scope, machine uuid.UUID) int {
		var n int
		inTenant(t, pool, scope, func(tx *sql.Tx) {
			require.NoError(t, tx.QueryRow(`SELECT COUNT(*) FROM telemetry WHERE machine_id = $1`, machine).Scan(&n))
		})
		return n
	}
	assert.Equal(t, 2, count(a.id, a.machine))
	assert.Equal(t, 1, count(b.id, b.machine))
	assert.Equal(t, 0, count(b.id, a.machine), "B must see none of A's telemetry")
	assert.Equal(t, 0, count(a.id, b.machine), "A must see none of B's telemetry")
}
