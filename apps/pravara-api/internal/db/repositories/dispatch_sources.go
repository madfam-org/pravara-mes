package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/matchmaking"
)

// DispatchSources reads what dispatch needs from the rest of the schema:
// the task → order item → product chain, machines for matching, live device
// state, completion events and tenant slugs. All reads run in the tenant
// scope carried by ctx.
type DispatchSources struct {
	db DBTX
}

// NewDispatchSources creates the reader.
func NewDispatchSources(db DBTX) *DispatchSources { return &DispatchSources{db: db} }

// TaskContext is a task with its order item and product.
type TaskContext struct {
	TaskID         uuid.UUID
	TaskStatus     string
	OrderID        *uuid.UUID
	OrderItemID    *uuid.UUID
	ProductSKU     string
	ItemSpecs      map[string]any
	ProductID      *uuid.UUID
	ProductMeta    map[string]any
	ParametricSpec map[string]any
}

// TaskContext loads the chain for a task (nil when the task is not in the tenant).
func (s *DispatchSources) TaskContext(ctx context.Context, taskID uuid.UUID) (*TaskContext, error) {
	tc := &TaskContext{TaskID: taskID}
	var orderID, itemID uuid.NullUUID
	var sku sql.NullString
	var specs []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT t.status, t.order_id, t.order_item_id, oi.product_sku, oi.specifications
		FROM tasks t LEFT JOIN order_items oi ON oi.id = t.order_item_id
		WHERE t.id = $1`, taskID).Scan(&tc.TaskStatus, &orderID, &itemID, &sku, &specs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}
	tc.OrderID, tc.OrderItemID, tc.ProductSKU = ptrUUID(orderID), ptrUUID(itemID), sku.String
	tc.ItemSpecs = decodeObject(specs)
	if tc.ProductSKU == "" {
		return tc, nil
	}
	var pid uuid.UUID
	var meta, params []byte
	err = s.db.QueryRowContext(ctx, `
		SELECT id, metadata, parametric_specs FROM product_definitions
		WHERE sku = $1 AND is_active ORDER BY updated_at DESC LIMIT 1`, tc.ProductSKU).Scan(&pid, &meta, &params)
	if errors.Is(err, sql.ErrNoRows) {
		return tc, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load product: %w", err)
	}
	tc.ProductID, tc.ProductMeta, tc.ParametricSpec = &pid, decodeObject(meta), decodeObject(params)
	return tc, nil
}

func decodeObject(b []byte) map[string]any {
	out := map[string]any{}
	if len(b) > 0 {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// MatchMachines lists the tenant's machines with their registry capabilities,
// fabrication-prep profile, recorded material lots, queued work and
// reservation holder.
func (s *DispatchSources) MatchMachines(ctx context.Context) ([]matchmaking.Machine, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.code, m.name, COALESCE(m.status::text, 'offline'),
		       COALESCE(m.specifications -> 'fabrication_capabilities', '{}'::jsonb),
		       COALESCE(m.metadata -> 'fabrication_prep', '{}'::jsonb),
		       COALESCE(m.metadata -> 'material_lots', '{}'::jsonb),
		       (SELECT count(*) FROM tasks t WHERE t.machine_id = m.id AND t.status IN ('queued', 'in_progress')),
		       (SELECT r.dispatch_id FROM machine_reservations r
		         WHERE r.machine_id = m.id AND r.status = 'active' AND r.expires_at > NOW() LIMIT 1)
		FROM machines m ORDER BY m.code`)
	if err != nil {
		return nil, fmt.Errorf("list machines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []matchmaking.Machine
	for rows.Next() {
		var m matchmaking.Machine
		var caps, prep, lots []byte
		var reservedBy uuid.NullUUID
		if err := rows.Scan(&m.ID, &m.Code, &m.Name, &m.RegistryStatus, &caps, &prep, &lots, &m.ActiveTasks, &reservedBy); err != nil {
			return nil, fmt.Errorf("scan machine: %w", err)
		}
		m.Capabilities = decodeObject(caps)
		p := decodeObject(prep)
		m.PrinterProfile, _ = p["printer_profile"].(string)
		m.Target, _ = p["target"].(string)
		for k, v := range decodeObject(lots) {
			n, err := strconv.Atoi(k)
			if lot, ok := v.(string); ok && err == nil && lot != "" {
				if m.MaterialLots == nil {
					m.MaterialLots = map[int]string{}
				}
				m.MaterialLots[n] = lot
			}
		}
		m.ReservedBy = ptrUUID(reservedBy)
		out = append(out, m)
	}
	return out, rows.Err()
}

// MachineCode returns a machine's code ("" when not found).
func (s *DispatchSources) MachineCode(ctx context.Context, id uuid.UUID) (string, error) {
	var code string
	err := s.db.QueryRowContext(ctx, `SELECT code FROM machines WHERE id = $1`, id).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return code, err
}

// LiveStates reads machine_live_state (written by the telemetry worker's
// Sparkplug primary host, migration 031). It satisfies
// matchmaking.LiveStateReader. Machines without a row are absent.
func (s *DispatchSources) LiveStates(ctx context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]matchmaking.LiveState, error) {
	out := map[uuid.UUID]matchmaking.LiveState{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id.String()
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT machine_id, online, COALESCE(state_status, ''), material_slots, capabilities,
		       born_at, died_at, COALESCE(reported_at, updated_at)
		FROM machine_live_state WHERE machine_id = ANY($1::uuid[])`, pq.Array(args))
	if err != nil {
		return nil, fmt.Errorf("read live state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id uuid.UUID
		var online bool
		var status string
		var slots, caps []byte
		var born, died sql.NullTime
		var observed time.Time
		if err := rows.Scan(&id, &online, &status, &slots, &caps, &born, &died, &observed); err != nil {
			return nil, fmt.Errorf("scan live state: %w", err)
		}
		st := matchmaking.LiveState{Status: status, Capabilities: decodeObject(caps), ObservedAt: &observed}
		st.Born = online && born.Valid && (!died.Valid || died.Time.Before(born.Time))
		var list []struct {
			Slot   int     `json:"slot"`
			Class  *string `json:"class"`
			Loaded bool    `json:"loaded"`
		}
		_ = json.Unmarshal(slots, &list)
		for _, sl := range list {
			ms := matchmaking.MaterialSlot{Slot: sl.Slot, Loaded: sl.Loaded}
			if sl.Class != nil {
				ms.Class = *sl.Class
			}
			st.Materials = append(st.Materials, ms)
		}
		out[id] = st
	}
	return out, rows.Err()
}

// CompletionEvent is a job-completion event from the outbox.
type CompletionEvent struct {
	EventID   uuid.UUID
	Type      string
	CreatedAt time.Time
	Data      map[string]any
}

// FindCompletionEvent returns the outbox event of one of types whose
// data.command_id is commandID, preferring earlier entries of types (nil
// when none yet).
func (s *DispatchSources) FindCompletionEvent(ctx context.Context, types []string, commandID uuid.UUID) (*CompletionEvent, error) {
	var ev CompletionEvent
	var payload []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT id, event_type, created_at, payload FROM event_outbox
		WHERE event_type = ANY($1) AND payload -> 'data' ->> 'command_id' = $2
		ORDER BY array_position($1::text[], event_type::text), created_at LIMIT 1`, pq.Array(types), commandID.String()).Scan(&ev.EventID, &ev.Type, &ev.CreatedAt, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find completion event: %w", err)
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(payload, &env)
	ev.Data = env.Data
	return &ev, nil
}

// CommandLedgerState returns a ledger row's status, machine and completion time.
func (s *DispatchSources) CommandLedgerState(ctx context.Context, commandID uuid.UUID) (status string, machineID uuid.UUID, completedAt *time.Time, errMsg string, err error) {
	var done sql.NullTime
	var msg sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT status, machine_id, completed_at, error_message FROM task_commands WHERE command_id = $1`,
		commandID).Scan(&status, &machineID, &done, &msg)
	if errors.Is(err, sql.ErrNoRows) {
		return "", uuid.Nil, nil, "", nil
	}
	if done.Valid {
		completedAt = &done.Time
	}
	return status, machineID, completedAt, msg.String, err
}

// TenantSlug returns a tenant's slug (tenants has no row-level security).
func (s *DispatchSources) TenantSlug(ctx context.Context, tenantID uuid.UUID) (string, error) {
	var slug string
	err := s.db.QueryRowContext(ctx, `SELECT slug FROM tenants WHERE id = $1`, tenantID).Scan(&slug)
	return slug, err
}
