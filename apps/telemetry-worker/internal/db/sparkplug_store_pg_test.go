package db

// Sparkplug host store on a real PostgreSQL as the application role
// (NOSUPERUSER, NOBYPASSRLS, FORCE RLS, migration 031 applied). Runs only
// when PRAVARA_ISOLATION_DB_URL is set; skipped in -short mode.

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/host"
)

type spTenant struct {
	id   uuid.UUID
	slug string
}

func spAppDB(t *testing.T) *sql.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("needs PostgreSQL; skipped in -short mode")
	}
	raw := os.Getenv("PRAVARA_ISOLATION_DB_URL")
	if raw == "" {
		t.Skip("PRAVARA_ISOLATION_DB_URL not set")
	}
	pool, err := sql.Open("postgres", raw)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	var super, bypass bool
	require.NoError(t, pool.QueryRow(`SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&super, &bypass))
	require.False(t, super || bypass, "must run as a role subject to RLS")
	return pool
}

func spExec(t *testing.T, pool *sql.DB, tenantID uuid.UUID, q string, args ...interface{}) {
	t.Helper()
	require.NoError(t, inTenantTx(context.Background(), pool, tenantID, func(tx *sql.Tx) error {
		_, err := tx.Exec(q, args...)
		return err
	}))
}

func spScalar(t *testing.T, pool *sql.DB, tenantID uuid.UUID, q string, args ...interface{}) string {
	t.Helper()
	var v sql.NullString
	require.NoError(t, inTenantTx(context.Background(), pool, tenantID, func(tx *sql.Tx) error {
		return tx.QueryRow(q, args...).Scan(&v)
	}))
	return v.String
}

func spNewTenant(t *testing.T, pool *sql.DB) spTenant {
	t.Helper()
	tn := spTenant{id: uuid.New()}
	tn.slug = "sp-" + tn.id.String()[:8]
	_, err := pool.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Sparkplug test', $2)`, tn.id, tn.slug)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(`DELETE FROM tenants WHERE id = $1`, tn.id) })
	return tn
}

func spEdgeNode(t *testing.T, pool *sql.DB, tn spTenant, edge string, disabled bool) {
	t.Helper()
	var disabledAt interface{}
	if disabled {
		disabledAt = time.Now()
	}
	spExec(t, pool, tn.id, `INSERT INTO edge_nodes (tenant_id, edge_node_id, mqtt_username, password_hash, disabled_at)
		VALUES ($1, $2, $3, '$2a$04$placeholderplaceholderplaceholderplaceholderplace', $4)`,
		tn.id, edge, sparkplug.EdgeNodeUsername(tn.slug, edge), disabledAt)
}

func spMachine(t *testing.T, pool *sql.DB, tn spTenant, code, edge string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	spExec(t, pool, tn.id, `INSERT INTO machines (id, tenant_id, name, code, type, status, sparkplug_edge_id)
		VALUES ($1, $2, $3, $4, '3d_printer', 'offline', NULLIF($5, ''))`, id, tn.id, "Printer "+code, code, edge)
	return id
}

func spCommand(t *testing.T, pool *sql.DB, tn spTenant, machineID uuid.UUID, taskID *uuid.UUID, cmdType, status string, params string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	spExec(t, pool, tn.id, `INSERT INTO task_commands (tenant_id, task_id, machine_id, command_id, command_type, status, parameters, sent_at, deadline_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, NOW(), NOW() + INTERVAL '10 minutes')`,
		tn.id, taskID, machineID, id, cmdType, status, params)
	return id
}

