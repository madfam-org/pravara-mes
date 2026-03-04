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

// InventoryRepository handles inventory database operations.
type InventoryRepository struct {
	db *sql.DB
}

// NewInventoryRepository creates a new inventory repository.
func NewInventoryRepository(db *sql.DB) *InventoryRepository {
	return &InventoryRepository{db: db}
}

// InventoryItem represents an inventory item tracked in the system.
type InventoryItem struct {
	ID                uuid.UUID      `json:"id"`
	TenantID          uuid.UUID      `json:"tenant_id"`
	SKU               string         `json:"sku"`
	Name              string         `json:"name"`
	Category          string         `json:"category"`
	Description       string         `json:"description"`
	Unit              string         `json:"unit"`
	QuantityOnHand    float64        `json:"quantity_on_hand"`
	QuantityReserved  float64        `json:"quantity_reserved"`
	QuantityAvailable float64        `json:"quantity_available"`
	ReorderPoint      float64        `json:"reorder_point"`
	ReorderQuantity   float64        `json:"reorder_quantity"`
	ForgeSightID      *string        `json:"forgesight_id,omitempty"`
	UnitCost          *float64       `json:"unit_cost,omitempty"`
	Currency          string         `json:"currency"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// InventoryTransaction represents a record of inventory movement.
type InventoryTransaction struct {
	ID              uuid.UUID  `json:"id"`
	TenantID        uuid.UUID  `json:"tenant_id"`
	InventoryItemID uuid.UUID  `json:"inventory_item_id"`
	TransactionType string     `json:"transaction_type"` // receipt, consumption, adjustment, reservation, release
	Quantity        float64    `json:"quantity"`
	RunningBalance  float64    `json:"running_balance"`
	ReferenceType   *string    `json:"reference_type,omitempty"`
	ReferenceID     *uuid.UUID `json:"reference_id,omitempty"`
	Notes           *string    `json:"notes,omitempty"`
	CreatedBy       *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// InventoryFilter defines filtering options for listing inventory items.
type InventoryFilter struct {
	Category     *string
	LowStockOnly bool
	Search       *string
	Limit        int
	Offset       int
}

// ListItems retrieves inventory items matching the given filter with pagination.
// Results are ordered by name ascending.
func (r *InventoryRepository) ListItems(ctx context.Context, filter InventoryFilter) ([]InventoryItem, int, error) {
	query := `
		SELECT id, tenant_id, sku, name, category, description, unit,
		       quantity_on_hand, quantity_reserved, quantity_available,
		       reorder_point, reorder_quantity, forgesight_id, unit_cost,
		       currency, metadata, created_at, updated_at
		FROM inventory_items
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM inventory_items WHERE 1=1`

	var args []interface{}
	argIndex := 1

	if filter.Category != nil {
		query += fmt.Sprintf(" AND category = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND category = $%d", argIndex)
		args = append(args, *filter.Category)
		argIndex++
	}

	if filter.LowStockOnly {
		query += " AND (quantity_on_hand - quantity_reserved) <= reorder_point"
		countQuery += " AND (quantity_on_hand - quantity_reserved) <= reorder_point"
	}

	if filter.Search != nil {
		query += fmt.Sprintf(" AND (name ILIKE $%d OR sku ILIKE $%d)", argIndex, argIndex)
		countQuery += fmt.Sprintf(" AND (name ILIKE $%d OR sku ILIKE $%d)", argIndex, argIndex)
		args = append(args, "%"+*filter.Search+"%")
		argIndex++
	}

	// Get total count
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count inventory items: %w", err)
	}

	// Add ordering and pagination
	query += " ORDER BY name ASC"

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
		return nil, 0, fmt.Errorf("failed to query inventory items: %w", err)
	}
	defer rows.Close()

	var items []InventoryItem
	for rows.Next() {
		item, err := r.scanInventoryItem(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}

	return items, total, nil
}

// GetItemByID retrieves an inventory item by its unique identifier.
// Returns nil, nil if the item is not found.
func (r *InventoryRepository) GetItemByID(ctx context.Context, id uuid.UUID) (*InventoryItem, error) {
	query := `
		SELECT id, tenant_id, sku, name, category, description, unit,
		       quantity_on_hand, quantity_reserved, quantity_available,
		       reorder_point, reorder_quantity, forgesight_id, unit_cost,
		       currency, metadata, created_at, updated_at
		FROM inventory_items
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	item, err := r.scanInventoryItemRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get inventory item: %w", err)
	}

	return item, nil
}

