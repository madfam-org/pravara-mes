// Package costeventstest provides an httptest stand-in for Dhanam's
// POST /v1/cost-events, for tests only. It checks the bearer token's
// audience (minted by dispatchfakes.Janua), validates every event against
// costevents.ValidateWire (all-or-nothing, as Dhanam does) and is
// idempotent on (source, id): an identical replay is `unchanged`, a changed
// body is `updated`.
package costeventstest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/costevents"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatchfakes"
)

// Dhanam is the fake.
type Dhanam struct {
	Server *httptest.Server

	mu sync.Mutex
	// Stored is the last body stored per event id.
	Stored map[string]map[string]any
	// Outcomes lists every per-event outcome in order (created, updated,
	// unchanged).
	Outcomes []string
	// Requests counts POSTs received (any answer).
	Requests int
	// Tokens lists the bearer tokens seen, in order.
	Tokens []string
	// Script, when non-empty, answers the next requests with these HTTP
	// statuses before normal processing (0 = process normally).
	Script []int
	// RejectIDs are refused with a 400 naming their index.
	RejectIDs map[string]bool
}

// NewDhanam starts the fake.
func NewDhanam(t testing.TB) *Dhanam {
	d := &Dhanam{Stored: map[string]map[string]any{}, RejectIDs: map[string]bool{}}
	d.Server = httptest.NewServer(http.HandlerFunc(d.handle))
	t.Cleanup(d.Server.Close)
	return d
}

// URL is the API origin (events go to URL + /v1/cost-events).
func (d *Dhanam) URL() string { return d.Server.URL }

// Endpoint is the cost-events URL.
func (d *Dhanam) Endpoint() string { return d.Server.URL + "/v1/cost-events" }

// Count returns Requests under the lock.
func (d *Dhanam) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.Requests
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type indexError struct {
	Index  int      `json:"index"`
	ID     string   `json:"id,omitempty"`
	Errors []string `json:"errors"`
}

func (d *Dhanam) handle(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if r.URL.Path != "/v1/cost-events" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	d.Requests++
	d.Tokens = append(d.Tokens, r.Header.Get("Authorization"))
	if len(d.Script) > 0 {
		status := d.Script[0]
		d.Script = d.Script[1:]
		if status != 0 {
			writeJSON(w, status, map[string]string{"message": http.StatusText(status)})
			return
		}
	}
	if dispatchfakes.Audience(r) != costevents.Audience {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "wrong audience"})
		return
	}
	var body struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Events) == 0 || len(body.Events) > costevents.MaxBatch {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errors": []indexError{{Index: -1, Errors: []string{"body must be one cost.event.v1 object or { events: [...] }"}}}})
		return
	}
	var errs []indexError
	for i, ev := range body.Events {
		id, _ := ev["id"].(string)
		problems := costevents.ValidateWire(ev)
		if d.RejectIDs[id] {
			problems = append(problems, "quantity must be a decimal string (refused by the fake)")
		}
		if len(problems) > 0 {
			errs = append(errs, indexError{Index: i, ID: id, Errors: problems})
		}
	}
	if len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errors": errs})
		return
	}
	type result struct {
		ID      string `json:"id"`
		Outcome string `json:"outcome"`
	}
	var results []result
	counts := map[string]int{}
	for _, ev := range body.Events {
		id := ev["id"].(string)
		outcome := "created"
		if prev, ok := d.Stored[id]; ok {
			a, _ := json.Marshal(prev)
			b, _ := json.Marshal(ev)
			outcome = "updated"
			if string(a) == string(b) {
				outcome = "unchanged"
			}
		}
		d.Stored[id] = ev
		d.Outcomes = append(d.Outcomes, outcome)
		counts[outcome]++
		results = append(results, result{ID: id, Outcome: outcome})
	}
	writeJSON(w, http.StatusOK, map[string]any{"received": len(body.Events), "created": counts["created"],
		"updated": counts["updated"], "unchanged": counts["unchanged"], "results": results})
}
