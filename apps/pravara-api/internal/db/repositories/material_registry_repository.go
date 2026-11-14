package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Material code registry (migration 035, RFC 0058 rulings I3 and I4):
// Pravara owns the codes; only registered materials are stock. Quantities
// only: a material has no cost, price or currency.

// MaterialCategories are the registry's categories.
var MaterialCategories = map[string]bool{"filament": true, "resin": true, "sheet": true, "spare_part": true,
	"consumable": true, "packaging": true, "other": true}

var (
	materialCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	unitPattern         = regexp.MustCompile(`^[a-z][a-z0-9_]{0,15}$`)
)

// Registry errors.
var (
	ErrMaterialCodeTaken      = errors.New("material code already registered")
	ErrMaterialAliasTaken     = errors.New("alias already names a material")
	ErrMaterialConversionDup  = errors.New("conversion already exists for these units")
	ErrMaterialAlreadyStocked = errors.New("material already has a stock item")
)

// ValidMaterialCode reports whether code is a registry code: 1-64 letters,
// digits, dot, underscore or hyphen, starting with a letter or digit (it
// fits Dhanam's item.material_code, 64 characters).
func ValidMaterialCode(code string) bool { return materialCodePattern.MatchString(code) }

// NormalizeUnit lowercases and trims a unit for comparison and storage.
func NormalizeUnit(u string) string { return strings.ToLower(strings.TrimSpace(u)) }

// ValidUnit reports whether u (normalized) is a unit name: 1-16 lowercase
// letters, digits or underscores, starting with a letter.
func ValidUnit(u string) bool { return unitPattern.MatchString(u) }

// Material is one registry entry, with its stock item when it has one.
type Material struct {
	ID       uuid.UUID `json:"id"`
	Code     string    `json:"code"`
	Name     string    `json:"name"`
	Category string    `json:"category"`
	BaseUnit string    `json:"base_unit"`
	Active   bool      `json:"active"`
	Notes    string    `json:"notes,omitempty"`
	// The stock item linked to the material (none until one is linked).
	InventoryItemID *uuid.UUID `json:"inventory_item_id,omitempty"`
	StockSKU        string     `json:"stock_sku,omitempty"`
	StockUnit       string     `json:"stock_unit,omitempty"`
	QuantityOnHand  *float64   `json:"quantity_on_hand,omitempty"`
	CreatedBy       *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// MaterialAlias is another spelling of a material's code.
type MaterialAlias struct {
	ID         uuid.UUID  `json:"id"`
	MaterialID uuid.UUID  `json:"material_id"`
	Alias      string     `json:"alias"`
	CreatedBy  *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// UnitConversion is 1 FromUnit = Factor ToUnit for one material.
type UnitConversion struct {
	ID         uuid.UUID  `json:"id"`
	MaterialID uuid.UUID  `json:"material_id"`
	FromUnit   string     `json:"from_unit"`
	ToUnit     string     `json:"to_unit"`
	Factor     float64    `json:"factor"`
	Note       string     `json:"note,omitempty"`
	CreatedBy  *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// MaterialRepository stores the registry, its aliases and conversions.
// Every statement runs in the tenant scope carried by ctx.
type MaterialRepository struct {
	db DBTX
}

// NewMaterialRepository creates the repository.
func NewMaterialRepository(db DBTX) *MaterialRepository { return &MaterialRepository{db: db} }

const materialColumns = `m.id, m.code, m.name, m.category, m.base_unit, m.active, COALESCE(m.notes, ''),
	i.id, COALESCE(i.sku, ''), COALESCE(i.unit, ''), i.quantity_on_hand, m.created_by, m.created_at, m.updated_at`

const materialFrom = ` FROM material_registry m LEFT JOIN inventory_items i ON i.material_id = m.id AND i.tenant_id = m.tenant_id`

func scanMaterial(row interface{ Scan(...any) error }) (*Material, error) {
	var m Material
	var item, createdBy uuid.NullUUID
	var onHand sql.NullFloat64
	if err := row.Scan(&m.ID, &m.Code, &m.Name, &m.Category, &m.BaseUnit, &m.Active, &m.Notes,
		&item, &m.StockSKU, &m.StockUnit, &onHand, &createdBy, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	m.InventoryItemID, m.CreatedBy, m.QuantityOnHand = ptrUUID(item), ptrUUID(createdBy), ptrFloat(onHand)
	return &m, nil
}

func uniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

// Create registers m (code, name, category, base unit; active). The code is
// unique per tenant ignoring case.
func (r *MaterialRepository) Create(ctx context.Context, m *Material) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO material_registry (id, tenant_id, code, name, category, base_unit, active, notes, created_by)
		VALUES ($1, NULLIF(current_setting('app.current_tenant_id', true), '')::uuid, $2, $3, $4, $5, $6, NULLIF($7, ''), $8)
		RETURNING created_at, updated_at`,
		m.ID, m.Code, m.Name, m.Category, m.BaseUnit, m.Active, m.Notes, m.CreatedBy).Scan(&m.CreatedAt, &m.UpdatedAt)
	if uniqueViolation(err) {
		return ErrMaterialCodeTaken
	}
	if err != nil {
		return fmt.Errorf("create material: %w", err)
	}
	return nil
}

// Get returns one material (nil when not in the tenant).
func (r *MaterialRepository) Get(ctx context.Context, id uuid.UUID) (*Material, error) {
	m, err := scanMaterial(r.db.QueryRowContext(ctx, `SELECT `+materialColumns+materialFrom+`
		WHERE m.id = $1 AND `+tenantMatchOn("m"), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get material: %w", err)
	}
	return m, nil
}

// CodeTaken reports whether s already names a material in the tenant: a
// registry code (ignoring case), an alias, or a linked stock item's SKU
// (exactly). A second spelling would make resolution ambiguous.
func (r *MaterialRepository) CodeTaken(ctx context.Context, s string) (bool, error) {
	var taken bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM material_registry WHERE lower(code) = lower($1) AND `+tenantMatch+`)
		    OR EXISTS (SELECT 1 FROM material_aliases WHERE alias = $1 AND `+tenantMatch+`)
		    OR EXISTS (SELECT 1 FROM inventory_items WHERE sku = $1 AND material_id IS NOT NULL AND `+tenantMatch+`)`,
		s).Scan(&taken)
	if err != nil {
		return false, fmt.Errorf("check material code: %w", err)
	}
	return taken, nil
}

// MaterialFilter selects registry entries.
type MaterialFilter struct {
	Active   *bool
	Category string
	Search   string // code or name, case-insensitive substring
	Limit    int
	Offset   int
}

// List returns the tenant's registry entries ordered by code, with the
// total count.
func (r *MaterialRepository) List(ctx context.Context, f MaterialFilter) ([]Material, int, error) {
	where := " WHERE " + tenantMatchOn("m")
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where += strings.ReplaceAll(" AND "+cond, "$N", fmt.Sprintf("$%d", len(args)))
	}
	if f.Active != nil {
		add("m.active = $N", *f.Active)
	}
	if f.Category != "" {
		add("m.category = $N", f.Category)
	}
	if f.Search != "" {
		add("(m.code ILIKE $N OR m.name ILIKE $N)", "%"+f.Search+"%")
	}
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM material_registry m"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count materials: %w", err)
	}
	query := "SELECT " + materialColumns + materialFrom + where + " ORDER BY lower(m.code), m.id"
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
		return nil, 0, fmt.Errorf("list materials: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Material{}
	for rows.Next() {
		m, err := scanMaterial(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan material: %w", err)
		}
		out = append(out, *m)
	}
	return out, total, rows.Err()
}

