package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// InvoiceRepository provides CRUD operations for invoices.
type InvoiceRepository struct {
	db *sql.DB
}

// NewInvoiceRepository creates a new InvoiceRepository.
func NewInvoiceRepository(db *sql.DB) *InvoiceRepository {
	return &InvoiceRepository{db: db}
}

// Upsert creates or updates an invoice based on dhanam_id.
func (r *InvoiceRepository) Upsert(ctx context.Context, invoice *Invoice) error {
	lineItemsJSON, _ := json.Marshal(invoice.LineItems)
	rawJSON, _ := json.Marshal(invoice.RawPayload)

	query := `
		INSERT INTO invoices (
			id, tenant_id, dhanam_id, status, amount, currency,
			period_start, period_end, line_items, raw_payload,
			webhook_event, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (dhanam_id) DO UPDATE SET
			status = EXCLUDED.status,
			amount = EXCLUDED.amount,
			line_items = EXCLUDED.line_items,
			raw_payload = EXCLUDED.raw_payload,
			webhook_event = EXCLUDED.webhook_event,
			updated_at = EXCLUDED.updated_at
	`

	_, err := r.db.ExecContext(ctx, query,
		invoice.ID, invoice.TenantID, invoice.DhanamID, invoice.Status,
		invoice.Amount, invoice.Currency, invoice.PeriodStart, invoice.PeriodEnd,
		lineItemsJSON, rawJSON, invoice.WebhookEvent,
		invoice.CreatedAt, invoice.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert invoice: %w", err)
	}

	return nil
}

// GetByDhanamID retrieves an invoice by its Dhanam ID.
func (r *InvoiceRepository) GetByDhanamID(ctx context.Context, dhanamID string) (*Invoice, error) {
	query := `
		SELECT id, tenant_id, dhanam_id, status, amount, currency,
		       period_start, period_end, line_items, raw_payload,
		       webhook_event, created_at, updated_at
		FROM invoices
		WHERE dhanam_id = $1
	`

	var invoice Invoice
	var lineItemsJSON, rawJSON []byte

	err := r.db.QueryRowContext(ctx, query, dhanamID).Scan(
		&invoice.ID, &invoice.TenantID, &invoice.DhanamID, &invoice.Status,
		&invoice.Amount, &invoice.Currency, &invoice.PeriodStart, &invoice.PeriodEnd,
		&lineItemsJSON, &rawJSON, &invoice.WebhookEvent,
		&invoice.CreatedAt, &invoice.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get invoice: %w", err)
	}

	json.Unmarshal(lineItemsJSON, &invoice.LineItems)
	json.Unmarshal(rawJSON, &invoice.RawPayload)

	return &invoice, nil
}

// ListByTenant retrieves all invoices for a tenant.
func (r *InvoiceRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]Invoice, error) {
	query := `
		SELECT id, tenant_id, dhanam_id, status, amount, currency,
		       period_start, period_end, line_items, raw_payload,
		       webhook_event, created_at, updated_at
		FROM invoices
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT 100
	`

	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list invoices: %w", err)
	}
	defer rows.Close()

	var invoices []Invoice
	for rows.Next() {
		var inv Invoice
		var lineItemsJSON, rawJSON []byte

		err := rows.Scan(
			&inv.ID, &inv.TenantID, &inv.DhanamID, &inv.Status,
			&inv.Amount, &inv.Currency, &inv.PeriodStart, &inv.PeriodEnd,
			&lineItemsJSON, &rawJSON, &inv.WebhookEvent,
			&inv.CreatedAt, &inv.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan invoice: %w", err)
		}

		json.Unmarshal(lineItemsJSON, &inv.LineItems)
		json.Unmarshal(rawJSON, &inv.RawPayload)

		invoices = append(invoices, inv)
	}

	return invoices, nil
}
