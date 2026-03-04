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

// ProductCategory represents the category of a product definition.
type ProductCategory string

const (
	ProductCategory3DPrint  ProductCategory = "3d_print"
	ProductCategoryCNCPart  ProductCategory = "cnc_part"
	ProductCategoryLaserCut ProductCategory = "laser_cut"
	ProductCategoryAssembly ProductCategory = "assembly"
	ProductCategoryOther    ProductCategory = "other"
)

// ProductDefinition represents a product definition in the system.
type ProductDefinition struct {
	ID              uuid.UUID      `json:"id"`
	TenantID        uuid.UUID      `json:"tenant_id"`
	SKU             string         `json:"sku"`
	Name            string         `json:"name"`
	Version         string         `json:"version"`
	Category        string         `json:"category"`
	Description     string         `json:"description"`
	CADFileURL      string         `json:"cad_file_url"`
	ParametricSpecs map[string]any `json:"parametric_specs,omitempty"`
	IsActive        bool           `json:"is_active"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// BOMItem represents a bill of materials item for a product definition.
type BOMItem struct {
	ID                  uuid.UUID `json:"id"`
	TenantID            uuid.UUID `json:"tenant_id"`
	ProductDefinitionID uuid.UUID `json:"product_definition_id"`
	MaterialName        string    `json:"material_name"`
	MaterialCode        string    `json:"material_code"`
	Quantity            float64   `json:"quantity"`
	Unit                string    `json:"unit"`
	EstimatedCost       *float64  `json:"estimated_cost,omitempty"`
	Currency            string    `json:"currency"`
	Supplier            string    `json:"supplier"`
	SortOrder           int       `json:"sort_order"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// ProductFilter defines filtering options for listing product definitions.
type ProductFilter struct {
	Category *string
	IsActive *bool
	Search   *string // ILIKE on SKU/name
	Limit    int
	Offset   int
}

// ProductRepository handles product definition database operations.
type ProductRepository struct {
	db *sql.DB
}

// NewProductRepository creates a new product repository.
func NewProductRepository(db *sql.DB) *ProductRepository {
	return &ProductRepository{db: db}
}

// List retrieves product definitions matching the given filter with pagination.
// Results are ordered by created_at descending. Returns the list of products,
// total count (for pagination), and any error encountered.
func (r *ProductRepository) List(ctx context.Context, filter ProductFilter) ([]ProductDefinition, int, error) {
	query := `
		SELECT id, tenant_id, sku, name, version, category, description,
		       cad_file_url, parametric_specs, is_active, metadata,
		       created_at, updated_at
		FROM product_definitions
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM product_definitions WHERE 1=1`

	var args []interface{}
	argIndex := 1

	if filter.Category != nil {
		query += fmt.Sprintf(" AND category = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND category = $%d", argIndex)
		args = append(args, *filter.Category)
		argIndex++
	}

	if filter.IsActive != nil {
		query += fmt.Sprintf(" AND is_active = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND is_active = $%d", argIndex)
		args = append(args, *filter.IsActive)
		argIndex++
	}

	if filter.Search != nil {
		query += fmt.Sprintf(" AND (sku ILIKE $%d OR name ILIKE $%d)", argIndex, argIndex)
		countQuery += fmt.Sprintf(" AND (sku ILIKE $%d OR name ILIKE $%d)", argIndex, argIndex)
		args = append(args, "%"+*filter.Search+"%")
		argIndex++
	}

	// Get total count
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count product definitions: %w", err)
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
		return nil, 0, fmt.Errorf("failed to query product definitions: %w", err)
	}
	defer rows.Close()

	var products []ProductDefinition
	for rows.Next() {
		product, err := scanProductDefinition(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan product definition: %w", err)
		}
		products = append(products, product)
	}

	return products, total, nil
}

