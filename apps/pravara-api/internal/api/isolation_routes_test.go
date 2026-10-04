package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isoCase is one tenant-owned resource: tenant A creates it, tenant B must
// neither see nor change it through any route.
type isoCase struct {
	name   string
	setup  func(r *isolationRig) string // creates A's row, returns its id
	lists  []string                     // list routes B calls (fmt with %s = id)
	item   string                       // GET route for one row (fmt with %s = id); "" if none
	patch  any                          // PATCH body on item; nil if no PATCH route
	delete bool                         // DELETE route on item
	extra  []call                       // other B calls on A's row (fmt with %s = id)
	// markers are extra strings that only A's data can produce (for
	// aggregate routes that do not echo ids).
	markers []string
}

type call struct {
	method, path string
	body         any
}

const pwned = "pwned-by-tenant-b"

func (r *isolationRig) aMachine() string {
	return r.createID(r.A, "/v1/machines", map[string]any{
		"name": "A secret printer", "code": "A-" + uuid.NewString()[:8], "type": "3d_printer",
	})
}

func isolationCases() []isoCase {
	return []isoCase{
		{name: "orders", setup: func(r *isolationRig) string {
			return r.createID(r.A, "/v1/orders", map[string]any{"customer_name": "A secret customer"})
		}, lists: []string{"/v1/orders", "/v1/feeds/crm/orders"}, item: "/v1/orders/%s",
			patch: map[string]any{"customer_name": pwned}, delete: true,
			extra: []call{
				{http.MethodGet, "/v1/orders/%s/items", nil},
				{http.MethodPost, "/v1/orders/%s/items", map[string]any{"product_name": pwned, "quantity": 1}},
				{http.MethodGet, "/v1/feeds/crm/orders/%s/status", nil},
				{http.MethodGet, "/v1/feeds/crm/orders/%s/timeline", nil},
			}},
		{name: "order items", setup: func(r *isolationRig) string {
			order := r.createID(r.A, "/v1/orders", map[string]any{"customer_name": "A items"})
			r.createID(r.A, "/v1/orders/"+order+"/items", map[string]any{"product_name": "A secret item", "quantity": 1})
			return order
		}, extra: []call{{http.MethodGet, "/v1/orders/%s/items", nil}}},
		{name: "tasks", setup: func(r *isolationRig) string {
			return r.createID(r.A, "/v1/tasks", map[string]any{"title": "A secret task"})
		}, lists: []string{"/v1/tasks", "/v1/tasks/board"}, item: "/v1/tasks/%s",
			patch: map[string]any{"title": pwned}, delete: true,
			extra: []call{
				{http.MethodPost, "/v1/tasks/%s/move", map[string]any{"status": "in_progress", "position": 0}},
				{http.MethodPost, "/v1/tasks/%s/assign", map[string]any{}},
				{http.MethodGet, "/v1/tasks/%s/work-instructions", nil},
			}},
		{name: "machines", setup: func(r *isolationRig) string { return r.aMachine() },
			lists: []string{"/v1/machines"}, item: "/v1/machines/%s",
			patch: map[string]any{"name": pwned}, delete: true,
			extra: []call{
				{http.MethodGet, "/v1/machines/%s/telemetry", nil},
				{http.MethodPost, "/v1/machines/%s/heartbeat", map[string]any{}},
				{http.MethodGet, "/v1/machines/%s/maintenance", nil},
			}},
		{name: "quality certificates", setup: func(r *isolationRig) string {
			return r.createID(r.A, "/v1/quality/certificates", map[string]any{
				"certificate_number": "CERT-" + uuid.NewString()[:8], "type": "coc", "title": "A secret cert"})
		}, lists: []string{"/v1/quality/certificates"}, item: "/v1/quality/certificates/%s",
			patch: map[string]any{"title": pwned}, delete: true},
		{name: "inspections", setup: func(r *isolationRig) string {
			return r.createID(r.A, "/v1/quality/inspections", map[string]any{
				"inspection_number": "INS-" + uuid.NewString()[:8], "type": "final"})
		}, lists: []string{"/v1/quality/inspections"}, item: "/v1/quality/inspections/%s",
			patch: map[string]any{"notes": pwned}, delete: true,
			extra: []call{{http.MethodPost, "/v1/quality/inspections/%s/complete", map[string]any{"result": "pass"}}}},
		{name: "batch lots", setup: func(r *isolationRig) string {
			return r.createID(r.A, "/v1/quality/batches", map[string]any{
				"lot_number": "LOT-" + uuid.NewString()[:8], "product_name": "A secret resin", "quantity": 10, "unit": "kg"})
		}, lists: []string{"/v1/quality/batches"}, item: "/v1/quality/batches/%s",
			patch: map[string]any{"product_name": pwned}, delete: true},
		{name: "maintenance schedules", setup: func(r *isolationRig) string {
			return r.createID(r.A, "/v1/maintenance/schedules", map[string]any{
				"machine_id": r.aMachine(), "name": "A secret PM", "trigger_type": "calendar"})
		}, lists: []string{"/v1/maintenance/schedules"}, item: "/v1/maintenance/schedules/%s",
			patch: map[string]any{"name": pwned}, delete: true},
		{name: "maintenance work orders", setup: func(r *isolationRig) string {
			id := uuid.NewString()
			r.seed(r.A.ID, `INSERT INTO maintenance_work_orders (id, tenant_id, machine_id, work_order_number, title, status)
				VALUES ($1, $2, $3, $4, 'A secret work order', 'scheduled')`, id, r.A.ID, r.aMachine(), "WO-"+id[:8])
			return id
		}, lists: []string{"/v1/maintenance/work-orders"}, item: "/v1/maintenance/work-orders/%s",
			patch: map[string]any{"title": pwned},
			extra: []call{{http.MethodPost, "/v1/maintenance/work-orders/%s/complete", map[string]any{"notes": pwned}}}},
		{name: "products and BOM", setup: func(r *isolationRig) string {
			id := r.createID(r.A, "/v1/products", map[string]any{
				"sku": "SKU-" + uuid.NewString()[:8], "name": "A secret bracket", "version": "1.0", "category": "3d_print"})
			r.createID(r.A, "/v1/products/"+id+"/bom/items", map[string]any{"material_name": "A secret PLA", "quantity": 1.5, "unit": "kg"})
			return id
		}, lists: []string{"/v1/products"}, item: "/v1/products/%s",
			patch: map[string]any{"name": pwned}, delete: true,
			extra: []call{
				{http.MethodGet, "/v1/products/%s/bom", nil},
				{http.MethodPost, "/v1/products/%s/bom/items", map[string]any{"material_name": pwned, "quantity": 1, "unit": "kg"}},
			}},
		{name: "genealogy", setup: func(r *isolationRig) string {
			return r.createID(r.A, "/v1/genealogy", map[string]any{"serial_number": "A-secret-serial"})
		}, lists: []string{"/v1/genealogy"}, item: "/v1/genealogy/%s",
			patch: map[string]any{"status": "in_progress"},
			extra: []call{
				{http.MethodGet, "/v1/genealogy/%s/tree", nil},
				{http.MethodPost, "/v1/genealogy/%s/seal", map[string]any{}},
			}},
		{name: "work instructions", setup: func(r *isolationRig) string {
			return r.createID(r.A, "/v1/work-instructions", map[string]any{"title": "A secret setup", "version": "1.0", "category": "setup"})
		}, lists: []string{"/v1/work-instructions"}, item: "/v1/work-instructions/%s",
			patch: map[string]any{"title": pwned}, delete: true},
		{name: "inventory", setup: func(r *isolationRig) string {
			id := uuid.NewString()
			r.seed(r.A.ID, `INSERT INTO inventory_items (id, tenant_id, sku, name, category, quantity_on_hand, reorder_point)
				VALUES ($1, $2, $3, 'A secret spool', 'filament', 1, 10)`, id, r.A.ID, "INV-"+id[:8])
			return id
		}, lists: []string{"/v1/inventory", "/v1/inventory/low-stock"}, item: "/v1/inventory/%s",
			patch: map[string]any{"name": pwned},
			extra: []call{{http.MethodPost, "/v1/inventory/%s/adjust", map[string]any{"quantity": 5, "type": "adjustment"}}}},
		{name: "webhook subscriptions", setup: func(r *isolationRig) string {
			return r.createID(r.A, "/v1/webhooks/subscriptions", map[string]any{
				"name": "A secret hook", "url": "https://example.test/a", "secret": "a-secret", "event_types": []string{"order.created"}})
		}, lists: []string{"/v1/webhooks/subscriptions"}, item: "/v1/webhooks/subscriptions/%s",
			patch: map[string]any{"name": pwned}, delete: true,
			extra: []call{{http.MethodGet, "/v1/webhooks/subscriptions/%s/deliveries", nil}}},
		{name: "api keys", setup: func(r *isolationRig) string {
			return r.createID(r.A, "/v1/api-keys", map[string]any{"name": "A secret key"})
		}, lists: []string{"/v1/api-keys"}, extra: []call{{http.MethodDelete, "/v1/api-keys/%s", nil}}},
		{name: "outbox events", setup: func(r *isolationRig) string {
			id := uuid.NewString()
			r.seed(r.A.ID, `INSERT INTO event_outbox (id, tenant_id, event_type, channel_namespace, payload)
				VALUES ($1, $2, 'task.completed', 'tasks', '{"marker":"A secret event"}')`, id, r.A.ID)
			return id
		}, markers: []string{"task.completed"}, lists: []string{"/v1/events", "/v1/events/types", "/v1/feeds/social/highlights", "/v1/feeds/social/milestones"},
			item: "/v1/events/%s"},
		{name: "telemetry", setup: func(r *isolationRig) string {
			m := r.aMachine()
			res := r.do(r.A, http.MethodPost, "/v1/telemetry/batch", map[string]any{
				"records": []map[string]any{{"machine_id": m, "metric_type": "temperature", "value": 21.5}}})
			require.Equal(r.t, http.StatusCreated, res.Code, string(res.Body))
			return m
		}, lists: []string{"/v1/telemetry", "/v1/telemetry/latest?machine_id=%s", "/v1/telemetry/aggregated?machine_id=%s&metric_type=temperature"},
			markers: []string{`"avg":21.5`}},
		{name: "oee and spc", setup: func(r *isolationRig) string {
			m := r.aMachine()
			lim := uuid.NewString()
			r.seed(r.A.ID, `INSERT INTO oee_snapshots (tenant_id, machine_id, snapshot_date, oee) VALUES ($1, $2, CURRENT_DATE, 0.5)`, r.A.ID, m)
			r.seed(r.A.ID, `INSERT INTO spc_control_limits (id, tenant_id, machine_id, metric_type, mean, stddev, ucl, lcl, sample_count, sample_start, sample_end)
				VALUES ($1, $2, $3, 'temperature', 20, 1, 23, 17, 30, NOW() - INTERVAL '1 day', NOW())`, lim, r.A.ID, m)
			r.seed(r.A.ID, `INSERT INTO spc_violations (tenant_id, control_limit_id, machine_id, violation_type, metric_type, value)
				VALUES ($1, $2, $3, 'above_ucl', 'temperature', 30)`, r.A.ID, lim, m)
			return m
		}, lists: []string{"/v1/analytics/oee", "/v1/analytics/oee/summary", "/v1/analytics/spc/violations?machine_id=%s",
			"/v1/analytics/spc/limits?machine_id=%s", "/v1/feeds/social/stats"},
			markers: []string{`"average_oee":0.5`, `"oee":0.5`}},
		{name: "factory layout", setup: func(r *isolationRig) string {
			id := uuid.NewString()
			r.seed(r.A.ID, `INSERT INTO factory_layouts (id, tenant_id, name) VALUES ($1, $2, 'A secret layout')`, id, r.A.ID)
			return id
		}, lists: []string{"/v1/layouts/active"}},
	}
}

