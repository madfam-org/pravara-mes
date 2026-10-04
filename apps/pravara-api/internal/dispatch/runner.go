package dispatch

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/remote"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
)

const (
	claimBatch   = 10
	leaseFor     = 5 * time.Minute
	maxBackoff   = 10 * time.Minute
	passportPage = 20
)

// Start runs the dispatch and passport loops until ctx ends. It does nothing
// unless dispatch is enabled.
func (s *Service) Start(ctx context.Context) {
	if !s.cfg.Enabled {
		s.log.Info("Fabrication dispatch disabled; runner not started")
		return
	}
	go func() {
		ticker := time.NewTicker(s.cfg.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.RunOnce(ctx)
			}
		}
	}()
}

// RunOnce advances every due dispatch by one hop and attempts every due
// passport delivery, for every tenant. It returns the number of dispatches
// and deliveries processed.
func (s *Service) RunOnce(ctx context.Context) (dispatches, deliveries int) {
	var tenants []uuid.UUID
	err := db.RunInSystemScope(ctx, s.Pool, "dispatch.list_tenants", func(ctx context.Context) error {
		rows, err := s.Pool.QueryContext(ctx, `SELECT id FROM tenants ORDER BY id`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			tenants = append(tenants, id)
		}
		return rows.Err()
	})
	if err != nil {
		s.log.WithError(err).Warn("dispatch: failed to list tenants")
		return 0, 0
	}
	for _, t := range tenants {
		dispatches += s.runTenant(ctx, t)
		deliveries += s.deliverTenant(ctx, t)
	}
	return dispatches, deliveries
}

func (s *Service) runTenant(ctx context.Context, tenantID uuid.UUID) int {
	var due []repositories.DispatchJob
	err := s.inTenant(ctx, tenantID, func(ctx context.Context) error {
		var err error
		due, err = s.Dispatch.ClaimDue(ctx, tenantID, s.owner, leaseFor, claimBatch)
		return err
	})
	if err != nil {
		s.log.WithError(err).WithField("tenant_id", tenantID).Warn("dispatch: failed to claim work")
		return 0
	}
	for i := range due {
		s.step(ctx, &due[i])
	}
	return len(due)
}

// step runs one hop and saves the outcome (releasing the lease).
func (s *Service) step(ctx context.Context, d *repositories.DispatchJob) {
	before := d.Status
	serr := s.advance(ctx, d)
	now := s.now()
	log := s.log.WithFields(logrus.Fields{"dispatch_id": d.ID, "task_id": d.TaskID, "from": before, "to": d.Status})
	switch {
	case serr == nil:
		d.ErrorCode, d.ErrorMessage, d.ErrorRetryable = "", "", nil
		if d.Status != before {
			d.NextAttemptAt = now
			log.Info("dispatch advanced")
		} else if !d.NextAttemptAt.After(now) {
			d.NextAttemptAt = now.Add(s.cfg.PollInterval)
		}
	case serr.retryable && d.Attempts+1 < d.MaxAttempts:
		d.Attempts++
		d.ErrorCode, d.ErrorMessage, d.ErrorRetryable = serr.code, serr.msg, ptrBool(true)
		d.NextAttemptAt = now.Add(backoff(s.cfg.PollInterval, d.Attempts))
		log.WithFields(logrus.Fields{"code": serr.code, "attempt": d.Attempts}).Warn("dispatch hop failed; will retry: " + serr.msg)
	default:
		d.Attempts++
		d.ErrorCode, d.ErrorMessage, d.ErrorRetryable = serr.code, serr.msg, ptrBool(serr.retryable)
		d.Status, d.CompletedAt = repositories.DispatchFailed, &now
		s.releaseQuietly(ctx, d, "dispatch failed: "+serr.code)
		log.WithField("code", serr.code).Error("dispatch failed: " + serr.msg)
	}
	err := s.inTenant(ctx, d.TenantID, func(ctx context.Context) error { return s.Dispatch.Save(ctx, d, s.owner) })
	if errors.Is(err, repositories.ErrLeaseLost) {
		log.Warn("dispatch lease lost; another runner owns it")
	} else if err != nil {
		log.WithError(err).Error("dispatch: failed to save progress (the lease expires and the hop is retried)")
	}
}

