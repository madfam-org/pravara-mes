package costevents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/observability"
)

// Backoffs between attempts of a row whose batch failed (attempt 1 waits
// 30 s, attempt 5 and later wait 6 h). Rows are never dropped: a transient
// failure keeps retrying every 6 h until it is delivered.
var Backoffs = []time.Duration{30 * time.Second, 2 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour}

// Backoff is the wait after the n-th failed attempt (n >= 1).
func Backoff(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	if n > len(Backoffs) {
		n = len(Backoffs)
	}
	return Backoffs[n-1]
}

// Lease is how long a claimed row is hidden from other replicas while its
// POST is in flight. A replica that dies mid-POST leaves the row due again
// when the lease ends; Dhanam's idempotency makes the re-send harmless.
const Lease = 2 * time.Minute

// maxBatchesPerRun bounds one delivery run; the next tick continues.
const maxBatchesPerRun = 20

// Settings drive the emitter.
type Settings struct {
	Tenant    uuid.UUID
	Options   Options
	Lookback  time.Duration
	BatchSize int
	Interval  time.Duration
	// DryRun builds and stores, never sends.
	DryRun bool
}

// Scope runs fn in the tenant's transaction (db.RunInTenantTx in
// production; a plain call in unit tests).
type Scope func(ctx context.Context, fn func(ctx context.Context) error) error

// Emitter turns job consumption into cost events and delivers them.
type Emitter struct {
	store  Store
	poster Poster // nil in dry run
	scope  Scope
	set    Settings
	log    *logrus.Logger
	now    func() time.Time
}

// NewEmitter creates the emitter. poster may be nil only in a dry run.
func NewEmitter(store Store, poster Poster, scope Scope, set Settings, log *logrus.Logger) *Emitter {
	if scope == nil {
		scope = func(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }
	}
	if set.BatchSize < 1 || set.BatchSize > MaxBatch {
		set.BatchSize = 100
	}
	return &Emitter{store: store, poster: poster, scope: scope, set: set, log: log, now: time.Now}
}

// SetClock replaces the clock (tests).
func (e *Emitter) SetClock(now func() time.Time) { e.now = now }

// SweepReport counts what a sweep did.
type SweepReport struct {
	Lines    int
	Stored   int // new or changed bodies, now pending
	Rejected int // new or changed bodies the contract refuses
	Skipped  map[string]int
}

// Sweep rebuilds every line with a fact recorded within the lookback and
// stores new or changed events as pending. Unchanged lines write nothing.
func (e *Emitter) Sweep(ctx context.Context) (SweepReport, error) {
	rep := SweepReport{Skipped: map[string]int{}}
	now := e.now().UTC()
	err := e.scope(ctx, func(ctx context.Context) error {
		lines, err := e.store.RecentLines(ctx, now.Add(-e.set.Lookback))
		if err != nil {
			return err
		}
		rep.Lines = len(lines)
		ids := make([]string, len(lines))
		for i, l := range lines {
			ids[i] = EventID(l.JobKind, l.JobID, l.MaterialCode)
		}
		stored, err := e.store.Stored(ctx, ids)
		if err != nil {
			return err
		}
		builds := make([]Built, 0, len(lines))
		for _, l := range lines {
			var prev *Event
			if p, ok := stored[EventID(l.JobKind, l.JobID, l.MaterialCode)]; ok {
				prev = &p
			}
			builds = append(builds, Build(l, prev, e.set.Options))
		}
		orphans, err := e.orphans(ctx, lines)
		if err != nil {
			return err
		}
		builds = append(builds, orphans...)
		for _, b := range builds {
			if b.Event == nil {
				rep.Skipped[b.Skip]++
				continue
			}
			body, sha, err := Body(*b.Event)
			if err != nil {
				return fmt.Errorf("marshal cost event %s: %w", b.EventID, err)
			}
			status, reason := RowPending, ""
			if len(b.Problems) > 0 {
				status, reason = RowRejected, "contract: "+strings.Join(b.Problems, "; ")
			}
			changed, err := e.store.Upsert(ctx, b, body, sha, status, reason, now)
			if err != nil {
				return err
			}
			if !changed {
				continue
			}
			if status == RowRejected {
				rep.Rejected++
				e.log.WithFields(logrus.Fields{"event_id": b.EventID, "problems": reason}).
					Error("cost events: an event breaks cost.event.v1 and will not be sent")
				observability.CostEventsTotal.WithLabelValues("contract_rejected").Inc()
			} else {
				rep.Stored++
			}
		}
		return nil
	})
	return rep, err
}