// CreateItem inserts a new inventory item into the database.
// If item.ID is nil, a new UUID is generated automatically.
func (r *InventoryRepository) CreateItem(ctx context.Context, item *InventoryItem) error {
	query := `
		INSERT INTO inventory_items (
			id, tenant_id, sku, name, category, description, unit,
			quantity_on_hand, quantity_reserved, quantity_available,
			reorder_point, reorder_quantity, forgesight_id, unit_cost,
			currency, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING created_at, updated_at
	`

	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(item.Metadata)

	var description sql.NullString
	if item.Description != "" {
		description = sql.NullString{String: item.Description, Valid: true}
	}

	err := r.db.QueryRowContext(ctx, query,
		item.ID, item.TenantID, item.SKU, item.Name, item.Category,
		description, item.Unit, item.QuantityOnHand, item.QuantityReserved,
		item.QuantityAvailable, item.ReorderPoint, item.ReorderQuantity,
		item.ForgeSightID, item.UnitCost, item.Currency, metadataJSON,
	).Scan(&item.CreatedAt, &item.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create inventory item: %w", err)
	}

	return nil
}

// UpdateItem modifies an existing inventory item's mutable fields.
func (r *InventoryRepository) UpdateItem(ctx context.Context, item *InventoryItem) error {
	query := `
		UPDATE inventory_items SET
			sku = $2,
			name = $3,
			category = $4,
			description = $5,
			unit = $6,
			reorder_point = $7,
			reorder_quantity = $8,
			forgesight_id = $9,
			unit_cost = $10,
			currency = $11,
			metadata = $12
		WHERE id = $1
		RETURNING updated_at
	`

	metadataJSON, _ := json.Marshal(item.Metadata)

	var description sql.NullString
	if item.Description != "" {
		description = sql.NullString{String: item.Description, Valid: true}
	}

	err := r.db.QueryRowContext(ctx, query,
		item.ID, item.SKU, item.Name, item.Category, description,
		item.Unit, item.ReorderPoint, item.ReorderQuantity,
		item.ForgeSightID, item.UnitCost, item.Currency, metadataJSON,
	).Scan(&item.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("inventory item not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update inventory item: %w", err)
	}

	return nil
}

// UpsertByForgeSightID inserts or updates an inventory item keyed by forgesight_id.
func (r *InventoryRepository) UpsertByForgeSightID(ctx context.Context, item *InventoryItem) error {
	query := `
		INSERT INTO inventory_items (
			id, tenant_id, sku, name, category, description, unit,
			quantity_on_hand, quantity_reserved, quantity_available,
			reorder_point, reorder_quantity, forgesight_id, unit_cost,
			currency, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (forgesight_id) WHERE forgesight_id IS NOT NULL
		DO UPDATE SET
			name = EXCLUDED.name,
			category = EXCLUDED.category,
			description = EXCLUDED.description,
			unit = EXCLUDED.unit,
			quantity_on_hand = EXCLUDED.quantity_on_hand,
			quantity_available = EXCLUDED.quantity_on_hand - inventory_items.quantity_reserved,
			unit_cost = EXCLUDED.unit_cost,
			currency = EXCLUDED.currency,
			metadata = EXCLUDED.metadata,
			updated_at = NOW()
		RETURNING id, created_at, updated_at
	`

	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(item.Metadata)

	var description sql.NullString
	if item.Description != "" {
		description = sql.NullString{String: item.Description, Valid: true}
	}

	err := r.db.QueryRowContext(ctx, query,
		item.ID, item.TenantID, item.SKU, item.Name, item.Category,
		description, item.Unit, item.QuantityOnHand, item.QuantityReserved,
		item.QuantityAvailable, item.ReorderPoint, item.ReorderQuantity,
		item.ForgeSightID, item.UnitCost, item.Currency, metadataJSON,
	).Scan(&item.ID, &item.CreatedAt, &item.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to upsert inventory item by forgesight_id: %w", err)
	}

	return nil
}

