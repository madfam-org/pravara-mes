package db

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/command"
)

const rlsTestRole = "pravara_ledger_rls_test"

// openAppRoleDB connects as a NOSUPERUSER NOBYPASSRLS role so row-level
// security applies, as it should for the production application role.
func openAppRoleDB(t *testing.T, admin *sql.DB) *sql.DB {
	t.Helper()
	stmts := []string{
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '` + rlsTestRole + `') THEN
				CREATE ROLE ` + rlsTestRole + ` LOGIN NOSUPERUSER NOBYPASSRLS;
			END IF;
		END $$`,
		`GRANT SELECT, INSERT, UPDATE ON task_commands, tasks, orders, machines, event_outbox TO ` + rlsTestRole,
		`GRANT SELECT ON tenants TO ` + rlsTestRole,
	}
	for _, s := range stmts {
		_, err := admin.Exec(s)
		require.NoError(t, err)
	}

	u, err := url.Parse(os.Getenv(testDatabaseEnv))
	require.NoError(t, err)
	u.User = url.User(rlsTestRole)
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })

	var bypass bool
	require.NoError(t, db.QueryRow(`SELECT rolbypassrls OR rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&bypass))
	require.False(t, bypass)
	return db
}

func TestLedgerPG_TenantScopedWritesUnderRLS(t *testing.T) {
	admin := openTestDB(t)
	appDB := openAppRoleDB(t, admin)
	f1, f2 := newFixture(t, admin), newFixture(t, admin)
	l := NewCommandLedgerWithScope(appDB, TxTenantScope{DB: appDB})
	ctx := context.Background()

	// Without tenant context the application role sees no commands at all.
	var visible int
	require.NoError(t, appDB.QueryRow(`SELECT count(*) FROM task_commands`).Scan(&visible))
	require.Zero(t, visible)

	m1 := f1.machine(t, "rls-1", "", "online")
	m2 := f2.machine(t, "rls-2", "", "online")
	overdue1 := f1.command(t, m1, nil, "home", "pending")
	overdue2 := f2.command(t, m2, nil, "home", "pending")
	_, err := admin.Exec(`UPDATE task_commands SET issued_at = NOW() - INTERVAL '1 hour' WHERE command_id IN ($1, $2)`, overdue1, overdue2)
	require.NoError(t, err)

	now := time.Now().UTC()
	expired, err := l.ExpireOverdue(ctx, f1.tenantID, now, now.Add(-10*time.Minute), 100)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	require.Equal(t, overdue1, expired[0].CommandID)

	s1, _, _ := f1.commandStatus(t, overdue1)
	s2, _, _ := f2.commandStatus(t, overdue2)
	require.Equal(t, "timeout", s1)
	require.Equal(t, "pending", s2, "another tenant's command must be untouched")
	require.Len(t, f1.outboxEvents(t)[EventMachineCommandFailed], 1, "outbox insert must pass RLS with tenant context")
	require.Empty(t, f2.outboxEvents(t))

	// An ack applied in tenant 2's context cannot reach tenant 1's command.
	cmd1 := f1.command(t, m1, nil, "pause", "sent")
	out, err := l.ApplyAck(ctx, command.AckApplication{
		CommandID: cmd1, Machine: command.AckMachine{ID: m1, TenantID: f2.tenantID}, Success: true, AckedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, command.AckUnknownCommand, out.Disposition)

	// Ack resolution runs under RLS too: the tenant comes from the topic and
	// the machine is looked up inside that tenant only.
	m3 := f1.machine(t, "rls-3", f1.topic("s/a/l/rls-3"), "online")
	resolved, err := l.ResolveAckMachine(ctx, f1.topic("s/a/l/rls-3"), "rls-3")
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.Equal(t, m3, resolved.ID)
	require.Equal(t, f1.tenantID, resolved.TenantID)
	resolved, err = l.ResolveAckMachine(ctx, f2.topic("s/a/l/rls-3"), "rls-3")
	require.NoError(t, err)
	require.Nil(t, resolved, "another tenant's topic must not resolve tenant 1's machine")
}
