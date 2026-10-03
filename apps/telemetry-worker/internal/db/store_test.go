package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/tenantctx"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

func setupTestDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	return db, mock
}

// expectTenantTx expects the start of a tenant-scoped transaction.
func expectTenantTx(mock sqlmock.Sqlmock, tenantID uuid.UUID) {
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT set_config\('app.current_tenant_id', \$1, true\)`).
		WithArgs(tenantID.String()).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

var machineCols = []string{
	"id", "tenant_id", "name", "code", "type", "description", "status",
	"mqtt_topic", "location", "specifications", "metadata",
	"last_heartbeat", "created_at", "updated_at",
}

func TestStore_CreateBatch_OneTransactionPerTenant(t *testing.T) {
	db, mock := setupTestDB(t)
	defer func() { _ = db.Close() }()
	store := newStoreWithDB(db)

	tenantA, tenantB := uuid.New(), uuid.New()
	ts := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	records := []types.Telemetry{
		{ID: uuid.New(), TenantID: tenantA, MachineID: uuid.New(), Timestamp: ts, MetricType: "temperature", Value: 45.2, Unit: "celsius", Metadata: map[string]interface{}{"sensor": "S001"}},
		{ID: uuid.New(), TenantID: tenantB, MachineID: uuid.New(), Timestamp: ts, MetricType: "power", Value: 1500, Unit: "watts"},
		{ID: uuid.New(), TenantID: tenantA, MachineID: uuid.New(), Timestamp: ts, MetricType: "power", Value: 10, Unit: "watts"},
	}

	expectInsert := func(r types.Telemetry) {
		meta, _ := json.Marshal(r.Metadata)
		mock.ExpectExec(`INSERT INTO telemetry (.+) ON CONFLICT \(id\) DO NOTHING`).
			WithArgs(r.ID, r.TenantID, r.MachineID, r.Timestamp, r.MetricType, r.Value, r.Unit, meta).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	expectTenantTx(mock, tenantA)
	expectInsert(records[0])
	expectInsert(records[2])
	mock.ExpectCommit()
	expectTenantTx(mock, tenantB)
	expectInsert(records[1])
	mock.ExpectCommit()

	if err := store.CreateBatch(context.Background(), records); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_CreateBatch_Empty(t *testing.T) {
	db, mock := setupTestDB(t)
	defer func() { _ = db.Close() }()
	store := newStoreWithDB(db)

	if err := store.CreateBatch(context.Background(), []types.Telemetry{}); err != nil {
		t.Errorf("expected no error for empty batch, got: %v", err)
	}
	if err := store.CreateBatch(context.Background(), nil); err != nil {
		t.Errorf("expected no error for nil batch, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_CreateBatch_RollsBackOnError(t *testing.T) {
	db, mock := setupTestDB(t)
	defer func() { _ = db.Close() }()
	store := newStoreWithDB(db)

	tenantID := uuid.New()
	records := []types.Telemetry{{ID: uuid.New(), TenantID: tenantID, MachineID: uuid.New(), Timestamp: time.Now(), MetricType: "temperature", Value: 1}}

	expectTenantTx(mock, tenantID)
	mock.ExpectExec("INSERT INTO telemetry").WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	if err := store.CreateBatch(context.Background(), records); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_CreateBatch_RejectsNilTenant(t *testing.T) {
	db, mock := setupTestDB(t)
	defer func() { _ = db.Close() }()
	store := newStoreWithDB(db)

	records := []types.Telemetry{{ID: uuid.New(), MachineID: uuid.New(), Timestamp: time.Now(), MetricType: "x"}}
	if err := store.CreateBatch(context.Background(), records); !errors.Is(err, ErrNoTenant) {
		t.Fatalf("expected ErrNoTenant, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unexpected database calls: %v", err)
	}
}

func TestStore_ResolveMachine_BySlugAndCode(t *testing.T) {
	db, mock := setupTestDB(t)
	defer func() { _ = db.Close() }()
	store := newStoreWithDB(db)

	tenantID, machineID := uuid.New(), uuid.New()
	code := "CNC-01"
	lastHeartbeat := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	specs, _ := json.Marshal(map[string]interface{}{"spindle_speed": 12000})
	meta, _ := json.Marshal(map[string]interface{}{"location": "floor-1"})

	mock.ExpectQuery(`SELECT id FROM tenants WHERE slug = \$1`).WithArgs("acme").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(tenantID))
	expectTenantTx(mock, tenantID)
	mock.ExpectQuery(`FROM machines WHERE tenant_id = \$1 AND code = \$2`).WithArgs(tenantID, code).
		WillReturnRows(sqlmock.NewRows(machineCols).AddRow(
			machineID, tenantID, "CNC Machine 01", code, "CNC", "Main CNC machine", "online",
			"acme/site/area/line/CNC-01", "Floor 1", specs, meta, lastHeartbeat, created, created))
	mock.ExpectCommit()

	machine, err := store.ResolveMachine(context.Background(), "acme", code)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if machine == nil || machine.ID != machineID || machine.TenantID != tenantID {
		t.Fatalf("unexpected machine: %+v", machine)
	}
	if machine.LastHeartbeat == nil || !machine.LastHeartbeat.Equal(lastHeartbeat) {
		t.Errorf("LastHeartbeat: got %v, want %v", machine.LastHeartbeat, lastHeartbeat)
	}

	// The tenant is cached: a second lookup goes straight to the machine query.
	expectTenantTx(mock, tenantID)
	mock.ExpectQuery(`FROM machines WHERE tenant_id = \$1 AND code = \$2`).WithArgs(tenantID, "OTHER").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectCommit()
	machine, err = store.ResolveMachine(context.Background(), "acme", "OTHER")
	if err != nil || machine != nil {
		t.Fatalf("expected nil machine for unknown code, got %+v, %v", machine, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_ResolveMachine_ByTenantUUID(t *testing.T) {
	db, mock := setupTestDB(t)
	defer func() { _ = db.Close() }()
	store := newStoreWithDB(db)

	tenantID := uuid.New()
	mock.ExpectQuery(`SELECT id FROM tenants WHERE id = \$1`).WithArgs(tenantID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(tenantID))
	expectTenantTx(mock, tenantID)
	mock.ExpectQuery(`FROM machines WHERE tenant_id = \$1 AND code = \$2`).WithArgs(tenantID, "M1").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectCommit()

	if _, err := store.ResolveMachine(context.Background(), tenantID.String(), "M1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_ResolveMachine_UnknownTenantIsDroppedAndCached(t *testing.T) {
	db, mock := setupTestDB(t)
	defer func() { _ = db.Close() }()
	store := newStoreWithDB(db)

	mock.ExpectQuery(`SELECT id FROM tenants WHERE slug = \$1`).WithArgs("nobody").
		WillReturnError(sql.ErrNoRows)

	for i := 0; i < 3; i++ {
		machine, err := store.ResolveMachine(context.Background(), "nobody", "M1")
		if err != nil || machine != nil {
			t.Fatalf("expected nil, nil for unknown tenant; got %+v, %v", machine, err)
		}
	}
	// Only one tenants lookup and no machine query: the miss is cached.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_ResolveMachine_NullableFields(t *testing.T) {
	db, mock := setupTestDB(t)
	defer func() { _ = db.Close() }()
	store := newStoreWithDB(db)

	tenantID, machineID := uuid.New(), uuid.New()
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT id FROM tenants WHERE slug = \$1`).WithArgs("acme").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(tenantID))
	expectTenantTx(mock, tenantID)
	mock.ExpectQuery(`FROM machines WHERE tenant_id`).
		WillReturnRows(sqlmock.NewRows(machineCols).AddRow(
			machineID, tenantID, "Simple Machine", "SIMPLE", "Generic",
			sql.NullString{}, "idle", sql.NullString{}, sql.NullString{},
			[]byte(nil), []byte(nil), sql.NullTime{}, created, created))
	mock.ExpectCommit()

	machine, err := store.ResolveMachine(context.Background(), "acme", "SIMPLE")
	if err != nil || machine == nil {
		t.Fatalf("expected machine, got %+v, %v", machine, err)
	}
	if machine.Description != "" || machine.MQTTTopic != "" || machine.Location != "" || machine.LastHeartbeat != nil {
		t.Errorf("expected empty nullable fields, got %+v", machine)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_UpdateMachineHeartbeat_TenantScoped(t *testing.T) {
	db, mock := setupTestDB(t)
	defer func() { _ = db.Close() }()
	store := newStoreWithDB(db)

	tenantID, machineID := uuid.New(), uuid.New()
	expectTenantTx(mock, tenantID)
	mock.ExpectExec(`UPDATE machines SET last_heartbeat = \$3, status = 'online' WHERE id = \$1 AND tenant_id = \$2`).
		WithArgs(machineID, tenantID, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := store.UpdateMachineHeartbeat(context.Background(), tenantID, machineID); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_AckPath_UsesTopicTenant(t *testing.T) {
	db, mock := setupTestDB(t)
	defer func() { _ = db.Close() }()
	store := newStoreWithDB(db)

	tenantID, commandID := uuid.New(), uuid.New()
	ctx := tenantctx.WithTopicSegment(context.Background(), "acme")

	mock.ExpectQuery(`SELECT id FROM tenants WHERE slug = \$1`).WithArgs("acme").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(tenantID))
	expectTenantTx(mock, tenantID)
	mock.ExpectExec(`UPDATE task_commands (.+) WHERE command_id = \$1 AND tenant_id = \$4`).
		WithArgs(commandID, "acknowledged", "", tenantID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := store.UpdateCommandStatus(ctx, commandID, "acknowledged", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_AckPath_WithoutTopicTenantFails(t *testing.T) {
	db, mock := setupTestDB(t)
	defer func() { _ = db.Close() }()
	store := newStoreWithDB(db)

	err := store.UpdateCommandStatus(context.Background(), uuid.New(), "acknowledged", "")
	if !errors.Is(err, ErrNoTenant) {
		t.Fatalf("expected ErrNoTenant, got %v", err)
	}
	info, err := store.GetMachineInfoByCode(context.Background(), "M1")
	if err != nil || info != nil {
		t.Fatalf("expected nil, nil without a tenant; got %+v, %v", info, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unexpected database calls: %v", err)
	}
}

func TestStore_Stats(t *testing.T) {
	db, _ := setupTestDB(t)
	defer func() { _ = db.Close() }()
	store := newStoreWithDB(db)
	_ = store.Stats()
}

func TestStore_Close(t *testing.T) {
	db, mock := setupTestDB(t)
	store := newStoreWithDB(db)
	mock.ExpectClose()
	if err := store.Close(); err != nil {
		t.Errorf("expected no error on close, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}
