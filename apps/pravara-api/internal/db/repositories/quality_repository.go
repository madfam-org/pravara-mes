// Package repositories provides database access layer implementations.
package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// QualityCertificateRepository handles quality certificate database operations.
type QualityCertificateRepository struct {
	db *sql.DB
}

// NewQualityCertificateRepository creates a new quality certificate repository.
func NewQualityCertificateRepository(db *sql.DB) *QualityCertificateRepository {
	return &QualityCertificateRepository{db: db}
}

// QualityCertificateFilter defines filtering options for listing certificates.
type QualityCertificateFilter struct {
	Type       *types.QualityCertType
	Status     *types.QualityCertStatus
	OrderID    *uuid.UUID
	TaskID     *uuid.UUID
	MachineID  *uuid.UUID
	BatchLotID *uuid.UUID
	FromDate   *time.Time
	ToDate     *time.Time
	Limit      int
	Offset     int
}

// List retrieves quality certificates matching the given filter with pagination.
// Results are ordered by created_at descending (most recent first).
// Supports filtering by type, status, and related entities (order, task, machine, batch lot).
// Returns the list of certificates, total count (for pagination), and any error encountered.
func (r *QualityCertificateRepository) List(ctx context.Context, filter QualityCertificateFilter) ([]types.QualityCertificate, int, error) {
	query := `
		SELECT id, tenant_id, certificate_number, type, status,
		       order_id, task_id, machine_id, batch_lot_id,
		       title, description, issued_date, expiry_date,
		       issued_by, approved_by, approved_at, document_url,
		       metadata, created_at, updated_at
		FROM quality_certificates
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM quality_certificates WHERE 1=1`

	var args []interface{}
	argIndex := 1

	if filter.Type != nil {
		query += fmt.Sprintf(" AND type = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND type = $%d", argIndex)
		args = append(args, *filter.Type)
		argIndex++
	}

	if filter.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND status = $%d", argIndex)
		args = append(args, *filter.Status)
		argIndex++
	}

	if filter.OrderID != nil {
		query += fmt.Sprintf(" AND order_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND order_id = $%d", argIndex)
		args = append(args, *filter.OrderID)
		argIndex++
	}

	if filter.TaskID != nil {
		query += fmt.Sprintf(" AND task_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND task_id = $%d", argIndex)
		args = append(args, *filter.TaskID)
		argIndex++
	}

	if filter.MachineID != nil {
		query += fmt.Sprintf(" AND machine_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND machine_id = $%d", argIndex)
		args = append(args, *filter.MachineID)
		argIndex++
	}

	if filter.BatchLotID != nil {
		query += fmt.Sprintf(" AND batch_lot_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND batch_lot_id = $%d", argIndex)
		args = append(args, *filter.BatchLotID)
		argIndex++
	}

	if filter.FromDate != nil {
		query += fmt.Sprintf(" AND issued_date >= $%d", argIndex)
		countQuery += fmt.Sprintf(" AND issued_date >= $%d", argIndex)
		args = append(args, *filter.FromDate)
		argIndex++
	}

	if filter.ToDate != nil {
		query += fmt.Sprintf(" AND issued_date <= $%d", argIndex)
		countQuery += fmt.Sprintf(" AND issued_date <= $%d", argIndex)
		args = append(args, *filter.ToDate)
		argIndex++
	}

	// Get total count
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count quality certificates: %w", err)
	}

	// Add ordering and pagination
	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIndex)
		args = append(args, filter.Limit)
		argIndex++
	}

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIndex)
		args = append(args, filter.Offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query quality certificates: %w", err)
	}
	defer rows.Close()

	var certificates []types.QualityCertificate
	for rows.Next() {
		cert, err := scanQualityCertificate(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan quality certificate: %w", err)
		}
		certificates = append(certificates, cert)
	}

	return certificates, total, nil
}

