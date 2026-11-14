package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// Count errors.
var (
	ErrCountNotFound = errors.New("count not found")
	ErrCountClosed   = errors.New("count is closed")
	ErrCountInvalid  = errors.New("invalid count line")
)

// CountService runs physical counts. Closing a count compares each counted
// material with the stock on hand when it was counted and posts the
// difference as an `adjustment` (reference_type 'inventory_count', the line
// as reference_id): count differences are their own movement, never folded
// into a job (ruling I8). Closing is idempotent.
type CountService struct {
	counts    *repositories.CountRepository
	materials *repositories.MaterialRepository
	stock     *repositories.StockRepository
}

// NewCountService creates the service over the tenant-scoped handle q.
func NewCountService(q repositories.DBTX) *CountService {
	return &CountService{counts: repositories.NewCountRepository(q),
		materials: repositories.NewMaterialRepository(q), stock: repositories.NewStockRepository(q)}
}

// Create opens a count; isOpening marks the tenant's one opening count.
func (s *CountService) Create(ctx context.Context, isOpening bool, notes string, by *uuid.UUID) (*repositories.Count, error) {
	c := &repositories.Count{IsOpening: isOpening, Notes: notes, CreatedBy: by}
	if err := s.counts.Create(ctx, c); err != nil {
		return nil, err
	}
	c.Lines = []repositories.CountLine{}
	return c, nil
}

// Get returns a count with its lines (ErrCountNotFound when not in the
// tenant).
func (s *CountService) Get(ctx context.Context, id uuid.UUID) (*repositories.Count, error) {
	c, err := s.counts.Get(ctx, id, false)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, ErrCountNotFound
	}
	if c.Lines, err = s.counts.Lines(ctx, id); err != nil {
		return nil, err
	}
	return c, nil
}

// List returns the tenant's counts without lines, newest first.
func (s *CountService) List(ctx context.Context, status string, limit, offset int) ([]repositories.Count, int, error) {
	return s.counts.List(ctx, status, limit, offset)
}

// LineInput is one counted material.
type LineInput struct {
	MaterialID      uuid.UUID
	CountedQuantity float64
	Unit            string
	CountedAt       time.Time // zero = now
	CountedBy       *uuid.UUID
}

// RecordLine records (or re-records) a material's counted quantity while
// the count is open.
func (s *CountService) RecordLine(ctx context.Context, countID uuid.UUID, in LineInput) (*repositories.CountLine, error) {
	in.Unit = repositories.NormalizeUnit(in.Unit)
	now := time.Now().UTC()
	if in.CountedAt.IsZero() {
		in.CountedAt = now
	}
	switch {
	case in.MaterialID == uuid.Nil:
		return nil, fmt.Errorf("%w: material_id is required", ErrCountInvalid)
	case math.IsNaN(in.CountedQuantity) || in.CountedQuantity < 0 || in.CountedQuantity >= 1e11:
		return nil, fmt.Errorf("%w: counted_quantity must be 0 or more", ErrCountInvalid)
	case !repositories.ValidUnit(in.Unit):
		return nil, fmt.Errorf("%w: unit must be 1-16 lowercase letters, digits or underscores", ErrCountInvalid)
	case in.CountedAt.After(now.Add(5 * time.Minute)):
		return nil, fmt.Errorf("%w: counted_at is in the future", ErrCountInvalid)
	}
	c, err := s.counts.Get(ctx, countID, true)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, ErrCountNotFound
	}
	if c.Status != repositories.CountOpen {
		return nil, ErrCountClosed
	}
	m, err := s.materials.Get(ctx, in.MaterialID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrMaterialNotFound
	}
	l := &repositories.CountLine{CountID: countID, MaterialID: m.ID, MaterialCode: m.Code,
		CountedQuantity: in.CountedQuantity, Unit: in.Unit, CountedAt: in.CountedAt.UTC(), CountedBy: in.CountedBy}
	if err := s.counts.UpsertLine(ctx, l); err != nil {
		return nil, err
	}
	return l, nil
}

