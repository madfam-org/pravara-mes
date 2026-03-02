// Package repositories provides database access layer implementations.
package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// TaskRepository handles task database operations.
type TaskRepository struct {
	db *sql.DB
}

// NewTaskRepository creates a new task repository.
func NewTaskRepository(db *sql.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

// TaskFilter defines filtering options for listing tasks.
type TaskFilter struct {
	Status    *types.TaskStatus
	MachineID *uuid.UUID
	OrderID   *uuid.UUID
	UserID    *uuid.UUID
	Limit     int
	Offset    int
}

// List retrieves tasks matching the given filter with pagination.
// Results are ordered by status (for Kanban grouping) then by kanban_position.
// Returns the list of tasks, total count (for pagination), and any error encountered.
// An empty filter returns all tasks. Use filter.Limit and filter.Offset for pagination.
func (r *TaskRepository) List(ctx context.Context, filter TaskFilter) ([]types.Task, int, error) {
	query := `
		SELECT id, tenant_id, order_id, order_item_id, machine_id, assigned_user_id,
		       title, description, status, priority, estimated_minutes, actual_minutes,
		       kanban_position, started_at, completed_at, metadata, created_at, updated_at
		FROM tasks
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM tasks WHERE 1=1`

	var args []interface{}
	argIndex := 1

	if filter.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND status = $%d", argIndex)
		args = append(args, *filter.Status)
		argIndex++
	}

	if filter.MachineID != nil {
		query += fmt.Sprintf(" AND machine_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND machine_id = $%d", argIndex)
		args = append(args, *filter.MachineID)
		argIndex++
	}

	if filter.OrderID != nil {
		query += fmt.Sprintf(" AND order_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND order_id = $%d", argIndex)
		args = append(args, *filter.OrderID)
		argIndex++
	}

	if filter.UserID != nil {
		query += fmt.Sprintf(" AND assigned_user_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND assigned_user_id = $%d", argIndex)
		args = append(args, *filter.UserID)
		argIndex++
	}

	// Get total count
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count tasks: %w", err)
	}

	// Add ordering (by status for Kanban grouping, then by position)
	query += " ORDER BY status, kanban_position ASC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIndex)
		args = append(args, filter.Limit)
		argIndex++
	}

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIndex)
		args = append(args, filter.Offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer rows.Close()

	var tasks []types.Task
	for rows.Next() {
		task, err := r.scanTask(rows)
		if err != nil {
			return nil, 0, err
		}
		tasks = append(tasks, *task)
	}

	return tasks, total, nil
}

