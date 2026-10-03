package repositories

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// PostgreSQL-backed tests run only when PRAVARA_TEST_DATABASE_URL points at
// a throwaway database (migrated on first use); otherwise they are skipped.
const testDatabaseEnv = "PRAVARA_TEST_DATABASE_URL"

const livenessRLSRole = "pravara_liveness_rls_test"

var pgSchemaOnce sync.Once

func openPG(t *testing.T) *sql.DB {
	t.Helper()
	raw := os.Getenv(testDatabaseEnv)
	if raw == "" {
		t.Skipf("%s not set; skipping PostgreSQL test", testDatabaseEnv)
	}
	db, err := sql.Open("postgres", raw)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })

	var schemaErr error
	pgSchemaOnce.Do(func() {
		var exists bool
		if schemaErr = db.QueryRow(`SELECT to_regclass('public.task_commands') IS NOT NULL`).Scan(&exists); schemaErr != nil || exists {
			return
		}
		files, _ := filepath.Glob(filepath.Join("..", "migrations", "*.up.sql"))
		sort.Strings(files)
		for _, f := range files {
			body, err := os.ReadFile(f)
			if err != nil {
				schemaErr = err
				return
			}
			if _, err := db.Exec(string(body)); err != nil {
				schemaErr = err
				return
			}
		}
	})
	require.NoError(t, schemaErr)
	return db
}

// openRLSRole connects as a NOSUPERUSER NOBYPASSRLS role.
func openRLSRole(t *testing.T, admin *sql.DB) *sql.DB {
	t.Helper()
	for _, s := range []string{
		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '` + livenessRLSRole + `') THEN
			CREATE ROLE ` + livenessRLSRole + ` LOGIN NOSUPERUSER NOBYPASSRLS; END IF; END $$`,
		`GRANT SELECT, INSERT, UPDATE ON machines, event_outbox, task_commands TO ` + livenessRLSRole,
		`GRANT SELECT ON tenants TO ` + livenessRLSRole,
	} {
		_, err := admin.Exec(s)
		require.NoError(t, err)
	}
	u, err := url.Parse(os.Getenv(testDatabaseEnv))
	require.NoError(t, err)
	u.User = url.User(livenessRLSRole)
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTenant(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'T', $2)`, id, "t-"+id.String())
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tenants WHERE id = $1`, id) })
	return id
}

func newMachine(t *testing.T, db *sql.DB, tenantID uuid.UUID, code string, heartbeatAgo time.Duration) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(`INSERT INTO machines (id, tenant_id, name, code, type, status, last_heartbeat)
		VALUES ($1, $2, $3, $3, '3d_printer', 'online', NOW() - make_interval(secs => $4))`,
		id, tenantID, code, heartbeatAgo.Seconds())
	require.NoError(t, err)
	return id
}

func TestLivenessPG_MarksOnlyStaleMachinesOfTheTenantUnderRLS(t *testing.T) {
	admin := openPG(t)
	app := openRLSRole(t, admin)
	repo := NewMachineRepository(app)
	ctx := context.Background()

	t1, t2 := newTenant(t, admin), newTenant(t, admin)
	stale := newMachine(t, admin, t1, "stale", 10*time.Minute)
	fresh := newMachine(t, admin, t1, "fresh", 10*time.Second)
	otherStale := newMachine(t, admin, t2, "other", 10*time.Minute)

	machines, err := repo.GetOfflineMachines(ctx, t1, 5*time.Minute)
	require.NoError(t, err)
	require.Len(t, machines, 1)
	require.Equal(t, stale, machines[0].ID)

	event := OutboxRecord{EventType: "machine.status_changed", Namespace: "machines", Payload: []byte(`{"type":"machine.status_changed"}`)}
	cutoff := time.Now().Add(-5 * time.Minute)
	changed, err := repo.MarkOfflineIfStale(ctx, t1, stale, cutoff, event)
	require.NoError(t, err)
	require.True(t, changed)

	// Second attempt is a no-op; a fresh machine is never marked; another
	// tenant's machine is invisible from this tenant's context.
	changed, err = repo.MarkOfflineIfStale(ctx, t1, stale, cutoff, event)
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = repo.MarkOfflineIfStale(ctx, t1, fresh, cutoff, event)
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = repo.MarkOfflineIfStale(ctx, t1, otherStale, cutoff, event)
	require.NoError(t, err)
	require.False(t, changed)

	status := func(id uuid.UUID) string {
		var s string
		require.NoError(t, admin.QueryRow(`SELECT status::text FROM machines WHERE id = $1`, id).Scan(&s))
		return s
	}
	require.Equal(t, "offline", status(stale))
	require.Equal(t, "online", status(fresh))
	require.Equal(t, "online", status(otherStale))

	var n int
	require.NoError(t, admin.QueryRow(`SELECT count(*) FROM event_outbox WHERE tenant_id = $1 AND event_type = 'machine.status_changed'`, t1).Scan(&n))
	require.Equal(t, 1, n)

	ids, err := repo.ListTenantIDs(ctx)
	require.NoError(t, err)
	require.Contains(t, ids, t1)
	require.Contains(t, ids, t2)
}

func TestTaskCommandPG_DirectCommandHasNoTask(t *testing.T) {
	admin := openPG(t)
	repo := NewTaskCommandRepository(admin)
	tenant := newTenant(t, admin)
	machine := newMachine(t, admin, tenant, "direct", time.Second)

	cmd := &TaskCommand{
		TenantID: tenant, MachineID: machine, CommandID: uuid.New(),
		CommandType: "home", Status: "pending", IssuedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.Create(context.Background(), cmd))

	got, err := repo.GetByCommandID(context.Background(), cmd.CommandID)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, got.TaskID)
	require.Equal(t, "pending", got.Status)

	require.NoError(t, repo.UpdateStatus(context.Background(), cmd.CommandID, "timeout", "no acknowledgement before the deadline"))
}
