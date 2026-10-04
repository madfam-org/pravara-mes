package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Tenant context is carried per transaction, never per pooled connection.
//
// Every statement issued through a TenantDB runs inside a transaction whose
// first statement is
//
//	SELECT set_config('app.current_tenant_id', $1, true)
//
// (is_local = true), so the setting ends with the transaction and can never
// leak to the next user of the pooled connection. Row-level security policies
// and the repositories' explicit tenant predicates both read that setting.
//
// Two kinds of scope exist:
//
//   - tenant scope: one tenant, read/write. HTTP requests get one from the
//     auth middleware; background jobs open one per item with RunInTenantTx.
//   - system scope: READ ONLY, cross-tenant discovery for a small set of
//     queue-like tables (see migration 028). Opened only with RunInSystemScope,
//     always with a named purpose. Writes are never done in system scope.
//
// A TenantDB statement issued with no scope in the context fails with
// ErrNoTenantScope instead of silently running unscoped.

// ErrNoTenantScope is returned when a statement is issued through a TenantDB
// without a tenant or system scope in its context.
var ErrNoTenantScope = errors.New("db: statement issued without a tenant scope")

// Querier is the statement surface shared by *sql.DB, *sql.Tx and TenantDB.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const (
	setTenantSQL      = "SELECT set_config('app.current_tenant_id', $1, true)"
	setSystemScopeSQL = "SELECT set_config('app.system_scope', 'on', true)"
)

type scopeKind int

const (
	kindTenant scopeKind = iota
	kindSystem
)

// Scope is a lazily opened, tenant-bound transaction attached to a context.
// The transaction begins on the first statement and ends with Commit or
// Rollback. After it ends, a later statement opens a fresh transaction with
// the same tenant, which the owner must end again.
type Scope struct {
	mu        sync.Mutex
	pool      *sql.DB
	base      context.Context // transactions are bound to this, not to per-call contexts
	kind      scopeKind
	tenantID  string
	purpose   string
	tx        *sql.Tx
	savepoint int
}

type scopeKey struct{}

func scopeFrom(ctx context.Context) *Scope {
	s, _ := ctx.Value(scopeKey{}).(*Scope)
	return s
}

// TenantIDFromContext returns the tenant bound to ctx by a tenant scope.
func TenantIDFromContext(ctx context.Context) (string, bool) {
	s := scopeFrom(ctx)
	if s == nil || s.kind != kindTenant {
		return "", false
	}
	return s.tenantID, true
}

// NewTenantScope attaches a lazy tenant scope for tenantID to ctx. The caller
// must end it with Commit or Rollback.
func NewTenantScope(ctx context.Context, pool *sql.DB, tenantID string) (context.Context, *Scope, error) {
	id, err := uuid.Parse(strings.TrimSpace(tenantID))
	if err != nil || id == uuid.Nil && tenantID != uuid.Nil.String() {
		return ctx, nil, fmt.Errorf("db: invalid tenant id %q", tenantID)
	}
	s := &Scope{pool: pool, base: ctx, kind: kindTenant, tenantID: id.String()}
	return context.WithValue(ctx, scopeKey{}, s), s, nil
}

func (s *Scope) txLocked() (*sql.Tx, error) {
	if s.tx != nil {
		return s.tx, nil
	}
	opts := &sql.TxOptions{ReadOnly: s.kind == kindSystem}
	tx, err := s.pool.BeginTx(s.base, opts)
	if err != nil {
		return nil, fmt.Errorf("db: begin scoped transaction: %w", err)
	}
	if s.kind == kindSystem {
		_, err = tx.ExecContext(s.base, setSystemScopeSQL)
	} else {
		_, err = tx.ExecContext(s.base, setTenantSQL, s.tenantID)
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("db: set transaction scope: %w", err)
	}
	s.tx = tx
	return tx, nil
}

func (s *Scope) currentTx() (*sql.Tx, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.txLocked()
}

// Active reports whether a transaction is currently open.
func (s *Scope) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tx != nil
}

// Commit commits the open transaction, if any.
func (s *Scope) Commit() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tx == nil {
		return nil
	}
	err := s.tx.Commit()
	s.tx = nil
	return err
}

// Rollback rolls back the open transaction, if any.
func (s *Scope) Rollback() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tx == nil {
		return nil
	}
	err := s.tx.Rollback()
	s.tx = nil
	if errors.Is(err, sql.ErrTxDone) {
		return nil
	}
	return err
}

// RunInTenantTx runs fn with a tenant scope for tenantID and commits when fn
// returns nil (rolls back otherwise). Statements fn issues through a TenantDB
// with the given context run in that one transaction. Use it for background
// work, one call per tenant-owned item.
func RunInTenantTx(ctx context.Context, pool *sql.DB, tenantID string, fn func(ctx context.Context) error) (err error) {
	sctx, s, err := NewTenantScope(ctx, pool, tenantID)
	if err != nil {
		return err
	}
	return runScope(sctx, s, fn)
}

