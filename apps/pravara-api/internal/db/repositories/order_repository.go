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

// List retrieves orders matching the given filter with pagination.
// Results are ordered by created_at descending. Returns the list of orders,
// total count (for pagination), and any error encountered.
// An empty filter returns all orders. Use filter.Limit and filter.Offset for pagination.
func (r *OrderRepository) List(ctx context.Context, filter OrderFilter) ([]types.Order, int, error) {
	// Build query with filters
	query := `
		SELECT id, tenant_id, external_id, customer_name, customer_email,
		       status, priority, due_date, total_amount, currency,
		       shipping_address, metadata, created_at, updated_at
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
		var shippingJSON, metadataJSON []byte

		err := rows.Scan(
			&order.ID, &order.TenantID, &externalID, &order.CustomerName,
			&customerEmail, &order.Status, &order.Priority, &dueDate,
			&totalAmount, &order.Currency, &shippingJSON, &metadataJSON,
			&order.CreatedAt, &order.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan order: %w", err)
		}

		if len(shippingJSON) > 0 {
			json.Unmarshal(shippingJSON, &order.ShippingAddress)
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

// GetByID retrieves an order by its unique identifier.
// Returns nil, nil if the order is not found (not an error condition).
// Returns nil, error if a database error occurs.
func (r *OrderRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.Order, error) {
	query := `
		SELECT id, tenant_id, external_id, customer_name, customer_email,
		       status, priority, due_date, total_amount, currency,
		       shipping_address, metadata, created_at, updated_at
		FROM orders
		WHERE id = $1
	`

	var order types.Order
	var externalID, customerEmail sql.NullString
	var dueDate sql.NullTime
	var totalAmount sql.NullFloat64
	var shippingJSON, metadataJSON []byte

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&order.ID, &order.TenantID, &externalID, &order.CustomerName,
		&customerEmail, &order.Status, &order.Priority, &dueDate,
		&totalAmount, &order.Currency, &shippingJSON, &metadataJSON,
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
	if len(shippingJSON) > 0 {
		json.Unmarshal(shippingJSON, &order.ShippingAddress)
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &order.Metadata)
	}

	return &order, nil
}

// Create inserts a new order into the database.
// If order.ID is nil, a new UUID is generated automatically.
// The order.CreatedAt and order.UpdatedAt fields are populated from the database
// after successful insertion.
func (r *OrderRepository) Create(ctx context.Context, order *types.Order) error {
	query := `
		INSERT INTO orders (
			id, tenant_id, external_id, customer_name, customer_email,
			status, priority, due_date, total_amount, currency,
			shipping_address, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING created_at, updated_at
	`

	if order.ID == uuid.Nil {
		order.ID = uuid.New()
	}

	shippingJSON := marshalNullableJSON(order.ShippingAddress)
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
		order.TotalAmount, order.Currency, shippingJSON, metadataJSON,
	).Scan(&order.CreatedAt, &order.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create order: %w", err)
	}

	return nil
}

// Update modifies an existing order's mutable fields.
// The order.ID must exist in the database. The order.UpdatedAt field
// is refreshed from the database after successful update.
// Returns an error if the order is not found.
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
			shipping_address = $9,
			metadata = $10
		WHERE id = $1
		RETURNING updated_at
	`

	shippingJSON := marshalNullableJSON(order.ShippingAddress)
	metadataJSON, _ := json.Marshal(order.Metadata)

	var customerEmail *string
	if order.CustomerEmail != "" {
		customerEmail = &order.CustomerEmail
	}

	err := r.db.QueryRowContext(ctx, query,
		order.ID, order.CustomerName, customerEmail, order.Status,
		order.Priority, order.DueDate, order.TotalAmount, order.Currency,
		shippingJSON, metadataJSON,
	).Scan(&order.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("order not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update order: %w", err)
	}

	return nil
}

// UpdateStatus updates only the status field of an order.
// This is more efficient than a full Update when only the status changes.
// Returns an error if the order is not found.
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

// Delete performs a soft delete by setting the order status to cancelled.
// The order record is preserved for audit purposes.
// Returns an error if the order is not found.
func (r *OrderRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.UpdateStatus(ctx, id, types.OrderStatusCancelled)
}

// GetByExternalID retrieves an order by its external (Cotiza) ID.
// This is used for integration with external order management systems.
// Returns nil, nil if the order is not found (not an error condition).
func (r *OrderRepository) GetByExternalID(ctx context.Context, externalID string) (*types.Order, error) {
	query := `
		SELECT id, tenant_id, external_id, customer_name, customer_email,
		       status, priority, due_date, total_amount, currency,
		       shipping_address, metadata, created_at, updated_at
		FROM orders
		WHERE external_id = $1
	`

	var order types.Order
	var extID, customerEmail sql.NullString
	var dueDate sql.NullTime
	var totalAmount sql.NullFloat64
	var shippingJSON, metadataJSON []byte

	err := r.db.QueryRowContext(ctx, query, externalID).Scan(
		&order.ID, &order.TenantID, &extID, &order.CustomerName,
		&customerEmail, &order.Status, &order.Priority, &dueDate,
		&totalAmount, &order.Currency, &shippingJSON, &metadataJSON,
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
	if len(shippingJSON) > 0 {
		json.Unmarshal(shippingJSON, &order.ShippingAddress)
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &order.Metadata)
	}

	return &order, nil
}

// marshalNullableJSON serializes a map for a nullable JSONB column,
// returning SQL NULL (nil) when the map itself is nil so absent data stays
// NULL instead of becoming the JSON literal "null".
func marshalNullableJSON(m map[string]any) []byte {
	if m == nil {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return b
}

// MarkInProgressIfStarted advances an order to in_progress when it is still
// in a pre-production status (received, validated, scheduled). Called by the
// order roll-up when the first task of an order starts. The guard set makes
// the call idempotent and prevents regressing later statuses (completed,
// shipped, cancelled). Returns the previous status and whether a transition
// happened.
//
// The row is locked (FOR UPDATE) while the guard is evaluated so concurrent
// roll-ups cannot interleave. Tenant scoping is enforced by RLS, matching
// the other order queries in this repository.
func (r *OrderRepository) MarkInProgressIfStarted(ctx context.Context, orderID uuid.UUID) (types.OrderStatus, bool, error) {
	query := `
		WITH prev AS (
			SELECT id, status FROM orders WHERE id = $1 FOR UPDATE
		)
		UPDATE orders o
		SET status = 'in_progress'
		FROM prev
		WHERE o.id = prev.id
		  AND prev.status IN ('received', 'validated', 'scheduled')
		RETURNING prev.status
	`

	var previous types.OrderStatus
	err := r.db.QueryRowContext(ctx, query, orderID).Scan(&previous)
	if err == sql.ErrNoRows {
		return "", false, nil // order missing or already at/past in_progress
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to mark order in progress: %w", err)
	}

	return previous, true, nil
}

// CompleteIfAllTasksDone advances an order to completed when every one of
// its tasks is completed. Orders with no tasks are never auto-completed.
// The guard set excludes completed, shipped, and cancelled so the roll-up
// can neither repeat itself nor regress later statuses. Returns the previous
// status and whether a transition happened.
func (r *OrderRepository) CompleteIfAllTasksDone(ctx context.Context, orderID uuid.UUID) (types.OrderStatus, bool, error) {
	query := `
		WITH prev AS (
			SELECT id, status FROM orders WHERE id = $1 FOR UPDATE
		)
		UPDATE orders o
		SET status = 'completed'
		FROM prev
		WHERE o.id = prev.id
		  AND prev.status IN ('received', 'validated', 'scheduled', 'in_progress')
		  AND EXISTS (
			SELECT 1 FROM tasks t WHERE t.order_id = o.id
		  )
		  AND NOT EXISTS (
			SELECT 1 FROM tasks t WHERE t.order_id = o.id AND t.status <> 'completed'
		  )
		RETURNING prev.status
	`

	var previous types.OrderStatus
	err := r.db.QueryRowContext(ctx, query, orderID).Scan(&previous)
	if err == sql.ErrNoRows {
		return "", false, nil // tasks still open, no tasks at all, or status past in_progress
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to complete order: %w", err)
	}

	return previous, true, nil
}

// Ensure pq is imported for array handling
var _ = pq.Array
