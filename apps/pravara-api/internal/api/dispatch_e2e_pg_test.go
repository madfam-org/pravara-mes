package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatch"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatchfakes"
)

const boxSpecs = `{"bounding_box_mm": {"x": 80, "y": 60, "z": 40}}`

// completeJob does what the telemetry worker does when the device reports
// Job/Status = complete for the job of the command (P5-HOST sparkplug store
// on top of #51's completion path), in one tenant transaction as
// pravara_app: the ledger row is completed, task.job_completed is written,
// and machine.job_completed carries the printer and host timestamps.
func (r *dispatchRig) completeJob(t *testing.T, commandID, machineID, taskID uuid.UUID) {
	t.Helper()
	q := db.NewTenantDB(r.app, nil)
	require.NoError(t, db.RunInTenantTx(context.Background(), r.app, r.tenantID.String(), func(ctx context.Context) error {
		if _, err := q.ExecContext(ctx, `UPDATE task_commands SET status = 'completed', completed_at = NOW()
			WHERE command_id = $1 AND machine_id = $2`, commandID, machineID); err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, ev := range []struct {
			typ, ns string
			data    map[string]any
		}{
			{"task.job_completed", "tasks", map[string]any{"task_id": taskID, "command_id": commandID,
				"machine_id": machineID, "command_type": "start_job", "status": "completed", "timestamp": now}},
			{"machine.job_completed", "machines", map[string]any{"machine_id": machineID, "machine_code": "voron-01",
				"edge_node_id": "site-lab", "command_id": commandID, "job_id": taskID.String(), "job_status": "complete",
				"task_id": taskID, "printer_reported_at": "2026-10-04T10:00:00.25Z",
				"host_received_at": "2026-10-04T10:00:00.4Z", "recorded_at": now, "bdseq": 3, "seq": 41}},
		} {
			payload, _ := json.Marshal(map[string]any{"id": uuid.NewString(), "type": ev.typ, "tenant_id": r.tenantID,
				"timestamp": now, "data": ev.data})
			if _, err := q.ExecContext(ctx, `INSERT INTO event_outbox (tenant_id, event_type, channel_namespace, payload)
				VALUES ($1, $2, $3, $4)`, r.tenantID, ev.typ, ev.ns, payload); err != nil {
				return err
			}
		}
		return nil
	}))
}

