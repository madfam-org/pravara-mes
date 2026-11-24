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
	t.Run("truncate and migration-table writes are not granted", func(t *testing.T) {
		_, err := r.pool.Exec(`TRUNCATE machines`)
		assert.ErrorContains(t, err, "permission denied")
		// schema_migrations is SELECT-only for the application role
		// (infra/db/roles/pravara_app_grants.sql, docs/operations/
		// database-app-role.md): the schema guard reads it; recording,
		// changing or removing a migration stays with the owner.
		_, err = r.pool.Exec(`SELECT 1 FROM schema_migrations LIMIT 1`)
		assert.NoError(t, err, "the schema guard reads schema_migrations")
		for _, stmt := range []string{
			`INSERT INTO schema_migrations (version) VALUES ('999_isolation_probe')`,
			`UPDATE schema_migrations SET applied_at = applied_at`,
			`DELETE FROM schema_migrations`,
			`TRUNCATE schema_migrations`,
		} {
			_, err = r.pool.Exec(stmt)
			assert.ErrorContains(t, err, "permission denied", stmt)
		}
	})
}

// TestTenantIsolation_MachineReferencesValidated: routes that write a row
// referencing a machine look the machine up in the caller's tenant first and
// answer 404 for any other machine; nothing is written.
func TestTenantIsolation_MachineReferencesValidated(t *testing.T) {
	r := newIsolationRig(t)
	aMachine := r.aMachine()
	bMachine := r.createID(r.B, "/v1/machines", map[string]any{"name": "B printer", "code": "B-REF-1", "type": "3d_printer"})

	writes := func(machine string) []call {
		return []call{
			{http.MethodPost, "/v1/maintenance/schedules", map[string]any{
				"machine_id": machine, "name": "B schedule", "trigger_type": "calendar"}},
			{http.MethodPost, "/v1/maintenance/work-orders", map[string]any{
				"machine_id": machine, "work_order_number": "WO-" + uuid.NewString()[:8], "title": "B work order"}},
			{http.MethodPost, "/v1/telemetry/batch", map[string]any{
				"records": []map[string]any{{"machine_id": machine, "metric_type": "temperature", "value": 2.0}}}},
		}
	}

	for _, c := range writes(aMachine) {
		res := r.do(r.B, c.method, c.path, c.body)
		assert.Equalf(t, http.StatusNotFound, res.Code, "B %s %s with A's machine: %s", c.method, c.path, res.Body)
	}
	// A batch mixing B's machine with A's is refused as a whole.
	res := r.do(r.B, http.MethodPost, "/v1/telemetry/batch", map[string]any{"records": []map[string]any{
		{"machine_id": bMachine, "metric_type": "temperature", "value": 3.0},
		{"machine_id": aMachine, "metric_type": "temperature", "value": 4.0},
	}})
	assert.Equal(t, http.StatusNotFound, res.Code, string(res.Body))

	// Nothing reached A's machine.
	for _, q := range []string{
		`SELECT COUNT(*) FROM maintenance_schedules WHERE machine_id = $1`,
		`SELECT COUNT(*) FROM maintenance_work_orders WHERE machine_id = $1`,
		`SELECT COUNT(*) FROM telemetry WHERE machine_id = $1`,
	} {
		var n int
		require.NoError(t, db.RunInTenantTx(context.Background(), r.pool, r.A.ID.String(), func(ctx context.Context) error {
			return r.tdb.QueryRowContext(ctx, q, aMachine).Scan(&n)
		}))
		assert.Zero(t, n, q)
	}

	// The same writes against B's own machine succeed; a work order opened
	// through the route starts as "scheduled".
	for _, c := range writes(bMachine) {
		res := r.do(r.B, c.method, c.path, c.body)
		require.Equalf(t, http.StatusCreated, res.Code, "B %s %s with its own machine: %s", c.method, c.path, res.Body)
		if c.path == "/v1/maintenance/work-orders" {
			var wo struct {
				Status    string `json:"status"`
				MachineID string `json:"machine_id"`
			}
			require.NoError(t, json.Unmarshal(res.Body, &wo))
			assert.Equal(t, "scheduled", wo.Status)
			assert.Equal(t, bMachine, wo.MachineID)
		}
	}
}