// GetByID retrieves a quality certificate by its unique identifier.
// Returns nil, nil if the certificate is not found (not an error condition).
// Returns nil, error if a database error occurs.
func (r *QualityCertificateRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.QualityCertificate, error) {
	query := `
		SELECT id, tenant_id, certificate_number, type, status,
		       order_id, task_id, machine_id, batch_lot_id,
		       title, description, issued_date, expiry_date,
		       issued_by, approved_by, approved_at, document_url,
		       metadata, created_at, updated_at
		FROM quality_certificates
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	cert, err := scanQualityCertificate(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get quality certificate: %w", err)
	}

	return &cert, nil
}

// Create inserts a new quality certificate into the database.
// If cert.ID is nil, a new UUID is generated automatically.
// The cert.CreatedAt and cert.UpdatedAt fields are populated from the database
// after successful insertion.
func (r *QualityCertificateRepository) Create(ctx context.Context, cert *types.QualityCertificate) error {
	query := `
		INSERT INTO quality_certificates (
			id, tenant_id, certificate_number, type, status,
			order_id, task_id, machine_id, batch_lot_id,
			title, description, issued_date, expiry_date,
			issued_by, approved_by, approved_at, document_url, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING created_at, updated_at
	`

	if cert.ID == uuid.Nil {
		cert.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(cert.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		cert.ID, cert.TenantID, cert.CertificateNumber, cert.Type, cert.Status,
		cert.OrderID, cert.TaskID, cert.MachineID, cert.BatchLotID,
		cert.Title, cert.Description, cert.IssuedDate, cert.ExpiryDate,
		cert.IssuedBy, cert.ApprovedBy, cert.ApprovedAt, cert.DocumentURL, metadataJSON,
	).Scan(&cert.CreatedAt, &cert.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create quality certificate: %w", err)
	}

	return nil
}

// Update modifies an existing quality certificate's mutable fields.
// The cert.ID must exist in the database. The cert.UpdatedAt field
// is refreshed from the database after successful update.
// Returns an error if the certificate is not found.
func (r *QualityCertificateRepository) Update(ctx context.Context, cert *types.QualityCertificate) error {
	query := `
		UPDATE quality_certificates SET
			status = $2,
			title = $3,
			description = $4,
			issued_date = $5,
			expiry_date = $6,
			issued_by = $7,
			approved_by = $8,
			approved_at = $9,
			document_url = $10,
			metadata = $11
		WHERE id = $1
		RETURNING updated_at
	`

	metadataJSON, _ := json.Marshal(cert.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		cert.ID, cert.Status, cert.Title, cert.Description,
		cert.IssuedDate, cert.ExpiryDate, cert.IssuedBy,
		cert.ApprovedBy, cert.ApprovedAt, cert.DocumentURL, metadataJSON,
	).Scan(&cert.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("quality certificate not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update quality certificate: %w", err)
	}

	return nil
}

// Delete permanently removes a quality certificate from the database.
// This is a hard delete - the certificate record is not recoverable.
// Returns an error if the certificate is not found.
func (r *QualityCertificateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM quality_certificates WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete quality certificate: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quality certificate not found")
	}

	return nil
}

// scanQualityCertificate is a helper to scan a quality certificate from a row.
func scanQualityCertificate(scanner interface {
	Scan(dest ...interface{}) error
}) (types.QualityCertificate, error) {
	var cert types.QualityCertificate
	var orderID, taskID, machineID, batchLotID sql.NullString
	var description, documentURL sql.NullString
	var issuedDate, expiryDate, approvedAt sql.NullTime
	var issuedBy, approvedBy sql.NullString
	var metadataJSON []byte

	err := scanner.Scan(
		&cert.ID, &cert.TenantID, &cert.CertificateNumber, &cert.Type, &cert.Status,
		&orderID, &taskID, &machineID, &batchLotID,
		&cert.Title, &description, &issuedDate, &expiryDate,
		&issuedBy, &approvedBy, &approvedAt, &documentURL,
		&metadataJSON, &cert.CreatedAt, &cert.UpdatedAt,
	)
	if err != nil {
		return cert, err
	}

	if orderID.Valid {
		id := uuid.MustParse(orderID.String)
		cert.OrderID = &id
	}
	if taskID.Valid {
		id := uuid.MustParse(taskID.String)
		cert.TaskID = &id
	}
	if machineID.Valid {
		id := uuid.MustParse(machineID.String)
		cert.MachineID = &id
	}
	if batchLotID.Valid {
		id := uuid.MustParse(batchLotID.String)
		cert.BatchLotID = &id
	}
	if description.Valid {
		cert.Description = description.String
	}
	if documentURL.Valid {
		cert.DocumentURL = documentURL.String
	}
	if issuedDate.Valid {
		cert.IssuedDate = &issuedDate.Time
	}
	if expiryDate.Valid {
		cert.ExpiryDate = &expiryDate.Time
	}
	if issuedBy.Valid {
		id := uuid.MustParse(issuedBy.String)
		cert.IssuedBy = &id
	}
	if approvedBy.Valid {
		id := uuid.MustParse(approvedBy.String)
		cert.ApprovedBy = &id
	}
	if approvedAt.Valid {
		cert.ApprovedAt = &approvedAt.Time
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &cert.Metadata)
	}

	return cert, nil
}

// InspectionRepository handles inspection database operations.
type InspectionRepository struct {
	db *sql.DB
}

// NewInspectionRepository creates a new inspection repository.
func NewInspectionRepository(db *sql.DB) *InspectionRepository {
	return &InspectionRepository{db: db}
}

// InspectionFilter defines filtering options for listing inspections.
type InspectionFilter struct {
	Type      *string
	Result    *types.InspectionResult
	OrderID   *uuid.UUID
	TaskID    *uuid.UUID
	MachineID *uuid.UUID
	FromDate  *time.Time
	ToDate    *time.Time
	Limit     int
	Offset    int
}

// List retrieves inspections matching the given filter with pagination.
// Results are ordered by created_at descending (most recent first).
// Supports filtering by type, result, and related entities (order, task, machine).
// Returns the list of inspections, total count (for pagination), and any error encountered.
func (r *InspectionRepository) List(ctx context.Context, filter InspectionFilter) ([]types.Inspection, int, error) {
	query := `
		SELECT id, tenant_id, inspection_number, order_id, task_id, machine_id,
		       type, scheduled_at, completed_at, inspector_id, result,
		       notes, checklist, certificate_id, metadata,
		       created_at, updated_at
		FROM inspections
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM inspections WHERE 1=1`

	var args []interface{}
	argIndex := 1

	if filter.Type != nil {
		query += fmt.Sprintf(" AND type = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND type = $%d", argIndex)
		args = append(args, *filter.Type)
		argIndex++
	}

	if filter.Result != nil {
		query += fmt.Sprintf(" AND result = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND result = $%d", argIndex)
		args = append(args, *filter.Result)
		argIndex++
	}

	if filter.OrderID != nil {
		query += fmt.Sprintf(" AND order_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND order_id = $%d", argIndex)
		args = append(args, *filter.OrderID)
		argIndex++
	}

	if filter.TaskID != nil {
		query += fmt.Sprintf(" AND task_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND task_id = $%d", argIndex)
		args = append(args, *filter.TaskID)
		argIndex++
	}

	if filter.MachineID != nil {
		query += fmt.Sprintf(" AND machine_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND machine_id = $%d", argIndex)
		args = append(args, *filter.MachineID)
		argIndex++
	}

	if filter.FromDate != nil {
		query += fmt.Sprintf(" AND scheduled_at >= $%d", argIndex)
		countQuery += fmt.Sprintf(" AND scheduled_at >= $%d", argIndex)
		args = append(args, *filter.FromDate)
		argIndex++
	}

	if filter.ToDate != nil {
		query += fmt.Sprintf(" AND scheduled_at <= $%d", argIndex)
		countQuery += fmt.Sprintf(" AND scheduled_at <= $%d", argIndex)
		args = append(args, *filter.ToDate)
		argIndex++
	}

	// Get total count
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count inspections: %w", err)
	}

	// Add ordering and pagination
	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIndex)
		args = append(args, filter.Limit)
		argIndex++
	}

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIndex)
		args = append(args, filter.Offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query inspections: %w", err)
	}
	defer rows.Close()

	var inspections []types.Inspection
	for rows.Next() {
		inspection, err := scanInspection(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan inspection: %w", err)
		}
		inspections = append(inspections, inspection)
	}

	return inspections, total, nil
}

// GetByID retrieves an inspection by its unique identifier.
// Returns nil, nil if the inspection is not found (not an error condition).
// Returns nil, error if a database error occurs.
func (r *InspectionRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.Inspection, error) {
	query := `
		SELECT id, tenant_id, inspection_number, order_id, task_id, machine_id,
		       type, scheduled_at, completed_at, inspector_id, result,
		       notes, checklist, certificate_id, metadata,
		       created_at, updated_at
		FROM inspections
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	inspection, err := scanInspection(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get inspection: %w", err)
	}

	return &inspection, nil
}

// Create inserts a new inspection into the database.
// If inspection.ID is nil, a new UUID is generated automatically.
// The inspection.CreatedAt and inspection.UpdatedAt fields are populated from the database
// after successful insertion.
func (r *InspectionRepository) Create(ctx context.Context, inspection *types.Inspection) error {
	query := `
		INSERT INTO inspections (
			id, tenant_id, inspection_number, order_id, task_id, machine_id,
			type, scheduled_at, completed_at, inspector_id, result,
			notes, checklist, certificate_id, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING created_at, updated_at
	`

	if inspection.ID == uuid.Nil {
		inspection.ID = uuid.New()
	}

	checklistJSON, _ := json.Marshal(inspection.Checklist)
	metadataJSON, _ := json.Marshal(inspection.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		inspection.ID, inspection.TenantID, inspection.InspectionNumber,
		inspection.OrderID, inspection.TaskID, inspection.MachineID,
		inspection.Type, inspection.ScheduledAt, inspection.CompletedAt,
		inspection.InspectorID, inspection.Result, inspection.Notes,
		checklistJSON, inspection.CertificateID, metadataJSON,
	).Scan(&inspection.CreatedAt, &inspection.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create inspection: %w", err)
	}

	return nil
}

// Update modifies an existing inspection's mutable fields.
// The inspection.ID must exist in the database. The inspection.UpdatedAt field
// is refreshed from the database after successful update.
// Returns an error if the inspection is not found.
func (r *InspectionRepository) Update(ctx context.Context, inspection *types.Inspection) error {
	query := `
		UPDATE inspections SET
			scheduled_at = $2,
			completed_at = $3,
			inspector_id = $4,
			result = $5,
			notes = $6,
			checklist = $7,
			certificate_id = $8,
			metadata = $9
		WHERE id = $1
		RETURNING updated_at
	`

	checklistJSON, _ := json.Marshal(inspection.Checklist)
	metadataJSON, _ := json.Marshal(inspection.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		inspection.ID, inspection.ScheduledAt, inspection.CompletedAt,
		inspection.InspectorID, inspection.Result, inspection.Notes,
		checklistJSON, inspection.CertificateID, metadataJSON,
	).Scan(&inspection.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("inspection not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update inspection: %w", err)
	}

	return nil
}

// Delete permanently removes an inspection from the database.
// This is a hard delete - the inspection record is not recoverable.
// Returns an error if the inspection is not found.
func (r *InspectionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM inspections WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete inspection: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("inspection not found")
	}

	return nil
}

