package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// Receipt errors (docs/operations/inventory.md lists their HTTP codes).
var (
	ErrMaterialNotFound   = errors.New("material not found")
	ErrMaterialInactive   = errors.New("material is inactive")
	ErrMaterialNotStocked = errors.New("material has no stock item")
	ErrLotNotFound        = errors.New("batch lot not found")
	ErrLotOtherItem       = errors.New("batch lot belongs to another stock item")
	ErrReceiptInvalid     = errors.New("invalid receipt")
)

// ReceiptService records receipts: what physically arrived, with its
// invoice evidence, raising stock by a 'receipt' movement that references
// the receipt. A receipt whose unit has no conversion to the stock item's
// unit is recorded as needs_conversion and raises nothing until a
// conversion exists. Quantities only.
type ReceiptService struct {
	receipts  *repositories.ReceiptRepository
	materials *repositories.MaterialRepository
	stock     *repositories.StockRepository
}

// NewReceiptService creates the service over the tenant-scoped handle q.
func NewReceiptService(q repositories.DBTX) *ReceiptService {
	return &ReceiptService{receipts: repositories.NewReceiptRepository(q),
		materials: repositories.NewMaterialRepository(q), stock: repositories.NewStockRepository(q)}
}

// ReceiptInput is a receipt to record.
type ReceiptInput struct {
	MaterialID    uuid.UUID
	Quantity      float64
	Unit          string
	ReceivedAt    time.Time
	ReceivedBy    *uuid.UUID
	BatchLotID    *uuid.UUID
	LotNumber     string // find or create the lot by number (when BatchLotID is not given)
	InvoiceStatus string
	CFDIUUID      *uuid.UUID
	CFDILine      *int
	Notes         string
	// IdempotencyKey makes a retried submission return the first receipt.
	IdempotencyKey string
}

// ReceiptResult is what Receive stored and moved.
type ReceiptResult struct {
	Receipt  *repositories.Receipt
	Created  bool
	Movement *repositories.InventoryTransaction
}

// Validate checks the input's own fields (not the database).
func (in ReceiptInput) Validate() error {
	switch {
	case in.MaterialID == uuid.Nil:
		return fmt.Errorf("%w: material_id is required", ErrReceiptInvalid)
	case !(in.Quantity > 0 && in.Quantity < 1e11):
		return fmt.Errorf("%w: quantity must be greater than 0", ErrReceiptInvalid)
	case !repositories.ValidUnit(in.Unit):
		return fmt.Errorf("%w: unit must be 1-16 lowercase letters, digits or underscores", ErrReceiptInvalid)
	case in.ReceivedAt.IsZero() || in.ReceivedAt.After(time.Now().Add(5*time.Minute)):
		return fmt.Errorf("%w: received_at is required and not in the future", ErrReceiptInvalid)
	}
	switch in.InvoiceStatus {
	case repositories.ReceiptInvoiced:
		if in.CFDIUUID == nil || in.CFDILine == nil || *in.CFDILine < 1 {
			return fmt.Errorf("%w: an invoiced receipt needs cfdi_uuid and cfdi_line (1 or more)", ErrReceiptInvalid)
		}
	case repositories.ReceiptNoInvoice:
		if in.CFDIUUID != nil || in.CFDILine != nil {
			return fmt.Errorf("%w: a receipt without invoice carries no cfdi_uuid or cfdi_line", ErrReceiptInvalid)
		}
	default:
		return fmt.Errorf("%w: invoice_status must be invoiced or no_invoice", ErrReceiptInvalid)
	}
	if in.BatchLotID != nil && in.LotNumber != "" {
		return fmt.Errorf("%w: give batch_lot_id or lot_number, not both", ErrReceiptInvalid)
	}
	return nil
}

