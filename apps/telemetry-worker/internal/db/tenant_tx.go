package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/tenantctx"
)

// Tenant context is set per transaction: every statement the worker issues on
// tenant tables runs inside a transaction whose first statement is
// set_config('app.current_tenant_id', $1, true). Nothing is set on pooled
// connections. See docs/operations/database-app-role.md.

const setTenantSQL = "SELECT set_config('app.current_tenant_id', $1, true)"

// ErrNoTenant is returned when a statement would run without a tenant.
var ErrNoTenant = errors.New("telemetry-worker: no tenant bound to this operation")

// inTenantTx runs fn in a transaction scoped to tenantID and commits when fn
// returns nil.
func inTenantTx(ctx context.Context, db *sql.DB, tenantID uuid.UUID, fn func(tx *sql.Tx) error) error {
	if tenantID == uuid.Nil {
		return ErrNoTenant
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	if _, err := tx.ExecContext(ctx, setTenantSQL, tenantID.String()); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed to set tenant context: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// TenantScope runs work inside one tenant's database context. The command
// ledger writes only through this interface; the production implementation
// is TxTenantScope, which is inTenantTx.
type TenantScope interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn func(tx *sql.Tx) error) error
}

// TxTenantScope is the TenantScope over inTenantTx: one transaction per call,
// tenant set with set_config(..., true) as its first statement.
type TxTenantScope struct {
	DB *sql.DB
}

// WithTenant implements TenantScope.
func (s TxTenantScope) WithTenant(ctx context.Context, tenantID uuid.UUID, fn func(tx *sql.Tx) error) error {
	return inTenantTx(ctx, s.DB, tenantID, fn)
}

// tenantResolver maps a topic tenant segment (tenant UUID or slug) to a
// tenant id, caching hits and misses briefly so unknown segments cannot turn
// every message into a database lookup.
type tenantResolver struct {
	mu      sync.Mutex
	entries map[string]tenantEntry
	hitTTL  time.Duration
	missTTL time.Duration
	maxSize int
	now     func() time.Time
}

type tenantEntry struct {
	id      uuid.UUID
	expires time.Time
}

func newTenantResolver() *tenantResolver {
	return &tenantResolver{
		entries: make(map[string]tenantEntry),
		hitTTL:  5 * time.Minute,
		missTTL: 30 * time.Second,
		maxSize: 10000,
		now:     time.Now,
	}
}

// resolve returns the tenant for segment, or uuid.Nil when no tenant matches.
// The tenants table is a global registry (no row-level security).
func (r *tenantResolver) resolve(ctx context.Context, db *sql.DB, segment string) (uuid.UUID, error) {
	if segment == "" {
		return uuid.Nil, nil
	}
	r.mu.Lock()
	if e, ok := r.entries[segment]; ok && r.now().Before(e.expires) {
		r.mu.Unlock()
		return e.id, nil
	}
	r.mu.Unlock()

	var id uuid.UUID
	var err error
	if parsed, perr := uuid.Parse(segment); perr == nil {
		err = db.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id = $1`, parsed).Scan(&id)
	} else {
		err = db.QueryRowContext(ctx, `SELECT id FROM tenants WHERE slug = $1`, segment).Scan(&id)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("failed to resolve tenant: %w", err)
	}

	ttl := r.hitTTL
	if id == uuid.Nil {
		ttl = r.missTTL
	}
	r.mu.Lock()
	if len(r.entries) >= r.maxSize {
		r.entries = make(map[string]tenantEntry)
	}
	r.entries[segment] = tenantEntry{id: id, expires: r.now().Add(ttl)}
	r.mu.Unlock()
	return id, nil
}

// tenantFromContext resolves the topic segment bound to ctx.
func (s *Store) tenantFromContext(ctx context.Context) (uuid.UUID, error) {
	segment, ok := tenantctx.TopicSegment(ctx)
	if !ok {
		return uuid.Nil, ErrNoTenant
	}
	id, err := s.tenants.resolve(ctx, s.db, segment)
	if err != nil {
		return uuid.Nil, err
	}
	if id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: unknown tenant segment %q", ErrNoTenant, segment)
	}
	return id, nil
}
