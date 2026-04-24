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

// SPCService manages Statistical Process Control business logic.
type SPCService struct {
	spcRepo   *repositories.SPCRepository
	publisher *pubsub.Publisher
	log       *logrus.Logger
}

// NewSPCService creates a new SPC service.
func NewSPCService(
	spcRepo *repositories.SPCRepository,
	publisher *pubsub.Publisher,
	log *logrus.Logger,
) *SPCService {
	return &SPCService{
		spcRepo:   spcRepo,
		publisher: publisher,
		log:       log,
	}
}

// CheckViolation evaluates a metric value against active control limits for a machine.
// If the value exceeds UCL or falls below LCL, a violation record is created and
// a notification event is published.
func (s *SPCService) CheckViolation(ctx context.Context, machineID uuid.UUID, metricType string, value float64) error {
	// Look up active control limits for this machine and metric
	limits, err := s.spcRepo.ListLimits(ctx, machineID)
	if err != nil {
		return fmt.Errorf("failed to retrieve control limits: %w", err)
	}

	// Find the active limit for this metric type
	var activeLimit *repositories.SPCControlLimit
	for i, cl := range limits {
		if cl.MetricType == metricType && cl.IsActive {
			activeLimit = &limits[i]
			break
		}
	}

	if activeLimit == nil {
		// No active control limit for this metric; nothing to check
		return nil
	}

	var violationType string
	var limitValue float64

	if value > activeLimit.UCL {
		violationType = "above_ucl"
		limitValue = activeLimit.UCL
	} else if value < activeLimit.LCL {
		violationType = "below_lcl"
		limitValue = activeLimit.LCL
	} else {
		// Value is within control limits
		return nil
	}

	// Create violation record
	violation := &repositories.SPCViolation{
		TenantID:       activeLimit.TenantID,
		ControlLimitID: activeLimit.ID,
		MachineID:      machineID,
		ViolationType:  violationType,
		MetricType:     metricType,
		Value:          value,
		LimitValue:     limitValue,
		DetectedAt:     time.Now().UTC(),
		Acknowledged:   false,
	}

	if err := s.spcRepo.CreateViolation(ctx, violation); err != nil {
		return fmt.Errorf("failed to create SPC violation: %w", err)
	}

	s.log.WithFields(logrus.Fields{
		"machine_id":     machineID,
		"metric_type":    metricType,
		"violation_type": violationType,
		"value":          value,
		"limit_value":    limitValue,
		"violation_id":   violation.ID,
	}).Warn("SPC violation detected")

	// Publish violation notification
	if s.publisher != nil {
		notifData := pubsub.NotificationData{
			Title:    fmt.Sprintf("SPC Violation: %s", metricType),
			Message:  fmt.Sprintf("Value %.4f exceeded %s limit (%.4f) for metric %s", value, violationType, limitValue, metricType),
			Severity: "warning",
			Source:   "machine",
			SourceID: &machineID,
			Metadata: map[string]interface{}{
				"violation_id":   violation.ID,
				"machine_id":     machineID,
				"metric_type":    metricType,
				"violation_type": violationType,
				"value":          value,
				"limit_value":    limitValue,
			},
		}

		if err := s.publisher.PublishNotification(ctx, activeLimit.TenantID, notifData); err != nil {
			s.log.WithError(err).Warn("Failed to publish SPC violation notification")
			// Non-critical: do not fail the operation
		}

		// Also publish to the analytics namespace
		event := pubsub.NewEvent(pubsub.EventSPCViolation, activeLimit.TenantID, map[string]interface{}{
			"violation_id":   violation.ID,
			"machine_id":     machineID,
			"metric_type":    metricType,
			"violation_type": violationType,
			"value":          value,
			"limit_value":    limitValue,
			"detected_at":    violation.DetectedAt,
		})

		if err := s.publisher.Publish(ctx, pubsub.NamespaceAnalytics, activeLimit.TenantID, event); err != nil {
			s.log.WithError(err).Warn("Failed to publish SPC violation analytics event")
		}
	}

	return nil
}

// ComputeLimits calculates control limits from historical telemetry data and
// persists them via upsert.
func (s *SPCService) ComputeLimits(ctx context.Context, machineID uuid.UUID, metricType string, sampleDays int) (*repositories.SPCControlLimit, error) {
	limit, err := s.spcRepo.ComputeLimits(ctx, machineID, metricType, sampleDays)
	if err != nil {
		return nil, fmt.Errorf("failed to compute SPC limits: %w", err)
	}

	// Extract tenant from context if available
	tenantID := uuid.Nil
	if tid, ok := ctx.Value("tenant_id").(string); ok {
		tenantID, _ = uuid.Parse(tid)
	}
	limit.TenantID = tenantID

	// Persist the computed limits
	if err := s.spcRepo.UpsertLimit(ctx, limit); err != nil {
		return nil, fmt.Errorf("failed to persist computed SPC limits: %w", err)
	}

	s.log.WithFields(logrus.Fields{
		"machine_id":   machineID,
		"metric_type":  metricType,
		"mean":         limit.Mean,
		"stddev":       limit.Stddev,
		"ucl":          limit.UCL,
		"lcl":          limit.LCL,
		"sample_count": limit.SampleCount,
	}).Info("SPC control limits computed and saved")

	return limit, nil
}