// RunInSystemScope runs fn in a READ ONLY transaction that may read the
// cross-tenant discovery policies of migration 028 (outbox and webhook
// queues, API-key lookup by hash). purpose names the caller for logs and
// review; it is required.
func RunInSystemScope(ctx context.Context, pool *sql.DB, purpose string, fn func(ctx context.Context) error) error {
	if strings.TrimSpace(purpose) == "" {
		return errors.New("db: system scope requires a purpose")
	}
	s := &Scope{pool: pool, base: ctx, kind: kindSystem, purpose: purpose}
	return runScope(context.WithValue(ctx, scopeKey{}, s), s, fn)
}

func runScope(ctx context.Context, s *Scope, fn func(ctx context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			_ = s.Rollback()
			panic(r)
		}
	}()
	if err = fn(ctx); err != nil {
		if rbErr := s.Rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback: %v)", err, rbErr)
		}
		return err
	}
	return s.Commit()
}

// TenantDB routes every statement into the tenant or system scope carried by
// the statement's context. It satisfies Querier.
type TenantDB struct {
	pool *sql.DB
	log  *logrus.Logger
}

// NewTenantDB wraps pool. log may be nil.
func NewTenantDB(pool *sql.DB, log *logrus.Logger) *TenantDB {
	return &TenantDB{pool: pool, log: log}
}

// Pool returns the underlying pool, for RunInTenantTx / RunInSystemScope.
func (t *TenantDB) Pool() *sql.DB { return t.pool }

func (t *TenantDB) unscoped(query string) error {
	first := strings.Join(strings.Fields(query), " ")
	if len(first) > 120 {
		first = first[:120]
	}
	if t.log != nil {
		t.log.WithField("query", first).Error("database statement issued without a tenant scope")
	}
	return fmt.Errorf("%w: %s", ErrNoTenantScope, first)
}

func (t *TenantDB) tx(ctx context.Context, query string) (*sql.Tx, error) {
	s := scopeFrom(ctx)
	if s == nil {
		return nil, t.unscoped(query)
	}
	return s.currentTx()
}

// ExecContext runs query in the context's scoped transaction.
func (t *TenantDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	tx, err := t.tx(ctx, query)
	if err != nil {
		return nil, err
	}
	return tx.ExecContext(ctx, query, args...)
}

// QueryContext runs query in the context's scoped transaction.
func (t *TenantDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	tx, err := t.tx(ctx, query)
	if err != nil {
		return nil, err
	}
	return tx.QueryContext(ctx, query, args...)
}

// QueryRowContext runs query in the context's scoped transaction. Without a
// scope it logs ErrNoTenantScope and returns a row whose Scan fails.
func (t *TenantDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	tx, err := t.tx(ctx, query)
	if err != nil {
		// *sql.Row cannot carry a custom error; a cancelled context makes
		// Scan fail without touching the database. The cause is logged above.
		dead, cancel := context.WithCancel(ctx)
		cancel()
		return t.pool.QueryRowContext(dead, query, args...)
	}
	return tx.QueryRowContext(ctx, query, args...)
}

// InTx runs fn atomically. On a TenantDB it uses a savepoint inside the
// scope's transaction, so a failure inside fn does not abort the caller's
// transaction. On a *sql.DB it opens and commits its own transaction (unit
// tests). On anything else fn runs directly.
func InTx(ctx context.Context, q Querier, fn func(q Querier) error) error {
	switch d := q.(type) {
	case *TenantDB:
		return d.inSavepoint(ctx, fn)
	case *sql.DB:
		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		if err := fn(tx); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	default:
		return fn(q)
	}
}

// Savepoint runs fn inside a savepoint when q is a TenantDB (so a failure in
// fn leaves the surrounding transaction usable) and runs fn directly on q
// otherwise.
func Savepoint(ctx context.Context, q Querier, fn func(q Querier) error) error {
	if t, ok := q.(*TenantDB); ok {
		return t.inSavepoint(ctx, fn)
	}
	return fn(q)
}

func (t *TenantDB) inSavepoint(ctx context.Context, fn func(q Querier) error) error {
	s := scopeFrom(ctx)
	if s == nil {
		return t.unscoped("SAVEPOINT")
	}
	s.mu.Lock()
	tx, err := s.txLocked()
	s.savepoint++
	name := fmt.Sprintf("tenant_sp_%d", s.savepoint)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		return fmt.Errorf("db: savepoint: %w", err)
	}
	if err := fn(t); err != nil {
		if _, rbErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+name); rbErr != nil {
			return fmt.Errorf("%w (rollback to savepoint: %v)", err, rbErr)
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+name); err != nil {
		return fmt.Errorf("db: release savepoint: %w", err)
	}
	return nil
}
