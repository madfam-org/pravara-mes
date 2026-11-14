package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Count statuses and line results (inventory_counts, 035).
const (
	CountOpen   = "open"
	CountClosed = "closed"

	CountLineMatched         = "matched"
	CountLineAdjusted        = "adjusted"
	CountLineNeedsConversion = "needs_conversion"
	CountLineNotStocked      = "not_stocked"
)

// ErrOpeningCountExists is returned when a tenant already has its opening
// count.
var ErrOpeningCountExists = errors.New("the opening count already exists")

// Count is a physical count. Closing it posts each difference as an
// `adjustment` with reference_type 'inventory_count' (ruling I8).
type Count struct {
	ID        uuid.UUID   `json:"id"`
	Status    string      `json:"status"`
	IsOpening bool        `json:"is_opening"`
	Notes     string      `json:"notes,omitempty"`
	CreatedBy *uuid.UUID  `json:"created_by,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	ClosedBy  *uuid.UUID  `json:"closed_by,omitempty"`
	ClosedAt  *time.Time  `json:"closed_at,omitempty"`
	LineCount int         `json:"line_count"`
	Lines     []CountLine `json:"lines,omitempty"`
}

// CountLine is one material counted. The result fields are set when the
// count closes.
type CountLine struct {
	ID                   uuid.UUID  `json:"id"`
	CountID              uuid.UUID  `json:"count_id"`
	MaterialID           uuid.UUID  `json:"material_id"`
	MaterialCode         string     `json:"material_code"`
	CountedQuantity      float64    `json:"counted_quantity"`
	Unit                 string     `json:"unit"`
	CountedAt            time.Time  `json:"counted_at"`
	CountedBy            *uuid.UUID `json:"counted_by,omitempty"`
	Result               string     `json:"result,omitempty"`
	InventoryItemID      *uuid.UUID `json:"inventory_item_id,omitempty"`
	ExpectedQuantity     *float64   `json:"expected_quantity,omitempty"`
	CountedStockQuantity *float64   `json:"counted_stock_quantity,omitempty"`
	Difference           *float64   `json:"difference,omitempty"`
	TransactionID        *uuid.UUID `json:"transaction_id,omitempty"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// CountRepository stores counts and their lines. Every statement runs in
// the tenant scope carried by ctx.
type CountRepository struct {
	db DBTX
}

// NewCountRepository creates the repository.
func NewCountRepository(db DBTX) *CountRepository { return &CountRepository{db: db} }

// Create opens a count.
func (r *CountRepository) Create(ctx context.Context, c *Count) error {
	c.ID, c.Status = uuid.New(), CountOpen
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO inventory_counts (id, tenant_id, is_opening, notes, created_by)
		VALUES ($1, NULLIF(current_setting('app.current_tenant_id', true), '')::uuid, $2, NULLIF($3, ''), $4)
		RETURNING created_at`, c.ID, c.IsOpening, c.Notes, c.CreatedBy).Scan(&c.CreatedAt)
	if uniqueViolation(err) {
		return ErrOpeningCountExists
	}
	if err != nil {
		return fmt.Errorf("create count: %w", err)
	}
	return nil
}

const countColumns = `c.id, c.status, c.is_opening, COALESCE(c.notes, ''), c.created_by, c.created_at, c.closed_by, c.closed_at,
	(SELECT count(*) FROM inventory_count_lines l WHERE l.count_id = c.id AND ` + "l." + tenantMatch + `)`

func scanCount(row interface{ Scan(...any) error }) (*Count, error) {
	var c Count
	var by, closedBy uuid.NullUUID
	var closed sql.NullTime
	if err := row.Scan(&c.ID, &c.Status, &c.IsOpening, &c.Notes, &by, &c.CreatedAt, &closedBy, &closed, &c.LineCount); err != nil {
		return nil, err
	}
	c.CreatedBy, c.ClosedBy = ptrUUID(by), ptrUUID(closedBy)
	if closed.Valid {
		c.ClosedAt = &closed.Time
	}
	return &c, nil
}

// Get returns a count without its lines (nil when not in the tenant). With
// lock, the count row is locked for update.
func (r *CountRepository) Get(ctx context.Context, id uuid.UUID, lock bool) (*Count, error) {
	q := `SELECT ` + countColumns + ` FROM inventory_counts c WHERE c.id = $1 AND ` + tenantMatchOn("c")
	if lock {
		q += ` FOR UPDATE OF c`
	}
	c, err := scanCount(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get count: %w", err)
	}
	return c, nil
}

// List returns the tenant's counts, newest first, with the total.
func (r *CountRepository) List(ctx context.Context, status string, limit, offset int) ([]Count, int, error) {
	where := ` WHERE ($1 = '' OR c.status = $1) AND ` + tenantMatchOn("c")
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM inventory_counts c`+where, status).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count counts: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+countColumns+` FROM inventory_counts c`+where+`
		ORDER BY c.created_at DESC, c.id LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Count{}
	for rows.Next() {
		c, err := scanCount(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan count: %w", err)
		}
		out = append(out, *c)
	}
	return out, total, rows.Err()
}

const countLineColumns = `l.id, l.count_id, l.material_id, m.code, l.counted_quantity, l.unit, l.counted_at, l.counted_by,
	COALESCE(l.result, ''), l.inventory_item_id, l.expected_quantity, l.counted_stock_quantity, l.difference,
	l.transaction_id, l.updated_at`

func scanCountLine(row interface{ Scan(...any) error }) (*CountLine, error) {
	var l CountLine
	var by, item, txn uuid.NullUUID
	var expected, stock, diff sql.NullFloat64
	if err := row.Scan(&l.ID, &l.CountID, &l.MaterialID, &l.MaterialCode, &l.CountedQuantity, &l.Unit, &l.CountedAt, &by,
		&l.Result, &item, &expected, &stock, &diff, &txn, &l.UpdatedAt); err != nil {
		return nil, err
	}
	l.CountedBy, l.InventoryItemID, l.TransactionID = ptrUUID(by), ptrUUID(item), ptrUUID(txn)
	l.ExpectedQuantity, l.CountedStockQuantity, l.Difference = ptrFloat(expected), ptrFloat(stock), ptrFloat(diff)
	return &l, nil
}

// UpsertLine records (or re-records, while the count is open) the counted
// quantity of one material.
func (r *CountRepository) UpsertLine(ctx context.Context, l *CountLine) error {
	l.CountedQuantity = Round4(l.CountedQuantity)
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO inventory_count_lines (id, tenant_id, count_id, material_id, counted_quantity, unit, counted_at, counted_by)
		VALUES ($1, NULLIF(current_setting('app.current_tenant_id', true), '')::uuid, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (count_id, material_id) DO UPDATE SET counted_quantity = EXCLUDED.counted_quantity,
			unit = EXCLUDED.unit, counted_at = EXCLUDED.counted_at, counted_by = EXCLUDED.counted_by, updated_at = NOW()
		RETURNING id, updated_at`, uuid.New(), l.CountID, l.MaterialID, l.CountedQuantity, l.Unit, l.CountedAt, l.CountedBy,
	).Scan(&l.ID, &l.UpdatedAt)
	if err != nil {
		return fmt.Errorf("record count line: %w", err)
	}
	return nil
}