func TestSparkplugStorePG_AsAppRole(t *testing.T) {
	pool := spAppDB(t)
	ctx := context.Background()
	log := logrus.New()
	log.SetOutput(io.Discard)
	s := NewSparkplugStore(newStoreWithDB(pool), NewCommandLedgerWithScope(pool, TxTenantScope{DB: pool}), log)

	a, b := spNewTenant(t, pool), spNewTenant(t, pool)
	spEdgeNode(t, pool, a, "site-north", false)
	spEdgeNode(t, pool, a, "site-off", true)
	spEdgeNode(t, pool, b, "site-north", false)
	voron := spMachine(t, pool, a, "VORON-01", "site-north")
	other := spMachine(t, pool, a, "OTHER-01", "site-north")
	bVoron := spMachine(t, pool, b, "VORON-01", "site-north")

	// Registry resolution: tenant from the slug, never a UUID group, never disabled.
	na, err := s.ResolveEdgeNode(ctx, a.slug, "site-north")
	require.NoError(t, err)
	require.NotNil(t, na)
	require.Equal(t, a.id.String(), na.TenantID)
	nb, err := s.ResolveEdgeNode(ctx, b.slug, "site-north")
	require.NoError(t, err)
	require.NotEqual(t, na.ID, nb.ID)
	for _, c := range [][2]string{{a.slug, "site-off"}, {a.id.String(), "site-north"}, {a.slug, "site-none"}, {"no-such-tenant", "site-north"}} {
		n, err := s.ResolveEdgeNode(ctx, c[0], c[1])
		require.NoError(t, err)
		require.Nil(t, n, c)
	}

	require.NoError(t, s.NodeBirth(ctx, *na, 4, time.Now()))
	require.Equal(t, "4", spScalar(t, pool, a.id, `SELECT last_bdseq::text FROM edge_nodes WHERE tenant_id = $1 AND edge_node_id = 'site-north'`, a.id))

	// A command that was sent but not acknowledged is re-sent after DBIRTH.
	taskID := uuid.New()
	spExec(t, pool, a.id, `INSERT INTO tasks (id, tenant_id, machine_id, title, status) VALUES ($1, $2, $3, 'Print', 'in_progress')`, taskID, a.id, voron)
	startJob := spCommand(t, pool, a, voron, &taskID, "start_job", "sent",
		`{"artifact_url":"https://prep.example.test/a.gcode","artifact_sha256":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08","artifact_media_type":"text/x-gcode"}`)
	foreign := spCommand(t, pool, a, other, nil, "pause", "sent", `{}`)

	now := time.Now().UTC().Truncate(time.Millisecond)
	out, err := s.DeviceReport(ctx, *na, host.DeviceReport{
		DeviceID: "VORON-01", Birth: true, BdSeq: 4, Seq: 1, Timestamp: now, ReceivedAt: now,
		Values: map[sparkplug.MetricName]any{
			sparkplug.MetricStateStatus: "idle", sparkplug.MetricTempsHotend: 24.5, sparkplug.MetricStateProgress: 0.0,
			sparkplug.MaterialSlotClassMetric(1): "pla", sparkplug.MaterialSlotLoadedMetric(1): true,
			sparkplug.MaterialSlotClassMetric(2): nil, sparkplug.MaterialSlotLoadedMetric(2): false,
			sparkplug.CapabilityMaxHotendTempC.Metric(): 300.0, sparkplug.CapabilityNozzleDiametersMM.Metric(): []float64{0.4},
			sparkplug.MetricPropertiesModel: "Voron 2.4", sparkplug.MetricPropertiesConnectivity: "moonraker",
			sparkplug.MetricCommandID: nil,
		},
	})
	require.NoError(t, err)
	require.True(t, out.Registered)
	require.Len(t, out.Resend, 1)
	require.Equal(t, startJob.String(), out.Resend[0].ID)
	require.Equal(t, taskID.String(), out.Resend[0].TaskID)
	require.Equal(t, "online", spScalar(t, pool, a.id, `SELECT status::text FROM machines WHERE id = $1`, voron))
	require.Equal(t, "idle|true|300|[{\"slot\": 1, \"class\": \"pla\", \"loaded\": true}, {\"slot\": 2, \"class\": null, \"loaded\": false}]",
		spScalar(t, pool, a.id, `SELECT state_status || '|' || online || '|' || (capabilities->>'max_hotend_temp_c') || '|' || material_slots::text
			FROM machine_live_state WHERE machine_id = $1`, voron))

	// Acks bind to the command AND the device: Command/* naming another
	// machine's command changes nothing.
	_, err = s.DeviceReport(ctx, *na, host.DeviceReport{DeviceID: "VORON-01", BdSeq: 4, Seq: 2, Timestamp: now, ReceivedAt: now,
		Values: map[sparkplug.MetricName]any{sparkplug.MetricCommandLastID: foreign.String(), sparkplug.MetricCommandStatus: "done"}})
	require.NoError(t, err)
	require.Equal(t, "sent", spScalar(t, pool, a.id, `SELECT status FROM task_commands WHERE command_id = $1`, foreign))

	_, err = s.DeviceReport(ctx, *na, host.DeviceReport{DeviceID: "VORON-01", BdSeq: 4, Seq: 3, Timestamp: now, ReceivedAt: now,
		Values: map[sparkplug.MetricName]any{sparkplug.MetricCommandLastID: startJob.String(), sparkplug.MetricCommandStatus: "running",
			sparkplug.MetricJobID: taskID.String(), sparkplug.MetricJobStatus: "printing", sparkplug.MetricStateStatus: "printing"}})
	require.NoError(t, err)
	require.Equal(t, "acknowledged", spScalar(t, pool, a.id, `SELECT status FROM task_commands WHERE command_id = $1`, startJob))

	printerAt := now.Add(-2 * time.Second)
	_, err = s.DeviceReport(ctx, *na, host.DeviceReport{DeviceID: "VORON-01", BdSeq: 4, Seq: 4, Timestamp: now, ReceivedAt: now,
		Values:      map[sparkplug.MetricName]any{sparkplug.MetricJobID: taskID.String(), sparkplug.MetricJobStatus: "complete"},
		MetricTimes: map[sparkplug.MetricName]time.Time{sparkplug.MetricJobStatus: printerAt}})
	require.NoError(t, err)
	require.Equal(t, "completed", spScalar(t, pool, a.id, `SELECT status FROM task_commands WHERE command_id = $1`, startJob))
	require.Equal(t, "quality_check", spScalar(t, pool, a.id, `SELECT status::text FROM tasks WHERE id = $1`, taskID))
	raw := spScalar(t, pool, a.id, `SELECT payload::text FROM event_outbox WHERE tenant_id = $1 AND event_type = 'machine.job_completed'`, a.id)
	var env struct {
		Data MachineJobCompletedData `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &env))
	require.Equal(t, startJob, env.Data.CommandID)
	require.Equal(t, taskID.String(), env.Data.JobID)
	require.Equal(t, "VORON-01", env.Data.MachineCode)
	require.True(t, env.Data.PrinterReportedAt.Equal(printerAt))
	require.Equal(t, "1", spScalar(t, pool, a.id, `SELECT count(*)::text FROM event_outbox WHERE tenant_id = $1 AND event_type = 'task.job_completed'`, a.id))

	// A repeated completion is not applied twice.
	_, err = s.DeviceReport(ctx, *na, host.DeviceReport{DeviceID: "VORON-01", BdSeq: 4, Seq: 5, Timestamp: now, ReceivedAt: now,
		Values: map[sparkplug.MetricName]any{sparkplug.MetricJobID: taskID.String(), sparkplug.MetricJobStatus: "complete"}})
	require.NoError(t, err)
	require.Equal(t, "1", spScalar(t, pool, a.id, `SELECT count(*)::text FROM event_outbox WHERE tenant_id = $1 AND event_type = 'machine.job_completed'`, a.id))

	// Unregistered device: quarantined, never a machine, counted per birth.
	for i := 0; i < 2; i++ {
		out, err = s.DeviceReport(ctx, *na, host.DeviceReport{DeviceID: "ROGUE-9", Birth: true, BdSeq: 4, Timestamp: now, ReceivedAt: now,
			Values: map[sparkplug.MetricName]any{sparkplug.MetricPropertiesModel: "Unknown"}})
		require.NoError(t, err)
		require.False(t, out.Registered)
	}
	require.Equal(t, "discovered|2|sparkplug", spScalar(t, pool, a.id,
		`SELECT status || '|' || birth_count || '|' || discovery_method FROM discovered_machines WHERE tenant_id = $1 AND device_id = 'ROGUE-9'`, a.id))
	require.Equal(t, "0", spScalar(t, pool, a.id, `SELECT count(*)::text FROM machines WHERE tenant_id = $1 AND code = 'ROGUE-9'`, a.id))

	// Tenant isolation: tenant B sees none of A's live state, and B's
	// same-coded machine was never touched.
	require.Equal(t, "0", spScalar(t, pool, b.id, `SELECT count(*)::text FROM machine_live_state`))
	require.Equal(t, "offline", spScalar(t, pool, b.id, `SELECT status::text FROM machines WHERE id = $1`, bVoron))

	// Deaths: a stale NDEATH does nothing; DDEATH and the current NDEATH do.
	require.NoError(t, s.Touch(ctx, *na, []string{"VORON-01"}, now))
	applied, err := s.NodeDeath(ctx, *na, 3, now)
	require.NoError(t, err)
	require.False(t, applied)
	require.Equal(t, "true", spScalar(t, pool, a.id, `SELECT online::text FROM machine_live_state WHERE machine_id = $1`, voron))
	require.NoError(t, s.DeviceDeath(ctx, *na, "VORON-01", now))
	require.Equal(t, "offline|false", spScalar(t, pool, a.id,
		`SELECT m.status::text || '|' || ls.online FROM machines m JOIN machine_live_state ls ON ls.machine_id = m.id WHERE m.id = $1`, voron))
	applied, err = s.NodeDeath(ctx, *na, 4, now)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, "false", spScalar(t, pool, a.id, `SELECT online::text FROM edge_nodes WHERE tenant_id = $1 AND edge_node_id = 'site-north'`, a.id))
	require.Equal(t, "2", spScalar(t, pool, a.id, `SELECT count(*)::text FROM event_outbox WHERE tenant_id = $1 AND event_type = 'machine.status_changed'`, a.id))
}