func TestDispatchEndToEnd(t *testing.T) {
	r := newDispatchRig(t)
	voron := r.seedMachine(t, "voron-01", "idle", "tpu-95a")
	plaOnly := r.seedMachine(t, "voron-02", "idle", "pla")
	task := r.seedTask(t, boxSpecs)
	person := r.userToken(t, "operator")

	// 1. Dry run: ranked and explained, nothing reserved.
	w := r.do(http.MethodPost, "/v1/match", person, map[string]any{"task_id": task})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var match dispatch.MatchOutcome
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &match))
	assert.True(t, match.DryRun)
	assert.Equal(t, r.typeID, match.Product.TypeShellID)
	assert.Equal(t, []string{"tpu-95a"}, match.Effective.Materials.AnyOf, "part 'body' narrows materials to tpu-95a")
	require.Len(t, match.Result.Candidates, 2)
	best := match.Result.Candidates[0]
	assert.Equal(t, voron, best.MachineID)
	assert.Equal(t, 1, best.Rank)
	assert.Equal(t, "tpu-safe-0.20-klipper@1", best.Selection.ProcessProfile)
	assert.False(t, match.Result.Candidates[1].Eligible)
	assert.Equal(t, plaOnly, match.Result.Candidates[1].MachineID)
	assert.Empty(t, match.Result.Gaps)
	var reservations int
	require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM machine_reservations`).Scan(&reservations))
	assert.Zero(t, reservations, "a dry run reserves nothing")

	// Route matrix: machine tokens may match (read) but never dispatch.
	reader := r.machineToken(t, "pravara-mes:read", nil)
	assert.Equal(t, http.StatusOK, r.do(http.MethodPost, "/v1/match", reader, map[string]any{"task_id": task}).Code)
	intake := r.machineToken(t, "pravara-mes:jobs", nil)
	assert.Equal(t, http.StatusForbidden, r.do(http.MethodPost, "/v1/dispatches", intake, map[string]any{"task_id": task}).Code)
	assert.Equal(t, http.StatusForbidden, r.do(http.MethodPost, "/v1/match", r.machineToken(t, "pravara-mes:nodes", nil), map[string]any{"task_id": task}).Code)

	// 2. Dispatch (a person).
	w = r.do(http.MethodPost, "/v1/dispatches", person, map[string]any{"task_id": task})
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var created repositories.DispatchJob
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.Equal(t, repositories.DispatchQueued, created.Status)
	assert.Equal(t, http.StatusConflict, r.do(http.MethodPost, "/v1/dispatches", person, map[string]any{"task_id": task}).Code,
		"one live dispatch per task")

	// 3. match + reservation → render → slice → start_job.
	d := r.runUntil(t, created.ID, repositories.DispatchReserved)
	assert.Equal(t, voron, *d.MachineID)
	d = r.runUntil(t, created.ID, repositories.DispatchRendered)
	assert.Len(t, r.yantra.Requests, 1)
	assert.Equal(t, "3mf", r.yantra.Requests[0]["export_format"])
	assert.Equal(t, float64(40), r.yantra.Requests[0]["parameters"].(map[string]any)["size"])
	d = r.runUntil(t, created.ID, repositories.DispatchSliced)
	require.Len(t, r.fabprep.Jobs, 1)
	for _, job := range r.fabprep.Jobs {
		assert.Equal(t, "klipper-corexy-350-0.4@1", job.Body["printer_profile"])
		assert.Equal(t, "tpu-95a-klipper@1", job.Body["filament_profile"])
		assert.Equal(t, "tpu-safe-0.20-klipper@1", job.Body["process_profile"])
		assert.Equal(t, "body", job.Body["part"])
		assert.Equal(t, dispatchfakes.Digest(r.yantra.Geometry), job.Body["input"].(map[string]any)["sha256"])
		assert.NotNil(t, job.Body["input"].(map[string]any)["variables"], "the GOC-1 sidecar travels with the input")
		reqs := job.Body["requirements"].(map[string]any)
		assert.Contains(t, reqs, "process_parameters")
		assert.True(t, strings.HasPrefix(job.Key, "pravara-dispatch:"+created.ID.String()))
		assert.GreaterOrEqual(t, job.Polls, 2, "polled until it succeeded")
	}
	d = r.runUntil(t, created.ID, repositories.DispatchCommandEnqueued)
	require.NotNil(t, d.CommandID)

	// The ledger row committed before the stream append. It carries the
	// artifact parameters: the Sparkplug host re-sends unacknowledged DCMDs
	// from the ledger after a DBIRTH.
	var ledgerStatus, ledgerParams string
	var ledgerMachine uuid.UUID
	require.NoError(t, r.admin.QueryRow(`SELECT status, machine_id, parameters::text FROM task_commands WHERE command_id = $1`,
		*d.CommandID).Scan(&ledgerStatus, &ledgerMachine, &ledgerParams))
	assert.Equal(t, "pending", ledgerStatus)
	assert.Equal(t, voron, ledgerMachine)
	assert.Contains(t, ledgerParams, "artifact_url")
	assert.Contains(t, ledgerParams, "artifact_sha256")

	entries, err := r.redis.Stream("pravara:commands")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	fields := map[string]string{}
	for i := 0; i+1 < len(entries[0].Values); i += 2 {
		fields[entries[0].Values[i]] = entries[0].Values[i+1]
	}
	assert.Equal(t, d.CommandID.String(), fields["command_id"])
	var cmd map[string]any
	require.NoError(t, json.Unmarshal([]byte(fields["payload"]), &cmd))
	assert.Equal(t, "start_job", cmd["command"])
	assert.Equal(t, voron.String(), cmd["machine_id"])
	assert.Equal(t, task.String(), cmd["task_id"])
	params := cmd["parameters"].(map[string]any)
	assert.True(t, strings.HasPrefix(params["artifact_url"].(string), r.fabprep.Server.URL+"/v1/artifacts/"))
	assert.Regexp(t, "^[0-9a-f]{64}$", params["artifact_sha256"])
	assert.Equal(t, "text/x-gcode", params["artifact_media_type"])
	assert.Equal(t, "voron-01", params["device_id"])

	// 4. Still printing: nothing happens.
	r.svc.RunOnce(context.Background())
	assert.Equal(t, repositories.DispatchCommandEnqueued, r.dispatch(t, created.ID).Status)

	// 5. Job/Status = complete → record, genealogy, passport outbox.
	r.shells.FailNext = 1 // first publish fails: the record must survive
	r.completeJob(t, *d.CommandID, voron, task)
	_, err = r.admin.Exec(`UPDATE dispatch_jobs SET next_attempt_at = NOW() WHERE id = $1`, created.ID)
	require.NoError(t, err)
	d = r.runUntil(t, created.ID, repositories.DispatchCompleted)
	var reserved int
	require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM machine_reservations WHERE dispatch_id = $1 AND status = 'active'`, created.ID).Scan(&reserved))
	assert.Zero(t, reserved, "reservation released on completion")

	var record []byte
	var genealogy uuid.NullUUID
	require.NoError(t, r.admin.QueryRow(`SELECT record, genealogy_id FROM manufacturing_records WHERE dispatch_id = $1`, created.ID).Scan(&record, &genealogy))
	assert.True(t, genealogy.Valid, "genealogy record created")
	var rec map[string]any
	require.NoError(t, json.Unmarshal(record, &rec))
	assert.Equal(t, r.typeID, rec["type_shell_id"])
	assert.Equal(t, "voron-01", rec["machine_code"])
	assert.Equal(t, "tpu-95a", rec["material_class"])
	assert.Equal(t, "LOT-TPU-95A-0042", rec["material_lot"])
	assert.Equal(t, "2026-10-04T10:00:00.25Z", rec["printer_reported_at"])
	assert.Equal(t, "2026-10-04T10:00:00.4Z", rec["broker_received_at"])
	assert.NotEmpty(t, rec["server_recorded_at"])
	assert.Regexp(t, "^[0-9a-f]{64}$", rec["gcode_sha256"])
	assert.Regexp(t, "^[0-9a-f]{64}$", rec["goc1_instance_id"])
	assert.Len(t, rec["slicer_profiles"].(map[string]any), 3)
	assert.Nil(t, rec["gaps"], "every fact was available")
	_, err = r.admin.Exec(`UPDATE manufacturing_records SET record = '{}' WHERE dispatch_id = $1`, created.ID)
	assert.ErrorContains(t, err, "append-only")

	// 6. Delivery: 503 first (kept, retried), then instance, then the event.
	deadline := time.Now().Add(30 * time.Second)
	for {
		r.svc.RunOnce(context.Background())
		require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM passport_outbox WHERE status <> 'delivered'`).Scan(&reserved))
		if reserved == 0 || time.Now().After(deadline) {
			break
		}
		_, _ = r.admin.Exec(`UPDATE passport_outbox SET next_attempt_at = NOW() WHERE status = 'pending'`)
		time.Sleep(100 * time.Millisecond)
	}
	require.Zero(t, reserved, "all passport rows delivered")
	require.Len(t, r.shells.Instances, 1)
	for id, env := range r.shells.Instances {
		assert.Equal(t, "jnc_ash", r.shells.Tenants[id], "published with the tenant's org-bound client")
		assert.Contains(t, string(env), r.typeID)
		assert.Len(t, r.shells.Events[id], 1)
	}

	w = r.do(http.MethodGet, "/v1/dispatches/"+created.ID.String(), reader, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var view struct {
		Dispatch   repositories.DispatchJob             `json:"dispatch"`
		Record     *repositories.ManufacturingRecordRow `json:"manufacturing_record"`
		Deliveries []repositories.PassportOutboxRow     `json:"passport_deliveries"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &view))
	assert.Equal(t, repositories.DispatchCompleted, view.Dispatch.Status)
	require.NotNil(t, view.Record)
	require.Len(t, view.Deliveries, 2)
	assert.Equal(t, "instance", view.Deliveries[0].Kind)
	assert.Equal(t, 2, view.Deliveries[0].Attempts, "one 503, then delivered")
	assert.Equal(t, "delivered", view.Deliveries[1].Status)
	assert.Equal(t, 1, r.janua.Issued["jnc_ash"], "token cached across deliveries")

	// Another tenant (pravara_app, its own scope) sees none of it.
	other := uuid.New()
	_, err = r.admin.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Other', 'other')`, other)
	require.NoError(t, err)
	require.NoError(t, db.RunInTenantTx(context.Background(), r.app, other.String(), func(ctx context.Context) error {
		q := db.NewTenantDB(r.app, nil)
		for _, table := range []string{"dispatch_jobs", "machine_reservations", "manufacturing_records", "passport_outbox"} {
			var n int
			require.NoError(t, q.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n))
			assert.Zero(t, n, table)
		}
		return nil
	}))
}
