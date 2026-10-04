package command

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type ackRig struct {
	handler  *AckHandler
	ledger   *fakeAckLedger
	pub      *recordingAckPublisher
	tenant   uuid.UUID
	machineA AckMachine
	machineB AckMachine
	taskID   uuid.UUID
}

func newAckRig(t *testing.T) *ackRig {
	t.Helper()
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	tenant := uuid.New()
	a := AckMachine{ID: uuid.New(), TenantID: tenant, Code: "printer-a", Name: "Printer A"}
	b := AckMachine{ID: uuid.New(), TenantID: tenant, Code: "printer-b", Name: "Printer B"}
	ledger := &fakeAckLedger{
		machines: map[string]AckMachine{"org/site/area/line/printer-a": a, "org/site/area/line/printer-b": b},
		commands: map[uuid.UUID]*fakeAckCommand{},
	}
	pub := &recordingAckPublisher{}
	h := NewAckHandler(&MockMQTTClient{}, pub, log, "+/+/+/+/+/#")
	h.SetLedger(ledger)
	return &ackRig{handler: h, ledger: ledger, pub: pub, tenant: tenant, machineA: a, machineB: b, taskID: uuid.New()}
}

func (r *ackRig) issueStartJob(machine AckMachine) uuid.UUID {
	id := uuid.New()
	task := r.taskID
	r.ledger.commands[id] = &fakeAckCommand{
		tenantID: machine.TenantID, machineID: machine.ID, taskID: &task,
		cmdType: string(CommandStartJob), status: StatusSent,
	}
	return id
}

func ackPayload(t *testing.T, commandID uuid.UUID, success, completed bool) []byte {
	t.Helper()
	b, err := json.Marshal(CommandAck{CommandID: commandID.String(), Success: success, JobCompleted: completed, Timestamp: time.Now().UTC()})
	require.NoError(t, err)
	return b
}

func TestAck_FromIssuingMachineIsApplied(t *testing.T) {
	r := newAckRig(t)
	id := r.issueStartJob(r.machineA)

	got := r.handler.ProcessAck(context.Background(), "org/site/area/line/printer-a/ack", ackPayload(t, id, true, false))
	require.Equal(t, AckApplied, got)
	require.Equal(t, StatusAcknowledged, r.ledger.commands[id].status)
	require.Equal(t, 1, r.pub.count)
}

func TestAck_SpoofedFromAnotherMachineIsRejected(t *testing.T) {
	r := newAckRig(t)
	id := r.issueStartJob(r.machineA)

	// Machine B reports completion of a command issued to machine A.
	got := r.handler.ProcessAck(context.Background(), "org/site/area/line/printer-b/ack", ackPayload(t, id, true, true))
	require.Equal(t, AckMachineMismatch, got)
	require.Equal(t, StatusSent, r.ledger.commands[id].status, "command must be untouched")
	require.Zero(t, r.pub.count)
}

func TestAck_FromUnknownTopicIsDropped(t *testing.T) {
	r := newAckRig(t)
	id := r.issueStartJob(r.machineA)

	got := r.handler.ProcessAck(context.Background(), "org/site/area/line/ghost/ack", ackPayload(t, id, true, true))
	require.Equal(t, AckDisposition("unknown_machine"), got)
	require.Equal(t, StatusSent, r.ledger.commands[id].status)
}

func TestAck_UnknownCommandIsDropped(t *testing.T) {
	r := newAckRig(t)
	got := r.handler.ProcessAck(context.Background(), "org/site/area/line/printer-a/ack", ackPayload(t, uuid.New(), true, false))
	require.Equal(t, AckUnknownCommand, got)
}

func TestAck_InvalidPayloadIsDropped(t *testing.T) {
	r := newAckRig(t)
	require.Equal(t, AckDisposition("invalid"), r.handler.ProcessAck(context.Background(), "org/site/area/line/printer-a/ack", []byte("{")))
	require.Equal(t, AckDisposition("invalid"), r.handler.ProcessAck(context.Background(), "org/site/area/line/printer-a/ack", []byte(`{"command_id":"x"}`)))
}

