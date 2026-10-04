package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Dispatch statuses (migration 032).
const (
	DispatchQueued          = "queued"
	DispatchReserved        = "reserved"
	DispatchRendered        = "rendered"
	DispatchSlicing         = "slicing"
	DispatchSliced          = "sliced"
	DispatchEnqueuing       = "enqueuing"
	DispatchCommandEnqueued = "command_enqueued"
	DispatchCompleted       = "completed"
	DispatchFailed          = "failed"
	DispatchCancelled       = "cancelled"
)

// DispatchLive lists the statuses the runner still advances.
var DispatchLive = []string{DispatchQueued, DispatchReserved, DispatchRendered, DispatchSlicing,
	DispatchSliced, DispatchEnqueuing, DispatchCommandEnqueued}

// ErrDispatchExists is returned when the task already has a live dispatch.
var ErrDispatchExists = errors.New("the task already has a dispatch in progress")

// ErrMachineReserved is returned when another dispatch holds the machine.
var ErrMachineReserved = errors.New("the machine is reserved by another dispatch")

// ErrLeaseLost is returned when a runner saves a dispatch it no longer leases.
var ErrLeaseLost = errors.New("dispatch lease lost")

// DispatchJob is one dispatch record.
type DispatchJob struct {
	ID                  uuid.UUID       `json:"id"`
	TenantID            uuid.UUID       `json:"tenant_id"`
	TaskID              uuid.UUID       `json:"task_id"`
	OrderItemID         *uuid.UUID      `json:"order_item_id,omitempty"`
	ProductDefinitionID *uuid.UUID      `json:"product_definition_id,omitempty"`
	MachineID           *uuid.UUID      `json:"machine_id,omitempty"`
	Status              string          `json:"status"`
	TypeShellID         string          `json:"type_shell_id,omitempty"`
	Part                string          `json:"part,omitempty"`
	Requirements        json.RawMessage `json:"requirements,omitempty"`
	MatchResult         json.RawMessage `json:"match,omitempty"`
	RenderBundle        json.RawMessage `json:"render,omitempty"`
	SliceResult         json.RawMessage `json:"slice,omitempty"`
	CommandID           *uuid.UUID      `json:"command_id,omitempty"`
	Attempts            int             `json:"attempts"`
	MaxAttempts         int             `json:"max_attempts"`
	NextAttemptAt       time.Time       `json:"next_attempt_at"`
	ErrorCode           string          `json:"error_code,omitempty"`
	ErrorMessage        string          `json:"error_message,omitempty"`
	ErrorRetryable      *bool           `json:"error_retryable,omitempty"`
	RequestedBy         *uuid.UUID      `json:"requested_by,omitempty"`
	RequestedByActor    *uuid.UUID      `json:"requested_by_actor,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	CompletedAt         *time.Time      `json:"completed_at,omitempty"`
}

// Terminal reports whether the dispatch is finished.
func (d *DispatchJob) Terminal() bool {
	return d.Status == DispatchCompleted || d.Status == DispatchFailed || d.Status == DispatchCancelled
}

// DispatchRepository stores dispatch records and machine reservations. Every
// statement runs in the tenant scope carried by ctx.
type DispatchRepository struct {
	db DBTX
}

// NewDispatchRepository creates the repository.
func NewDispatchRepository(db DBTX) *DispatchRepository { return &DispatchRepository{db: db} }

const dispatchColumns = `id, tenant_id, task_id, order_item_id, product_definition_id, machine_id, status,
	COALESCE(type_shell_id, ''), COALESCE(part, ''), requirements, match_result, render_bundle, slice_result,
	command_id, attempts, max_attempts, next_attempt_at, COALESCE(error_code, ''), COALESCE(error_message, ''),
	error_retryable, requested_by, requested_by_actor, created_at, updated_at, completed_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanDispatch(row rowScanner) (*DispatchJob, error) {
	d := &DispatchJob{}
	var orderItem, product, machine, command, requestedBy, actor uuid.NullUUID
	var req, match, render, slice []byte
	var retryable sql.NullBool
	var completed sql.NullTime
	if err := row.Scan(&d.ID, &d.TenantID, &d.TaskID, &orderItem, &product, &machine, &d.Status,
		&d.TypeShellID, &d.Part, &req, &match, &render, &slice, &command, &d.Attempts, &d.MaxAttempts,
		&d.NextAttemptAt, &d.ErrorCode, &d.ErrorMessage, &retryable, &requestedBy, &actor,
		&d.CreatedAt, &d.UpdatedAt, &completed); err != nil {
		return nil, err
	}
	d.OrderItemID, d.ProductDefinitionID, d.MachineID = ptrUUID(orderItem), ptrUUID(product), ptrUUID(machine)
	d.CommandID, d.RequestedBy, d.RequestedByActor = ptrUUID(command), ptrUUID(requestedBy), ptrUUID(actor)
	d.Requirements, d.MatchResult, d.RenderBundle, d.SliceResult = raw(req), raw(match), raw(render), raw(slice)
	if retryable.Valid {
		d.ErrorRetryable = &retryable.Bool
	}
	if completed.Valid {
		d.CompletedAt = &completed.Time
	}
	return d, nil
}

func ptrUUID(n uuid.NullUUID) *uuid.UUID {
	if !n.Valid {
		return nil
	}
	id := n.UUID
	return &id
}

func raw(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}

func jsonArg(m json.RawMessage) any {
	if len(m) == 0 {
		return nil
	}
	return []byte(m)
}

// Create inserts a queued dispatch. A second live dispatch for the same task
// returns ErrDispatchExists.
func (r *DispatchRepository) Create(ctx context.Context, d *DispatchJob) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	if d.MaxAttempts <= 0 {
		d.MaxAttempts = 5
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO dispatch_jobs (id, tenant_id, task_id, order_item_id, product_definition_id, status,
			max_attempts, requested_by, requested_by_actor)
		VALUES ($1, $2, $3, $4, $5, 'queued', $6, $7, $8)
		RETURNING status, next_attempt_at, created_at, updated_at`,
		d.ID, d.TenantID, d.TaskID, nullUUID(d.OrderItemID), nullUUID(d.ProductDefinitionID), d.MaxAttempts,
		nullUUID(d.RequestedBy), nullUUID(d.RequestedByActor),
	).Scan(&d.Status, &d.NextAttemptAt, &d.CreatedAt, &d.UpdatedAt)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" && strings.Contains(pqErr.Constraint, "live_task") {
		return ErrDispatchExists
	}
	if err != nil {
		return fmt.Errorf("create dispatch: %w", err)
	}
	return nil
}

// Get reads one dispatch (nil when not found in the tenant).
func (r *DispatchRepository) Get(ctx context.Context, id uuid.UUID) (*DispatchJob, error) {
	d, err := scanDispatch(r.db.QueryRowContext(ctx, `SELECT `+dispatchColumns+` FROM dispatch_jobs WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get dispatch: %w", err)
	}
	return d, nil
}

