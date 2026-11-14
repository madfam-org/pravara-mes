package api

// Owner ruling (2026-11-14): the inventory record writes need a person
// with the admin or inventory_manager role. A plain signed-in user gets 403
// error forbidden; machine tokens and API keys stay refused. Reads, and the
// shop-floor consumption report, stay open to the tenant. Opt-in with
// PRAVARA_TEST_DATABASE_URL.

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInventoryWritesNeedAnInventoryRole(t *testing.T) {
	r := newDispatchRig(t)
	manager := r.userToken(t, "inventory_manager")
	operator := r.userToken(t, "operator")
	machine := r.machineToken(t, "pravara-mes:read", nil)
	key := r.apiKey(t, "pravara-mes:read")

	refused := func(method, path string, body any) {
		t.Helper()
		w := r.do(method, path, operator, body)
		require.Equalf(t, http.StatusForbidden, w.Code, "operator %s %s: %s", method, path, w.Body.String())
		assert.Contains(t, w.Body.String(), `"error":"forbidden"`)
		assert.Equalf(t, http.StatusForbidden, r.do(method, path, machine, body).Code, "machine token %s %s", method, path)
		assert.Equalf(t, http.StatusForbidden, r.do(method, path, key, body).Code, "scoped API key %s %s", method, path)
	}

	// Registry: create, update, alias, conversion, link a stock item.
	material := map[string]any{"code": "FIL-ROLE-1", "name": "role test", "category": "filament", "base_unit": "g"}
	refused(http.MethodPost, "/v1/inventory/materials", material)
	mID := r.call(t, manager, http.MethodPost, "/v1/inventory/materials", material, http.StatusCreated)["id"].(string)
	refused(http.MethodPatch, "/v1/inventory/materials/"+mID, map[string]any{"name": "renamed"})
	r.call(t, manager, http.MethodPatch, "/v1/inventory/materials/"+mID, map[string]any{"name": "renamed"}, http.StatusOK)
	refused(http.MethodPost, "/v1/inventory/materials/"+mID+"/aliases", map[string]any{"alias": "role-alias"})
	r.call(t, manager, http.MethodPost, "/v1/inventory/materials/"+mID+"/aliases", map[string]any{"alias": "role-alias"}, http.StatusCreated)
	conv := map[string]any{"from_unit": "spool", "to_unit": "g", "factor": 1000}
	refused(http.MethodPost, "/v1/inventory/materials/"+mID+"/conversions", conv)
	r.call(t, manager, http.MethodPost, "/v1/inventory/materials/"+mID+"/conversions", conv, http.StatusCreated)

	// Linking a stock item on the pre-existing item routes: the link needs
	// the role; the rest of those writes stay as they were.
	item := r.call(t, operator, http.MethodPost, "/v1/inventory", map[string]any{"sku": "ROLE-ITEM", "name": "item", "unit": "g"},
		http.StatusCreated)["id"].(string)
	assert.Equal(t, http.StatusForbidden, r.do(http.MethodPatch, "/v1/inventory/"+item, operator, map[string]any{"material_id": mID}).Code)
	assert.Equal(t, http.StatusForbidden, r.do(http.MethodPost, "/v1/inventory", operator,
		map[string]any{"sku": "ROLE-ITEM-2", "name": "item", "unit": "g", "material_id": mID}).Code)
	r.call(t, operator, http.MethodPatch, "/v1/inventory/"+item, map[string]any{"name": "renamed by an operator"}, http.StatusOK)
	r.call(t, manager, http.MethodPatch, "/v1/inventory/"+item, map[string]any{"material_id": mID}, http.StatusOK)

	// Receipts.
	receipt := map[string]any{"material_id": mID, "quantity": 1, "unit": "spool", "invoice_status": "no_invoice", "idempotency_key": "role-1"}
	refused(http.MethodPost, "/v1/inventory/receipts", receipt)
	r.call(t, manager, http.MethodPost, "/v1/inventory/receipts", receipt, http.StatusCreated)

	// Counts: open (and the opening count), lines, close.
	refused(http.MethodPost, "/v1/inventory/counts", map[string]any{"is_opening": true})
	countID := r.call(t, manager, http.MethodPost, "/v1/inventory/counts", map[string]any{"is_opening": true}, http.StatusCreated)["id"].(string)
	line := map[string]any{"material_id": mID, "counted_quantity": 1000, "unit": "g"}
	refused(http.MethodPost, "/v1/inventory/counts/"+countID+"/lines", line)
	r.call(t, manager, http.MethodPost, "/v1/inventory/counts/"+countID+"/lines", line, http.StatusCreated)
	refused(http.MethodPost, "/v1/inventory/counts/"+countID+"/close", nil)
	r.call(t, manager, http.MethodPost, "/v1/inventory/counts/"+countID+"/close", nil, http.StatusOK)

	// Reads and the shop-floor report stay open to a plain user.
	for _, path := range []string{"/v1/inventory/materials", "/v1/inventory/materials/" + mID, "/v1/inventory/receipts",
		"/v1/inventory/counts", "/v1/inventory/counts/" + countID, "/v1/inventory/conversion-gaps",
		"/v1/inventory/materials/unlinked", "/v1/jobs/consumption/coverage"} {
		assert.Equalf(t, http.StatusOK, r.do(http.MethodGet, path, operator, nil).Code, "GET %s", path)
	}
	task := r.seedTask(t, boxSpecs)
	r.call(t, operator, http.MethodPost, "/v1/tasks/"+task.String()+"/consumption",
		map[string]any{"material_code": "FIL-ROLE-1", "quantity": 5, "unit": "g"}, http.StatusCreated)

	// An admin passes too.
	r.call(t, r.userToken(t, "admin"), http.MethodPost, "/v1/inventory/counts", nil, http.StatusCreated)
}
