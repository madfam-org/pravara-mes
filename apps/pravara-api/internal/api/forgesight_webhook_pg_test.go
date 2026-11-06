package api

// The ForgeSight webhook on PostgreSQL, through the real routes and auth:
// an item ForgeSight already references keeps its unit cost, stock and every
// other column; an id ForgeSight sends that Pravara does not stock creates
// nothing; no inventory transaction is written. Opt-in with
// PRAVARA_TEST_DATABASE_URL (newScopeRig skips without it).

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForgeSightWebhookLeavesCostAndStockOnPostgres(t *testing.T) {
	rig := newScopeRig(t)

	var itemID uuid.UUID
	var updatedBefore time.Time
	require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO inventory_items
		(tenant_id, sku, name, unit, quantity_on_hand, quantity_reserved, forgesight_id, unit_cost, currency, metadata)
		VALUES ($1, 'FIL-PLA-BLK', 'PLA black 1 kg', 'g', 1000, 100, 'fs-pla-1', 250.00, 'MXN', '{"warehouse": "A1"}')
		RETURNING id, updated_at`, rig.tenantID).Scan(&itemID, &updatedBefore))

	cost := 999.99
	payload := ForgeSightWebhookPayload{Event: "inventory.sync", Items: []ForgeSightWebhookItem{
		{ForgeSightID: "fs-pla-1", SKU: "FIL-PLA-BLK", Name: "Renamed by ForgeSight", Category: "resin",
			Unit: "kg", Quantity: 5, UnitCost: &cost, Currency: "USD"},
		{ForgeSightID: "fs-new-2", SKU: "FIL-NEW", Unit: "g", Quantity: 40, UnitCost: &cost},
	}}

	// A person's token and a wildcard API key both reach the route (it is
	// not open to Janua machine tokens, as before).
	for _, bearer := range []string{rig.userToken(t, "admin"), rig.apiKey(t, "*")} {
		w := rig.do(http.MethodPost, "/v1/webhooks/forgesight", bearer, payload)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"reference_only":true`)
		assert.Contains(t, w.Body.String(), `"processed":2`)
	}

	var (
		unitCost                    float64
		onHand, reserved, available float64
		name, unit, currency, meta  string
		category                    *string
		updatedAfter                time.Time
	)
	require.NoError(t, rig.sqlDB.QueryRow(`SELECT unit_cost, quantity_on_hand, quantity_reserved, quantity_available,
		name, unit, currency, category, metadata::text, updated_at FROM inventory_items WHERE id = $1`, itemID).
		Scan(&unitCost, &onHand, &reserved, &available, &name, &unit, &currency, &category, &meta, &updatedAfter))
	assert.Equal(t, 250.00, unitCost, "unit_cost is not ForgeSight's to set")
	assert.Equal(t, []float64{1000, 100, 900}, []float64{onHand, reserved, available}, "stock is not ForgeSight's to set")
	assert.Equal(t, []string{"PLA black 1 kg", "g", "MXN", `{"warehouse": "A1"}`}, []string{name, unit, currency, meta})
	assert.Nil(t, category)
	assert.True(t, updatedAfter.Equal(updatedBefore), "the row was not touched")

	var items, created, movements int
	require.NoError(t, rig.sqlDB.QueryRow(`SELECT count(*) FROM inventory_items WHERE tenant_id = $1`, rig.tenantID).Scan(&items))
	require.NoError(t, rig.sqlDB.QueryRow(`SELECT count(*) FROM inventory_items WHERE forgesight_id = 'fs-new-2'`).Scan(&created))
	require.NoError(t, rig.sqlDB.QueryRow(`SELECT count(*) FROM inventory_transactions WHERE tenant_id = $1`, rig.tenantID).Scan(&movements))
	assert.Equal(t, []int{1, 0, 0}, []int{items, created, movements})
}
