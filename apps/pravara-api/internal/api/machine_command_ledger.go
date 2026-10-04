package api

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
)

// CommandLedger records machine commands in task_commands so their acks,
// failures and timeouts can be correlated by command_id. Both writes commit
// in their own tenant transaction, independently of the request transaction:
// the row must exist before the command is enqueued, and a failure write-back
// must survive the request's rollback.
// *repositories.TaskCommandRepository satisfies it.
type CommandLedger interface {
	CreateDurable(ctx context.Context, cmd *repositories.TaskCommand) error
	UpdateStatusDurable(ctx context.Context, tenantID, commandID uuid.UUID, status, errorMsg string) error
}

// SetCommandLedger sets the ledger used for direct machine commands.
func (h *MachineHandler) SetCommandLedger(l CommandLedger) {
	h.commandLedger = l
}

// recordDirectCommand writes the ledger row for a command issued directly
// against a machine. It has no task: the request's task/order ids travel in
// the command payload only, so a direct command never moves a task.
func recordDirectCommand(ctx context.Context, l CommandLedger, tenantID uuid.UUID, data pubsub.MachineCommandData) error {
	issuedBy := data.IssuedBy
	params := data.Parameters
	if params == nil {
		params = map[string]interface{}{}
	}
	return l.CreateDurable(ctx, &repositories.TaskCommand{
		TenantID:    tenantID,
		MachineID:   data.MachineID,
		CommandID:   data.CommandID,
		CommandType: string(data.Command),
		Status:      "pending",
		Parameters:  params,
		IssuedBy:    nonNilUUID(issuedBy),
		IssuedAt:    data.IssuedAt,
	})
}

func nonNilUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// directCommandIssuedAt is the ledger timestamp for direct commands.
func directCommandIssuedAt() time.Time { return time.Now().UTC() }
