package api

// The task move with from_status and an optional position (RFC 0064 §4.1,
// ruling PV-5), against PostgreSQL (opt-in with PRAVARA_TEST_DATABASE_URL,
// see newScopeRig), through the real middleware with a person's token.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
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

// A person moves a machine-assigned task into in_progress with automation
// wired: automation writes the durable start_job command in its own
// transaction while the move's transaction holds the task's row. The row
// lock must not block that write's foreign-key check, or the request hangs.
func TestTaskMoveWithAutomationAgainstPostgres(t *testing.T) {
	redis := miniredis.RunT(t)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	publisher, err := pubsub.NewPublisher(pubsub.PublisherConfig{RedisURL: "redis://" + redis.Addr()}, log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = publisher.Close() })
	rig := newScopeRigWith(t, publisher)
	person := rig.userToken(t, "operator")

	machineID := uuid.New()
	_, err = rig.sqlDB.Exec(`INSERT INTO machines (id, tenant_id, name, code, type, status, mqtt_topic)
		VALUES ($1, $2, 'Simulated printer', 'SIM-01', '3d_printer', 'online', 'sim/printer/01')`, machineID, rig.tenantID)
	require.NoError(t, err)
	var taskID uuid.UUID
	require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO tasks (tenant_id, title, status, machine_id)
		VALUES ($1, 'Assigned', 'queued', $2) RETURNING id`, rig.tenantID, machineID).Scan(&taskID))

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- rig.do(http.MethodPost, "/v1/tasks/"+taskID.String()+"/move", person,
			map[string]any{"status": "in_progress", "from_status": "queued"})
	}()
	var w *httptest.ResponseRecorder
	select {
	case w = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the move did not answer: its row lock blocks automation's command write")
	}
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var status string
	require.NoError(t, rig.sqlDB.QueryRow(`SELECT status FROM tasks WHERE id = $1`, taskID).Scan(&status))
	assert.Equal(t, "in_progress", status)
	var commands int
	require.NoError(t, rig.sqlDB.QueryRow(`SELECT count(*) FROM task_commands WHERE task_id = $1 AND command_type = 'start_job'`,
		taskID).Scan(&commands))
	assert.Equal(t, 1, commands, "automation recorded one durable start_job command")
}

// Concurrent moves to the end of one column get distinct positions.
func TestTaskMoveConcurrentEndOfColumnAgainstPostgres(t *testing.T) {
	rig := newScopeRig(t)
	person := rig.userToken(t, "operator")
	const n = 5
	ids := make([]uuid.UUID, n)
	for i := range ids {
		require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO tasks (tenant_id, title, status, kanban_position)
			VALUES ($1, 'Raced', 'queued', $2) RETURNING id`, rig.tenantID, i+1).Scan(&ids[i]))
	}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			w := rig.do(http.MethodPost, "/v1/tasks/"+id.String()+"/move", person, map[string]any{"status": "completed"})
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		}(id)
	}
	wg.Wait()
	rows, err := rig.sqlDB.Query(`SELECT kanban_position FROM tasks WHERE tenant_id = $1 AND status = 'completed' ORDER BY 1`, rig.tenantID)
	require.NoError(t, err)
	defer rows.Close()
	var got []int
	for rows.Next() {
		var p int
		require.NoError(t, rows.Scan(&p))
		got = append(got, p)
	}
	assert.Equal(t, []int{1, 2, 3, 4, 5}, got)
}
