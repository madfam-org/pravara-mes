package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/observability"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// MachineLivenessStore is the tenant-scoped persistence the offline sweeper
// needs. Every per-tenant call takes the tenant id explicitly, so the way
// tenant context is established stays behind this interface.
// *repositories.MachineRepository satisfies it.
type MachineLivenessStore interface {
	ListTenantIDs(ctx context.Context) ([]uuid.UUID, error)
	GetOfflineMachines(ctx context.Context, tenantID uuid.UUID, threshold time.Duration) ([]types.Machine, error)
	MarkOfflineIfStale(ctx context.Context, tenantID, machineID uuid.UUID, cutoff time.Time, event repositories.OutboxRecord) (bool, error)
}

// RealtimeNotifier pushes an already-persisted event to live subscribers.
// *pubsub.Publisher satisfies it.
type RealtimeNotifier interface {
	NotifyRealtime(ctx context.Context, namespace pubsub.ChannelNamespace, tenantID uuid.UUID, event *pubsub.Event) error
}

// OfflineSweeper marks machines offline when their heartbeat goes stale.
// For each tenant it lists online machines whose last heartbeat is older
// than the heartbeat timeout (GetOfflineMachines) and moves each one to
// offline with a guarded update that writes a machine.status_changed event
// to the outbox in the same transaction.
type OfflineSweeper struct {
	store     MachineLivenessStore
	notifier  RealtimeNotifier
	timeout   time.Duration
	interval  time.Duration
	log       *logrus.Logger
	now       func() time.Time
	startOnce sync.Once
}

// NewOfflineSweeper creates a sweeper. notifier may be nil.
func NewOfflineSweeper(store MachineLivenessStore, notifier RealtimeNotifier, heartbeatTimeout, interval time.Duration, log *logrus.Logger) *OfflineSweeper {
	if heartbeatTimeout <= 0 {
		heartbeatTimeout = 5 * time.Minute
	}
	if interval <= 0 {
		interval = time.Minute
	}
	return &OfflineSweeper{
		store:    store,
		notifier: notifier,
		timeout:  heartbeatTimeout,
		interval: interval,
		log:      log,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// Start runs the sweep loop until ctx is cancelled.
func (s *OfflineSweeper) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(s.interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.SweepOnce(ctx)
				}
			}
		}()
	})
}

// SweepOnce runs one pass over every tenant and returns how many machines
// were marked offline.
func (s *OfflineSweeper) SweepOnce(ctx context.Context) int {
	tenants, err := s.store.ListTenantIDs(ctx)
	if err != nil {
		observability.LivenessSweepErrors.Inc()
		s.log.WithError(err).Warn("Liveness sweep: failed to list tenants")
		return 0
	}

	marked := 0
	for _, tenantID := range tenants {
		n, err := s.sweepTenant(ctx, tenantID)
		if err != nil {
			observability.LivenessSweepErrors.Inc()
			s.log.WithError(err).WithField("tenant_id", tenantID).Warn("Liveness sweep failed for tenant")
		}
		marked += n
	}
	return marked
}

func (s *OfflineSweeper) sweepTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	stale, err := s.store.GetOfflineMachines(ctx, tenantID, s.timeout)
	if err != nil {
		return 0, err
	}

	marked := 0
	for _, m := range stale {
		now := s.now()
		event := pubsub.NewEvent(pubsub.EventMachineStatusChanged, tenantID, pubsub.MachineStatusData{
			MachineID:   m.ID,
			MachineName: m.Name,
			OldStatus:   string(types.MachineStatusOnline),
			NewStatus:   string(types.MachineStatusOffline),
			UpdatedAt:   now,
		})
		payload, err := json.Marshal(event)
		if err != nil {
			return marked, fmt.Errorf("marshal status event: %w", err)
		}

		changed, err := s.store.MarkOfflineIfStale(ctx, tenantID, m.ID, now.Add(-s.timeout), repositories.OutboxRecord{
			EventType: string(pubsub.EventMachineStatusChanged),
			Namespace: string(pubsub.NamespaceMachines),
			Payload:   payload,
		})
		if err != nil {
			return marked, err
		}
		if !changed {
			continue // heartbeat arrived meanwhile, or another replica won
		}

		marked++
		observability.MachinesMarkedOffline.Inc()
		s.log.WithFields(logrus.Fields{
			"tenant_id":      tenantID,
			"machine_id":     m.ID,
			"last_heartbeat": m.LastHeartbeat,
		}).Warn("Machine marked offline: heartbeat timeout")

		if s.notifier != nil {
			if err := s.notifier.NotifyRealtime(ctx, pubsub.NamespaceMachines, tenantID, event); err != nil {
				s.log.WithError(err).WithField("machine_id", m.ID).Debug("Failed to push offline status in real time")
			}
		}
	}
	return marked, nil
}
