package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/command"
	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/host"
)

// machine.status_changed (pravara-api pubsub.EventMachineStatusChanged).
const EventMachineStatusChanged = "machine.status_changed"

// MachineStatusData is the payload of machine.status_changed (pubsub.MachineStatusData).
type MachineStatusData struct {
	MachineID   uuid.UUID `json:"machine_id"`
	MachineName string    `json:"machine_name"`
	OldStatus   string    `json:"old_status,omitempty"`
	NewStatus   string    `json:"new_status"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// maxResend bounds the commands re-sent after one DBIRTH.
const maxResend = 20

// bindCommands applies DDATA/DBIRTH Command/* and Job/* metrics to the
// command ledger inside tx. A command is only ever changed when it was
// issued to this machine (the device topic's machine); Job/* binds to the
// open start_job of this machine whose task id (or command id) is Job/Id.
func (s *SparkplugStore) bindCommands(ctx context.Context, tx *sql.Tx, n host.EdgeNode, m command.AckMachine, r host.DeviceReport) ([]command.JobCompletion, error) {
	var completions []command.JobCompletion
	log := s.log.WithFields(logrus.Fields{"tenant_id": m.TenantID, "machine_id": m.ID, "device_id": r.DeviceID})

	if lastID, _ := r.Values[sparkplug.MetricCommandLastID].(string); lastID != "" {
		status, _ := r.Values[sparkplug.MetricCommandStatus].(string)
		if cmdID, err := uuid.Parse(lastID); err == nil && validCommandStatus[status] {
			var cmdType string
			err := tx.QueryRowContext(ctx,
				`SELECT command_type FROM task_commands WHERE command_id = $1 AND tenant_id = $2`,
				cmdID, m.TenantID).Scan(&cmdType)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				log.WithField("command_id", cmdID).Warn("Command/LastId names no command of this tenant")
			case err != nil:
				return nil, fmt.Errorf("bind command: %w", err)
			default:
				a := command.AckApplication{CommandID: cmdID, Machine: m, Success: status != "failed", AckedAt: r.ReceivedAt}
				if status == "failed" {
					a.Message, _ = r.Values[sparkplug.MetricCommandError].(string)
					if a.Message == "" {
						a.Message = "device reported failure"
					}
				}
				// A start_job is complete when its job is; other commands when done.
				a.JobCompleted = status == "done" && cmdType != string(command.CommandStartJob)
				out, err := s.ledger.applyAckTx(ctx, tx, a)
				if err != nil {
					return nil, err
				}
				if out.Disposition == command.AckMachineMismatch {
					log.WithField("command_id", cmdID).Warn("Command/* from a device the command was not issued to; ignored")
				}
				if out.Completion != nil {
					completions = append(completions, *out.Completion)
				}
			}
		}
	}

	jobStatus, _ := r.Values[sparkplug.MetricJobStatus].(string)
	jobID, _ := r.Values[sparkplug.MetricJobID].(string)
	switch sparkplug.JobStatus(jobStatus) {
	case sparkplug.JobComplete, sparkplug.JobFailed, sparkplug.JobCancelled:
	default:
		return completions, nil
	}
	if jobID == "" {
		return completions, nil
	}
	var cmdID uuid.UUID
	var taskID uuid.NullUUID
	err := tx.QueryRowContext(ctx, `
		SELECT command_id, task_id FROM task_commands
		WHERE tenant_id = $1 AND machine_id = $2 AND command_type = 'start_job'
		  AND status IN ('sent', 'acknowledged')
		  AND (command_id::text = $3 OR task_id::text = $3)
		ORDER BY issued_at DESC LIMIT 1`, m.TenantID, m.ID, jobID).Scan(&cmdID, &taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return completions, nil // already final, or not a job this host issued
	}
	if err != nil {
		return nil, fmt.Errorf("bind job: %w", err)
	}
	a := command.AckApplication{CommandID: cmdID, Machine: m, Success: true, JobCompleted: true, AckedAt: r.ReceivedAt}
	if jobStatus != string(sparkplug.JobComplete) {
		a.Success, a.JobCompleted = false, false
		a.Message = "job " + jobStatus + " on the device"
	}
	out, err := s.ledger.applyAckTx(ctx, tx, a)
	if err != nil {
		return nil, err
	}
	if out.Disposition != command.AckApplied || out.Status != command.StatusCompleted {
		return completions, nil
	}
	data := MachineJobCompletedData{
		MachineID: m.ID, MachineName: m.Name, MachineCode: m.Code, EdgeNodeID: n.EdgeNodeID,
		CommandID: cmdID, JobID: jobID, JobStatus: jobStatus, TaskID: nullableUUID(taskID),
		PrinterReportedAt: r.Timestamp, HostReceivedAt: r.ReceivedAt, RecordedAt: s.now(),
		BdSeq: r.BdSeq, Seq: r.Seq,
	}
	if t, ok := r.MetricTimes[sparkplug.MetricJobStatus]; ok {
		data.PrinterReportedAt = t
	}
	if out.Completion != nil {
		data.OrderID = out.Completion.OrderID
		completions = append(completions, *out.Completion)
	}
	if err := insertOutboxEvent(ctx, tx, m.TenantID, EventMachineJobCompleted, NamespaceMachines, data); err != nil {
		return nil, err
	}
	log.WithFields(logrus.Fields{"command_id": cmdID, "job_id": jobID}).Info("Sparkplug job completion recorded")
	return completions, nil
}

// unacknowledged lists the machine's commands that were sent but never
// acknowledged and are still within their deadline, oldest first, as DCMDs
// to re-send after a DBIRTH.
func (s *SparkplugStore) unacknowledged(ctx context.Context, tx *sql.Tx, tenantID, machineID uuid.UUID) ([]sparkplug.DeviceCommand, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT command_id, command_type, task_id, COALESCE(parameters, '{}'::jsonb)
		FROM task_commands
		WHERE tenant_id = $1 AND machine_id = $2 AND status = 'sent'
		  AND (deadline_at IS NULL OR deadline_at > NOW())
		ORDER BY issued_at
		LIMIT $3`, tenantID, machineID, maxResend)
	if err != nil {
		return nil, fmt.Errorf("unacknowledged commands: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []sparkplug.DeviceCommand
	for rows.Next() {
		var (
			id      uuid.UUID
			cmdType string
			taskID  uuid.NullUUID
			raw     []byte
		)
		if err := rows.Scan(&id, &cmdType, &taskID, &raw); err != nil {
			return nil, fmt.Errorf("unacknowledged commands: %w", err)
		}
		params := map[string]interface{}{}
		_ = json.Unmarshal(raw, &params)
		cmd, err := command.BuildSparkplugCommand(cmdType, id, nullableUUID(taskID), params)
		if err != nil {
			s.log.WithError(err).WithField("command_id", id).Warn("Sent command cannot be re-sent as DCMD")
			continue
		}
		out = append(out, cmd)
	}
	return out, rows.Err()
}