// orphans cancels events stored for a job under a line key the job no
// longer has at all (whatever the age of its facts): its code now resolves
// to another material's line (a code registered or aliased after the event
// was stored), so that consumption is sent once, under the line it belongs
// to. A sibling line outside the lookback is still one of the job's keys
// and is left alone.
func (e *Emitter) orphans(ctx context.Context, lines []Line) ([]Built, error) {
	type job struct {
		kind string
		id   uuid.UUID
	}
	var jobs []job
	seen := map[job]bool{}
	for _, l := range lines {
		if j := (job{l.JobKind, l.JobID}); !seen[j] {
			seen[j] = true
			jobs = append(jobs, j)
		}
	}
	var out []Built
	for _, j := range jobs {
		keys, err := e.store.JobLineKeys(ctx, j.kind, j.id)
		if err != nil {
			return nil, err
		}
		current := map[string]bool{}
		for _, k := range keys {
			current[EventID(j.kind, j.id, k)] = true
		}
		stored, err := e.store.JobEvents(ctx, j.kind, j.id)
		if err != nil {
			return nil, err
		}
		for _, se := range stored {
			if current[se.EventID] || se.Event.Status == StatusCancelled {
				continue
			}
			ev := se.Event
			ev.Status = StatusCancelled
			out = append(out, Built{EventID: se.EventID, JobKind: j.kind, JobID: j.id, MaterialCode: se.MaterialCode,
				Event: &ev, Problems: Validate(ev)})
		}
	}
	return out, nil
}

// DeliverReport counts what a delivery run did.
type DeliverReport struct {
	Batches   int
	Delivered int
	Rejected  int
	Retried   int
	// Stopped names the outcome that ended the run early ("" = drained).
	Stopped Outcome
}

// Deliver sends due pending rows in batches until none are due, a batch
// fails as a whole, or the run's batch limit is reached.
func (e *Emitter) Deliver(ctx context.Context) (DeliverReport, error) {
	var rep DeliverReport
	if e.set.DryRun || e.poster == nil {
		return rep, nil
	}
	for i := 0; i < maxBatchesPerRun; i++ {
		var claimed []Claimed
		if err := e.scope(ctx, func(ctx context.Context) error {
			var err error
			claimed, err = e.store.Claim(ctx, e.set.BatchSize, e.now().UTC(), Lease)
			return err
		}); err != nil {
			return rep, err
		}
		if len(claimed) == 0 {
			return rep, nil
		}
		rep.Batches++
		events := make([]json.RawMessage, len(claimed))
		for j, c := range claimed {
			events[j] = c.Body
		}
		res := e.poster.Post(ctx, events)
		if err := e.settle(ctx, claimed, res, &rep); err != nil {
			return rep, err
		}
		if res.Outcome != OutcomeOK && res.Outcome != OutcomeRejected {
			rep.Stopped = res.Outcome
			return rep, nil
		}
		// After a 400 the rows not at fault are due again at once.
		if res.Outcome == OutcomeOK && len(claimed) < e.set.BatchSize {
			return rep, nil
		}
	}
	return rep, nil
}

