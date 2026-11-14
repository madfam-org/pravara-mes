package costevents_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/costevents"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/costevents/costeventstest"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatchfakes"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
)

// memStore is an in-memory Store with the same semantics as PGStore. The
// rows outlive any emitter, as the table does.
type memStore struct {
	mu    sync.Mutex
	lines []costevents.Line
	rows  map[string]*memRow
}

type memRow struct {
	built                       costevents.Built
	body                        []byte
	sha, status, lastErr        string
	attempts                    int
	next, leased                time.Time
	deliveredSHA, dhanamOutcome string
}

func newMemStore() *memStore { return &memStore{rows: map[string]*memRow{}} }

func (s *memStore) RecentLines(_ context.Context, since time.Time) ([]costevents.Line, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []costevents.Line
	for _, l := range s.lines {
		for _, f := range l.Facts {
			if !f.CreatedAt.Before(since) {
				out = append(out, l)
				break
			}
		}
	}
	return out, nil
}

func (s *memStore) Stored(_ context.Context, ids []string) (map[string]costevents.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]costevents.Event{}
	for _, id := range ids {
		if r, ok := s.rows[id]; ok {
			var e costevents.Event
			if err := json.Unmarshal(r.body, &e); err != nil {
				return nil, err
			}
			out[id] = e
		}
	}
	return out, nil
}

func (s *memStore) Upsert(_ context.Context, b costevents.Built, body []byte, sha, status, lastError string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.rows[b.EventID]; ok && r.sha == sha {
		return false, nil
	}
	prev := s.rows[b.EventID]
	r := &memRow{built: b, body: body, sha: sha, status: status, lastErr: lastError, next: now}
	if prev != nil {
		r.deliveredSHA, r.dhanamOutcome, r.leased = prev.deliveredSHA, prev.dhanamOutcome, prev.leased
	}
	s.rows[b.EventID] = r
	return true, nil
}

func (s *memStore) Claim(_ context.Context, limit int, now time.Time, lease time.Duration) ([]costevents.Claimed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for id, r := range s.rows {
		if r.status == costevents.RowPending && !r.next.After(now) && !r.leased.After(now) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := s.rows[ids[i]], s.rows[ids[j]]
		if !a.next.Equal(b.next) {
			return a.next.Before(b.next)
		}
		return ids[i] < ids[j]
	})
	if len(ids) > limit {
		ids = ids[:limit]
	}
	var out []costevents.Claimed
	for _, id := range ids {
		r := s.rows[id]
		r.leased = now.Add(lease)
		out = append(out, costevents.Claimed{EventID: id, Body: r.body, BodySHA256: r.sha, Attempts: r.attempts})
	}
	return out, nil
}

func (s *memStore) with(id, sha string, fn func(r *memRow)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.rows[id]; ok {
		if r.sha == sha {
			fn(r)
		}
		r.leased = time.Time{} // the POST is over, whatever the body
	}
}

func (s *memStore) MarkDelivered(_ context.Context, id, sha, outcome string, _ time.Time) error {
	s.with(id, sha, func(r *memRow) {
		r.status, r.deliveredSHA, r.dhanamOutcome, r.attempts, r.lastErr = costevents.RowDelivered, sha, outcome, 0, ""
	})
	return nil
}

func (s *memStore) MarkRejected(_ context.Context, id, sha, reason string, _ time.Time) error {
	s.with(id, sha, func(r *memRow) { r.status, r.lastErr = costevents.RowRejected, reason })
	return nil
}

func (s *memStore) Release(_ context.Context, id, sha string, now time.Time) error {
	s.with(id, sha, func(r *memRow) {
		if r.status == costevents.RowPending {
			r.next = now
		}
	})
	return nil
}

func (s *memStore) Retry(_ context.Context, id, sha, reason string, next, _ time.Time) error {
	s.with(id, sha, func(r *memRow) {
		if r.status == costevents.RowPending {
			r.attempts++
			r.next, r.lastErr = next, reason
		}
	})
	return nil
}

func (s *memStore) Counts(context.Context) (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int{}
	for _, r := range s.rows {
		out[r.status]++
	}
	return out, nil
}

func (s *memStore) row(id string) memRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.rows[id]
}

