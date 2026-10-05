package command

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Command ledger statuses (task_commands.status).
const (
	StatusPending      = "pending"
	StatusSent         = "sent"
	StatusAcknowledged = "acknowledged"
	StatusCompleted    = "completed"
	StatusFailed       = "failed"
	StatusTimeout      = "timeout"
)

// IsTerminalStatus reports whether a ledger status can no longer change.
func IsTerminalStatus(status string) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusTimeout:
		return true
	}
	return false
}

// LedgerCommand is the ledger view of a command needed to dispatch it.
type LedgerCommand struct {
	TenantID    uuid.UUID
	CommandID   uuid.UUID
	MachineID   uuid.UUID
	TaskID      *uuid.UUID
	CommandType string
	Status      string
	Attempts    int
	// MachineTopic is the machine's current base MQTT topic (machines.mqtt_topic).
	MachineTopic string
	// SparkplugEdgeID is set for a machine registered as a Sparkplug device
	// (machines.sparkplug_edge_id); such commands go out as DCMD to
	// spBv1.0/{TenantSlug}/DCMD/{SparkplugEdgeID}/{MachineCode}.
	SparkplugEdgeID string
	MachineCode     string
	TenantSlug      string
}

// DispatchFailure describes a failed MQTT publish attempt.
type DispatchFailure struct {
	TenantID    uuid.UUID
	CommandID   uuid.UUID
	Error       string
	MaxAttempts int
	// Permanent fails the command immediately, regardless of attempts.
	Permanent bool
}

// DispatchLedger is the persistence the stream dispatcher needs. Every
// method runs inside the given tenant's context.
type DispatchLedger interface {
	// LoadForDispatch returns the command if it exists in the tenant, or nil.
	LoadForDispatch(ctx context.Context, tenantID, commandID uuid.UUID) (*LedgerCommand, error)
	// MarkSent records a successful publish and arms the ack deadline. It
	// only moves a pending command to sent; a command that was already
	// acknowledged keeps its status.
	MarkSent(ctx context.Context, tenantID, commandID uuid.UUID, sentAt, deadline time.Time) error
	// RecordDispatchFailure counts the attempt and stores the error. When
	// the attempt budget is spent (or the failure is permanent) the command
	// becomes failed and failure events are written to the outbox in the
	// same transaction. It reports whether the command is now failed.
	RecordDispatchFailure(ctx context.Context, f DispatchFailure) (bool, error)
}

// AckMachine is the machine an ack topic resolves to.
type AckMachine struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Code     string
	Name     string
}

// AckApplication is an ack bound to the machine its topic resolved to.
type AckApplication struct {
	CommandID    uuid.UUID
	Machine      AckMachine
	Success      bool
	JobCompleted bool
	Message      string
	AckedAt      time.Time
}

// AckDisposition is what the ledger did with an ack.
type AckDisposition string

const (
	// AckApplied means the ack changed the command's state.
	AckApplied AckDisposition = "applied"
	// AckUnknownCommand means no command with that id exists in the
	// machine's tenant.
	AckUnknownCommand AckDisposition = "unknown_command"
	// AckMachineMismatch means the command was issued to a different machine
	// than the one the ack topic belongs to.
	AckMachineMismatch AckDisposition = "machine_mismatch"
	// AckAlreadyFinal means the command was already completed, failed or
	// timed out; the ack is ignored.
	AckAlreadyFinal AckDisposition = "already_final"
)

// AckOutcome is the result of applying an ack.
type AckOutcome struct {
	Disposition AckDisposition
	Status      string // new ledger status when applied
	// Completion is set when the ack completed a task-linked start_job.
	Completion *JobCompletion
}

// AckLedger is the persistence the ack handler needs.
type AckLedger interface {
	// ResolveAckMachine finds the machine an ack topic belongs to, inside
	// the tenant named by the topic's first level (tenant UUID or slug): the
	// machine whose mqtt_topic equals topicBase, falling back to the machine
	// code segment. It returns nil for an unknown tenant or when no machine
	// matches unambiguously.
	ResolveAckMachine(ctx context.Context, topicBase, code string) (*AckMachine, error)
	// ApplyAck applies the ack inside the machine's tenant. It never changes
	// a command that belongs to another machine.
	ApplyAck(ctx context.Context, a AckApplication) (*AckOutcome, error)
}

// ExpiredCommand is a command the deadline sweep moved to timeout.
type ExpiredCommand struct {
	TenantID       uuid.UUID
	CommandID      uuid.UUID
	MachineID      uuid.UUID
	TaskID         *uuid.UUID
	CommandType    string
	PreviousStatus string
}

// DeadlineLedger is the persistence the deadline sweeper needs.
type DeadlineLedger interface {
	// ListTenantIDs returns every tenant to sweep.
	ListTenantIDs(ctx context.Context) ([]uuid.UUID, error)
	// ExpireOverdue moves the tenant's overdue commands to timeout (sent
	// commands past deadline_at, pending commands issued before
	// pendingCutoff) and writes failure events to the outbox in the same
	// transaction.
	ExpireOverdue(ctx context.Context, tenantID uuid.UUID, now, pendingCutoff time.Time, limit int) ([]ExpiredCommand, error)
}