func ptrBool(b bool) *bool { return &b }

func backoff(base time.Duration, attempt int) time.Duration {
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
		if d > maxBackoff {
			return maxBackoff
		}
	}
	return d
}

// deliverTenant attempts due passport deliveries of one tenant.
func (s *Service) deliverTenant(ctx context.Context, tenantID uuid.UUID) int {
	var due []repositories.PassportOutboxRow
	var slug string
	err := s.inTenant(ctx, tenantID, func(ctx context.Context) error {
		var err error
		if due, err = s.Passports.DuePassportDeliveries(ctx, passportPage); err != nil || len(due) == 0 {
			return err
		}
		slug, err = s.Sources.TenantSlug(ctx, tenantID)
		return err
	})
	if err != nil {
		s.log.WithError(err).WithField("tenant_id", tenantID).Warn("passport: failed to read the outbox")
		return 0
	}
	if len(due) == 0 {
		return 0
	}
	var ts *machineclients.TokenSource
	var tsErr error
	if s.Clients == nil {
		tsErr = machineclients.ErrNotConfigured
	} else {
		ts, tsErr = s.Clients.AssetShellsPublisher(slug)
	}
	// The due query returns an event only once every earlier row of its
	// record is delivered, so an instance is always published first.
	for _, row := range due {
		var res *publishOutcome
		switch {
		case tsErr != nil:
			res = &publishOutcome{err: tsErr}
		case s.Shells == nil:
			res = &publishOutcome{err: errors.New("asset-shells client not configured")}
		default:
			res = s.publish(ctx, ts, row)
		}
		s.recordDelivery(ctx, row, res)
	}
	return len(due)
}

type publishOutcome struct {
	status int
	body   []byte
	err    error
}

func (s *Service) publish(ctx context.Context, ts *machineclients.TokenSource, row repositories.PassportOutboxRow) *publishOutcome {
	var res *publishOutcome
	if row.Kind == repositories.PassportKindInstance {
		r, err := s.Shells.PublishInstance(ctx, ts, row.Payload)
		res = &publishOutcome{err: err}
		if r != nil {
			res.status, res.body = r.Status, encode(r.Body)
		}
	} else {
		r, err := s.Shells.AppendPassportEvent(ctx, ts, row.InstanceUUID.String(), row.Payload)
		res = &publishOutcome{err: err}
		if r != nil {
			res.status, res.body = r.Status, encode(r.Body)
		}
	}
	return res
}

func (s *Service) recordDelivery(ctx context.Context, row repositories.PassportOutboxRow, res *publishOutcome) {
	log := s.log.WithFields(logrus.Fields{"passport_row": row.ID, "kind": row.Kind, "record": row.ManufacturingRecordID})
	err := s.inTenant(ctx, row.TenantID, func(ctx context.Context) error {
		if res.err == nil {
			log.Info("passport delivered to asset-shells")
			return s.Passports.MarkDelivered(ctx, row.ID, res.status, res.body)
		}
		status := 0
		var re *remote.Error
		if errors.As(res.err, &re) {
			status = re.Status
		}
		// Content the service rejects will not change on retry; configuration
		// and transport problems will, once fixed.
		rejected := status == http.StatusBadRequest || status == http.StatusConflict ||
			status == http.StatusUnprocessableEntity || status == http.StatusRequestEntityTooLarge
		terminalNow := rejected || row.Attempts+1 >= s.cfg.PassportAttempts
		log.WithError(res.err).WithField("terminal", terminalNow).Warn("passport delivery failed")
		return s.Passports.MarkAttemptFailed(ctx, row.ID, status, res.err.Error(), terminalNow,
			s.now().Add(backoff(s.cfg.PollInterval, row.Attempts+1)))
	})
	if err != nil {
		log.WithError(err).Error("passport: failed to record the delivery attempt")
	}
}
