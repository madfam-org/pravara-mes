package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// PostgreSQL-backed tests run only when PRAVARA_TEST_DATABASE_URL points at
// a throwaway database (it is migrated on first use). They are skipped
// otherwise, so CI without a database reports them as skipped, not passed.
const testDatabaseEnv = "PRAVARA_TEST_DATABASE_URL"

var schemaOnce sync.Once

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv(testDatabaseEnv)
	if url == "" {
		t.Skipf("%s not set; skipping PostgreSQL test", testDatabaseEnv)
	}
	db, err := sql.Open("postgres", url)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })

	var schemaErr error
	schemaOnce.Do(func() { schemaErr = ensureSchema(db) })
	require.NoError(t, schemaErr)
	return db
}

// ensureSchema applies the API's migrations when the database is empty.
func ensureSchema(db *sql.DB) error {
	var exists bool
	if err := db.QueryRow(`SELECT to_regclass('public.task_commands') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	dir := filepath.Join("..", "..", "..", "pravara-api", "internal", "db", "migrations")
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if _, err := db.Exec(string(body)); err != nil {
			return &migrationError{file: filepath.Base(f), err: err}
		}
	}
	return nil
}

type migrationError struct {
	file string
	err  error
}

func (e *migrationError) Error() string { return "migration " + e.file + ": " + e.err.Error() }

// fixture holds rows created for one test, scoped to a fresh tenant.
type fixture struct {
	db       *sql.DB
	tenantID uuid.UUID
}

func newFixture(t *testing.T, db *sql.DB) *fixture {
	t.Helper()
	f := &fixture{db: db, tenantID: uuid.New()}
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Test tenant', $2)`,
		f.tenantID, "t-"+f.tenantID.String())
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tenants WHERE id = $1`, f.tenantID) })
	return f
}

func (f *fixture) machine(t *testing.T, code, topic, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.db.Exec(`INSERT INTO machines (id, tenant_id, name, code, type, status, mqtt_topic)
		VALUES ($1, $2, $3, $4, '3d_printer', $5, NULLIF($6, ''))`,
		id, f.tenantID, "Machine "+code, code, status, topic)
	require.NoError(t, err)
	return id
}

func (f *fixture) order(t *testing.T, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.db.Exec(`INSERT INTO orders (id, tenant_id, customer_name, status) VALUES ($1, $2, 'Customer', $3)`,
		id, f.tenantID, status)
	require.NoError(t, err)
	return id
}

func (f *fixture) task(t *testing.T, orderID *uuid.UUID, machineID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.db.Exec(`INSERT INTO tasks (id, tenant_id, order_id, machine_id, title, status)
		VALUES ($1, $2, $3, $4, 'Print part', 'in_progress')`,
		id, f.tenantID, orderID, machineID)
	require.NoError(t, err)
	return id
}

func (f *fixture) command(t *testing.T, machineID uuid.UUID, taskID *uuid.UUID, cmdType, status string) uuid.UUID {
	t.Helper()
	commandID := uuid.New()
	_, err := f.db.Exec(`INSERT INTO task_commands (tenant_id, task_id, machine_id, command_id, command_type, status)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		f.tenantID, taskID, machineID, commandID, cmdType, status)
	require.NoError(t, err)
	return commandID
}

func (f *fixture) commandStatus(t *testing.T, commandID uuid.UUID) (status string, attempts int, errMsg string) {
	t.Helper()
	var msg sql.NullString
	require.NoError(t, f.db.QueryRow(`SELECT status, attempts, error_message FROM task_commands WHERE command_id = $1`,
		commandID).Scan(&status, &attempts, &msg))
	return status, attempts, msg.String
}

func (f *fixture) scalar(t *testing.T, query string, args ...interface{}) string {
	t.Helper()
	var v string
	require.NoError(t, f.db.QueryRow(query, args...).Scan(&v))
	return v
}

// outboxEvents returns this tenant's outbox events as type -> data payloads.
func (f *fixture) outboxEvents(t *testing.T) map[string][]map[string]interface{} {
	t.Helper()
	rows, err := f.db.QueryContext(context.Background(),
		`SELECT event_type, payload FROM event_outbox WHERE tenant_id = $1 ORDER BY created_at`, f.tenantID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	out := map[string][]map[string]interface{}{}
	for rows.Next() {
		var typ string
		var payload []byte
		require.NoError(t, rows.Scan(&typ, &payload))
		var env struct {
			Type     string                 `json:"type"`
			TenantID string                 `json:"tenant_id"`
			Data     map[string]interface{} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(payload, &env))
		require.Equal(t, typ, env.Type)
		require.True(t, strings.EqualFold(env.TenantID, f.tenantID.String()))
		out[typ] = append(out[typ], env.Data)
	}
	require.NoError(t, rows.Err())
	return out
}