func (s *memStore) addLine(l costevents.Line) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.lines {
		if s.lines[i].JobID == l.JobID && s.lines[i].MaterialCode == l.MaterialCode {
			s.lines[i] = l
			return
		}
	}
	s.lines = append(s.lines, l)
}

// rig wires an emitter to the fake Janua and Dhanam with a controllable clock.
type rig struct {
	store  *memStore
	janua  *dispatchfakes.Janua
	dhanam *costeventstest.Dhanam
	log    *logrus.Logger
	hook   *test.Hook
	now    time.Time
	batch  int
}

func newRig(t *testing.T) *rig {
	log, hook := test.NewNullLogger()
	r := &rig{store: newMemStore(), log: log, hook: hook, now: completed.Add(time.Hour), batch: 100,
		janua: dispatchfakes.NewJanua(t, dispatchfakes.Client{ID: "jnc_pravara_cost", Secret: "jns_test_only",
			Scope: costevents.WriteScope, Audience: costevents.Audience}),
		dhanam: costeventstest.NewDhanam(t)}
	return r
}

func (r *rig) emitter(dryRun bool) *costevents.Emitter {
	var poster costevents.Poster
	if !dryRun {
		tokens := machineclients.NewTokenSource(machineclients.Credentials{Name: "dhanam-cost-events",
			TokenURL: r.janua.TokenURL(), ClientID: "jnc_pravara_cost", ClientSecret: "jns_test_only",
			Scope: costevents.WriteScope, Audience: costevents.Audience}, nil)
		poster = costevents.NewClient(r.dhanam.Endpoint(), tokens, nil)
	}
	e := costevents.NewEmitter(r.store, poster, nil, costevents.Settings{Options: opts,
		Lookback: 62 * 24 * time.Hour, BatchSize: r.batch, DryRun: dryRun}, r.log)
	e.SetClock(func() time.Time { return r.now })
	return e
}

func (r *rig) run(t *testing.T, e *costevents.Emitter) costevents.DeliverReport {
	t.Helper()
	_, err := e.Sweep(context.Background())
	require.NoError(t, err)
	rep, err := e.Deliver(context.Background())
	require.NoError(t, err)
	return rep
}

func idOf(l costevents.Line) string { return costevents.EventID(l.JobKind, l.JobID, l.MaterialCode) }

func TestDeliverAndIdempotentReplay(t *testing.T) {
	r := newRig(t)
	l := line(fact("estimate", "5.2000", "g", completed))
	r.store.addLine(l)
	e := r.emitter(false)

	rep := r.run(t, e)
	assert.Equal(t, 1, rep.Delivered)
	assert.Equal(t, []string{"created"}, r.dhanam.Outcomes)
	row := r.store.row(idOf(l))
	assert.Equal(t, costevents.RowDelivered, row.status)
	assert.Equal(t, "created", row.dhanamOutcome)
	assert.Equal(t, "Bearer "+costevents.Audience+"|jnc_pravara_cost|1", r.dhanam.Tokens[0])

	// Nothing changed: nothing is sent again.
	r.now = r.now.Add(10 * time.Minute)
	assert.Equal(t, 0, r.run(t, e).Batches)
	assert.Equal(t, 1, r.dhanam.Count())

	// An operator's actual corrects the line: the SAME id is re-sent.
	r.store.addLine(line(fact("estimate", "5.2000", "g", completed), fact("actual", "6.0000", "g", r.now)))
	assert.Equal(t, 1, r.run(t, e).Delivered)
	assert.Equal(t, []string{"created", "updated"}, r.dhanam.Outcomes)
	assert.Len(t, r.dhanam.Stored, 1)
	assert.Equal(t, "6", r.dhanam.Stored[idOf(l)]["quantity"])
	assert.Equal(t, "actual", r.dhanam.Stored[idOf(l)]["basis"])
	assert.Equal(t, 1, r.janua.Issued["jnc_pravara_cost"], "the token is cached")
}

