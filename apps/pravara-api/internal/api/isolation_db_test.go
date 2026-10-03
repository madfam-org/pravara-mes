package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
)

// TestTenantIsolation_CrossTenantWritesRejected exercises the database layer
// directly as the application role.
func TestTenantIsolation_CrossTenantWritesRejected(t *testing.T) {
	r := newIsolationRig(t)
	aMachine := r.aMachine()
	bMachine := r.createID(r.B, "/v1/machines", map[string]any{"name": "B printer", "code": "B-1", "type": "3d_printer"})

	t.Run("insert with another tenant's tenant_id", func(t *testing.T) {
		_, err := r.scopedErr(r.B.ID, `INSERT INTO machines (tenant_id, name, code, type) VALUES ($1, 'x', 'X-1', 'cnc')`, r.A.ID)
		assert.ErrorContains(t, err, "row-level security")
	})
	t.Run("move own row to another tenant", func(t *testing.T) {
		_, err := r.scopedErr(r.B.ID, `UPDATE machines SET tenant_id = $1 WHERE id = $2`, r.A.ID, bMachine)
		assert.ErrorContains(t, err, "row-level security")
	})
	t.Run("update or delete another tenant's row affects nothing", func(t *testing.T) {
		n, err := r.scopedErr(r.B.ID, `UPDATE machines SET name = 'pwned' WHERE id = $1`, aMachine)
		require.NoError(t, err)
		assert.Zero(t, n)
		n, err = r.scopedErr(r.B.ID, `DELETE FROM machines WHERE id = $1`, aMachine)
		require.NoError(t, err)
		assert.Zero(t, n)
	})
	t.Run("own row referencing another tenant's parent", func(t *testing.T) {
		for _, q := range []string{
			`INSERT INTO telemetry (tenant_id, machine_id, metric_type, value) VALUES ($1, $2, 'temperature', 1)`,
			`INSERT INTO tasks (tenant_id, title, machine_id) VALUES ($1, 'x', $2)`,
			`INSERT INTO maintenance_schedules (tenant_id, machine_id, name, trigger_type) VALUES ($1, $2, 'x', 'calendar')`,
		} {
			_, err := r.scopedErr(r.B.ID, q, r.B.ID, aMachine)
			assert.ErrorContains(t, err, "row-level security", q)
		}
		// Through the API as well.
		res := r.do(r.B, http.MethodPost, "/v1/telemetry/batch", map[string]any{
			"records": []map[string]any{{"machine_id": aMachine, "metric_type": "temperature", "value": 1.0}}})
		assert.False(t, isSuccess(res.Code), "B attached telemetry to A's machine: %d %s", res.Code, res.Body)
		res = r.do(r.B, http.MethodPost, "/v1/tasks", map[string]any{"title": "x", "machine_id": aMachine})
		assert.False(t, isSuccess(res.Code), "B created a task on A's machine: %d %s", res.Code, res.Body)
		// The same writes against B's own machine succeed.
		_, err := r.scopedErr(r.B.ID, `INSERT INTO telemetry (tenant_id, machine_id, metric_type, value) VALUES ($1, $2, 'temperature', 1)`, r.B.ID, bMachine)
		assert.NoError(t, err)
	})
	t.Run("no scope sees nothing and writes nothing", func(t *testing.T) {
		var n int
		require.NoError(t, r.pool.QueryRow(`SELECT COUNT(*) FROM machines`).Scan(&n))
		assert.Zero(t, n, "a connection with no tenant must see no tenant rows")
		_, err := r.pool.Exec(`INSERT INTO machines (tenant_id, name, code, type) VALUES ($1, 'x', 'NS-1', 'cnc')`, r.A.ID)
		assert.ErrorContains(t, err, "row-level security")
	})
	t.Run("system scope is read-only and limited to queue tables", func(t *testing.T) {
		r.seed(r.A.ID, `INSERT INTO event_outbox (tenant_id, event_type, channel_namespace, payload) VALUES ($1, 'x', 'x', '{}')`, r.A.ID)
		err := db.RunInSystemScope(context.Background(), r.pool, "test.system", func(ctx context.Context) error {
			var events, machines int
			if err := r.tdb.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_outbox WHERE tenant_id = $1`, r.A.ID).Scan(&events); err != nil {
				return err
			}
			assert.Equal(t, 1, events, "system scope reads the outbox across tenants")
			if err := r.tdb.QueryRowContext(ctx, `SELECT COUNT(*) FROM machines`).Scan(&machines); err != nil {
				return err
			}
			assert.Zero(t, machines, "system scope must not read tenant tables outside the queue set")
			return nil
		})
		require.NoError(t, err)
		err = db.RunInSystemScope(context.Background(), r.pool, "test.system", func(ctx context.Context) error {
			_, err := r.tdb.ExecContext(ctx, `DELETE FROM event_outbox`)
			return err
		})
		assert.ErrorContains(t, err, "read-only")
	})
	t.Run("truncate and migration table are not granted", func(t *testing.T) {
		_, err := r.pool.Exec(`TRUNCATE machines`)
		assert.ErrorContains(t, err, "permission denied")
		_, err = r.pool.Exec(`SELECT 1 FROM schema_migrations LIMIT 1`)
		assert.ErrorContains(t, err, "permission denied")
	})
}

// TestTenantIsolation_APIKeyPath: a tenant's API key reads only that tenant.
func TestTenantIsolation_APIKeyPath(t *testing.T) {
	r := newIsolationRig(t)
	aMachine := r.aMachine()
	bMachine := r.createID(r.B, "/v1/machines", map[string]any{"name": "B printer", "code": "B-1", "type": "3d_printer"})

	res := r.do(r.B, http.MethodPost, "/v1/api-keys", map[string]any{"name": "B key"})
	require.Equal(t, http.StatusCreated, res.Code, string(res.Body))
	var created struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	require.NoError(t, json.Unmarshal(res.Body, &created))

	list := r.doWith(map[string]string{"X-API-Key": created.Key}, http.MethodGet, "/v1/machines", nil)
	require.Equal(t, http.StatusOK, list.Code, string(list.Body))
	assert.Contains(t, string(list.Body), bMachine)
	assert.NotContains(t, string(list.Body), aMachine)
	assert.NotContains(t, string(list.Body), r.A.ID.String())

	get := r.doWith(map[string]string{"Authorization": "Bearer " + created.Key}, http.MethodGet, "/v1/machines/"+aMachine, nil)
	assert.Equal(t, http.StatusNotFound, get.Code)

	// last_used_at is written in the key's tenant transaction, off the request path.
	assert.Eventually(t, func() bool {
		var used bool
		_ = db.RunInTenantTx(context.Background(), r.pool, r.B.ID.String(), func(ctx context.Context) error {
			return r.tdb.QueryRowContext(ctx, `SELECT last_used_at IS NOT NULL FROM api_keys WHERE id = $1`, created.ID).Scan(&used)
		})
		return used
	}, 5*time.Second, 50*time.Millisecond)

	bad := r.doWith(map[string]string{"X-API-Key": "prv_" + uuid.NewString()}, http.MethodGet, "/v1/machines", nil)
	assert.Equal(t, http.StatusUnauthorized, bad.Code)
}

// TestTenantIsolation_ConcurrentRequests is the pool-race regression test:
// parallel requests for different tenants on a 4-connection pool must never
// see each other's rows.
func TestTenantIsolation_ConcurrentRequests(t *testing.T) {
	r := newIsolationRig(t)
	for i := 0; i < 5; i++ {
		r.createID(r.A, "/v1/machines", map[string]any{"name": fmt.Sprintf("A-%d", i), "code": fmt.Sprintf("A-%d", i), "type": "cnc"})
		r.createID(r.B, "/v1/machines", map[string]any{"name": fmt.Sprintf("B-%d", i), "code": fmt.Sprintf("B-%d", i), "type": "cnc"})
		r.createID(r.A, "/v1/orders", map[string]any{"customer_name": fmt.Sprintf("A-%d", i)})
		r.createID(r.B, "/v1/orders", map[string]any{"customer_name": fmt.Sprintf("B-%d", i)})
	}

	const workers, perWorker = 16, 40
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []string
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				me, other := r.A, r.B
				if (w+i)%2 == 1 {
					me, other = r.B, r.A
				}
				path := []string{"/v1/machines", "/v1/orders"}[i%2]
				res := r.do(me, http.MethodGet, path, nil)
				var out struct {
					Data []struct {
						TenantID string `json:"tenant_id"`
					} `json:"data"`
				}
				err := json.Unmarshal(res.Body, &out)
				ok := res.Code == http.StatusOK && err == nil && len(out.Data) == 5
				for _, row := range out.Data {
					ok = ok && row.TenantID == me.ID.String()
				}
				if !ok || containsID(res.Body, other.ID) {
					mu.Lock()
					failures = append(failures, fmt.Sprintf("%s as %s: %d %.200s", path, me.Slug, res.Code, res.Body))
					mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()
	assert.Empty(t, failures, "cross-tenant rows or wrong counts under concurrency")
	t.Logf("%d concurrent requests, %d failures", workers*perWorker, len(failures))
}

func containsID(body []byte, id uuid.UUID) bool {
	return strings.Contains(string(body), id.String())
}
