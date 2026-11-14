package api

// RFC 0058 wave 1 on PostgreSQL, with the API connected as pravara_app
// (NOSUPERUSER NOBYPASSRLS): the material code registry, unit conversions,
// receipts with invoice evidence, conversion gaps (a quantity with no
// conversion is recorded and moves no stock), physical counts (closing is
// idempotent) and consumption coverage. Opt-in with
// PRAVARA_TEST_DATABASE_URL; no machine is reached (the rig's fakes only).

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// call sends one request as token and decodes the JSON answer, requiring
// the status want.
func (r *dispatchRig) call(t *testing.T, token, method, path string, body any, want int) map[string]any {
	t.Helper()
	w := r.do(method, path, token, body)
	require.Equalf(t, want, w.Code, "%s %s: %s", method, path, w.Body.String())
	out := map[string]any{}
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	}
	return out
}

func (r *dispatchRig) onHand(t *testing.T, item string) float64 {
	t.Helper()
	var q float64
	require.NoError(t, r.admin.QueryRow(`SELECT quantity_on_hand FROM inventory_items WHERE id = $1`, item).Scan(&q))
	return q
}

func (r *dispatchRig) movementCount(t *testing.T, refType string) int {
	t.Helper()
	var n int
	require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM inventory_transactions WHERE reference_type = $1`, refType).Scan(&n))
	return n
}

func errCode(m map[string]any) string { s, _ := m["error"].(string); return s }

// newStockedMaterial registers a material and a stock item linked to it.
func (r *dispatchRig) newStockedMaterial(t *testing.T, person, code, category, unit string, onHand float64) (string, string) {
	t.Helper()
	m := r.call(t, person, http.MethodPost, "/v1/inventory/materials",
		map[string]any{"code": code, "name": code + " (synthetic)", "category": category, "base_unit": unit}, http.StatusCreated)
	item := r.call(t, person, http.MethodPost, "/v1/inventory",
		map[string]any{"sku": code, "name": code + " stock", "unit": unit, "quantity_on_hand": onHand, "material_id": m["id"]},
		http.StatusCreated)
	assert.Equal(t, code, item["material_code"])
	return m["id"].(string), item["id"].(string)
}

func TestInventoryRegistryReceiptsAndConversions(t *testing.T) {
	r := newDispatchRig(t)
	person := r.userToken(t, "admin") // inventory writes need an inventory role

	// 1. Registry: codes are unique ignoring case and never guessed.
	plaID, plaItem := r.newStockedMaterial(t, person, "FIL-PLA-BLK", "filament", "g", 0)
	assert.Equal(t, "material_code_taken", errCode(r.call(t, person, http.MethodPost, "/v1/inventory/materials",
		map[string]any{"code": "fil-pla-blk", "name": "dup", "category": "filament", "base_unit": "g"}, http.StatusConflict)))
	for _, bad := range []map[string]any{
		{"code": "has space", "name": "x", "category": "filament", "base_unit": "g"},
		{"code": "OK-1", "name": "x", "category": "money", "base_unit": "g"},
		{"code": "OK-1", "name": "x", "category": "filament", "base_unit": "Grams!"},
	} {
		assert.Equal(t, "validation_error", errCode(r.call(t, person, http.MethodPost, "/v1/inventory/materials", bad, http.StatusBadRequest)))
	}
	assert.Equal(t, "material_already_stocked", errCode(r.call(t, person, http.MethodPost, "/v1/inventory",
		map[string]any{"sku": "OTHER", "name": "second item", "unit": "g", "material_id": plaID}, http.StatusConflict)))
	assert.Equal(t, http.StatusForbidden, r.do(http.MethodPost, "/v1/inventory/materials", r.machineToken(t, "pravara-mes:jobs", nil),
		map[string]any{"code": "M-1", "name": "x", "category": "other", "base_unit": "g"}).Code, "registry writes are people only")
	assert.Equal(t, http.StatusOK, r.do(http.MethodGet, "/v1/inventory/materials", r.machineToken(t, "pravara-cogs:read", nil), nil).Code,
		"the cost ledger reads the registry")

	// 2. Receipt with invoice evidence; kg → g is built in.
	cfdi := uuid.NewString()
	rc := r.call(t, person, http.MethodPost, "/v1/inventory/receipts", map[string]any{"idempotency_key": "synthetic-1", "material_id": plaID, "quantity": 2,
		"unit": "kg", "invoice_status": "invoiced", "cfdi_uuid": cfdi, "cfdi_line": 3, "lot_number": "LOT-SYN-0001"}, http.StatusCreated)
	receipt := rc["receipt"].(map[string]any)
	assert.Equal(t, "posted", receipt["status"])
	assert.Equal(t, 2000.0, receipt["stock_quantity"])
	assert.Equal(t, cfdi, receipt["cfdi_uuid"])
	assert.Equal(t, "LOT-SYN-0001", receipt["lot_number"])
	assert.Equal(t, "receipt", rc["stock_movement"].(map[string]any)["transaction_type"])
	assert.Equal(t, 2000.0, r.onHand(t, plaItem))
	var lotItem string
	require.NoError(t, r.admin.QueryRow(`SELECT inventory_item_id FROM batch_lots WHERE lot_number = 'LOT-SYN-0001'`).Scan(&lotItem))
	assert.Equal(t, plaItem, lotItem, "the lot is created for the stock item")

	// Invoice evidence is mandatory and consistent (ruling I7).
	for _, bad := range []map[string]any{
		{"material_id": plaID, "quantity": 1, "unit": "g", "idempotency_key": "bad-1"},
		{"material_id": plaID, "quantity": 1, "unit": "g", "invoice_status": "invoiced", "cfdi_uuid": cfdi, "idempotency_key": "bad-2"},
		{"material_id": plaID, "quantity": 1, "unit": "g", "invoice_status": "no_invoice", "cfdi_uuid": cfdi, "cfdi_line": 1, "idempotency_key": "bad-3"},
		{"material_id": plaID, "quantity": 0, "unit": "g", "invoice_status": "no_invoice", "idempotency_key": "bad-4"},
		{"material_id": plaID, "quantity": 1, "unit": "g", "invoice_status": "no_invoice", "unit_cost": 5, "idempotency_key": "money-1"},
	} {
		w := r.do(http.MethodPost, "/v1/inventory/receipts", person, bad)
		if _, money := bad["unit_cost"]; money {
			// An unknown field is ignored by the binder, and never stored:
			// receipts have no cost column at all.
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			assert.NotContains(t, w.Body.String(), "unit_cost")
			continue
		}
		assert.Equal(t, http.StatusBadRequest, w.Code, "%v: %s", bad, w.Body.String())
	}
	assert.Equal(t, 2001.0, r.onHand(t, plaItem))

	// 3. No conversion: recorded as needs_conversion, stock untouched, gap listed.
	before := r.movementCount(t, "inventory_receipt")
	rc = r.call(t, person, http.MethodPost, "/v1/inventory/receipts", map[string]any{"material_id": plaID, "quantity": 1,
		"unit": "spool", "invoice_status": "no_invoice", "idempotency_key": "synthetic-receipt-1"}, http.StatusCreated)
	spool := rc["receipt"].(map[string]any)
	assert.Equal(t, "needs_conversion", spool["status"])
	assert.Nil(t, rc["stock_movement"])
	assert.Equal(t, 2001.0, r.onHand(t, plaItem), "no conversion: stock untouched")
	assert.Equal(t, before, r.movementCount(t, "inventory_receipt"))
	gaps := r.call(t, person, http.MethodGet, "/v1/inventory/conversion-gaps", nil, http.StatusOK)
	require.EqualValues(t, 1, gaps["total"])
	gap := gaps["data"].([]any)[0].(map[string]any)
	assert.Equal(t, "inventory_receipt", gap["source_type"])
	assert.Equal(t, spool["id"], gap["source_id"])
	assert.Equal(t, "spool", gap["from_unit"])

	// A retry with the same idempotency key returns the first receipt.
	again := r.call(t, person, http.MethodPost, "/v1/inventory/receipts", map[string]any{"material_id": plaID, "quantity": 1,
		"unit": "spool", "invoice_status": "no_invoice", "idempotency_key": "synthetic-receipt-1"}, http.StatusOK)
	assert.Equal(t, true, again["replayed"])
	assert.Equal(t, spool["id"], again["receipt"].(map[string]any)["id"])

	// 4. The conversion posts what waited for it, in the same request.
	assert.Equal(t, "conversion_builtin", errCode(r.call(t, person, http.MethodPost, "/v1/inventory/materials/"+plaID+"/conversions",
		map[string]any{"from_unit": "kg", "to_unit": "g", "factor": 1000}, http.StatusConflict)))
	conv := r.call(t, person, http.MethodPost, "/v1/inventory/materials/"+plaID+"/conversions",
		map[string]any{"from_unit": "spool", "to_unit": "g", "factor": 1000, "note": "1 kg spool"}, http.StatusCreated)
	assert.EqualValues(t, 1, conv["gaps_resolved"])
	assert.Equal(t, 3001.0, r.onHand(t, plaItem))
	assert.Equal(t, "conversion_exists", errCode(r.call(t, person, http.MethodPost, "/v1/inventory/materials/"+plaID+"/conversions",
		map[string]any{"from_unit": "g", "to_unit": "spool", "factor": 0.001}, http.StatusConflict)), "one factor per pair")
	got := r.call(t, person, http.MethodGet, "/v1/inventory/receipts/"+spool["id"].(string), nil, http.StatusOK)
	assert.Equal(t, "posted", got["status"])
	assert.EqualValues(t, 0, r.call(t, person, http.MethodGet, "/v1/inventory/conversion-gaps", nil, http.StatusOK)["total"])
	detail := r.call(t, person, http.MethodGet, "/v1/inventory/materials/"+plaID, nil, http.StatusOK)
	assert.Len(t, detail["conversions"], 1)

	// 5. A lot of another stock item is refused.
	_, petgItem := r.newStockedMaterial(t, person, "FIL-PETG-CLR", "filament", "g", 0)
	var otherLot string
	require.NoError(t, r.admin.QueryRow(`INSERT INTO batch_lots (tenant_id, lot_number, product_name, quantity, unit, inventory_item_id)
		VALUES ($1, 'LOT-SYN-PETG', 'PETG', 1, 'g', $2) RETURNING id`, r.tenantID, petgItem).Scan(&otherLot))
	assert.Equal(t, "lot_belongs_to_other_item", errCode(r.call(t, person, http.MethodPost, "/v1/inventory/receipts",
		map[string]any{"material_id": plaID, "quantity": 1, "unit": "g", "invoice_status": "no_invoice", "batch_lot_id": otherLot,
			"idempotency_key": "synthetic-lot"},
		http.StatusConflict)))

	// 6. Consumption: a registered code relieves stock; a unit with no
	// conversion is flagged and moves nothing.
	task := r.seedTask(t, boxSpecs)
	report := "/v1/tasks/" + task.String() + "/consumption"
	r.call(t, person, http.MethodPost, report, map[string]any{"material_code": "FIL-PLA-BLK", "quantity": 25, "unit": "g"}, http.StatusCreated)
	assert.Equal(t, 2976.0, r.onHand(t, plaItem))
	sheetID, sheetItem := r.newStockedMaterial(t, person, "SHT-ACR-3MM", "sheet", "pcs", 10)
	out := r.call(t, person, http.MethodPost, report, map[string]any{"material_code": "SHT-ACR-3MM", "quantity": 2, "unit": "sheet"}, http.StatusCreated)
	assert.Empty(t, out["stock_movements"])
	assert.Equal(t, 10.0, r.onHand(t, sheetItem), "sheet → pcs has no conversion: stock untouched")
	gaps = r.call(t, person, http.MethodGet, "/v1/inventory/conversion-gaps", nil, http.StatusOK)
	require.EqualValues(t, 1, gaps["total"])
	assert.Equal(t, "job_consumption", gaps["data"].([]any)[0].(map[string]any)["source_type"])
	conv = r.call(t, person, http.MethodPost, "/v1/inventory/materials/"+sheetID+"/conversions",
		map[string]any{"from_unit": "sheet", "to_unit": "pcs", "factor": 1}, http.StatusCreated)
	assert.EqualValues(t, 1, conv["gaps_resolved"])
	assert.Equal(t, 8.0, r.onHand(t, sheetItem), "the waiting relief is posted once the conversion exists")

	// 7. Coverage: an unregistered code is counted and listed until a person
	// aliases it; the read returns the registry code.
	r.call(t, person, http.MethodPost, report, map[string]any{"material_code": "pla", "quantity": 3, "unit": "g"}, http.StatusCreated)
	assert.Equal(t, 2976.0, r.onHand(t, plaItem), "an unregistered code moves no stock")
	cov := r.call(t, person, http.MethodGet, "/v1/jobs/consumption/coverage", nil, http.StatusOK)
	assert.EqualValues(t, 3, cov["material_rows"])
	assert.EqualValues(t, 2, cov["with_registry_code"])
	assert.EqualValues(t, 1, cov["without_registry_code"])
	assert.Equal(t, "pla", cov["unresolved_codes"].([]any)[0].(map[string]any)["material_code"])
	unlinked := r.call(t, person, http.MethodGet, "/v1/inventory/materials/unlinked", nil, http.StatusOK)
	assert.Len(t, unlinked["consumption_codes"], 1)

	r.call(t, person, http.MethodPost, "/v1/inventory/materials/"+plaID+"/aliases", map[string]any{"alias": "pla"}, http.StatusCreated)
	assert.Equal(t, "alias_taken", errCode(r.call(t, person, http.MethodPost, "/v1/inventory/materials/"+sheetID+"/aliases",
		map[string]any{"alias": "pla"}, http.StatusConflict)))
	cov = r.call(t, person, http.MethodGet, "/v1/jobs/consumption/coverage", nil, http.StatusOK)
	assert.EqualValues(t, 0, cov["without_registry_code"], "the gate: every row carries a registry code")
	rows := r.call(t, person, http.MethodGet, "/v1/jobs/consumption?task_id="+task.String(), nil, http.StatusOK)["data"].([]any)
	for _, row := range rows {
		m := row.(map[string]any)
		if m["material_code"] == "pla" {
			assert.Equal(t, "FIL-PLA-BLK", m["registry_code"])
		}
	}

	// 8. Another tenant's material is invisible.
	other := uuid.New()
	_, err := r.admin.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Other', 'other-inventory')`, other)
	require.NoError(t, err)
	var otherMaterial string
	require.NoError(t, r.admin.QueryRow(`INSERT INTO material_registry (tenant_id, code, name, category, base_unit)
		VALUES ($1, 'FIL-OTHER', 'other tenant', 'filament', 'g') RETURNING id`, other).Scan(&otherMaterial))
	r.call(t, person, http.MethodGet, "/v1/inventory/materials/"+otherMaterial, nil, http.StatusNotFound)
	r.call(t, person, http.MethodPost, "/v1/inventory/receipts", map[string]any{"idempotency_key": "synthetic-4", "material_id": otherMaterial, "quantity": 1,
		"unit": "g", "invoice_status": "no_invoice"}, http.StatusNotFound)

	// Receipts are append-only.
	_, err = r.admin.Exec(`UPDATE inventory_receipts SET quantity = 5 WHERE id = $1`, spool["id"])
	assert.ErrorContains(t, err, "append-only")
	_, err = r.admin.Exec(`UPDATE material_registry SET code = 'X' WHERE id = $1`, plaID)
	assert.ErrorContains(t, err, "immutable")
}

