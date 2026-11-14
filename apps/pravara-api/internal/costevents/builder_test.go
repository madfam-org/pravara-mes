package costevents_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/costevents"
)

func mustUUID(s string) uuid.UUID { return uuid.MustParse(s) }

var (
	jobA      = mustUUID("0b6b1f9e-0d7e-4c55-9a43-7c6f2b1d2e10")
	completed = time.Date(2026, 11, 3, 18, 20, 0, 0, time.UTC)
	opts      = costevents.Options{CostClass: "cogs", Start: time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC)}
)

func fact(basis, qty, unit string, created time.Time) costevents.Fact {
	return costevents.Fact{ID: uuid.New(), JobKind: "dispatch", JobID: jobA, MaterialCode: "FIL-PLA-BLK",
		Quantity: qty, Unit: unit, Basis: basis, OccurredAt: completed, CreatedAt: created, JobStatus: "completed"}
}

func line(facts ...costevents.Fact) costevents.Line {
	return costevents.Line{JobKind: "dispatch", JobID: jobA, MaterialCode: "FIL-PLA-BLK", Facts: facts}
}

func wire(t *testing.T, e *costevents.Event) map[string]any {
	t.Helper()
	require.NotNil(t, e)
	raw, _, err := costevents.Body(*e)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

func TestBuildEstimateOnly(t *testing.T) {
	b := costevents.Build(line(fact("estimate", "5.2000", "g", completed)), nil, opts)
	require.Empty(t, b.Problems)
	m := wire(t, b.Event)
	assert.Equal(t, "estimate", m["basis"])
	assert.Equal(t, "5.2", m["quantity"])
	assert.Equal(t, "g", m["unit"])
	assert.Equal(t, "active", m["status"])
	assert.Equal(t, "2026-11-03T18:20:00Z", m["occurred_at"])
	assert.Equal(t, map[string]any{"material_code": "FIL-PLA-BLK"}, m["item"])
	assert.Equal(t, map[string]any{"job_id": jobA.String()}, m["evidence"])
	assert.NotContains(t, m, "attribution", "no engagement on the order: omitted, never guessed")
	for _, k := range costevents.ForbiddenForQuantityOnly {
		assert.NotContains(t, m, k)
	}
	assert.Empty(t, costevents.ValidateWire(m))
}

func TestBuildActualWinsAndKeepsCompletionDate(t *testing.T) {
	est := fact("estimate", "5.2000", "g", completed)
	act := fact("actual", "6.0000", "g", completed.Add(48*time.Hour))
	act.OccurredAt = completed.Add(48 * time.Hour) // an operator report is stamped when reported
	act.EngagementSlug = "taller-demo"
	b := costevents.Build(line(act, est), nil, opts)
	m := wire(t, b.Event)
	assert.Equal(t, "actual", m["basis"])
	assert.Equal(t, "6", m["quantity"])
	assert.Equal(t, "2026-11-03T18:20:00Z", m["occurred_at"], "the period is the job's completion")
	assert.Equal(t, map[string]any{"engagement": "eng:taller-demo"}, m["attribution"])
	assert.Equal(t, costevents.EventID("dispatch", jobA, "FIL-PLA-BLK"), m["id"])
}

func TestCorrectionKeepsIDAndChangesBody(t *testing.T) {
	est := fact("estimate", "5.2000", "g", completed)
	b1 := costevents.Build(line(est), nil, opts)
	b2 := costevents.Build(line(est, fact("actual", "6.0000", "g", completed.Add(time.Hour))), b1.Event, opts)
	assert.Equal(t, b1.EventID, b2.EventID)
	_, sha1, _ := costevents.Body(*b1.Event)
	_, sha2, _ := costevents.Body(*b2.Event)
	assert.NotEqual(t, sha1, sha2)
	_, again, _ := costevents.Body(*costevents.Build(line(est), nil, opts).Event)
	assert.Equal(t, sha1, again, "the same line always hashes the same")
}

func TestBuildBeforeStartDateIsSkipped(t *testing.T) {
	f := fact("actual", "6", "g", completed)
	f.OccurredAt = opts.Start.Add(-time.Second)
	b := costevents.Build(line(f), nil, opts)
	assert.Nil(t, b.Event)
	assert.Equal(t, costevents.SkipBeforeStart, b.Skip)
}

func TestZeroQuantity(t *testing.T) {
	zero := fact("actual", "0.0000", "g", completed)
	b := costevents.Build(line(zero), nil, opts)
	assert.Nil(t, b.Event, "nothing to send and nothing sent before")
	assert.Equal(t, costevents.SkipZeroNeverSent, b.Skip)

	prev := costevents.Build(line(fact("estimate", "5.2", "g", completed)), nil, opts).Event
	b = costevents.Build(line(fact("estimate", "5.2", "g", completed), fact("actual", "0.0000", "g", completed.Add(time.Hour))), prev, opts)
	m := wire(t, b.Event)
	assert.Equal(t, "cancelled", m["status"], "an operator's zero withdraws what was sent")
	assert.Equal(t, "5.2", m["quantity"], "a cancelled event repeats the last valid body (Dhanam needs a non-zero quantity)")
	assert.Empty(t, b.Problems)
}

func TestWithdrawnJob(t *testing.T) {
	f := fact("estimate", "5.2", "g", completed)
	f.JobStatus = "failed"
	b := costevents.Build(line(f), nil, opts)
	assert.Nil(t, b.Event)
	assert.Equal(t, costevents.SkipWithdrawnUnsent, b.Skip)

	prev := costevents.Build(line(fact("estimate", "5.2", "g", completed)), nil, opts).Event
	b = costevents.Build(line(f), prev, opts)
	assert.Equal(t, "cancelled", wire(t, b.Event)["status"])

	// Completed again: the same id becomes active again.
	f.JobStatus = "completed"
	assert.Equal(t, "active", wire(t, costevents.Build(line(f), b.Event, opts).Event)["status"])

	// A task, or a deleted job row (no status), keeps its consumption.
	f.JobStatus = ""
	assert.Equal(t, "active", wire(t, costevents.Build(line(f), nil, opts).Event)["status"])
}

func TestContractProblemsAreReported(t *testing.T) {
	f := fact("actual", "6", "g", completed)
	f.MaterialCode = strings.Repeat("M", 70)
	l := line(f)
	l.MaterialCode = f.MaterialCode
	b := costevents.Build(l, nil, opts)
	require.NotNil(t, b.Event)
	assert.NotEmpty(t, b.Problems)
}

func TestNormalizeQuantity(t *testing.T) {
	for in, want := range map[string]string{"12.5000": "12.5", "5.0000": "5", "0.0000": "0", "100": "100", "0.0001": "0.0001", "": "0"} {
		assert.Equal(t, want, costevents.NormalizeQuantity(in), in)
	}
}