// GetByID retrieves a task by its unique identifier.
// Returns nil, nil if the task is not found (not an error condition).
// Returns nil, error if a database error occurs.
func (r *TaskRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.Task, error) {
	query := `
		SELECT id, tenant_id, order_id, order_item_id, machine_id, assigned_user_id,
		       title, description, status, priority, estimated_minutes, actual_minutes,
		       kanban_position, started_at, completed_at, metadata, created_at, updated_at
		FROM tasks
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	task, err := r.scanTaskRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	return task, nil
}

// Create inserts a new task into the database.
// If task.ID is nil, a new UUID is generated automatically.
// The task.KanbanPosition is automatically set to the next available position
// within its status column. CreatedAt and UpdatedAt are populated from the database.
func (r *TaskRepository) Create(ctx context.Context, task *types.Task) error {
	// Get the next kanban position for the status
	var maxPosition int
	posQuery := `SELECT COALESCE(MAX(kanban_position), 0) FROM tasks WHERE tenant_id = $1 AND status = $2`
	r.db.QueryRowContext(ctx, posQuery, task.TenantID, task.Status).Scan(&maxPosition)
	task.KanbanPosition = maxPosition + 1

	query := `
		INSERT INTO tasks (
			id, tenant_id, order_id, order_item_id, machine_id, assigned_user_id,
			title, description, status, priority, estimated_minutes,
			kanban_position, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING created_at, updated_at
	`

	if task.ID == uuid.Nil {
		task.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(task.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		task.ID, task.TenantID, nullUUID(task.OrderID), nullUUID(task.OrderItemID),
		nullUUID(task.MachineID), nullUUID(task.AssignedUserID),
		task.Title, task.Description, task.Status, task.Priority,
		task.EstimatedMinutes, task.KanbanPosition, metadataJSON,
	).Scan(&task.CreatedAt, &task.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}

	return nil
}

// Update modifies an existing task's mutable fields.
// The task.ID must exist in the database. The UpdatedAt field
// is refreshed from the database after successful update.
// Returns an error if the task is not found.
func (r *TaskRepository) Update(ctx context.Context, task *types.Task) error {
	query := `
		UPDATE tasks SET
			order_id = $2,
			order_item_id = $3,
			machine_id = $4,
			assigned_user_id = $5,
			title = $6,
			description = $7,
			status = $8,
			priority = $9,
			estimated_minutes = $10,
			actual_minutes = $11,
			kanban_position = $12,
			started_at = $13,
			completed_at = $14,
			metadata = $15
		WHERE id = $1
		RETURNING updated_at
	`

	metadataJSON, _ := json.Marshal(task.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		task.ID, nullUUID(task.OrderID), nullUUID(task.OrderItemID),
		nullUUID(task.MachineID), nullUUID(task.AssignedUserID),
		task.Title, task.Description, task.Status, task.Priority,
		task.EstimatedMinutes, task.ActualMinutes, task.KanbanPosition,
		task.StartedAt, task.CompletedAt, metadataJSON,
	).Scan(&task.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("task not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}

	return nil
}

// MoveTask updates the task's status and/or position for Kanban board operations.
// This method handles all the position recalculations needed when moving a card:
//   - Moving between columns: shifts positions in both old and new columns
//   - Moving within a column: shifts affected tasks up or down
//
// The operation is performed within a transaction to maintain data integrity.
// Returns an error if the task is not found.
func (r *TaskRepository) MoveTask(ctx context.Context, id uuid.UUID, newStatus types.TaskStatus, newPosition int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get the current task
	var currentStatus types.TaskStatus
	var currentPosition int
	var tenantID uuid.UUID

	err = tx.QueryRowContext(ctx,
		`SELECT tenant_id, status, kanban_position FROM tasks WHERE id = $1`, id,
	).Scan(&tenantID, &currentStatus, &currentPosition)
	if err == sql.ErrNoRows {
		return fmt.Errorf("task not found")
	}
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// If moving to a different status column
	if currentStatus != newStatus {
		// Shift positions in the old column
		_, err = tx.ExecContext(ctx,
			`UPDATE tasks SET kanban_position = kanban_position - 1
			 WHERE tenant_id = $1 AND status = $2 AND kanban_position > $3`,
			tenantID, currentStatus, currentPosition,
		)
		if err != nil {
			return fmt.Errorf("failed to shift old column: %w", err)
		}

		// Shift positions in the new column to make room
		_, err = tx.ExecContext(ctx,
			`UPDATE tasks SET kanban_position = kanban_position + 1
			 WHERE tenant_id = $1 AND status = $2 AND kanban_position >= $3`,
			tenantID, newStatus, newPosition,
		)
		if err != nil {
			return fmt.Errorf("failed to shift new column: %w", err)
		}
	} else {
		// Moving within the same column
		if newPosition > currentPosition {
			// Moving down: shift items between current and new position up
			_, err = tx.ExecContext(ctx,
				`UPDATE tasks SET kanban_position = kanban_position - 1
				 WHERE tenant_id = $1 AND status = $2
				 AND kanban_position > $3 AND kanban_position <= $4`,
				tenantID, currentStatus, currentPosition, newPosition,
			)
		} else if newPosition < currentPosition {
			// Moving up: shift items between new and current position down
			_, err = tx.ExecContext(ctx,
				`UPDATE tasks SET kanban_position = kanban_position + 1
				 WHERE tenant_id = $1 AND status = $2
				 AND kanban_position >= $3 AND kanban_position < $4`,
				tenantID, currentStatus, newPosition, currentPosition,
			)
		}
		if err != nil {
			return fmt.Errorf("failed to shift positions: %w", err)
		}
	}

	// Update the task's status and position
	_, err = tx.ExecContext(ctx,
		`UPDATE tasks SET status = $2, kanban_position = $3 WHERE id = $1`,
		id, newStatus, newPosition,
	)
	if err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}

	return tx.Commit()
}

// AssignTask assigns a task to a user and/or machine.
// Either userID or machineID can be nil to indicate no assignment.
// Passing nil for both clears all assignments.
// Returns an error if the task is not found.
func (r *TaskRepository) AssignTask(ctx context.Context, id uuid.UUID, userID, machineID *uuid.UUID) error {
	query := `UPDATE tasks SET assigned_user_id = $2, machine_id = $3 WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id, nullUUID(userID), nullUUID(machineID))
	if err != nil {
		return fmt.Errorf("failed to assign task: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task not found")
	}

	return nil
}