func TestInventoryCountCloseIsIdempotent(t *testing.T) {
	r := newDispatchRig(t)
	person := r.userToken(t, "admin") // inventory writes need an inventory role
	plaID, plaItem := r.newStockedMaterial(t, person, "FIL-PLA-WHT", "filament", "g", 1000)
	resinID, resinItem := r.newStockedMaterial(t, person, "RES-STD-GRY", "resin", "ml", 500)
	loose := r.call(t, person, http.MethodPost, "/v1/inventory/materials",
		map[string]any{"code": "PKG-BOX-S", "name": "box (synthetic)", "category": "packaging", "base_unit": "pcs"}, http.StatusCreated)

	count := r.call(t, person, http.MethodPost, "/v1/inventory/counts", map[string]any{"is_opening": true}, http.StatusCreated)
	id := count["id"].(string)
	assert.Equal(t, "opening_count_exists", errCode(r.call(t, person, http.MethodPost, "/v1/inventory/counts",
		map[string]any{"is_opening": true}, http.StatusConflict)))

	countedAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	lines := "/v1/inventory/counts/" + id + "/lines"
	r.call(t, person, http.MethodPost, lines, map[string]any{"material_id": plaID, "counted_quantity": 0.9, "unit": "kg",
		"counted_at": countedAt}, http.StatusCreated)
	r.call(t, person, http.MethodPost, lines, map[string]any{"material_id": resinID, "counted_quantity": 2, "unit": "bottle"}, http.StatusCreated)
	r.call(t, person, http.MethodPost, lines, map[string]any{"material_id": loose["id"], "counted_quantity": 40, "unit": "pcs"}, http.StatusCreated)
	r.call(t, person, http.MethodPost, lines, map[string]any{"material_id": plaID, "counted_quantity": -1, "unit": "g"}, http.StatusBadRequest)

	// A movement after counted_at is not the count's business.
	r.call(t, person, http.MethodPost, "/v1/inventory/receipts", map[string]any{"idempotency_key": "synthetic-5", "material_id": plaID, "quantity": 50, "unit": "g",
		"invoice_status": "no_invoice"}, http.StatusCreated)
	assert.Equal(t, 1050.0, r.onHand(t, plaItem))

	closed := r.call(t, person, http.MethodPost, "/v1/inventory/counts/"+id+"/close", nil, http.StatusOK)
	assert.Equal(t, false, closed["already_closed"])
	results := map[string]map[string]any{}
	for _, l := range closed["count"].(map[string]any)["lines"].([]any) {
		m := l.(map[string]any)
		results[m["material_code"].(string)] = m
	}
	pla := results["FIL-PLA-WHT"]
	assert.Equal(t, "adjusted", pla["result"])
	assert.Equal(t, 1000.0, pla["expected_quantity"], "stock on hand at counted_at")
	assert.Equal(t, 900.0, pla["counted_stock_quantity"])
	assert.Equal(t, -100.0, pla["difference"])
	assert.Equal(t, 950.0, r.onHand(t, plaItem), "the difference is posted on today's stock")
	assert.Equal(t, "needs_conversion", results["RES-STD-GRY"]["result"])
	assert.Equal(t, 500.0, r.onHand(t, resinItem), "no conversion: stock untouched")
	assert.Equal(t, "not_stocked", results["PKG-BOX-S"]["result"])
	var refType, typ string
	require.NoError(t, r.admin.QueryRow(`SELECT reference_type, transaction_type::text FROM inventory_transactions WHERE id = $1`,
		pla["transaction_id"]).Scan(&refType, &typ))
	assert.Equal(t, "inventory_count", refType, "count differences are their own movement, never a job's")
	assert.Equal(t, "adjustment", typ)

	// Closing again moves nothing.
	movements := r.movementCount(t, "inventory_count")
	again := r.call(t, person, http.MethodPost, "/v1/inventory/counts/"+id+"/close", nil, http.StatusOK)
	assert.Equal(t, true, again["already_closed"])
	assert.Equal(t, movements, r.movementCount(t, "inventory_count"))
	assert.Equal(t, 950.0, r.onHand(t, plaItem))
	assert.Equal(t, "count_closed", errCode(r.call(t, person, http.MethodPost, lines,
		map[string]any{"material_id": plaID, "counted_quantity": 1, "unit": "g"}, http.StatusConflict)))

	// The count line waiting for a conversion is applied once it exists.
	conv := r.call(t, person, http.MethodPost, "/v1/inventory/materials/"+resinID+"/conversions",
		map[string]any{"from_unit": "bottle", "to_unit": "ml", "factor": 1000}, http.StatusCreated)
	assert.EqualValues(t, 1, conv["gaps_resolved"])
	assert.Equal(t, 2000.0, r.onHand(t, resinItem))
	again = r.call(t, person, http.MethodGet, "/v1/inventory/counts/"+id, nil, http.StatusOK)
	for _, l := range again["lines"].([]any) {
		if m := l.(map[string]any); m["material_code"] == "RES-STD-GRY" {
			assert.Equal(t, "adjusted", m["result"])
		}
	}
	list := r.call(t, person, http.MethodGet, "/v1/inventory/counts?status=closed", nil, http.StatusOK)
	assert.EqualValues(t, 1, list["total"])
}