// scanInspection is a helper to scan an inspection from a row.
func scanInspection(scanner interface {
	Scan(dest ...interface{}) error
}) (types.Inspection, error) {
	var inspection types.Inspection
	var orderID, taskID, machineID, certificateID, inspectorID sql.NullString
	var scheduledAt, completedAt sql.NullTime
	var notes sql.NullString
	var checklistJSON, metadataJSON []byte

	err := scanner.Scan(
		&inspection.ID, &inspection.TenantID, &inspection.InspectionNumber,
		&orderID, &taskID, &machineID, &inspection.Type,
		&scheduledAt, &completedAt, &inspectorID, &inspection.Result,
		&notes, &checklistJSON, &certificateID, &metadataJSON,
		&inspection.CreatedAt, &inspection.UpdatedAt,
	)
	if err != nil {
		return inspection, err
	}

	if orderID.Valid {
		id := uuid.MustParse(orderID.String)
		inspection.OrderID = &id
	}
	if taskID.Valid {
		id := uuid.MustParse(taskID.String)
		inspection.TaskID = &id
	}
	if machineID.Valid {
		id := uuid.MustParse(machineID.String)
		inspection.MachineID = &id
	}
	if certificateID.Valid {
		id := uuid.MustParse(certificateID.String)
		inspection.CertificateID = &id
	}
	if inspectorID.Valid {
		id := uuid.MustParse(inspectorID.String)
		inspection.InspectorID = &id
	}
	if scheduledAt.Valid {
		inspection.ScheduledAt = &scheduledAt.Time
	}
	if completedAt.Valid {
		inspection.CompletedAt = &completedAt.Time
	}
	if notes.Valid {
		inspection.Notes = notes.String
	}
	if len(checklistJSON) > 0 {
		json.Unmarshal(checklistJSON, &inspection.Checklist)
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &inspection.Metadata)
	}

	return inspection, nil
}

