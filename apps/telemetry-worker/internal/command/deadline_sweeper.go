package command

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/observability"
)

// DeadlineSweeper periodically expires commands that were never sent
// (pending past the dispatch timeout) or never acknowledged (sent past
// deadline_at). Each expiry moves the command to timeout and writes failure
// events to the outbox in the same transaction. It runs per tenant.
type DeadlineSweeper struct {
	ledger          DeadlineLedger
	interval        time.Duration
	dispatchTimeout time.Duration
	batchLimit      int
	log             *logrus.Logger
	now             func() time.Time

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewDeadlineSweeper creates a sweeper. interval and dispatchTimeout fall
// back to 15s and 10m when not positive.
func NewDeadlineSweeper(ledger DeadlineLedger, interval, dispatchTimeout time.Duration, log *logrus.Logger) *DeadlineSweeper {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if dispatchTimeout <= 0 {
		dispatchTimeout = 10 * time.Minute
	}
	return &DeadlineSweeper{
		ledger:          ledger,
		interval:        interval,
		dispatchTimeout: dispatchTimeout,
		batchLimit:      200,
		log:             log,
		now:             func() time.Time { return time.Now().UTC() },
	}
}

// Start runs the sweep loop until Stop or ctx cancellation.
func (s *DeadlineSweeper) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				s.SweepOnce(runCtx)
			}
		}
	}()
}

// SweepOnce expires overdue commands across all tenants and returns how
// many were expired.
func (s *DeadlineSweeper) SweepOnce(ctx context.Context) int {
	tenants, err := s.ledger.ListTenantIDs(ctx)
	if err != nil {
		s.log.WithError(err).Warn("Command deadline sweep: failed to list tenants")
		return 0
	}

	now := s.now()
	total := 0
	for _, tenantID := range tenants {
		expired, err := s.ledger.ExpireOverdue(ctx, tenantID, now, now.Add(-s.dispatchTimeout), s.batchLimit)
		if err != nil {
			s.log.WithError(err).WithField("tenant_id", tenantID).Warn("Command deadline sweep failed for tenant")
			continue
		}
		for _, e := range expired {
			observability.CommandTimeouts.WithLabelValues(e.PreviousStatus).Inc()
			s.log.WithFields(logrus.Fields{
				"tenant_id":       e.TenantID,
				"command_id":      e.CommandID,
				"machine_id":      e.MachineID,
				"command":         e.CommandType,
				"previous_status": e.PreviousStatus,
			}).Warn("Command timed out")
		}
		total += len(expired)
	}
	return total
}

// Stop stops the sweep loop.
func (s *DeadlineSweeper) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}
