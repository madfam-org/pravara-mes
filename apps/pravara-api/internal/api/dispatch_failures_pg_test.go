package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
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
