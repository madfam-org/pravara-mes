package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/command"
)

func TestLedgerPG_AckFromAnotherMachineIsRejected(t *testing.T) {
	db := openTestDB(t)
	f := newFixture(t, db)
	l := NewCommandLedgerWithScope(db, TxTenantScope{DB: db})
	ctx := context.Background()

	topicA := f.topic("s/a/l/printer-a")
	topicB := f.topic("s/a/l/printer-b")
	machineA := f.machine(t, "printer-a-"+f.tenantID.String()[:6], topicA, "online")
	f.machine(t, "printer-b-"+f.tenantID.String()[:6], topicB, "online")
	taskID := f.task(t, nil, machineA)
	cmdID := f.command(t, machineA, &taskID, "start_job", "sent")

	spoofer, err := l.ResolveAckMachine(ctx, topicB, "")
	require.NoError(t, err)
	require.NotNil(t, spoofer)

	out, err := l.ApplyAck(ctx, command.AckApplication{
		CommandID: cmdID, Machine: *spoofer, Success: true, JobCompleted: true, AckedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	require.Equal(t, command.AckMachineMismatch, out.Disposition)

	status, _, _ := f.commandStatus(t, cmdID)
	require.Equal(t, "sent", status)
	require.Equal(t, "in_progress", f.scalar(t, `SELECT status::text FROM tasks WHERE id = $1`, taskID))
	require.Empty(t, f.outboxEvents(t))
}

func TestLedgerPG_AckFromAnotherTenantSeesNoCommand(t *testing.T) {
	db := openTestDB(t)
	f1, f2 := newFixture(t, db), newFixture(t, db)
	l := NewCommandLedgerWithScope(db, TxTenantScope{DB: db})

	m1 := f1.machine(t, "shared-code", "", "online")
	m2 := f2.machine(t, "shared-code", "", "online")
	cmdID := f1.command(t, m1, nil, "home", "sent")

	out, err := l.ApplyAck(context.Background(), command.AckApplication{
		CommandID: cmdID, Machine: command.AckMachine{ID: m2, TenantID: f2.tenantID}, Success: true, AckedAt: time.Now(),
	})
	require.NoError(t, err)
	require.Equal(t, command.AckUnknownCommand, out.Disposition)

	// The code exists in two tenants: resolution happens only inside the
	// tenant named by the topic, never across tenants.
	m, err := l.ResolveAckMachine(context.Background(), f1.topic("x/y/z/shared-code"), "shared-code")
	require.NoError(t, err)
	require.NotNil(t, m)
	require.Equal(t, m1, m.ID)
	m, err = l.ResolveAckMachine(context.Background(), f2.tenantID.String()+"/x/y/z/shared-code", "shared-code")
	require.NoError(t, err)
	require.NotNil(t, m)
	require.Equal(t, m2, m.ID)
	m, err = l.ResolveAckMachine(context.Background(), "no-such-tenant/x/y/z/shared-code", "shared-code")
	require.NoError(t, err)
	require.Nil(t, m)
}

func TestLedgerPG_JobCompletionWritesEventsAndRollsUpOrder(t *testing.T) {
	db := openTestDB(t)
	f := newFixture(t, db)
	l := NewCommandLedgerWithScope(db, TxTenantScope{DB: db})
	ctx := context.Background()

	topic := f.topic("a/l/p1")
	machineID := f.machine(t, "p1-"+f.tenantID.String()[:6], topic, "online")
	orderID := f.order(t, "scheduled")
	taskID := f.task(t, &orderID, machineID)
	cmdID := f.command(t, machineID, &taskID, "start_job", "sent")

	m, err := l.ResolveAckMachine(ctx, topic, "")
	require.NoError(t, err)
	require.Equal(t, machineID, m.ID)

	ack := command.AckApplication{CommandID: cmdID, Machine: *m, Success: true, AckedAt: time.Now().UTC()}
	out, err := l.ApplyAck(ctx, ack)
	require.NoError(t, err)
	require.Equal(t, command.StatusAcknowledged, out.Status)
	require.Nil(t, out.Completion)

	ack.JobCompleted = true
	out, err = l.ApplyAck(ctx, ack)
	require.NoError(t, err)
	require.Equal(t, command.AckApplied, out.Disposition)
	require.Equal(t, command.StatusCompleted, out.Status)
	require.NotNil(t, out.Completion)
	require.Equal(t, taskID, out.Completion.TaskID)
	require.True(t, out.Completion.OrderRolledUp)

	require.Equal(t, "quality_check", f.scalar(t, `SELECT status::text FROM tasks WHERE id = $1`, taskID))
	require.Equal(t, "in_progress", f.scalar(t, `SELECT status::text FROM orders WHERE id = $1`, orderID))

	events := f.outboxEvents(t)
	require.Len(t, events[EventTaskJobCompleted], 1)
	require.Equal(t, taskID.String(), events[EventTaskJobCompleted][0]["task_id"])
	require.Equal(t, cmdID.String(), events[EventTaskJobCompleted][0]["command_id"])
	require.Len(t, events[EventOrderStatusChanged], 1)
	require.Equal(t, "scheduled", events[EventOrderStatusChanged][0]["old_status"])
	require.Equal(t, "in_progress", events[EventOrderStatusChanged][0]["new_status"])

	// A repeated completion is ignored and emits nothing new.
	out, err = l.ApplyAck(ctx, ack)
	require.NoError(t, err)
	require.Equal(t, command.AckAlreadyFinal, out.Disposition)
	require.Len(t, f.outboxEvents(t)[EventTaskJobCompleted], 1)
}

func TestLedgerPG_FailureAckWritesFailureEvents(t *testing.T) {
	db := openTestDB(t)
	f := newFixture(t, db)
	l := NewCommandLedgerWithScope(db, TxTenantScope{DB: db})

	machineID := f.machine(t, "p2", "", "online")
	taskID := f.task(t, nil, machineID)
	cmdID := f.command(t, machineID, &taskID, "start_job", "acknowledged")

	out, err := l.ApplyAck(context.Background(), command.AckApplication{
		CommandID: cmdID, Machine: command.AckMachine{ID: machineID, TenantID: f.tenantID, Name: "Machine p2"},
		Success: false, Message: "nozzle clog", AckedAt: time.Now(),
	})
	require.NoError(t, err)
	require.Equal(t, command.StatusFailed, out.Status)

	status, _, msg := f.commandStatus(t, cmdID)
	require.Equal(t, "failed", status)
	require.Equal(t, "nozzle clog", msg)
	events := f.outboxEvents(t)
	require.Len(t, events[EventMachineCommandFailed], 1)
	require.Len(t, events[EventTaskJobFailed], 1)
	require.Equal(t, "nozzle clog", events[EventTaskJobFailed][0]["error_message"])
}

func TestLedgerPG_DispatchFailureWriteBackIsBounded(t *testing.T) {
	db := openTestDB(t)
	f := newFixture(t, db)
	l := NewCommandLedgerWithScope(db, TxTenantScope{DB: db})
	ctx := context.Background()

	machineID := f.machine(t, "p3", "o/s/a/l/p3", "online")
	cmdID := f.command(t, machineID, nil, "preheat", "pending")

	lc, err := l.LoadForDispatch(ctx, f.tenantID, cmdID)
	require.NoError(t, err)
	require.Equal(t, "o/s/a/l/p3", lc.MachineTopic)
	require.Nil(t, lc.TaskID)

	// Wrong tenant sees nothing.
	other, err := l.LoadForDispatch(ctx, uuid.New(), cmdID)
	require.NoError(t, err)
	require.Nil(t, other)

	for i := 1; i <= 2; i++ {
		final, err := l.RecordDispatchFailure(ctx, command.DispatchFailure{
			TenantID: f.tenantID, CommandID: cmdID, Error: "mqtt publish failed: timeout", MaxAttempts: 3,
		})
		require.NoError(t, err)
		require.False(t, final)
		status, attempts, msg := f.commandStatus(t, cmdID)
		require.Equal(t, "pending", status)
		require.Equal(t, i, attempts)
		require.Equal(t, "mqtt publish failed: timeout", msg)
	}
	final, err := l.RecordDispatchFailure(ctx, command.DispatchFailure{
		TenantID: f.tenantID, CommandID: cmdID, Error: "mqtt publish failed: timeout", MaxAttempts: 3,
	})
	require.NoError(t, err)
	require.True(t, final)
	status, attempts, _ := f.commandStatus(t, cmdID)
	require.Equal(t, "failed", status)
	require.Equal(t, 3, attempts)
	require.Len(t, f.outboxEvents(t)[EventMachineCommandFailed], 1)
}

func TestLedgerPG_MarkSentDoesNotOverwriteEarlyAck(t *testing.T) {
	db := openTestDB(t)
	f := newFixture(t, db)
	l := NewCommandLedgerWithScope(db, TxTenantScope{DB: db})

	machineID := f.machine(t, "p4", "o/s/a/l/p4", "online")
	cmdID := f.command(t, machineID, nil, "home", "acknowledged")
	now := time.Now().UTC()
	require.NoError(t, l.MarkSent(context.Background(), f.tenantID, cmdID, now, now.Add(time.Minute)))
	status, attempts, _ := f.commandStatus(t, cmdID)
	require.Equal(t, "acknowledged", status)
	require.Equal(t, 1, attempts)
}

func TestLedgerPG_ExpireOverdueTimesOutUnsentAndUnacked(t *testing.T) {
	db := openTestDB(t)
	f := newFixture(t, db)
	l := NewCommandLedgerWithScope(db, TxTenantScope{DB: db})
	ctx := context.Background()

	machineID := f.machine(t, "p5", "o/s/a/l/p5", "online")
	taskID := f.task(t, nil, machineID)
	unacked := f.command(t, machineID, &taskID, "start_job", "pending")
	unsent := f.command(t, machineID, nil, "home", "pending")
	fresh := f.command(t, machineID, nil, "pause", "pending")

	now := time.Now().UTC()
	require.NoError(t, l.MarkSent(ctx, f.tenantID, unacked, now.Add(-2*time.Minute), now.Add(-time.Minute)))
	_, err := db.Exec(`UPDATE task_commands SET issued_at = NOW() - INTERVAL '1 hour' WHERE command_id = $1`, unsent)
	require.NoError(t, err)

	expired, err := l.ExpireOverdue(ctx, f.tenantID, now, now.Add(-10*time.Minute), 100)
	require.NoError(t, err)
	require.Len(t, expired, 2)

	byID := map[uuid.UUID]string{}
	for _, e := range expired {
		byID[e.CommandID] = e.PreviousStatus
	}
	require.Equal(t, "sent", byID[unacked])
	require.Equal(t, "pending", byID[unsent])

	s, _, msg := f.commandStatus(t, unacked)
	require.Equal(t, "timeout", s)
	require.Equal(t, "no acknowledgement before the deadline", msg)
	s, _, msg = f.commandStatus(t, unsent)
	require.Equal(t, "timeout", s)
	require.Equal(t, "not dispatched before the deadline", msg)
	s, _, _ = f.commandStatus(t, fresh)
	require.Equal(t, "pending", s)

	events := f.outboxEvents(t)
	require.Len(t, events[EventMachineCommandFailed], 2)
	require.Len(t, events[EventTaskJobFailed], 1)
	for _, e := range events[EventMachineCommandFailed] {
		require.Equal(t, "timeout", e["status"])
	}

	// Late ack after timeout is ignored.
	out, err := l.ApplyAck(ctx, command.AckApplication{
		CommandID: unacked, Machine: command.AckMachine{ID: machineID, TenantID: f.tenantID}, Success: true, AckedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, command.AckAlreadyFinal, out.Disposition)
}
