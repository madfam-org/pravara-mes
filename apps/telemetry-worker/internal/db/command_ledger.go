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

// CommandLedger implements the command package's ledger interfaces on the
// task_commands table. Every state change runs in a tenant-scoped
// transaction together with the outbox events it produces.
type CommandLedger struct {
	db    *sql.DB
	scope TenantScope
}

// NewCommandLedger returns a ledger using per-transaction tenant context.
func NewCommandLedger(store *Store) *CommandLedger {
	return &CommandLedger{db: store.db, scope: TxTenantScope{DB: store.db}}
}

// NewCommandLedgerWithScope returns a ledger with a custom tenant scope.
func NewCommandLedgerWithScope(db *sql.DB, scope TenantScope) *CommandLedger {
	return &CommandLedger{db: db, scope: scope}
}

var (
	_ command.DispatchLedger = (*CommandLedger)(nil)
	_ command.AckLedger      = (*CommandLedger)(nil)
	_ command.DeadlineLedger = (*CommandLedger)(nil)
)

// LoadForDispatch implements command.DispatchLedger.
func (l *CommandLedger) LoadForDispatch(ctx context.Context, tenantID, commandID uuid.UUID) (*command.LedgerCommand, error) {
	var lc *command.LedgerCommand
	err := l.scope.WithTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var c command.LedgerCommand
		var taskID uuid.NullUUID
		err := tx.QueryRowContext(ctx, `
			SELECT tc.tenant_id, tc.command_id, tc.machine_id, tc.task_id, tc.command_type,
			       COALESCE(tc.status, 'pending'), tc.attempts, COALESCE(m.mqtt_topic, '')
			FROM task_commands tc
			JOIN machines m ON m.id = tc.machine_id AND m.tenant_id = tc.tenant_id
			WHERE tc.command_id = $1 AND tc.tenant_id = $2`,
			commandID, tenantID,
		).Scan(&c.TenantID, &c.CommandID, &c.MachineID, &taskID, &c.CommandType,
			&c.Status, &c.Attempts, &c.MachineTopic)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("load command: %w", err)
		}
		if taskID.Valid {
			c.TaskID = &taskID.UUID
		}
		lc = &c
		return nil
	})
	return lc, err
}

