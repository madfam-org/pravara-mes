package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/command"
)

// failureEvent describes a command that became failed or timed out.
type failureEvent struct {
	tenantID    uuid.UUID
	commandID   uuid.UUID
	machineID   uuid.UUID
	machineName string
	taskID      *uuid.UUID
	cmdType     string
	status      string // failed | timeout
	reason      string
	attempts    int
}

// writeFailureEvents writes machine.command_failed and, for a task-linked
// start_job, task.job_failed to the outbox inside tx.
func (l *CommandLedger) writeFailureEvents(ctx context.Context, tx *sql.Tx, f failureEvent) error {
	now := time.Now().UTC()
	if f.machineName == "" {
		if err := tx.QueryRowContext(ctx,
			`SELECT name FROM machines WHERE id = $1 AND tenant_id = $2`,
			f.machineID, f.tenantID,
		).Scan(&f.machineName); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failure events: machine name: %w", err)
		}
	}

	if err := insertOutboxEvent(ctx, tx, f.tenantID, EventMachineCommandFailed, NamespaceMachines, CommandFailedData{
		CommandID:   f.commandID,
		MachineID:   f.machineID,
		MachineName: f.machineName,
		CommandType: f.cmdType,
		Status:      f.status,
		Reason:      f.reason,
		TaskID:      f.taskID,
		Attempts:    f.attempts,
		FailedAt:    now,
	}); err != nil {
		return err
	}

	if f.taskID == nil || f.cmdType != string(command.CommandStartJob) {
		return nil
	}

	var title string
	if err := tx.QueryRowContext(ctx,
		`SELECT title FROM tasks WHERE id = $1 AND tenant_id = $2`,
		*f.taskID, f.tenantID,
	).Scan(&title); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failure events: task title: %w", err)
	}

	return insertOutboxEvent(ctx, tx, f.tenantID, EventTaskJobFailed, NamespaceTasks, TaskJobData{
		TaskID:       *f.taskID,
		TaskTitle:    title,
		CommandID:    f.commandID,
		MachineID:    f.machineID,
		MachineName:  f.machineName,
		CommandType:  f.cmdType,
		Status:       "failed",
		ErrorMessage: f.reason,
		Timestamp:    now,
	})
}

// markOrderInProgressSQL is the order roll-up rule for a task that reached
// quality_check: the order advances to in_progress if it is still in a
// pre-production status. It is the same guarded statement as pravara-api's
// OrderRepository.MarkInProgressIfStarted (used by OrderRollupService), with
// an explicit tenant predicate; keep the two in step.
const markOrderInProgressSQL = `
	WITH prev AS (
		SELECT id, status FROM orders WHERE id = $1 AND tenant_id = $2 FOR UPDATE
	)
	UPDATE orders o
	SET status = 'in_progress'
	FROM prev
	WHERE o.id = prev.id
	  AND prev.status IN ('received', 'validated', 'scheduled')
	RETURNING prev.status`

// completeTaskJob applies a start_job completion inside tx: the task moves
// to quality_check (unchanged behaviour), the order roll-up runs, and
// task.job_completed (plus order.status_changed when the order advanced) is
// written to the outbox.
func completeTaskJob(ctx context.Context, tx *sql.Tx, a command.AckApplication, taskID uuid.UUID, cmdType string) (*command.JobCompletion, error) {
	tenantID := a.Machine.TenantID

	var (
		title   string
		orderID uuid.NullUUID
	)
	err := tx.QueryRowContext(ctx, `
		UPDATE tasks
		SET status = 'quality_check',
		    completed_at = $3,
		    updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING title, order_id`,
		taskID, tenantID, a.AckedAt,
	).Scan(&title, &orderID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("job completion: task %s not found in tenant", taskID)
	}
	if err != nil {
		return nil, fmt.Errorf("job completion: move task: %w", err)
	}

	completion := &command.JobCompletion{
		TenantID:    tenantID,
		CommandID:   a.CommandID,
		MachineID:   a.Machine.ID,
		MachineName: a.Machine.Name,
		TaskID:      taskID,
		OrderID:     nullableUUID(orderID),
		CompletedAt: a.AckedAt,
	}

	if err := insertOutboxEvent(ctx, tx, tenantID, EventTaskJobCompleted, NamespaceTasks, TaskJobData{
		TaskID:      taskID,
		TaskTitle:   title,
		CommandID:   a.CommandID,
		MachineID:   a.Machine.ID,
		MachineName: a.Machine.Name,
		CommandType: cmdType,
		Status:      "completed",
		Timestamp:   a.AckedAt,
	}); err != nil {
		return nil, err
	}

	if !orderID.Valid {
		return completion, nil
	}

	var previous string
	err = tx.QueryRowContext(ctx, markOrderInProgressSQL, orderID.UUID, tenantID).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		return completion, nil // already at or past in_progress
	}
	if err != nil {
		return nil, fmt.Errorf("job completion: order roll-up: %w", err)
	}
	completion.OrderRolledUp = true

	if err := insertOutboxEvent(ctx, tx, tenantID, EventOrderStatusChanged, NamespaceOrders, OrderStatusData{
		OrderID:   orderID.UUID,
		OldStatus: previous,
		NewStatus: "in_progress",
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		return nil, err
	}
	return completion, nil
}
