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

// MaintenanceService manages maintenance schedule logic and work order lifecycle.
type MaintenanceService struct {
	maintRepo *repositories.MaintenanceRepository
	publisher *pubsub.Publisher
	log       *logrus.Logger
}

// NewMaintenanceService creates a new maintenance service.
func NewMaintenanceService(
	maintRepo *repositories.MaintenanceRepository,
	publisher *pubsub.Publisher,
	log *logrus.Logger,
) *MaintenanceService {
	return &MaintenanceService{
		maintRepo: maintRepo,
		publisher: publisher,
		log:       log,
	}
}

// CheckOverdue checks if any maintenance schedule for the given machine has exceeded
// its next_due_hours threshold. If overdue, it auto-creates a work order with status 'overdue'
// and publishes a maintenance.overdue notification.
func (s *MaintenanceService) CheckOverdue(ctx context.Context, machineID uuid.UUID, currentHours float64) error {
	// Query active schedules for this machine that are hours-based and overdue
	query := `
		SELECT id, tenant_id, machine_id, name, description, trigger_type, priority,
		       interval_days, interval_hours, last_done_hours, next_due_hours,
		       interval_cycles, last_done_cycles, next_due_cycles,
		       condition_metric, condition_threshold,
		       last_done_at, next_due_at, assigned_to, is_active,
		       metadata, created_at, updated_at
		FROM maintenance_schedules
		WHERE machine_id = $1
		  AND is_active = true
		  AND next_due_hours IS NOT NULL
		  AND next_due_hours <= $2
	`

	rows, err := s.maintRepo.DB().QueryContext(ctx, query, machineID, currentHours)
	if err != nil {
		return fmt.Errorf("failed to query overdue schedules: %w", err)
	}
	defer rows.Close()

	type overdueSchedule struct {
		ID       uuid.UUID
		TenantID uuid.UUID
		Name     string
		Priority int
	}

	var overdueSchedules []overdueSchedule
	for rows.Next() {
		// We only need a few fields for work order creation
		var schedule repositories.MaintenanceSchedule
		// Use a simplified scan for the fields we need
		var desc, condMetric, assignedTo interface{}
		var intervalDays, intervalCycles, lastDoneCycles, nextDueCycles interface{}
		var intervalHours, lastDoneHours, nextDueHours, condThreshold interface{}
		var lastDoneAt, nextDueAt interface{}
		var metadataJSON []byte

		err := rows.Scan(
			&schedule.ID, &schedule.TenantID, &schedule.MachineID,
			&schedule.Name, &desc, &schedule.TriggerType, &schedule.Priority,
			&intervalDays, &intervalHours, &lastDoneHours, &nextDueHours,
			&intervalCycles, &lastDoneCycles, &nextDueCycles,
			&condMetric, &condThreshold,
			&lastDoneAt, &nextDueAt, &assignedTo, &schedule.IsActive,
			&metadataJSON, &schedule.CreatedAt, &schedule.UpdatedAt,
		)
		if err != nil {
			s.log.WithError(err).Warn("Failed to scan overdue schedule row, skipping")
			continue
		}

		overdueSchedules = append(overdueSchedules, overdueSchedule{
			ID:       schedule.ID,
			TenantID: schedule.TenantID,
			Name:     schedule.Name,
			Priority: schedule.Priority,
		})
	}

	for _, sched := range overdueSchedules {
		// Check if an overdue work order already exists for this schedule
		existsQuery := `
			SELECT COUNT(*) FROM maintenance_work_orders
			WHERE schedule_id = $1 AND status = 'overdue'
		`
		var count int
		if err := s.maintRepo.DB().QueryRowContext(ctx, existsQuery, sched.ID).Scan(&count); err != nil {
			s.log.WithError(err).Warn("Failed to check existing overdue work order")
			continue
		}
		if count > 0 {
			continue // Already has an overdue work order
		}

		// Create overdue work order
		wo := &repositories.MaintenanceWorkOrder{
			TenantID:        sched.TenantID,
			ScheduleID:      &sched.ID,
			MachineID:       machineID,
			WorkOrderNumber: fmt.Sprintf("WO-OD-%s", uuid.New().String()[:8]),
			Title:           fmt.Sprintf("OVERDUE: %s", sched.Name),
			Description:     fmt.Sprintf("Maintenance schedule '%s' is overdue at %.1f hours", sched.Name, currentHours),
			Status:          "overdue",
			Priority:        sched.Priority,
		}

		if err := s.maintRepo.CreateWorkOrder(ctx, wo); err != nil {
			s.log.WithError(err).WithField("schedule_id", sched.ID).Error("Failed to create overdue work order")
			continue
		}

		s.log.WithFields(logrus.Fields{
			"schedule_id":    sched.ID,
			"work_order_id":  wo.ID,
			"machine_id":     machineID,
			"current_hours":  currentHours,
		}).Warn("Maintenance schedule overdue, work order created")

		// Publish maintenance.overdue notification
		if s.publisher != nil {
			s.publisher.PublishNotification(ctx, sched.TenantID, pubsub.NotificationData{
				Title:    fmt.Sprintf("Maintenance Overdue: %s", sched.Name),
				Message:  fmt.Sprintf("Maintenance schedule '%s' is overdue at %.1f hours. Work order %s created.", sched.Name, currentHours, wo.WorkOrderNumber),
				Severity: "warning",
				Source:   "maintenance",
				Metadata: map[string]interface{}{
					"schedule_id":    sched.ID.String(),
					"work_order_id":  wo.ID.String(),
					"machine_id":     machineID.String(),
					"current_hours":  currentHours,
				},
			})
		}
	}

	return nil
}