// Delete permanently removes a task from the database.
// This is a hard delete - the task record is not recoverable.
// Returns an error if the task is not found.
func (r *TaskRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM tasks WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task not found")
	}

	return nil
}

// GetKanbanBoard retrieves all tasks grouped by status for a Kanban board view.
// Returns a map where keys are TaskStatus values and values are slices of tasks.
// All status columns are initialized (even if empty) to ensure consistent UI rendering.
// Tasks within each column are ordered by their kanban_position.
func (r *TaskRepository) GetKanbanBoard(ctx context.Context) (map[types.TaskStatus][]types.Task, error) {
	tasks, _, err := r.List(ctx, TaskFilter{Limit: 1000})
	if err != nil {
		return nil, err
	}

	board := make(map[types.TaskStatus][]types.Task)
	for _, status := range []types.TaskStatus{
		types.TaskStatusBacklog,
		types.TaskStatusQueued,
		types.TaskStatusInProgress,
		types.TaskStatusQualityCheck,
		types.TaskStatusCompleted,
		types.TaskStatusBlocked,
	} {
		board[status] = []types.Task{}
	}

	for _, task := range tasks {
		board[task.Status] = append(board[task.Status], task)
	}

	return board, nil
}

// Helper functions

func (r *TaskRepository) scanTask(rows *sql.Rows) (*types.Task, error) {
	var task types.Task
	var orderID, orderItemID, machineID, assignedUserID sql.NullString
	var description sql.NullString
	var estimatedMinutes, actualMinutes sql.NullInt64
	var startedAt, completedAt sql.NullTime
	var metadataJSON []byte

	err := rows.Scan(
		&task.ID, &task.TenantID, &orderID, &orderItemID, &machineID, &assignedUserID,
		&task.Title, &description, &task.Status, &task.Priority,
		&estimatedMinutes, &actualMinutes, &task.KanbanPosition,
		&startedAt, &completedAt, &metadataJSON, &task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan task: %w", err)
	}

	if orderID.Valid {
		id, _ := uuid.Parse(orderID.String)
		task.OrderID = &id
	}
	if orderItemID.Valid {
		id, _ := uuid.Parse(orderItemID.String)
		task.OrderItemID = &id
	}
	if machineID.Valid {
		id, _ := uuid.Parse(machineID.String)
		task.MachineID = &id
	}
	if assignedUserID.Valid {
		id, _ := uuid.Parse(assignedUserID.String)
		task.AssignedUserID = &id
	}
	if description.Valid {
		task.Description = description.String
	}
	if estimatedMinutes.Valid {
		task.EstimatedMinutes = int(estimatedMinutes.Int64)
	}
	if actualMinutes.Valid {
		task.ActualMinutes = int(actualMinutes.Int64)
	}
	if startedAt.Valid {
		task.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		task.CompletedAt = &completedAt.Time
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &task.Metadata)
	}

	return &task, nil
}

func (r *TaskRepository) scanTaskRow(row *sql.Row) (*types.Task, error) {
	var task types.Task
	var orderID, orderItemID, machineID, assignedUserID sql.NullString
	var description sql.NullString
	var estimatedMinutes, actualMinutes sql.NullInt64
	var startedAt, completedAt sql.NullTime
	var metadataJSON []byte

	err := row.Scan(
		&task.ID, &task.TenantID, &orderID, &orderItemID, &machineID, &assignedUserID,
		&task.Title, &description, &task.Status, &task.Priority,
		&estimatedMinutes, &actualMinutes, &task.KanbanPosition,
		&startedAt, &completedAt, &metadataJSON, &task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if orderID.Valid {
		id, _ := uuid.Parse(orderID.String)
		task.OrderID = &id
	}
	if orderItemID.Valid {
		id, _ := uuid.Parse(orderItemID.String)
		task.OrderItemID = &id
	}
	if machineID.Valid {
		id, _ := uuid.Parse(machineID.String)
		task.MachineID = &id
	}
	if assignedUserID.Valid {
		id, _ := uuid.Parse(assignedUserID.String)
		task.AssignedUserID = &id
	}
	if description.Valid {
		task.Description = description.String
	}
	if estimatedMinutes.Valid {
		task.EstimatedMinutes = int(estimatedMinutes.Int64)
	}
	if actualMinutes.Valid {
		task.ActualMinutes = int(actualMinutes.Int64)
	}
	if startedAt.Valid {
		task.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		task.CompletedAt = &completedAt.Time
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &task.Metadata)
	}

	return &task, nil
}

func nullUUID(id *uuid.UUID) interface{} {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	return *id
}
