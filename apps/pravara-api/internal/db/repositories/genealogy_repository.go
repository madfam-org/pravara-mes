// Package repositories provides database access layer implementations.
package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// GenealogyStatus represents the status of a product genealogy record.
type GenealogyStatus string

const (
	GenealogyStatusDraft      GenealogyStatus = "draft"
	GenealogyStatusInProgress GenealogyStatus = "in_progress"
	GenealogyStatusCompleted  GenealogyStatus = "completed"
	GenealogyStatusSealed     GenealogyStatus = "sealed"
)

// ProductGenealogy represents a product genealogy (birth certificate) record.
type ProductGenealogy struct {
	ID                  uuid.UUID      `json:"id"`
	TenantID            uuid.UUID      `json:"tenant_id"`
	ProductDefinitionID *uuid.UUID     `json:"product_definition_id,omitempty"`
	OrderID             *uuid.UUID     `json:"order_id,omitempty"`
	OrderItemID         *uuid.UUID     `json:"order_item_id,omitempty"`
	TaskID              *uuid.UUID     `json:"task_id,omitempty"`
	MachineID           *uuid.UUID     `json:"machine_id,omitempty"`
	InspectionID        *uuid.UUID     `json:"inspection_id,omitempty"`
	CertificateID       *uuid.UUID     `json:"certificate_id,omitempty"`
	SerialNumber        *string        `json:"serial_number,omitempty"`
	LotNumber           *string        `json:"lot_number,omitempty"`
	Status              string         `json:"status"`
	SealedAt            *time.Time     `json:"sealed_at,omitempty"`
	SealedBy            *uuid.UUID     `json:"sealed_by,omitempty"`
	SealHash            *string        `json:"seal_hash,omitempty"`
	BirthCertURL        *string        `json:"birth_cert_url,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

// MaterialConsumption represents a material consumption record linked to a genealogy.
type MaterialConsumption struct {
	ID               uuid.UUID  `json:"id"`
	TenantID         uuid.UUID  `json:"tenant_id"`
	GenealogyID      uuid.UUID  `json:"genealogy_id"`
	BatchLotID       *uuid.UUID `json:"batch_lot_id,omitempty"`
	MaterialName     string     `json:"material_name"`
	MaterialCode     string     `json:"material_code"`
	QuantityConsumed float64    `json:"quantity_consumed"`
	Unit             string     `json:"unit"`
	CreatedAt        time.Time  `json:"created_at"`
}

// GenealogyFilter defines filtering options for listing genealogy records.
type GenealogyFilter struct {
	ProductDefinitionID *uuid.UUID
	OrderID             *uuid.UUID
	TaskID              *uuid.UUID
	MachineID           *uuid.UUID
	Status              *string
	SerialNumber        *string
	LotNumber           *string
	Limit               int
	Offset              int
}

// GenealogyTree represents a tree view of a genealogy record with joined data.
type GenealogyTree struct {
	Genealogy         ProductGenealogy      `json:"genealogy"`
	ProductDefinition *ProductDefinition    `json:"product_definition,omitempty"`
	BOMItems          []BOMItem             `json:"bom_items,omitempty"`
	MaterialsConsumed []MaterialConsumption `json:"materials_consumed,omitempty"`
}

// GenealogyRepository handles product genealogy database operations.
type GenealogyRepository struct {
	db *sql.DB
}

// NewGenealogyRepository creates a new genealogy repository.
func NewGenealogyRepository(db *sql.DB) *GenealogyRepository {
	return &GenealogyRepository{db: db}
}

// List retrieves genealogy records matching the given filter with pagination.
// Results are ordered by created_at descending.
func (r *GenealogyRepository) List(ctx context.Context, filter GenealogyFilter) ([]ProductGenealogy, int, error) {
	query := `
		SELECT id, tenant_id, product_definition_id, order_id, order_item_id,
		       task_id, machine_id, inspection_id, certificate_id,
		       serial_number, lot_number, status, sealed_at, sealed_by,
		       seal_hash, birth_cert_url, metadata, created_at, updated_at
		FROM product_genealogy
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM product_genealogy WHERE 1=1`

	var args []interface{}
	argIndex := 1

	if filter.ProductDefinitionID != nil {
		query += fmt.Sprintf(" AND product_definition_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND product_definition_id = $%d", argIndex)
		args = append(args, *filter.ProductDefinitionID)
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

	if filter.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND status = $%d", argIndex)
		args = append(args, *filter.Status)
		argIndex++
	}

	if filter.SerialNumber != nil {
		query += fmt.Sprintf(" AND serial_number = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND serial_number = $%d", argIndex)
		args = append(args, *filter.SerialNumber)
		argIndex++
	}

	if filter.LotNumber != nil {
		query += fmt.Sprintf(" AND lot_number = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND lot_number = $%d", argIndex)
		args = append(args, *filter.LotNumber)
		argIndex++
	}

	// Get total count
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count genealogy records: %w", err)
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
		return nil, 0, fmt.Errorf("failed to query genealogy records: %w", err)
	}
	defer rows.Close()

	var records []ProductGenealogy
	for rows.Next() {
		record, err := scanProductGenealogy(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan genealogy record: %w", err)
		}
		records = append(records, record)
	}

	return records, total, nil
}

// GetByID retrieves a genealogy record by its unique identifier.
// Returns nil, nil if the record is not found (not an error condition).
func (r *GenealogyRepository) GetByID(ctx context.Context, id uuid.UUID) (*ProductGenealogy, error) {
	query := `
		SELECT id, tenant_id, product_definition_id, order_id, order_item_id,
		       task_id, machine_id, inspection_id, certificate_id,
		       serial_number, lot_number, status, sealed_at, sealed_by,
		       seal_hash, birth_cert_url, metadata, created_at, updated_at
		FROM product_genealogy
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	record, err := scanProductGenealogy(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get genealogy record: %w", err)
	}

	return &record, nil
}

