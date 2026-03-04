// Package services provides business logic services for PravaraMES.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
)

// OEEService manages OEE computation and analytics.
type OEEService struct {
	oeeRepo   *repositories.OEERepository
	publisher *pubsub.Publisher
	log       *logrus.Logger
}

// NewOEEService creates a new OEE service.
func NewOEEService(
	oeeRepo *repositories.OEERepository,
	publisher *pubsub.Publisher,
	log *logrus.Logger,
) *OEEService {
	return &OEEService{
		oeeRepo:   oeeRepo,
		publisher: publisher,
		log:       log,
	}
}

// ComputeDaily computes the OEE for a specific machine on a given date.
// It delegates to the repository's ComputeForMachine and publishes an analytics.oee_updated event.
func (s *OEEService) ComputeDaily(ctx context.Context, tenantID, machineID uuid.UUID, date time.Time) (*repositories.OEESnapshot, error) {
	snapshot, err := s.oeeRepo.ComputeForMachine(ctx, tenantID, machineID, date)
	if err != nil {
		s.log.WithError(err).WithFields(logrus.Fields{
			"tenant_id":  tenantID,
			"machine_id": machineID,
			"date":       date.Format("2006-01-02"),
		}).Error("Failed to compute daily OEE")
		return nil, fmt.Errorf("failed to compute daily OEE: %w", err)
	}

	// Publish analytics event
	if s.publisher != nil {
		event := pubsub.NewEvent("analytics.oee_updated", tenantID, map[string]interface{}{
			"machine_id":   machineID,
			"snapshot_date": snapshot.SnapshotDate,
			"availability": snapshot.Availability,
			"performance":  snapshot.Performance,
			"quality":      snapshot.Quality,
			"oee":          snapshot.OEE,
		})
		if err := s.publisher.Publish(ctx, pubsub.NamespaceMachines, tenantID, event); err != nil {
			s.log.WithError(err).Warn("Failed to publish OEE updated event")
			// Non-blocking: do not fail the computation if publishing fails
		}
	}

	s.log.WithFields(logrus.Fields{
		"machine_id": machineID,
		"date":       date.Format("2006-01-02"),
		"oee":        snapshot.OEE,
	}).Info("Daily OEE computed")

	return snapshot, nil
}

// ComputeAllMachines computes OEE for all machines belonging to a tenant on a given date.
// It queries the machines table for the tenant, then computes OEE for each machine.
func (s *OEEService) ComputeAllMachines(ctx context.Context, tenantID uuid.UUID, date time.Time) ([]repositories.OEESnapshot, error) {
	// Query machine IDs for this tenant
	rows, err := s.oeeRepo.DB().QueryContext(ctx, `SELECT id FROM machines WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list machines for tenant: %w", err)
	}
	defer rows.Close()

	var machineIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan machine id: %w", err)
		}
		machineIDs = append(machineIDs, id)
	}

	var snapshots []repositories.OEESnapshot
	for _, machineID := range machineIDs {
		snapshot, err := s.ComputeDaily(ctx, tenantID, machineID, date)
		if err != nil {
			s.log.WithError(err).WithField("machine_id", machineID).Warn("Failed to compute OEE for machine, skipping")
			continue
		}
		snapshots = append(snapshots, *snapshot)
	}

	s.log.WithFields(logrus.Fields{
		"tenant_id":      tenantID,
		"date":           date.Format("2006-01-02"),
		"machines_count": len(snapshots),
	}).Info("OEE computed for all machines")

	return snapshots, nil
}
