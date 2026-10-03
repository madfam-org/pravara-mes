package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// TenantScope runs work inside one tenant's database context.
//
// The command ledger only writes through this interface, so the way tenant
// context is established can change (for example to a shared
// per-transaction helper) without touching the ledger.
type TenantScope interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn func(tx *sql.Tx) error) error
}

// TxTenantScope opens a transaction and sets app.current_tenant_id for that
// transaction only (set_config(..., true) is the SET LOCAL equivalent), so
// row-level security applies to every statement and the setting never leaks
// to another pooled connection user.
type TxTenantScope struct {
	DB *sql.DB
}

// WithTenant implements TenantScope.
func (s TxTenantScope) WithTenant(ctx context.Context, tenantID uuid.UUID, fn func(tx *sql.Tx) error) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("tenant scope: nil tenant id")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("tenant scope: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.current_tenant_id', $1, true)`, tenantID.String()); err != nil {
		return fmt.Errorf("tenant scope: set tenant: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("tenant scope: commit: %w", err)
	}
	return nil
}
