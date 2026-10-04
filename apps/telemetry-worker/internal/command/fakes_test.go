package command

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// fakeDispatchLedger mirrors the SQL semantics of db.CommandLedger in memory.
type fakeDispatchLedger struct {
	mu        sync.Mutex
	commands  map[uuid.UUID]*LedgerCommand
	errors    map[uuid.UUID]string
	loadErr   error
	sentCalls int
}

func newFakeDispatchLedger() *fakeDispatchLedger {
	return &fakeDispatchLedger{
		commands: map[uuid.UUID]*LedgerCommand{},
		errors:   map[uuid.UUID]string{},
	}
}

func (f *fakeDispatchLedger) add(c LedgerCommand) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cc := c
	f.commands[c.CommandID] = &cc
}

func (f *fakeDispatchLedger) get(id uuid.UUID) LedgerCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.commands[id]
}

func (f *fakeDispatchLedger) LoadForDispatch(_ context.Context, tenantID, commandID uuid.UUID) (*LedgerCommand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	c, ok := f.commands[commandID]
	if !ok || c.TenantID != tenantID {
		return nil, nil
	}
	cc := *c
	return &cc, nil
}

func (f *fakeDispatchLedger) MarkSent(_ context.Context, tenantID, commandID uuid.UUID, _, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentCalls++
	c, ok := f.commands[commandID]
	if !ok || c.TenantID != tenantID || IsTerminalStatus(c.Status) {
		return nil
	}
	c.Attempts++
	if c.Status == StatusPending {
		c.Status = StatusSent
	}
	return nil
}

func (f *fakeDispatchLedger) RecordDispatchFailure(_ context.Context, df DispatchFailure) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.commands[df.CommandID]
	if !ok || c.TenantID != df.TenantID || c.Status != StatusPending {
		return true, nil
	}
	c.Attempts++
	f.errors[df.CommandID] = df.Error
	if df.Permanent || c.Attempts >= df.MaxAttempts {
		c.Status = StatusFailed
		return true, nil
	}
	return false, nil
}

// fakePublisher records publishes and can fail on demand.
type fakePublisher struct {
	mu        sync.Mutex
	published []string
	attempts  int
	fail      bool
}

func (p *fakePublisher) Publish(_ context.Context, topic string, _ []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.attempts++
	if p.fail {
		return errors.New("broker unavailable")
	}
	p.published = append(p.published, topic)
	return nil
}

func (p *fakePublisher) snapshot() (published []string, attempts int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.published...), p.attempts
}

func (p *fakePublisher) setFail(v bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail = v
}

// fakeAckLedger applies acks with the binding rules of db.CommandLedger.
type fakeAckLedger struct {
	mu       sync.Mutex
	machines map[string]AckMachine // by topic base
	commands map[uuid.UUID]*fakeAckCommand
}

type fakeAckCommand struct {
	tenantID  uuid.UUID
	machineID uuid.UUID
	taskID    *uuid.UUID
	cmdType   string
	status    string
}

func (f *fakeAckLedger) ResolveAckMachine(_ context.Context, topicBase, _ string) (*AckMachine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.machines[topicBase]
	if !ok {
		return nil, nil
	}
	return &m, nil
}

func (f *fakeAckLedger) ApplyAck(_ context.Context, a AckApplication) (*AckOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.commands[a.CommandID]
	if !ok || c.tenantID != a.Machine.TenantID {
		return &AckOutcome{Disposition: AckUnknownCommand}, nil
	}
	if c.machineID != a.Machine.ID {
		return &AckOutcome{Disposition: AckMachineMismatch}, nil
	}
	if IsTerminalStatus(c.status) {
		return &AckOutcome{Disposition: AckAlreadyFinal}, nil
	}
	switch {
	case !a.Success:
		c.status = StatusFailed
	case a.JobCompleted:
		c.status = StatusCompleted
	default:
		c.status = StatusAcknowledged
	}
	out := &AckOutcome{Disposition: AckApplied, Status: c.status}
	if c.status == StatusCompleted && c.taskID != nil && c.cmdType == string(CommandStartJob) {
		out.Completion = &JobCompletion{
			TenantID: c.tenantID, CommandID: a.CommandID, MachineID: c.machineID,
			TaskID: *c.taskID, CompletedAt: a.AckedAt,
		}
	}
	return out, nil
}

// recordingAckPublisher counts real-time ack notifications.
type recordingAckPublisher struct {
	mu    sync.Mutex
	count int
}

func (p *recordingAckPublisher) PublishCommandAck(context.Context, uuid.UUID, uuid.UUID, CommandAckData) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.count++
	return nil
}
