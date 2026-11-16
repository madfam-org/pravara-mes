package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
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

func (r *dispatchRig) create(t *testing.T, task uuid.UUID) uuid.UUID {
	t.Helper()
	w := r.do(http.MethodPost, "/v1/dispatches", r.userToken(t, "operator"), map[string]any{"task_id": task})
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var d repositories.DispatchJob
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &d))
	return d.ID
}

func (r *dispatchRig) due(t *testing.T) {
	_, err := r.admin.Exec(`UPDATE dispatch_jobs SET next_attempt_at = NOW() WHERE status NOT IN ('completed','failed','cancelled')`)
	require.NoError(t, err)
}

// Two dispatches, one eligible machine: the second waits (retryable,
// explained) instead of double-booking, and gets the machine once the first
// releases it.
func TestDispatchReservationPreventsDoubleBooking(t *testing.T) {
	r := newDispatchRig(t)
	voron := r.seedMachine(t, "voron-01", "idle", "tpu-95a")
	first := r.create(t, r.seedTask(t, boxSpecs))
	r.runUntil(t, first, repositories.DispatchReserved)
	second := r.create(t, r.seedTask(t, boxSpecs))
	r.due(t)
	r.svc.RunOnce(context.Background())
	d := r.dispatch(t, second)
	assert.Equal(t, repositories.DispatchQueued, d.Status)
	assert.Equal(t, "no_eligible_machine", d.ErrorCode)
	assert.Contains(t, d.ErrorMessage, "voron-01: reservation")
	require.NotNil(t, d.ErrorRetryable)
	assert.True(t, *d.ErrorRetryable)
	assert.Zero(t, d.Attempts, "waiting for a machine spends no attempts")

	var active int
	require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM machine_reservations WHERE machine_id = $1 AND status = 'active'`, voron).Scan(&active))
	assert.Equal(t, 1, active)

	// The first dispatch's start_job fails on the printer: it fails, the
	// reservation is released, and the second dispatch takes the machine.
	r.runUntil(t, first, repositories.DispatchCommandEnqueued)
	cmd := r.dispatch(t, first).CommandID
	_, err := r.admin.Exec(`UPDATE task_commands SET status = 'failed', error_message = 'device busy' WHERE command_id = $1`, *cmd)
	require.NoError(t, err)
	r.due(t)
	failed := r.runUntil(t, first, repositories.DispatchFailed)
	assert.Equal(t, "command_failed", failed.ErrorCode)
	assert.Contains(t, failed.ErrorMessage, "device busy")
	r.due(t)
	got := r.runUntil(t, second, repositories.DispatchReserved)
	assert.Equal(t, voron, *got.MachineID)
}

// The reserved machine starts another job (live state no longer idle)
// before start_job: the dispatch-time re-check releases it and matches again.
func TestDispatchRechecksTheMachineBeforeStartJob(t *testing.T) {
	r := newDispatchRig(t)
	busy := r.seedMachine(t, "voron-01", "idle", "tpu-95a")
	id := r.create(t, r.seedTask(t, boxSpecs))
	r.runUntil(t, id, repositories.DispatchSliced)

	_, err := r.admin.Exec(`UPDATE machine_live_state SET state_status = 'printing' WHERE machine_id = $1`, busy)
	require.NoError(t, err)
	r.svc.RunOnce(context.Background())
	d := r.dispatch(t, id)
	assert.Equal(t, repositories.DispatchQueued, d.Status)
	assert.Equal(t, "machine_no_longer_eligible", d.ErrorCode)
	assert.Contains(t, d.ErrorMessage, "printing")
	assert.Nil(t, d.CommandID, "no start_job was issued")
	var commands int
	require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM task_commands`).Scan(&commands))
	assert.Zero(t, commands)
	stream, _ := r.redis.Stream("pravara:commands")
	assert.Empty(t, stream)

	// A second idle machine takes over; the render is reused, slicing redone.
	other := r.seedMachine(t, "voron-02", "idle", "tpu-95a")
	r.due(t)
	d = r.runUntil(t, id, repositories.DispatchCommandEnqueued)
	assert.Equal(t, other, *d.MachineID)
	assert.Len(t, r.yantra.Requests, 1, "render not repeated")
	assert.Len(t, r.fabprep.Jobs, 2, "sliced again for the new machine (new idempotency key)")
}

