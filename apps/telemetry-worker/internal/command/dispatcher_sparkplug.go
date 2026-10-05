package command

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/observability"
	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/host"
)

// SparkplugSender delivers a command to a Sparkplug device: the primary host
// application (host.Engine).
type SparkplugSender interface {
	SendDeviceCommand(t host.Target, cmd sparkplug.DeviceCommand) (host.Delivery, error)
}

// SetSparkplugSender enables DCMD delivery for Sparkplug-registered machines.
func (d *Dispatcher) SetSparkplugSender(s SparkplugSender) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sparkplug = s
}

// Parameter keys of a start_job command that carry the artifact (MES-1 §1
// Command/Artifact/*). The dispatching side puts them in
// MachineCommand.Parameters and task_commands.parameters.
const (
	ParamArtifactURL       = "artifact_url"
	ParamArtifactSHA256    = "artifact_sha256"
	ParamArtifactMediaType = "artifact_media_type"
)

// sparkplugCommandNames maps ledger command types to MES-1 Command/Name.
var sparkplugCommandNames = map[string]sparkplug.CommandName{
	string(CommandStartJob): sparkplug.CommandStartJob,
	string(CommandPause):    sparkplug.CommandPause,
	string(CommandResume):   sparkplug.CommandResume,
	string(CommandStop):     sparkplug.CommandCancel,
	"cancel":                sparkplug.CommandCancel,
}

// BuildSparkplugCommand turns a ledger command into a MES-1 DCMD. Command
// types without a MES-1 equivalent are an error. Job/Id on the edge is the
// task id when one is given, else the command id.
func BuildSparkplugCommand(commandType string, commandID uuid.UUID, taskID *uuid.UUID, params map[string]interface{}) (sparkplug.DeviceCommand, error) {
	name, ok := sparkplugCommandNames[commandType]
	if !ok {
		return sparkplug.DeviceCommand{}, fmt.Errorf("command %q is not supported by Sparkplug devices", commandType)
	}
	cmd := sparkplug.DeviceCommand{ID: commandID.String(), Name: name}
	if taskID != nil && *taskID != uuid.Nil {
		cmd.TaskID = taskID.String()
	}
	if name == sparkplug.CommandStartJob {
		cmd.ArtifactURL, _ = params[ParamArtifactURL].(string)
		cmd.ArtifactSHA256, _ = params[ParamArtifactSHA256].(string)
		cmd.ArtifactMediaType, _ = params[ParamArtifactMediaType].(string)
	}
	if err := cmd.Validate(); err != nil {
		return sparkplug.DeviceCommand{}, err
	}
	return cmd, nil
}

// dispatchSparkplug sends one ledger command as a DCMD. A device that is not
// born in the host's current session gets the command after its next DBIRTH
// (the host re-sends sent, unacknowledged commands with the same id); the
// ack deadline applies either way.
func (d *Dispatcher) dispatchSparkplug(ctx context.Context, log *logrus.Entry, entryID string, tenantID uuid.UUID, cmd *MachineCommand, lc *LedgerCommand) {
	d.mu.Lock()
	sender := d.sparkplug
	d.mu.Unlock()

	var permanent string
	switch {
	case lc.MachineID != cmd.MachineID:
		permanent = "stream entry machine does not match the command ledger"
	case lc.Attempts >= d.cfg.MaxAttempts:
		permanent = "dispatch attempts exhausted"
	case sender == nil:
		permanent = "machine is a Sparkplug device but the primary host is not enabled"
	}
	if permanent != "" {
		d.recordFailure(ctx, log, entryID, tenantID, cmd.CommandID, permanent, true)
		return
	}

	dcmd, err := BuildSparkplugCommand(lc.CommandType, cmd.CommandID, lc.TaskID, cmd.Parameters)
	if err != nil {
		d.recordFailure(ctx, log, entryID, tenantID, cmd.CommandID, err.Error(), true)
		return
	}
	target := host.Target{Group: lc.TenantSlug, EdgeNodeID: lc.SparkplugEdgeID, DeviceID: lc.MachineCode}
	delivery, err := sender.SendDeviceCommand(target, dcmd)
	if err != nil {
		d.recordFailure(ctx, log, entryID, tenantID, cmd.CommandID, "sparkplug DCMD failed: "+err.Error(), false)
		return
	}

	sentAt := d.now()
	if err := d.ledger.MarkSent(ctx, tenantID, cmd.CommandID, sentAt, sentAt.Add(d.cfg.AckTimeout)); err != nil {
		log.WithError(err).Error("DCMD handed to the host but ledger update failed, redelivery pending")
		return
	}
	outcome := "published"
	if delivery == host.Deferred {
		outcome = "deferred"
	}
	observability.CommandDispatchOutcomes.WithLabelValues(outcome).Inc()
	d.ackEntry(ctx, entryID)
	log.WithFields(logrus.Fields{
		"edge_node_id": lc.SparkplugEdgeID,
		"device_id":    lc.MachineCode,
		"delivery":     outcome,
	}).Info("Command dispatched to Sparkplug device")
}
