package costevents_test

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/costevents"
)

// validWire is a quantity-only consumption event exactly as Pravara sends it.
func validWire() map[string]any {
	return map[string]any{
		"schema": "cost.event.v1", "id": "pravara.consumption.task.0b6b1f9e-0d7e-4c55-9a43-7c6f2b1d2e10.1a2b3c4d5e6f7a8b",
		"source": "pravara", "kind": "consumption", "status": "active", "occurred_at": "2026-11-03T18:20:00Z",
		"cost_class": "cogs", "basis": "actual", "quantity": "12.5", "unit": "g",
		"item":        map[string]any{"material_code": "FIL-PLA-BLK"},
		"attribution": map[string]any{"engagement": "eng:taller-demo"},
		"evidence":    map[string]any{"job_id": "0b6b1f9e-0d7e-4c55-9a43-7c6f2b1d2e10"},
	}
}

func TestValidWireEventPasses(t *testing.T) {
	assert.Empty(t, costevents.ValidateWire(validWire()))
}

// The rules of Dhanam contract v1.1 for quantity-only consumption from
// source pravara (cost-event.contract.ts, cost-event.v11-rules.ts). A drift
// in either side fails here.
func TestQuantityOnlyRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(m map[string]any)
		want   string
	}{
		{"amount present", func(m map[string]any) { m["amount_minor"] = 1250 }, "carries no amount_minor"},
		{"fx present", func(m map[string]any) { m["fx"] = map[string]any{"rate": "17.1", "date": "2026-11-03", "source": "x"} }, "carries no fx"},
		{"unit value present", func(m map[string]any) { m["unit_value_minor"] = 30 }, "carries no unit_value_minor"},
		{"taxes present", func(m map[string]any) { m["taxes"] = map[string]any{} }, "carries no taxes"},
		{"currency USD", func(m map[string]any) { m["currency"] = "USD" }, "carries no currency"},
		{"zero quantity", func(m map[string]any) { m["quantity"] = "0.000" }, "non-zero quantity"},
		{"float quantity", func(m map[string]any) { m["quantity"] = 12.5 }, "decimal string"},
		{"seven decimals", func(m map[string]any) { m["quantity"] = "1.1234567" }, "decimal string"},
		{"no unit", func(m map[string]any) { delete(m, "unit") }, "unit must be"},
		{"no item code", func(m map[string]any) { m["item"] = map[string]any{"description": "PLA"} }, "item.material_code or item.sku"},
		{"no item", func(m map[string]any) { delete(m, "item") }, "item.material_code or item.sku"},
		{"material code too long", func(m map[string]any) { m["item"] = map[string]any{"material_code": strings.Repeat("x", 65)} }, "at most 64"},
		{"unknown item key", func(m map[string]any) { m["item"] = map[string]any{"material_code": "A", "lot": "L1"} }, "unknown field item.lot"},
		{"engagement without prefix", func(m map[string]any) { m["attribution"] = map[string]any{"engagement": "taller-demo"} }, "eng:<slug>"},
		{"engagement upper case", func(m map[string]any) { m["attribution"] = map[string]any{"engagement": "eng:Taller"} }, "eng:<slug>"},
		{"impossible date", func(m map[string]any) { m["occurred_at"] = "2026-02-30T10:00:00Z" }, "real calendar date"},
		{"no offset", func(m map[string]any) { m["occurred_at"] = "2026-11-03T18:20:00" }, "with an offset"},
		{"unknown top-level", func(m map[string]any) { m["engagement"] = "eng:x" }, "unknown field engagement"},
		{"other source", func(m map[string]any) { m["source"] = "karafiel" }, "source must be"},
		{"other kind", func(m map[string]any) { m["kind"] = "purchase" }, "kind must be"},
		{"bad status", func(m map[string]any) { m["status"] = "reopened" }, "status must be"},
		{"bad id", func(m map[string]any) { m["id"] = "job 1/PLA" }, "id must be"},
		{"no cost class", func(m map[string]any) { delete(m, "cost_class") }, "cost_class"},
		{"bad basis", func(m map[string]any) { m["basis"] = "slicer" }, "basis must be"},
		{"unknown evidence", func(m map[string]any) { m["evidence"] = map[string]any{"job": "x"} }, "unknown field evidence.job"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validWire()
			tc.mutate(m)
			errs := costevents.ValidateWire(m)
			require.NotEmpty(t, errs)
			assert.Contains(t, strings.Join(errs, "; "), tc.want)
		})
	}
	m := validWire()
	m["currency"] = "MXN" // Dhanam accepts MXN or absent
	assert.Empty(t, costevents.ValidateWire(m))
	m = validWire()
	m["status"] = "cancelled"
	assert.Empty(t, costevents.ValidateWire(m))
}