// staleRematch takes a dispatch to "sliced", makes its machine busy so it goes
// back to matching, and ages its kept render by a day (yantra4d keeps a
// render for 24 hours). It returns the dispatch id and its row before the
// re-match.
func staleRematch(t *testing.T, r *dispatchRig, machine uuid.UUID) (uuid.UUID, *repositories.DispatchJob) {
	t.Helper()
	id := r.create(t, r.seedTask(t, boxSpecs))
	r.runUntil(t, id, repositories.DispatchSliced)
	require.Len(t, r.yantra.Requests, 1)
	first := r.dispatch(t, id)

	_, err := r.admin.Exec(`UPDATE machine_live_state SET state_status = 'printing' WHERE machine_id = $1`, machine)
	require.NoError(t, err)
	r.svc.RunOnce(context.Background())
	require.Equal(t, repositories.DispatchQueued, r.dispatch(t, id).Status)

	_, err = r.admin.Exec(`UPDATE dispatch_jobs SET render_bundle = jsonb_set(render_bundle, '{rendered_at}', to_jsonb($2::text))
		WHERE id = $1`, id, time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339))
	require.NoError(t, err)
	return id, first
}

// sliceKeys returns the distinct idempotency keys fabrication-prep received.
func sliceKeys(r *dispatchRig) []string {
	seen := map[string]bool{}
	for _, job := range r.fabprep.Jobs {
		seen[job.Key] = true
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func bundleOf(t *testing.T, d *repositories.DispatchJob) map[string]any {
	t.Helper()
	var bundle map[string]any
	require.NoError(t, json.Unmarshal(d.RenderBundle, &bundle))
	return bundle
}

// A render kept across a re-match is only reused while yantra4d still serves
// it. A day-old one is requested again, past yantra4d's cache, and an answer
// in which yantra4d left every part out is retried, not failed.
func TestDispatchRerendersAStaleRenderAfterARematch(t *testing.T) {
	r := newDispatchRig(t)
	busy := r.seedMachine(t, "voron-01", "idle", "tpu-95a")
	id, first := staleRematch(t, r, busy)

	r.yantra.Set(func(y *dispatchfakes.Yantra4D) { y.FailingRenders = 1 })
	r.seedMachine(t, "voron-02", "idle", "tpu-95a")
	r.due(t)
	d := r.runUntil(t, id, repositories.DispatchCommandEnqueued)

	require.Len(t, r.yantra.Requests, 3, "rendered again (one empty answer, one good) instead of reusing the day-old render")
	_, firstBypassed := r.yantra.Requests[0]["ignore_cache"]
	assert.False(t, firstBypassed, "the first render may come from yantra4d's cache")
	assert.Equal(t, true, r.yantra.Requests[1]["ignore_cache"], "a re-render must not be answered with the expiring artifact")
	assert.Equal(t, true, r.yantra.Requests[2]["ignore_cache"])

	before, after := bundleOf(t, first), bundleOf(t, d)
	assert.Equal(t, before["instance_id"], after["instance_id"], "the same design instance")
	assert.Equal(t, before["sha256"], after["sha256"])
	stamped, err := time.Parse(time.RFC3339Nano, after["rendered_at"].(string))
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), stamped, time.Minute, "the new render carries its own time")
	_, changed := after["previous_sha256"]
	assert.False(t, changed, "the bytes did not change, so nothing is recorded as superseded")
}