// CompleteWorkOrder completes a work order and advances the associated schedule's
// last_done_*/next_due_* fields. Publishes a maintenance.completed event.
func (s *MaintenanceService) CompleteWorkOrder(ctx context.Context, id uuid.UUID, notes string) error {
	// Get the work order to find the schedule
	wo, err := s.maintRepo.GetWorkOrderByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get work order: %w", err)
	}
	if wo == nil {
		return fmt.Errorf("work order not found")
	}

	// Complete the work order
	if err := s.maintRepo.CompleteWorkOrder(ctx, id, notes); err != nil {
		return fmt.Errorf("failed to complete work order: %w", err)
	}

	// If this work order is linked to a schedule, advance the schedule
	if wo.ScheduleID != nil {
		schedule, err := s.maintRepo.GetScheduleByID(ctx, *wo.ScheduleID)
		if err != nil {
			s.log.WithError(err).WithField("schedule_id", wo.ScheduleID).Warn("Failed to get schedule for advancement")
		} else if schedule != nil {
			now := time.Now()
			schedule.LastDoneAt = &now

			// Advance hours-based schedule
			if schedule.IntervalHours != nil && schedule.NextDueHours != nil {
				lastDone := *schedule.NextDueHours
				schedule.LastDoneHours = &lastDone
				nextDue := lastDone + *schedule.IntervalHours
				schedule.NextDueHours = &nextDue
			}

			// Advance cycle-based schedule
			if schedule.IntervalCycles != nil && schedule.NextDueCycles != nil {
				lastDone := *schedule.NextDueCycles
				schedule.LastDoneCycles = &lastDone
				nextDue := lastDone + *schedule.IntervalCycles
				schedule.NextDueCycles = &nextDue
			}

			// Advance time-based schedule
			if schedule.IntervalDays != nil {
				nextDue := now.AddDate(0, 0, *schedule.IntervalDays)
				schedule.NextDueAt = &nextDue
			}

			if err := s.maintRepo.UpdateSchedule(ctx, schedule); err != nil {
				s.log.WithError(err).WithField("schedule_id", schedule.ID).Warn("Failed to advance schedule after completion")
			}
		}
	}

	s.log.WithFields(logrus.Fields{
		"work_order_id": id,
		"machine_id":    wo.MachineID,
	}).Info("Maintenance work order completed")

	// Publish maintenance.completed event
	if s.publisher != nil {
		event := pubsub.NewEvent("maintenance.completed", wo.TenantID, map[string]interface{}{
			"work_order_id":     id.String(),
			"work_order_number": wo.WorkOrderNumber,
			"machine_id":        wo.MachineID.String(),
			"title":             wo.Title,
			"completed_at":      time.Now().UTC(),
		})
		if err := s.publisher.Publish(ctx, pubsub.NamespaceMachines, wo.TenantID, event); err != nil {
			s.log.WithError(err).Warn("Failed to publish maintenance completed event")
		}
	}

	return nil
}
