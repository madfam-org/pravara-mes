package costevents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Fact is one material row of job_consumption (migration 033) with what the
// builder needs from its job and order.
type Fact struct {
	ID           uuid.UUID
	JobKind      string
	JobID        uuid.UUID
	MaterialCode string
	// RegistryCode is the material registry code MaterialCode resolves to
	// (RFC 0058 ruling I3; "" when unregistered).
	RegistryCode string
	// Quantity is the stored NUMERIC as text (never a float).
	Quantity   string
	Unit       string
	Basis      string // actual | estimate
	OccurredAt time.Time
	CreatedAt  time.Time
	// EngagementSlug is the order's current engagement ("" = none).
	EngagementSlug string
	// JobStatus is the dispatch's or print job's current status; "" when the
	// job is a task or its row no longer exists.
	JobStatus string
}

// Line is every material fact of one job and material code: one cost event.
type Line struct {
	JobKind      string
	JobID        uuid.UUID
	MaterialCode string
	Facts        []Fact
}

// Options are the settings the builder applies to every event.
type Options struct {
	CostClass string
	// Start: lines whose completion is earlier are never sent.
	Start time.Time
}

// Built is what a line becomes.
type Built struct {
	EventID      string
	JobKind      string
	JobID        uuid.UUID
	MaterialCode string
	// Event is nil when there is nothing to store (see Skip).
	Event *Event
	// Skip says why Event is nil.
	Skip string
	// Problems are contract violations: the event is stored as rejected and
	// never sent.
	Problems []string
}

// Skip reasons.
const (
	SkipBeforeStart     = "before_start_date"
	SkipZeroNeverSent   = "zero_quantity_never_sent"
	SkipWithdrawnUnsent = "job_not_completed_never_sent"
	SkipNoFacts         = "no_facts"
)

// EventID is the deterministic cost.event.v1 id of a job and material: the
// same line always gets the same id, so a correction re-sends it and Dhanam
// reports it `updated`, never a second event. The material code is hashed
// because it is free text and the id alphabet is restricted.
func EventID(jobKind string, jobID uuid.UUID, materialCode string) string {
	sum := sha256.Sum256([]byte(materialCode))
	return "pravara.consumption." + jobKind + "." + jobID.String() + "." + hex.EncodeToString(sum[:8])
}

// effective is the latest actual, else the latest estimate (the same rule
// stock relief uses, docs/operations/job-consumption.md). facts are ordered
// by created_at, id.
func effective(facts []Fact) *Fact {
	var actual, estimate *Fact
	for i := range facts {
		switch facts[i].Basis {
		case BasisActual:
			actual = &facts[i]
		case BasisEstimate:
			estimate = &facts[i]
		}
	}
	if actual != nil {
		return actual
	}
	return estimate
}

// NormalizeQuantity renders a NUMERIC text as cost.event.v1 wants it: no
// trailing zeros, no trailing point ("12.5000" → "12.5", "5.0000" → "5").
func NormalizeQuantity(q string) string {
	q = strings.TrimSpace(q)
	if strings.Contains(q, ".") {
		q = strings.TrimRight(strings.TrimRight(q, "0"), ".")
	}
	if q == "" || q == "-" || q == "-0" {
		return "0"
	}
	return q
}

// jobWithdrawn reports a dispatch or print job that is no longer completed:
// its completion was withdrawn, so the consumption recorded at completion is
// cancelled in the ledger. A task (no status) or a deleted job row keeps its
// facts: what the job consumed was consumed.
func jobWithdrawn(f Fact) bool {
	return f.JobStatus != "" && f.JobStatus != "completed"
}

// Build turns a line into its event. prev is the event last stored for the
// line (nil when none): a line whose quantity became zero, or whose job is no
// longer completed, re-sends prev's id as `cancelled` (Dhanam's status) when
// something was stored before, and stores nothing otherwise.
func Build(line Line, prev *Event, opts Options) Built {
	b := Built{EventID: EventID(line.JobKind, line.JobID, line.MaterialCode),
		JobKind: line.JobKind, JobID: line.JobID, MaterialCode: line.MaterialCode}
	facts := append([]Fact(nil), line.Facts...)
	sort.SliceStable(facts, func(i, j int) bool {
		if !facts[i].CreatedAt.Equal(facts[j].CreatedAt) {
			return facts[i].CreatedAt.Before(facts[j].CreatedAt)
		}
		return facts[i].ID.String() < facts[j].ID.String()
	})
	eff := effective(facts)
	if eff == nil {
		b.Skip = SkipNoFacts
		return b
	}
	// occurred_at is the job's completion: the earliest fact (an operator's
	// later actual is stamped when reported, and must not move the period).
	occurred := facts[0].OccurredAt
	for _, f := range facts[1:] {
		if f.OccurredAt.Before(occurred) {
			occurred = f.OccurredAt
		}
	}
	if !opts.Start.IsZero() && occurred.Before(opts.Start) {
		b.Skip = SkipBeforeStart
		return b
	}
	slug := ""
	for i := len(facts) - 1; i >= 0 && slug == ""; i-- {
		slug = facts[i].EngagementSlug
	}

	ev := Event{
		Schema: Schema, ID: b.EventID, Source: Source, Kind: KindConsumption, Status: StatusActive,
		OccurredAt: occurred.UTC().Format(time.RFC3339), CostClass: opts.CostClass, Basis: eff.Basis,
		Quantity: NormalizeQuantity(eff.Quantity), Unit: eff.Unit,
		Item:     Item{MaterialCode: itemCode(line.MaterialCode, facts)},
		Evidence: &Evidence{JobID: line.JobID.String()},
	}
	if slug != "" {
		// Never guessed: only the order's own engagement.
		ev.Attribution = &Attribution{Engagement: "eng:" + slug}
	}

	switch {
	case zeroQuantity.MatchString(ev.Quantity):
		if prev == nil {
			b.Skip = SkipZeroNeverSent
			return b
		}
		cancelled := *prev
		cancelled.Status = StatusCancelled
		ev = cancelled
	case jobWithdrawn(*eff):
		if prev == nil {
			b.Skip = SkipWithdrawnUnsent
			return b
		}
		ev.Status = StatusCancelled
	}
	b.Event = &ev
	b.Problems = Validate(ev)
	return b
}

// itemCode is the code sent as item.material_code: the material registry
// code the line's code resolves to (Pravara owns the registry, RFC 0058
// ruling I3), else the code as recorded. The event id stays keyed by the
// recorded code, so a line rebuilt after its code was registered or
// aliased (within the emitter's lookback) re-sends the same event with the
// registry code, which Dhanam reports as updated.
func itemCode(recorded string, facts []Fact) string {
	for i := len(facts) - 1; i >= 0; i-- {
		if facts[i].RegistryCode != "" {
			return facts[i].RegistryCode
		}
	}
	return recorded
}

// Body is the event's wire JSON and its SHA-256 (hex). The same event always
// marshals to the same bytes (fixed field order), so an unchanged line keeps
// its hash and is never re-sent.
func Body(e Event) ([]byte, string, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}
