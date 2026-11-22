package api

// A terminal's device token against PostgreSQL (opt-in with
// PRAVARA_TEST_DATABASE_URL, see newScopeRig): the real middleware, a token
// shaped as Janua's device token (client_credentials, client_kind "device",
// scope pravara-mes:terminal). It reads orders, tasks, a lot by its number
// and the event history; every operator act and every other route is 403.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (r *scopeRig) deviceToken(t *testing.T, scope string) string {
	return r.machineToken(t, scope, func(c jwt.MapClaims) {
		c["sub"] = "service-account:jnc_terminal_test"
		c["client_id"] = "jnc_terminal_test"
		c["client_kind"] = "device"
	})
}

func TestTerminalDeviceTokenAgainstPostgres(t *testing.T) {
	rig := newScopeRig(t)

	var orderID, taskID, materialID, itemID, lotID uuid.UUID
	require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO orders (tenant_id, customer_name, external_id)
		VALUES ($1, 'Terminal test', 'term-1') RETURNING id`, rig.tenantID).Scan(&orderID))
	require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO tasks (tenant_id, order_id, title, status)
		VALUES ($1, $2, 'Print bracket', 'queued') RETURNING id`, rig.tenantID, orderID).Scan(&taskID))
	_, err := rig.sqlDB.Exec(`INSERT INTO order_items (order_id, product_name, quantity, unit_price, cad_file_url)
		VALUES ($1, 'Bracket', 2, 10.5, 'https://files.example.test/bracket.step')`, orderID)
	require.NoError(t, err)
	require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO material_registry (tenant_id, code, name, category, base_unit)
		VALUES ($1, 'FIL-PLA-BLK', 'PLA negro', 'filament', 'g') RETURNING id`, rig.tenantID).Scan(&materialID))
	require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO inventory_items (tenant_id, sku, name, unit, quantity_on_hand, material_id)
		VALUES ($1, 'SKU-PLA-BLK', 'PLA negro 1 kg', 'g', 1000, $2) RETURNING id`, rig.tenantID, materialID).Scan(&itemID))
	require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO batch_lots (tenant_id, lot_number, product_name, quantity, unit, inventory_item_id)
		VALUES ($1, 'LOT-TERM-0001', 'PLA negro', 1000, 'g', $2) RETURNING id`, rig.tenantID, itemID).Scan(&lotID))

	device := rig.deviceToken(t, "pravara-mes:terminal")
	task := "/v1/tasks/" + taskID.String()

	for _, path := range []string{
		"/v1/orders",
		"/v1/orders/" + orderID.String(),
		"/v1/orders/" + orderID.String() + "/items",
		"/v1/tasks?order_id=" + orderID.String(),
		"/v1/tasks/board",
		task,
		task + "/work-instructions",
		"/v1/events?types=task.*",
		"/v1/events/types",
	} {
		w := rig.do(http.MethodGet, path, device, nil)
		assert.Equal(t, http.StatusOK, w.Code, "GET %s: %s", path, w.Body.String())
	}

	// Orders come in the terminal's narrow view: no customer fields.
	for _, path := range []string{"/v1/orders", "/v1/orders/" + orderID.String(), "/v1/orders/" + orderID.String() + "/items"} {
		w := rig.do(http.MethodGet, path, device, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		for _, field := range []string{"customer_name", "customer_email", "shipping_address", "total_amount", "currency", "metadata", "unit_price", "cad_file_url"} {
			assert.NotContains(t, w.Body.String(), `"`+field+`"`, "GET %s", path)
		}
		assert.NotContains(t, w.Body.String(), "Terminal test", "GET %s", path)
	}
	full := rig.do(http.MethodGet, "/v1/orders/"+orderID.String(), rig.machineToken(t, "pravara-mes:read", nil), nil)
	require.Equal(t, http.StatusOK, full.Code, full.Body.String())
	assert.Contains(t, full.Body.String(), `"customer_name":"Terminal test"`, "a pravara-mes:read caller is unchanged")

	// The lot lookup answers the lot's stock item and registry material.
	w := rig.do(http.MethodGet, "/v1/quality/batches/lookup?lot_number="+url.QueryEscape("LOT-TERM-0001"), device, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var lot map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &lot))
	assert.Equal(t, lotID.String(), lot["id"])
	assert.Equal(t, "SKU-PLA-BLK", lot["sku"])
	assert.Equal(t, "FIL-PLA-BLK", lot["material_code"])
	w = rig.do(http.MethodGet, "/v1/quality/batches/lookup?lot_number=LOT-NONE", device, nil)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	// Every operator act, and every other route, refuses the device token.
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, task + "/move", map[string]any{"status": "in_progress", "position": 1}},
		{http.MethodPost, task + "/consumption", map[string]any{"material_code": "FIL-PLA-BLK", "quantity": 5, "unit": "g"}},
		{http.MethodPost, task + "/work-instructions/" + uuid.NewString() + "/acknowledge", map[string]any{"step_number": 1}},
		{http.MethodPatch, task, map[string]any{"title": "Renamed by a device"}},
		{http.MethodPost, "/v1/orders", map[string]any{"customer_name": "Not created"}},
		{http.MethodPost, "/v1/quality/batches", map[string]any{"lot_number": "LOT-X", "product_name": "X", "quantity": 1, "unit": "g"}},
		{http.MethodGet, "/v1/quality/batches", nil},
		{http.MethodGet, "/v1/inventory", nil},
		{http.MethodGet, "/v1/machines", nil},
		{http.MethodGet, "/v1/api-keys", nil},
	} {
		w := rig.do(tc.method, tc.path, device, tc.body)
		assert.Equal(t, http.StatusForbidden, w.Code, "%s %s: %s", tc.method, tc.path, w.Body.String())
	}
	var status string
	require.NoError(t, rig.sqlDB.QueryRow(`SELECT status FROM tasks WHERE id = $1`, taskID).Scan(&status))
	assert.Equal(t, "queued", status, "a device token moved nothing")

	// pravara-mes:read does not open the terminal's task reads (PV-10).
	w = rig.do(http.MethodGet, task, rig.machineToken(t, "pravara-mes:read", nil), nil)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

// Event history for a terminal (owner ruling 2026-11-22, "Yes, remove
// them"): the customer's name is removed from order events on the list and
// the by-id read; a pravara-mes:read caller still gets it. Task metadata for
// a terminal carries no CAD file URL.
func TestTerminalNarrowedEventsAndTaskMetadataAgainstPostgres(t *testing.T) {
	rig := newScopeRig(t)
	orderID := uuid.New()
	const customer = "Taller Ejemplo SA"
	payloads := map[string]string{
		"order.created": `{"id":"e-created","type":"order.created","tenant_id":"` + rig.tenantID.String() + `","data":{"entity_id":"` +
			orderID.String() + `","entity_type":"order","name":"` + customer + `","metadata":{"item_count":1}}}`,
		"order.status_changed": `{"id":"e-status","type":"order.status_changed","tenant_id":"` + rig.tenantID.String() + `","data":{"order_id":"` +
			orderID.String() + `","new_status":"in_progress","customer_name":"` + customer + `"}}`,
	}
	ids := map[string]uuid.UUID{}
	for typ, payload := range payloads {
		var id uuid.UUID
		require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO event_outbox (id, tenant_id, event_type, channel_namespace, payload)
			VALUES (gen_random_uuid(), $1, $2, 'orders', $3::jsonb) RETURNING id`, rig.tenantID, typ, payload).Scan(&id))
		ids[typ] = id
	}
	var taskID uuid.UUID
	require.NoError(t, rig.sqlDB.QueryRow(`INSERT INTO tasks (tenant_id, title, status, metadata) VALUES ($1, 'Bracket', 'queued',
		'{"source":"order_decomposition","product_sku":"SOP-1","cad_file_url":"https://files.example.test/bracket.step"}') RETURNING id`,
		rig.tenantID).Scan(&taskID))

	device := rig.deviceToken(t, "pravara-mes:terminal")
	reader := rig.machineToken(t, "pravara-mes:read", nil)
	routes := []string{"/v1/events?types=order.*", "/v1/events/" + ids["order.created"].String(), "/v1/events/" + ids["order.status_changed"].String()}
	for _, path := range routes {
		w := rig.do(http.MethodGet, path, device, nil)
		require.Equal(t, http.StatusOK, w.Code, "GET %s: %s", path, w.Body.String())
		assert.NotContains(t, w.Body.String(), customer, "a terminal reads no customer name: GET %s", path)
		assert.Contains(t, w.Body.String(), orderID.String(), "the order id stays: GET %s", path)

		w = rig.do(http.MethodGet, path, reader, nil)
		require.Equal(t, http.StatusOK, w.Code, "GET %s: %s", path, w.Body.String())
		assert.Contains(t, w.Body.String(), customer, "a pravara-mes:read caller is unchanged: GET %s", path)
	}

	for _, path := range []string{"/v1/tasks/" + taskID.String(), "/v1/tasks", "/v1/tasks/board"} {
		w := rig.do(http.MethodGet, path, device, nil)
		require.Equal(t, http.StatusOK, w.Code, "GET %s: %s", path, w.Body.String())
		assert.NotContains(t, w.Body.String(), "cad_file_url", "GET %s", path)
		assert.NotContains(t, w.Body.String(), "files.example.test", "GET %s", path)
		assert.Contains(t, w.Body.String(), `"product_sku":"SOP-1"`, "GET %s", path)
	}
	w := rig.do(http.MethodGet, "/v1/tasks/"+taskID.String(), rig.userToken(t, "operator"), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "cad_file_url", "a person's task read is unchanged")
}
