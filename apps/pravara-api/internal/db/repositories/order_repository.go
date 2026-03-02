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

// OrderRepository handles order database operations.
type OrderRepository struct {
	db *sql.DB
}

// NewOrderRepository creates a new order repository.
func NewOrderRepository(db *sql.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

// OrderFilter defines filtering options for listing orders.
type OrderFilter struct {
	Status     *types.OrderStatus
	Priority   *int
	FromDate   *time.Time
	ToDate     *time.Time
	CustomerID *string
	Limit      int
	Offset     int
}

// List retrieves orders with optional filtering.
func (r *OrderRepository) List(ctx context.Context, filter OrderFilter) ([]types.Order, int, error) {
	// Build query with filters
	query := `
		SELECT id, tenant_id, external_id, customer_name, customer_email,
		       status, priority, due_date, total_amount, currency, metadata,
		       created_at, updated_at
		FROM orders
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM orders WHERE 1=1`

	var args []interface{}
	argIndex := 1

	if filter.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND status = $%d", argIndex)
		args = append(args, *filter.Status)
		argIndex++
	}

	if filter.Priority != nil {
		query += fmt.Sprintf(" AND priority = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND priority = $%d", argIndex)
		args = append(args, *filter.Priority)
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
		return nil, 0, fmt.Errorf("failed to count orders: %w", err)
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
		return nil, 0, fmt.Errorf("failed to query orders: %w", err)
	}
	defer rows.Close()

	var orders []types.Order
	for rows.Next() {
		var order types.Order
		var externalID, customerEmail sql.NullString
		var dueDate sql.NullTime
		var totalAmount sql.NullFloat64
		var metadataJSON []byte

		err := rows.Scan(
			&order.ID, &order.TenantID, &externalID, &order.CustomerName,
			&customerEmail, &order.Status, &order.Priority, &dueDate,
			&totalAmount, &order.Currency, &metadataJSON,
			&order.CreatedAt, &order.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan order: %w", err)
		}

		if externalID.Valid {
			order.ExternalID = externalID.String
		}
		if customerEmail.Valid {
			order.CustomerEmail = customerEmail.String
		}
		if dueDate.Valid {
			order.DueDate = &dueDate.Time
		}
		if totalAmount.Valid {
			order.TotalAmount = totalAmount.Float64
		}
		if len(metadataJSON) > 0 {
			json.Unmarshal(metadataJSON, &order.Metadata)
		}

		orders = append(orders, order)
	}

	return orders, total, nil
}

// GetByID retrieves an order by ID.
func (r *OrderRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.Order, error) {
	query := `
		SELECT id, tenant_id, external_id, customer_name, customer_email,
		       status, priority, due_date, total_amount, currency, metadata,
		       created_at, updated_at
		FROM orders
		WHERE id = $1
	`

	var order types.Order
	var externalID, customerEmail sql.NullString
	var dueDate sql.NullTime
	var totalAmount sql.NullFloat64
	var metadataJSON []byte

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&order.ID, &order.TenantID, &externalID, &order.CustomerName,
		&customerEmail, &order.Status, &order.Priority, &dueDate,
		&totalAmount, &order.Currency, &metadataJSON,
		&order.CreatedAt, &order.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get order: %w", err)
	}

	if externalID.Valid {
		order.ExternalID = externalID.String
	}
	if customerEmail.Valid {
		order.CustomerEmail = customerEmail.String
	}
	if dueDate.Valid {
		order.DueDate = &dueDate.Time
	}
	if totalAmount.Valid {
		order.TotalAmount = totalAmount.Float64
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &order.Metadata)
	}

	return &order, nil
}

// Create inserts a new order.
func (r *OrderRepository) Create(ctx context.Context, order *types.Order) error {
	query := `
		INSERT INTO orders (
			id, tenant_id, external_id, customer_name, customer_email,
			status, priority, due_date, total_amount, currency, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING created_at, updated_at
	`

	if order.ID == uuid.Nil {
		order.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(order.Metadata)

	var externalID, customerEmail *string
	if order.ExternalID != "" {
		externalID = &order.ExternalID
	}
	if order.CustomerEmail != "" {
		customerEmail = &order.CustomerEmail
	}

	err := r.db.QueryRowContext(ctx, query,
		order.ID, order.TenantID, externalID, order.CustomerName,
		customerEmail, order.Status, order.Priority, order.DueDate,
		order.TotalAmount, order.Currency, metadataJSON,
	).Scan(&order.CreatedAt, &order.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create order: %w", err)
	}

	return nil
}

// Update modifies an existing order.
func (r *OrderRepository) Update(ctx context.Context, order *types.Order) error {
	query := `
		UPDATE orders SET
			customer_name = $2,
			customer_email = $3,
			status = $4,
			priority = $5,
			due_date = $6,
			total_amount = $7,
			currency = $8,
			metadata = $9
		WHERE id = $1
		RETURNING updated_at
	`

	metadataJSON, _ := json.Marshal(order.Metadata)

	var customerEmail *string
	if order.CustomerEmail != "" {
		customerEmail = &order.CustomerEmail
	}

	err := r.db.QueryRowContext(ctx, query,
		order.ID, order.CustomerName, customerEmail, order.Status,
		order.Priority, order.DueDate, order.TotalAmount, order.Currency,
		metadataJSON,
	).Scan(&order.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("order not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update order: %w", err)
	}

	return nil
}

// UpdateStatus updates only the order status.
func (r *OrderRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status types.OrderStatus) error {
	query := `UPDATE orders SET status = $2 WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id, status)
	if err != nil {
		return fmt.Errorf("failed to update order status: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("order not found")
	}

	return nil
}

// Delete removes an order (soft delete by setting status to cancelled).
func (r *OrderRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.UpdateStatus(ctx, id, types.OrderStatusCancelled)
}

// GetByExternalID retrieves an order by its external (Cotiza) ID.
func (r *OrderRepository) GetByExternalID(ctx context.Context, externalID string) (*types.Order, error) {
	query := `
		SELECT id, tenant_id, external_id, customer_name, customer_email,
		       status, priority, due_date, total_amount, currency, metadata,
		       created_at, updated_at
		FROM orders
		WHERE external_id = $1
	`

	var order types.Order
	var extID, customerEmail sql.NullString
	var dueDate sql.NullTime
	var totalAmount sql.NullFloat64
	var metadataJSON []byte

	err := r.db.QueryRowContext(ctx, query, externalID).Scan(
		&order.ID, &order.TenantID, &extID, &order.CustomerName,
		&customerEmail, &order.Status, &order.Priority, &dueDate,
		&totalAmount, &order.Currency, &metadataJSON,
		&order.CreatedAt, &order.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get order by external ID: %w", err)
	}

	if extID.Valid {
		order.ExternalID = extID.String
	}
	if customerEmail.Valid {
		order.CustomerEmail = customerEmail.String
	}
	if dueDate.Valid {
		order.DueDate = &dueDate.Time
	}
	if totalAmount.Valid {
		order.TotalAmount = totalAmount.Float64
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &order.Metadata)
	}

	return &order, nil
}

// Ensure pq is imported for array handling
var _ = pq.Array
