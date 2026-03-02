// Package repositories provides database access layer implementations.
package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TaskCommand represents a command dispatched from a task to a machine.
// Commands enable the control system to orchestrate machine operations by
// sending job control instructions (start, pause, stop) with parameters.
// Each command is tracked through its lifecycle: pending → sent → acknowledged → completed/failed.
type TaskCommand struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	TaskID       uuid.UUID
	MachineID    uuid.UUID
	CommandID    uuid.UUID
	CommandType  string
	Status       string // pending, sent, acknowledged, failed, completed
	Parameters   map[string]interface{}
	IssuedBy     *uuid.UUID
	IssuedAt     time.Time
	AckedAt      *time.Time
	CompletedAt  *time.Time
	ErrorMessage *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// TaskCommandRepository handles task_commands database operations.
// This repository manages the command queue between tasks and machines,
// providing command dispatch tracking and status management for machine control.
type TaskCommandRepository struct {
	db *sql.DB
}

// NewTaskCommandRepository creates a new task command repository.
// The returned repository is ready to perform database operations
// for task command management and tracking.
func NewTaskCommandRepository(db *sql.DB) *TaskCommandRepository {
	return &TaskCommandRepository{db: db}
}

// Create inserts a new task command record into the database.
// If cmd.ID is nil, a new UUID is generated automatically.
// The command status should initially be 'pending' and will transition through
// the states: pending → sent → acknowledged → completed/failed.
// Parameters are marshaled to JSON for storage. Empty parameters default to {}.
// Returns an error if the insertion fails.
func (r *TaskCommandRepository) Create(ctx context.Context, cmd *TaskCommand) error {
	query := `
		INSERT INTO task_commands (
			id, tenant_id, task_id, machine_id, command_id, command_type,
			status, parameters, issued_by, issued_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at, updated_at
	`

	if cmd.ID == uuid.Nil {
		cmd.ID = uuid.New()
	}

	paramsJSON, err := json.Marshal(cmd.Parameters)
	if err != nil {
		paramsJSON = []byte("{}")
	}

	err = r.db.QueryRowContext(ctx, query,
		cmd.ID, cmd.TenantID, cmd.TaskID, cmd.MachineID, cmd.CommandID,
		cmd.CommandType, cmd.Status, paramsJSON, nullUUID(cmd.IssuedBy), cmd.IssuedAt,
	).Scan(&cmd.CreatedAt, &cmd.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create task command: %w", err)
	}

	return nil
}

// UpdateStatus updates the status of a command by its command_id.
// Valid status transitions:
//   - pending → sent: Command dispatched to MQTT
//   - sent → acknowledged: Machine received the command
//   - acknowledged → completed: Command executed successfully
//   - any → failed: Command execution failed
// Automatically sets acked_at timestamp when status is 'acknowledged',
// and completed_at when status is 'completed' or 'failed'.
// Returns an error if the command is not found or update fails.
func (r *TaskCommandRepository) UpdateStatus(ctx context.Context, commandID uuid.UUID, status, errorMsg string) error {
	query := `
		UPDATE task_commands
		SET status = $2,
		    error_message = NULLIF($3, ''),
		    acked_at = CASE WHEN $2 = 'acknowledged' THEN NOW() ELSE acked_at END,
		    completed_at = CASE WHEN $2 IN ('completed', 'failed') THEN NOW() ELSE completed_at END
		WHERE command_id = $1
	`

	result, err := r.db.ExecContext(ctx, query, commandID, status, errorMsg)
	if err != nil {
		return fmt.Errorf("failed to update command status: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("command not found: %s", commandID)
	}

	return nil
}

// GetByCommandID retrieves a task command by its unique command_id.
// The command_id is used for correlation between the API and machine responses.
// Returns nil, nil if the command is not found (not an error condition).
// Returns nil, error if a database error occurs.
func (r *TaskCommandRepository) GetByCommandID(ctx context.Context, commandID uuid.UUID) (*TaskCommand, error) {
	query := `
		SELECT id, tenant_id, task_id, machine_id, command_id, command_type,
		       status, parameters, issued_by, issued_at, acked_at, completed_at,
		       error_message, created_at, updated_at
		FROM task_commands
		WHERE command_id = $1
	`

	row := r.db.QueryRowContext(ctx, query, commandID)
	cmd, err := r.scanTaskCommandRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get task command: %w", err)
	}

	return cmd, nil
}

// GetActiveByTaskID retrieves the most recent active command for a task.
// Active commands are those with status: 'pending', 'sent', or 'acknowledged'.
// This is useful for checking if a task already has a command in progress
// before issuing a new command.
// Returns nil, nil if no active command exists.
// Results are ordered by creation time, most recent first.
func (r *TaskCommandRepository) GetActiveByTaskID(ctx context.Context, taskID uuid.UUID) (*TaskCommand, error) {
	query := `
		SELECT id, tenant_id, task_id, machine_id, command_id, command_type,
		       status, parameters, issued_by, issued_at, acked_at, completed_at,
		       error_message, created_at, updated_at
		FROM task_commands
		WHERE task_id = $1 AND status IN ('pending', 'sent', 'acknowledged')
		ORDER BY created_at DESC
		LIMIT 1
	`

	row := r.db.QueryRowContext(ctx, query, taskID)
	cmd, err := r.scanTaskCommandRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active task command: %w", err)
	}

	return cmd, nil
}

