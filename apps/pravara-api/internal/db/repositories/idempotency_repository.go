package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
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

// Claim records (client, key) for this tenant with the request's
// fingerprint, unless a row for it younger than notBefore exists: then it
// returns that row and stores nothing. A nil record means the key is now
// this request's. An older row counts as absent and is replaced. While
// another transaction holds an uncommitted claim on the same key, Claim
// waits for it to end.
func (r *IdempotencyRepository) Claim(ctx context.Context, client, key, route, fingerprint string, notBefore time.Time) (*IdempotencyRecord, error) {
	// Expired rows of this tenant, this key's first, a bounded batch at a time.
	if _, err := r.db.ExecContext(ctx, `
		DELETE FROM idempotency_keys
		WHERE `+tenantMatch+` AND created_at < $1
		  AND (client, idempotency_key) IN (
		      SELECT client, idempotency_key FROM idempotency_keys
		      WHERE `+tenantMatch+` AND created_at < $1
		      ORDER BY (client = $2 AND idempotency_key = $3) DESC, created_at
		      LIMIT $4)`,
		notBefore, client, key, idempotencySweepBatch); err != nil {
		return nil, fmt.Errorf("delete expired idempotency keys: %w", err)
	}

	var claimed bool
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO idempotency_keys (tenant_id, client, idempotency_key, route, request_sha256)
		VALUES (NULLIF(current_setting('app.current_tenant_id', true), '')::uuid, $1, $2, $3, $4)
		ON CONFLICT (tenant_id, client, idempotency_key) DO NOTHING
		RETURNING TRUE`, client, key, route, fingerprint).Scan(&claimed)
	if err == nil {
		return nil, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("claim idempotency key: %w", err)
	}

	var (
		rec         IdempotencyRecord
		status      sql.NullInt64
		contentType sql.NullString
	)
	err = r.db.QueryRowContext(ctx, `
		SELECT request_sha256, response_status, response_content_type, response_body, created_at
		FROM idempotency_keys
		WHERE client = $1 AND idempotency_key = $2 AND `+tenantMatch,
		client, key).Scan(&rec.RequestSHA256, &status, &contentType, &rec.ResponseBody, &rec.CreatedAt)
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