// The same machine frees up again and the re-render comes back with other
// bytes for the same design instance (yantra4d does not promise to reproduce
// them). The dispatch must go on with the new render: the slice request has a
// new body, so it needs a new idempotency key, or fabrication-prep refuses it
// for good.
func TestDispatchRerenderOnTheSameMachineWithChangedBytes(t *testing.T) {
	r := newDispatchRig(t)
	machine := r.seedMachine(t, "voron-01", "idle", "tpu-95a")
	id, first := staleRematch(t, r, machine)
	firstKeys := sliceKeys(r)
	require.Len(t, firstKeys, 1)

	r.yantra.Set(func(y *dispatchfakes.Yantra4D) {
		y.Geometry = append([]byte(nil), append(y.Geometry, " rebuilt"...)...)
	})
	_, err := r.admin.Exec(`UPDATE machine_live_state SET state_status = 'idle' WHERE machine_id = $1`, machine)
	require.NoError(t, err)
	r.due(t)
	d := r.runUntil(t, id, repositories.DispatchCommandEnqueued)
	require.Equal(t, machine, *d.MachineID, "matched to the same machine again")

	before, after := bundleOf(t, first), bundleOf(t, d)
	assert.Equal(t, before["instance_id"], after["instance_id"])
	assert.NotEqual(t, before["sha256"], after["sha256"], "the re-render produced other bytes")
	assert.Equal(t, before["sha256"], after["previous_sha256"], "the superseded digest is kept")

	keys := sliceKeys(r)
	require.Len(t, keys, 2, "sliced again from the new render, under a new idempotency key (a new body needs one)")
	for _, key := range keys {
		assert.True(t, strings.HasPrefix(key, "pravara-dispatch:"+id.String()+":"+machine.String()[:8]+":"), key)
	}

	// RFC 0059 S12: the new bytes are kept, and the earlier kept copy stays
	// in the store and on the record (the manufacturing record is never
	// deleted).
	was, now := renderOf(t, first), renderOf(t, d)
	require.Len(t, now.Kept, 2)
	assert.Equal(t, now.SHA256, now.Kept[0].SHA256)
	require.Len(t, now.SupersededKept, 2, "the earlier geometry and sidecar")
	assert.Equal(t, was.Kept, now.SupersededKept)
	for _, k := range append(append([]dispatch.KeptObject(nil), was.Kept...), now.Kept...) {
		_, ok := r.store.Bytes(k.Key)
		assert.True(t, ok, "%s %s still in the store", k.Kind, k.SHA256)
	}
	w := r.do(http.MethodGet, "/v1/dispatches/"+id.String()+"/artifacts/artifact?sha256="+was.SHA256, r.userToken(t, "operator"), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, was.SHA256, hexSHA(w.Body.Bytes()), "the superseded geometry can still be read")
}

// The design changed while the dispatch waited: the re-render is another
// instance. Making it under the first one's records would be wrong, so the
// dispatch stops and says to dispatch again.
func TestDispatchStopsWhenTheDesignInstanceChangedWhileItWaited(t *testing.T) {
	r := newDispatchRig(t)
	machine := r.seedMachine(t, "voron-01", "idle", "tpu-95a")
	id, _ := staleRematch(t, r, machine)

	r.yantra.Set(func(y *dispatchfakes.Yantra4D) { y.InstanceSalt = "edited" })
	r.seedMachine(t, "voron-02", "idle", "tpu-95a")
	r.due(t)
	d := r.runUntil(t, id, repositories.DispatchFailed)

	assert.Equal(t, "render_instance_changed", d.ErrorCode)
	assert.Contains(t, d.ErrorMessage, "dispatch again")
	require.NotNil(t, d.ErrorRetryable)
	assert.False(t, *d.ErrorRetryable)
	assert.Len(t, r.fabprep.Jobs, 1, "nothing was sliced from the changed design")
	var commands int
	require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM task_commands`).Scan(&commands))
	assert.Zero(t, commands, "no start_job was issued")
	var held int
	require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM machine_reservations WHERE dispatch_id = $1 AND released_at IS NULL`, id).Scan(&held))
	assert.Zero(t, held, "the machine is released")
}

