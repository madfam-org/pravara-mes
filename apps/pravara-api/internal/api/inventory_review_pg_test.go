package api

// Review round of RFC 0058 wave 1, part A: each test first reproduced a
// defect found in review (re-pointed codes relieved twice, an older count
// over-adjusted after a newer one, receipt retries were unbound, the cost
// event line disagreed with stock, an inverse conversion pair could be
// stored), then guards its fix. Opt-in with PRAVARA_TEST_DATABASE_URL.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/costevents"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
)

func (r *dispatchRig) unlinkedItem(t *testing.T, sku, unit string, onHand float64) string {
	t.Helper()
	var id string
	require.NoError(t, r.admin.QueryRow(`INSERT INTO inventory_items (tenant_id, sku, name, unit, quantity_on_hand)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, r.tenantID, sku, sku+" (synthetic, unlinked)", unit, onHand).Scan(&id))
	return id
}

// Finding 1: a code that names another stock item, or a material re-linked
// to another item, must never relieve a job twice.
func TestRepointedCodeNeverDoubleRelieves(t *testing.T) {
	r := newDispatchRig(t)
	admin := r.userToken(t, "admin")
	task := r.seedTask(t, boxSpecs)
	report := "/v1/tasks/" + task.String() + "/consumption"

	// (a) A code or alias equal to an unlinked item's SKU would silently
	// re-point that SKU's consumption: refused unless it links that item.
	unlinked := r.unlinkedItem(t, "PLA", "g", 1000)
	r.call(t, admin, http.MethodPost, report, map[string]any{"material_code": "PLA", "quantity": 50, "unit": "g"}, http.StatusCreated)
	assert.Equal(t, 950.0, r.onHand(t, unlinked))
	mID, xItem := r.newStockedMaterial(t, admin, "FIL-PLA-WHT", "filament", "g", 1000)
	assert.Equal(t, "alias_taken", errCode(r.call(t, admin, http.MethodPost, "/v1/inventory/materials/"+mID+"/aliases",
		map[string]any{"alias": "PLA"}, http.StatusConflict)))
	assert.Equal(t, "material_code_taken", errCode(r.call(t, admin, http.MethodPost, "/v1/inventory/materials",
		map[string]any{"code": "PLA", "name": "PLA", "category": "filament", "base_unit": "g"}, http.StatusConflict)))
	created := r.call(t, admin, http.MethodPost, "/v1/inventory/materials", map[string]any{"code": "PLA", "name": "PLA",
		"category": "filament", "base_unit": "g", "inventory_item_id": unlinked}, http.StatusCreated)
	assert.Equal(t, unlinked, created["inventory_item_id"], "registered and linked in one transaction")
	assert.Equal(t, 950.0, r.onHand(t, unlinked), "the same item: nothing moves")

	// (b) A material re-linked to another item: the job's relief moves, once.
	r.call(t, admin, http.MethodPost, report, map[string]any{"material_code": "FIL-PLA-WHT", "quantity": 30, "unit": "g"}, http.StatusCreated)
	assert.Equal(t, 970.0, r.onHand(t, xItem))
	r.call(t, admin, http.MethodPatch, "/v1/inventory/"+xItem, map[string]any{"material_id": ""}, http.StatusOK)
	yItem := r.call(t, admin, http.MethodPost, "/v1/inventory", map[string]any{"sku": "FIL-PLA-WHT-2", "name": "second spool",
		"unit": "g", "quantity_on_hand": 1000, "material_id": mID}, http.StatusCreated)["id"].(string)
	r.call(t, admin, http.MethodPost, "/v1/inventory/materials/"+mID+"/aliases", map[string]any{"alias": "pla-white"}, http.StatusCreated)
	r.call(t, admin, http.MethodPost, report, map[string]any{"material_code": "pla-white", "quantity": 40, "unit": "g"}, http.StatusCreated)
	assert.Equal(t, 1000.0, r.onHand(t, xItem), "the old item's relief is reversed")
	assert.Equal(t, 960.0, r.onHand(t, yItem), "the job relieves its effective 40 g once, from the item its code names now")
}

// Finding 3: closing an older count after a newer count's adjustment would
// fold the newer difference in twice.
func TestOlderCountCannotCloseAfterNewerCount(t *testing.T) {
	r := newDispatchRig(t)
	admin := r.userToken(t, "admin")
	mID, item := r.newStockedMaterial(t, admin, "RES-CLR", "resin", "ml", 1000)
	older := r.call(t, admin, http.MethodPost, "/v1/inventory/counts", nil, http.StatusCreated)["id"].(string)
	r.call(t, admin, http.MethodPost, "/v1/inventory/counts/"+older+"/lines", map[string]any{"material_id": mID,
		"counted_quantity": 900, "unit": "ml", "counted_at": time.Now().UTC().Add(-10 * time.Minute)}, http.StatusCreated)
	r.call(t, admin, http.MethodPost, "/v1/inventory/receipts", map[string]any{"material_id": mID, "quantity": 100, "unit": "ml",
		"invoice_status": "no_invoice", "idempotency_key": "synthetic-count-receipt"}, http.StatusCreated)

	newer := r.call(t, admin, http.MethodPost, "/v1/inventory/counts", nil, http.StatusCreated)["id"].(string)
	r.call(t, admin, http.MethodPost, "/v1/inventory/counts/"+newer+"/lines", map[string]any{"material_id": mID,
		"counted_quantity": 950, "unit": "ml"}, http.StatusCreated)
	closed := r.call(t, admin, http.MethodPost, "/v1/inventory/counts/"+newer+"/close", nil, http.StatusOK)
	assert.Equal(t, 950.0, r.onHand(t, item))
	line := closed["count"].(map[string]any)["lines"].([]any)[0].(map[string]any)
	assert.EqualValues(t, 0, line["movements_since_counted"])

	refused := r.call(t, admin, http.MethodPost, "/v1/inventory/counts/"+older+"/close", nil, http.StatusConflict)
	assert.Equal(t, "newer_count_exists", refused["error"])
	assert.Equal(t, 950.0, r.onHand(t, item), "the newer count stands")

	// Movements between counted_at and close are surfaced on the line.
	third := r.call(t, admin, http.MethodPost, "/v1/inventory/counts", nil, http.StatusCreated)["id"].(string)
	r.call(t, admin, http.MethodPost, "/v1/inventory/counts/"+third+"/lines", map[string]any{"material_id": mID,
		"counted_quantity": 950, "unit": "ml"}, http.StatusCreated)
	r.call(t, admin, http.MethodPost, "/v1/inventory/receipts", map[string]any{"material_id": mID, "quantity": 25, "unit": "ml",
		"invoice_status": "no_invoice", "idempotency_key": "synthetic-count-receipt-2"}, http.StatusCreated)
	closed = r.call(t, admin, http.MethodPost, "/v1/inventory/counts/"+third+"/close", nil, http.StatusOK)
	line = closed["count"].(map[string]any)["lines"].([]any)[0].(map[string]any)
	assert.EqualValues(t, 1, line["movements_since_counted"])
	assert.EqualValues(t, 25, line["quantity_since_counted"])
	assert.Equal(t, "matched", line["result"], "the receipt after counted_at is not the count's business")
}

// Finding 4: a receipt retry is bound to its payload, and the key is
// mandatory.
func TestReceiptIdempotencyKeyIsRequiredAndBound(t *testing.T) {
	r := newDispatchRig(t)
	admin := r.userToken(t, "admin")
	mID, item := r.newStockedMaterial(t, admin, "FIL-PETG-BLU", "filament", "g", 0)
	body := map[string]any{"material_id": mID, "quantity": 10, "unit": "g", "invoice_status": "no_invoice"}
	assert.Equal(t, "validation_error", errCode(r.call(t, admin, http.MethodPost, "/v1/inventory/receipts", body, http.StatusBadRequest)))
	body["idempotency_key"] = "synthetic-retry-1"
	r.call(t, admin, http.MethodPost, "/v1/inventory/receipts", body, http.StatusCreated)
	r.call(t, admin, http.MethodPost, "/v1/inventory/receipts", body, http.StatusOK) // same payload: replayed
	body["quantity"] = 11
	assert.Equal(t, "idempotency_key_reused", errCode(r.call(t, admin, http.MethodPost, "/v1/inventory/receipts", body, http.StatusConflict)))
	assert.Equal(t, 10.0, r.onHand(t, item))
}

// Finding 5: the cost event line is what stock settled per job and
// material; a line whose code later resolves elsewhere is cancelled.
func TestCostEventLinesMatchStockPerMaterial(t *testing.T) {
	r := newDispatchRig(t)
	admin := r.userToken(t, "admin")
	mID, item := r.newStockedMaterial(t, admin, "FIL-ABS-RED", "filament", "g", 1000)
	task := r.seedTask(t, boxSpecs)
	report := "/v1/tasks/" + task.String() + "/consumption"

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	emitter := costevents.NewEmitter(costevents.NewPGStore(db.NewTenantDB(r.app, log)), nil,
		func(ctx context.Context, fn func(ctx context.Context) error) error {
			return db.RunInTenantTx(ctx, r.app, r.tenantID.String(), fn)
		}, costevents.Settings{Tenant: r.tenantID, Options: costevents.Options{CostClass: "cogs"},
			Lookback: 24 * time.Hour, BatchSize: 100, DryRun: true}, log)
	sweep := func() {
		_, err := emitter.Sweep(context.Background())
		require.NoError(t, err)
	}
	type outboxRow struct{ code, status, quantity string }
	outbox := func() map[string]outboxRow {
		rows, err := r.admin.Query(`SELECT event_id, material_code, body->>'status', body->>'quantity' FROM cost_event_outbox`)
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		out := map[string]outboxRow{}
		for rows.Next() {
			var id string
			var o outboxRow
			require.NoError(t, rows.Scan(&id, &o.code, &o.status, &o.quantity))
			out[id] = o
		}
		return out
	}

	// An unregistered class code first: its own line.
	r.call(t, admin, http.MethodPost, report, map[string]any{"material_code": "abs", "quantity": 100, "unit": "g"}, http.StatusCreated)
	sweep()
	require.Len(t, outbox(), 1)
	classEvent := costevents.EventID("task", task, "abs")
	assert.Equal(t, "active", outbox()[classEvent].status)

	// The class is aliased to the material, then the registered code gets a
	// later actual: stock relieves the effective 90 g once, and the ledger
	// gets one event of 90 g for the material, the old line cancelled.
	r.call(t, admin, http.MethodPost, "/v1/inventory/materials/"+mID+"/aliases", map[string]any{"alias": "abs"}, http.StatusCreated)
	r.call(t, admin, http.MethodPost, report, map[string]any{"material_code": "FIL-ABS-RED", "quantity": 90, "unit": "g"}, http.StatusCreated)
	assert.Equal(t, 910.0, r.onHand(t, item))
	sweep()
	events := outbox()
	materialEvent := costevents.EventID("task", task, "FIL-ABS-RED")
	require.Contains(t, events, materialEvent)
	assert.Equal(t, outboxRow{"FIL-ABS-RED", "active", "90"}, events[materialEvent])
	assert.Equal(t, "cancelled", events[classEvent].status, "the class line now belongs to the material line")
	var active int
	for _, e := range events {
		if e.status == "active" {
			active++
		}
	}
	assert.Equal(t, 1, active, "one active event per job and material")
}

// An inverse conversion pair is one factor: the database refuses a second
// direction even when two requests race past the application check.
func TestInverseConversionPairIsOneFactor(t *testing.T) {
	r := newDispatchRig(t)
	admin := r.userToken(t, "admin")
	mID, _ := r.newStockedMaterial(t, admin, "SHT-MDF-3", "sheet", "pcs", 0)
	_, err := r.admin.Exec(`INSERT INTO material_unit_conversions (tenant_id, material_id, from_unit, to_unit, factor)
		VALUES ($1, $2, 'sheet', 'pcs', 1)`, r.tenantID, mID)
	require.NoError(t, err)
	_, err = r.admin.Exec(`INSERT INTO material_unit_conversions (tenant_id, material_id, from_unit, to_unit, factor)
		VALUES ($1, $2, 'pcs', 'sheet', 2)`, r.tenantID, mID)
	assert.Error(t, err, "an inconsistent inverse pair cannot be stored")
	_ = uuid.Nil
}

// Review pass 2, finding B: an earlier-counted count closed late posts its
// adjustment after a later count's counted_at. Closing the later count (or
// applying its line once a conversion arrives) must count that adjustment
// in its expected balance, or stock ends off by it. The opposite ordering
// (a later-counted count already closed) is TestOlderCountCannotCloseAfterNewerCount.
func TestCountIncludesAnEarlierCountsLateAdjustment(t *testing.T) {
	r := newDispatchRig(t)
	admin := r.userToken(t, "admin")
	plaID, pla := r.newStockedMaterial(t, admin, "FIL-PLA-LATE", "filament", "g", 1000)
	resinID, resin := r.newStockedMaterial(t, admin, "RES-LATE", "resin", "ml", 1000)
	t0 := time.Now().UTC().Add(-10 * time.Minute)
	t1 := time.Now().UTC().Add(-5 * time.Minute)

	earlier := r.call(t, admin, http.MethodPost, "/v1/inventory/counts", nil, http.StatusCreated)["id"].(string)
	later := r.call(t, admin, http.MethodPost, "/v1/inventory/counts", nil, http.StatusCreated)["id"].(string)
	for _, l := range []struct {
		count, material string
		qty             float64
		unit            string
		at              time.Time
	}{
		{earlier, plaID, 900, "g", t0}, {later, plaID, 880, "g", t1},
		{earlier, resinID, 900, "ml", t0}, {later, resinID, 0.88, "bottle", t1}, // no conversion yet
	} {
		r.call(t, admin, http.MethodPost, "/v1/inventory/counts/"+l.count+"/lines", map[string]any{"material_id": l.material,
			"counted_quantity": l.qty, "unit": l.unit, "counted_at": l.at}, http.StatusCreated)
	}
	r.call(t, admin, http.MethodPost, "/v1/inventory/counts/"+earlier+"/close", nil, http.StatusOK)
	assert.Equal(t, 900.0, r.onHand(t, pla))
	assert.Equal(t, 900.0, r.onHand(t, resin))

	closed := r.call(t, admin, http.MethodPost, "/v1/inventory/counts/"+later+"/close", nil, http.StatusOK)
	assert.Equal(t, 880.0, r.onHand(t, pla), "the later count is the truth: 880, not 780")
	for _, l := range closed["count"].(map[string]any)["lines"].([]any) {
		if m := l.(map[string]any); m["material_code"] == "FIL-PLA-LATE" {
			assert.EqualValues(t, 900, m["expected_quantity"], "the earlier count's late adjustment is expected")
			assert.EqualValues(t, -20, m["difference"])
		}
	}

	// The same rule when the later line is applied by a conversion.
	r.call(t, admin, http.MethodPost, "/v1/inventory/materials/"+resinID+"/conversions",
		map[string]any{"from_unit": "bottle", "to_unit": "ml", "factor": 1000}, http.StatusCreated)
	assert.Equal(t, 880.0, r.onHand(t, resin), "resolve line follows the same rule as close")
}
