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

func setupTestDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	return db, mock
}

// Quality Certificate Tests

func TestQualityCertificateRepository_List_NoFilters(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewQualityCertificateRepository(db)

	certID := uuid.New()
	tenantID := uuid.New()
	createdAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	// Expect count query
	countRows := sqlmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery("SELECT COUNT(.+) FROM quality_certificates").
		WillReturnRows(countRows)

	// Expect data query
	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "certificate_number", "type", "status",
		"order_id", "task_id", "machine_id", "batch_lot_id",
		"title", "description", "issued_date", "expiry_date",
		"issued_by", "approved_by", "approved_at", "document_url",
		"metadata", "created_at", "updated_at",
	}).AddRow(
		certID, tenantID, "CERT-001", "iso_9001", "active",
		sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
		"Quality Certificate", sql.NullString{}, sql.NullTime{}, sql.NullTime{},
		sql.NullString{}, sql.NullString{}, sql.NullTime{}, sql.NullString{},
		[]byte("{}"), createdAt, updatedAt,
	)

	mock.ExpectQuery("SELECT (.+) FROM quality_certificates").
		WillReturnRows(rows)

	ctx := context.Background()
	certs, total, err := repo.List(ctx, QualityCertificateFilter{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if total != 1 {
		t.Errorf("total: got %d, want 1", total)
	}
	if len(certs) != 1 {
		t.Fatalf("certificates count: got %d, want 1", len(certs))
	}
	if certs[0].ID != certID {
		t.Errorf("ID: got %v, want %v", certs[0].ID, certID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestQualityCertificateRepository_List_WithFilters(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewQualityCertificateRepository(db)

	orderID := uuid.New()
	certType := types.QualityCertTypeCOC
	status := types.QualityCertStatusApproved
	fromDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	toDate := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)

	filter := QualityCertificateFilter{
		Type:     &certType,
		Status:   &status,
		OrderID:  &orderID,
		FromDate: &fromDate,
		ToDate:   &toDate,
		Limit:    10,
		Offset:   0,
	}

	// Expect count query with filters
	countRows := sqlmock.NewRows([]string{"count"}).AddRow(5)
	mock.ExpectQuery("SELECT COUNT(.+) FROM quality_certificates WHERE 1=1 AND type = (.+) AND status = (.+) AND order_id = (.+) AND issued_date >= (.+) AND issued_date <= (.+)").
		WithArgs(certType, status, orderID, fromDate, toDate).
		WillReturnRows(countRows)

	// Expect data query with filters
	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "certificate_number", "type", "status",
		"order_id", "task_id", "machine_id", "batch_lot_id",
		"title", "description", "issued_date", "expiry_date",
		"issued_by", "approved_by", "approved_at", "document_url",
		"metadata", "created_at", "updated_at",
	})

	mock.ExpectQuery("SELECT (.+) FROM quality_certificates WHERE 1=1 AND type = (.+) AND status = (.+) AND order_id = (.+) AND issued_date >= (.+) AND issued_date <= (.+)").
		WithArgs(certType, status, orderID, fromDate, toDate, 10).
		WillReturnRows(rows)

	ctx := context.Background()
	_, total, err := repo.List(ctx, filter)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if total != 5 {
		t.Errorf("total: got %d, want 5", total)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestQualityCertificateRepository_GetByID_Found(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewQualityCertificateRepository(db)

	certID := uuid.New()
	tenantID := uuid.New()
	issuedDate := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "certificate_number", "type", "status",
		"order_id", "task_id", "machine_id", "batch_lot_id",
		"title", "description", "issued_date", "expiry_date",
		"issued_by", "approved_by", "approved_at", "document_url",
		"metadata", "created_at", "updated_at",
	}).AddRow(
		certID, tenantID, "CERT-001", "iso_9001", "active",
		sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
		"ISO 9001 Certificate", sql.NullString{Valid: true, String: "Quality certificate for product line"},
		sql.NullTime{Valid: true, Time: issuedDate}, sql.NullTime{},
		sql.NullString{}, sql.NullString{}, sql.NullTime{}, sql.NullString{Valid: true, String: "https://example.com/cert.pdf"},
		[]byte(`{"auditor":"John Doe"}`), createdAt, updatedAt,
	)

	mock.ExpectQuery("SELECT (.+) FROM quality_certificates WHERE id").
		WithArgs(certID).
		WillReturnRows(rows)

	ctx := context.Background()
	cert, err := repo.GetByID(ctx, certID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cert == nil {
		t.Fatal("expected certificate, got nil")
	}
	if cert.ID != certID {
		t.Errorf("ID: got %v, want %v", cert.ID, certID)
	}
	if cert.CertificateNumber != "CERT-001" {
		t.Errorf("CertificateNumber: got %q, want %q", cert.CertificateNumber, "CERT-001")
	}
	if cert.Description != "Quality certificate for product line" {
		t.Errorf("Description: got %q, want expected", cert.Description)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestQualityCertificateRepository_GetByID_NotFound(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewQualityCertificateRepository(db)

	certID := uuid.New()

	mock.ExpectQuery("SELECT (.+) FROM quality_certificates WHERE id").
		WithArgs(certID).
		WillReturnError(sql.ErrNoRows)

	ctx := context.Background()
	cert, err := repo.GetByID(ctx, certID)
	if err != nil {
		t.Fatalf("expected no error for not found, got: %v", err)
	}

	if cert != nil {
		t.Errorf("expected nil certificate, got: %v", cert)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestQualityCertificateRepository_Create(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewQualityCertificateRepository(db)

	cert := &types.QualityCertificate{
		ID:                uuid.New(),
		TenantID:          uuid.New(),
		CertificateNumber: "CERT-002",
		Type:              types.QualityCertTypeCOA,
		Status:            types.QualityCertStatusApproved,
		Title:             "Environmental Certificate",
		Metadata:          map[string]interface{}{"version": "2.0"},
	}

	createdAt := time.Now()
	updatedAt := time.Now()

	metadataJSON, _ := json.Marshal(cert.Metadata)

	mock.ExpectQuery("INSERT INTO quality_certificates").
		WithArgs(
			cert.ID, cert.TenantID, cert.CertificateNumber, cert.Type, cert.Status,
			cert.OrderID, cert.TaskID, cert.MachineID, cert.BatchLotID,
			cert.Title, cert.Description, cert.IssuedDate, cert.ExpiryDate,
			cert.IssuedBy, cert.ApprovedBy, cert.ApprovedAt, cert.DocumentURL, metadataJSON,
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(createdAt, updatedAt))

	ctx := context.Background()
	err := repo.Create(ctx, cert)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cert.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if cert.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestQualityCertificateRepository_Update(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewQualityCertificateRepository(db)

	cert := &types.QualityCertificate{
		ID:          uuid.New(),
		Status:      types.QualityCertStatusExpired,
		Title:       "Updated Certificate",
		Description: "Updated description",
	}

	updatedAt := time.Now()

	metadataJSON, _ := json.Marshal(cert.Metadata)

	mock.ExpectQuery("UPDATE quality_certificates SET").
		WithArgs(
			cert.ID, cert.Status, cert.Title, cert.Description,
			cert.IssuedDate, cert.ExpiryDate, cert.IssuedBy,
			cert.ApprovedBy, cert.ApprovedAt, cert.DocumentURL, metadataJSON,
		).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(updatedAt))

	ctx := context.Background()
	err := repo.Update(ctx, cert)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cert.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestQualityCertificateRepository_Update_NotFound(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewQualityCertificateRepository(db)

	cert := &types.QualityCertificate{
		ID:     uuid.New(),
		Status: types.QualityCertStatusApproved,
	}

	mock.ExpectQuery("UPDATE quality_certificates SET").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)

	ctx := context.Background()
	err := repo.Update(ctx, cert)
	if err == nil {
		t.Error("expected error for not found")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestQualityCertificateRepository_Delete(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewQualityCertificateRepository(db)

	certID := uuid.New()

	mock.ExpectExec("DELETE FROM quality_certificates WHERE id").
		WithArgs(certID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	ctx := context.Background()
	err := repo.Delete(ctx, certID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestQualityCertificateRepository_Delete_NotFound(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewQualityCertificateRepository(db)

	certID := uuid.New()

	mock.ExpectExec("DELETE FROM quality_certificates WHERE id").
		WithArgs(certID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	ctx := context.Background()
	err := repo.Delete(ctx, certID)
	if err == nil {
		t.Error("expected error for not found")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// Inspection Tests

func TestInspectionRepository_List_NoFilters(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewInspectionRepository(db)

	inspectionID := uuid.New()
	tenantID := uuid.New()
	createdAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	countRows := sqlmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery("SELECT COUNT(.+) FROM inspections").
		WillReturnRows(countRows)

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "inspection_number", "order_id", "task_id", "machine_id",
		"type", "scheduled_at", "completed_at", "inspector_id", "result",
		"notes", "checklist", "certificate_id", "metadata",
		"created_at", "updated_at",
	}).AddRow(
		inspectionID, tenantID, "INSP-001", sql.NullString{}, sql.NullString{}, sql.NullString{},
		"visual", sql.NullTime{}, sql.NullTime{}, sql.NullString{}, "pending",
		sql.NullString{}, []byte("[]"), sql.NullString{}, []byte("{}"),
		createdAt, updatedAt,
	)

	mock.ExpectQuery("SELECT (.+) FROM inspections").
		WillReturnRows(rows)

	ctx := context.Background()
	inspections, total, err := repo.List(ctx, InspectionFilter{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if total != 1 {
		t.Errorf("total: got %d, want 1", total)
	}
	if len(inspections) != 1 {
		t.Fatalf("inspections count: got %d, want 1", len(inspections))
	}
	if inspections[0].ID != inspectionID {
		t.Errorf("ID: got %v, want %v", inspections[0].ID, inspectionID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestInspectionRepository_GetByID_Found(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewInspectionRepository(db)

	inspectionID := uuid.New()
	tenantID := uuid.New()
	scheduledAt := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	checklist := []map[string]interface{}{
		{"item": "Check dimensions", "status": "pass"},
	}
	checklistJSON, _ := json.Marshal(checklist)

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "inspection_number", "order_id", "task_id", "machine_id",
		"type", "scheduled_at", "completed_at", "inspector_id", "result",
		"notes", "checklist", "certificate_id", "metadata",
		"created_at", "updated_at",
	}).AddRow(
		inspectionID, tenantID, "INSP-001", sql.NullString{}, sql.NullString{}, sql.NullString{},
		"dimensional", sql.NullTime{Valid: true, Time: scheduledAt}, sql.NullTime{}, sql.NullString{}, "pending",
		sql.NullString{Valid: true, String: "Inspection notes"}, checklistJSON, sql.NullString{}, []byte("{}"),
		createdAt, updatedAt,
	)

	mock.ExpectQuery("SELECT (.+) FROM inspections WHERE id").
		WithArgs(inspectionID).
		WillReturnRows(rows)

	ctx := context.Background()
	inspection, err := repo.GetByID(ctx, inspectionID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if inspection == nil {
		t.Fatal("expected inspection, got nil")
	}
	if inspection.ID != inspectionID {
		t.Errorf("ID: got %v, want %v", inspection.ID, inspectionID)
	}
	if inspection.Type != "dimensional" {
		t.Errorf("Type: got %q, want %q", inspection.Type, "dimensional")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestInspectionRepository_Create(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewInspectionRepository(db)

	inspection := &types.Inspection{
		ID:               uuid.New(),
		TenantID:         uuid.New(),
		InspectionNumber: "INSP-002",
		Type:             "final",
		Result:           types.InspectionResultPending,
		Checklist:        []any{map[string]interface{}{"item": "test"}},
		Metadata:         map[string]interface{}{"priority": "high"},
	}

	createdAt := time.Now()
	updatedAt := time.Now()

	checklistJSON, _ := json.Marshal(inspection.Checklist)
	metadataJSON, _ := json.Marshal(inspection.Metadata)

	mock.ExpectQuery("INSERT INTO inspections").
		WithArgs(
			inspection.ID, inspection.TenantID, inspection.InspectionNumber,
			inspection.OrderID, inspection.TaskID, inspection.MachineID,
			inspection.Type, inspection.ScheduledAt, inspection.CompletedAt,
			inspection.InspectorID, inspection.Result, inspection.Notes,
			checklistJSON, inspection.CertificateID, metadataJSON,
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(createdAt, updatedAt))

	ctx := context.Background()
	err := repo.Create(ctx, inspection)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if inspection.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestInspectionRepository_Update(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewInspectionRepository(db)

	inspection := &types.Inspection{
		ID:     uuid.New(),
		Result: types.InspectionResultPass,
		Notes:  "Inspection completed successfully",
	}

	updatedAt := time.Now()

	checklistJSON, _ := json.Marshal(inspection.Checklist)
	metadataJSON, _ := json.Marshal(inspection.Metadata)

	mock.ExpectQuery("UPDATE inspections SET").
		WithArgs(
			inspection.ID, inspection.ScheduledAt, inspection.CompletedAt,
			inspection.InspectorID, inspection.Result, inspection.Notes,
			checklistJSON, inspection.CertificateID, metadataJSON,
		).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(updatedAt))

	ctx := context.Background()
	err := repo.Update(ctx, inspection)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// BatchLot Tests

func TestBatchLotRepository_List_NoFilters(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewBatchLotRepository(db)

	lotID := uuid.New()
	tenantID := uuid.New()
	createdAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	countRows := sqlmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery("SELECT COUNT(.+) FROM batch_lots").
		WillReturnRows(countRows)

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "lot_number", "product_name", "product_code",
		"quantity", "unit", "manufactured_date", "expiry_date", "received_date",
		"supplier_name", "supplier_lot_number", "purchase_order",
		"status", "order_id", "metadata", "created_at", "updated_at",
	}).AddRow(
		lotID, tenantID, "LOT-001", "Steel Sheets", sql.NullString{Valid: true, String: "SS-304"},
		100.0, "kg", sql.NullTime{}, sql.NullTime{}, sql.NullTime{},
		sql.NullString{}, sql.NullString{}, sql.NullString{},
		"received", sql.NullString{}, []byte("{}"), createdAt, updatedAt,
	)

	mock.ExpectQuery("SELECT (.+) FROM batch_lots").
		WillReturnRows(rows)

	ctx := context.Background()
	lots, total, err := repo.List(ctx, BatchLotFilter{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if total != 1 {
		t.Errorf("total: got %d, want 1", total)
	}
	if len(lots) != 1 {
		t.Fatalf("lots count: got %d, want 1", len(lots))
	}
	if lots[0].ID != lotID {
		t.Errorf("ID: got %v, want %v", lots[0].ID, lotID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestBatchLotRepository_GetByID_Found(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewBatchLotRepository(db)

	lotID := uuid.New()
	tenantID := uuid.New()
	manufacturedDate := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "lot_number", "product_name", "product_code",
		"quantity", "unit", "manufactured_date", "expiry_date", "received_date",
		"supplier_name", "supplier_lot_number", "purchase_order",
		"status", "order_id", "metadata", "created_at", "updated_at",
	}).AddRow(
		lotID, tenantID, "LOT-001", "Aluminum Rods", sql.NullString{Valid: true, String: "AL-6061"},
		500.0, "pieces", sql.NullTime{Valid: true, Time: manufacturedDate}, sql.NullTime{}, sql.NullTime{},
		sql.NullString{Valid: true, String: "ACME Corp"}, sql.NullString{}, sql.NullString{},
		"inspected", sql.NullString{}, []byte(`{"inspector":"Jane"}`), createdAt, updatedAt,
	)

	mock.ExpectQuery("SELECT (.+) FROM batch_lots WHERE id").
		WithArgs(lotID).
		WillReturnRows(rows)

	ctx := context.Background()
	lot, err := repo.GetByID(ctx, lotID)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if lot == nil {
		t.Fatal("expected lot, got nil")
	}
	if lot.ID != lotID {
		t.Errorf("ID: got %v, want %v", lot.ID, lotID)
	}
	if lot.ProductName != "Aluminum Rods" {
		t.Errorf("ProductName: got %q, want %q", lot.ProductName, "Aluminum Rods")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestBatchLotRepository_Create(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewBatchLotRepository(db)

	lot := &types.BatchLot{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		LotNumber:   "LOT-002",
		ProductName: "Copper Wire",
		Quantity:    1000.0,
		Unit:        "meters",
		Status:      "pending",
		Metadata:    map[string]interface{}{"gauge": "14"},
	}

	createdAt := time.Now()
	updatedAt := time.Now()

	metadataJSON, _ := json.Marshal(lot.Metadata)

	mock.ExpectQuery("INSERT INTO batch_lots").
		WithArgs(
			lot.ID, lot.TenantID, lot.LotNumber, lot.ProductName, lot.ProductCode,
			lot.Quantity, lot.Unit, lot.ManufacturedDate, lot.ExpiryDate, lot.ReceivedDate,
			lot.SupplierName, lot.SupplierLotNumber, lot.PurchaseOrder,
			lot.Status, lot.OrderID, metadataJSON,
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(createdAt, updatedAt))

	ctx := context.Background()
	err := repo.Create(ctx, lot)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if lot.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestBatchLotRepository_Update(t *testing.T) {
	db, mock := setupTestDB(t)
	defer db.Close()

	repo := NewBatchLotRepository(db)

	lot := &types.BatchLot{
		ID:          uuid.New(),
		ProductName: "Updated Product",
		Status:      "approved",
	}

	updatedAt := time.Now()

	metadataJSON, _ := json.Marshal(lot.Metadata)

	mock.ExpectQuery("UPDATE batch_lots SET").
		WithArgs(
			lot.ID, lot.ProductName, lot.ProductCode, lot.Quantity, lot.Unit,
			lot.ManufacturedDate, lot.ExpiryDate, lot.ReceivedDate,
			lot.SupplierName, lot.SupplierLotNumber, lot.PurchaseOrder,
			lot.Status, metadataJSON,
		).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(updatedAt))

	ctx := context.Background()
	err := repo.Update(ctx, lot)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}
