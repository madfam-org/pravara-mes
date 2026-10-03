package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const setTenantPattern = `SELECT set_config\('app.current_tenant_id', \$1, true\)`

func TestTenantDB_SetsTenantAsFirstStatementOfTransaction(t *testing.T) {
	pool, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	tenantID := uuid.New().String()
	mock.ExpectBegin()
	mock.ExpectExec(setTenantPattern).WithArgs(tenantID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"x"}).AddRow(1))
	mock.ExpectExec("UPDATE machines").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tdb := NewTenantDB(pool, nil)
	ctx, scope, err := NewTenantScope(context.Background(), pool, tenantID)
	require.NoError(t, err)

	var x int
	require.NoError(t, tdb.QueryRowContext(ctx, "SELECT 1").Scan(&x))
	_, err = tdb.ExecContext(ctx, "UPDATE machines SET status = 'idle'")
	require.NoError(t, err)
	require.NoError(t, scope.Commit())
	assert.False(t, scope.Active())

	got, ok := TenantIDFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, tenantID, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantDB_NoScopeFailsWithoutTouchingTheDatabase(t *testing.T) {
	pool, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	tdb := NewTenantDB(pool, nil)

	_, err = tdb.ExecContext(context.Background(), "DELETE FROM machines")
	assert.ErrorIs(t, err, ErrNoTenantScope)

	_, err = tdb.QueryContext(context.Background(), "SELECT * FROM machines")
	assert.ErrorIs(t, err, ErrNoTenantScope)

	var x int
	assert.Error(t, tdb.QueryRowContext(context.Background(), "SELECT 1").Scan(&x))

	assert.ErrorIs(t, InTx(context.Background(), tdb, func(Querier) error { return nil }), ErrNoTenantScope)
	assert.NoError(t, mock.ExpectationsWereMet(), "no statement may reach the database")
}

func TestNewTenantScope_RejectsInvalidTenant(t *testing.T) {
	for _, bad := range []string{"", "not-a-uuid", "x'; RESET ALL; --"} {
		_, _, err := NewTenantScope(context.Background(), nil, bad)
		assert.Error(t, err, bad)
	}
}

func TestRunInTenantTx_RollsBackOnError(t *testing.T) {
	pool, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	tenantID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(setTenantPattern).WithArgs(tenantID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO telemetry").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	tdb := NewTenantDB(pool, nil)
	boom := errors.New("boom")
	err = RunInTenantTx(context.Background(), pool, tenantID, func(ctx context.Context) error {
		if _, err := tdb.ExecContext(ctx, "INSERT INTO telemetry DEFAULT VALUES"); err != nil {
			return err
		}
		return boom
	})
	assert.ErrorIs(t, err, boom)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRunInSystemScope_SetsSystemScopeAndRequiresPurpose(t *testing.T) {
	pool, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	assert.Error(t, RunInSystemScope(context.Background(), pool, " ", func(context.Context) error { return nil }))

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app.system_scope', 'on', true\)`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("FROM event_outbox").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectCommit()

	tdb := NewTenantDB(pool, nil)
	err = RunInSystemScope(context.Background(), pool, "test.discovery", func(ctx context.Context) error {
		_, ok := TenantIDFromContext(ctx)
		assert.False(t, ok, "system scope carries no tenant")
		rows, err := tdb.QueryContext(ctx, "SELECT id FROM event_outbox")
		if err != nil {
			return err
		}
		return rows.Close()
	})
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSavepoint_FailureKeepsOuterTransactionUsable(t *testing.T) {
	pool, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	tenantID := uuid.New().String()

	mock.ExpectBegin()
	mock.ExpectExec(setTenantPattern).WithArgs(tenantID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SAVEPOINT tenant_sp_1").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO event_outbox").WillReturnError(errors.New("insert failed"))
	mock.ExpectExec("ROLLBACK TO SAVEPOINT tenant_sp_1").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE orders").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tdb := NewTenantDB(pool, nil)
	err = RunInTenantTx(context.Background(), pool, tenantID, func(ctx context.Context) error {
		spErr := Savepoint(ctx, tdb, func(q Querier) error {
			_, err := q.ExecContext(ctx, "INSERT INTO event_outbox DEFAULT VALUES")
			return err
		})
		assert.Error(t, spErr)
		_, err := tdb.ExecContext(ctx, "UPDATE orders SET status = 'validated'")
		return err
	})
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestNoPoolLevelTenantSetting is a static check: tenant context must never
// again be set on a pooled connection with a plain SET.
func TestNoPoolLevelTenantSetting(t *testing.T) {
	apps, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	roots := []string{filepath.Join(apps, "pravara-api"), filepath.Join(apps, "telemetry-worker")}
	forbidden := regexp.MustCompile(`(?i)\bSET\s+(SESSION\s+)?app\.(current_tenant_id|tenant_id|current_tenant)\b|SetTenantID\(|ClearTenantID\(|set_config\([^)]*,\s*false\s*\)`)

	checked := 0
	for _, r := range roots {
		err := filepath.WalkDir(r, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == "migrations" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			checked++
			if loc := forbidden.FindIndex(src); loc != nil {
				t.Errorf("%s: pool-level tenant setting %q", path, src[loc[0]:loc[1]])
			}
			return nil
		})
		require.NoError(t, err)
	}
	assert.Greater(t, checked, 50, "the static check must actually scan the sources")
}
