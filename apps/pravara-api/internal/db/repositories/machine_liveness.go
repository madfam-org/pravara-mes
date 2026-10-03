package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// OutboxRecord is an event row written in the same transaction as the state
// change it describes.
type OutboxRecord struct {
	EventType string
	Namespace string
	Payload   json.RawMessage
}

// ListTenantIDs returns every tenant id (system-level; tenants has no RLS).
// Used by background sweeps that then work one tenant at a time.
func (r *MachineRepository) ListTenantIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM tenants ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan tenant id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkOfflineIfStale moves an online machine of the tenant to offline when
// its heartbeat is still older than cutoff, and writes event to the outbox
// in the same tenant-scoped transaction. The guard makes concurrent sweeps
// (several API replicas) and a heartbeat arriving mid-sweep safe: only the
// sweep that actually changes the row writes the event. Reports whether the
// machine was marked offline.
func (r *MachineRepository) MarkOfflineIfStale(ctx context.Context, tenantID, machineID uuid.UUID, cutoff time.Time, event OutboxRecord) (bool, error) {
	changed := false
	err := withTenantTx(ctx, r.db, tenantID, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE machines
			SET status = 'offline', updated_at = NOW()
			WHERE id = $1 AND tenant_id = $2
			  AND status = 'online'
			  AND (last_heartbeat IS NULL OR last_heartbeat < $3)`,
			machineID, tenantID, cutoff)
		if err != nil {
			return fmt.Errorf("failed to mark machine offline: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to mark machine offline: %w", err)
		}
		if n == 0 {
			return nil
		}
		changed = true
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO event_outbox (tenant_id, event_type, channel_namespace, payload)
			 VALUES ($1, $2, $3, $4)`,
			tenantID, event.EventType, event.Namespace, []byte(event.Payload)); err != nil {
			return fmt.Errorf("failed to write offline event: %w", err)
		}
		return nil
	})
	return changed, err
}