// Pinned to Dhanam's TOP_LEVEL_KEYS (contract v1.1).
func TestTopLevelKeysMatchDhanam(t *testing.T) {
	want := []string{"schema", "id", "source", "kind", "occurred_at", "amount_minor", "currency", "fx", "cost_class",
		"basis", "quantity", "unit", "unit_value_minor", "taxes", "item", "attribution", "evidence", "status", "allocates"}
	var got []string
	for k := range costevents.AllowedTopLevelKeys {
		got = append(got, k)
	}
	sort.Strings(got)
	sort.Strings(want)
	assert.Equal(t, want, got)
	assert.Equal(t, "1.1", costevents.ContractVersion)
	assert.Equal(t, "pravara", costevents.Source)
	assert.Equal(t, "dhanam-cost-events:write", costevents.WriteScope, "one colon: Janua registers namespace:action")
}

// The Event type cannot carry money: none of its JSON fields is a money
// field, whatever the values.
func TestEventTypeHasNoMoneyFields(t *testing.T) {
	forbidden := map[string]bool{}
	for _, k := range costevents.ForbiddenForQuantityOnly {
		forbidden[k] = true
	}
	typ := reflect.TypeOf(costevents.Event{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		assert.False(t, forbidden[name], "Event has a money field %q", name)
		assert.True(t, costevents.AllowedTopLevelKeys[name], "Event field %q is not in cost.event.v1", name)
	}
	raw, err := json.Marshal(costevents.Event{})
	require.NoError(t, err)
	for k := range forbidden {
		assert.NotContains(t, string(raw), `"`+k+`"`)
	}
}

func TestEventIDIsDeterministic(t *testing.T) {
	job := mustUUID("0b6b1f9e-0d7e-4c55-9a43-7c6f2b1d2e10")
	a := costevents.EventID("dispatch", job, "FIL-PLA-BLK")
	assert.Equal(t, a, costevents.EventID("dispatch", job, "FIL-PLA-BLK"))
	assert.Equal(t, "pravara.consumption.dispatch.0b6b1f9e-0d7e-4c55-9a43-7c6f2b1d2e10.", a[:len(a)-16])
	assert.NotEqual(t, a, costevents.EventID("dispatch", job, "FIL-PETG-BLK"))
	assert.NotEqual(t, a, costevents.EventID("print_job", job, "FIL-PLA-BLK"))
	assert.NotEqual(t, a, costevents.EventID("dispatch", mustUUID("6f1c0c3e-5f43-4f0b-9b1e-2d7a3c8e9f01"), "FIL-PLA-BLK"))
	// Free-text codes never leak into the id alphabet.
	odd := costevents.EventID("task", job, "PLA negro / 1.75 mm ✓")
	m := validWire()
	m["id"] = odd
	assert.Empty(t, costevents.ValidateWire(m))
}

func TestErrorCode(t *testing.T) {
	assert.Equal(t, "amount_minor", costevents.ErrorCode("amount_minor must be a non-zero integer"))
	assert.Equal(t, "unknown_field", costevents.ErrorCode("unknown field taxes.x"))
	assert.Equal(t, "duplicate_id", costevents.ErrorCode("duplicate (source, id) in the same batch"))
	assert.Equal(t, "quantityonly", costevents.ErrorCode("Quantity-only consumption needs a unit"))
	assert.Equal(t, "unspecified", costevents.ErrorCode(""))
}
