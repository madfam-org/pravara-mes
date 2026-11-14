package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// Material registry errors.
var (
	ErrMaterialInvalid   = errors.New("invalid material")
	ErrConversionBuiltin = errors.New("this conversion is built in")
	ErrConversionInvalid = errors.New("invalid conversion")
	ErrAliasInvalid      = errors.New("invalid alias")
	ErrItemNotFound      = errors.New("inventory item not found")
	ErrItemNotLinkable   = errors.New("inventory item not found or already linked to a material")
)

// MaterialService keeps the material code registry (ruling I3), its
// aliases and unit conversions. Adding a conversion settles every quantity
// that was waiting for it (conversion gaps) in the same transaction.
type MaterialService struct {
	materials   *repositories.MaterialRepository
	stock       *repositories.StockRepository
	consumption *ConsumptionService
	receipts    *ReceiptService
	counts      *CountService
}

// NewMaterialService creates the service over the tenant-scoped handle q.
func NewMaterialService(q repositories.DBTX, consumption *ConsumptionService, receipts *ReceiptService, counts *CountService) *MaterialService {
	return &MaterialService{materials: repositories.NewMaterialRepository(q), stock: repositories.NewStockRepository(q),
		consumption: consumption, receipts: receipts, counts: counts}
}

// Create registers a material and, with linkItem, links that unlinked stock
// item to it in the same transaction. The code must be new to the tenant:
// not a code (ignoring case), an alias, or a linked stock item's SKU; the
// SKU of an unlinked item only when that item is the one linked.
func (s *MaterialService) Create(ctx context.Context, m *repositories.Material, linkItem *uuid.UUID) error {
	m.Code, m.Name = strings.TrimSpace(m.Code), strings.TrimSpace(m.Name)
	m.BaseUnit, m.Category = repositories.NormalizeUnit(m.BaseUnit), strings.TrimSpace(m.Category)
	switch {
	case !repositories.ValidMaterialCode(m.Code):
		return fmt.Errorf("%w: code must be 1-64 letters, digits, '.', '_' or '-', starting with a letter or digit", ErrMaterialInvalid)
	case m.Name == "" || len(m.Name) > 255:
		return fmt.Errorf("%w: name is required (at most 255 characters)", ErrMaterialInvalid)
	case !repositories.MaterialCategories[m.Category]:
		return fmt.Errorf("%w: category must be one of filament, resin, sheet, spare_part, consumable, packaging, other", ErrMaterialInvalid)
	case !repositories.ValidUnit(m.BaseUnit):
		return fmt.Errorf("%w: base_unit must be 1-16 lowercase letters, digits or underscores", ErrMaterialInvalid)
	}
	use, err := s.materials.CodeUse(ctx, m.Code)
	if err != nil {
		return err
	}
	switch {
	case use.Taken:
		return repositories.ErrMaterialCodeTaken
	case use.UnlinkedItemID != nil && (linkItem == nil || *linkItem != *use.UnlinkedItemID):
		// Consumption under this SKU relieves that item today; registering
		// the code without linking it would re-point it.
		return fmt.Errorf("%w: unlinked stock item %s has this SKU; send it as inventory_item_id to register and link it together",
			repositories.ErrMaterialCodeTaken, *use.UnlinkedItemID)
	}
	if err := s.materials.Create(ctx, m); err != nil {
		return err
	}
	if linkItem == nil {
		return nil
	}
	linked, err := s.materials.LinkItem(ctx, *linkItem, m.ID)
	if err != nil {
		return err
	}
	if !linked {
		return ErrItemNotLinkable
	}
	got, err := s.materials.Get(ctx, m.ID)
	if err == nil && got != nil {
		*m = *got
	}
	return err
}

// Update changes a material's mutable fields; the code never changes.
func (s *MaterialService) Update(ctx context.Context, id uuid.UUID, p repositories.MaterialPatch) (*repositories.Material, error) {
	if p.Name != nil {
		n := strings.TrimSpace(*p.Name)
		if n == "" || len(n) > 255 {
			return nil, fmt.Errorf("%w: name is required (at most 255 characters)", ErrMaterialInvalid)
		}
		p.Name = &n
	}
	if p.Category != nil && !repositories.MaterialCategories[*p.Category] {
		return nil, fmt.Errorf("%w: category must be one of filament, resin, sheet, spare_part, consumable, packaging, other", ErrMaterialInvalid)
	}
	if p.BaseUnit != nil {
		u := repositories.NormalizeUnit(*p.BaseUnit)
		if !repositories.ValidUnit(u) {
			return nil, fmt.Errorf("%w: base_unit must be 1-16 lowercase letters, digits or underscores", ErrMaterialInvalid)
		}
		p.BaseUnit = &u
	}
	found, err := s.materials.Update(ctx, id, p)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrMaterialNotFound
	}
	return s.materials.Get(ctx, id)
}