func TestAck_CompletionCallsHookOnce(t *testing.T) {
	r := newAckRig(t)
	id := r.issueStartJob(r.machineA)

	var calls []JobCompletion
	r.handler.SetCompletionHook(JobCompletionHookFunc(func(_ context.Context, c JobCompletion) error {
		calls = append(calls, c)
		return nil
	}))

	topic := "org/site/area/line/printer-a/ack"
	require.Equal(t, AckApplied, r.handler.ProcessAck(context.Background(), topic, ackPayload(t, id, true, true)))
	require.Equal(t, AckAlreadyFinal, r.handler.ProcessAck(context.Background(), topic, ackPayload(t, id, true, true)))

	require.Len(t, calls, 1)
	require.Equal(t, r.taskID, calls[0].TaskID)
	require.Equal(t, r.machineA.ID, calls[0].MachineID)
}

func TestAck_HookErrorDoesNotUndoCompletion(t *testing.T) {
	r := newAckRig(t)
	id := r.issueStartJob(r.machineA)
	r.handler.SetCompletionHook(JobCompletionHookFunc(func(context.Context, JobCompletion) error {
		return errors.New("recorder down")
	}))

	got := r.handler.ProcessAck(context.Background(), "org/site/area/line/printer-a/ack", ackPayload(t, id, true, true))
	require.Equal(t, AckApplied, got)
	require.Equal(t, StatusCompleted, r.ledger.commands[id].status)
}

func TestAck_FailureMarksCommandFailed(t *testing.T) {
	r := newAckRig(t)
	id := r.issueStartJob(r.machineA)
	got := r.handler.ProcessAck(context.Background(), "org/site/area/line/printer-a/ack", ackPayload(t, id, false, false))
	require.Equal(t, AckApplied, got)
	require.Equal(t, StatusFailed, r.ledger.commands[id].status)
}

// fakeDeadlineLedger expires everything it holds for a tenant.
type fakeDeadlineLedger struct {
	tenants []uuid.UUID
	due     map[uuid.UUID][]ExpiredCommand
	failFor uuid.UUID
	cutoffs []time.Time
}

func (f *fakeDeadlineLedger) ListTenantIDs(context.Context) ([]uuid.UUID, error) {
	return f.tenants, nil
}

func (f *fakeDeadlineLedger) ExpireOverdue(_ context.Context, tenantID uuid.UUID, _, pendingCutoff time.Time, _ int) ([]ExpiredCommand, error) {
	f.cutoffs = append(f.cutoffs, pendingCutoff)
	if tenantID == f.failFor {
		return nil, errors.New("tenant unavailable")
	}
	out := f.due[tenantID]
	delete(f.due, tenantID)
	return out, nil
}

func TestDeadlineSweeper_ExpiresPerTenantAndContinuesPastErrors(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	t1, t2, t3 := uuid.New(), uuid.New(), uuid.New()
	ledger := &fakeDeadlineLedger{
		tenants: []uuid.UUID{t1, t2, t3},
		failFor: t2,
		due: map[uuid.UUID][]ExpiredCommand{
			t1: {{TenantID: t1, CommandID: uuid.New(), PreviousStatus: StatusSent}},
			t3: {{TenantID: t3, CommandID: uuid.New(), PreviousStatus: StatusPending}, {TenantID: t3, CommandID: uuid.New(), PreviousStatus: StatusSent}},
		},
	}
	s := NewDeadlineSweeper(ledger, time.Hour, 10*time.Minute, log)
	fixed := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return fixed }

	require.Equal(t, 3, s.SweepOnce(context.Background()))
	require.Len(t, ledger.cutoffs, 3)
	require.Equal(t, fixed.Add(-10*time.Minute), ledger.cutoffs[0])
	require.Equal(t, 0, s.SweepOnce(context.Background()))
}