// Create inserts a new genealogy record into the database.
// If record.ID is nil, a new UUID is generated automatically.
func (r *GenealogyRepository) Create(ctx context.Context, record *ProductGenealogy) error {
	query := `
		INSERT INTO product_genealogy (
			id, tenant_id, product_definition_id, order_id, order_item_id,
			task_id, machine_id, inspection_id, certificate_id,
			serial_number, lot_number, status, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING created_at, updated_at
	`

	if record.ID == uuid.Nil {
		record.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(record.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		record.ID, record.TenantID, record.ProductDefinitionID,
		record.OrderID, record.OrderItemID, record.TaskID, record.MachineID,
		record.InspectionID, record.CertificateID,
		record.SerialNumber, record.LotNumber, record.Status, metadataJSON,
	).Scan(&record.CreatedAt, &record.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create genealogy record: %w", err)
	}

	return nil
}

// Update modifies an existing genealogy record's mutable fields.
// The record.ID must exist in the database. The record.UpdatedAt field
// is refreshed from the database after successful update.
// Returns an error if the record is not found.
func (r *GenealogyRepository) Update(ctx context.Context, record *ProductGenealogy) error {
	query := `
		UPDATE product_genealogy SET
			product_definition_id = $2,
			order_id = $3,
			order_item_id = $4,
			task_id = $5,
			machine_id = $6,
			inspection_id = $7,
			certificate_id = $8,
			serial_number = $9,
			lot_number = $10,
			status = $11,
			metadata = $12
		WHERE id = $1
		RETURNING updated_at
	`

	metadataJSON, _ := json.Marshal(record.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		record.ID, record.ProductDefinitionID, record.OrderID,
		record.OrderItemID, record.TaskID, record.MachineID,
		record.InspectionID, record.CertificateID,
		record.SerialNumber, record.LotNumber, record.Status, metadataJSON,
	).Scan(&record.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("genealogy record not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update genealogy record: %w", err)
	}

	return nil
}

// Seal marks a genealogy record as sealed with a cryptographic hash and birth certificate URL.
// Sets status=sealed, sealed_at=now, and stores the seal_hash and birth_cert_url.
func (r *GenealogyRepository) Seal(ctx context.Context, id uuid.UUID, hash, birthCertURL string, sealedBy uuid.UUID) error {
	query := `
		UPDATE product_genealogy SET
			status = $2,
			sealed_at = NOW(),
			sealed_by = $3,
			seal_hash = $4,
			birth_cert_url = $5
		WHERE id = $1
		RETURNING updated_at
	`

	var updatedAt time.Time
	var birthCertURLPtr *string
	if birthCertURL != "" {
		birthCertURLPtr = &birthCertURL
	}

	err := r.db.QueryRowContext(ctx, query,
		id, string(GenealogyStatusSealed), sealedBy, hash, birthCertURLPtr,
	).Scan(&updatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("genealogy record not found")
	}
	if err != nil {
		return fmt.Errorf("failed to seal genealogy record: %w", err)
	}

	return nil
}

// GetTree retrieves a genealogy record with joined product definition, BOM items,
// and material consumption data to build a complete tree view.
func (r *GenealogyRepository) GetTree(ctx context.Context, id uuid.UUID) (*GenealogyTree, error) {
	// Get the genealogy record
	record, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}

	tree := &GenealogyTree{
		Genealogy: *record,
	}

	// Get product definition if linked
	if record.ProductDefinitionID != nil {
		pdQuery := `
			SELECT id, tenant_id, sku, name, version, category, description,
			       cad_file_url, parametric_specs, is_active, metadata,
			       created_at, updated_at
			FROM product_definitions
			WHERE id = $1
		`
		row := r.db.QueryRowContext(ctx, pdQuery, *record.ProductDefinitionID)
		pd, err := scanProductDefinition(row)
		if err == nil {
			tree.ProductDefinition = &pd
		}

		// Get BOM items for the product definition
		bomQuery := `
			SELECT id, tenant_id, product_definition_id, material_name, material_code,
			       quantity, unit, estimated_cost, currency, supplier, sort_order,
			       created_at, updated_at
			FROM bom_items
			WHERE product_definition_id = $1
			ORDER BY sort_order ASC
		`
		bomRows, err := r.db.QueryContext(ctx, bomQuery, *record.ProductDefinitionID)
		if err == nil {
			defer bomRows.Close()
			for bomRows.Next() {
				item, err := scanBOMItem(bomRows)
				if err == nil {
					tree.BOMItems = append(tree.BOMItems, item)
				}
			}
		}
	}

	// Get material consumption records
	mcQuery := `
		SELECT id, tenant_id, genealogy_id, batch_lot_id,
		       material_name, material_code, quantity_consumed, unit,
		       created_at
		FROM genealogy_material_consumption
		WHERE genealogy_id = $1
		ORDER BY created_at ASC
	`
	mcRows, err := r.db.QueryContext(ctx, mcQuery, id)
	if err == nil {
		defer mcRows.Close()
		for mcRows.Next() {
			mc, err := scanMaterialConsumption(mcRows)
			if err == nil {
				tree.MaterialsConsumed = append(tree.MaterialsConsumed, mc)
			}
		}
	}

	return tree, nil
}