func TestTransientFailureRetriesWithBackoffAndNeverDrops(t *testing.T) {
	r := newRig(t)
	l := line(fact("actual", "6", "g", completed))
	r.store.addLine(l)
	r.dhanam.Script = []int{http.StatusServiceUnavailable, http.StatusServiceUnavailable}
	e := r.emitter(false)

	rep := r.run(t, e)
	assert.Equal(t, costevents.OutcomeTransient, rep.Stopped)
	row := r.store.row(idOf(l))
	assert.Equal(t, costevents.RowPending, row.status)
	assert.Equal(t, 1, row.attempts)
	assert.Equal(t, "http_503", row.lastErr)
	assert.Equal(t, r.now.Add(30*time.Second), row.next)

	// Not due yet: nothing is sent.
	r.now = r.now.Add(10 * time.Second)
	assert.Equal(t, 0, r.run(t, e).Batches)
	// Due: second failure backs off 2 minutes.
	r.now = r.now.Add(time.Minute)
	r.run(t, e)
	row = r.store.row(idOf(l))
	assert.Equal(t, 2, row.attempts)
	assert.Equal(t, r.now.Add(2*time.Minute), row.next)
	// Dhanam is back: delivered, same id, once.
	r.now = r.now.Add(3 * time.Minute)
	assert.Equal(t, 1, r.run(t, e).Delivered)
	assert.Equal(t, costevents.RowDelivered, r.store.row(idOf(l)).status)
	assert.Equal(t, []string{"created"}, r.dhanam.Outcomes)
}

func TestBackoffSchedule(t *testing.T) {
	assert.Equal(t, 30*time.Second, costevents.Backoff(0))
	assert.Equal(t, 30*time.Second, costevents.Backoff(1))
	assert.Equal(t, time.Hour, costevents.Backoff(4))
	assert.Equal(t, 6*time.Hour, costevents.Backoff(5))
	assert.Equal(t, 6*time.Hour, costevents.Backoff(50), "never dropped: capped, not dead")
}

func TestRejectedEventIsIsolated(t *testing.T) {
	r := newRig(t)
	good := line(fact("actual", "6", "g", completed))
	badFact := fact("actual", "3", "g", completed)
	badFact.JobID = mustUUID("6f1c0c3e-5f43-4f0b-9b1e-2d7a3c8e9f01")
	bad := costevents.Line{JobKind: "dispatch", JobID: badFact.JobID, MaterialCode: "FIL-PLA-BLK", Facts: []costevents.Fact{badFact}}
	r.store.addLine(good)
	r.store.addLine(bad)
	r.dhanam.RejectIDs[idOf(bad)] = true
	e := r.emitter(false)

	rep := r.run(t, e)
	assert.Equal(t, 2, rep.Batches, "the refused event is set aside and the rest is sent at once")
	assert.Equal(t, 1, rep.Rejected)
	assert.Equal(t, 1, rep.Delivered)
	assert.Equal(t, costevents.RowDelivered, r.store.row(idOf(good)).status)
	rej := r.store.row(idOf(bad))
	assert.Equal(t, costevents.RowRejected, rej.status)
	assert.Equal(t, "dhanam: quantity", rej.lastErr, "codes only, never values")
	// A rejected row waits for a new body; it is not retried as is.
	r.now = r.now.Add(7 * time.Hour)
	assert.Equal(t, 0, r.run(t, e).Batches)
}

func TestLocalContractViolationIsNeverSent(t *testing.T) {
	r := newRig(t)
	f := fact("actual", "6", "g", completed)
	f.MaterialCode = "MATERIAL-CODE-LONGER-THAN-SIXTY-FOUR-CHARACTERS-WHICH-DHANAM-REFUSES-0001"
	l := costevents.Line{JobKind: "dispatch", JobID: jobA, MaterialCode: f.MaterialCode, Facts: []costevents.Fact{f}}
	r.store.addLine(l)
	e := r.emitter(false)
	rep := r.run(t, e)
	assert.Equal(t, 0, rep.Batches)
	assert.Equal(t, 0, r.dhanam.Count())
	row := r.store.row(idOf(l))
	assert.Equal(t, costevents.RowRejected, row.status)
	assert.Contains(t, row.lastErr, "contract: item.material_code")
	assert.NotEmpty(t, r.hook.AllEntries())
}

func TestForbiddenStopsTheRunAndAlerts(t *testing.T) {
	r := newRig(t)
	l := line(fact("actual", "6", "g", completed))
	r.store.addLine(l)
	r.dhanam.Script = []int{http.StatusForbidden}
	e := r.emitter(false)
	rep := r.run(t, e)
	assert.Equal(t, costevents.OutcomeForbidden, rep.Stopped)
	row := r.store.row(idOf(l))
	assert.Equal(t, costevents.RowPending, row.status)
	assert.Equal(t, "http_403", row.lastErr)
	var alerted bool
	for _, en := range r.hook.AllEntries() {
		alerted = alerted || (en.Level == logrus.ErrorLevel && strings.Contains(en.Message, "ALERT") && strings.Contains(en.Message, "CostEventWriterGrant"))
	}
	assert.True(t, alerted)
}

