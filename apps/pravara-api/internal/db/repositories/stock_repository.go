package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Reference types of inventory_transactions rows posted by the inventory
// records of migration 035 (job consumption keeps
// StockReferenceJobConsumption).
const (
	StockReferenceReceipt = "inventory_receipt"
	StockReferenceCount   = "inventory_count"
)

// Conversion gap sources and statuses (inventory_conversion_gaps, 035).
const (
	GapSourceJobConsumption = "job_consumption"
	GapSourceReceipt        = "inventory_receipt"
	GapSourceCountLine      = "inventory_count_line"

	GapOpen       = "open"
	GapResolved   = "resolved"
	GapSuperseded = "superseded"
)

// StockItem is the inventory item a material maps to, as stock movements
// need it.
type StockItem struct {
	ID         uuid.UUID
	SKU        string
	Name       string
	Unit       string
	MaterialID *uuid.UUID
}

// StockRepository locks stock items, posts movements and keeps the list of
// quantities waiting for a unit conversion. Every statement runs in the
// tenant scope carried by ctx; callers lock the item before they post.
type StockRepository struct {
	db DBTX
}

// NewStockRepository creates the repository.
func NewStockRepository(db DBTX) *StockRepository { return &StockRepository{db: db} }

const stockItemColumns = `i.id, i.sku, i.name, i.unit, i.material_id`

func scanStockItem(row *sql.Row) (*StockItem, error) {
	var it StockItem
	var material uuid.NullUUID
	err := row.Scan(&it.ID, &it.SKU, &it.Name, &it.Unit, &material)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	it.MaterialID = ptrUUID(material)
	return &it, nil
}

// ResolveStockItem returns, locked for update, the stock item a consumption
// material code names: the item linked to the code's registry entry (code,
// alias, or the SKU of a linked item); when the code is not registered, an
// unlinked item whose SKU is the code (stock recorded before the registry).
// nil when the code names no stock item.
func (r *StockRepository) ResolveStockItem(ctx context.Context, code string) (*StockItem, error) {
	it, err := scanStockItem(r.db.QueryRowContext(ctx, `
		WITH m AS (SELECT material_resolve($1) AS id)
		SELECT `+stockItemColumns+` FROM inventory_items i, m
		WHERE ((m.id IS NOT NULL AND i.material_id = m.id)
		    OR (m.id IS NULL AND i.material_id IS NULL AND i.sku = $1))
		  AND `+tenantMatchOn("i")+`
		FOR UPDATE OF i`, code))
	if err != nil {
		return nil, fmt.Errorf("resolve stock item: %w", err)
	}
	return it, nil
}

// LockItem returns the item, locked for update (nil when not in the tenant).
func (r *StockRepository) LockItem(ctx context.Context, itemID uuid.UUID) (*StockItem, error) {
	it, err := scanStockItem(r.db.QueryRowContext(ctx, `
		SELECT `+stockItemColumns+` FROM inventory_items i
		WHERE i.id = $1 AND `+tenantMatchOn("i")+` FOR UPDATE OF i`, itemID))
	if err != nil {
		return nil, fmt.Errorf("lock inventory item: %w", err)
	}
	return it, nil
}

// LockMaterialItem returns the stock item linked to a registry entry,
// locked for update (nil when the material has no stock item).
func (r *StockRepository) LockMaterialItem(ctx context.Context, materialID uuid.UUID) (*StockItem, error) {
	it, err := scanStockItem(r.db.QueryRowContext(ctx, `
		SELECT `+stockItemColumns+` FROM inventory_items i
		WHERE i.material_id = $1 AND `+tenantMatchOn("i")+` FOR UPDATE OF i`, materialID))
	if err != nil {
		return nil, fmt.Errorf("lock material stock item: %w", err)
	}
	return it, nil
}

// MaterialConversion returns the stored factor (1 from = factor to) of a
// material, or of its inverse pair (then 1/factor). ok is false when neither
// is stored.
func (r *StockRepository) MaterialConversion(ctx context.Context, materialID uuid.UUID, from, to string) (float64, bool, error) {
	var factor float64
	var inverse bool
	err := r.db.QueryRowContext(ctx, `
		SELECT factor, from_unit <> $2 FROM material_unit_conversions
		WHERE material_id = $1 AND ((from_unit = $2 AND to_unit = $3) OR (from_unit = $3 AND to_unit = $2))
		  AND `+tenantMatch+`
		ORDER BY (from_unit = $2) DESC LIMIT 1`, materialID, from, to).Scan(&factor, &inverse)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read unit conversion: %w", err)
	}
	if inverse {
		return 1 / factor, true, nil
	}
	return factor, true, nil
}

