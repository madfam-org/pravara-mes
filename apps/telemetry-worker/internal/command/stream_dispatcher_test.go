package command

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

const testStream = "test:commands"

type streamRig struct {
	mr     *miniredis.Miniredis
	client *redis.Client
	ledger *fakeDispatchLedger
	pub    *fakePublisher
	log    *logrus.Logger
}

func newStreamRig(t *testing.T) *streamRig {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	t.Cleanup(func() {
		client.Close()
		mr.Close()
	})
	return &streamRig{mr: mr, client: client, ledger: newFakeDispatchLedger(), pub: &fakePublisher{}, log: log}
}

func (r *streamRig) dispatcher(consumer string, maxAttempts int) *Dispatcher {
	return NewDispatcher(r.client, r.pub, r.ledger, DispatcherConfig{
		StreamKey:   testStream,
		Group:       "workers",
		Consumer:    consumer,
		MaxAttempts: maxAttempts,
		RetryIdle:   50 * time.Millisecond,
		AckTimeout:  time.Minute,
		ReadBlock:   20 * time.Millisecond,
	}, r.log)
}

// enqueue appends a command exactly as pravara-api does and registers it as
// pending in the ledger.
func (r *streamRig) enqueue(t *testing.T, register bool) (tenantID, commandID uuid.UUID) {
	t.Helper()
	tenantID, commandID, machineID := uuid.New(), uuid.New(), uuid.New()
	cmd := MachineCommand{
		CommandID: commandID, MachineID: machineID, MQTTTopic: "t/s/a/l/m1",
		Command: string(CommandStartJob), IssuedBy: uuid.New(), IssuedAt: time.Now().UTC(),
	}
	payload, err := json.Marshal(cmd)
	require.NoError(t, err)
	require.NoError(t, r.client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: testStream,
		Values: map[string]interface{}{
			StreamFieldTenantID:  tenantID.String(),
			StreamFieldCommandID: commandID.String(),
			StreamFieldPayload:   string(payload),
		},
	}).Err())
	if register {
		r.ledger.add(LedgerCommand{
			TenantID: tenantID, CommandID: commandID, MachineID: machineID,
			CommandType: string(CommandStartJob), Status: StatusPending, MachineTopic: "t/s/a/l/m1",
		})
	}
	return tenantID, commandID
}

// allDeliveredAndAcked reports whether the group has delivered every entry
// in the stream and none is left pending.
func (r *streamRig) allDeliveredAndAcked(t *testing.T) bool {
	t.Helper()
	ctx := context.Background()
	groups, err := r.client.XInfoGroups(ctx, testStream).Result()
	if err != nil || len(groups) == 0 {
		return false
	}
	last, err := r.client.XRevRangeN(ctx, testStream, "+", "-", 1).Result()
	if err != nil || len(last) == 0 {
		return false
	}
	return groups[0].LastDeliveredID == last[0].ID && groups[0].Pending == 0
}

func (r *streamRig) pendingCount(t *testing.T) int64 {
	t.Helper()
	p, err := r.client.XPending(context.Background(), testStream, "workers").Result()
	require.NoError(t, err)
	return p.Count
}

func TestDispatcher_PublishesAndAcknowledgesEntry(t *testing.T) {
	r := newStreamRig(t)
	_, commandID := r.enqueue(t, true)

	d := r.dispatcher("w1", 3)
	require.NoError(t, d.Start(context.Background()))
	defer d.Stop()

	require.Eventually(t, func() bool { return r.ledger.get(commandID).Status == StatusSent }, 2*time.Second, 10*time.Millisecond)
	published, _ := r.pub.snapshot()
	require.Equal(t, []string{"t/s/a/l/m1/cmd"}, published)
	require.Eventually(t, func() bool { return r.pendingCount(t) == 0 }, time.Second, 10*time.Millisecond)
}

func TestDispatcher_EntryAppendedBeforeGroupExistsIsDelivered(t *testing.T) {
	r := newStreamRig(t)
	_, commandID := r.enqueue(t, true) // stream exists, group does not

	d := r.dispatcher("w1", 3)
	require.NoError(t, d.Start(context.Background()))
	defer d.Stop()

	require.Eventually(t, func() bool { return r.ledger.get(commandID).Status == StatusSent }, 2*time.Second, 10*time.Millisecond)
}

func TestDispatcher_RedeliversEntryOfCrashedConsumer(t *testing.T) {
	r := newStreamRig(t)
	ctx := context.Background()
	require.NoError(t, r.client.XGroupCreateMkStream(ctx, testStream, "workers", "0").Err())
	_, commandID := r.enqueue(t, true)

	// A consumer reads the entry and dies before acknowledging it.
	got, err := r.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: "workers", Consumer: "crashed", Streams: []string{testStream, ">"}, Count: 1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, got[0].Messages, 1)
	require.EqualValues(t, 1, r.pendingCount(t))

	// The restarted worker (new consumer name) reclaims and dispatches it.
	d := r.dispatcher("restarted", 3)
	require.NoError(t, d.Start(ctx))
	defer d.Stop()

	require.Eventually(t, func() bool { return r.ledger.get(commandID).Status == StatusSent }, 2*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return r.pendingCount(t) == 0 }, time.Second, 10*time.Millisecond)
}

