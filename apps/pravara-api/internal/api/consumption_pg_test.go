package api

// Job consumption on PostgreSQL, with the API connected as pravara_app
// (NOSUPERUSER NOBYPASSRLS): dispatch completion records estimates and
// machine time, relieves stock and links the lot to the genealogy; operator
// actuals settle the difference; GET /v1/jobs/consumption serves it with the
// order's engagement. Opt-in with PRAVARA_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

const (
	tpuLot = "LOT-TPU-95A-0042" // the lot seedMachine loads for class tpu-95a
	tpuSKU = "FIL-TPU95A-BLK"
)

type stock struct {
	onHand, available float64
}

func (r *dispatchRig) stock(t *testing.T, item uuid.UUID) stock {
	t.Helper()
	var s stock
	require.NoError(t, r.admin.QueryRow(`SELECT quantity_on_hand, quantity_available FROM inventory_items WHERE id = $1`, item).
		Scan(&s.onHand, &s.available))
	return s
}

type movement struct {
	typ             string
	quantity, after float64
	refID           uuid.UUID
}

func (r *dispatchRig) movements(t *testing.T, item uuid.UUID) []movement {
	t.Helper()
	rows, err := r.admin.Query(`SELECT transaction_type::text, quantity, running_balance, reference_id FROM inventory_transactions
		WHERE inventory_item_id = $1 AND reference_type = 'job_consumption' ORDER BY created_at, id`, item)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []movement
	for rows.Next() {
		var m movement
		require.NoError(t, rows.Scan(&m.typ, &m.quantity, &m.after, &m.refID))
		out = append(out, m)
	}
	return out
}

type consumptionRow struct {
	ID           uuid.UUID
	Kind, Basis  string
	Source, Unit string
	Code, Lot    string
	Quantity     float64
}