// Lines lists a count's lines by material code.
func (r *CountRepository) Lines(ctx context.Context, countID uuid.UUID) ([]CountLine, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+countLineColumns+` FROM inventory_count_lines l
		JOIN material_registry m ON m.id = l.material_id
		WHERE l.count_id = $1 AND `+tenantMatchOn("l")+` ORDER BY lower(m.code), l.id`, countID)
	if err != nil {
		return nil, fmt.Errorf("list count lines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []CountLine{}
	for rows.Next() {
		l, err := scanCountLine(rows)
		if err != nil {
			return nil, fmt.Errorf("scan count line: %w", err)
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// Line returns one line (nil when not in the tenant).
func (r *CountRepository) Line(ctx context.Context, id uuid.UUID) (*CountLine, error) {
	l, err := scanCountLine(r.db.QueryRowContext(ctx, `SELECT `+countLineColumns+` FROM inventory_count_lines l
		JOIN material_registry m ON m.id = l.material_id WHERE l.id = $1 AND `+tenantMatchOn("l"), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get count line: %w", err)
	}
	return l, nil
}

// SetLineResult stores what closing (or a later conversion) did with a line.
func (r *CountRepository) SetLineResult(ctx context.Context, l *CountLine) error {
	_, err := r.db.ExecContext(ctx, `UPDATE inventory_count_lines SET result = $2, inventory_item_id = $3,
		expected_quantity = $4, counted_stock_quantity = $5, difference = $6, transaction_id = $7, updated_at = NOW()
		WHERE id = $1 AND `+tenantMatch, l.ID, l.Result, l.InventoryItemID, l.ExpectedQuantity, l.CountedStockQuantity,
		l.Difference, l.TransactionID)
	if err != nil {
		return fmt.Errorf("store count line result: %w", err)
	}
	return nil
}

// Close marks the count closed.
func (r *CountRepository) Close(ctx context.Context, id uuid.UUID, by *uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE inventory_counts SET status = 'closed', closed_at = NOW(), closed_by = $2
		WHERE id = $1 AND status = 'open' AND `+tenantMatch, id, by)
	if err != nil {
		return fmt.Errorf("close count: %w", err)
	}
	return nil
}
