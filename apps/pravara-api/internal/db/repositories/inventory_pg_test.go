package repositories

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	tdb "github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
)

// quantity_available is a generated column: creating and adjusting an item
// must not write it (PostgreSQL rejects that), and it must follow on hand.
func TestInventoryPG_CreateAndAdjustLeaveTheGeneratedColumnToPostgres(t *testing.T) {
	admin := openPG(t)
	app := openRLSRole(t, admin)
	repo := NewInventoryRepository(tdb.NewTenantDB(app, nil))
	tenantID := newTenant(t, admin)
	ctx := context.Background()

	item := &InventoryItem{TenantID: tenantID, SKU: "FIL-" + uuid.NewString()[:8], Name: "PLA 1 kg", Unit: "g",
		QuantityOnHand: 1000, QuantityReserved: 100, Currency: "MXN"}
	require.NoError(t, tdb.RunInTenantTx(ctx, app, tenantID.String(), func(ctx context.Context) error {
		return repo.CreateItem(ctx, item)
	}))
	require.Equal(t, 900.0, item.QuantityAvailable)

	require.NoError(t, tdb.RunInTenantTx(ctx, app, tenantID.String(), func(ctx context.Context) error {
		return repo.AdjustQuantity(ctx, item.ID, -250, "consumption", nil, nil, nil, nil)
	}))
	var onHand, available, balance float64
	require.NoError(t, admin.QueryRow(`SELECT quantity_on_hand, quantity_available FROM inventory_items WHERE id = $1`, item.ID).
		Scan(&onHand, &available))
	require.NoError(t, admin.QueryRow(`SELECT running_balance FROM inventory_transactions WHERE inventory_item_id = $1`, item.ID).
		Scan(&balance))
	require.Equal(t, []float64{750, 650, 750}, []float64{onHand, available, balance})
}