// GetActiveByMachineID retrieves all active commands for a machine.
// Active commands are those with status: 'pending', 'sent', or 'acknowledged'.
// This enables the system to track all pending operations for a specific machine
// and manage command queue depth.
// Returns an empty slice if no active commands exist.
// Results are ordered by creation time, most recent first.
func (r *TaskCommandRepository) GetActiveByMachineID(ctx context.Context, machineID uuid.UUID) ([]TaskCommand, error) {
	query := `
		SELECT id, tenant_id, task_id, machine_id, command_id, command_type,
		       status, parameters, issued_by, issued_at, acked_at, completed_at,
		       error_message, created_at, updated_at
		FROM task_commands
		WHERE machine_id = $1 AND status IN ('pending', 'sent', 'acknowledged')
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, machineID)
	if err != nil {
		return nil, fmt.Errorf("failed to query active commands: %w", err)
	}
	defer rows.Close()

	var commands []TaskCommand
	for rows.Next() {
		cmd, err := r.scanTaskCommand(rows)
		if err != nil {
			return nil, err
		}
		commands = append(commands, *cmd)
	}

	return commands, nil
}

// GetByTaskID retrieves the complete command history for a task.
// This includes all commands regardless of status, useful for:
//   - Auditing task execution history
//   - Debugging command failures
//   - Analyzing task-machine interaction patterns
// Returns an empty slice if no commands exist for the task.
// Results are ordered by creation time, most recent first.
func (r *TaskCommandRepository) GetByTaskID(ctx context.Context, taskID uuid.UUID) ([]TaskCommand, error) {
	query := `
		SELECT id, tenant_id, task_id, machine_id, command_id, command_type,
		       status, parameters, issued_by, issued_at, acked_at, completed_at,
		       error_message, created_at, updated_at
		FROM task_commands
		WHERE task_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to query task commands: %w", err)
	}
	defer rows.Close()

	var commands []TaskCommand
	for rows.Next() {
		cmd, err := r.scanTaskCommand(rows)
		if err != nil {
			return nil, err
		}
		commands = append(commands, *cmd)
	}

	return commands, nil
}

// scanTaskCommandRow is a helper that scans a single database row into a TaskCommand.
// Handles nullable fields (IssuedBy, AckedAt, CompletedAt, ErrorMessage) and
// unmarshals the JSON parameters field.
func (r *TaskCommandRepository) scanTaskCommandRow(row *sql.Row) (*TaskCommand, error) {
	var cmd TaskCommand
	var issuedBy sql.NullString
	var ackedAt, completedAt sql.NullTime
	var errorMessage sql.NullString
	var paramsJSON []byte

	err := row.Scan(
		&cmd.ID, &cmd.TenantID, &cmd.TaskID, &cmd.MachineID, &cmd.CommandID,
		&cmd.CommandType, &cmd.Status, &paramsJSON, &issuedBy, &cmd.IssuedAt,
		&ackedAt, &completedAt, &errorMessage, &cmd.CreatedAt, &cmd.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if issuedBy.Valid {
		id, _ := uuid.Parse(issuedBy.String)
		cmd.IssuedBy = &id
	}
	if ackedAt.Valid {
		cmd.AckedAt = &ackedAt.Time
	}
	if completedAt.Valid {
		cmd.CompletedAt = &completedAt.Time
	}
	if errorMessage.Valid {
		cmd.ErrorMessage = &errorMessage.String
	}
	if len(paramsJSON) > 0 {
		json.Unmarshal(paramsJSON, &cmd.Parameters)
	}

	return &cmd, nil
}

// scanTaskCommand is a helper that scans from sql.Rows into a TaskCommand.
// Handles nullable fields and unmarshals JSON parameters.
// Used for queries returning multiple rows.
func (r *TaskCommandRepository) scanTaskCommand(rows *sql.Rows) (*TaskCommand, error) {
	var cmd TaskCommand
	var issuedBy sql.NullString
	var ackedAt, completedAt sql.NullTime
	var errorMessage sql.NullString
	var paramsJSON []byte

	err := rows.Scan(
		&cmd.ID, &cmd.TenantID, &cmd.TaskID, &cmd.MachineID, &cmd.CommandID,
		&cmd.CommandType, &cmd.Status, &paramsJSON, &issuedBy, &cmd.IssuedAt,
		&ackedAt, &completedAt, &errorMessage, &cmd.CreatedAt, &cmd.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan task command: %w", err)
	}

	if issuedBy.Valid {
		id, _ := uuid.Parse(issuedBy.String)
		cmd.IssuedBy = &id
	}
	if ackedAt.Valid {
		cmd.AckedAt = &ackedAt.Time
	}
	if completedAt.Valid {
		cmd.CompletedAt = &completedAt.Time
	}
	if errorMessage.Valid {
		cmd.ErrorMessage = &errorMessage.String
	}
	if len(paramsJSON) > 0 {
		json.Unmarshal(paramsJSON, &cmd.Parameters)
	}

	return &cmd, nil
}
