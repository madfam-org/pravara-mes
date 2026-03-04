// Package services provides business logic services for PravaraMES.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
)

// WorkInstructionService manages work instruction business logic.
type WorkInstructionService struct {
	wiRepo    *repositories.WorkInstructionRepository
	publisher *pubsub.Publisher
	log       *logrus.Logger
}

// NewWorkInstructionService creates a new work instruction service.
func NewWorkInstructionService(
	wiRepo *repositories.WorkInstructionRepository,
	publisher *pubsub.Publisher,
	log *logrus.Logger,
) *WorkInstructionService {
	return &WorkInstructionService{
		wiRepo:    wiRepo,
		publisher: publisher,
		log:       log,
	}
}

// AutoAttachToTask looks up active work instructions matching the given product definition
// and machine type, then creates task_work_instructions records for each match.
func (s *WorkInstructionService) AutoAttachToTask(ctx context.Context, taskID, tenantID uuid.UUID, productDefID *uuid.UUID, machineType *string) error {
	instructions, err := s.wiRepo.GetByProductAndMachineType(ctx, productDefID, machineType)
	if err != nil {
		return fmt.Errorf("failed to lookup work instructions: %w", err)
	}

	if len(instructions) == 0 {
		s.log.WithFields(logrus.Fields{
			"task_id": taskID,
		}).Debug("No matching work instructions found for auto-attach")
		return nil
	}

	attached := 0
	for _, wi := range instructions {
		twi := &repositories.TaskWorkInstruction{
			TenantID:             tenantID,
			TaskID:               taskID,
			WorkInstructionID:    wi.ID,
			StepAcknowledgements: json.RawMessage("{}"),
			AllAcknowledged:      false,
		}

		if err := s.wiRepo.AttachToTask(ctx, twi); err != nil {
			s.log.WithError(err).WithFields(logrus.Fields{
				"task_id":             taskID,
				"work_instruction_id": wi.ID,
			}).Warn("Failed to auto-attach work instruction to task")
			continue
		}
		attached++
	}

	s.log.WithFields(logrus.Fields{
		"task_id":  taskID,
		"attached": attached,
		"total":    len(instructions),
	}).Info("Auto-attached work instructions to task")

	return nil
}

// AcknowledgeStep records that an operator acknowledged a specific step in a work instruction
// and publishes a real-time event.
func (s *WorkInstructionService) AcknowledgeStep(ctx context.Context, taskID, wiID uuid.UUID, stepNumber int, userID uuid.UUID) error {
	if err := s.wiRepo.AcknowledgeStep(ctx, taskID, wiID, stepNumber, userID); err != nil {
		return fmt.Errorf("failed to acknowledge step: %w", err)
	}

	s.log.WithFields(logrus.Fields{
		"task_id":             taskID,
		"work_instruction_id": wiID,
		"step_number":         stepNumber,
		"user_id":             userID,
	}).Info("Work instruction step acknowledged")

	// Publish acknowledgement event
	if s.publisher != nil {
		tenantID := uuid.Nil
		// Extract tenant from context if available
		if tid, ok := ctx.Value("tenant_id").(string); ok {
			tenantID, _ = uuid.Parse(tid)
		}

		event := pubsub.NewEvent(pubsub.EventWorkInstructionAck, tenantID, map[string]interface{}{
			"task_id":             taskID,
			"work_instruction_id": wiID,
			"step_number":         stepNumber,
			"user_id":             userID,
			"acknowledged_at":     time.Now().UTC(),
		})

		if err := s.publisher.Publish(ctx, pubsub.NamespaceTasks, tenantID, event); err != nil {
			s.log.WithError(err).Warn("Failed to publish work instruction acknowledgement event")
			// Non-critical: do not fail the operation
		}
	}

	return nil
}
