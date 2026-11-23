// Package repositories provides database access layer implementations.
package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// TaskRepository handles task database operations.
type TaskRepository struct {
	db DBTX
}

// NewTaskRepository creates a new task repository.
func NewTaskRepository(db DBTX) *TaskRepository {
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
		WHERE 1=1 AND ` + tenantMatch + `
	`
	countQuery := `SELECT COUNT(*) FROM tasks WHERE 1=1 AND ` + tenantMatch

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
		WHERE id = $1 AND ` + tenantMatch + `
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
		WHERE id = $1 AND ` + tenantMatch + `
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

// MoveTask updates the task's status and position for Kanban board
// operations. It is MoveTaskExpecting with a position and no expected status.
func (r *TaskRepository) MoveTask(ctx context.Context, id uuid.UUID, newStatus types.TaskStatus, newPosition int) error {
	_, err := r.MoveTaskExpecting(ctx, id, newStatus, &newPosition, nil)
	return err
}

// ErrTaskNotFound is returned by MoveTaskExpecting for a task the tenant
// does not have. Its text is the "task not found" callers already match.
var ErrTaskNotFound = errors.New("task not found")

// TaskBoardLockTimeout bounds each of a move's two lock waits: for the
// tenant's board lock, then for the task's row. A move that answers
// ErrTaskBoardBusy has waited at most twice this long.
const TaskBoardLockTimeout = 5 * time.Second

// ErrTaskBoardBusy: another move held the board or the task for longer than
// TaskBoardLockTimeout. Nothing changed; the caller answers with a retryable
// status.
var ErrTaskBoardBusy = errors.New("the task board is busy with another move")

// isLockTimeout reports PostgreSQL's lock_not_available (55P03), raised when
// lock_timeout expires.
func isLockTimeout(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && string(pqErr.Code) == "55P03"
}

// TaskStatusConflictError is returned by MoveTaskExpecting when the task is
// not in the status the caller said it leaves. Nothing was changed.
type TaskStatusConflictError struct {
	Current types.TaskStatus
}

func (e *TaskStatusConflictError) Error() string {
	return fmt.Sprintf("task is %s, not in the expected status", e.Current)
}

// TaskMove is what a move changed.
type TaskMove struct {
	OldStatus   types.TaskStatus
	OldPosition int
	NewStatus   types.TaskStatus
	NewPosition int
}

// MoveTaskExpecting moves a task to newStatus and recalculates positions:
//   - Moving between columns: shifts positions in both old and new columns
//   - Moving within a column: shifts affected tasks up or down
//
// newPosition nil puts the task at the end of the target column (a caller
// that does not know the board, such as a shop-floor terminal). expected,
// when not nil, is the status the caller says the task leaves: the task's
// row is locked, and when its status differs the move is refused with
// *TaskStatusConflictError and nothing changes (RFC 0064 §4.1, ruling PV-5).
//
// Moves on one tenant's board run one at a time (a transaction-scoped
// advisory lock taken first; waiting for it, or for the task's row, is
// bounded by TaskBoardLockTimeout, then ErrTaskBoardBusy), so two moves to the end of a column get
// distinct positions. The task's row is locked FOR NO KEY UPDATE, not FOR
// UPDATE: rows that reference the task (the durable machine command that
// automation writes in its own transaction while this one is open) take FOR
// KEY SHARE on it, which FOR UPDATE would block until this transaction ends,
// and this transaction waits for that write.
//
// The operation is performed within a transaction to maintain data integrity.
// Returns ErrTaskNotFound if the task is not found.
func (r *TaskRepository) MoveTaskExpecting(ctx context.Context, id uuid.UUID, newStatus types.TaskStatus, newPosition *int, expected *types.TaskStatus) (*TaskMove, error) {
	var move *TaskMove
	err := db.InTx(ctx, r.db, func(tx DBTX) error {
		var err error

		// The two lock waits below are bounded by TaskBoardLockTimeout, for
		// these statements only; the setting is put back afterwards.
		var previousTimeout string
		if err = tx.QueryRowContext(ctx, `SELECT current_setting('lock_timeout')`).Scan(&previousTimeout); err != nil {
			return fmt.Errorf("failed to read lock_timeout: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `SELECT set_config('lock_timeout', $1, true)`,
			fmt.Sprintf("%dms", TaskBoardLockTimeout.Milliseconds())); err != nil {
			return fmt.Errorf("failed to set lock_timeout: %w", err)
		}

		// One move at a time on this tenant's board.
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(
			'pravara.task_board:' || COALESCE(current_setting('app.current_tenant_id', true), ''), 0))`); err != nil {
			if isLockTimeout(err) {
				return ErrTaskBoardBusy
			}
			return fmt.Errorf("failed to lock the task board: %w", err)
		}

		// Get the current task, locked so the expected status holds until
		// the move is written.
		var currentStatus types.TaskStatus
		var currentPosition int
		var tenantID uuid.UUID

		err = tx.QueryRowContext(ctx,
			`SELECT tenant_id, status, kanban_position FROM tasks WHERE id = $1 AND `+tenantMatch+` FOR NO KEY UPDATE`, id,
		).Scan(&tenantID, &currentStatus, &currentPosition)
		if isLockTimeout(err) {
			return ErrTaskBoardBusy
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			// The statement failed and the savepoint is aborted; its
			// rollback also reverts lock_timeout, so nothing to restore.
			return fmt.Errorf("failed to get task: %w", err)
		}
		if _, rerr := tx.ExecContext(ctx, `SELECT set_config('lock_timeout', $1, true)`, previousTimeout); rerr != nil {
			return fmt.Errorf("failed to restore lock_timeout: %w", rerr)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTaskNotFound
		}
		if expected != nil && currentStatus != *expected {
			return &TaskStatusConflictError{Current: currentStatus}
		}

		position := 0
		if newPosition != nil {
			position = *newPosition
		} else {
			// The end of the target column. Within the same column the end
			// is the last position, which the task may already hold.
			var last int
			if err = tx.QueryRowContext(ctx,
				`SELECT COALESCE(MAX(kanban_position), 0) FROM tasks WHERE tenant_id = $1 AND status = $2`,
				tenantID, newStatus,
			).Scan(&last); err != nil {
				return fmt.Errorf("failed to find the end of the column: %w", err)
			}
			position = last + 1
			if currentStatus == newStatus {
				position = last
			}
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
				tenantID, newStatus, position,
			)
			if err != nil {
				return fmt.Errorf("failed to shift new column: %w", err)
			}
		} else {
			// Moving within the same column
			if position > currentPosition {
				// Moving down: shift items between current and new position up
				_, err = tx.ExecContext(ctx,
					`UPDATE tasks SET kanban_position = kanban_position - 1
				 WHERE tenant_id = $1 AND status = $2
				 AND kanban_position > $3 AND kanban_position <= $4`,
					tenantID, currentStatus, currentPosition, position,
				)
			} else if position < currentPosition {
				// Moving up: shift items between new and current position down
				_, err = tx.ExecContext(ctx,
					`UPDATE tasks SET kanban_position = kanban_position + 1
				 WHERE tenant_id = $1 AND status = $2
				 AND kanban_position >= $3 AND kanban_position < $4`,
					tenantID, currentStatus, position, currentPosition,
				)
			}
			if err != nil {
				return fmt.Errorf("failed to shift positions: %w", err)
			}
		}

		// Update the task's status and position
		_, err = tx.ExecContext(ctx,
			`UPDATE tasks SET status = $2, kanban_position = $3 WHERE id = $1 AND `+tenantMatch,
			id, newStatus, position,
		)
		if err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		move = &TaskMove{OldStatus: currentStatus, OldPosition: currentPosition, NewStatus: newStatus, NewPosition: position}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return move, nil
}

// AssignTask assigns a task to a user and/or machine.
// Either userID or machineID can be nil to indicate no assignment.
// Passing nil for both clears all assignments.
// Returns an error if the task is not found.
func (r *TaskRepository) AssignTask(ctx context.Context, id uuid.UUID, userID, machineID *uuid.UUID) error {
	query := `UPDATE tasks SET assigned_user_id = $2, machine_id = $3 WHERE id = $1 AND ` + tenantMatch

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
	query := `DELETE FROM tasks WHERE id = $1 AND ` + tenantMatch

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
