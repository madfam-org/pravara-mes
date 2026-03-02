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
type TaskCommandRepository struct {
	db *sql.DB
}

// NewTaskCommandRepository creates a new task command repository.
func NewTaskCommandRepository(db *sql.DB) *TaskCommandRepository {
	return &TaskCommandRepository{db: db}
}

// Create inserts a new task command record.
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

// GetByCommandID retrieves a task command by its command_id.
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

// GetByTaskID retrieves all commands for a task (command history).
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

// Helper: scan a single row
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

// Helper: scan from rows
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