// DispatchFilter narrows List.
type DispatchFilter struct {
	TaskID *uuid.UUID
	Status string
	Limit  int
}

// List returns dispatches, newest first.
func (r *DispatchRepository) List(ctx context.Context, f DispatchFilter) ([]DispatchJob, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+dispatchColumns+` FROM dispatch_jobs
		WHERE ($1::uuid IS NULL OR task_id = $1) AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC LIMIT $3`, nullUUID(f.TaskID), f.Status, limit)
	if err != nil {
		return nil, fmt.Errorf("list dispatches: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []DispatchJob
	for rows.Next() {
		d, err := scanDispatch(rows)
		if err != nil {
			return nil, fmt.Errorf("scan dispatch: %w", err)
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// ClaimDue leases up to limit live dispatches of the tenant whose next
// attempt is due, so concurrent runners (several API replicas) never work on
// the same dispatch. The lease ends with Save or after lease.
func (r *DispatchRepository) ClaimDue(ctx context.Context, tenantID uuid.UUID, owner string, lease time.Duration, limit int) ([]DispatchJob, error) {
	rows, err := r.db.QueryContext(ctx, `
		UPDATE dispatch_jobs d SET lease_owner = $2, lease_until = NOW() + make_interval(secs => $3)
		WHERE d.id IN (
			SELECT id FROM dispatch_jobs
			WHERE tenant_id = $1 AND status = ANY($5) AND next_attempt_at <= NOW()
			  AND (lease_until IS NULL OR lease_until < NOW())
			ORDER BY next_attempt_at
			LIMIT $4
			FOR UPDATE SKIP LOCKED)
		RETURNING `+prefixed(dispatchColumns, "d."),
		tenantID, owner, lease.Seconds(), limit, pq.Array(DispatchLive))
	if err != nil {
		return nil, fmt.Errorf("claim dispatches: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []DispatchJob
	for rows.Next() {
		d, err := scanDispatch(rows)
		if err != nil {
			return nil, fmt.Errorf("scan dispatch: %w", err)
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func prefixed(cols, p string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		c = strings.TrimSpace(c)
		if strings.HasPrefix(c, "COALESCE(") {
			parts[i] = "COALESCE(" + p + strings.TrimPrefix(c, "COALESCE(")
		} else {
			parts[i] = p + c
		}
	}
	return strings.Join(parts, ", ")
}

// Save writes the runner's progress and ends its lease. It fails with
// ErrLeaseLost when another runner took the dispatch over.
func (r *DispatchRepository) Save(ctx context.Context, d *DispatchJob, owner string) error {
	var retryable any
	if d.ErrorRetryable != nil {
		retryable = *d.ErrorRetryable
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE dispatch_jobs SET status = $2, machine_id = $3, product_definition_id = $4, order_item_id = $5,
			type_shell_id = NULLIF($6, ''), part = NULLIF($7, ''), requirements = $8, match_result = $9,
			render_bundle = $10, slice_result = $11, command_id = $12, attempts = $13, next_attempt_at = $14,
			error_code = NULLIF($15, ''), error_message = NULLIF($16, ''), error_retryable = $17,
			completed_at = $18, lease_owner = NULL, lease_until = NULL, updated_at = NOW()
		WHERE id = $1 AND lease_owner = $19`,
		d.ID, d.Status, nullUUID(d.MachineID), nullUUID(d.ProductDefinitionID), nullUUID(d.OrderItemID),
		d.TypeShellID, d.Part, jsonArg(d.Requirements), jsonArg(d.MatchResult), jsonArg(d.RenderBundle),
		jsonArg(d.SliceResult), nullUUID(d.CommandID), d.Attempts, d.NextAttemptAt, d.ErrorCode, d.ErrorMessage,
		retryable, d.CompletedAt, owner)
	if err != nil {
		return fmt.Errorf("save dispatch: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrLeaseLost
	}
	return nil
}

// AcquireReservation holds machineID for dispatchID until ttl. Expired
// reservations of the machine are closed first; an active one held by
// another dispatch returns ErrMachineReserved. A reservation the dispatch
// already holds is extended instead.
func (r *DispatchRepository) AcquireReservation(ctx context.Context, tenantID, machineID, dispatchID uuid.UUID, ttl time.Duration) (uuid.UUID, time.Time, error) {
	if _, err := r.db.ExecContext(ctx, `
		UPDATE machine_reservations SET status = 'expired', released_at = NOW(), release_reason = 'ttl elapsed'
		WHERE machine_id = $1 AND status = 'active' AND expires_at <= NOW()`, machineID); err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("expire reservations: %w", err)
	}
	var id uuid.UUID
	var expires time.Time
	err := r.db.QueryRowContext(ctx, `
		UPDATE machine_reservations SET expires_at = NOW() + make_interval(secs => $3)
		WHERE machine_id = $1 AND dispatch_id = $2 AND status = 'active'
		RETURNING id, expires_at`, machineID, dispatchID, ttl.Seconds()).Scan(&id, &expires)
	if err == nil {
		return id, expires, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, time.Time{}, fmt.Errorf("extend reservation: %w", err)
	}
	err = db0(r).QueryRowContext(ctx, `
		INSERT INTO machine_reservations (tenant_id, machine_id, dispatch_id, expires_at)
		VALUES ($1, $2, $3, NOW() + make_interval(secs => $4))
		RETURNING id, expires_at`, tenantID, machineID, dispatchID, ttl.Seconds()).Scan(&id, &expires)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return uuid.Nil, time.Time{}, ErrMachineReserved
	}
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("insert reservation: %w", err)
	}
	return id, expires, nil
}

func db0(r *DispatchRepository) DBTX { return r.db }

// ExtendReservation pushes the dispatch's active reservation out to ttl from
// now; it reports false when the dispatch holds no active reservation.
func (r *DispatchRepository) ExtendReservation(ctx context.Context, dispatchID uuid.UUID, ttl time.Duration) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE machine_reservations SET expires_at = NOW() + make_interval(secs => $2)
		WHERE dispatch_id = $1 AND status = 'active' AND expires_at > NOW()`, dispatchID, ttl.Seconds())
	if err != nil {
		return false, fmt.Errorf("extend reservation: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ReleaseReservations closes every active reservation of the dispatch.
func (r *DispatchRepository) ReleaseReservations(ctx context.Context, dispatchID uuid.UUID, reason string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE machine_reservations SET status = 'released', released_at = NOW(), release_reason = $2
		WHERE dispatch_id = $1 AND status = 'active'`, dispatchID, reason)
	if err != nil {
		return fmt.Errorf("release reservations: %w", err)
	}
	return nil
}

// ActiveReservations maps machine id → dispatch id for unexpired reservations.
func (r *DispatchRepository) ActiveReservations(ctx context.Context) (map[uuid.UUID]uuid.UUID, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT machine_id, dispatch_id FROM machine_reservations
		WHERE status = 'active' AND expires_at > NOW()`)
	if err != nil {
		return nil, fmt.Errorf("list reservations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var m, d uuid.UUID
		if err := rows.Scan(&m, &d); err != nil {
			return nil, err
		}
		out[m] = d
	}
	return out, rows.Err()
}
