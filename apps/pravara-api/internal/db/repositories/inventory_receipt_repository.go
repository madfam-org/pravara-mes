package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Receipt invoice statuses and posting statuses (inventory_receipts, 035).
const (
	ReceiptInvoiced  = "invoiced"
	ReceiptNoInvoice = "no_invoice"

	ReceiptPosted          = "posted"
	ReceiptNeedsConversion = "needs_conversion"
)

// Receipt is what physically arrived: a registered material, its quantity
// and unit, when and by whom, its lot, and its invoice evidence (a CFDI UUID
// and line position, or an explicit «no invoice», ruling I7). Quantities
// only: a receipt carries no cost, price or currency.
type Receipt struct {
	ID              uuid.UUID  `json:"id"`
	MaterialID      uuid.UUID  `json:"material_id"`
	MaterialCode    string     `json:"material_code"`
	InventoryItemID uuid.UUID  `json:"inventory_item_id"`
	Quantity        float64    `json:"quantity"`
	Unit            string     `json:"unit"`
	ReceivedAt      time.Time  `json:"received_at"`
	ReceivedBy      *uuid.UUID `json:"received_by,omitempty"`
	BatchLotID      *uuid.UUID `json:"batch_lot_id,omitempty"`
	LotNumber       string     `json:"lot_number,omitempty"`
	InvoiceStatus   string     `json:"invoice_status"`
	CFDIUUID        *uuid.UUID `json:"cfdi_uuid,omitempty"`
	CFDILine        *int       `json:"cfdi_line,omitempty"`
	Status          string     `json:"status"`
	// StockQuantity is Quantity in the stock item's unit, once posted.
	StockQuantity  *float64   `json:"stock_quantity,omitempty"`
	TransactionID  *uuid.UUID `json:"transaction_id,omitempty"`
	PostedAt       *time.Time `json:"posted_at,omitempty"`
	Notes          string     `json:"notes,omitempty"`
	IdempotencyKey string     `json:"idempotency_key,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ReceiptRepository stores receipts. Every statement runs in the tenant
// scope carried by ctx.
type ReceiptRepository struct {
	db DBTX
}

// NewReceiptRepository creates the repository.
func NewReceiptRepository(db DBTX) *ReceiptRepository { return &ReceiptRepository{db: db} }

const receiptColumns = `r.id, r.material_id, m.code, r.inventory_item_id, r.quantity, r.unit, r.received_at, r.received_by,
	r.batch_lot_id, COALESCE(b.lot_number, ''), r.invoice_status, r.cfdi_uuid, r.cfdi_line, r.status, r.stock_quantity,
	r.transaction_id, r.posted_at, COALESCE(r.notes, ''), COALESCE(r.idempotency_key, ''), r.created_at`

const receiptFrom = ` FROM inventory_receipts r JOIN material_registry m ON m.id = r.material_id
	LEFT JOIN batch_lots b ON b.id = r.batch_lot_id`

func scanReceipt(row interface{ Scan(...any) error }) (*Receipt, error) {
	var r Receipt
	var by, lot, cfdi, txn uuid.NullUUID
	var line sql.NullInt64
	var stockQty sql.NullFloat64
	var posted sql.NullTime
	if err := row.Scan(&r.ID, &r.MaterialID, &r.MaterialCode, &r.InventoryItemID, &r.Quantity, &r.Unit, &r.ReceivedAt, &by,
		&lot, &r.LotNumber, &r.InvoiceStatus, &cfdi, &line, &r.Status, &stockQty, &txn, &posted, &r.Notes,
		&r.IdempotencyKey, &r.CreatedAt); err != nil {
		return nil, err
	}
	r.ReceivedBy, r.BatchLotID, r.CFDIUUID, r.TransactionID = ptrUUID(by), ptrUUID(lot), ptrUUID(cfdi), ptrUUID(txn)
	r.StockQuantity = ptrFloat(stockQty)
	if line.Valid {
		n := int(line.Int64)
		r.CFDILine = &n
	}
	if posted.Valid {
		r.PostedAt = &posted.Time
	}
	return &r, nil
}

// Insert stores rc as needs_conversion (Post marks it posted). With an
// idempotency key already used, it stores nothing and returns the stored
// receipt with created false.
func (r *ReceiptRepository) Insert(ctx context.Context, rc *Receipt) (bool, error) {
	if rc.ID == uuid.Nil {
		rc.ID = uuid.New()
	}
	rc.Quantity = Round4(rc.Quantity)
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO inventory_receipts (id, tenant_id, material_id, inventory_item_id, quantity, unit, received_at,
			received_by, batch_lot_id, invoice_status, cfdi_uuid, cfdi_line, status, notes, idempotency_key)
		VALUES ($1, NULLIF(current_setting('app.current_tenant_id', true), '')::uuid, $2, $3, $4, $5, $6, $7, $8, $9,
			$10, $11, 'needs_conversion', NULLIF($12, ''), NULLIF($13, ''))
		ON CONFLICT (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		RETURNING created_at`,
		rc.ID, rc.MaterialID, rc.InventoryItemID, rc.Quantity, rc.Unit, rc.ReceivedAt, rc.ReceivedBy, rc.BatchLotID,
		rc.InvoiceStatus, rc.CFDIUUID, rc.CFDILine, rc.Notes, rc.IdempotencyKey).Scan(&rc.CreatedAt)
	if err == nil {
		rc.Status = ReceiptNeedsConversion
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("insert receipt: %w", err)
	}
	existing, err := scanReceipt(r.db.QueryRowContext(ctx, `SELECT `+receiptColumns+receiptFrom+`
		WHERE r.idempotency_key = $1 AND `+tenantMatchOn("r"), rc.IdempotencyKey))
	if err != nil {
		return false, fmt.Errorf("load receipt by idempotency key: %w", err)
	}
	*rc = *existing
	return false, nil
}

