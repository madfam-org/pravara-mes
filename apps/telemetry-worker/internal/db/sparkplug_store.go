package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/command"
	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/host"
)

// SparkplugStore is the primary host's persistence (host.Store) on the
// registry tables of migration 031. Every write runs in one tenant
// transaction: the edge node's tenant, resolved from the topic group (the
// tenant slug) and confirmed by the edge_nodes row of that tenant.
//
// Device reports update machine_live_state, bind Command/* and Job/* to the
// command ledger (the command must have been issued to this machine) and
// write outbox events, all in the same transaction.
type SparkplugStore struct {
	db      *sql.DB
	scope   TenantScope
	tenants *tenantResolver
	ledger  *CommandLedger
	hook    command.JobCompletionHook
	log     *logrus.Logger
	now     func() time.Time
}

var _ host.Store = (*SparkplugStore)(nil)

// NewSparkplugStore returns the store; it shares the worker store's tenant
// resolver and applies acks through ledger.
func NewSparkplugStore(store *Store, ledger *CommandLedger, log *logrus.Logger) *SparkplugStore {
	return &SparkplugStore{
		db: store.db, scope: TxTenantScope{DB: store.db}, tenants: store.tenants,
		ledger: ledger, log: log, now: func() time.Time { return time.Now().UTC() },
	}
}

// SetCompletionHook registers the hook called after a committed job
// completion (same contract as AckHandler.SetCompletionHook).
func (s *SparkplugStore) SetCompletionHook(h command.JobCompletionHook) { s.hook = h }

// Event written when a Sparkplug device reports Job/Status = complete for a
// job bound to an issued start_job command. The passport updater consumes it.
const EventMachineJobCompleted = "machine.job_completed"

// MachineJobCompletedData is the payload of machine.job_completed.
type MachineJobCompletedData struct {
	MachineID   uuid.UUID  `json:"machine_id"`
	MachineName string     `json:"machine_name"`
	MachineCode string     `json:"machine_code"` // Sparkplug device_id
	EdgeNodeID  string     `json:"edge_node_id"`
	CommandID   uuid.UUID  `json:"command_id"`
	JobID       string     `json:"job_id"`
	JobStatus   string     `json:"job_status"` // "complete"
	TaskID      *uuid.UUID `json:"task_id,omitempty"`
	OrderID     *uuid.UUID `json:"order_id,omitempty"`
	// PrinterReportedAt is the Job/Status metric timestamp set by the edge node.
	PrinterReportedAt time.Time `json:"printer_reported_at"`
	// HostReceivedAt is when the host received the DDATA from the broker.
	HostReceivedAt time.Time `json:"host_received_at"`
	// RecordedAt is when the completion was committed.
	RecordedAt time.Time `json:"recorded_at"`
	BdSeq      uint64    `json:"bdseq"`
	Seq        uint64    `json:"seq"`
}

// ResolveEdgeNode implements host.Store. The group must be a tenant slug
// (MES-1 §1: group_id = tenant slug) and the edge node must be registered
// and enabled in that tenant.
func (s *SparkplugStore) ResolveEdgeNode(ctx context.Context, group, edgeNodeID string) (*host.EdgeNode, error) {
	if _, err := uuid.Parse(group); err == nil {
		return nil, nil
	}
	tenantID, err := s.tenants.resolve(ctx, s.db, group)
	if err != nil || tenantID == uuid.Nil {
		return nil, err
	}
	var node *host.EdgeNode
	err = s.scope.WithTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var id uuid.UUID
		var disabled bool
		err := tx.QueryRowContext(ctx, `
			SELECT id, disabled_at IS NOT NULL FROM edge_nodes
			WHERE tenant_id = $1 AND edge_node_id = $2`, tenantID, edgeNodeID,
		).Scan(&id, &disabled)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && disabled) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("resolve edge node: %w", err)
		}
		node = &host.EdgeNode{ID: id.String(), TenantID: tenantID.String(), Group: group, EdgeNodeID: edgeNodeID}
		return nil
	})
	return node, err
}

func ids(n host.EdgeNode) (tenantID, nodeID uuid.UUID, err error) {
	if tenantID, err = uuid.Parse(n.TenantID); err != nil {
		return
	}
	nodeID, err = uuid.Parse(n.ID)
	return
}

