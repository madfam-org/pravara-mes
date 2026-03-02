package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

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

func TestStore_CreateBatch_Success(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	store := &Store{db: db}

	tenantID := uuid.New()
	machineID := uuid.New()
	timestamp := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	records := []types.Telemetry{
		{
			ID:         uuid.New(),
			TenantID:   tenantID,
			MachineID:  machineID,
			Timestamp:  timestamp,
			MetricType: "temperature",
			Value:      45.2,
			Unit:       "celsius",
			Metadata:   map[string]interface{}{"sensor": "S001"},
		},
		{
			ID:         uuid.New(),
			TenantID:   tenantID,
			MachineID:  machineID,
			Timestamp:  timestamp.Add(1 * time.Minute),
			MetricType: "power",
			Value:      1500.0,
			Unit:       "watts",
			Metadata:   map[string]interface{}{"phase": "A"},
		},
	}

	// Expect transaction begin
	mock.ExpectBegin()

	// Expect prepare statement
	mock.ExpectPrepare("INSERT INTO telemetry")

	// Expect first record insert
	metadata1, _ := json.Marshal(records[0].Metadata)
	mock.ExpectExec("INSERT INTO telemetry").
		WithArgs(
			records[0].ID, records[0].TenantID, records[0].MachineID,
			records[0].Timestamp, records[0].MetricType, records[0].Value,
			records[0].Unit, metadata1,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Expect second record insert
	metadata2, _ := json.Marshal(records[1].Metadata)
	mock.ExpectExec("INSERT INTO telemetry").
		WithArgs(
			records[1].ID, records[1].TenantID, records[1].MachineID,
			records[1].Timestamp, records[1].MetricType, records[1].Value,
			records[1].Unit, metadata2,
		).
		WillReturnResult(sqlmock.NewResult(2, 1))

	// Expect transaction commit
	mock.ExpectCommit()

	ctx := context.Background()
	err := store.CreateBatch(ctx, records)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_CreateBatch_Empty(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	store := &Store{db: db}

	ctx := context.Background()

	// Empty slice should not trigger any database operations
	err := store.CreateBatch(ctx, []types.Telemetry{})
	if err != nil {
		t.Errorf("expected no error for empty batch, got: %v", err)
	}

	// Nil slice should not trigger any database operations
	err = store.CreateBatch(ctx, nil)
	if err != nil {
		t.Errorf("expected no error for nil batch, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_GetMachineByCode_Found(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	store := &Store{db: db}

	machineID := uuid.New()
	tenantID := uuid.New()
	code := "CNC-01"
	lastHeartbeat := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC)

	specifications := map[string]interface{}{"spindle_speed": 12000}
	metadata := map[string]interface{}{"location": "floor-1"}
	specificationsJSON, _ := json.Marshal(specifications)
	metadataJSON, _ := json.Marshal(metadata)

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "name", "code", "type", "description", "status",
		"mqtt_topic", "location", "specifications", "metadata",
		"last_heartbeat", "created_at", "updated_at",
	}).AddRow(
		machineID, tenantID, "CNC Machine 01", code, "CNC",
		"Main CNC machine", "online", "factory/cnc-01", "Floor 1",
		specificationsJSON, metadataJSON, lastHeartbeat, createdAt, updatedAt,
	)

	mock.ExpectQuery("SELECT (.+) FROM machines WHERE code").
		WithArgs(code).
		WillReturnRows(rows)

	ctx := context.Background()
	machine, err := store.GetMachineByCode(ctx, code)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if machine == nil {
		t.Fatal("expected machine, got nil")
	}

	if machine.ID != machineID {
		t.Errorf("ID: got %v, want %v", machine.ID, machineID)
	}
	if machine.Code != code {
		t.Errorf("Code: got %q, want %q", machine.Code, code)
	}
	if machine.Name != "CNC Machine 01" {
		t.Errorf("Name: got %q, want %q", machine.Name, "CNC Machine 01")
	}
	if machine.Status != "online" {
		t.Errorf("Status: got %q, want %q", machine.Status, "online")
	}
	if machine.LastHeartbeat == nil || !machine.LastHeartbeat.Equal(lastHeartbeat) {
		t.Errorf("LastHeartbeat: got %v, want %v", machine.LastHeartbeat, lastHeartbeat)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_GetMachineByCode_NotFound(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	store := &Store{db: db}

	code := "NONEXISTENT"

	mock.ExpectQuery("SELECT (.+) FROM machines WHERE code").
		WithArgs(code).
		WillReturnError(sql.ErrNoRows)

	ctx := context.Background()
	machine, err := store.GetMachineByCode(ctx, code)
	if err != nil {
		t.Fatalf("expected no error for not found, got: %v", err)
	}

	if machine != nil {
		t.Errorf("expected nil machine, got: %v", machine)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_GetMachineByID_Found(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	store := &Store{db: db}

	machineID := uuid.New()
	tenantID := uuid.New()
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "name", "code", "type", "description", "status",
		"mqtt_topic", "location", "specifications", "metadata",
		"last_heartbeat", "created_at", "updated_at",
	}).AddRow(
		machineID, tenantID, "Machine 01", "M-01", "CNC",
		sql.NullString{}, "offline", sql.NullString{}, sql.NullString{},
		[]byte("{}"), []byte("{}"), sql.NullTime{}, createdAt, updatedAt,
	)

	mock.ExpectQuery("SELECT (.+) FROM machines WHERE id").
		WithArgs(machineID).
		WillReturnRows(rows)

	ctx := context.Background()
	machine, err := store.GetMachineByID(ctx, machineID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if machine == nil {
		t.Fatal("expected machine, got nil")
	}

	if machine.ID != machineID {
		t.Errorf("ID: got %v, want %v", machine.ID, machineID)
	}
	if machine.Status != "offline" {
		t.Errorf("Status: got %q, want %q", machine.Status, "offline")
	}
	if machine.LastHeartbeat != nil {
		t.Errorf("expected nil LastHeartbeat, got %v", machine.LastHeartbeat)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_UpdateMachineHeartbeat_Success(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	store := &Store{db: db}

	machineID := uuid.New()

	mock.ExpectExec("UPDATE machines SET last_heartbeat").
		WithArgs(machineID, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	ctx := context.Background()
	err := store.UpdateMachineHeartbeat(ctx, machineID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_CreateBatch_TransactionRollback(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	store := &Store{db: db}

	records := []types.Telemetry{
		{
			ID:         uuid.New(),
			TenantID:   uuid.New(),
			MachineID:  uuid.New(),
			Timestamp:  time.Now(),
			MetricType: "temperature",
			Value:      45.2,
			Unit:       "celsius",
		},
	}

	// Expect transaction begin
	mock.ExpectBegin()

	// Expect prepare to fail
	mock.ExpectPrepare("INSERT INTO telemetry").
		WillReturnError(sql.ErrConnDone)

	// Expect rollback
	mock.ExpectRollback()

	ctx := context.Background()
	err := store.CreateBatch(ctx, records)
	if err == nil {
		t.Error("expected error, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_GetMachineByCode_NullableFields(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	store := &Store{db: db}

	machineID := uuid.New()
	tenantID := uuid.New()
	code := "SIMPLE-MACHINE"
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC)

	// Machine with all nullable fields as NULL
	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "name", "code", "type", "description", "status",
		"mqtt_topic", "location", "specifications", "metadata",
		"last_heartbeat", "created_at", "updated_at",
	}).AddRow(
		machineID, tenantID, "Simple Machine", code, "Generic",
		sql.NullString{Valid: false}, "idle",
		sql.NullString{Valid: false}, sql.NullString{Valid: false},
		[]byte(nil), []byte(nil), sql.NullTime{Valid: false}, createdAt, updatedAt,
	)

	mock.ExpectQuery("SELECT (.+) FROM machines WHERE code").
		WithArgs(code).
		WillReturnRows(rows)

	ctx := context.Background()
	machine, err := store.GetMachineByCode(ctx, code)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if machine == nil {
		t.Fatal("expected machine, got nil")
	}

	if machine.Description != "" {
		t.Errorf("Description: expected empty, got %q", machine.Description)
	}
	if machine.MQTTTopic != "" {
		t.Errorf("MQTTTopic: expected empty, got %q", machine.MQTTTopic)
	}
	if machine.Location != "" {
		t.Errorf("Location: expected empty, got %q", machine.Location)
	}
	if machine.LastHeartbeat != nil {
		t.Errorf("LastHeartbeat: expected nil, got %v", machine.LastHeartbeat)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestStore_Stats(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()

	store := &Store{db: db}

	stats := store.Stats()

	// Verify we get stats structure
	if stats.MaxOpenConnections != 0 {
		// Stats should have some fields available
		t.Logf("Stats: %+v", stats)
	}
}

func TestStore_Close(t *testing.T) {
	db, mock := setupTestDB(t)

	store := &Store{db: db}

	// Expect close call
	mock.ExpectClose()

	err := store.Close()
	if err != nil {
		t.Errorf("expected no error on close, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}
