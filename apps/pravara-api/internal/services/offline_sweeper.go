package services

import (
	"context"
	"database/sql"
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

// MachineLivenessStore is the persistence the offline sweeper needs. Every
// per-tenant call takes the tenant id explicitly and runs in the tenant scope
// the sweeper opens for it (UseTenantScopes); ListTenantIDs runs in the
// read-only system scope. *repositories.MachineRepository on a db.TenantDB
// satisfies it.
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
	scopes    *dbScopes
}

// UseTenantScopes makes the sweeper list tenants in the read-only system
// scope, read each tenant's stale machines in a tenant transaction and mark
// each machine offline in its own tenant transaction (update and outbox event
// together). Required when the store is built on a db.TenantDB.
func (s *OfflineSweeper) UseTenantScopes(pool *sql.DB) {
	s.scopes = &dbScopes{pool: pool}
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
	var tenants []uuid.UUID
	err := s.scopes.system(ctx, "liveness.list_tenants", func(ctx context.Context) error {
		var err error
		tenants, err = s.store.ListTenantIDs(ctx)
		return err
	})
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
	var stale []types.Machine
	err := s.scopes.tenant(ctx, tenantID, func(ctx context.Context) error {
		var err error
		stale, err = s.store.GetOfflineMachines(ctx, tenantID, s.timeout)
		return err
	})
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

		record := repositories.OutboxRecord{
			EventType: string(pubsub.EventMachineStatusChanged),
			Namespace: string(pubsub.NamespaceMachines),
			Payload:   payload,
		}
		var changed bool
		err = s.scopes.tenant(ctx, tenantID, func(ctx context.Context) error {
			var err error
			changed, err = s.store.MarkOfflineIfStale(ctx, tenantID, m.ID, now.Add(-s.timeout), record)
			return err
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