// NodeBirth implements host.Store.
func (s *SparkplugStore) NodeBirth(ctx context.Context, n host.EdgeNode, bdSeq uint64, at time.Time) error {
	tenantID, nodeID, err := ids(n)
	if err != nil {
		return err
	}
	return s.scope.WithTenant(ctx, tenantID, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE edge_nodes SET online = TRUE, last_bdseq = $3, last_birth_at = $4, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $2`, nodeID, tenantID, int64(bdSeq), at); err != nil {
			return fmt.Errorf("node birth: %w", err)
		}
		// Devices are live again only after their DBIRTH in this session.
		if _, err := tx.ExecContext(ctx, `
			UPDATE machine_live_state SET online = FALSE, updated_at = NOW()
			WHERE tenant_id = $1 AND edge_node_id = $2 AND online`, tenantID, n.EdgeNodeID); err != nil {
			return fmt.Errorf("node birth: devices: %w", err)
		}
		return nil
	})
}

// NodeDeath implements host.Store. It applies only when bdSeq is the bdSeq
// of the node's latest NBIRTH.
func (s *SparkplugStore) NodeDeath(ctx context.Context, n host.EdgeNode, bdSeq uint64, at time.Time) (bool, error) {
	tenantID, nodeID, err := ids(n)
	if err != nil {
		return false, err
	}
	applied := false
	err = s.scope.WithTenant(ctx, tenantID, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE edge_nodes SET online = FALSE, last_death_at = $4, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $2 AND last_bdseq = $3`, nodeID, tenantID, int64(bdSeq), at)
		if err != nil {
			return fmt.Errorf("node death: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		applied = true
		return s.devicesOffline(ctx, tx, tenantID, n.EdgeNodeID, nil, at)
	})
	return applied, err
}

// DeviceDeath implements host.Store.
func (s *SparkplugStore) DeviceDeath(ctx context.Context, n host.EdgeNode, deviceID string, at time.Time) error {
	tenantID, _, err := ids(n)
	if err != nil {
		return err
	}
	return s.scope.WithTenant(ctx, tenantID, func(tx *sql.Tx) error {
		return s.devicesOffline(ctx, tx, tenantID, n.EdgeNodeID, []string{deviceID}, at)
	})
}

// devicesOffline marks the edge node's devices (all, or the listed codes)
// offline in the live state and in machines, with machine.status_changed
// events for machines whose status changed.
func (s *SparkplugStore) devicesOffline(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, edge string, codes []string, at time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE machine_live_state ls SET online = FALSE, died_at = $3, updated_at = NOW()
		FROM machines m
		WHERE ls.machine_id = m.id AND ls.tenant_id = $1 AND m.tenant_id = $1 AND ls.edge_node_id = $2
		  AND ($4::text[] IS NULL OR m.code = ANY($4))`,
		tenantID, edge, at, nullableArray(codes)); err != nil {
		return fmt.Errorf("devices offline: live state: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
		WITH prev AS (
			SELECT id, name, status::text AS status FROM machines
			WHERE tenant_id = $1 AND sparkplug_edge_id = $2 AND status <> 'offline'
			  AND ($3::text[] IS NULL OR code = ANY($3))
			FOR UPDATE
		)
		UPDATE machines m SET status = 'offline', updated_at = NOW()
		FROM prev WHERE m.id = prev.id
		RETURNING m.id, prev.name, prev.status`,
		tenantID, edge, nullableArray(codes))
	if err != nil {
		return fmt.Errorf("devices offline: machines: %w", err)
	}
	type change struct {
		id        uuid.UUID
		name, old string
	}
	var changes []change
	for rows.Next() {
		var c change
		if err := rows.Scan(&c.id, &c.name, &c.old); err != nil {
			_ = rows.Close()
			return err
		}
		changes = append(changes, c)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, c := range changes {
		if err := insertOutboxEvent(ctx, tx, tenantID, EventMachineStatusChanged, NamespaceMachines, MachineStatusData{
			MachineID: c.id, MachineName: c.name, OldStatus: c.old, NewStatus: "offline", UpdatedAt: at,
		}); err != nil {
			return err
		}
	}
	return nil
}

func nullableArray(codes []string) interface{} {
	if codes == nil {
		return nil
	}
	return pq.Array(codes)
}