// AddAlias names a material by another code (a material class, a
// supplier's code). It changes how consumption recorded from now on
// resolves; stock already relieved, or not, is not re-settled.
func (s *MaterialService) AddAlias(ctx context.Context, materialID uuid.UUID, alias string, by *uuid.UUID) (*repositories.MaterialAlias, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" || len(alias) > 100 {
		return nil, fmt.Errorf("%w: alias must be 1-100 characters", ErrAliasInvalid)
	}
	m, err := s.materials.Get(ctx, materialID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrMaterialNotFound
	}
	use, err := s.materials.CodeUse(ctx, alias)
	if err != nil {
		return nil, err
	}
	switch {
	case use.Taken:
		return nil, repositories.ErrMaterialAliasTaken
	case use.UnlinkedItemID != nil:
		return nil, fmt.Errorf("%w: unlinked stock item %s has this SKU; link that item to a material instead",
			repositories.ErrMaterialAliasTaken, *use.UnlinkedItemID)
	}
	a := &repositories.MaterialAlias{MaterialID: m.ID, Alias: alias, CreatedBy: by}
	return a, s.materials.AddAlias(ctx, a)
}

// ConversionResult is a stored conversion and what it settled.
type ConversionResult struct {
	Conversion *repositories.UnitConversion
	// GapsResolved counts the waiting quantities posted because of it.
	GapsResolved int
	Movements    []repositories.InventoryTransaction
}

// AddConversion stores 1 from = factor to for a material, then settles the
// material's waiting quantities (consumption relief, receipts and count
// lines) that it makes convertible.
func (s *MaterialService) AddConversion(ctx context.Context, c *repositories.UnitConversion) (*ConversionResult, error) {
	c.FromUnit, c.ToUnit = repositories.NormalizeUnit(c.FromUnit), repositories.NormalizeUnit(c.ToUnit)
	switch {
	case !repositories.ValidUnit(c.FromUnit) || !repositories.ValidUnit(c.ToUnit):
		return nil, fmt.Errorf("%w: from_unit and to_unit must be 1-16 lowercase letters, digits or underscores", ErrConversionInvalid)
	case c.FromUnit == c.ToUnit:
		return nil, fmt.Errorf("%w: from_unit and to_unit differ", ErrConversionInvalid)
	case math.IsNaN(c.Factor) || math.IsInf(c.Factor, 0) || c.Factor <= 0 || c.Factor >= 1e10:
		return nil, fmt.Errorf("%w: factor must be a positive number", ErrConversionInvalid)
	}
	if _, ok := BuiltinConversion(c.FromUnit, c.ToUnit); ok {
		return nil, ErrConversionBuiltin
	}
	m, err := s.materials.Get(ctx, c.MaterialID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrMaterialNotFound
	}
	if err := s.materials.AddConversion(ctx, c); err != nil {
		return nil, err
	}
	res := &ConversionResult{Conversion: c, Movements: []repositories.InventoryTransaction{}}
	if m.InventoryItemID == nil {
		return res, nil
	}
	// Lock every item the waiting quantities may move, in one ordered
	// statement, before touching any (StockRepository.LockItems).
	ids := []uuid.UUID{*m.InventoryItemID}
	gaps, err := s.stock.OpenGapsForItem(ctx, *m.InventoryItemID)
	if err != nil {
		return nil, err
	}
	for _, g := range gaps {
		if g.SourceType == repositories.GapSourceJobConsumption {
			more, err := s.consumption.JobLockSet(ctx, g.JobKind, *g.JobID, nil)
			if err != nil {
				return nil, err
			}
			ids = append(ids, more...)
		}
	}
	if err := s.stock.LockItems(ctx, ids); err != nil {
		return nil, err
	}
	item, err := s.stock.LockMaterialItem(ctx, m.ID)
	if err != nil || item == nil {
		return res, err
	}
	n, moves, err := s.resolveGaps(ctx, item)
	res.GapsResolved, res.Movements = n, append(res.Movements, moves...)
	return res, err
}

// resolveGaps re-posts every open gap of item. The caller holds the lock.
func (s *MaterialService) resolveGaps(ctx context.Context, item *repositories.StockItem) (int, []repositories.InventoryTransaction, error) {
	gaps, err := s.stock.OpenGapsForItem(ctx, item.ID)
	if err != nil {
		return 0, nil, err
	}
	var moves []repositories.InventoryTransaction
	for _, g := range gaps {
		var mv *repositories.InventoryTransaction
		switch g.SourceType {
		case repositories.GapSourceJobConsumption:
			var job []repositories.InventoryTransaction
			job, err = s.consumption.ResettleJob(ctx, g.JobKind, *g.JobID, item)
			moves = append(moves, job...)
		case repositories.GapSourceReceipt:
			mv, err = s.receipts.PostPending(ctx, g.SourceID, item)
		case repositories.GapSourceCountLine:
			err = s.counts.ResolveLine(ctx, g.SourceID)
		}
		if err != nil {
			return 0, nil, err
		}
		if mv != nil {
			moves = append(moves, *mv)
		}
	}
	left, err := s.stock.OpenGapsForItem(ctx, item.ID)
	if err != nil {
		return 0, nil, err
	}
	return len(gaps) - len(left), moves, nil
}
