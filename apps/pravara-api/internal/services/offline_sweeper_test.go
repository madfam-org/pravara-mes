package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

type fakeLivenessStore struct {
	mu        sync.Mutex
	tenants   []uuid.UUID
	stale     map[uuid.UUID][]types.Machine
	refreshed map[uuid.UUID]bool // heartbeat arrived between list and mark
	failFor   uuid.UUID
	marked    map[uuid.UUID]repositories.OutboxRecord
	cutoffs   []time.Time
}

func (f *fakeLivenessStore) ListTenantIDs(context.Context) ([]uuid.UUID, error) {
	return f.tenants, nil
}

func (f *fakeLivenessStore) GetOfflineMachines(_ context.Context, tenantID uuid.UUID, _ time.Duration) ([]types.Machine, error) {
	if tenantID == f.failFor {
		return nil, errors.New("tenant unavailable")
	}
	return f.stale[tenantID], nil
}

func (f *fakeLivenessStore) MarkOfflineIfStale(_ context.Context, tenantID, machineID uuid.UUID, cutoff time.Time, event repositories.OutboxRecord) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cutoffs = append(f.cutoffs, cutoff)
	if f.refreshed[machineID] {
		return false, nil
	}
	if _, done := f.marked[machineID]; done {
		return false, nil
	}
	f.marked[machineID] = event
	return true, nil
}

type recordingNotifier struct {
	events []*pubsub.Event
}

func (n *recordingNotifier) NotifyRealtime(_ context.Context, _ pubsub.ChannelNamespace, _ uuid.UUID, e *pubsub.Event) error {
	n.events = append(n.events, e)
	return nil
}

func TestOfflineSweeper_MarksStaleMachinesPerTenantWithOutboxEvent(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	t1, t2, t3 := uuid.New(), uuid.New(), uuid.New()
	staleA := types.Machine{ID: uuid.New(), TenantID: t1, Name: "Printer A", Status: types.MachineStatusOnline}
	staleB := types.Machine{ID: uuid.New(), TenantID: t3, Name: "Printer B", Status: types.MachineStatusOnline}
	raced := types.Machine{ID: uuid.New(), TenantID: t3, Name: "Printer C", Status: types.MachineStatusOnline}

	store := &fakeLivenessStore{
		tenants:   []uuid.UUID{t1, t2, t3},
		failFor:   t2,
		stale:     map[uuid.UUID][]types.Machine{t1: {staleA}, t3: {staleB, raced}},
		refreshed: map[uuid.UUID]bool{raced.ID: true},
		marked:    map[uuid.UUID]repositories.OutboxRecord{},
	}
	notifier := &recordingNotifier{}
	s := NewOfflineSweeper(store, notifier, 5*time.Minute, time.Hour, log)
	fixed := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return fixed }

	require.Equal(t, 2, s.SweepOnce(context.Background()), "tenant errors must not stop other tenants")
	require.Len(t, store.marked, 2)
	require.Equal(t, fixed.Add(-5*time.Minute), store.cutoffs[0])

	rec := store.marked[staleA.ID]
	require.Equal(t, "machine.status_changed", rec.EventType)
	require.Equal(t, "machines", rec.Namespace)
	var env struct {
		Type     string                   `json:"type"`
		TenantID uuid.UUID                `json:"tenant_id"`
		Data     pubsub.MachineStatusData `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Payload, &env))
	require.Equal(t, t1, env.TenantID)
	require.Equal(t, "online", env.Data.OldStatus)
	require.Equal(t, "offline", env.Data.NewStatus)
	require.Equal(t, staleA.ID, env.Data.MachineID)

	require.Len(t, notifier.events, 2, "only real transitions are pushed")
	require.Equal(t, 0, s.SweepOnce(context.Background()), "a second pass changes nothing")
}

func TestOfflineSweeper_WorksWithoutNotifier(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	tenant := uuid.New()
	m := types.Machine{ID: uuid.New(), TenantID: tenant}
	store := &fakeLivenessStore{
		tenants: []uuid.UUID{tenant},
		stale:   map[uuid.UUID][]types.Machine{tenant: {m}},
		marked:  map[uuid.UUID]repositories.OutboxRecord{},
	}
	s := NewOfflineSweeper(store, nil, 0, 0, log)
	require.Equal(t, 5*time.Minute, s.timeout)
	require.Equal(t, 1, s.SweepOnce(context.Background()))
}
