package repositories

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// withTenantTx runs fn in a transaction whose app.current_tenant_id is set
// for that transaction only (set_config(..., true), the SET LOCAL
// equivalent), so row-level security applies to every statement and the
// setting never outlives the transaction on a pooled connection.
func withTenantTx(ctx context.Context, db *sql.DB, tenantID uuid.UUID, fn func(tx *sql.Tx) error) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("tenant transaction: nil tenant id")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("tenant transaction: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.current_tenant_id', $1, true)`, tenantID.String()); err != nil {
		return fmt.Errorf("tenant transaction: set tenant: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("tenant transaction: commit: %w", err)
	}
	return nil
}
