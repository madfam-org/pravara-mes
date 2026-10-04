// Package db provides database access for the telemetry worker.
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/config"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// Store provides database operations for telemetry data. Every statement on
// a tenant table runs in a transaction scoped to one tenant (tenant_tx.go).
type Store struct {
	db      *sql.DB
	tenants *tenantResolver
}

// NewStore creates a new database store.
func NewStore(cfg *config.DatabaseConfig) (*Store, error) {
	db, err := sql.Open("postgres", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return newStoreWithDB(db), nil
}

func newStoreWithDB(db *sql.DB) *Store {
	return &Store{db: db, tenants: newTenantResolver()}
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Stats returns database connection pool statistics.
func (s *Store) Stats() sql.DBStats {
	return s.db.Stats()
}

const insertTelemetrySQL = `
	INSERT INTO telemetry (
		id, tenant_id, machine_id, timestamp, metric_type, value, unit, metadata
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	ON CONFLICT (id) DO NOTHING
`

// CreateBatch inserts telemetry records, one transaction per tenant. Inserts
// are idempotent on id, so a retry after a partial failure does not
// duplicate the tenants that already committed.
func (s *Store) CreateBatch(ctx context.Context, records []types.Telemetry) error {
	if len(records) == 0 {
		return nil
	}

	order := make([]uuid.UUID, 0, 1)
	byTenant := make(map[uuid.UUID][]int)
	for i := range records {
		t := records[i].TenantID
		if _, seen := byTenant[t]; !seen {
			order = append(order, t)
		}
		byTenant[t] = append(byTenant[t], i)
	}

	for _, tenantID := range order {
		idx := byTenant[tenantID]
		err := inTenantTx(ctx, s.db, tenantID, func(tx *sql.Tx) error {
			for _, i := range idx {
				metadataJSON, _ := json.Marshal(records[i].Metadata)
				_, err := tx.ExecContext(ctx, insertTelemetrySQL,
					records[i].ID, records[i].TenantID, records[i].MachineID,
					records[i].Timestamp, records[i].MetricType, records[i].Value,
					records[i].Unit, metadataJSON,
				)
				if err != nil {
					return fmt.Errorf("failed to insert telemetry: %w", err)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

const machineColumns = `id, tenant_id, name, code, type, description, status,
	       mqtt_topic, location, specifications, metadata,
	       last_heartbeat, created_at, updated_at`

// ResolveMachine returns the machine with code that belongs to the tenant
// named by tenantSegment (the topic's first level: tenant UUID or slug).
// It returns nil, nil when the tenant or the (tenant, code) pair is unknown.
func (s *Store) ResolveMachine(ctx context.Context, tenantSegment, code string) (*types.Machine, error) {
	tenantID, err := s.tenants.resolve(ctx, s.db, tenantSegment)
	if err != nil {
		return nil, err
	}
	if tenantID == uuid.Nil {
		return nil, nil
	}
	return s.getMachineByTenantAndCode(ctx, tenantID, code)
}

func (s *Store) getMachineByTenantAndCode(ctx context.Context, tenantID uuid.UUID, code string) (*types.Machine, error) {
	var machine *types.Machine
	err := inTenantTx(ctx, s.db, tenantID, func(tx *sql.Tx) error {
		var err error
		machine, err = scanMachine(tx.QueryRowContext(ctx,
			`SELECT `+machineColumns+` FROM machines WHERE tenant_id = $1 AND code = $2`,
			tenantID, code))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get machine by code: %w", err)
	}
	return machine, nil
}

// UpdateMachineHeartbeat updates the last heartbeat timestamp for a machine
// of tenantID.
func (s *Store) UpdateMachineHeartbeat(ctx context.Context, tenantID, machineID uuid.UUID) error {
	err := inTenantTx(ctx, s.db, tenantID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE machines SET last_heartbeat = $3, status = 'online' WHERE id = $1 AND tenant_id = $2`,
			machineID, tenantID, time.Now())
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}
	return nil
}

// MachineInfo is a lightweight machine info struct for the ack handler.
type MachineInfo struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Code     string
	Name     string
}

// GetMachineInfoByCode retrieves machine info by code for the tenant bound to
// ctx (tenantctx.WithTopicSegment). Unknown tenant or code returns nil, nil.
func (s *Store) GetMachineInfoByCode(ctx context.Context, code string) (*MachineInfo, error) {
	tenantID, err := s.tenantFromContext(ctx)
	if errors.Is(err, ErrNoTenant) {
		return nil, nil // unknown tenant: treated like an unknown machine
	}
	if err != nil {
		return nil, err
	}
	machine, err := s.getMachineByTenantAndCode(ctx, tenantID, code)
	if err != nil || machine == nil {
		return nil, err
	}
	return &MachineInfo{
		ID:       machine.ID,
		TenantID: machine.TenantID,
		Code:     machine.Code,
		Name:     machine.Name,
	}, nil
}

// UpdateCommandStatus updates the status of a task command of the tenant
// bound to ctx.
func (s *Store) UpdateCommandStatus(ctx context.Context, commandID uuid.UUID, status, message string) error {
	tenantID, err := s.tenantFromContext(ctx)
	if err != nil {
		return err
	}
	query := `
		UPDATE task_commands
		SET status = $2,
		    error_message = NULLIF($3, ''),
		    acked_at = CASE WHEN $2 = 'acknowledged' THEN NOW() ELSE acked_at END,
		    completed_at = CASE WHEN $2 IN ('completed', 'failed') THEN NOW() ELSE completed_at END
		WHERE command_id = $1 AND tenant_id = $4
	`
	err = inTenantTx(ctx, s.db, tenantID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, commandID, status, message, tenantID)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to update command status: %w", err)
	}
	return nil
}

// TaskCommandInfo contains task command information.
type TaskCommandInfo struct {
	ID          uuid.UUID
	TaskID      uuid.UUID
	TenantID    uuid.UUID
	MachineID   uuid.UUID
	CommandType string
}

// GetTaskCommandByCommandID retrieves task command info by command ID for
// the tenant bound to ctx.
func (s *Store) GetTaskCommandByCommandID(ctx context.Context, commandID uuid.UUID) (*TaskCommandInfo, error) {
	tenantID, err := s.tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	var info *TaskCommandInfo
	err = inTenantTx(ctx, s.db, tenantID, func(tx *sql.Tx) error {
		var i TaskCommandInfo
		err := tx.QueryRowContext(ctx, `
			SELECT id, task_id, tenant_id, machine_id, command_type
			FROM task_commands
			WHERE command_id = $1 AND tenant_id = $2
		`, commandID, tenantID).Scan(&i.ID, &i.TaskID, &i.TenantID, &i.MachineID, &i.CommandType)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		info = &i
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get task command: %w", err)
	}
	return info, nil
}

// UpdateTaskStatusOnJobComplete updates a task's status when a job completes,
// for the tenant bound to ctx.
func (s *Store) UpdateTaskStatusOnJobComplete(ctx context.Context, taskID uuid.UUID, newStatus string, completedAt time.Time) error {
	tenantID, err := s.tenantFromContext(ctx)
	if err != nil {
		return err
	}
	var rows int64
	err = inTenantTx(ctx, s.db, tenantID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET status = $2,
			    completed_at = $3,
			    updated_at = NOW()
			WHERE id = $1 AND tenant_id = $4
		`, taskID, newStatus, completedAt, tenantID)
		if err != nil {
			return err
		}
		rows, _ = result.RowsAffected()
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task not found: %s", taskID)
	}
	return nil
}

// GetMachineByID retrieves a machine by ID for the tenant bound to ctx.
func (s *Store) GetMachineByID(ctx context.Context, id uuid.UUID) (*types.Machine, error) {
	tenantID, err := s.tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	var machine *types.Machine
	err = inTenantTx(ctx, s.db, tenantID, func(tx *sql.Tx) error {
		var err error
		machine, err = scanMachine(tx.QueryRowContext(ctx,
			`SELECT `+machineColumns+` FROM machines WHERE id = $1 AND tenant_id = $2`,
			id, tenantID))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get machine: %w", err)
	}
	return machine, nil
}

// scanMachine scans one machine row; it returns nil, nil for no row.
func scanMachine(row *sql.Row) (*types.Machine, error) {
	var machine types.Machine
	var description, mqttTopic, location sql.NullString
	var lastHeartbeat sql.NullTime
	var specificationsJSON, metadataJSON []byte

	err := row.Scan(
		&machine.ID, &machine.TenantID, &machine.Name, &machine.Code,
		&machine.Type, &description, &machine.Status, &mqttTopic,
		&location, &specificationsJSON, &metadataJSON,
		&lastHeartbeat, &machine.CreatedAt, &machine.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if description.Valid {
		machine.Description = description.String
	}
	if mqttTopic.Valid {
		machine.MQTTTopic = mqttTopic.String
	}
	if location.Valid {
		machine.Location = location.String
	}
	if lastHeartbeat.Valid {
		machine.LastHeartbeat = &lastHeartbeat.Time
	}
	if len(specificationsJSON) > 0 {
		_ = json.Unmarshal(specificationsJSON, &machine.Specifications)
	}
	if len(metadataJSON) > 0 {
		_ = json.Unmarshal(metadataJSON, &machine.Metadata)
	}

	return &machine, nil
}
