package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// IdempotencyRecord is a stored Idempotency-Key (migration 037).
type IdempotencyRecord struct {
	RequestSHA256 string
	// Completed is false only for a row whose answer was never stored; the
	// middleware stores the answer in the same transaction as the row, so a
	// committed row without one means a write ended its transaction early.
	Completed           bool
	ResponseStatus      int
	ResponseContentType string
	ResponseBody        []byte
	CreatedAt           time.Time
}

// IdempotencyRepository keeps Idempotency-Keys for the current tenant. Every
// statement runs in the request's tenant transaction.
type IdempotencyRepository struct {
	db DBTX
}

// NewIdempotencyRepository creates an idempotency key repository.
func NewIdempotencyRepository(db DBTX) *IdempotencyRepository {
	return &IdempotencyRepository{db: db}
}

// idempotencySweepBatch bounds how many expired rows one claim deletes.
const idempotencySweepBatch = 100

// IdempotencyLockTimeout bounds how long a claim waits for another request
// holding the same key: the duplicate then answers ErrIdempotencyBusy
// instead of keeping a connection.
const IdempotencyLockTimeout = 5 * time.Second

// idempotencyClaimAttempts bounds the claim's retries when the row it
// conflicted with turns out to be expired (another request was deleting it
// and gave up).
const idempotencyClaimAttempts = 3

// ErrIdempotencyBusy: another request holds the same key and did not finish
// within IdempotencyLockTimeout. The request transaction is aborted; the
// caller answers with a retryable status.
var ErrIdempotencyBusy = errors.New("idempotency key is held by another request")

// pqLockNotAvailable is PostgreSQL's lock_not_available (55P03), raised when
// lock_timeout expires.
const pqLockNotAvailable = "55P03"

// Claim records (client, key) for this tenant with the request's
// fingerprint, unless a row for it no older than notBefore exists: then it
// returns that row and stores nothing. A nil record means the key is now
// this request's. An older row counts as absent and is replaced. While
// another transaction holds an uncommitted claim on the same key, Claim
// waits for it to end, at most IdempotencyLockTimeout (ErrIdempotencyBusy).
func (r *IdempotencyRepository) Claim(ctx context.Context, client, key, route, fingerprint string, notBefore time.Time) (*IdempotencyRecord, error) {
	for attempt := 0; attempt < idempotencyClaimAttempts; attempt++ {
		claimed, err := r.tryClaim(ctx, client, key, route, fingerprint, notBefore)
		if err != nil || claimed {
			return nil, err
		}
		rec, err := r.load(ctx, client, key, notBefore)
		if err != nil {
			return nil, err
		}
		if rec != nil {
			return rec, nil
		}
		// The row we conflicted with is expired: sweep and claim again.
	}
	return nil, fmt.Errorf("claim idempotency key: still held by an expired row after %d attempts", idempotencyClaimAttempts)
}

// tryClaim sweeps expired rows and inserts the claim; false means a row for
// the key already exists.
func (r *IdempotencyRepository) tryClaim(ctx context.Context, client, key, route, fingerprint string, notBefore time.Time) (bool, error) {
	// Expired rows of this tenant, this key's first, a bounded batch at a
	// time. Rows another request is already deleting are skipped, so two
	// requests never wait on each other here.
	if _, err := r.db.ExecContext(ctx, `
		DELETE FROM idempotency_keys
		WHERE `+tenantMatch+` AND created_at < $1
		  AND (client, idempotency_key) IN (
		      SELECT client, idempotency_key FROM idempotency_keys
		      WHERE `+tenantMatch+` AND created_at < $1
		      ORDER BY (client = $2 AND idempotency_key = $3) DESC, created_at
		      LIMIT $4
		      FOR UPDATE SKIP LOCKED)`,
		notBefore, client, key, idempotencySweepBatch); err != nil {
		return false, fmt.Errorf("delete expired idempotency keys: %w", err)
	}

	// The insert waits while another transaction holds the same key; bound
	// that wait for this statement only, then put the setting back.
	var previous string
	if err := r.db.QueryRowContext(ctx, `SELECT current_setting('lock_timeout')`).Scan(&previous); err != nil {
		return false, fmt.Errorf("read lock_timeout: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, `SELECT set_config('lock_timeout', $1, true)`,
		fmt.Sprintf("%dms", IdempotencyLockTimeout.Milliseconds())); err != nil {
		return false, fmt.Errorf("set lock_timeout: %w", err)
	}
	var claimed bool
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO idempotency_keys (tenant_id, client, idempotency_key, route, request_sha256)
		VALUES (NULLIF(current_setting('app.current_tenant_id', true), '')::uuid, $1, $2, $3, $4)
		ON CONFLICT (tenant_id, client, idempotency_key) DO NOTHING
		RETURNING TRUE`, client, key, route, fingerprint).Scan(&claimed)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && string(pqErr.Code) == pqLockNotAvailable {
		return false, ErrIdempotencyBusy
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("claim idempotency key: %w", err)
	}
	if _, rerr := r.db.ExecContext(ctx, `SELECT set_config('lock_timeout', $1, true)`, previous); rerr != nil {
		return false, fmt.Errorf("restore lock_timeout: %w", rerr)
	}
	return err == nil, nil
}

// load returns the key's row when it is no older than notBefore, else nil.
func (r *IdempotencyRepository) load(ctx context.Context, client, key string, notBefore time.Time) (*IdempotencyRecord, error) {
	var (
		rec         IdempotencyRecord
		status      sql.NullInt64
		contentType sql.NullString
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT request_sha256, response_status, response_content_type, response_body, created_at
		FROM idempotency_keys
		WHERE client = $1 AND idempotency_key = $2 AND created_at >= $3 AND `+tenantMatch,
		client, key, notBefore).Scan(&rec.RequestSHA256, &status, &contentType, &rec.ResponseBody, &rec.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load idempotency key: %w", err)
	}
	rec.Completed = status.Valid
	rec.ResponseStatus = int(status.Int64)
	rec.ResponseContentType = contentType.String
	return &rec, nil
}

// Complete stores the answer of the request that claimed (client, key).
func (r *IdempotencyRepository) Complete(ctx context.Context, client, key string, status int, contentType string, body []byte) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE idempotency_keys
		SET response_status = $3, response_content_type = $4, response_body = $5
		WHERE client = $1 AND idempotency_key = $2 AND response_status IS NULL AND `+tenantMatch,
		client, key, status, contentType, body)
	if err != nil {
		return fmt.Errorf("store idempotent response: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n != 1 {
		return fmt.Errorf("store idempotent response: %d rows for one claimed key", n)
	}
	return nil
}
