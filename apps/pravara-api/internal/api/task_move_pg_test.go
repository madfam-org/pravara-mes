package api

// The task move with from_status and an optional position (RFC 0064 §4.1,
// ruling PV-5), against PostgreSQL (opt-in with PRAVARA_TEST_DATABASE_URL,
// see newScopeRig), through the real middleware with a person's token.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskMoveFromStatusAgainstPostgres(t *testing.T) {
	rig := newScopeRig(t)
	person := rig.userToken(t, "operator")

	seed := func(title, status string, position int) uuid.UUID {
		var id uuid.UUID
		require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO tasks (tenant_id, title, status, kanban_position)
			VALUES ($1, $2, $3, $4) RETURNING id`, rig.tenantID, title, status, position).Scan(&id))
		return id
	}
	state := func(id uuid.UUID) (string, int) {
		var status string
		var position int
		require.NoError(t, rig.sqlDB.QueryRow(`SELECT status, kanban_position FROM tasks WHERE id = $1`, id).Scan(&status, &position))
		return status, position
	}
	move := func(id uuid.UUID, body map[string]any) (int, map[string]any) {
		w := rig.do(http.MethodPost, "/v1/tasks/"+id.String()+"/move", person, body)
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
		return w.Code, out
	}

	// Two tasks already in progress; the one moved goes after them.
	seed("running 1", "in_progress", 1)
	seed("running 2", "in_progress", 2)
	queued := seed("waiting", "queued", 1)

	// A move from a status the task is not in changes nothing.
	code, out := move(queued, map[string]any{"status": "completed", "from_status": "in_progress"})
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "status_conflict", out["error"])
	assert.Equal(t, "queued", out["current_status"])
	status, position := state(queued)
	assert.Equal(t, "queued", status)
	assert.Equal(t, 1, position)

	// The stated status matches, no position: the end of the target column.
	code, out = move(queued, map[string]any{"status": "in_progress", "from_status": "queued"})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "in_progress", out["status"])
	assert.Equal(t, "queued", out["previous_status"])
	assert.EqualValues(t, 3, out["kanban_position"])
	status, position = state(queued)
	assert.Equal(t, "in_progress", status)
	assert.Equal(t, 3, position)

	// The same move delivered again is refused: the task left "queued".
	code, out = move(queued, map[string]any{"status": "in_progress", "from_status": "queued"})
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "in_progress", out["current_status"])

	// A caller that sends status and position, as before, is unchanged.
	code, out = move(queued, map[string]any{"status": "quality_check", "position": 1})
	require.Equal(t, http.StatusOK, code, out)
	status, position = state(queued)
	assert.Equal(t, "quality_check", status)
	assert.Equal(t, 1, position)

	// An unknown from_status is a 400, not a conflict.
	code, out = move(queued, map[string]any{"status": "completed", "from_status": "done"})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "invalid_from_status", out["error"])

	// A task of no one's: 404, with or without from_status.
	code, _ = move(uuid.New(), map[string]any{"status": "completed", "from_status": "in_progress"})
	assert.Equal(t, http.StatusNotFound, code)
}