// BatchLotRepository handles batch lot database operations.
type BatchLotRepository struct {
	db *sql.DB
}

// NewBatchLotRepository creates a new batch lot repository.
func NewBatchLotRepository(db *sql.DB) *BatchLotRepository {
	return &BatchLotRepository{db: db}
}

// BatchLotFilter defines filtering options for listing batch lots.
type BatchLotFilter struct {
	Status      *string
	OrderID     *uuid.UUID
	ProductCode *string
	FromDate    *time.Time
	ToDate      *time.Time
	Limit       int
	Offset      int
}

// List retrieves batch lots matching the given filter with pagination.
// Results are ordered by created_at descending (most recent first).
// Supports filtering by status, product code, order, and date range.
// Returns the list of batch lots, total count (for pagination), and any error encountered.
func (r *BatchLotRepository) List(ctx context.Context, filter BatchLotFilter) ([]types.BatchLot, int, error) {
	query := `
		SELECT id, tenant_id, lot_number, product_name, product_code,
		       quantity, unit, manufactured_date, expiry_date, received_date,
		       supplier_name, supplier_lot_number, purchase_order,
		       status, order_id, metadata, created_at, updated_at
		FROM batch_lots
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM batch_lots WHERE 1=1`

	var args []interface{}
	argIndex := 1

	if filter.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND status = $%d", argIndex)
		args = append(args, *filter.Status)
		argIndex++
	}

	if filter.OrderID != nil {
		query += fmt.Sprintf(" AND order_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND order_id = $%d", argIndex)
		args = append(args, *filter.OrderID)
		argIndex++
	}

	if filter.ProductCode != nil {
		query += fmt.Sprintf(" AND product_code = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND product_code = $%d", argIndex)
		args = append(args, *filter.ProductCode)
		argIndex++
	}

	if filter.FromDate != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", argIndex)
		countQuery += fmt.Sprintf(" AND created_at >= $%d", argIndex)
		args = append(args, *filter.FromDate)
		argIndex++
	}

	if filter.ToDate != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", argIndex)
		countQuery += fmt.Sprintf(" AND created_at <= $%d", argIndex)
		args = append(args, *filter.ToDate)
		argIndex++
	}

	// Get total count
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count batch lots: %w", err)
	}

	// Add ordering and pagination
	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIndex)
		args = append(args, filter.Limit)
		argIndex++
	}

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIndex)
		args = append(args, filter.Offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query batch lots: %w", err)
	}
	defer rows.Close()

	var batchLots []types.BatchLot
	for rows.Next() {
		lot, err := scanBatchLot(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan batch lot: %w", err)
		}
		batchLots = append(batchLots, lot)
	}

	return batchLots, total, nil
}

