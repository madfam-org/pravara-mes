package repositories

import (
	"context"
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

// ListTenantIDs returns every tenant id. tenants is the global registry and
// has no row-level security; production callers run this in the read-only
// system scope (db.RunInSystemScope) and then work one tenant at a time.
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
// its heartbeat is still older than cutoff, and writes event to the outbox.
// It runs in the tenant scope carried by ctx (db.RunInTenantTx, one per
// machine), so the update and the outbox row commit or roll back together.
// The guard makes concurrent sweeps (several API replicas) and a heartbeat
// arriving mid-sweep safe: only the sweep that actually changes the row
// writes the event. Reports whether the machine was marked offline.
func (r *MachineRepository) MarkOfflineIfStale(ctx context.Context, tenantID, machineID uuid.UUID, cutoff time.Time, event OutboxRecord) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE machines
		SET status = 'offline', updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		  AND status = 'online'
		  AND (last_heartbeat IS NULL OR last_heartbeat < $3) AND `+tenantMatch,
		machineID, tenantID, cutoff)
	if err != nil {
		return false, fmt.Errorf("failed to mark machine offline: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to mark machine offline: %w", err)
	}
	if n == 0 {
		return false, nil
	}
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO event_outbox (tenant_id, event_type, channel_namespace, payload)
		 VALUES ($1, $2, $3, $4)`,
		tenantID, event.EventType, event.Namespace, []byte(event.Payload)); err != nil {
		return false, fmt.Errorf("failed to write offline event: %w", err)
	}
	return true, nil
}