// UpsertBySKU inserts or updates an inventory item keyed by (tenant_id, sku).
func (r *InventoryRepository) UpsertBySKU(ctx context.Context, item *InventoryItem) error {
	query := `
		INSERT INTO inventory_items (
			id, tenant_id, sku, name, category, description, unit,
			quantity_on_hand, quantity_reserved, quantity_available,
			reorder_point, reorder_quantity, forgesight_id, unit_cost,
			currency, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (tenant_id, sku)
		DO UPDATE SET
			name = EXCLUDED.name,
			category = EXCLUDED.category,
			description = EXCLUDED.description,
			unit = EXCLUDED.unit,
			quantity_on_hand = EXCLUDED.quantity_on_hand,
			quantity_available = EXCLUDED.quantity_on_hand - inventory_items.quantity_reserved,
			unit_cost = EXCLUDED.unit_cost,
			currency = EXCLUDED.currency,
			metadata = EXCLUDED.metadata,
			updated_at = NOW()
		RETURNING id, created_at, updated_at
	`

	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(item.Metadata)

	var description sql.NullString
	if item.Description != "" {
		description = sql.NullString{String: item.Description, Valid: true}
	}

	err := r.db.QueryRowContext(ctx, query,
		item.ID, item.TenantID, item.SKU, item.Name, item.Category,
		description, item.Unit, item.QuantityOnHand, item.QuantityReserved,
		item.QuantityAvailable, item.ReorderPoint, item.ReorderQuantity,
		item.ForgeSightID, item.UnitCost, item.Currency, metadataJSON,
	).Scan(&item.ID, &item.CreatedAt, &item.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to upsert inventory item by SKU: %w", err)
	}

	return nil
}

// AdjustQuantity modifies the quantity on hand for an inventory item and creates
// an inventory transaction record with a running balance.
// Positive quantity adds stock, negative quantity removes stock.
func (r *InventoryRepository) AdjustQuantity(ctx context.Context, itemID uuid.UUID, quantity float64, txnType string, refType *string, refID *uuid.UUID, userID *uuid.UUID, notes *string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update quantity on hand and recalculate available
	updateQuery := `
		UPDATE inventory_items SET
			quantity_on_hand = quantity_on_hand + $2,
			quantity_available = (quantity_on_hand + $2) - quantity_reserved,
			updated_at = NOW()
		WHERE id = $1
		RETURNING quantity_on_hand
	`

	var newBalance float64
	err = tx.QueryRowContext(ctx, updateQuery, itemID, quantity).Scan(&newBalance)
	if err == sql.ErrNoRows {
		return fmt.Errorf("inventory item not found")
	}
	if err != nil {
		return fmt.Errorf("failed to adjust inventory quantity: %w", err)
	}

	// Create transaction record
	txnQuery := `
		INSERT INTO inventory_transactions (
			id, tenant_id, inventory_item_id, transaction_type,
			quantity, running_balance, reference_type, reference_id,
			notes, created_by
		) VALUES (
			$1,
			(SELECT tenant_id FROM inventory_items WHERE id = $2),
			$2, $3, $4, $5, $6, $7, $8, $9
		)
	`

	txnID := uuid.New()
	_, err = tx.ExecContext(ctx, txnQuery,
		txnID, itemID, txnType, quantity, newBalance,
		refType, refID, notes, userID,
	)
	if err != nil {
		return fmt.Errorf("failed to create inventory transaction: %w", err)
	}

	return tx.Commit()
}