// MarkSent implements command.DispatchLedger.
func (l *CommandLedger) MarkSent(ctx context.Context, tenantID, commandID uuid.UUID, sentAt, deadline time.Time) error {
	return l.scope.WithTenant(ctx, tenantID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE task_commands
			SET attempts = attempts + 1,
			    sent_at = $3,
			    deadline_at = $4,
			    error_message = NULL,
			    status = CASE WHEN status = 'pending' THEN 'sent' ELSE status END
			WHERE command_id = $1 AND tenant_id = $2
			  AND status NOT IN ('completed', 'failed', 'timeout')`,
			commandID, tenantID, sentAt, deadline)
		if err != nil {
			return fmt.Errorf("mark command sent: %w", err)
		}
		return nil
	})
}

// RecordDispatchFailure implements command.DispatchLedger. It reports true
// when no further attempt must be made: the command is now failed, or it is
// no longer pending at all.
func (l *CommandLedger) RecordDispatchFailure(ctx context.Context, f command.DispatchFailure) (bool, error) {
	final := false
	err := l.scope.WithTenant(ctx, f.TenantID, func(tx *sql.Tx) error {
		var (
			status, cmdType string
			machineID       uuid.UUID
			taskID          uuid.NullUUID
			attempts        int
		)
		err := tx.QueryRowContext(ctx, `
			UPDATE task_commands
			SET attempts = attempts + 1,
			    error_message = $3,
			    status = CASE WHEN $4 OR attempts + 1 >= $5 THEN 'failed' ELSE status END,
			    completed_at = CASE WHEN $4 OR attempts + 1 >= $5 THEN NOW() ELSE completed_at END
			WHERE command_id = $1 AND tenant_id = $2 AND status = 'pending'
			RETURNING status, machine_id, task_id, command_type, attempts`,
			f.CommandID, f.TenantID, f.Error, f.Permanent, f.MaxAttempts,
		).Scan(&status, &machineID, &taskID, &cmdType, &attempts)
		if errors.Is(err, sql.ErrNoRows) {
			final = true // not pending any more: nothing left to retry
			return nil
		}
		if err != nil {
			return fmt.Errorf("record dispatch failure: %w", err)
		}
		if status != command.StatusFailed {
			return nil
		}
		final = true
		return l.writeFailureEvents(ctx, tx, failureEvent{
			tenantID:  f.TenantID,
			commandID: f.CommandID,
			machineID: machineID,
			taskID:    nullableUUID(taskID),
			cmdType:   cmdType,
			status:    command.StatusFailed,
			reason:    f.Error,
			attempts:  attempts,
		})
	})
	return final, err
}

// ResolveAckMachine implements command.AckLedger. The lookup precedes any
// tenant context: the tenant is derived from the machine it finds.
func (l *CommandLedger) ResolveAckMachine(ctx context.Context, topicBase, code string) (*command.AckMachine, error) {
	if m, err := l.uniqueMachine(ctx, `SELECT id, tenant_id, code, name FROM machines WHERE mqtt_topic = $1 LIMIT 2`, topicBase); err != nil || m != nil {
		return m, err
	}
	return l.uniqueMachine(ctx, `SELECT id, tenant_id, code, name FROM machines WHERE code = $1 LIMIT 2`, code)
}

func (l *CommandLedger) uniqueMachine(ctx context.Context, query, arg string) (*command.AckMachine, error) {
	if arg == "" {
		return nil, nil
	}
	rows, err := l.db.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("resolve ack machine: %w", err)
	}
	defer rows.Close()

	var found []command.AckMachine
	for rows.Next() {
		var m command.AckMachine
		if err := rows.Scan(&m.ID, &m.TenantID, &m.Code, &m.Name); err != nil {
			return nil, fmt.Errorf("resolve ack machine: %w", err)
		}
		found = append(found, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve ack machine: %w", err)
	}
	if len(found) != 1 {
		return nil, nil // none, or ambiguous: never guess
	}
	return &found[0], nil
}

// ApplyAck implements command.AckLedger.
func (l *CommandLedger) ApplyAck(ctx context.Context, a command.AckApplication) (*command.AckOutcome, error) {
	out := &command.AckOutcome{}
	err := l.scope.WithTenant(ctx, a.Machine.TenantID, func(tx *sql.Tx) error {
		var (
			machineID       uuid.UUID
			status, cmdType string
			taskID          uuid.NullUUID
			attempts        int
		)
		err := tx.QueryRowContext(ctx, `
			SELECT machine_id, COALESCE(status, 'pending'), task_id, command_type, attempts
			FROM task_commands
			WHERE command_id = $1 AND tenant_id = $2
			FOR UPDATE`,
			a.CommandID, a.Machine.TenantID,
		).Scan(&machineID, &status, &taskID, &cmdType, &attempts)
		if errors.Is(err, sql.ErrNoRows) {
			out.Disposition = command.AckUnknownCommand
			return nil
		}
		if err != nil {
			return fmt.Errorf("load command for ack: %w", err)
		}
		if machineID != a.Machine.ID {
			out.Disposition = command.AckMachineMismatch
			return nil
		}
		if command.IsTerminalStatus(status) {
			out.Disposition = command.AckAlreadyFinal
			return nil
		}

		newStatus := command.StatusAcknowledged
		switch {
		case !a.Success:
			newStatus = command.StatusFailed
		case a.JobCompleted:
			newStatus = command.StatusCompleted
		}
		message := a.Message
		if newStatus == command.StatusFailed && message == "" {
			message = "machine reported failure"
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE task_commands
			SET status = $3::varchar,
			    acked_at = COALESCE(acked_at, NOW()),
			    error_message = CASE WHEN $3::varchar = 'failed' THEN $4::text ELSE error_message END,
			    completed_at = CASE WHEN $3::varchar IN ('completed', 'failed') THEN NOW() ELSE completed_at END
			WHERE command_id = $1 AND tenant_id = $2`,
			a.CommandID, a.Machine.TenantID, newStatus, message,
		); err != nil {
			return fmt.Errorf("apply ack: %w", err)
		}
		out.Disposition = command.AckApplied
		out.Status = newStatus

		isTaskJob := cmdType == string(command.CommandStartJob) && taskID.Valid
		switch newStatus {
		case command.StatusFailed:
			return l.writeFailureEvents(ctx, tx, failureEvent{
				tenantID:    a.Machine.TenantID,
				commandID:   a.CommandID,
				machineID:   a.Machine.ID,
				machineName: a.Machine.Name,
				taskID:      nullableUUID(taskID),
				cmdType:     cmdType,
				status:      command.StatusFailed,
				reason:      message,
				attempts:    attempts,
			})
		case command.StatusCompleted:
			if !isTaskJob {
				return nil
			}
			completion, err := completeTaskJob(ctx, tx, a, taskID.UUID, cmdType)
			if err != nil {
				return err
			}
			out.Completion = completion
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListTenantIDs implements command.DeadlineLedger.
func (l *CommandLedger) ListTenantIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT id FROM tenants ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list tenants: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ExpireOverdue implements command.DeadlineLedger.
func (l *CommandLedger) ExpireOverdue(ctx context.Context, tenantID uuid.UUID, now, pendingCutoff time.Time, limit int) ([]command.ExpiredCommand, error) {
	var expired []command.ExpiredCommand
	err := l.scope.WithTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			WITH due AS (
				SELECT id, status AS previous_status
				FROM task_commands
				WHERE tenant_id = $1
				  AND ((status = 'sent' AND deadline_at < $2)
				    OR (status = 'pending' AND issued_at < $3))
				ORDER BY issued_at
				LIMIT $4
				FOR UPDATE SKIP LOCKED
			)
			UPDATE task_commands tc
			SET status = 'timeout',
			    completed_at = NOW(),
			    error_message = CASE WHEN due.previous_status = 'sent'
			                         THEN 'no acknowledgement before the deadline'
			                         ELSE 'not dispatched before the deadline' END
			FROM due
			WHERE tc.id = due.id
			RETURNING tc.command_id, tc.machine_id, tc.task_id, tc.command_type,
			          due.previous_status, tc.attempts, tc.error_message`,
			tenantID, now, pendingCutoff, limit)
		if err != nil {
			return fmt.Errorf("expire overdue commands: %w", err)
		}

		type row struct {
			e        command.ExpiredCommand
			attempts int
			reason   string
		}
		var due []row
		for rows.Next() {
			var r row
			var taskID uuid.NullUUID
			if err := rows.Scan(&r.e.CommandID, &r.e.MachineID, &taskID, &r.e.CommandType,
				&r.e.PreviousStatus, &r.attempts, &r.reason); err != nil {
				rows.Close()
				return fmt.Errorf("expire overdue commands: %w", err)
			}
			r.e.TenantID = tenantID
			r.e.TaskID = nullableUUID(taskID)
			due = append(due, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("expire overdue commands: %w", err)
		}

		for _, r := range due {
			if err := l.writeFailureEvents(ctx, tx, failureEvent{
				tenantID:  tenantID,
				commandID: r.e.CommandID,
				machineID: r.e.MachineID,
				taskID:    r.e.TaskID,
				cmdType:   r.e.CommandType,
				status:    command.StatusTimeout,
				reason:    r.reason,
				attempts:  r.attempts,
			}); err != nil {
				return err
			}
			expired = append(expired, r.e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return expired, nil
}

func nullableUUID(id uuid.NullUUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	v := id.UUID
	return &v
}