// GetByID retrieves a batch lot by ID.
func (r *BatchLotRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.BatchLot, error) {
	query := `
		SELECT id, tenant_id, lot_number, product_name, product_code,
		       quantity, unit, manufactured_date, expiry_date, received_date,
		       supplier_name, supplier_lot_number, purchase_order,
		       status, order_id, metadata, created_at, updated_at
		FROM batch_lots
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	lot, err := scanBatchLot(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get batch lot: %w", err)
	}

	return &lot, nil
}

// Create inserts a new batch lot.
func (r *BatchLotRepository) Create(ctx context.Context, lot *types.BatchLot) error {
	query := `
		INSERT INTO batch_lots (
			id, tenant_id, lot_number, product_name, product_code,
			quantity, unit, manufactured_date, expiry_date, received_date,
			supplier_name, supplier_lot_number, purchase_order,
			status, order_id, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING created_at, updated_at
	`

	if lot.ID == uuid.Nil {
		lot.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(lot.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		lot.ID, lot.TenantID, lot.LotNumber, lot.ProductName, lot.ProductCode,
		lot.Quantity, lot.Unit, lot.ManufacturedDate, lot.ExpiryDate, lot.ReceivedDate,
		lot.SupplierName, lot.SupplierLotNumber, lot.PurchaseOrder,
		lot.Status, lot.OrderID, metadataJSON,
	).Scan(&lot.CreatedAt, &lot.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create batch lot: %w", err)
	}

	return nil
}

// Update modifies an existing batch lot.
func (r *BatchLotRepository) Update(ctx context.Context, lot *types.BatchLot) error {
	query := `
		UPDATE batch_lots SET
			product_name = $2,
			product_code = $3,
			quantity = $4,
			unit = $5,
			manufactured_date = $6,
			expiry_date = $7,
			received_date = $8,
			supplier_name = $9,
			supplier_lot_number = $10,
			purchase_order = $11,
			status = $12,
			metadata = $13
		WHERE id = $1
		RETURNING updated_at
	`

	metadataJSON, _ := json.Marshal(lot.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		lot.ID, lot.ProductName, lot.ProductCode, lot.Quantity, lot.Unit,
		lot.ManufacturedDate, lot.ExpiryDate, lot.ReceivedDate,
		lot.SupplierName, lot.SupplierLotNumber, lot.PurchaseOrder,
		lot.Status, metadataJSON,
	).Scan(&lot.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("batch lot not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update batch lot: %w", err)
	}

	return nil
}

// Delete removes a batch lot.
func (r *BatchLotRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM batch_lots WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete batch lot: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("batch lot not found")
	}

	return nil
}

// scanBatchLot is a helper to scan a batch lot from a row.
func scanBatchLot(scanner interface {
	Scan(dest ...interface{}) error
}) (types.BatchLot, error) {
	var lot types.BatchLot
	var productCode, supplierName, supplierLotNumber, purchaseOrder sql.NullString
	var manufacturedDate, expiryDate, receivedDate sql.NullTime
	var orderID sql.NullString
	var metadataJSON []byte

	err := scanner.Scan(
		&lot.ID, &lot.TenantID, &lot.LotNumber, &lot.ProductName, &productCode,
		&lot.Quantity, &lot.Unit, &manufacturedDate, &expiryDate, &receivedDate,
		&supplierName, &supplierLotNumber, &purchaseOrder,
		&lot.Status, &orderID, &metadataJSON,
		&lot.CreatedAt, &lot.UpdatedAt,
	)
	if err != nil {
		return lot, err
	}

	if productCode.Valid {
		lot.ProductCode = productCode.String
	}
	if supplierName.Valid {
		lot.SupplierName = supplierName.String
	}
	if supplierLotNumber.Valid {
		lot.SupplierLotNumber = supplierLotNumber.String
	}
	if purchaseOrder.Valid {
		lot.PurchaseOrder = purchaseOrder.String
	}
	if manufacturedDate.Valid {
		lot.ManufacturedDate = &manufacturedDate.Time
	}
	if expiryDate.Valid {
		lot.ExpiryDate = &expiryDate.Time
	}
	if receivedDate.Valid {
		lot.ReceivedDate = &receivedDate.Time
	}
	if orderID.Valid {
		id := uuid.MustParse(orderID.String)
		lot.OrderID = &id
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &lot.Metadata)
	}

	return lot, nil
}

// Ensure pq is imported for array handling
var _ = pq.Array