// GetLowStock retrieves inventory items where available stock is at or below the reorder point.
func (r *InventoryRepository) GetLowStock(ctx context.Context) ([]InventoryItem, error) {
	query := `
		SELECT id, tenant_id, sku, name, category, description, unit,
		       quantity_on_hand, quantity_reserved, quantity_available,
		       reorder_point, reorder_quantity, forgesight_id, unit_cost,
		       currency, metadata, created_at, updated_at
		FROM inventory_items
		WHERE (quantity_on_hand - quantity_reserved) <= reorder_point
		  AND reorder_point > 0
		ORDER BY (quantity_on_hand - quantity_reserved) / NULLIF(reorder_point, 0) ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query low stock items: %w", err)
	}
	defer rows.Close()

	var items []InventoryItem
	for rows.Next() {
		item, err := r.scanInventoryItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}

	return items, nil
}

// ListTransactions retrieves inventory transactions for a specific item,
// ordered by most recent first.
func (r *InventoryRepository) ListTransactions(ctx context.Context, itemID uuid.UUID, limit int) ([]InventoryTransaction, error) {
	query := `
		SELECT id, tenant_id, inventory_item_id, transaction_type,
		       quantity, running_balance, reference_type, reference_id,
		       notes, created_by, created_at
		FROM inventory_transactions
		WHERE inventory_item_id = $1
		ORDER BY created_at DESC
	`

	var args []interface{}
	args = append(args, itemID)

	if limit > 0 {
		query += " LIMIT $2"
		args = append(args, limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query inventory transactions: %w", err)
	}
	defer rows.Close()

	var transactions []InventoryTransaction
	for rows.Next() {
		var txn InventoryTransaction
		var refType sql.NullString
		var refID *uuid.UUID
		var notes sql.NullString
		var createdBy *uuid.UUID

		err := rows.Scan(
			&txn.ID, &txn.TenantID, &txn.InventoryItemID, &txn.TransactionType,
			&txn.Quantity, &txn.RunningBalance, &refType, &refID,
			&notes, &createdBy, &txn.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan inventory transaction: %w", err)
		}

		if refType.Valid {
			txn.ReferenceType = &refType.String
		}
		txn.ReferenceID = refID
		if notes.Valid {
			txn.Notes = &notes.String
		}
		txn.CreatedBy = createdBy

		transactions = append(transactions, txn)
	}

	return transactions, nil
}

// Helper functions

func (r *InventoryRepository) scanInventoryItem(rows *sql.Rows) (*InventoryItem, error) {
	var item InventoryItem
	var description sql.NullString
	var forgeSightID sql.NullString
	var unitCost sql.NullFloat64
	var metadataJSON []byte

	err := rows.Scan(
		&item.ID, &item.TenantID, &item.SKU, &item.Name, &item.Category,
		&description, &item.Unit, &item.QuantityOnHand, &item.QuantityReserved,
		&item.QuantityAvailable, &item.ReorderPoint, &item.ReorderQuantity,
		&forgeSightID, &unitCost, &item.Currency, &metadataJSON,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan inventory item: %w", err)
	}

	if description.Valid {
		item.Description = description.String
	}
	if forgeSightID.Valid {
		item.ForgeSightID = &forgeSightID.String
	}
	if unitCost.Valid {
		item.UnitCost = &unitCost.Float64
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &item.Metadata)
	}

	return &item, nil
}

func (r *InventoryRepository) scanInventoryItemRow(row *sql.Row) (*InventoryItem, error) {
	var item InventoryItem
	var description sql.NullString
	var forgeSightID sql.NullString
	var unitCost sql.NullFloat64
	var metadataJSON []byte

	err := row.Scan(
		&item.ID, &item.TenantID, &item.SKU, &item.Name, &item.Category,
		&description, &item.Unit, &item.QuantityOnHand, &item.QuantityReserved,
		&item.QuantityAvailable, &item.ReorderPoint, &item.ReorderQuantity,
		&forgeSightID, &unitCost, &item.Currency, &metadataJSON,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if description.Valid {
		item.Description = description.String
	}
	if forgeSightID.Valid {
		item.ForgeSightID = &forgeSightID.String
	}
	if unitCost.Valid {
		item.UnitCost = &unitCost.Float64
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &item.Metadata)
	}

	return &item, nil
}
