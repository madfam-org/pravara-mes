package costevents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
)

// Outbox row statuses (cost_event_outbox.status, migration 034).
const (
	RowPending   = "pending"   // due for sending (again)
	RowDelivered = "delivered" // Dhanam stored this exact body
	RowRejected  = "rejected"  // the contract refuses this body; waits for a new body
)

// Claimed is a pending row leased for one delivery attempt.
type Claimed struct {
	EventID    string
	Body       json.RawMessage
	BodySHA256 string
	Attempts   int
}

// Store is the durable outbox. Every method runs in the tenant scope carried
// by ctx; a row is only ever settled for the body hash that was sent, so a
// line rebuilt while its POST was in flight stays pending.
type Store interface {
	// RecentLines returns every material line with a fact recorded at or
	// after since, each with ALL its facts.
	RecentLines(ctx context.Context, since time.Time) ([]Line, error)
	// Stored returns the events last stored for ids.
	Stored(ctx context.Context, ids []string) (map[string]Event, error)
	// Upsert stores a built event. A new or changed body becomes due at now
	// with its attempts reset; an unchanged body is left alone. changed
	// reports whether anything was written.
	Upsert(ctx context.Context, b Built, body []byte, sha, status, lastError string, now time.Time) (changed bool, err error)
	// Claim leases up to limit due, unleased pending rows until now+lease.
	// A leased row is not claimed again, even if its body changes, until
	// its POST is settled or the lease ends: two bodies of one id are never
	// in flight together.
	Claim(ctx context.Context, limit int, now time.Time, lease time.Duration) ([]Claimed, error)
	MarkDelivered(ctx context.Context, id, sha, outcome string, now time.Time) error
	MarkRejected(ctx context.Context, id, sha, reason string, now time.Time) error
	// Release makes a leased row due again at now without counting an
	// attempt (it was not at fault: another event failed the batch).
	Release(ctx context.Context, id, sha string, now time.Time) error
	// Retry counts a failed attempt and makes the row due at next.
	Retry(ctx context.Context, id, sha, reason string, next, now time.Time) error
	// Counts returns rows per status.
	Counts(ctx context.Context) (map[string]int, error)
}

// PGStore is the PostgreSQL outbox (cost_event_outbox) over a tenant-scoped
// handle.
type PGStore struct {
	q db.Querier
}

// NewPGStore creates the store. q is normally a *db.TenantDB.
func NewPGStore(q db.Querier) *PGStore { return &PGStore{q: q} }

const tenant = "NULLIF(current_setting('app.current_tenant_id', true), '')::uuid"

