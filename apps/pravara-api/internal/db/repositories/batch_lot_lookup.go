package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// LotLookup is a lot found by its number, with the stock item and registry
// material it is linked to (RFC 0064 §4.2, ruling PV-6): a lot tag names
// the lot and nothing else, so which material a lot is, is what Pravara
// says it is. It carries no supplier, purchase-order or metadata fields:
// a terminal needs the lot's identity, status and material, nothing more.
type LotLookup struct {
	ID          uuid.UUID  `json:"id"`
	LotNumber   string     `json:"lot_number"`
	Status      string     `json:"status"`
	ProductName string     `json:"product_name"`
	ProductCode string     `json:"product_code,omitempty"`
	Quantity    float64    `json:"quantity"`
	Unit        string     `json:"unit"`
	ExpiryDate  *time.Time `json:"expiry_date,omitempty"`
	// InventoryItemID and SKU: the stock item the lot is linked to
	// (batch_lots.inventory_item_id, migration 035); absent when unlinked.
	InventoryItemID *uuid.UUID `json:"inventory_item_id,omitempty"`
	SKU             string     `json:"sku,omitempty"`
	// MaterialID and MaterialCode: the registry material of that stock item,
	// the code a consumption report names; absent when the item has none.
	MaterialID   *uuid.UUID `json:"material_id,omitempty"`
	MaterialCode string     `json:"material_code,omitempty"`
}

// LookupByLotNumber returns the tenant's lot with exactly this number (lot
// numbers are unique per tenant, migration 009), or nil when there is none.
func (r *BatchLotRepository) LookupByLotNumber(ctx context.Context, lotNumber string) (*LotLookup, error) {
	query := `
		SELECT bl.id, bl.lot_number, bl.status, bl.product_name, bl.product_code,
		       bl.quantity, bl.unit, bl.expiry_date,
		       ii.id, ii.sku, m.id, m.code
		FROM batch_lots bl
		LEFT JOIN inventory_items ii ON ii.id = bl.inventory_item_id AND ii.tenant_id = bl.tenant_id
		LEFT JOIN material_registry m ON m.id = ii.material_id AND m.tenant_id = bl.tenant_id
		WHERE bl.lot_number = $1 AND ` + tenantMatchOn("bl")

	var (
		lot                       LotLookup
		productCode, sku, matCode sql.NullString
		expiry                    sql.NullTime
		itemID, materialID        uuid.NullUUID
	)
	err := r.db.QueryRowContext(ctx, query, lotNumber).Scan(
		&lot.ID, &lot.LotNumber, &lot.Status, &lot.ProductName, &productCode,
		&lot.Quantity, &lot.Unit, &expiry,
		&itemID, &sku, &materialID, &matCode,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to look up batch lot: %w", err)
	}
	lot.ProductCode = productCode.String
	if expiry.Valid {
		lot.ExpiryDate = &expiry.Time
	}
	if itemID.Valid {
		id := itemID.UUID
		lot.InventoryItemID = &id
		lot.SKU = sku.String
	}
	if materialID.Valid {
		id := materialID.UUID
		lot.MaterialID = &id
		lot.MaterialCode = matCode.String
	}
	return &lot, nil
}
