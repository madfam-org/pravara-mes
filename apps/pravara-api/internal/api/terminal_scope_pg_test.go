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
