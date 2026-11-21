package api

// Idempotency-Key on the terminal's writes (RFC 0064 §5, ruling PV-4),
// against PostgreSQL (opt-in with PRAVARA_TEST_DATABASE_URL, see
// newScopeRig): the claim, the write and the stored answer share the
// request's transaction, so a repeated or concurrent delivery runs once.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

func (r *scopeRig) doWithKey(method, path, bearer, key string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	if key != "" {
		req.Header.Set(middleware.IdempotencyKeyHeader, key)
	}
	w := httptest.NewRecorder()
	r.router.ServeHTTP(w, req)
	return w
}

func TestIdempotencyKeyOnTheTaskMoveAgainstPostgres(t *testing.T) {
	rig := newScopeRig(t)
	// A person's token issued through a terminal's client: the key belongs
	// to that client.
	viaDevice := func(sub string) string {
		c := rig.baseClaims(sub)
		c["type"] = "access"
		c["client_id"] = "jnc_terminal_test"
		c["roles"] = []string{testRoleApp + ":operator"}
		return rig.sign(t, c, rig.key)
	}
	operator := viaDevice(uuid.NewString())

	newTask := func() string {
		var id uuid.UUID
		require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO tasks (tenant_id, title, status) VALUES ($1, 'Idempotent', 'queued') RETURNING id`,
			rig.tenantID).Scan(&id))
		return "/v1/tasks/" + id.String() + "/move"
	}
	keys := func(key string) int {
		var n int
		require.NoError(t, rig.sqlDB.QueryRow(`SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1`, key).Scan(&n))
		return n
	}
	body := map[string]any{"status": "in_progress", "from_status": "queued"}

	// First delivery runs; the second is the stored answer, not a 409.
	move := newTask()
	key := "01JDF3Q8Z5X7K2M4N6P8R0T2V4"
	first := rig.doWithKey(http.MethodPost, move, operator, key, body)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	again := rig.doWithKey(http.MethodPost, move, operator, key, body)
	assert.Equal(t, http.StatusOK, again.Code, again.Body.String())
	assert.Equal(t, "true", again.Header().Get(middleware.IdempotentReplayedHeader))
	assert.JSONEq(t, first.Body.String(), again.Body.String())
	assert.Equal(t, 1, keys(key))

	// The same key for another request of the same client.
	w := rig.doWithKey(http.MethodPost, move, operator, key, map[string]any{"status": "completed"})
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "idempotency_key_reused")

	// Another operator on the same device shares the device's keys.
	w = rig.doWithKey(http.MethodPost, move, viaDevice(uuid.NewString()), key, body)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "true", w.Header().Get(middleware.IdempotentReplayedHeader))

	// A refused write stores no key: the move's 409 leaves it free.
	conflictKey := "01JDF3Q8Z5X7K2M4N6P8R0T2V5"
	w = rig.doWithKey(http.MethodPost, move, operator, conflictKey, body) // the task already left "queued"
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "status_conflict")
	assert.Zero(t, keys(conflictKey))

	// A person at a desk with no client: the key is the person's own.
	desk := rig.userToken(t, "operator")
	other := newTask()
	w = rig.doWithKey(http.MethodPost, other, desk, key, body)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, w.Header().Get(middleware.IdempotentReplayedHeader), "another client's key is not this one")
}

func TestIdempotencyKeyConcurrentDeliveriesRunOnceAgainstPostgres(t *testing.T) {
	rig := newScopeRig(t)
	device := func() string {
		c := rig.baseClaims(uuid.NewString())
		c["type"] = "access"
		c["client_id"] = "jnc_terminal_test"
		c["roles"] = []string{testRoleApp + ":operator"}
		return rig.sign(t, c, rig.key)
	}()
	var taskID uuid.UUID
	require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO tasks (tenant_id, title, status) VALUES ($1, 'Raced', 'queued') RETURNING id`,
		rig.tenantID).Scan(&taskID))
	move := "/v1/tasks/" + taskID.String() + "/move"
	body := map[string]any{"status": "in_progress", "from_status": "queued"}

	const deliveries = 5
	codes := make([]int, deliveries)
	replayed := make([]bool, deliveries)
	var wg sync.WaitGroup
	for i := 0; i < deliveries; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := rig.doWithKey(http.MethodPost, move, device, "01JDF3Q8Z5X7K2M4N6P8R0T2V9", body)
			codes[i] = w.Code
			replayed[i] = w.Header().Get(middleware.IdempotentReplayedHeader) == "true"
		}(i)
	}
	wg.Wait()

	ran := 0
	for i := range codes {
		assert.Equal(t, http.StatusOK, codes[i], "delivery %d", i)
		if !replayed[i] {
			ran++
		}
	}
	assert.Equal(t, 1, ran, "exactly one delivery ran; the others got its answer")
	var status string
	require.NoError(t, rig.sqlDB.QueryRow(`SELECT status FROM tasks WHERE id = $1`, taskID).Scan(&status))
	assert.Equal(t, "in_progress", status)
}

// The key is checked after authentication and the route table: a device
// token is still refused on the move, key or no key.
func TestIdempotencyKeyOpensNothingAgainstPostgres(t *testing.T) {
	rig := newScopeRig(t)
	device := rig.machineToken(t, "pravara-mes:terminal", func(c jwt.MapClaims) { c["client_kind"] = "device" })
	w := rig.doWithKey(http.MethodPost, "/v1/tasks/"+uuid.NewString()+"/move", device, "01JDF3Q8Z5X7K2M4N6P8R0T2VA",
		map[string]any{"status": "in_progress"})
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

// A key older than the retention (35 days by default) counts as unused, and
// the claim deletes expired rows of its tenant.
func TestIdempotencyKeyExpiresAgainstPostgres(t *testing.T) {
	rig := newScopeRig(t)
	c := rig.baseClaims(uuid.NewString())
	c["type"] = "access"
	c["client_id"] = "jnc_terminal_test"
	c["roles"] = []string{testRoleApp + ":operator"}
	operator := rig.sign(t, c, rig.key)

	const key = "01JDF3Q8Z5X7K2M4N6P8R0T2VB"
	for _, k := range []string{key, "01JDF3Q8Z5X7K2M4N6P8R0T2VC"} {
		_, err := rig.sqlDB.Exec(`INSERT INTO idempotency_keys (tenant_id, client, idempotency_key, route, request_sha256,
			response_status, response_content_type, response_body, created_at)
			VALUES ($1, 'client:jnc_terminal_test', $2, '/v1/tasks/:id/move', repeat('0', 64), 200, 'application/json', '{}',
			NOW() - INTERVAL '40 days')`, rig.tenantID, k)
		require.NoError(t, err)
	}
	var taskID uuid.UUID
	require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO tasks (tenant_id, title, status) VALUES ($1, 'Old key', 'queued') RETURNING id`,
		rig.tenantID).Scan(&taskID))

	w := rig.doWithKey(http.MethodPost, "/v1/tasks/"+taskID.String()+"/move", operator, key,
		map[string]any{"status": "in_progress", "from_status": "queued"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, w.Header().Get(middleware.IdempotentReplayedHeader), "an expired key is not replayed")

	var fresh, expired int
	require.NoError(t, rig.sqlDB.QueryRow(`SELECT count(*) FILTER (WHERE created_at > NOW() - INTERVAL '1 day'),
		count(*) FILTER (WHERE created_at < NOW() - INTERVAL '35 days') FROM idempotency_keys`).Scan(&fresh, &expired))
	assert.Equal(t, 1, fresh, "the key now holds this request")
	assert.Zero(t, expired, "the tenant's expired rows were deleted")
}