func (r *dispatchRig) consumption(t *testing.T, jobID uuid.UUID) []consumptionRow {
	t.Helper()
	rows, err := r.admin.Query(`SELECT id, kind, basis, source, unit, COALESCE(material_code, ''), COALESCE(material_lot, ''), quantity
		FROM job_consumption WHERE job_id = $1
		ORDER BY CASE kind WHEN 'material' THEN 0 ELSE 1 END, basis, source`, jobID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []consumptionRow
	for rows.Next() {
		var c consumptionRow
		require.NoError(t, rows.Scan(&c.ID, &c.Kind, &c.Basis, &c.Source, &c.Unit, &c.Code, &c.Lot, &c.Quantity))
		out = append(out, c)
	}
	return out
}

func (r *dispatchRig) completeDispatch(t *testing.T, task, machine uuid.UUID, ackedAt string) uuid.UUID {
	t.Helper()
	id := r.create(t, task)
	d := r.runUntil(t, id, repositories.DispatchCommandEnqueued)
	_, err := r.admin.Exec(`UPDATE task_commands SET status = 'acknowledged', acked_at = $2 WHERE command_id = $1`, *d.CommandID, ackedAt)
	require.NoError(t, err)
	r.completeJob(t, *d.CommandID, machine, task)
	r.due(t)
	r.runUntil(t, id, repositories.DispatchCompleted)
	return id
}

func TestJobConsumptionDispatchOperatorAndRead(t *testing.T) {
	r := newDispatchRig(t)
	voron := r.seedMachine(t, "voron-01", "idle", "tpu-95a")
	task := r.seedTask(t, boxSpecs)
	var orderID uuid.UUID
	require.NoError(t, r.admin.QueryRow(`SELECT order_id FROM tasks WHERE id = $1`, task).Scan(&orderID))

	// The lot names its stock SKU; the SKU is stocked in grams.
	var lotID, item uuid.UUID
	require.NoError(t, r.admin.QueryRow(`INSERT INTO batch_lots (tenant_id, lot_number, product_name, product_code, quantity, unit)
		VALUES ($1, $2, 'TPU 95A black', $3, 1000, 'g') RETURNING id`, r.tenantID, tpuLot, tpuSKU).Scan(&lotID))
	require.NoError(t, r.admin.QueryRow(`INSERT INTO inventory_items (tenant_id, sku, name, unit, quantity_on_hand)
		VALUES ($1, $2, 'TPU 95A black 1 kg', 'g', 1000) RETURNING id`, r.tenantID, tpuSKU).Scan(&item))

	// 1. Completion: the printer reports at 10:00:00.25; start_job was
	// acknowledged at 09:43.
	dispatchID := r.completeDispatch(t, task, voron, "2026-10-04T09:43:00Z")
	facts := r.consumption(t, dispatchID)
	require.Len(t, facts, 3, "%+v", facts)
	est := facts[0]
	assert.Equal(t, consumptionRow{ID: est.ID, Kind: "material", Basis: "estimate", Source: "slicer", Unit: "g",
		Code: tpuSKU, Lot: tpuLot, Quantity: 5.2}, est, "fabrication-prep filament_g, named by the lot's SKU")
	assert.Equal(t, consumptionRow{ID: facts[1].ID, Kind: "machine_time", Basis: "actual", Source: "printer", Unit: "min",
		Quantity: 17.0042}, facts[1], "acknowledged 09:43:00 → printer reported 10:00:00.25")
	assert.Equal(t, consumptionRow{ID: facts[2].ID, Kind: "machine_time", Basis: "estimate", Source: "slicer", Unit: "min",
		Quantity: 16.25}, facts[2], "fabrication-prep print_time_s 975")

	var occurred time.Time
	var linkedTask, linkedOrder, linkedMachine, linkedDispatch uuid.UUID
	require.NoError(t, r.admin.QueryRow(`SELECT occurred_at, task_id, order_id, machine_id, dispatch_id FROM job_consumption WHERE id = $1`, est.ID).
		Scan(&occurred, &linkedTask, &linkedOrder, &linkedMachine, &linkedDispatch))
	assert.True(t, occurred.Equal(time.Date(2026, 10, 4, 10, 0, 0, 250e6, time.UTC)), occurred)
	assert.Equal(t, []uuid.UUID{task, orderID, voron, dispatchID}, []uuid.UUID{linkedTask, linkedOrder, linkedMachine, linkedDispatch})

	// Stock relieved by the estimate, with the running balance.
	assert.Equal(t, stock{994.8, 994.8}, r.stock(t, item))
	assert.Equal(t, []movement{{"consumption", -5.2, 994.8, est.ID}}, r.movements(t, item))

	// The lot is on the dispatch's genealogy record, once.
	var genealogyID uuid.UUID
	require.NoError(t, r.admin.QueryRow(`SELECT genealogy_id FROM manufacturing_records WHERE dispatch_id = $1`, dispatchID).Scan(&genealogyID))
	var gLot uuid.UUID
	var gName, gCode, gUnit string
	var gQty float64
	require.NoError(t, r.admin.QueryRow(`SELECT batch_lot_id, material_name, material_code, quantity_consumed, unit
		FROM genealogy_material_consumption WHERE genealogy_id = $1`, genealogyID).Scan(&gLot, &gName, &gCode, &gQty, &gUnit))
	assert.Equal(t, lotID, gLot)
	assert.Equal(t, "TPU 95A black 1 kg", gName)
	assert.Equal(t, tpuSKU, gCode)
	assert.Equal(t, 5.2, gQty)
	assert.Equal(t, "g", gUnit)

	// 2. A replay records nothing new and relieves nothing more.
	job := services.JobRef{TenantID: r.tenantID, Kind: repositories.JobKindDispatch, ID: dispatchID,
		TaskID: &task, OrderID: &orderID, MachineID: &voron, GenealogyID: &genealogyID}
	require.NoError(t, db.RunInTenantTx(context.Background(), r.app, r.tenantID.String(), func(ctx context.Context) error {
		res, err := r.svc.Consumption.Record(ctx, job, []repositories.JobConsumption{{Kind: "material", MaterialCode: tpuSKU,
			MaterialLot: tpuLot, Quantity: 5.2, Unit: "g", Basis: "estimate", Source: "slicer", OccurredAt: occurred}})
		require.NoError(t, err)
		assert.Equal(t, []bool{false}, res.Created)
		assert.Empty(t, res.Movements)
		return nil
	}))
	assert.Equal(t, stock{994.8, 994.8}, r.stock(t, item))
	assert.Len(t, r.consumption(t, dispatchID), 3)

	// 3. Operator actual: people only; it settles the dispatch's estimate.
	person := r.userToken(t, "operator")
	report := "/v1/tasks/" + task.String() + "/consumption"
	assert.Equal(t, http.StatusForbidden, r.do(http.MethodPost, report, r.machineToken(t, "pravara-mes:jobs", nil),
		map[string]any{"material_code": tpuSKU, "quantity": 6, "unit": "g"}).Code, "machine callers cannot move stock")

	w := r.do(http.MethodPost, report, person, map[string]any{"material_code": tpuSKU, "quantity": 6, "unit": "g"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var reported struct {
		Consumption    repositories.JobConsumption         `json:"consumption"`
		StockMovements []repositories.InventoryTransaction `json:"stock_movements"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reported))
	assert.Equal(t, "dispatch", reported.Consumption.JobKind)
	assert.Equal(t, dispatchID, reported.Consumption.JobID)
	assert.Equal(t, "actual", reported.Consumption.Basis)
	assert.Equal(t, "operator", reported.Consumption.Source)
	assert.Equal(t, tpuLot, reported.Consumption.MaterialLot, "the lot is inherited from the dispatch")
	require.Len(t, reported.StockMovements, 1)
	assert.Equal(t, "adjustment", reported.StockMovements[0].TransactionType)
	assert.InDelta(t, -0.8, reported.StockMovements[0].Quantity, 1e-9)
	assert.Equal(t, stock{994, 994}, r.stock(t, item))
	assert.Len(t, r.movements(t, item), 2)

	w = r.do(http.MethodPost, report, person, map[string]any{"material_code": tpuSKU, "quantity": 7, "unit": "g"})
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "already_recorded")
	assert.Equal(t, stock{994, 994}, r.stock(t, item), "a refused report moves nothing")

	w = r.do(http.MethodPost, report, person, map[string]any{"quantity": 18, "unit": "min"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"stock_movements":[]`)

	for _, bad := range []map[string]any{
		{"material_code": tpuSKU, "quantity": 1, "unit": "kg"},
		{"material_code": tpuSKU, "quantity": -1, "unit": "g"},
		{"quantity": 1, "unit": "g"},
		{"material_code": tpuSKU, "quantity": 1, "unit": "min"},
		{"material_code": tpuSKU, "unit": "g"},
	} {
		w = r.do(http.MethodPost, report, person, bad)
		assert.Equal(t, http.StatusBadRequest, w.Code, "%v: %s", bad, w.Body.String())
	}
	assert.Equal(t, http.StatusNotFound, r.do(http.MethodPost, "/v1/tasks/"+uuid.NewString()+"/consumption", person,
		map[string]any{"material_code": tpuSKU, "quantity": 1, "unit": "g"}).Code)

	var gRows int
	require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM genealogy_material_consumption WHERE genealogy_id = $1`, genealogyID).Scan(&gRows))
	assert.Equal(t, 1, gRows, "the genealogy lists the material once")

	// 4. Engagement on the order, then the read.
	w = r.do(http.MethodPatch, "/v1/orders/"+orderID.String(), person, map[string]any{"engagement_slug": "Not Valid"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w = r.do(http.MethodPatch, "/v1/orders/"+orderID.String(), person, map[string]any{"engagement_slug": "acme-fab-2026"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"engagement_slug":"acme-fab-2026"`)

	type page struct {
		Data  []map[string]any `json:"data"`
		Total int              `json:"total"`
	}
	read := func(token, query string) page {
		t.Helper()
		w := r.do(http.MethodGet, "/v1/jobs/consumption"+query, token, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var p page
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
		return p
	}
	ledger := r.machineToken(t, "pravara-mes:jobs", nil)
	window := read(ledger, "?from=2026-10-01T00:00:00Z&to=2026-10-05T00:00:00Z")
	assert.Equal(t, 3, window.Total, "operator reports happened now, outside the window")
	for _, row := range window.Data {
		assert.Equal(t, "acme-fab-2026", row["engagement_slug"])
		assert.Equal(t, orderID.String(), row["order_id"])
		assert.Equal(t, task.String(), row["task_id"])
		for _, key := range []string{"quantity", "unit", "basis", "source", "kind", "occurred_at", "job_kind", "job_id"} {
			assert.Contains(t, row, key)
		}
		for key := range row {
			assert.NotContains(t, []string{"cost", "unit_cost", "amount", "price", "currency"}, key, "quantities only")
		}
	}
	assert.Equal(t, 5, read(ledger, "").Total)
	assert.Equal(t, 5, read(r.machineToken(t, "pravara-mes:read", nil), "?engagement_slug=acme-fab-2026").Total)
	assert.Equal(t, 0, read(person, "?engagement_slug=other-engagement").Total)
	assert.Equal(t, 5, read(person, "?task_id="+task.String()).Total)
	assert.Equal(t, 2, len(read(person, "?limit=2").Data))
	assert.Equal(t, http.StatusForbidden, r.do(http.MethodGet, "/v1/jobs/consumption", r.machineToken(t, "pravara-mes:nodes", nil), nil).Code)
	for _, q := range []string{"?from=yesterday", "?to=2026-13-01", "?task_id=nope", "?engagement_slug=Bad", "?limit=0", "?limit=1001", "?offset=-1"} {
		assert.Equal(t, http.StatusBadRequest, r.do(http.MethodGet, "/v1/jobs/consumption"+q, person, nil).Code, q)
	}

	// 5. Another tenant's consumption is invisible; facts are append-only;
	// the slug format holds in the database too.
	other := uuid.New()
	_, err := r.admin.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Other', 'other-consumption')`, other)
	require.NoError(t, err)
	_, err = r.admin.Exec(`INSERT INTO job_consumption (tenant_id, job_kind, job_id, kind, material_code, quantity, unit, basis, source, occurred_at)
		VALUES ($1, 'task', $2, 'material', $3, 1, 'g', 'actual', 'operator', '2026-10-04T10:00:00Z')`, other, uuid.New(), tpuSKU)
	require.NoError(t, err)
	assert.Equal(t, 5, read(ledger, "").Total)

	_, err = r.admin.Exec(`UPDATE job_consumption SET quantity = 1 WHERE id = $1`, est.ID)
	assert.ErrorContains(t, err, "append-only")
	_, err = r.admin.Exec(`UPDATE job_consumption SET order_id = NULL WHERE id = $1`, est.ID)
	assert.NoError(t, err, "ON DELETE SET NULL links may still clear")
	_, err = r.admin.Exec(`UPDATE orders SET engagement_slug = 'Bad Slug' WHERE id = $1`, orderID)
	assert.ErrorContains(t, err, "orders_engagement_slug_format")

	// 6. Order create carries the engagement.
	w = r.do(http.MethodPost, "/v1/orders", person, map[string]any{"customer_name": "Engagement test", "engagement_slug": "acme-fab-2026"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created struct {
		ID uuid.UUID `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	w = r.do(http.MethodGet, "/v1/orders/"+created.ID.String(), ledger, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"engagement_slug":"acme-fab-2026"`)
	assert.Equal(t, http.StatusBadRequest, r.do(http.MethodPost, "/v1/orders", person,
		map[string]any{"customer_name": "x", "engagement_slug": "eng:acme"}).Code)
}

func TestJobConsumptionPrintJob(t *testing.T) {
	r := newDispatchRig(t)
	task := r.seedTask(t, boxSpecs)
	var withDensity, withoutDensity, item uuid.UUID
	require.NoError(t, r.admin.QueryRow(`INSERT INTO material_profiles (tenant_id, name, material_type, diameter, density)
		VALUES ($1, 'PLA black', 'PLA', 1.75, 1.24) RETURNING id`, r.tenantID).Scan(&withDensity))
	require.NoError(t, r.admin.QueryRow(`INSERT INTO material_profiles (tenant_id, name, material_type, diameter)
		VALUES ($1, 'PETG clear', 'PETG', 1.75) RETURNING id`, r.tenantID).Scan(&withoutDensity))
	_, err := r.admin.Exec(`UPDATE material_profiles SET density = NULL WHERE id = $1`, withoutDensity)
	require.NoError(t, err)
	require.NoError(t, r.admin.QueryRow(`INSERT INTO inventory_items (tenant_id, sku, name, unit, quantity_on_hand)
		VALUES ($1, 'PLA', 'PLA black 1 kg', 'g', 500) RETURNING id`, r.tenantID).Scan(&item))
	_, err = r.admin.Exec(`INSERT INTO inventory_items (tenant_id, sku, name, unit, quantity_on_hand)
		VALUES ($1, 'PETG', 'PETG clear 1 kg', 'g', 500)`, r.tenantID)
	require.NoError(t, err)

	printJob := func(material uuid.UUID, status string) uuid.UUID {
		var id uuid.UUID
		require.NoError(t, r.admin.QueryRow(`INSERT INTO print_jobs (tenant_id, material_id, job_name, status, task_id,
			estimated_filament_g, actual_filament_mm, actual_time_seconds, started_at, completed_at)
			VALUES ($1, $2, 'bracket', $3, $4, 12.5, 4000, 3900, '2026-11-05T10:00:00Z', '2026-11-05T11:05:00Z') RETURNING id`,
			r.tenantID, material, status, task).Scan(&id))
		return id
	}
	record := func(id uuid.UUID) (*services.ConsumptionResult, error) {
		var res *services.ConsumptionResult
		err := db.RunInTenantTx(context.Background(), r.app, r.tenantID.String(), func(ctx context.Context) error {
			var err error
			res, err = r.svc.Consumption.RecordPrintJob(ctx, r.tenantID, id)
			return err
		})
		return res, err
	}

	pla := printJob(withDensity, "completed")
	res, err := record(pla)
	require.NoError(t, err)
	assert.Equal(t, []bool{true, true, true}, res.Created)
	grams, _ := services.FilamentGrams(4000, ptr(1.75), ptr(1.24))
	rows := r.consumption(t, pla)
	require.Len(t, rows, 3, "%+v", rows)
	assert.Equal(t, "actual", rows[0].Basis)
	assert.Equal(t, "printer", rows[0].Source)
	assert.Equal(t, "g", rows[0].Unit)
	assert.InDelta(t, repositories.Round4(grams), rows[0].Quantity, 1e-9, "4000 mm of 1.75 mm at 1.24 g/cm³")
	assert.Equal(t, consumptionRow{ID: rows[1].ID, Kind: "material", Basis: "estimate", Source: "slicer", Unit: "g", Code: "PLA", Quantity: 12.5}, rows[1])
	assert.Equal(t, consumptionRow{ID: rows[2].ID, Kind: "machine_time", Basis: "actual", Source: "printer", Unit: "min", Quantity: 65}, rows[2])
	moves := r.movements(t, item)
	require.Len(t, moves, 1, "one relief, at the actual")
	assert.Equal(t, "consumption", moves[0].typ)
	assert.InDelta(t, -repositories.Round4(grams), moves[0].quantity, 1e-9)
	assert.Equal(t, rows[0].ID, moves[0].refID)

	res, err = record(pla)
	require.NoError(t, err)
	assert.Equal(t, []bool{false, false, false}, res.Created, "idempotent")
	assert.Len(t, r.movements(t, item), 1)

	petg := printJob(withoutDensity, "completed")
	_, err = record(petg)
	require.NoError(t, err)
	rows = r.consumption(t, petg)
	require.Len(t, rows, 3)
	var note string
	require.NoError(t, r.admin.QueryRow(`SELECT note FROM job_consumption WHERE job_id = $1 AND basis = 'actual' AND kind = 'material'`, petg).Scan(&note))
	assert.Contains(t, note, "kept in mm")
	assert.Equal(t, "mm", rows[0].Unit)
	assert.Equal(t, 4000.0, rows[0].Quantity)
	var petgMoves int
	require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM inventory_transactions t JOIN inventory_items i ON i.id = t.inventory_item_id
		WHERE i.sku = 'PETG'`).Scan(&petgMoves))
	assert.Zero(t, petgMoves, "the mm actual is effective and has no conversion to g: nothing is relieved by guessing")
	var gapUnit string
	require.NoError(t, r.admin.QueryRow(`SELECT from_unit FROM inventory_conversion_gaps
		WHERE job_id = $1 AND status = 'open'`, petg).Scan(&gapUnit))
	assert.Equal(t, "mm", gapUnit, "the job waits for a conversion, listed as a gap")

	_, err = record(printJob(withDensity, "printing"))
	assert.ErrorIs(t, err, services.ErrConsumptionPrintJobOpen)
	_, err = record(uuid.New())
	assert.ErrorIs(t, err, services.ErrConsumptionPrintJobNotFound)
}

func ptr(v float64) *float64 { return &v }

// Consumption runs in a savepoint: when it fails, the manufacturing record,
// genealogy and passport outbox are still written and the dispatch completes.
func TestJobConsumptionFailureNeverBlocksCompletion(t *testing.T) {
	r := newDispatchRig(t)
	voron := r.seedMachine(t, "voron-01", "idle", "tpu-95a")
	task := r.seedTask(t, boxSpecs)
	_, err := r.admin.Exec(`REVOKE INSERT ON job_consumption FROM pravara_app`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = r.admin.Exec(`GRANT INSERT ON job_consumption TO pravara_app`) })

	dispatchID := r.completeDispatch(t, task, voron, "2026-10-04T09:43:00Z")
	var records, outbox int
	require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM manufacturing_records WHERE dispatch_id = $1`, dispatchID).Scan(&records))
	require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM passport_outbox`).Scan(&outbox))
	assert.Equal(t, 1, records)
	assert.Equal(t, 2, outbox)
	assert.Empty(t, r.consumption(t, dispatchID))
}