// Touch implements host.Store.
func (s *SparkplugStore) Touch(ctx context.Context, n host.EdgeNode, deviceIDs []string, at time.Time) error {
	tenantID, _, err := ids(n)
	if err != nil {
		return err
	}
	return s.scope.WithTenant(ctx, tenantID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE machines SET last_heartbeat = $4
			WHERE tenant_id = $1 AND sparkplug_edge_id = $2 AND code = ANY($3)`,
			tenantID, n.EdgeNodeID, pq.Array(deviceIDs), at)
		if err != nil {
			return fmt.Errorf("touch devices: %w", err)
		}
		return nil
	})
}

// DeviceReport implements host.Store.
func (s *SparkplugStore) DeviceReport(ctx context.Context, n host.EdgeNode, r host.DeviceReport) (host.DeviceOutcome, error) {
	tenantID, _, err := ids(n)
	if err != nil {
		return host.DeviceOutcome{}, err
	}
	var (
		out         host.DeviceOutcome
		completions []command.JobCompletion
	)
	err = s.scope.WithTenant(ctx, tenantID, func(tx *sql.Tx) error {
		out, completions = host.DeviceOutcome{}, nil
		var m command.AckMachine
		var status string
		err := tx.QueryRowContext(ctx, `
			SELECT id, tenant_id, code, name, status::text FROM machines
			WHERE tenant_id = $1 AND sparkplug_edge_id = $2 AND code = $3`,
			tenantID, n.EdgeNodeID, r.DeviceID,
		).Scan(&m.ID, &m.TenantID, &m.Code, &m.Name, &status)
		if errors.Is(err, sql.ErrNoRows) {
			if r.Birth {
				return s.quarantine(ctx, tx, tenantID, n.EdgeNodeID, r)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("device report: machine: %w", err)
		}
		out.Registered = true

		ls, err := loadLiveState(ctx, tx, tenantID, m.ID)
		if err != nil {
			return err
		}
		for _, w := range ls.apply(r) {
			s.log.WithFields(logrus.Fields{"tenant_id": tenantID, "machine_id": m.ID}).Warn("Sparkplug metric skipped: " + w)
		}
		if err := ls.save(ctx, tx, tenantID, m.ID, n.EdgeNodeID, s.now()); err != nil {
			return err
		}
		if err := s.markOnline(ctx, tx, tenantID, m, status, r.Birth); err != nil {
			return err
		}
		completions, err = s.bindCommands(ctx, tx, n, m, r)
		if err != nil {
			return err
		}
		if r.Birth {
			out.Resend, err = s.unacknowledged(ctx, tx, tenantID, m.ID)
		}
		return err
	})
	if err != nil {
		return host.DeviceOutcome{}, err
	}
	if s.hook != nil {
		for _, c := range completions {
			if herr := s.hook.OnJobCompleted(ctx, c); herr != nil {
				s.log.WithError(herr).WithField("command_id", c.CommandID).Error("Job completion hook failed")
			}
		}
	}
	return out, nil
}

// markOnline refreshes the machine's heartbeat; a DBIRTH also brings an
// offline machine online (operator states such as maintenance are kept).
func (s *SparkplugStore) markOnline(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, m command.AckMachine, status string, birth bool) error {
	now := s.now()
	if !birth || status != "offline" {
		_, err := tx.ExecContext(ctx, `UPDATE machines SET last_heartbeat = $3 WHERE id = $1 AND tenant_id = $2`, m.ID, tenantID, now)
		if err != nil {
			return fmt.Errorf("device heartbeat: %w", err)
		}
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE machines SET status = 'online', last_heartbeat = $3, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2`, m.ID, tenantID, now); err != nil {
		return fmt.Errorf("device online: %w", err)
	}
	return insertOutboxEvent(ctx, tx, tenantID, EventMachineStatusChanged, NamespaceMachines, MachineStatusData{
		MachineID: m.ID, MachineName: m.Name, OldStatus: status, NewStatus: "online", UpdatedAt: now,
	})
}

// quarantine records a DBIRTH of a device that is not a registered machine
// of this edge node. It is never trusted: status stays as the operator set
// it ('discovered' for a new row).
func (s *SparkplugStore) quarantine(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, edge string, r host.DeviceReport) error {
	values := map[string]any{}
	for k, v := range r.Values {
		values[string(k)] = v
	}
	birth, err := json.Marshal(values)
	if err != nil {
		return err
	}
	str := func(name sparkplug.MetricName) sql.NullString {
		v, ok := r.Values[name].(string)
		if ok && len(v) > 100 {
			v = v[:100]
		}
		return sql.NullString{String: v, Valid: ok}
	}
	fw := str(sparkplug.MetricPropertiesFirmware)
	if len(fw.String) > 50 {
		fw.String = fw.String[:50]
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO discovered_machines (
			tenant_id, discovery_method, sparkplug_edge_id, device_id, birth_payload,
			model, firmware_version, connection_type, status, first_seen_at, last_seen_at, birth_count, discovered_at)
		VALUES ($1, 'sparkplug', $2, $3, $4, $5, $6, 'sparkplug', 'discovered', $7, $7, 1, $7)
		ON CONFLICT (tenant_id, sparkplug_edge_id, device_id) WHERE sparkplug_edge_id IS NOT NULL
		DO UPDATE SET birth_payload = EXCLUDED.birth_payload, model = EXCLUDED.model,
			firmware_version = EXCLUDED.firmware_version, last_seen_at = EXCLUDED.last_seen_at,
			birth_count = discovered_machines.birth_count + 1, updated_at = NOW()`,
		tenantID, edge, r.DeviceID, birth, str(sparkplug.MetricPropertiesModel), fw, r.ReceivedAt)
	if err != nil {
		return fmt.Errorf("quarantine device: %w", err)
	}
	s.log.WithFields(logrus.Fields{"tenant_id": tenantID, "edge_node_id": edge, "device_id": r.DeviceID}).
		Warn("DBIRTH from an unregistered device quarantined in discovered_machines")
	return nil
}