// Content errors are terminal with the callee's reason; the completion must
// come from the dispatched machine.
func TestDispatchTerminalFailures(t *testing.T) {
	r := newDispatchRig(t)
	r.seedMachine(t, "voron-01", "idle", "tpu-95a")

	t.Run("override out of range", func(t *testing.T) {
		id := r.create(t, r.seedTask(t, `{"bounding_box_mm": {"x": 80, "y": 60, "z": 40}, "process_overrides": {"outer_wall_speed": 300}}`))
		d := r.runUntil(t, id, repositories.DispatchFailed)
		assert.Equal(t, "slice_rejected:override_out_of_range", d.ErrorCode)
		assert.Contains(t, d.ErrorMessage, "/overrides/outer_wall_speed")
		assert.False(t, *d.ErrorRetryable)
		var active int
		require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM machine_reservations WHERE dispatch_id = $1 AND status = 'active'`, id).Scan(&active))
		assert.Zero(t, active, "a failed dispatch releases its machine")
	})

	t.Run("part too large for every build volume", func(t *testing.T) {
		task := r.seedTask(t, `{"bounding_box_mm": {"x": 80, "y": 60, "z": 400}}`)
		w := r.do(http.MethodPost, "/v1/match", r.userToken(t, "operator"), map[string]any{"task_id": task})
		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "exceeds 350x350x340 mm")
	})

	t.Run("slice job failed", func(t *testing.T) {
		r.fabprep.FailWith = &struct{ Code, Message string }{"requirement_not_met", "sparse_infill_density 15 < required 40"}
		defer func() { r.fabprep.FailWith = nil }()
		id := r.create(t, r.seedTask(t, boxSpecs))
		d := r.runUntil(t, id, repositories.DispatchFailed)
		assert.Equal(t, "slice_failed:requirement_not_met", d.ErrorCode)
	})

	t.Run("completion from another machine is refused", func(t *testing.T) {
		task := r.seedTask(t, boxSpecs)
		id := r.create(t, task)
		d := r.runUntil(t, id, repositories.DispatchCommandEnqueued)
		impostor := uuid.New()
		q := db.NewTenantDB(r.app, nil)
		require.NoError(t, db.RunInTenantTx(context.Background(), r.app, r.tenantID.String(), func(ctx context.Context) error {
			if _, err := q.ExecContext(ctx, `UPDATE task_commands SET status = 'completed' WHERE command_id = $1`, *d.CommandID); err != nil {
				return err
			}
			payload, _ := json.Marshal(map[string]any{"type": "task.job_completed",
				"data": map[string]any{"command_id": d.CommandID, "machine_id": impostor, "task_id": task}})
			_, err := q.ExecContext(ctx, `INSERT INTO event_outbox (tenant_id, event_type, channel_namespace, payload)
				VALUES ($1, 'task.job_completed', 'tasks', $2)`, r.tenantID, payload)
			return err
		}))
		r.due(t)
		failed := r.runUntil(t, id, repositories.DispatchFailed)
		assert.Equal(t, "completion_machine_mismatch", failed.ErrorCode)
		var records int
		require.NoError(t, r.admin.QueryRow(`SELECT count(*) FROM manufacturing_records WHERE dispatch_id = $1`, id).Scan(&records))
		assert.Zero(t, records, "no passport for an unbound completion")
	})

	t.Run("unknown task and missing product are explained", func(t *testing.T) {
		w := r.do(http.MethodPost, "/v1/match", r.userToken(t, "operator"), map[string]any{"task_id": uuid.New()})
		assert.Equal(t, http.StatusNotFound, w.Code)
		var task uuid.UUID
		require.NoError(t, r.admin.QueryRow(`INSERT INTO tasks (tenant_id, title) VALUES ($1, 'no item') RETURNING id`, r.tenantID).Scan(&task))
		w = r.do(http.MethodPost, "/v1/dispatches", r.userToken(t, "operator"), map[string]any{"task_id": task})
		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
		assert.Contains(t, w.Body.String(), "task_without_order_item")
	})
}