// GetByID retrieves a product definition by its unique identifier.
// Returns nil, nil if the product is not found (not an error condition).
func (r *ProductRepository) GetByID(ctx context.Context, id uuid.UUID) (*ProductDefinition, error) {
	query := `
		SELECT id, tenant_id, sku, name, version, category, description,
		       cad_file_url, parametric_specs, is_active, metadata,
		       created_at, updated_at
		FROM product_definitions
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	product, err := scanProductDefinition(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get product definition: %w", err)
	}

	return &product, nil
}

// GetBySKU retrieves a product definition by SKU and version.
// Returns nil, nil if the product is not found (not an error condition).
func (r *ProductRepository) GetBySKU(ctx context.Context, sku, version string) (*ProductDefinition, error) {
	query := `
		SELECT id, tenant_id, sku, name, version, category, description,
		       cad_file_url, parametric_specs, is_active, metadata,
		       created_at, updated_at
		FROM product_definitions
		WHERE sku = $1 AND version = $2
	`

	row := r.db.QueryRowContext(ctx, query, sku, version)
	product, err := scanProductDefinition(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get product definition by SKU: %w", err)
	}

	return &product, nil
}

// Create inserts a new product definition into the database.
// If product.ID is nil, a new UUID is generated automatically.
// The product.CreatedAt and product.UpdatedAt fields are populated from the database
// after successful insertion.
func (r *ProductRepository) Create(ctx context.Context, product *ProductDefinition) error {
	query := `
		INSERT INTO product_definitions (
			id, tenant_id, sku, name, version, category, description,
			cad_file_url, parametric_specs, is_active, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING created_at, updated_at
	`

	if product.ID == uuid.Nil {
		product.ID = uuid.New()
	}

	parametricSpecsJSON, _ := json.Marshal(product.ParametricSpecs)
	metadataJSON, _ := json.Marshal(product.Metadata)

	var description, cadFileURL *string
	if product.Description != "" {
		description = &product.Description
	}
	if product.CADFileURL != "" {
		cadFileURL = &product.CADFileURL
	}

	err := r.db.QueryRowContext(ctx, query,
		product.ID, product.TenantID, product.SKU, product.Name, product.Version,
		product.Category, description, cadFileURL, parametricSpecsJSON,
		product.IsActive, metadataJSON,
	).Scan(&product.CreatedAt, &product.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create product definition: %w", err)
	}

	return nil
}

// Update modifies an existing product definition's mutable fields.
// The product.ID must exist in the database. The product.UpdatedAt field
// is refreshed from the database after successful update.
// Returns an error if the product is not found.
func (r *ProductRepository) Update(ctx context.Context, product *ProductDefinition) error {
	query := `
		UPDATE product_definitions SET
			name = $2,
			version = $3,
			category = $4,
			description = $5,
			cad_file_url = $6,
			parametric_specs = $7,
			is_active = $8,
			metadata = $9
		WHERE id = $1
		RETURNING updated_at
	`

	parametricSpecsJSON, _ := json.Marshal(product.ParametricSpecs)
	metadataJSON, _ := json.Marshal(product.Metadata)

	var description, cadFileURL *string
	if product.Description != "" {
		description = &product.Description
	}
	if product.CADFileURL != "" {
		cadFileURL = &product.CADFileURL
	}

	err := r.db.QueryRowContext(ctx, query,
		product.ID, product.Name, product.Version, product.Category,
		description, cadFileURL, parametricSpecsJSON, product.IsActive,
		metadataJSON,
	).Scan(&product.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("product definition not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update product definition: %w", err)
	}

	return nil
}

// Delete permanently removes a product definition from the database.
// Returns an error if the product is not found.
func (r *ProductRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM product_definitions WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete product definition: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("product definition not found")
	}

	return nil
}

// ListBOMItems retrieves all BOM items for a product definition, ordered by sort_order.
func (r *ProductRepository) ListBOMItems(ctx context.Context, productID uuid.UUID) ([]BOMItem, error) {
	query := `
		SELECT id, tenant_id, product_definition_id, material_name, material_code,
		       quantity, unit, estimated_cost, currency, supplier, sort_order,
		       created_at, updated_at
		FROM bom_items
		WHERE product_definition_id = $1
		ORDER BY sort_order ASC
	`

	rows, err := r.db.QueryContext(ctx, query, productID)
	if err != nil {
		return nil, fmt.Errorf("failed to query BOM items: %w", err)
	}
	defer rows.Close()

	var items []BOMItem
	for rows.Next() {
		item, err := scanBOMItem(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan BOM item: %w", err)
		}
		items = append(items, item)
	}

	return items, nil
}

// CreateBOMItem inserts a new BOM item into the database.
// If item.ID is nil, a new UUID is generated automatically.
func (r *ProductRepository) CreateBOMItem(ctx context.Context, item *BOMItem) error {
	query := `
		INSERT INTO bom_items (
			id, tenant_id, product_definition_id, material_name, material_code,
			quantity, unit, estimated_cost, currency, supplier, sort_order
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING created_at, updated_at
	`

	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}

	var materialCode, currency, supplier *string
	if item.MaterialCode != "" {
		materialCode = &item.MaterialCode
	}
	if item.Currency != "" {
		currency = &item.Currency
	}
	if item.Supplier != "" {
		supplier = &item.Supplier
	}

	err := r.db.QueryRowContext(ctx, query,
		item.ID, item.TenantID, item.ProductDefinitionID, item.MaterialName,
		materialCode, item.Quantity, item.Unit, item.EstimatedCost,
		currency, supplier, item.SortOrder,
	).Scan(&item.CreatedAt, &item.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create BOM item: %w", err)
	}

	return nil
}

// DeleteBOMItem permanently removes a BOM item from the database.
// Returns an error if the BOM item is not found.
func (r *ProductRepository) DeleteBOMItem(ctx context.Context, itemID uuid.UUID) error {
	query := `DELETE FROM bom_items WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, itemID)
	if err != nil {
		return fmt.Errorf("failed to delete BOM item: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("BOM item not found")
	}

	return nil
}

// scanProductDefinition is a helper to scan a product definition from a row.
func scanProductDefinition(scanner interface {
	Scan(dest ...interface{}) error
}) (ProductDefinition, error) {
	var product ProductDefinition
	var description, cadFileURL sql.NullString
	var parametricSpecsJSON, metadataJSON []byte

	err := scanner.Scan(
		&product.ID, &product.TenantID, &product.SKU, &product.Name,
		&product.Version, &product.Category, &description,
		&cadFileURL, &parametricSpecsJSON, &product.IsActive, &metadataJSON,
		&product.CreatedAt, &product.UpdatedAt,
	)
	if err != nil {
		return product, err
	}

	if description.Valid {
		product.Description = description.String
	}
	if cadFileURL.Valid {
		product.CADFileURL = cadFileURL.String
	}
	if len(parametricSpecsJSON) > 0 {
		json.Unmarshal(parametricSpecsJSON, &product.ParametricSpecs)
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &product.Metadata)
	}

	return product, nil
}

// scanBOMItem is a helper to scan a BOM item from a row.
func scanBOMItem(scanner interface {
	Scan(dest ...interface{}) error
}) (BOMItem, error) {
	var item BOMItem
	var materialCode, currency, supplier sql.NullString
	var estimatedCost sql.NullFloat64

	err := scanner.Scan(
		&item.ID, &item.TenantID, &item.ProductDefinitionID,
		&item.MaterialName, &materialCode, &item.Quantity, &item.Unit,
		&estimatedCost, &currency, &supplier, &item.SortOrder,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return item, err
	}

	if materialCode.Valid {
		item.MaterialCode = materialCode.String
	}
	if estimatedCost.Valid {
		item.EstimatedCost = &estimatedCost.Float64
	}
	if currency.Valid {
		item.Currency = currency.String
	}
	if supplier.Valid {
		item.Supplier = supplier.String
	}

	return item, nil
}
