// Package costevents sends job consumption to Dhanam's cost ledger as
// quantity-only cost.event.v1 `consumption` events (RFC 0050; owner ruling
// C12: Pravara never values consumption, Dhanam does).
//
// The wire rules below mirror Dhanam's contract v1.1
// (apps/api/src/modules/cost-events/cost-event.contract.ts and
// cost-event.v11-rules.ts in madfam-org/dhanam) for the one shape Pravara
// sends. Every event is checked against them before it is stored as pending,
// so a drift between the builder and the contract fails in tests here, and
// an event Dhanam would refuse is never sent.
package costevents

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Contract constants (Dhanam cost.event.v1, contract revision 1.1).
const (
	Schema          = "cost.event.v1"
	ContractVersion = "1.1"
	Source          = "pravara"
	KindConsumption = "consumption"
	StatusActive    = "active"
	StatusCancelled = "cancelled"
	BasisActual     = "actual"
	BasisEstimate   = "estimate"
	// Scope and audience of the Janua client Dhanam's writer grant binds.
	TokenScope = "dhanam-cost-events:write"
	Audience   = "dhanam-api"
	// MaxBatch is Dhanam's batch limit.
	MaxBatch = 500
)

var (
	idPattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	rfc3339Pattern    = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(\.\d{1,9})?(?:Z|[+-](\d{2}):(\d{2}))$`)
	quantityPattern   = regexp.MustCompile(`^-?\d{1,12}(\.\d{1,6})?$`)
	zeroQuantity      = regexp.MustCompile(`^-?0+(\.0+)?$`)
	engagementPattern = regexp.MustCompile(`^eng:([a-z0-9][a-z0-9._-]{0,63})$`)
	costClasses       = map[string]bool{"cogs": true, "opex": true, "capex": true, "financing": true}
	bases             = map[string]bool{"actual": true, "standard": true, "estimate": true}
)

// Item names what was consumed. Dhanam values quantity-only consumption by
// material_code or sku (one of them is required).
type Item struct {
	MaterialCode string `json:"material_code,omitempty"`
	SKU          string `json:"sku,omitempty"`
}

// Attribution carries the engagement (`eng:<slug>`), only when the order
// names one.
type Attribution struct {
	Engagement string `json:"engagement,omitempty"`
}

// Evidence points back at the Pravara job.
type Evidence struct {
	JobID string `json:"job_id,omitempty"`
}

// Event is the quantity-only consumption event Pravara sends. It has no
// amount_minor, currency, fx, unit_value_minor or taxes fields at all:
// Pravara cannot send money even by mistake.
type Event struct {
	Schema      string       `json:"schema"`
	ID          string       `json:"id"`
	Source      string       `json:"source"`
	Kind        string       `json:"kind"`
	Status      string       `json:"status"`
	OccurredAt  string       `json:"occurred_at"`
	CostClass   string       `json:"cost_class"`
	Basis       string       `json:"basis"`
	Quantity    string       `json:"quantity"`
	Unit        string       `json:"unit"`
	Item        Item         `json:"item"`
	Attribution *Attribution `json:"attribution,omitempty"`
	Evidence    *Evidence    `json:"evidence,omitempty"`
}

// AllowedTopLevelKeys are the top-level fields cost.event.v1 accepts
// (Dhanam TOP_LEVEL_KEYS). Anything else is a 400.
var AllowedTopLevelKeys = map[string]bool{
	"schema": true, "id": true, "source": true, "kind": true, "occurred_at": true,
	"amount_minor": true, "currency": true, "fx": true, "cost_class": true, "basis": true,
	"quantity": true, "unit": true, "unit_value_minor": true, "taxes": true, "item": true,
	"attribution": true, "evidence": true, "status": true, "allocates": true,
}

// ForbiddenForQuantityOnly are fields a quantity-only consumption event never
// carries: Dhanam values it (C12, D1). Pravara adds none of them, ever.
var ForbiddenForQuantityOnly = []string{"amount_minor", "currency", "fx", "unit_value_minor", "taxes", "allocates"}

var (
	allowedItemKeys        = map[string]int{"sku": 64, "material_code": 64, "sat_clave_prod_serv": 8, "description": 200}
	allowedAttributionKeys = map[string]bool{"engagement": true, "order": true, "sku": true, "client_ref": true, "product": true}
	allowedEvidenceKeys    = map[string]bool{"cfdi_uuid": true, "cfdi_total_minor": true, "transaction_id": true,
		"job_id": true, "time_entry_id": true, "allocation_id": true, "related_cfdi_uuid": true, "payment_method": true}
)

// Validate checks e, as it goes on the wire, against the v1.1 rules for a
// quantity-only consumption event from source pravara. It returns every
// violation (empty = Dhanam accepts it).
func Validate(e Event) []string {
	raw, err := json.Marshal(e)
	if err != nil {
		return []string{"event does not marshal: " + err.Error()}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return []string{"event does not round-trip: " + err.Error()}
	}
	return ValidateWire(m)
}

// ValidateWire checks one decoded wire object (see Validate).
func ValidateWire(m map[string]any) []string {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	for k := range m {
		if !AllowedTopLevelKeys[k] {
			add("unknown field %s", k)
		}
	}
	str := func(k string) (string, bool) { s, ok := m[k].(string); return s, ok }

	if s, _ := str("schema"); s != Schema {
		add("schema must be %q", Schema)
	}
	if id, ok := str("id"); !ok || !idPattern.MatchString(id) {
		add("id must be 1-128 characters of letters, digits, . _ : -")
	}
	if s, _ := str("source"); s != Source {
		add("source must be %q", Source)
	}
	if s, _ := str("kind"); s != KindConsumption {
		add("kind must be %q", KindConsumption)
	}
	if v, present := m["status"]; present {
		if s, _ := v.(string); s != StatusActive && s != StatusCancelled {
			add("status must be active or cancelled")
		}
	}
	if s, ok := str("occurred_at"); !ok || !strictDateTime(s) {
		add("occurred_at must be an RFC 3339 datetime with an offset, on a real calendar date")
	}
	// Quantity-only: no money of its own (C12). Dhanam accepts currency MXN
	// or absent; Pravara always leaves it absent.
	for _, k := range ForbiddenForQuantityOnly {
		if v, present := m[k]; present && v != nil {
			if k == "currency" && v == "MXN" {
				continue
			}
			add("quantity-only consumption carries no %s: Dhanam values it", k)
		}
	}
	if s, _ := str("cost_class"); !costClasses[s] {
		add("cost_class must be one of cogs, opex, capex, financing")
	}
	if v, present := m["basis"]; present {
		if s, _ := v.(string); !bases[s] {
			add("basis must be one of actual, standard, estimate")
		}
	}
	q, ok := str("quantity")
	switch {
	case !ok || !quantityPattern.MatchString(q):
		add("quantity must be a decimal string (up to 12 integer and 6 decimal digits)")
	case zeroQuantity.MatchString(q):
		add("quantity-only consumption needs a non-zero quantity and a unit")
	}
	if u, ok := str("unit"); !ok || strings.TrimSpace(u) == "" || len(u) > 32 {
		add("unit must be a non-empty string of at most 32 characters")
	}
	item, _ := m["item"].(map[string]any)
	if item == nil {
		add("quantity-only consumption needs item.material_code or item.sku to be valued")
	} else {
		for k, v := range item {
			max, known := allowedItemKeys[k]
			s, isStr := v.(string)
			switch {
			case !known:
				add("unknown field item.%s", k)
			case !isStr || strings.TrimSpace(s) == "" || len(s) > max:
				add("item.%s must be a non-empty string of at most %d characters", k, max)
			}
		}
		mc, _ := item["material_code"].(string)
		sku, _ := item["sku"].(string)
		if mc == "" && sku == "" {
			add("quantity-only consumption needs item.material_code or item.sku to be valued")
		}
	}
	if v, present := m["attribution"]; present {
		a, _ := v.(map[string]any)
		if a == nil {
			add("attribution must be an object")
		}
		for k, val := range a {
			s, isStr := val.(string)
			switch {
			case !allowedAttributionKeys[k]:
				add("unknown field attribution.%s", k)
			case !isStr || strings.TrimSpace(s) == "" || len(s) > 128:
				add("attribution.%s must be a non-empty string of at most 128 characters", k)
			case k == "engagement" && !engagementPattern.MatchString(s):
				add("attribution.engagement must be eng:<slug> (lowercase slug)")
			}
		}
	}
	if v, present := m["evidence"]; present {
		ev, _ := v.(map[string]any)
		if ev == nil {
			add("evidence must be an object")
		}
		for k, val := range ev {
			s, isStr := val.(string)
			switch {
			case !allowedEvidenceKeys[k]:
				add("unknown field evidence.%s", k)
			case k == "job_id" && (!isStr || strings.TrimSpace(s) == "" || len(s) > 128):
				add("evidence.job_id must be a non-empty string of at most 128 characters")
			}
		}
	}
	return errs
}

// strictDateTime is Dhanam's parseStrictDateTime: RFC 3339 with an offset,
// every field in range, on a real calendar day (2026-02-30 is refused, never
// rolled to 2 March).
func strictDateTime(s string) bool {
	m := rfc3339Pattern.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	var y, mo, d, h, mi, sec, offH, offM int
	_, _ = fmt.Sscanf(m[1]+" "+m[2]+" "+m[3]+" "+m[4]+" "+m[5]+" "+m[6], "%d %d %d %d %d %d", &y, &mo, &d, &h, &mi, &sec)
	if m[8] != "" {
		_, _ = fmt.Sscanf(m[8]+" "+m[9], "%d %d", &offH, &offM)
	}
	if mo < 1 || mo > 12 || d < 1 || h > 23 || mi > 59 || sec > 59 || offH > 23 || offM > 59 {
		return false
	}
	// time.Date normalises an impossible day into the next month.
	return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC).Day() == d
}