// refRows are one tenant's rows that other writes can reference.
type refRows struct {
	order, item, task, machine, user, schedule, lot, cert, inspection, product string
}

func (r *isolationRig) refRows(f tenantFixture) refRows {
	r.t.Helper()
	var x refRows
	x.order = r.createID(f, "/v1/orders", map[string]any{"customer_name": "ref customer"})
	x.item = r.createID(f, "/v1/orders/"+x.order+"/items", map[string]any{"product_name": "ref item", "quantity": 1})
	x.task = r.createID(f, "/v1/tasks", map[string]any{"title": "ref task"})
	x.machine = r.createID(f, "/v1/machines", map[string]any{"name": "ref printer", "code": "R-" + uuid.NewString()[:8], "type": "3d_printer"})
	x.user = f.UserID.String()
	x.schedule = r.createID(f, "/v1/maintenance/schedules", map[string]any{"machine_id": x.machine, "name": "ref PM", "trigger_type": "calendar"})
	x.lot = r.createID(f, "/v1/quality/batches", map[string]any{
		"lot_number": "LOT-" + uuid.NewString()[:8], "product_name": "ref resin", "quantity": 1, "unit": "kg"})
	x.cert = r.createID(f, "/v1/quality/certificates", map[string]any{
		"certificate_number": "CERT-" + uuid.NewString()[:8], "type": "coc", "title": "ref cert"})
	x.inspection = r.createID(f, "/v1/quality/inspections", map[string]any{
		"inspection_number": "INS-" + uuid.NewString()[:8], "type": "final"})
	x.product = r.createID(f, "/v1/products", map[string]any{
		"sku": "SKU-" + uuid.NewString()[:8], "name": "ref bracket", "version": "1.0", "category": "3d_print"})
	return x
}