// Movement is one stock movement to post.
type Movement struct {
	ItemID        uuid.UUID
	Type          string // inventory_transaction_type
	Quantity      float64
	ReferenceType string
	ReferenceID   uuid.UUID
	Note          string
	CreatedBy     *uuid.UUID
}

// PostMovement changes the item's quantity on hand by m.Quantity (negative
// relieves stock) and records the movement with its running balance. The
// caller holds the item lock in the same transaction.
func (r *StockRepository) PostMovement(ctx context.Context, m Movement) (*InventoryTransaction, error) {
	t := &InventoryTransaction{ID: uuid.New(), InventoryItemID: m.ItemID, TransactionType: m.Type, Quantity: Round4(m.Quantity)}
	err := r.db.QueryRowContext(ctx, `
		UPDATE inventory_items SET quantity_on_hand = quantity_on_hand + $2, updated_at = NOW()
		WHERE id = $1 AND `+tenantMatch+`
		RETURNING tenant_id, quantity_on_hand`, m.ItemID, t.Quantity).Scan(&t.TenantID, &t.RunningBalance)
	if err != nil {
		return nil, fmt.Errorf("move inventory item: %w", err)
	}
	ref, refID, note := m.ReferenceType, m.ReferenceID, m.Note
	t.ReferenceType, t.ReferenceID, t.Notes, t.CreatedBy = &ref, &refID, &note, m.CreatedBy
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO inventory_transactions (id, tenant_id, inventory_item_id, transaction_type, quantity,
			running_balance, reference_type, reference_id, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at`, t.ID, t.TenantID, m.ItemID, m.Type, t.Quantity, t.RunningBalance,
		ref, refID, note, m.CreatedBy).Scan(&t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("record inventory transaction: %w", err)
	}
	return t, nil
}

// BalanceAt is the item's quantity on hand at a moment: today's on hand
// minus every movement recorded after it. The caller holds the item lock.
func (r *StockRepository) BalanceAt(ctx context.Context, itemID uuid.UUID, at time.Time) (float64, error) {
	var q float64
	err := r.db.QueryRowContext(ctx, `
		SELECT i.quantity_on_hand - COALESCE((SELECT SUM(t.quantity) FROM inventory_transactions t
			WHERE t.inventory_item_id = i.id AND t.created_at > $2 AND `+tenantMatchOn("t")+`), 0)
		FROM inventory_items i WHERE i.id = $1 AND `+tenantMatchOn("i"), itemID, at).Scan(&q)
	if err != nil {
		return 0, fmt.Errorf("balance at: %w", err)
	}
	return q, nil
}

// ConversionGap is a quantity recorded in a unit that has no conversion to
// its stock item's unit: stock waits until a conversion exists.
type ConversionGap struct {
	ID              uuid.UUID  `json:"id"`
	InventoryItemID uuid.UUID  `json:"inventory_item_id"`
	SKU             string     `json:"sku"`
	MaterialID      *uuid.UUID `json:"material_id,omitempty"`
	MaterialCode    string     `json:"material_code,omitempty"`
	SourceType      string     `json:"source_type"`
	SourceID        uuid.UUID  `json:"source_id"`
	JobKind         string     `json:"job_kind,omitempty"`
	JobID           *uuid.UUID `json:"job_id,omitempty"`
	Quantity        float64    `json:"quantity"`
	FromUnit        string     `json:"from_unit"`
	ToUnit          string     `json:"to_unit"`
	Status          string     `json:"status"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// OpenGap records that g waits for a conversion (one open gap per source;
// recording it again is a no-op). For job consumption, the job's older open
// gaps for the same item are superseded.
func (r *StockRepository) OpenGap(ctx context.Context, g ConversionGap) error {
	if g.SourceType == GapSourceJobConsumption && g.JobID != nil {
		if _, err := r.db.ExecContext(ctx, `
			UPDATE inventory_conversion_gaps SET status = 'superseded', resolved_at = NOW()
			WHERE status = 'open' AND source_type = 'job_consumption' AND inventory_item_id = $1
			  AND job_kind = $2 AND job_id = $3 AND source_id <> $4 AND `+tenantMatch,
			g.InventoryItemID, g.JobKind, *g.JobID, g.SourceID); err != nil {
			return fmt.Errorf("supersede conversion gaps: %w", err)
		}
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO inventory_conversion_gaps (tenant_id, inventory_item_id, material_id, source_type, source_id,
			job_kind, job_id, quantity, from_unit, to_unit)
		VALUES (NULLIF(current_setting('app.current_tenant_id', true), '')::uuid, $1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9)
		ON CONFLICT (tenant_id, source_type, source_id) WHERE status = 'open' DO NOTHING`,
		g.InventoryItemID, g.MaterialID, g.SourceType, g.SourceID, g.JobKind, g.JobID, Round4(g.Quantity), g.FromUnit, g.ToUnit)
	if err != nil {
		return fmt.Errorf("record conversion gap: %w", err)
	}
	return nil
}

// CloseJobGaps resolves the open job consumption gaps of a job and item: its
// stock relief no longer waits.
func (r *StockRepository) CloseJobGaps(ctx context.Context, itemID uuid.UUID, jobKind string, jobID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE inventory_conversion_gaps SET status = 'resolved', resolved_at = NOW()
		WHERE status = 'open' AND source_type = 'job_consumption' AND inventory_item_id = $1
		  AND job_kind = $2 AND job_id = $3 AND `+tenantMatch, itemID, jobKind, jobID)
	if err != nil {
		return fmt.Errorf("close conversion gaps: %w", err)
	}
	return nil
}

// CloseGap resolves one open gap of a receipt or count line.
func (r *StockRepository) CloseGap(ctx context.Context, sourceType string, sourceID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE inventory_conversion_gaps SET status = 'resolved', resolved_at = NOW()
		WHERE status = 'open' AND source_type = $1 AND source_id = $2 AND `+tenantMatch, sourceType, sourceID)
	if err != nil {
		return fmt.Errorf("close conversion gap: %w", err)
	}
	return nil
}

// GapFilter selects conversion gaps.
type GapFilter struct {
	Status string // open (default), resolved, superseded or all
	ItemID *uuid.UUID
	Limit  int
	Offset int
}

// ListGaps returns the tenant's conversion gaps, oldest first, with the
// total count.
func (r *StockRepository) ListGaps(ctx context.Context, f GapFilter) ([]ConversionGap, int, error) {
	where := " WHERE " + tenantMatchOn("g")
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where += fmt.Sprintf(" AND "+cond, len(args))
	}
	if f.Status != "" && f.Status != "all" {
		add("g.status = $%d", f.Status)
	}
	if f.ItemID != nil {
		add("g.inventory_item_id = $%d", *f.ItemID)
	}
	from := ` FROM inventory_conversion_gaps g JOIN inventory_items i ON i.id = g.inventory_item_id
		LEFT JOIN material_registry m ON m.id = g.material_id`
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT count(*)"+from+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count conversion gaps: %w", err)
	}
	query := `SELECT g.id, g.inventory_item_id, i.sku, g.material_id, COALESCE(m.code, ''), g.source_type, g.source_id,
		COALESCE(g.job_kind, ''), g.job_id, g.quantity, g.from_unit, g.to_unit, g.status, g.resolved_at, g.created_at` +
		from + where + " ORDER BY g.created_at, g.id"
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
		return nil, 0, fmt.Errorf("list conversion gaps: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ConversionGap{}
	for rows.Next() {
		var g ConversionGap
		var material, job uuid.NullUUID
		var resolved sql.NullTime
		if err := rows.Scan(&g.ID, &g.InventoryItemID, &g.SKU, &material, &g.MaterialCode, &g.SourceType, &g.SourceID,
			&g.JobKind, &job, &g.Quantity, &g.FromUnit, &g.ToUnit, &g.Status, &resolved, &g.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan conversion gap: %w", err)
		}
		g.MaterialID, g.JobID = ptrUUID(material), ptrUUID(job)
		if resolved.Valid {
			g.ResolvedAt = &resolved.Time
		}
		out = append(out, g)
	}
	return out, total, rows.Err()
}

// OpenGapsForItem returns the item's open gaps (oldest first), for
// re-posting once a conversion exists.
func (r *StockRepository) OpenGapsForItem(ctx context.Context, itemID uuid.UUID) ([]ConversionGap, error) {
	gaps, _, err := r.ListGaps(ctx, GapFilter{Status: GapOpen, ItemID: &itemID})
	return gaps, err
}