// MarkPosted records the receipt's movement.
func (r *ReceiptRepository) MarkPosted(ctx context.Context, id uuid.UUID, stockQuantity float64, txnID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE inventory_receipts SET status = 'posted', stock_quantity = $2,
		transaction_id = $3, posted_at = NOW() WHERE id = $1 AND status = 'needs_conversion' AND `+tenantMatch,
		id, Round4(stockQuantity), txnID)
	if err != nil {
		return fmt.Errorf("mark receipt posted: %w", err)
	}
	return nil
}

// Get returns one receipt (nil when not in the tenant).
func (r *ReceiptRepository) Get(ctx context.Context, id uuid.UUID) (*Receipt, error) {
	rc, err := scanReceipt(r.db.QueryRowContext(ctx, `SELECT `+receiptColumns+receiptFrom+`
		WHERE r.id = $1 AND `+tenantMatchOn("r"), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get receipt: %w", err)
	}
	return rc, nil
}

// ByIdempotencyKey returns the receipt stored with key (nil when none).
func (r *ReceiptRepository) ByIdempotencyKey(ctx context.Context, key string) (*Receipt, error) {
	rc, err := scanReceipt(r.db.QueryRowContext(ctx, `SELECT `+receiptColumns+receiptFrom+`
		WHERE r.idempotency_key = $1 AND `+tenantMatchOn("r"), key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get receipt by idempotency key: %w", err)
	}
	return rc, nil
}

// ReceiptFilter selects receipts.
type ReceiptFilter struct {
	MaterialID    *uuid.UUID
	InvoiceStatus string
	Status        string
	From, To      *time.Time // received_at in [From, To)
	Limit, Offset int
}

// List returns the tenant's receipts, newest received first, with the total.
func (r *ReceiptRepository) List(ctx context.Context, f ReceiptFilter) ([]Receipt, int, error) {
	where := " WHERE " + tenantMatchOn("r")
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where += fmt.Sprintf(" AND "+cond, len(args))
	}
	if f.MaterialID != nil {
		add("r.material_id = $%d", *f.MaterialID)
	}
	if f.InvoiceStatus != "" {
		add("r.invoice_status = $%d", f.InvoiceStatus)
	}
	if f.Status != "" {
		add("r.status = $%d", f.Status)
	}
	if f.From != nil {
		add("r.received_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("r.received_at < $%d", *f.To)
	}
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM inventory_receipts r"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count receipts: %w", err)
	}
	query := "SELECT " + receiptColumns + receiptFrom + where + " ORDER BY r.received_at DESC, r.id"
	if f.Limit > 0 {
		args = append(args, f.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if f.Offset > 0 {
		args = append(args, f.Offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list receipts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Receipt{}
	for rows.Next() {
		rc, err := scanReceipt(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan receipt: %w", err)
		}
		out = append(out, *rc)
	}
	return out, total, rows.Err()
}

// LotRef is a batch lot as a receipt links it.
type LotRef struct {
	ID              uuid.UUID
	LotNumber       string
	InventoryItemID *uuid.UUID
}

// LockLot returns a batch lot by id, locked for update (nil when not in the
// tenant).
func (r *ReceiptRepository) LockLot(ctx context.Context, id uuid.UUID) (*LotRef, error) {
	return r.lockLot(ctx, `id = $1`, id)
}

// LockLotByNumber returns a batch lot by lot number, locked (nil when none).
func (r *ReceiptRepository) LockLotByNumber(ctx context.Context, number string) (*LotRef, error) {
	return r.lockLot(ctx, `lot_number = $1`, number)
}

func (r *ReceiptRepository) lockLot(ctx context.Context, cond string, arg any) (*LotRef, error) {
	var l LotRef
	var item uuid.NullUUID
	err := r.db.QueryRowContext(ctx, `SELECT id, lot_number, inventory_item_id FROM batch_lots
		WHERE `+cond+` AND `+tenantMatch+` FOR UPDATE`, arg).Scan(&l.ID, &l.LotNumber, &item)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock batch lot: %w", err)
	}
	l.InventoryItemID = ptrUUID(item)
	return &l, nil
}

// LinkLot sets a lot's stock item.
func (r *ReceiptRepository) LinkLot(ctx context.Context, lotID, itemID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE batch_lots SET inventory_item_id = $2, updated_at = NOW()
		WHERE id = $1 AND `+tenantMatch, lotID, itemID)
	if err != nil {
		return fmt.Errorf("link batch lot: %w", err)
	}
	return nil
}

// CreateLot creates a batch lot for a receipt (lot number unique per
// tenant), linked to the stock item.
func (r *ReceiptRepository) CreateLot(ctx context.Context, number, productName, productCode string, quantity float64,
	unit string, receivedAt time.Time, itemID uuid.UUID) (*LotRef, error) {
	l := &LotRef{ID: uuid.New(), LotNumber: number, InventoryItemID: &itemID}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO batch_lots (id, tenant_id, lot_number, product_name, product_code, quantity, unit, received_date,
			status, inventory_item_id)
		VALUES ($1, NULLIF(current_setting('app.current_tenant_id', true), '')::uuid, $2, $3, $4, $5, $6, $7, 'active', $8)`,
		l.ID, number, productName, productCode, Round4(quantity), unit, receivedAt, itemID)
	if err != nil {
		return nil, fmt.Errorf("create batch lot: %w", err)
	}
	return l, nil
}