func (e *Emitter) settle(ctx context.Context, claimed []Claimed, res BatchResult, rep *DeliverReport) error {
	now := e.now().UTC()
	fields := logrus.Fields{"batch": len(claimed), "status": res.Status, "code": res.Code}
	switch res.Outcome {
	case OutcomeOK:
	case OutcomeRejected:
		fields["invalid"] = len(res.IndexErrors)
		e.log.WithFields(fields).Error("cost events: Dhanam refused the batch (400); refused events are marked rejected")
	case OutcomeForbidden:
		e.log.WithFields(fields).Error("ALERT cost events: HTTP 403 — no CostEventWriterGrant binds this client to source pravara; rows stay pending")
	case OutcomeConflict:
		e.log.WithFields(fields).Error("ALERT cost events: HTTP 409 — an event id already belongs to another Dhanam space; rows stay pending")
	case OutcomeUnauthorized:
		e.log.WithFields(fields).Error("ALERT cost events: Janua or Dhanam refuses the client's token (audience, scope, org binding or rotated secret); rows stay pending")
	case OutcomeTransient:
		e.log.WithFields(fields).Warn("cost events: Dhanam or Janua unavailable; rows retry with backoff")
	default:
		e.log.WithFields(fields).Error("cost events: unexpected answer from Dhanam; rows retry with backoff")
	}
	return e.scope(ctx, func(ctx context.Context) error {
		for i, c := range claimed {
			var err error
			switch {
			case res.Outcome == OutcomeOK:
				err = e.store.MarkDelivered(ctx, c.EventID, c.BodySHA256, res.Results[c.EventID], now)
				rep.Delivered++
				observability.CostEventsTotal.WithLabelValues("delivered").Inc()
			case res.Outcome == OutcomeRejected && len(res.IndexErrors) > 0:
				if codes, bad := res.IndexErrors[i]; bad {
					err = e.store.MarkRejected(ctx, c.EventID, c.BodySHA256, "dhanam: "+strings.Join(codes, ","), now)
					rep.Rejected++
					observability.CostEventsTotal.WithLabelValues("dhanam_rejected").Inc()
				} else {
					// Not at fault: due again at once, in a batch without the refused ones.
					err = e.store.Release(ctx, c.EventID, c.BodySHA256, now)
				}
			default:
				// A whole-batch failure (or a 400 naming no event) counts an
				// attempt and backs off; the row is never dropped.
				err = e.store.Retry(ctx, c.EventID, c.BodySHA256, res.Code, now.Add(Backoff(c.Attempts+1)), now)
				rep.Retried++
				observability.CostEventsTotal.WithLabelValues("retry").Inc()
			}
			if err != nil {
				return fmt.Errorf("settle cost event %s: %w", c.EventID, err)
			}
		}
		return nil
	})
}

// RunOnce sweeps, then delivers, and logs a summary when anything happened.
func (e *Emitter) RunOnce(ctx context.Context) {
	sw, err := e.Sweep(ctx)
	if err != nil {
		e.log.WithError(err).Error("cost events: sweep failed")
		return
	}
	dl, err := e.Deliver(ctx)
	if err != nil {
		e.log.WithError(err).Error("cost events: delivery failed")
	}
	if sw.Stored+sw.Rejected+dl.Batches == 0 {
		return
	}
	f := logrus.Fields{"lines": sw.Lines, "stored": sw.Stored, "contract_rejected": sw.Rejected,
		"batches": dl.Batches, "delivered": dl.Delivered, "dhanam_rejected": dl.Rejected, "retried": dl.Retried}
	for k, v := range sw.Skipped {
		f["skipped_"+k] = v
	}
	if e.set.DryRun {
		e.log.WithFields(f).Info("cost events: dry run — events stored in cost_event_outbox, nothing sent")
		return
	}
	e.log.WithFields(f).Info("cost events: run")
}

// Start runs the emitter every Interval until ctx ends. It returns at once.
func (e *Emitter) Start(ctx context.Context) {
	interval := e.set.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	go func() {
		e.RunOnce(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				e.RunOnce(ctx)
			}
		}
	}()
}