// ListMaterials retrieves all material consumption records for a genealogy.
func (r *GenealogyRepository) ListMaterials(ctx context.Context, genealogyID uuid.UUID) ([]MaterialConsumption, error) {
	query := `
		SELECT id, tenant_id, genealogy_id, batch_lot_id,
		       material_name, material_code, quantity_consumed, unit,
		       created_at
		FROM genealogy_material_consumption
		WHERE genealogy_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, genealogyID)
	if err != nil {
		return nil, fmt.Errorf("failed to query material consumption: %w", err)
	}
	defer rows.Close()

	var materials []MaterialConsumption
	for rows.Next() {
		mc, err := scanMaterialConsumption(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan material consumption: %w", err)
		}
		materials = append(materials, mc)
	}

	return materials, nil
}

// CreateMaterialConsumption inserts a new material consumption record.
// If mc.ID is nil, a new UUID is generated automatically.
func (r *GenealogyRepository) CreateMaterialConsumption(ctx context.Context, mc *MaterialConsumption) error {
	query := `
		INSERT INTO genealogy_material_consumption (
			id, tenant_id, genealogy_id, batch_lot_id,
			material_name, material_code, quantity_consumed, unit
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at
	`

	if mc.ID == uuid.Nil {
		mc.ID = uuid.New()
	}

	var materialCode *string
	if mc.MaterialCode != "" {
		materialCode = &mc.MaterialCode
	}

	err := r.db.QueryRowContext(ctx, query,
		mc.ID, mc.TenantID, mc.GenealogyID, mc.BatchLotID,
		mc.MaterialName, materialCode, mc.QuantityConsumed, mc.Unit,
	).Scan(&mc.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to create material consumption: %w", err)
	}

	return nil
}

// scanProductGenealogy is a helper to scan a product genealogy record from a row.
func scanProductGenealogy(scanner interface {
	Scan(dest ...interface{}) error
}) (ProductGenealogy, error) {
	var record ProductGenealogy
	var productDefID, orderID, orderItemID, taskID, machineID sql.NullString
	var inspectionID, certificateID, sealedBy sql.NullString
	var serialNumber, lotNumber, sealHash, birthCertURL sql.NullString
	var sealedAt sql.NullTime
	var metadataJSON []byte

	err := scanner.Scan(
		&record.ID, &record.TenantID, &productDefID, &orderID, &orderItemID,
		&taskID, &machineID, &inspectionID, &certificateID,
		&serialNumber, &lotNumber, &record.Status, &sealedAt, &sealedBy,
		&sealHash, &birthCertURL, &metadataJSON, &record.CreatedAt, &record.UpdatedAt,
	)
	if err != nil {
		return record, err
	}

	if productDefID.Valid {
		id := uuid.MustParse(productDefID.String)
		record.ProductDefinitionID = &id
	}
	if orderID.Valid {
		id := uuid.MustParse(orderID.String)
		record.OrderID = &id
	}
	if orderItemID.Valid {
		id := uuid.MustParse(orderItemID.String)
		record.OrderItemID = &id
	}
	if taskID.Valid {
		id := uuid.MustParse(taskID.String)
		record.TaskID = &id
	}
	if machineID.Valid {
		id := uuid.MustParse(machineID.String)
		record.MachineID = &id
	}
	if inspectionID.Valid {
		id := uuid.MustParse(inspectionID.String)
		record.InspectionID = &id
	}
	if certificateID.Valid {
		id := uuid.MustParse(certificateID.String)
		record.CertificateID = &id
	}
	if sealedBy.Valid {
		id := uuid.MustParse(sealedBy.String)
		record.SealedBy = &id
	}
	if serialNumber.Valid {
		record.SerialNumber = &serialNumber.String
	}
	if lotNumber.Valid {
		record.LotNumber = &lotNumber.String
	}
	if sealHash.Valid {
		record.SealHash = &sealHash.String
	}
	if birthCertURL.Valid {
		record.BirthCertURL = &birthCertURL.String
	}
	if sealedAt.Valid {
		record.SealedAt = &sealedAt.Time
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &record.Metadata)
	}

	return record, nil
}

// scanMaterialConsumption is a helper to scan a material consumption record from a row.
func scanMaterialConsumption(scanner interface {
	Scan(dest ...interface{}) error
}) (MaterialConsumption, error) {
	var mc MaterialConsumption
	var batchLotID sql.NullString
	var materialCode sql.NullString

	err := scanner.Scan(
		&mc.ID, &mc.TenantID, &mc.GenealogyID, &batchLotID,
		&mc.MaterialName, &materialCode, &mc.QuantityConsumed, &mc.Unit,
		&mc.CreatedAt,
	)
	if err != nil {
		return mc, err
	}

	if batchLotID.Valid {
		id := uuid.MustParse(batchLotID.String)
		mc.BatchLotID = &id
	}
	if materialCode.Valid {
		mc.MaterialCode = materialCode.String
	}

	return mc, nil
}