func TestUnauthorizedRefreshesTokenOnce(t *testing.T) {
	r := newRig(t)
	r.store.addLine(line(fact("actual", "6", "g", completed)))
	r.dhanam.Script = []int{http.StatusUnauthorized}
	e := r.emitter(false)
	rep := r.run(t, e)
	assert.Equal(t, 1, rep.Delivered, "a 401 drops the token and retries once with a fresh one")
	assert.Equal(t, 2, r.janua.Issued["jnc_pravara_cost"])

	r.store.addLine(line(fact("actual", "7", "g", completed)))
	r.dhanam.Script = []int{http.StatusUnauthorized, http.StatusUnauthorized}
	r.now = r.now.Add(time.Minute)
	rep = r.run(t, e)
	assert.Equal(t, costevents.OutcomeUnauthorized, rep.Stopped)
}

func TestBatching(t *testing.T) {
	r := newRig(t)
	r.batch = 2
	for i := 0; i < 5; i++ {
		f := fact("actual", "1", "g", completed)
		f.JobID = mustUUID([]string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002",
			"00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-000000000004", "00000000-0000-4000-8000-000000000005"}[i])
		r.store.addLine(costevents.Line{JobKind: "dispatch", JobID: f.JobID, MaterialCode: f.MaterialCode, Facts: []costevents.Fact{f}})
	}
	rep := r.run(t, r.emitter(false))
	assert.Equal(t, 3, rep.Batches)
	assert.Equal(t, 5, rep.Delivered)
}

func TestDryRunStoresButNeverSends(t *testing.T) {
	r := newRig(t)
	l := line(fact("actual", "6", "g", completed))
	r.store.addLine(l)
	e := r.emitter(true)
	sw, err := e.Sweep(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, sw.Stored)
	rep, err := e.Deliver(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, rep.Batches)
	assert.Equal(t, 0, r.dhanam.Count())
	assert.Equal(t, 0, r.janua.Issued["jnc_pravara_cost"])
	assert.Equal(t, costevents.RowPending, r.store.row(idOf(l)).status)

	// Leaving the dry run sends what was stored.
	assert.Equal(t, 1, r.run(t, r.emitter(false)).Delivered)
}

func TestOutboxSurvivesRestart(t *testing.T) {
	r := newRig(t)
	l := line(fact("actual", "6", "g", completed))
	r.store.addLine(l)
	r.dhanam.Script = []int{http.StatusBadGateway}
	r.run(t, r.emitter(false)) // fails; row pending with backoff

	// A new process: a new emitter over the same table.
	r.now = r.now.Add(time.Minute)
	restarted := r.emitter(false)
	assert.Equal(t, 1, r.run(t, restarted).Delivered)
	assert.Equal(t, []string{"created"}, r.dhanam.Outcomes)
}

func TestSwitchOffBuildsNothing(t *testing.T) {
	cfg := &config.Config{}
	// Garbage everywhere: with the switch off nothing is required or read.
	cfg.CostEvents = config.CostEventsConfig{Enabled: false, TenantID: "not-a-uuid", DhanamURL: "nope", BatchSize: 9000}
	e, problems := costevents.FromConfig(cfg, nil, logrus.New())
	assert.Nil(t, e)
	assert.Empty(t, problems)
	assert.Nil(t, costevents.StartFromConfig(context.Background(), cfg, nil, logrus.New()))
}

func TestSwitchOnButIncompleteDoesNotStart(t *testing.T) {
	cfg := &config.Config{}
	cfg.CostEvents = config.CostEventsConfig{Enabled: true, ClientSecret: "jns_must_not_leak"}
	log, hook := test.NewNullLogger()
	assert.Nil(t, costevents.StartFromConfig(context.Background(), cfg, nil, log))
	require.NotEmpty(t, hook.AllEntries())
	last := hook.LastEntry()
	assert.Equal(t, logrus.ErrorLevel, last.Level)
	raw, _ := json.Marshal(last.Data)
	assert.NotContains(t, string(raw), "jns_must_not_leak")
}