func isSuccess(code int) bool { return code >= 200 && code < 300 }

// TestTenantIsolation_Routes: every list/get/update/delete route gives tenant
// B nothing of tenant A's, and B's writes against A's rows are rejected.
func TestTenantIsolation_Routes(t *testing.T) {
	r := newIsolationRig(t)
	for _, tc := range isolationCases() {
		t.Run(tc.name, func(t *testing.T) {
			r.t = t
			id := tc.setup(r)
			leak := func(res rigResponse) bool {
				body := string(res.Body)
				// Routes that echo the requested id back are not a leak.
				for _, echo := range []string{"machine_id", "task_id"} {
					body = strings.ReplaceAll(body, fmt.Sprintf(`"%s":"%s"`, echo, id), "")
				}
				if strings.Contains(body, id) || strings.Contains(body, r.A.ID.String()) ||
					strings.Contains(body, "A secret") {
					return true
				}
				for _, m := range tc.markers {
					if strings.Contains(body, m) {
						return true
					}
				}
				return false
			}

			for _, l := range tc.lists {
				path := l
				if strings.Contains(l, "%s") {
					path = fmt.Sprintf(l, id)
				}
				// Positive control: A sees its own row through the same route.
				if a := r.do(r.A, http.MethodGet, path, nil); isSuccess(a.Code) {
					assert.Truef(t, leak(a), "control: A must see its row via %s: %s", path, a.Body)
				}
				res := r.do(r.B, http.MethodGet, path, nil)
				assert.Falsef(t, leak(res), "B saw A's data via GET %s (%d): %s", path, res.Code, res.Body)
			}

			if tc.item != "" {
				path := fmt.Sprintf(tc.item, id)
				a := r.do(r.A, http.MethodGet, path, nil)
				require.Truef(t, isSuccess(a.Code), "control: A GET %s: %d %s", path, a.Code, a.Body)
				res := r.do(r.B, http.MethodGet, path, nil)
				assert.Falsef(t, isSuccess(res.Code), "B GET %s returned %d", path, res.Code)
				assert.False(t, leak(res), "B GET %s leaked: %s", path, res.Body)
			}

			if tc.patch != nil {
				path := fmt.Sprintf(tc.item, id)
				res := r.do(r.B, http.MethodPatch, path, tc.patch)
				assert.Falsef(t, isSuccess(res.Code), "B PATCH %s returned %d: %s", path, res.Code, res.Body)
				a := r.do(r.A, http.MethodGet, path, nil)
				assert.NotContains(t, string(a.Body), pwned, "A's row was modified by B")
			}

			for _, c := range tc.extra {
				path := fmt.Sprintf(c.path, id)
				res := r.do(r.B, c.method, path, c.body)
				if c.method == http.MethodGet {
					assert.Falsef(t, leak(res), "B %s %s leaked (%d): %s", c.method, path, res.Code, res.Body)
				} else {
					assert.Falsef(t, isSuccess(res.Code), "B %s %s returned %d: %s", c.method, path, res.Code, res.Body)
				}
			}

			if tc.delete {
				path := fmt.Sprintf(tc.item, id)
				res := r.do(r.B, http.MethodDelete, path, nil)
				assert.Falsef(t, isSuccess(res.Code), "B DELETE %s returned %d", path, res.Code)
				a := r.do(r.A, http.MethodGet, path, nil)
				assert.Truef(t, isSuccess(a.Code), "A's row must survive B's DELETE: %d", a.Code)
				assert.NotContains(t, string(a.Body), pwned)
			}
		})
	}
	r.t = t
}
