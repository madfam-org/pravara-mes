package api

// RFC 0058 wave 1 part B on PostgreSQL, as pravara_app: operator
// corrections are append-only and move stock by the difference; disposition
// (good, scrap, failed_print) is recorded and never sent to the cost
// ledger; an operator marking a print job finished records its actual
// consumption without reaching any machine. Opt-in with
// PRAVARA_TEST_DATABASE_URL.

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

func TestConsumptionCorrectionIsAppendOnly(t *testing.T) {
	r := newDispatchRig(t)
	person := r.userToken(t, "operator") // shop-floor writes stay open to any signed-in user
	_, item := r.newStockedMaterial(t, r.userToken(t, "admin"), "FIL-PLA-BLK", "filament", "g", 1000)
	task := r.seedTask(t, boxSpecs)
	report := "/v1/tasks/" + task.String() + "/consumption"

	first := r.call(t, person, http.MethodPost, report, map[string]any{"material_code": "FIL-PLA-BLK", "quantity": 25, "unit": "g"},
		http.StatusCreated)["consumption"].(map[string]any)
	assert.Equal(t, "good", first["disposition"], "default disposition")
	assert.Equal(t, 975.0, r.onHand(t, item))
	firstID := first["id"].(string)

	// A second report is still refused, and now names the correction route.
	conflict := r.call(t, person, http.MethodPost, report, map[string]any{"material_code": "FIL-PLA-BLK", "quantity": 30, "unit": "g"},
		http.StatusConflict)
	assert.Equal(t, "/v1/jobs/consumption/"+firstID+"/corrections", conflict["correct_with"])

	corrections := "/v1/jobs/consumption/" + firstID + "/corrections"
	assert.Equal(t, http.StatusForbidden, r.do(http.MethodPost, corrections, r.machineToken(t, "pravara-mes:jobs", nil),
		map[string]any{"quantity": 30}).Code, "corrections move stock: people only")
	r.call(t, person, http.MethodPost, corrections, map[string]any{"quantity": 30, "disposition": "lost"}, http.StatusBadRequest)
	r.call(t, person, http.MethodPost, "/v1/jobs/consumption/"+uuid.NewString()+"/corrections", map[string]any{"quantity": 1},
		http.StatusNotFound)

	// The correction is a new row; the stock moves by the difference.
	out := r.call(t, person, http.MethodPost, corrections,
		map[string]any{"quantity": 30, "disposition": "scrap", "reason": "support material weighed"}, http.StatusCreated)
	corr := out["consumption"].(map[string]any)
	assert.Equal(t, firstID, corr["supersedes_id"])
	assert.Equal(t, "scrap", corr["disposition"])
	assert.Equal(t, "operator", corr["source"])
	moves := out["stock_movements"].([]any)
	require.Len(t, moves, 1)
	assert.Equal(t, "adjustment", moves[0].(map[string]any)["transaction_type"])
	assert.Equal(t, -5.0, moves[0].(map[string]any)["quantity"])
	assert.Equal(t, 970.0, r.onHand(t, item))
	var stored float64
	require.NoError(t, r.admin.QueryRow(`SELECT quantity FROM job_consumption WHERE id = $1`, firstID).Scan(&stored))
	assert.Equal(t, 25.0, stored, "history is never updated")

	// A row is corrected once; correct the latest correction instead.
	again := r.call(t, person, http.MethodPost, corrections, map[string]any{"quantity": 20}, http.StatusConflict)
	assert.Equal(t, "already_superseded", again["error"])
	assert.Equal(t, corr["id"], again["superseded_by"])
	out = r.call(t, person, http.MethodPost, "/v1/jobs/consumption/"+corr["id"].(string)+"/corrections",
		map[string]any{"quantity": 20}, http.StatusCreated)
	assert.Equal(t, "scrap", out["consumption"].(map[string]any)["disposition"], "disposition carries over unless changed")
	assert.Equal(t, 980.0, r.onHand(t, item))

	rows := r.call(t, person, http.MethodGet, "/v1/jobs/consumption?task_id="+task.String(), nil, http.StatusOK)["data"].([]any)
	require.Len(t, rows, 3)
	byID := map[string]map[string]any{}
	for _, row := range rows {
		m := row.(map[string]any)
		byID[m["id"].(string)] = m
	}
	assert.Equal(t, corr["id"], byID[firstID]["superseded_by"])

	_, err := r.admin.Exec(`UPDATE job_consumption_corrections SET disposition = 'good' WHERE id = $1`, corr["id"])
	assert.ErrorContains(t, err, "append-only")
	_, err = r.admin.Exec(`UPDATE job_consumption SET disposition = 'scrap' WHERE id = $1`, firstID)
	assert.ErrorContains(t, err, "append-only")
	var facts int
	require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM job_consumption WHERE task_id = $1`, task).Scan(&facts))
	assert.Equal(t, 1, facts, "corrections live in job_consumption_corrections; the 033 fact key is untouched")

	// The cost event line counts only rows no correction superseded, and
	// carries no disposition (Dhanam's contract has no such field).
	log := logrus.New()
	store := costevents.NewPGStore(db.NewTenantDB(r.app, log))
	require.NoError(t, db.RunInTenantTx(context.Background(), r.app, r.tenantID.String(), func(ctx context.Context) error {
		lines, err := store.RecentLines(ctx, firstCreated(t, r, firstID))
		require.NoError(t, err)
		require.Len(t, lines, 1)
		require.Len(t, lines[0].Facts, 1, "the superseded rows are not facts of the line")
		b := costevents.Build(lines[0], nil, costevents.Options{CostClass: "cogs"})
		require.NotNil(t, b.Event)
		assert.Equal(t, "20", b.Event.Quantity)
		raw, _, err := costevents.Body(*b.Event)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), "disposition")
		assert.NotContains(t, string(raw), "scrap")
		return nil
	}))
}

func firstCreated(t *testing.T, r *dispatchRig, id string) (at time.Time) {
	t.Helper()
	require.NoError(t, r.admin.QueryRow(`SELECT created_at FROM job_consumption WHERE id = $1`, id).Scan(&at))
	return at
}

func TestPrintJobCompletionRecordsActuals(t *testing.T) {
	r := newDispatchRig(t)
	person := r.userToken(t, "operator") // shop-floor writes stay open to any signed-in user
	admin := r.userToken(t, "admin")
	_, item := r.newStockedMaterial(t, admin, "FIL-PLA-GRY", "filament", "g", 500)
	task := r.seedTask(t, boxSpecs)
	var profile uuid.UUID
	require.NoError(t, r.admin.QueryRow(`INSERT INTO material_profiles (tenant_id, name, material_type, diameter, density)
		VALUES ($1, 'PLA grey', 'PLA', 1.75, 1.24) RETURNING id`, r.tenantID).Scan(&profile))
	newPrintJob := func(status string) string {
		var id string
		require.NoError(t, r.admin.QueryRow(`INSERT INTO print_jobs (tenant_id, material_id, job_name, status, task_id,
			estimated_filament_g, estimated_time_seconds, started_at)
			VALUES ($1, $2, 'bracket', $3, $4, 12.5, 3600, NOW() - interval '1 hour') RETURNING id`,
			r.tenantID, profile, status, task).Scan(&id))
		return id
	}

	// The profile's material type is a class, not a registered code: the
	// estimate is recorded, but relieves no stock until a person aliases it.
	pj := newPrintJob("printing")
	out := r.call(t, person, http.MethodPost, "/v1/print-jobs/"+pj+"/complete", nil, http.StatusOK)
	assert.Equal(t, true, out["marked_completed"])
	assert.Len(t, out["consumption"], 3, "slicer material estimate, slicer minutes, minutes started→completed")
	assert.Equal(t, 500.0, r.onHand(t, item))
	var status string
	require.NoError(t, r.admin.QueryRow(`SELECT status FROM print_jobs WHERE id = $1`, pj).Scan(&status))
	assert.Equal(t, "completed", status)

	// Again: nothing new.
	out = r.call(t, person, http.MethodPost, "/v1/print-jobs/"+pj+"/complete", nil, http.StatusOK)
	assert.Equal(t, false, out["marked_completed"])
	assert.Equal(t, []any{false, false, false}, out["created"])

	// With the class aliased and the operator's weighed actual, a failed
	// print: the actual is recorded with its disposition and relieves stock.
	plaID := r.call(t, person, http.MethodGet, "/v1/inventory/materials?search=FIL-PLA-GRY", nil, http.StatusOK)["data"].([]any)[0].(map[string]any)["id"].(string)
	r.call(t, admin, http.MethodPost, "/v1/inventory/materials/"+plaID+"/aliases", map[string]any{"alias": "PLA"}, http.StatusCreated)
	pj2 := newPrintJob("printing")
	out = r.call(t, person, http.MethodPost, "/v1/print-jobs/"+pj2+"/complete",
		map[string]any{"material_quantity": 9, "material_unit": "g", "disposition": "failed_print"}, http.StatusOK)
	assert.Equal(t, true, out["material_recorded"])
	assert.Equal(t, 491.0, r.onHand(t, item), "the operator actual (9 g) is effective over the 12.5 g estimate")
	var dispositions []string
	rows, err := r.admin.Query(`SELECT DISTINCT disposition FROM job_consumption WHERE job_id = $1 AND kind = 'material'`, pj2)
	require.NoError(t, err)
	for rows.Next() {
		var d string
		require.NoError(t, rows.Scan(&d))
		dispositions = append(dispositions, d)
	}
	require.NoError(t, rows.Close())
	assert.Equal(t, []string{"failed_print"}, dispositions)

	// Refusals: unknown, cancelled, bad disposition, material without code.
	r.call(t, person, http.MethodPost, "/v1/print-jobs/"+uuid.NewString()+"/complete", nil, http.StatusNotFound)
	assert.Equal(t, "print_job_closed", errCode(r.call(t, person, http.MethodPost,
		"/v1/print-jobs/"+newPrintJob("cancelled")+"/complete", nil, http.StatusConflict)))
	r.call(t, person, http.MethodPost, "/v1/print-jobs/"+newPrintJob("printing")+"/complete",
		map[string]any{"disposition": "lost"}, http.StatusBadRequest)
	assert.Equal(t, http.StatusForbidden, r.do(http.MethodPost, "/v1/print-jobs/"+pj+"/complete",
		r.machineToken(t, "pravara-mes:jobs", nil), nil).Code, "people only")
}

// Finding 6: only the job's effective row may be corrected; a correction of
// an estimate or an older actual would override a later real actual.
// Also: a queued print job cannot be completed, and an already completed
// one does not silently ignore a different report.
func TestCorrectionTargetsTheEffectiveRowAndCompletionRefusals(t *testing.T) {
	r := newDispatchRig(t)
	admin := r.userToken(t, "admin")
	operator := r.userToken(t, "operator")
	mID, item := r.newStockedMaterial(t, admin, "FIL-PLA-ORG", "filament", "g", 500)
	r.call(t, admin, http.MethodPost, "/v1/inventory/materials/"+mID+"/aliases", map[string]any{"alias": "PLA"}, http.StatusCreated)
	task := r.seedTask(t, boxSpecs)
	var profile uuid.UUID
	require.NoError(t, r.admin.QueryRow(`INSERT INTO material_profiles (tenant_id, name, material_type, diameter, density)
		VALUES ($1, 'PLA orange', 'PLA', 1.75, 1.24) RETURNING id`, r.tenantID).Scan(&profile))
	newPrintJob := func(status string, actualMM any) string {
		var id string
		require.NoError(t, r.admin.QueryRow(`INSERT INTO print_jobs (tenant_id, material_id, job_name, status, task_id,
			estimated_filament_g, actual_filament_mm, started_at)
			VALUES ($1, $2, 'bracket', $3, $4, 12.5, $5, NOW() - interval '1 hour') RETURNING id`,
			r.tenantID, profile, status, task, actualMM).Scan(&id))
		return id
	}

	// The printer's actual (4000 mm ≈ 11.9 g) is effective over the estimate.
	pj := newPrintJob("printing", 4000)
	out := r.call(t, operator, http.MethodPost, "/v1/print-jobs/"+pj+"/complete", nil, http.StatusOK)
	var estimateID, actualID string
	for _, f := range out["consumption"].([]any) {
		m := f.(map[string]any)
		if m["kind"] != "material" {
			continue
		}
		if m["basis"] == "estimate" {
			estimateID = m["id"].(string)
		} else {
			actualID = m["id"].(string)
		}
	}
	require.NotEmpty(t, estimateID)
	require.NotEmpty(t, actualID)
	before := r.onHand(t, item)
	refused := r.call(t, operator, http.MethodPost, "/v1/jobs/consumption/"+estimateID+"/corrections", map[string]any{"quantity": 30},
		http.StatusConflict)
	assert.Equal(t, "not_effective", refused["error"])
	assert.Equal(t, actualID, refused["effective_id"])
	assert.Equal(t, before, r.onHand(t, item), "a refused correction moves nothing")
	r.call(t, operator, http.MethodPost, "/v1/jobs/consumption/"+actualID+"/corrections", map[string]any{"quantity": 13}, http.StatusCreated)
	assert.Equal(t, 487.0, r.onHand(t, item))

	// A queued print job has not printed: it cannot be completed.
	assert.Equal(t, "print_job_not_started", errCode(r.call(t, operator, http.MethodPost,
		"/v1/print-jobs/"+newPrintJob("pending", nil)+"/complete", nil, http.StatusConflict)))

	// An already completed job: a different disposition or weighed quantity
	// is refused and points at the correction route; the same report is a
	// no-op.
	pj2 := newPrintJob("printing", nil)
	r.call(t, operator, http.MethodPost, "/v1/print-jobs/"+pj2+"/complete",
		map[string]any{"material_quantity": 9, "material_unit": "g"}, http.StatusOK)
	r.call(t, operator, http.MethodPost, "/v1/print-jobs/"+pj2+"/complete",
		map[string]any{"material_quantity": 9, "material_unit": "g"}, http.StatusOK)
	differs := r.call(t, operator, http.MethodPost, "/v1/print-jobs/"+pj2+"/complete",
		map[string]any{"disposition": "scrap"}, http.StatusConflict)
	assert.Equal(t, "disposition_differs", differs["error"])
	assert.Contains(t, differs["correct_with"], "/corrections")
	again := r.call(t, operator, http.MethodPost, "/v1/print-jobs/"+pj2+"/complete",
		map[string]any{"material_quantity": 10, "material_unit": "g"}, http.StatusConflict)
	assert.Equal(t, "already_recorded", again["error"])
	assert.Contains(t, again["correct_with"], "/corrections")
}