// RecentLines implements Store.
func (s *PGStore) RecentLines(ctx context.Context, since time.Time) ([]Line, error) {
	rows, err := s.q.QueryContext(ctx, `
		WITH recent AS (
			SELECT DISTINCT job_kind, job_id, material_code FROM job_consumption
			WHERE kind = 'material' AND created_at >= $1 AND tenant_id = `+tenant+`
		)
		SELECT c.id, c.job_kind, c.job_id, c.material_code, c.quantity::text, c.unit, c.basis,
		       c.occurred_at, c.created_at, COALESCE(o.engagement_slug, ''),
		       COALESCE((SELECT m.code FROM material_registry m
		                 WHERE m.id = material_resolve(c.material_code) AND m.tenant_id = `+tenant+`), ''),
		       COALESCE(CASE c.job_kind WHEN 'dispatch' THEN d.status WHEN 'print_job' THEN p.status END, '')
		FROM job_consumption c
		JOIN recent r ON r.job_kind = c.job_kind AND r.job_id = c.job_id AND r.material_code = c.material_code
		LEFT JOIN orders o ON o.id = c.order_id AND o.tenant_id = c.tenant_id
		LEFT JOIN dispatch_jobs d ON c.job_kind = 'dispatch' AND d.id = c.job_id AND d.tenant_id = c.tenant_id
		LEFT JOIN print_jobs p ON c.job_kind = 'print_job' AND p.id = c.job_id AND p.tenant_id = c.tenant_id
		WHERE c.kind = 'material' AND c.tenant_id = `+tenant+`
		  -- a row a correction superseded never counts (migration 036)
		  AND NOT EXISTS (SELECT 1 FROM job_consumption s WHERE s.supersedes_id = c.id AND s.tenant_id = c.tenant_id)
		ORDER BY c.job_kind, c.job_id, c.material_code, c.created_at, c.id`, since)
	if err != nil {
		return nil, fmt.Errorf("list consumption lines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Line
	for rows.Next() {
		var f Fact
		if err := rows.Scan(&f.ID, &f.JobKind, &f.JobID, &f.MaterialCode, &f.Quantity, &f.Unit, &f.Basis,
			&f.OccurredAt, &f.CreatedAt, &f.EngagementSlug, &f.RegistryCode, &f.JobStatus); err != nil {
			return nil, fmt.Errorf("scan consumption fact: %w", err)
		}
		n := len(out)
		if n == 0 || out[n-1].JobKind != f.JobKind || out[n-1].JobID != f.JobID || out[n-1].MaterialCode != f.MaterialCode {
			out = append(out, Line{JobKind: f.JobKind, JobID: f.JobID, MaterialCode: f.MaterialCode})
			n++
		}
		out[n-1].Facts = append(out[n-1].Facts, f)
	}
	return out, rows.Err()
}

// Stored implements Store.
func (s *PGStore) Stored(ctx context.Context, ids []string) (map[string]Event, error) {
	out := map[string]Event{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.q.QueryContext(ctx, `SELECT event_id, body FROM cost_event_outbox
		WHERE event_id = ANY($1) AND tenant_id = `+tenant, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("read stored cost events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var body []byte
		if err := rows.Scan(&id, &body); err != nil {
			return nil, fmt.Errorf("scan stored cost event: %w", err)
		}
		var e Event
		if err := json.Unmarshal(body, &e); err != nil {
			return nil, fmt.Errorf("decode stored cost event %s: %w", id, err)
		}
		out[id] = e
	}
	return out, rows.Err()
}

// Upsert implements Store.
func (s *PGStore) Upsert(ctx context.Context, b Built, body []byte, sha, status, lastError string, now time.Time) (bool, error) {
	var id string
	err := s.q.QueryRowContext(ctx, `
		INSERT INTO cost_event_outbox (tenant_id, event_id, job_kind, job_id, material_code, body, body_sha256,
			status, attempts, next_attempt_at, last_error, created_at, updated_at)
		VALUES (`+tenant+`, $1, $2, $3, $4, $5, $6, $7, 0, $8, NULLIF($9, ''), $8, $8)
		ON CONFLICT (tenant_id, event_id) DO UPDATE SET
			body = EXCLUDED.body, body_sha256 = EXCLUDED.body_sha256, status = EXCLUDED.status,
			attempts = 0, next_attempt_at = EXCLUDED.next_attempt_at, last_error = EXCLUDED.last_error,
			updated_at = EXCLUDED.updated_at
		WHERE cost_event_outbox.body_sha256 <> EXCLUDED.body_sha256
		RETURNING event_id`,
		b.EventID, b.JobKind, b.JobID, b.MaterialCode, body, sha, status, now, lastError).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store cost event %s: %w", b.EventID, err)
	}
	return true, nil
}

// Claim implements Store.
func (s *PGStore) Claim(ctx context.Context, limit int, now time.Time, lease time.Duration) ([]Claimed, error) {
	rows, err := s.q.QueryContext(ctx, `
		UPDATE cost_event_outbox o SET leased_until = $2, updated_at = $3
		FROM (SELECT tenant_id, event_id FROM cost_event_outbox
		      WHERE status = 'pending' AND next_attempt_at <= $3
		        AND (leased_until IS NULL OR leased_until <= $3) AND tenant_id = `+tenant+`
		      ORDER BY next_attempt_at, event_id LIMIT $1 FOR UPDATE SKIP LOCKED) due
		WHERE o.tenant_id = due.tenant_id AND o.event_id = due.event_id
		RETURNING o.event_id, o.body, o.body_sha256, o.attempts`, limit, now.Add(lease), now)
	if err != nil {
		return nil, fmt.Errorf("claim cost events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Claimed
	for rows.Next() {
		var c Claimed
		var body []byte
		if err := rows.Scan(&c.EventID, &body, &c.BodySHA256, &c.Attempts); err != nil {
			return nil, fmt.Errorf("scan claimed cost event: %w", err)
		}
		c.Body = body
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *PGStore) settle(ctx context.Context, query string, args ...any) error {
	_, err := s.q.ExecContext(ctx, query, args...)
	return err
}

// unlease ends the row's lease whatever its body: the POST that held it is
// over, so a body changed meanwhile (still pending) becomes claimable.
func (s *PGStore) unlease(ctx context.Context, id string, now time.Time) error {
	return s.settle(ctx, `UPDATE cost_event_outbox SET leased_until = NULL, updated_at = $2
		WHERE event_id = $1 AND leased_until IS NOT NULL AND tenant_id = `+tenant, id, now)
}

// MarkDelivered implements Store.
func (s *PGStore) MarkDelivered(ctx context.Context, id, sha, outcome string, now time.Time) error {
	if err := s.settle(ctx, `UPDATE cost_event_outbox SET status = 'delivered', delivered_sha256 = body_sha256,
		delivered_at = $3, dhanam_outcome = NULLIF($4, ''), attempts = 0, last_error = NULL, updated_at = $3
		WHERE event_id = $1 AND body_sha256 = $2 AND tenant_id = `+tenant, id, sha, now, outcome); err != nil {
		return err
	}
	return s.unlease(ctx, id, now)
}

// MarkRejected implements Store.
func (s *PGStore) MarkRejected(ctx context.Context, id, sha, reason string, now time.Time) error {
	if err := s.settle(ctx, `UPDATE cost_event_outbox SET status = 'rejected', last_error = $3, updated_at = $4
		WHERE event_id = $1 AND body_sha256 = $2 AND tenant_id = `+tenant, id, sha, reason, now); err != nil {
		return err
	}
	return s.unlease(ctx, id, now)
}

// Release implements Store.
func (s *PGStore) Release(ctx context.Context, id, sha string, now time.Time) error {
	if err := s.settle(ctx, `UPDATE cost_event_outbox SET next_attempt_at = $3, updated_at = $3
		WHERE event_id = $1 AND body_sha256 = $2 AND status = 'pending' AND tenant_id = `+tenant, id, sha, now); err != nil {
		return err
	}
	return s.unlease(ctx, id, now)
}

// Retry implements Store.
func (s *PGStore) Retry(ctx context.Context, id, sha, reason string, next, now time.Time) error {
	if err := s.settle(ctx, `UPDATE cost_event_outbox SET attempts = attempts + 1, next_attempt_at = $3,
		last_error = $4, updated_at = $5
		WHERE event_id = $1 AND body_sha256 = $2 AND status = 'pending' AND tenant_id = `+tenant, id, sha, next, reason, now); err != nil {
		return err
	}
	return s.unlease(ctx, id, now)
}

// Counts implements Store.
func (s *PGStore) Counts(ctx context.Context) (map[string]int, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT status, count(*) FROM cost_event_outbox
		WHERE tenant_id = `+tenant+` GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("count cost events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

var _ Store = (*PGStore)(nil)