// Receive records a receipt and posts it when its unit converts.
func (s *ReceiptService) Receive(ctx context.Context, in ReceiptInput) (*ReceiptResult, error) {
	in.Unit = repositories.NormalizeUnit(in.Unit)
	if err := in.Validate(); err != nil {
		return nil, err
	}
	material, err := s.materials.Get(ctx, in.MaterialID)
	if err != nil {
		return nil, err
	}
	if material == nil {
		return nil, ErrMaterialNotFound
	}
	if !material.Active {
		return nil, ErrMaterialInactive
	}
	item, err := s.stock.LockMaterialItem(ctx, material.ID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrMaterialNotStocked
	}
	rc := &repositories.Receipt{MaterialID: material.ID, MaterialCode: material.Code, InventoryItemID: item.ID,
		Quantity: in.Quantity, Unit: in.Unit, ReceivedAt: in.ReceivedAt.UTC(), ReceivedBy: in.ReceivedBy,
		InvoiceStatus: in.InvoiceStatus, CFDIUUID: in.CFDIUUID, CFDILine: in.CFDILine, Notes: in.Notes,
		IdempotencyKey: in.IdempotencyKey}
	if in.IdempotencyKey != "" {
		// A retried submission: return the first receipt before creating a lot.
		existing, err := s.receipts.ByIdempotencyKey(ctx, in.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return &ReceiptResult{Receipt: existing}, nil
		}
	}
	lotID, err := s.lot(ctx, in, material, item)
	if err != nil {
		return nil, err
	}
	rc.BatchLotID = lotID
	created, err := s.receipts.Insert(ctx, rc)
	if err != nil {
		return nil, err
	}
	res := &ReceiptResult{Receipt: rc, Created: created}
	if !created {
		return res, nil
	}
	if res.Movement, err = s.post(ctx, rc, item); err != nil {
		return nil, err
	}
	stored, err := s.receipts.Get(ctx, rc.ID)
	if err != nil {
		return nil, err
	}
	res.Receipt = stored
	return res, nil
}

// lot resolves the receipt's batch lot: the given one (linked to the item
// when it has no item yet), or the lot with the given number, created when
// it does not exist.
func (s *ReceiptService) lot(ctx context.Context, in ReceiptInput, m *repositories.Material, item *repositories.StockItem) (*uuid.UUID, error) {
	var lot *repositories.LotRef
	var err error
	switch {
	case in.BatchLotID != nil:
		if lot, err = s.receipts.LockLot(ctx, *in.BatchLotID); err != nil {
			return nil, err
		}
		if lot == nil {
			return nil, ErrLotNotFound
		}
	case in.LotNumber != "":
		if lot, err = s.receipts.LockLotByNumber(ctx, in.LotNumber); err != nil {
			return nil, err
		}
		if lot == nil {
			created, err := s.receipts.CreateLot(ctx, in.LotNumber, m.Name, m.Code, in.Quantity, in.Unit, in.ReceivedAt, item.ID)
			if err != nil {
				return nil, err
			}
			return &created.ID, nil
		}
	default:
		return nil, nil
	}
	switch {
	case lot.InventoryItemID == nil:
		if err := s.receipts.LinkLot(ctx, lot.ID, item.ID); err != nil {
			return nil, err
		}
	case *lot.InventoryItemID != item.ID:
		return nil, ErrLotOtherItem
	}
	return &lot.ID, nil
}

// post raises stock by the receipt in the item's unit, or records a
// conversion gap. The caller holds the item lock.
func (s *ReceiptService) post(ctx context.Context, rc *repositories.Receipt, item *repositories.StockItem) (*repositories.InventoryTransaction, error) {
	factor, ok, err := stockFactor(ctx, s.stock, item, rc.Unit)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, s.stock.OpenGap(ctx, repositories.ConversionGap{InventoryItemID: item.ID, MaterialID: item.MaterialID,
			SourceType: repositories.GapSourceReceipt, SourceID: rc.ID, Quantity: rc.Quantity, FromUnit: rc.Unit, ToUnit: item.Unit})
	}
	qty := repositories.Round4(rc.Quantity * factor)
	note := fmt.Sprintf("receipt %s: %s %s %s (%s)", rc.ID, rc.MaterialCode, formatQuantity(rc.Quantity), rc.Unit, rc.InvoiceStatus)
	if factor != 1 {
		note += fmt.Sprintf(" = %s %s", formatQuantity(qty), item.Unit)
	}
	mv, err := s.stock.PostMovement(ctx, repositories.Movement{ItemID: item.ID, Type: "receipt", Quantity: qty,
		ReferenceType: repositories.StockReferenceReceipt, ReferenceID: rc.ID, Note: note})
	if err != nil {
		return nil, err
	}
	if err := s.receipts.MarkPosted(ctx, rc.ID, qty, mv.ID); err != nil {
		return nil, err
	}
	return mv, s.stock.CloseGap(ctx, repositories.GapSourceReceipt, rc.ID)
}

// PostPending posts a needs_conversion receipt once its conversion exists.
// The caller holds the item lock. It moves nothing when the receipt is
// already posted or still has no conversion.
func (s *ReceiptService) PostPending(ctx context.Context, receiptID uuid.UUID, item *repositories.StockItem) (*repositories.InventoryTransaction, error) {
	rc, err := s.receipts.Get(ctx, receiptID)
	if err != nil || rc == nil || rc.Status != repositories.ReceiptNeedsConversion {
		return nil, err
	}
	return s.post(ctx, rc, item)
}
