package command

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// JobCompletion is a committed start_job completion: the command is
// completed, its task moved to quality_check, and the task.job_completed
// event (plus any order roll-up event) is in the outbox.
type JobCompletion struct {
	TenantID    uuid.UUID
	CommandID   uuid.UUID
	MachineID   uuid.UUID
	MachineName string
	TaskID      uuid.UUID
	OrderID     *uuid.UUID
	CompletedAt time.Time
	// OrderRolledUp is true when the completion advanced the order's status.
	OrderRolledUp bool
}

// JobCompletionHook receives every committed job completion, after the
// ledger transaction commits. It is the extension point for production
// genealogy and product-passport recording. A hook error is logged and
// counted; it never undoes the completion.
type JobCompletionHook interface {
	OnJobCompleted(ctx context.Context, c JobCompletion) error
}

// JobCompletionHookFunc adapts a function to JobCompletionHook.
type JobCompletionHookFunc func(ctx context.Context, c JobCompletion) error

// OnJobCompleted calls f.
func (f JobCompletionHookFunc) OnJobCompleted(ctx context.Context, c JobCompletion) error {
	return f(ctx, c)
}