// CloseResult is a closed count and whether this call closed it.
type CloseResult struct {
	Count         *repositories.Count
	AlreadyClosed bool
}

// Close applies every line and closes the count. Closing a closed count
// returns it unchanged with AlreadyClosed and posts nothing.
func (s *CountService) Close(ctx context.Context, countID uuid.UUID, by *uuid.UUID) (*CloseResult, error) {
	c, err := s.counts.Get(ctx, countID, true)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, ErrCountNotFound
	}
	if c.Status == repositories.CountClosed {
		full, err := s.Get(ctx, countID)
		return &CloseResult{Count: full, AlreadyClosed: true}, err
	}
	lines, err := s.counts.Lines(ctx, countID)
	if err != nil {
		return nil, err
	}
	for i := range lines {
		if err := s.applyLine(ctx, c, &lines[i]); err != nil {
			return nil, err
		}
	}
	if err := s.counts.Close(ctx, countID, by); err != nil {
		return nil, err
	}
	full, err := s.Get(ctx, countID)
	return &CloseResult{Count: full}, err
}

// applyLine compares one line with stock and posts the difference. The
// item is locked here.
func (s *CountService) applyLine(ctx context.Context, c *repositories.Count, l *repositories.CountLine) error {
	item, err := s.stock.LockMaterialItem(ctx, l.MaterialID)
	if err != nil {
		return err
	}
	if item == nil {
		l.Result = repositories.CountLineNotStocked
		return s.counts.SetLineResult(ctx, l)
	}
	l.InventoryItemID = &item.ID
	factor, ok, err := stockFactor(ctx, s.stock, item, l.Unit)
	if err != nil {
		return err
	}
	if !ok {
		l.Result = repositories.CountLineNeedsConversion
		if err := s.stock.OpenGap(ctx, repositories.ConversionGap{InventoryItemID: item.ID, MaterialID: item.MaterialID,
			SourceType: repositories.GapSourceCountLine, SourceID: l.ID, Quantity: l.CountedQuantity,
			FromUnit: l.Unit, ToUnit: item.Unit}); err != nil {
			return err
		}
		return s.counts.SetLineResult(ctx, l)
	}
	expected, err := s.stock.BalanceAt(ctx, item.ID, l.CountedAt)
	if err != nil {
		return err
	}
	counted := repositories.Round4(l.CountedQuantity * factor)
	expected = repositories.Round4(expected)
	diff := repositories.Round4(counted - expected)
	l.ExpectedQuantity, l.CountedStockQuantity, l.Difference = &expected, &counted, &diff
	l.Result = repositories.CountLineMatched
	if diff != 0 {
		kind := "count"
		if c.IsOpening {
			kind = "opening count"
		}
		note := fmt.Sprintf("%s %s: %s counted %s %s, expected %s %s", kind, c.ID, l.MaterialCode,
			formatQuantity(counted), item.Unit, formatQuantity(expected), item.Unit)
		mv, err := s.stock.PostMovement(ctx, repositories.Movement{ItemID: item.ID, Type: "adjustment", Quantity: diff,
			ReferenceType: repositories.StockReferenceCount, ReferenceID: l.ID, Note: note})
		if err != nil {
			return err
		}
		l.Result, l.TransactionID = repositories.CountLineAdjusted, &mv.ID
	}
	if err := s.counts.SetLineResult(ctx, l); err != nil {
		return err
	}
	return s.stock.CloseGap(ctx, repositories.GapSourceCountLine, l.ID)
}

// ResolveLine applies a closed count's needs_conversion line once its
// conversion exists. It moves nothing otherwise.
func (s *CountService) ResolveLine(ctx context.Context, lineID uuid.UUID) error {
	l, err := s.counts.Line(ctx, lineID)
	if err != nil || l == nil || l.Result != repositories.CountLineNeedsConversion {
		return err
	}
	c, err := s.counts.Get(ctx, l.CountID, false)
	if err != nil || c == nil {
		return err
	}
	return s.applyLine(ctx, c, l)
}