// TestTenantIsolation_ReferencesValidated: writes that reference an order,
// order item, task, machine, user, schedule, lot, certificate, inspection or
// product definition look it up in the caller's tenant first and answer 404
// for a row of another tenant; nothing is written. The same writes with the
// caller's own rows succeed.
func TestTenantIsolation_ReferencesValidated(t *testing.T) {
	r := newIsolationRig(t)
	a, own := r.refRows(r.A), r.refRows(r.B)
	bWorkOrder := r.createID(r.B, "/v1/maintenance/work-orders", map[string]any{
		"machine_id": own.machine, "work_order_number": "WO-" + uuid.NewString()[:8], "title": "B work order"})
	bGenealogy := r.createID(r.B, "/v1/genealogy", map[string]any{"serial_number": "B-ref-serial"})
	bInstruction := r.createID(r.B, "/v1/work-instructions", map[string]any{"title": "B setup", "version": "1.0", "category": "setup"})

	// Each write is B's, on B's rows, with one reference taken from x.
	// ownFails marks writes that fail in this rig for another reason when
	// every reference is B's (the genealogy service is not wired here).
	type refWrite struct {
		call
		ownFails bool
	}
	writes := func(x refRows) []refWrite {
		w := func(method, path string, body map[string]any) refWrite {
			return refWrite{call: call{method, path, body}}
		}
		wo := func() string { return "WO-" + uuid.NewString()[:8] }
		cert := func(field, id string) map[string]any {
			return map[string]any{"certificate_number": "CERT-" + uuid.NewString()[:8], "type": "coc", "title": "B cert", field: id}
		}
		insp := func(field, id string) map[string]any {
			return map[string]any{"inspection_number": "INS-" + uuid.NewString()[:8], "type": "final", field: id}
		}
		return []refWrite{
			w(http.MethodPost, "/v1/tasks", map[string]any{"title": "B task", "order_id": x.order}),
			w(http.MethodPost, "/v1/tasks", map[string]any{"title": "B task", "order_item_id": x.item}),
			w(http.MethodPost, "/v1/tasks", map[string]any{"title": "B task", "machine_id": x.machine}),
			w(http.MethodPost, "/v1/tasks", map[string]any{"title": "B task", "assigned_user_id": x.user}),
			w(http.MethodPatch, "/v1/tasks/"+own.task, map[string]any{"order_id": x.order}),
			w(http.MethodPatch, "/v1/tasks/"+own.task, map[string]any{"machine_id": x.machine}),
			w(http.MethodPost, "/v1/tasks/"+own.task+"/assign", map[string]any{"machine_id": x.machine}),
			w(http.MethodPost, "/v1/tasks/"+own.task+"/assign", map[string]any{"user_id": x.user}),
			w(http.MethodPost, "/v1/maintenance/schedules", map[string]any{
				"machine_id": own.machine, "name": "B PM", "trigger_type": "calendar", "assigned_to": x.user}),
			w(http.MethodPatch, "/v1/maintenance/schedules/"+own.schedule, map[string]any{"assigned_to": x.user}),
			w(http.MethodPost, "/v1/maintenance/work-orders", map[string]any{
				"machine_id": own.machine, "work_order_number": wo(), "title": "B work order", "schedule_id": x.schedule}),
			w(http.MethodPost, "/v1/maintenance/work-orders", map[string]any{
				"machine_id": own.machine, "work_order_number": wo(), "title": "B work order", "assigned_to": x.user}),
			w(http.MethodPatch, "/v1/maintenance/work-orders/"+bWorkOrder, map[string]any{"assigned_to": x.user}),
			w(http.MethodPost, "/v1/quality/certificates", cert("order_id", x.order)),
			w(http.MethodPost, "/v1/quality/certificates", cert("task_id", x.task)),
			w(http.MethodPost, "/v1/quality/certificates", cert("machine_id", x.machine)),
			w(http.MethodPost, "/v1/quality/certificates", cert("batch_lot_id", x.lot)),
			w(http.MethodPost, "/v1/quality/certificates", cert("issued_by", x.user)),
			w(http.MethodPatch, "/v1/quality/certificates/"+own.cert, map[string]any{"approved_by": x.user}),
			w(http.MethodPost, "/v1/quality/inspections", insp("order_id", x.order)),
			w(http.MethodPost, "/v1/quality/inspections", insp("task_id", x.task)),
			w(http.MethodPost, "/v1/quality/inspections", insp("machine_id", x.machine)),
			w(http.MethodPost, "/v1/quality/inspections", insp("inspector_id", x.user)),
			w(http.MethodPatch, "/v1/quality/inspections/"+own.inspection, map[string]any{"certificate_id": x.cert}),
			w(http.MethodPost, "/v1/quality/inspections/"+own.inspection+"/complete", map[string]any{"result": "pass", "certificate_id": x.cert}),
			w(http.MethodPost, "/v1/quality/batches", map[string]any{
				"lot_number": "LOT-" + uuid.NewString()[:8], "product_name": "B resin", "quantity": 1, "unit": "kg", "order_id": x.order}),
			w(http.MethodPost, "/v1/genealogy", map[string]any{"serial_number": "B-" + uuid.NewString()[:8], "product_definition_id": x.product}),
			w(http.MethodPost, "/v1/genealogy", map[string]any{"serial_number": "B-" + uuid.NewString()[:8], "order_item_id": x.item}),
			w(http.MethodPost, "/v1/genealogy", map[string]any{"serial_number": "B-" + uuid.NewString()[:8], "inspection_id": x.inspection}),
			w(http.MethodPost, "/v1/genealogy", map[string]any{"serial_number": "B-" + uuid.NewString()[:8], "certificate_id": x.cert}),
			w(http.MethodPatch, "/v1/genealogy/"+bGenealogy, map[string]any{"task_id": x.task}),
			{call: call{http.MethodPost, "/v1/genealogy/" + bGenealogy + "/seal", map[string]any{"sealed_by": x.user}}, ownFails: true},
			w(http.MethodPost, "/v1/work-instructions", map[string]any{
				"title": "B setup", "version": "1.0", "category": "setup", "product_definition_id": x.product}),
			w(http.MethodPatch, "/v1/work-instructions/"+bInstruction, map[string]any{"product_definition_id": x.product}),
			w(http.MethodPost, "/v1/tasks/"+x.task+"/work-instructions", map[string]any{"work_instruction_id": bInstruction}),
		}
	}

	for _, c := range writes(a) {
		res := r.do(r.B, c.method, c.path, c.body)
		assert.Equalf(t, http.StatusNotFound, res.Code, "B %s %s referencing A's row %v: %s", c.method, c.path, c.body, res.Body)
	}

	// No row of B's references any of A's rows.
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`SELECT COUNT(*) FROM tasks WHERE order_id = $1 OR order_item_id = $2 OR machine_id = $3 OR assigned_user_id = $4`,
			[]any{a.order, a.item, a.machine, a.user}},
		{`SELECT COUNT(*) FROM maintenance_schedules WHERE assigned_to = $1`, []any{a.user}},
		{`SELECT COUNT(*) FROM maintenance_work_orders WHERE schedule_id = $1 OR assigned_to = $2`, []any{a.schedule, a.user}},
		{`SELECT COUNT(*) FROM quality_certificates WHERE order_id = $1 OR task_id = $2 OR machine_id = $3 OR batch_lot_id = $4 OR issued_by = $5 OR approved_by = $5`,
			[]any{a.order, a.task, a.machine, a.lot, a.user}},
		{`SELECT COUNT(*) FROM inspections WHERE order_id = $1 OR task_id = $2 OR machine_id = $3 OR inspector_id = $4 OR certificate_id = $5`,
			[]any{a.order, a.task, a.machine, a.user, a.cert}},
		{`SELECT COUNT(*) FROM batch_lots WHERE order_id = $1`, []any{a.order}},
		{`SELECT COUNT(*) FROM product_genealogy WHERE product_definition_id = $1 OR order_item_id = $2 OR task_id = $3 OR inspection_id = $4 OR certificate_id = $5 OR sealed_by = $6`,
			[]any{a.product, a.item, a.task, a.inspection, a.cert, a.user}},
		{`SELECT COUNT(*) FROM work_instructions WHERE product_definition_id = $1`, []any{a.product}},
		{`SELECT COUNT(*) FROM task_work_instructions WHERE task_id = $1`, []any{a.task}},
	} {
		var n int
		require.NoError(t, db.RunInTenantTx(context.Background(), r.pool, r.B.ID.String(), func(ctx context.Context) error {
			return r.tdb.QueryRowContext(ctx, q.sql, q.args...).Scan(&n)
		}))
		assert.Zero(t, n, q.sql)
	}

	// The same writes with B's own rows succeed.
	for _, c := range writes(own) {
		if c.ownFails {
			continue
		}
		res := r.do(r.B, c.method, c.path, c.body)
		assert.Truef(t, isSuccess(res.Code), "B %s %s with its own rows %v: %d %s", c.method, c.path, c.body, res.Code, res.Body)
	}
}

// TestTenantIsolation_APIKeyPath: a tenant's API key reads only that tenant.
func TestTenantIsolation_APIKeyPath(t *testing.T) {
	r := newIsolationRig(t)
	aMachine := r.aMachine()
	bMachine := r.createID(r.B, "/v1/machines", map[string]any{"name": "B printer", "code": "B-1", "type": "3d_printer"})

	// The key needs a machine read scope: the route scope matrix refuses
	// /v1/machines to the default read:events/read:feeds key.
	res := r.do(r.B, http.MethodPost, "/v1/api-keys", map[string]any{"name": "B key", "scopes": []string{"pravara-mes:read"}})
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