func TestDispatcher_BoundedRetriesThenFailed(t *testing.T) {
	r := newStreamRig(t)
	r.pub.setFail(true)
	_, commandID := r.enqueue(t, true)

	d := r.dispatcher("w1", 3)
	require.NoError(t, d.Start(context.Background()))
	defer d.Stop()

	require.Eventually(t, func() bool { return r.ledger.get(commandID).Status == StatusFailed }, 3*time.Second, 10*time.Millisecond)
	_, attempts := r.pub.snapshot()
	require.Equal(t, 3, attempts, "publish attempts must stop at MaxAttempts")
	require.Equal(t, 3, r.ledger.get(commandID).Attempts)
	r.ledger.mu.Lock()
	lastErr := r.ledger.errors[commandID]
	r.ledger.mu.Unlock()
	require.Contains(t, lastErr, "broker unavailable")
	require.Eventually(t, func() bool { return r.pendingCount(t) == 0 }, time.Second, 10*time.Millisecond)
}

func TestDispatcher_RetrySucceedsAfterTransientPublishFailure(t *testing.T) {
	r := newStreamRig(t)
	r.pub.setFail(true)
	_, commandID := r.enqueue(t, true)

	d := r.dispatcher("w1", 5)
	require.NoError(t, d.Start(context.Background()))
	defer d.Stop()

	require.Eventually(t, func() bool { return r.ledger.get(commandID).Attempts >= 1 }, 2*time.Second, 10*time.Millisecond)
	r.pub.setFail(false)
	require.Eventually(t, func() bool { return r.ledger.get(commandID).Status == StatusSent }, 3*time.Second, 10*time.Millisecond)
}

func TestDispatcher_UnknownCommandIsDiscardedWithoutPublish(t *testing.T) {
	r := newStreamRig(t)
	r.enqueue(t, false)

	d := r.dispatcher("w1", 3)
	require.NoError(t, d.Start(context.Background()))
	defer d.Stop()

	require.Eventually(t, func() bool { return r.allDeliveredAndAcked(t) }, 2*time.Second, 10*time.Millisecond)
	_, attempts := r.pub.snapshot()
	require.Zero(t, attempts)
}

func TestDispatcher_CommandAlreadySentIsNotPublishedAgain(t *testing.T) {
	r := newStreamRig(t)
	_, commandID := r.enqueue(t, true)
	r.ledger.mu.Lock()
	r.ledger.commands[commandID].Status = StatusSent
	r.ledger.mu.Unlock()

	d := r.dispatcher("w1", 3)
	require.NoError(t, d.Start(context.Background()))
	defer d.Stop()

	require.Eventually(t, func() bool { return r.allDeliveredAndAcked(t) }, 2*time.Second, 10*time.Millisecond)
	_, attempts := r.pub.snapshot()
	require.Zero(t, attempts)
}

func TestDispatcher_MachineMismatchFailsPermanently(t *testing.T) {
	r := newStreamRig(t)
	_, commandID := r.enqueue(t, true)
	r.ledger.mu.Lock()
	r.ledger.commands[commandID].MachineID = uuid.New()
	r.ledger.mu.Unlock()

	d := r.dispatcher("w1", 3)
	require.NoError(t, d.Start(context.Background()))
	defer d.Stop()

	require.Eventually(t, func() bool { return r.ledger.get(commandID).Status == StatusFailed }, 2*time.Second, 10*time.Millisecond)
	_, attempts := r.pub.snapshot()
	require.Zero(t, attempts)
}

func TestDispatcher_LedgerUnavailableLeavesEntryPending(t *testing.T) {
	r := newStreamRig(t)
	r.ledger.loadErr = errors.New("database unavailable")
	r.enqueue(t, true)

	d := r.dispatcher("w1", 3)
	require.NoError(t, d.Start(context.Background()))

	require.Eventually(t, func() bool { return r.pendingCount(t) == 1 }, 2*time.Second, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	d.Stop()
	require.EqualValues(t, 1, r.pendingCount(t))
	_, attempts := r.pub.snapshot()
	require.Zero(t, attempts)
}

func TestDispatcher_MalformedEntryIsDiscarded(t *testing.T) {
	r := newStreamRig(t)
	require.NoError(t, r.client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: testStream,
		Values: map[string]interface{}{StreamFieldTenantID: "not-a-uuid", StreamFieldPayload: "{"},
	}).Err())

	d := r.dispatcher("w1", 3)
	require.NoError(t, d.Start(context.Background()))
	defer d.Stop()

	require.Eventually(t, func() bool { return r.allDeliveredAndAcked(t) }, 2*time.Second, 10*time.Millisecond)
}