// MaterialPatch changes a material's mutable fields (nil = unchanged). The
// code never changes.
type MaterialPatch struct {
	Name     *string
	Category *string
	BaseUnit *string
	Active   *bool
	Notes    *string
}

// Update applies p. found is false when the material is not in the tenant.
func (r *MaterialRepository) Update(ctx context.Context, id uuid.UUID, p MaterialPatch) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE material_registry SET name = COALESCE($2, name), category = COALESCE($3, category),
			base_unit = COALESCE($4, base_unit), active = COALESCE($5, active),
			notes = CASE WHEN $6::text IS NULL THEN notes ELSE NULLIF($6, '') END
		WHERE id = $1 AND `+tenantMatch, id, p.Name, p.Category, p.BaseUnit, p.Active, p.Notes)
	if err != nil {
		return false, fmt.Errorf("update material: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// AddAlias stores another spelling of a material's code.
func (r *MaterialRepository) AddAlias(ctx context.Context, a *MaterialAlias) error {
	a.ID = uuid.New()
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO material_aliases (id, tenant_id, material_id, alias, created_by)
		VALUES ($1, NULLIF(current_setting('app.current_tenant_id', true), '')::uuid, $2, $3, $4)
		RETURNING created_at`, a.ID, a.MaterialID, a.Alias, a.CreatedBy).Scan(&a.CreatedAt)
	if uniqueViolation(err) {
		return ErrMaterialAliasTaken
	}
	if err != nil {
		return fmt.Errorf("add material alias: %w", err)
	}
	return nil
}

