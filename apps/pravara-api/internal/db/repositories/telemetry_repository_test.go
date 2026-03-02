package repositories

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

func TestTelemetryRepository_List_NoFilters(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	telemetryID := uuid.New()
	tenantID := uuid.New()
	machineID := uuid.New()
	timestamp := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 3, 1, 12, 0, 5, 0, time.UTC)

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "machine_id", "timestamp", "metric_type", "value", "unit", "metadata", "created_at",
	}).AddRow(
		telemetryID, tenantID, machineID, timestamp, "temperature", 45.2, "celsius", []byte(`{"sensor":"S001"}`), createdAt,
	)

	mock.ExpectQuery("SELECT (.+) FROM telemetry WHERE 1=1 ORDER BY timestamp DESC").
		WillReturnRows(rows)

	ctx := context.Background()
	telemetry, err := repo.List(ctx, TelemetryFilter{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(telemetry) != 1 {
		t.Fatalf("telemetry count: got %d, want 1", len(telemetry))
	}
	if telemetry[0].ID != telemetryID {
		t.Errorf("ID: got %v, want %v", telemetry[0].ID, telemetryID)
	}
	if telemetry[0].MetricType != "temperature" {
		t.Errorf("MetricType: got %q, want %q", telemetry[0].MetricType, "temperature")
	}
	if telemetry[0].Value != 45.2 {
		t.Errorf("Value: got %f, want %f", telemetry[0].Value, 45.2)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_List_WithFilters(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	machineID := uuid.New()
	metricType := "power"
	fromTime := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	toTime := time.Date(2026, 3, 1, 23, 59, 59, 0, time.UTC)

	filter := TelemetryFilter{
		MachineID:  &machineID,
		MetricType: &metricType,
		FromTime:   &fromTime,
		ToTime:     &toTime,
		Limit:      100,
	}

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "machine_id", "timestamp", "metric_type", "value", "unit", "metadata", "created_at",
	})

	mock.ExpectQuery("SELECT (.+) FROM telemetry WHERE 1=1 AND machine_id = (.+) AND metric_type = (.+) AND timestamp >= (.+) AND timestamp <= (.+) ORDER BY timestamp DESC LIMIT (.+)").
		WithArgs(machineID, metricType, fromTime, toTime, 100).
		WillReturnRows(rows)

	ctx := context.Background()
	_, err := repo.List(ctx, filter)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_Create(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	telemetry := &types.Telemetry{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		MachineID:  uuid.New(),
		Timestamp:  time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		MetricType: "vibration",
		Value:      0.5,
		Unit:       "g",
		Metadata:   map[string]interface{}{"axis": "x"},
	}

	createdAt := time.Now()
	metadataJSON, _ := json.Marshal(telemetry.Metadata)

	mock.ExpectQuery("INSERT INTO telemetry").
		WithArgs(
			telemetry.ID, telemetry.TenantID, telemetry.MachineID,
			telemetry.Timestamp, telemetry.MetricType, telemetry.Value,
			telemetry.Unit, metadataJSON,
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(createdAt))

	ctx := context.Background()
	err := repo.Create(ctx, telemetry)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if telemetry.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_Create_GeneratesID(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	telemetry := &types.Telemetry{
		TenantID:   uuid.New(),
		MachineID:  uuid.New(),
		Timestamp:  time.Now(),
		MetricType: "current",
		Value:      15.5,
		Unit:       "amps",
	}

	createdAt := time.Now()

	mock.ExpectQuery("INSERT INTO telemetry").
		WithArgs(
			sqlmock.AnyArg(), // ID should be generated
			telemetry.TenantID, telemetry.MachineID,
			telemetry.Timestamp, telemetry.MetricType, telemetry.Value,
			telemetry.Unit, sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(createdAt))

	ctx := context.Background()
	err := repo.Create(ctx, telemetry)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if telemetry.ID == uuid.Nil {
		t.Error("expected ID to be generated")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_CreateBatch_Success(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

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

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO telemetry")

	metadata1, _ := json.Marshal(records[0].Metadata)
	mock.ExpectExec("INSERT INTO telemetry").
		WithArgs(
			records[0].ID, records[0].TenantID, records[0].MachineID,
			records[0].Timestamp, records[0].MetricType, records[0].Value,
			records[0].Unit, metadata1,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	metadata2, _ := json.Marshal(records[1].Metadata)
	mock.ExpectExec("INSERT INTO telemetry").
		WithArgs(
			records[1].ID, records[1].TenantID, records[1].MachineID,
			records[1].Timestamp, records[1].MetricType, records[1].Value,
			records[1].Unit, metadata2,
		).
		WillReturnResult(sqlmock.NewResult(2, 1))

	mock.ExpectCommit()

	ctx := context.Background()
	err := repo.CreateBatch(ctx, records)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_CreateBatch_Empty(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	ctx := context.Background()

	// Empty slice should not trigger any database operations
	err := repo.CreateBatch(ctx, []types.Telemetry{})
	if err != nil {
		t.Errorf("expected no error for empty batch, got: %v", err)
	}

	// Nil slice should not trigger any database operations
	err = repo.CreateBatch(ctx, nil)
	if err != nil {
		t.Errorf("expected no error for nil batch, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_GetLatest_Found(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	machineID := uuid.New()
	metricType := "temperature"
	telemetryID := uuid.New()
	tenantID := uuid.New()
	timestamp := time.Date(2026, 3, 1, 12, 30, 0, 0, time.UTC)
	createdAt := time.Date(2026, 3, 1, 12, 30, 5, 0, time.UTC)

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "machine_id", "timestamp", "metric_type", "value", "unit", "metadata", "created_at",
	}).AddRow(
		telemetryID, tenantID, machineID, timestamp, metricType, 47.5, "celsius", []byte(`{}`), createdAt,
	)

	mock.ExpectQuery("SELECT (.+) FROM telemetry WHERE machine_id = (.+) AND metric_type = (.+) ORDER BY timestamp DESC LIMIT 1").
		WithArgs(machineID, metricType).
		WillReturnRows(rows)

	ctx := context.Background()
	telemetry, err := repo.GetLatest(ctx, machineID, metricType)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if telemetry == nil {
		t.Fatal("expected telemetry, got nil")
	}
	if telemetry.ID != telemetryID {
		t.Errorf("ID: got %v, want %v", telemetry.ID, telemetryID)
	}
	if telemetry.Value != 47.5 {
		t.Errorf("Value: got %f, want %f", telemetry.Value, 47.5)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_GetLatest_NotFound(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	machineID := uuid.New()
	metricType := "pressure"

	mock.ExpectQuery("SELECT (.+) FROM telemetry WHERE machine_id = (.+) AND metric_type = (.+)").
		WithArgs(machineID, metricType).
		WillReturnError(sql.ErrNoRows)

	ctx := context.Background()
	telemetry, err := repo.GetLatest(ctx, machineID, metricType)
	if err != nil {
		t.Fatalf("expected no error for not found, got: %v", err)
	}

	if telemetry != nil {
		t.Errorf("expected nil telemetry, got: %v", telemetry)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_GetAggregated_Hourly(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	machineID := uuid.New()
	metricType := "power"
	fromTime := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	toTime := time.Date(2026, 3, 1, 23, 59, 59, 0, time.UTC)

	bucket1 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	bucket2 := time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows([]string{
		"bucket", "avg_value", "min_value", "max_value", "count",
	}).AddRow(
		bucket1, 1450.5, 1200.0, 1600.0, 60,
	).AddRow(
		bucket2, 1520.3, 1300.0, 1700.0, 58,
	)

	mock.ExpectQuery("SELECT (.+) FROM telemetry WHERE machine_id = (.+) AND metric_type = (.+) AND timestamp >= (.+) AND timestamp <= (.+) GROUP BY bucket ORDER BY bucket ASC").
		WithArgs(machineID, metricType, fromTime, toTime).
		WillReturnRows(rows)

	ctx := context.Background()
	results, err := repo.GetAggregated(ctx, machineID, metricType, fromTime, toTime, "hour")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("results count: got %d, want 2", len(results))
	}

	if results[0]["avg"].(float64) != 1450.5 {
		t.Errorf("avg: got %v, want 1450.5", results[0]["avg"])
	}
	if results[0]["count"].(int) != 60 {
		t.Errorf("count: got %v, want 60", results[0]["count"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_GetAggregated_Minute(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	machineID := uuid.New()
	metricType := "temperature"
	fromTime := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	toTime := time.Date(2026, 3, 1, 12, 59, 59, 0, time.UTC)

	rows := sqlmock.NewRows([]string{
		"bucket", "avg_value", "min_value", "max_value", "count",
	})

	mock.ExpectQuery("SELECT (.+) FROM telemetry").
		WithArgs(machineID, metricType, fromTime, toTime).
		WillReturnRows(rows)

	ctx := context.Background()
	_, err := repo.GetAggregated(ctx, machineID, metricType, fromTime, toTime, "minute")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_GetAggregated_Day(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	machineID := uuid.New()
	metricType := "uptime"
	fromTime := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	toTime := time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC)

	rows := sqlmock.NewRows([]string{
		"bucket", "avg_value", "min_value", "max_value", "count",
	})

	mock.ExpectQuery("SELECT (.+) FROM telemetry").
		WithArgs(machineID, metricType, fromTime, toTime).
		WillReturnRows(rows)

	ctx := context.Background()
	_, err := repo.GetAggregated(ctx, machineID, metricType, fromTime, toTime, "day")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_DeleteOlderThan(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	before := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectExec("DELETE FROM telemetry WHERE timestamp < (.+)").
		WithArgs(before).
		WillReturnResult(sqlmock.NewResult(0, 1523))

	ctx := context.Background()
	deleted, err := repo.DeleteOlderThan(ctx, before)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if deleted != 1523 {
		t.Errorf("deleted rows: got %d, want 1523", deleted)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_DeleteOlderThan_NoRows(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	before := time.Now()

	mock.ExpectExec("DELETE FROM telemetry WHERE timestamp < (.+)").
		WithArgs(before).
		WillReturnResult(sqlmock.NewResult(0, 0))

	ctx := context.Background()
	deleted, err := repo.DeleteOlderThan(ctx, before)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if deleted != 0 {
		t.Errorf("deleted rows: got %d, want 0", deleted)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_CreateBatch_GeneratesIDs(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	records := []types.Telemetry{
		{
			TenantID:   uuid.New(),
			MachineID:  uuid.New(),
			Timestamp:  time.Now(),
			MetricType: "voltage",
			Value:      220.0,
			Unit:       "volts",
		},
	}

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO telemetry")

	mock.ExpectExec("INSERT INTO telemetry").
		WithArgs(
			sqlmock.AnyArg(), // ID should be generated
			records[0].TenantID, records[0].MachineID,
			records[0].Timestamp, records[0].MetricType, records[0].Value,
			records[0].Unit, sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	ctx := context.Background()
	err := repo.CreateBatch(ctx, records)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if records[0].ID == uuid.Nil {
		t.Error("expected ID to be generated")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTelemetryRepository_List_WithMetadata(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewTelemetryRepository(db)

	telemetryID := uuid.New()
	tenantID := uuid.New()
	machineID := uuid.New()
	timestamp := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 3, 1, 12, 0, 5, 0, time.UTC)

	metadata := map[string]interface{}{
		"sensor_id": "S001",
		"location":  "spindle",
		"accuracy":  0.1,
	}
	metadataJSON, _ := json.Marshal(metadata)

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "machine_id", "timestamp", "metric_type", "value", "unit", "metadata", "created_at",
	}).AddRow(
		telemetryID, tenantID, machineID, timestamp, "temperature", 45.2, "celsius", metadataJSON, createdAt,
	)

	mock.ExpectQuery("SELECT (.+) FROM telemetry WHERE 1=1 ORDER BY timestamp DESC").
		WillReturnRows(rows)

	ctx := context.Background()
	telemetry, err := repo.List(ctx, TelemetryFilter{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(telemetry) != 1 {
		t.Fatalf("telemetry count: got %d, want 1", len(telemetry))
	}

	if telemetry[0].Metadata == nil {
		t.Fatal("expected metadata to be populated")
	}
	if telemetry[0].Metadata["sensor_id"] != "S001" {
		t.Errorf("metadata sensor_id: got %v, want S001", telemetry[0].Metadata["sensor_id"])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}
