// Package repositories provides database access layer implementations.
package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// OrderItemRepository handles order item database operations.
type OrderItemRepository struct {
	db *sql.DB
}

// NewOrderItemRepository creates a new order item repository.
func NewOrderItemRepository(db *sql.DB) *OrderItemRepository {
	return &OrderItemRepository{db: db}
}

// List retrieves all items belonging to a specific order.
// Results are ordered by created_at ascending (oldest first).
// Returns an empty slice if the order has no items.
func (r *OrderItemRepository) List(ctx context.Context, orderID uuid.UUID) ([]types.OrderItem, error) {
	query := `
		SELECT id, order_id, product_name, product_sku, quantity, unit_price,
		       specifications, cad_file_url, created_at
		FROM order_items
		WHERE order_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to query order items: %w", err)
	}
	defer rows.Close()

	var items []types.OrderItem
	for rows.Next() {
		var item types.OrderItem
		var productSKU, cadFileURL sql.NullString
		var unitPrice sql.NullFloat64
		var specificationsJSON []byte

		err := rows.Scan(
			&item.ID, &item.OrderID, &item.ProductName, &productSKU,
			&item.Quantity, &unitPrice, &specificationsJSON, &cadFileURL,
			&item.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order item: %w", err)
		}

		if productSKU.Valid {
			item.ProductSKU = productSKU.String
		}
		if unitPrice.Valid {
			item.UnitPrice = unitPrice.Float64
		}
		if cadFileURL.Valid {
			item.CADFileURL = cadFileURL.String
		}
		if len(specificationsJSON) > 0 {
			json.Unmarshal(specificationsJSON, &item.Specifications)
		}

		items = append(items, item)
	}

	return items, nil
}

// GetByID retrieves an order item by its unique identifier.
// Returns nil, nil if the item is not found (not an error condition).
// Returns nil, error if a database error occurs.
func (r *OrderItemRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.OrderItem, error) {
	query := `
		SELECT id, order_id, product_name, product_sku, quantity, unit_price,
		       specifications, cad_file_url, created_at
		FROM order_items
		WHERE id = $1
	`

	var item types.OrderItem
	var productSKU, cadFileURL sql.NullString
	var unitPrice sql.NullFloat64
	var specificationsJSON []byte

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&item.ID, &item.OrderID, &item.ProductName, &productSKU,
		&item.Quantity, &unitPrice, &specificationsJSON, &cadFileURL,
		&item.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get order item: %w", err)
	}

	if productSKU.Valid {
		item.ProductSKU = productSKU.String
	}
	if unitPrice.Valid {
		item.UnitPrice = unitPrice.Float64
	}
	if cadFileURL.Valid {
		item.CADFileURL = cadFileURL.String
	}
	if len(specificationsJSON) > 0 {
		json.Unmarshal(specificationsJSON, &item.Specifications)
	}

	return &item, nil
}

// Create inserts a new order item into the database.
// If item.ID is nil, a new UUID is generated automatically.
// The item.CreatedAt field is populated from the database after insertion.
func (r *OrderItemRepository) Create(ctx context.Context, item *types.OrderItem) error {
	query := `
		INSERT INTO order_items (
			id, order_id, product_name, product_sku, quantity, unit_price,
			specifications, cad_file_url
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at
	`

	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}

	specificationsJSON, _ := json.Marshal(item.Specifications)

	var productSKU, cadFileURL *string
	if item.ProductSKU != "" {
		productSKU = &item.ProductSKU
	}
	if item.CADFileURL != "" {
		cadFileURL = &item.CADFileURL
	}

	var unitPrice *float64
	if item.UnitPrice > 0 {
		unitPrice = &item.UnitPrice
	}

	err := r.db.QueryRowContext(ctx, query,
		item.ID, item.OrderID, item.ProductName, productSKU,
		item.Quantity, unitPrice, specificationsJSON, cadFileURL,
	).Scan(&item.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to create order item: %w", err)
	}

	return nil
}

// Update modifies an existing order item's mutable fields.
// The item.ID must exist in the database.
// Returns an error if the item is not found.
func (r *OrderItemRepository) Update(ctx context.Context, item *types.OrderItem) error {
	query := `
		UPDATE order_items SET
			product_name = $2,
			product_sku = $3,
			quantity = $4,
			unit_price = $5,
			specifications = $6,
			cad_file_url = $7
		WHERE id = $1
	`

	specificationsJSON, _ := json.Marshal(item.Specifications)

	var productSKU, cadFileURL *string
	if item.ProductSKU != "" {
		productSKU = &item.ProductSKU
	}
	if item.CADFileURL != "" {
		cadFileURL = &item.CADFileURL
	}

	var unitPrice *float64
	if item.UnitPrice > 0 {
		unitPrice = &item.UnitPrice
	}

	result, err := r.db.ExecContext(ctx, query,
		item.ID, item.ProductName, productSKU, item.Quantity,
		unitPrice, specificationsJSON, cadFileURL,
	)
	if err != nil {
		return fmt.Errorf("failed to update order item: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("order item not found")
	}

	return nil
}

// Delete permanently removes an order item from the database.
// This is a hard delete - the item record is not recoverable.
// Returns an error if the item is not found.
func (r *OrderItemRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM order_items WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete order item: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("order item not found")
	}

	return nil
}

// DeleteByOrderID removes all items belonging to a specific order.
// This is typically used when deleting an order to clean up related items.
// Does not return an error if the order has no items.
func (r *OrderItemRepository) DeleteByOrderID(ctx context.Context, orderID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM order_items WHERE order_id = $1`, orderID)
	if err != nil {
		return fmt.Errorf("failed to delete order items: %w", err)
	}
	return nil
}

// Count returns the number of items belonging to a specific order.
// Returns 0 if the order has no items (not an error condition).
// Useful for pagination or validation before order submission.
func (r *OrderItemRepository) Count(ctx context.Context, orderID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_items WHERE order_id = $1`, orderID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count order items: %w", err)
	}
	return count, nil
}