// Aliases lists a material's aliases, oldest first.
func (r *MaterialRepository) Aliases(ctx context.Context, materialID uuid.UUID) ([]MaterialAlias, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, material_id, alias, created_by, created_at FROM material_aliases
		WHERE material_id = $1 AND `+tenantMatch+` ORDER BY created_at, id`, materialID)
	if err != nil {
		return nil, fmt.Errorf("list material aliases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []MaterialAlias{}
	for rows.Next() {
		var a MaterialAlias
		var by uuid.NullUUID
		if err := rows.Scan(&a.ID, &a.MaterialID, &a.Alias, &by, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan material alias: %w", err)
		}
		a.CreatedBy = ptrUUID(by)
		out = append(out, a)
	}
	return out, rows.Err()
}

// AddConversion stores a conversion. A pair already stored in either
// direction is ErrMaterialConversionDup (one factor per pair, never two
// that could disagree).
func (r *MaterialRepository) AddConversion(ctx context.Context, c *UnitConversion) error {
	var exists bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM material_unit_conversions
		WHERE material_id = $1 AND ((from_unit = $2 AND to_unit = $3) OR (from_unit = $3 AND to_unit = $2))
		  AND `+tenantMatch+`)`, c.MaterialID, c.FromUnit, c.ToUnit).Scan(&exists); err != nil {
		return fmt.Errorf("check unit conversion: %w", err)
	}
	if exists {
		return ErrMaterialConversionDup
	}
	c.ID = uuid.New()
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO material_unit_conversions (id, tenant_id, material_id, from_unit, to_unit, factor, note, created_by)
		VALUES ($1, NULLIF(current_setting('app.current_tenant_id', true), '')::uuid, $2, $3, $4, $5, NULLIF($6, ''), $7)
		RETURNING created_at`, c.ID, c.MaterialID, c.FromUnit, c.ToUnit, c.Factor, c.Note, c.CreatedBy).Scan(&c.CreatedAt)
	if uniqueViolation(err) {
		return ErrMaterialConversionDup
	}
	if err != nil {
		return fmt.Errorf("add unit conversion: %w", err)
	}
	return nil
}

// Conversions lists a material's conversions, oldest first.
func (r *MaterialRepository) Conversions(ctx context.Context, materialID uuid.UUID) ([]UnitConversion, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, material_id, from_unit, to_unit, factor, COALESCE(note, ''), created_by, created_at
		FROM material_unit_conversions WHERE material_id = $1 AND `+tenantMatch+` ORDER BY created_at, id`, materialID)
	if err != nil {
		return nil, fmt.Errorf("list unit conversions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []UnitConversion{}
	for rows.Next() {
		var c UnitConversion
		var by uuid.NullUUID
		if err := rows.Scan(&c.ID, &c.MaterialID, &c.FromUnit, &c.ToUnit, &c.Factor, &c.Note, &by, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan unit conversion: %w", err)
		}
		c.CreatedBy = ptrUUID(by)
		out = append(out, c)
	}
	return out, rows.Err()
}

// UnlinkedItem is a stock item with no registry entry, and why the
// migration did not register it.
type UnlinkedItem struct {
	ID     uuid.UUID `json:"id"`
	SKU    string    `json:"sku"`
	Name   string    `json:"name"`
	Unit   string    `json:"unit"`
	Reason string    `json:"reason"`
}

// UnlinkedLot is a batch lot with no stock item.
type UnlinkedLot struct {
	ID          uuid.UUID `json:"id"`
	LotNumber   string    `json:"lot_number"`
	ProductCode string    `json:"product_code,omitempty"`
	ProductName string    `json:"product_name"`
}

// Reasons an item is not linked.
const (
	UnlinkedInvalidCode   = "sku_is_not_a_valid_code"
	UnlinkedInvalidUnit   = "unit_is_not_a_valid_unit"
	UnlinkedCaseDuplicate = "sku_differs_from_another_only_in_case"
	UnlinkedCodeTaken     = "code_registered_for_another_material"
	UnlinkedNotLinked     = "not_linked"
)

// UnlinkedItems lists stock items with no registry entry (at most limit),
// each with the reason.
func (r *MaterialRepository) UnlinkedItems(ctx context.Context, limit int) ([]UnlinkedItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT i.id, i.sku, i.name, i.unit,
		       EXISTS (SELECT 1 FROM inventory_items o WHERE o.tenant_id = i.tenant_id AND o.id <> i.id
		               AND lower(o.sku) = lower(i.sku)),
		       EXISTS (SELECT 1 FROM material_registry m WHERE m.tenant_id = i.tenant_id AND lower(m.code) = lower(i.sku))
		FROM inventory_items i WHERE i.material_id IS NULL AND `+tenantMatchOn("i")+`
		ORDER BY i.sku, i.id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list unlinked items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []UnlinkedItem{}
	for rows.Next() {
		var it UnlinkedItem
		var dup, taken bool
		if err := rows.Scan(&it.ID, &it.SKU, &it.Name, &it.Unit, &dup, &taken); err != nil {
			return nil, fmt.Errorf("scan unlinked item: %w", err)
		}
		switch {
		case !ValidMaterialCode(it.SKU):
			it.Reason = UnlinkedInvalidCode
		case !ValidUnit(NormalizeUnit(it.Unit)):
			it.Reason = UnlinkedInvalidUnit
		case dup:
			it.Reason = UnlinkedCaseDuplicate
		case taken:
			it.Reason = UnlinkedCodeTaken
		default:
			it.Reason = UnlinkedNotLinked
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// UnlinkedLots lists batch lots with no stock item (at most limit).
func (r *MaterialRepository) UnlinkedLots(ctx context.Context, limit int) ([]UnlinkedLot, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, lot_number, COALESCE(product_code, ''), product_name FROM batch_lots
		WHERE inventory_item_id IS NULL AND `+tenantMatch+` ORDER BY lot_number, id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list unlinked lots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []UnlinkedLot{}
	for rows.Next() {
		var l UnlinkedLot
		if err := rows.Scan(&l.ID, &l.LotNumber, &l.ProductCode, &l.ProductName); err != nil {
			return nil, fmt.Errorf("scan unlinked lot: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
