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
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// MachineValidation represents the result of validating a machine for automation.
type MachineValidation struct {
	Valid   bool
	Blocked bool
	Warning string
	Error   string
	Machine *types.Machine
}

// AutomationService handles automated task-machine interactions.
type AutomationService struct {
	taskRepo    *repositories.TaskRepository
	machineRepo *repositories.MachineRepository
	taskCmdRepo *repositories.TaskCommandRepository
	publisher   *pubsub.Publisher
	log         *logrus.Logger
}

// NewAutomationService creates a new automation service.
func NewAutomationService(
	taskRepo *repositories.TaskRepository,
	machineRepo *repositories.MachineRepository,
	taskCmdRepo *repositories.TaskCommandRepository,
	publisher *pubsub.Publisher,
	log *logrus.Logger,
) *AutomationService {
	return &AutomationService{
		taskRepo:    taskRepo,
		machineRepo: machineRepo,
		taskCmdRepo: taskCmdRepo,
		publisher:   publisher,
		log:         log,
	}
}

// OnTaskStatusChange is called when a task's status changes.
// It triggers automation based on the status transition.
func (s *AutomationService) OnTaskStatusChange(
	ctx context.Context,
	task *types.Task,
	oldStatus, newStatus types.TaskStatus,
	userID uuid.UUID,
) error {
	s.log.WithFields(logrus.Fields{
		"task_id":    task.ID,
		"old_status": oldStatus,
		"new_status": newStatus,
		"machine_id": task.MachineID,
	}).Debug("Processing task status change for automation")

	// Task moved to in_progress with machine assigned → dispatch start_job
	if newStatus == types.TaskStatusInProgress && task.MachineID != nil {
		return s.dispatchStartJobCommand(ctx, task, userID)
	}

	return nil
}

// ValidateMachineForTask validates that a machine can accept automated commands.
func (s *AutomationService) ValidateMachineForTask(ctx context.Context, machineID uuid.UUID) (*MachineValidation, error) {
	machine, err := s.machineRepo.GetByID(ctx, machineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get machine: %w", err)
	}

	if machine == nil {
		return &MachineValidation{
			Valid: false,
			Error: "Machine not found",
		}, nil
	}

	validation := &MachineValidation{
		Valid:   true,
		Machine: machine,
	}

	// Check machine status
	switch machine.Status {
	case types.MachineStatusError:
		validation.Valid = false
		validation.Blocked = true
		validation.Error = "Machine is in error state"
	case types.MachineStatusMaintenance:
		validation.Valid = false
		validation.Blocked = true
		validation.Error = "Machine is under maintenance"
	case types.MachineStatusOffline:
		// Allow with warning - machine may come online
		validation.Warning = "Machine is currently offline - command will be queued"
	case types.MachineStatusRunning:
		validation.Warning = "Machine is currently running a job - command will be queued"
	}

	// Check for MQTT topic configuration
	if machine.MQTTTopic == "" {
		validation.Valid = false
		validation.Error = "Machine does not have an MQTT topic configured for command dispatch"
	}

	return validation, nil
}

// dispatchStartJobCommand dispatches a start_job command when a task moves to in_progress.
func (s *AutomationService) dispatchStartJobCommand(ctx context.Context, task *types.Task, userID uuid.UUID) error {
	if task.MachineID == nil {
		return nil // No machine assigned, nothing to dispatch
	}

	// Validate machine is ready for commands
	validation, err := s.ValidateMachineForTask(ctx, *task.MachineID)
	if err != nil {
		return fmt.Errorf("failed to validate machine: %w", err)
	}

	if !validation.Valid {
		s.log.WithFields(logrus.Fields{
			"task_id":    task.ID,
			"machine_id": *task.MachineID,
			"error":      validation.Error,
		}).Warn("Machine validation failed for automation")

		// If blocked, we might want to notify the UI
		if validation.Blocked && s.publisher != nil {
			s.publisher.PublishNotification(ctx, task.TenantID, pubsub.NotificationData{
				Title:    "Automation Blocked",
				Message:  fmt.Sprintf("Cannot start job on %s: %s", validation.Machine.Name, validation.Error),
				Severity: "warning",
				Source:   "task",
				SourceID: &task.ID,
			})
		}
		return fmt.Errorf("machine not ready: %s", validation.Error)
	}

	// Log warning if there's one (e.g., machine offline)
	if validation.Warning != "" {
		s.log.WithFields(logrus.Fields{
			"task_id":    task.ID,
			"machine_id": *task.MachineID,
			"warning":    validation.Warning,
		}).Warn("Machine validation warning")
	}

	machine := validation.Machine

	// Generate command ID
	commandID := uuid.New()
	now := time.Now().UTC()

	// Build command parameters
	parameters := map[string]interface{}{
		"task_title": task.Title,
	}
	if task.OrderID != nil {
		parameters["order_id"] = task.OrderID.String()
	}
	if task.Metadata != nil {
		// Include any task metadata that might be useful for the machine
		if fileID, ok := task.Metadata["file_id"]; ok {
			parameters["file_id"] = fileID
		}
		if fileName, ok := task.Metadata["file_name"]; ok {
			parameters["file_name"] = fileName
		}
	}

	// Create task command record for tracking
	taskCmd := &repositories.TaskCommand{
		ID:          uuid.New(),
		TenantID:    task.TenantID,
		TaskID:      task.ID,
		MachineID:   *task.MachineID,
		CommandID:   commandID,
		CommandType: string(pubsub.CommandStartJob),
		Status:      "pending",
		Parameters:  parameters,
		IssuedBy:    &userID,
		IssuedAt:    now,
	}

	if err := s.taskCmdRepo.Create(ctx, taskCmd); err != nil {
		return fmt.Errorf("failed to create task command record: %w", err)
	}

	// Build command data for dispatch
	commandData := pubsub.MachineCommandData{
		CommandID:   commandID,
		MachineID:   machine.ID,
		MachineName: machine.Name,
		MQTTTopic:   machine.MQTTTopic,
		Command:     pubsub.CommandStartJob,
		Parameters:  parameters,
		TaskID:      &task.ID,
		OrderID:     task.OrderID,
		IssuedBy:    userID,
		IssuedAt:    now,
	}

	// Publish to Centrifugo for UI real-time updates
	if s.publisher != nil {
		if err := s.publisher.PublishMachineCommand(ctx, task.TenantID, commandData); err != nil {
			s.log.WithError(err).WithFields(logrus.Fields{
				"task_id":    task.ID,
				"machine_id": machine.ID,
				"command_id": commandID,
			}).Error("Failed to publish automation command to Centrifugo")
			// Continue - Centrifugo publish is not critical
		}

		// Publish to command dispatch channel for telemetry-worker
		if err := s.publisher.PublishCommandForDispatch(ctx, task.TenantID, commandData); err != nil {
			s.log.WithError(err).WithFields(logrus.Fields{
				"task_id":    task.ID,
				"machine_id": machine.ID,
				"command_id": commandID,
			}).Error("Failed to dispatch automation command")

			// Update command status to failed
			s.taskCmdRepo.UpdateStatus(ctx, commandID, "failed", "Failed to dispatch: "+err.Error())

			return fmt.Errorf("failed to dispatch command: %w", err)
		}

		// Update command status to sent
		if err := s.taskCmdRepo.UpdateStatus(ctx, commandID, "sent", ""); err != nil {
			s.log.WithError(err).Warn("Failed to update command status to sent")
		}
	}

	s.log.WithFields(logrus.Fields{
		"task_id":    task.ID,
		"machine_id": machine.ID,
		"command_id": commandID,
	}).Info("Automation command dispatched: start_job")

	return nil
}

// OnMachineStatusChange handles machine status changes for automation.
// When a machine enters error state, it may need to block associated tasks.
func (s *AutomationService) OnMachineStatusChange(
	ctx context.Context,
	machineID uuid.UUID,
	oldStatus, newStatus types.MachineStatus,
) error {
	s.log.WithFields(logrus.Fields{
		"machine_id": machineID,
		"old_status": oldStatus,
		"new_status": newStatus,
	}).Debug("Processing machine status change for automation")

	// Machine went into error state → block associated in_progress tasks
	if newStatus == types.MachineStatusError {
		return s.blockTasksForMachine(ctx, machineID)
	}

	return nil
}

// blockTasksForMachine moves all in_progress tasks for a machine to blocked status.
func (s *AutomationService) blockTasksForMachine(ctx context.Context, machineID uuid.UUID) error {
	// Get in_progress tasks for this machine
	status := types.TaskStatusInProgress
	tasks, _, err := s.taskRepo.List(ctx, repositories.TaskFilter{
		Status:    &status,
		MachineID: &machineID,
		Limit:     100, // Reasonable limit for one machine
	})
	if err != nil {
		return fmt.Errorf("failed to get tasks for machine: %w", err)
	}

	if len(tasks) == 0 {
		return nil
	}

	machine, _ := s.machineRepo.GetByID(ctx, machineID)
	machineName := "Unknown"
	var tenantID uuid.UUID
	if machine != nil {
		machineName = machine.Name
		tenantID = machine.TenantID
	}

	s.log.WithFields(logrus.Fields{
		"machine_id": machineID,
		"task_count": len(tasks),
	}).Warn("Blocking tasks due to machine error")

	for _, task := range tasks {
		// Move task to blocked status
		if err := s.taskRepo.MoveTask(ctx, task.ID, types.TaskStatusBlocked, task.KanbanPosition); err != nil {
			s.log.WithError(err).WithField("task_id", task.ID).Error("Failed to block task")
			continue
		}

		// Publish notification
		if s.publisher != nil && tenantID != uuid.Nil {
			s.publisher.PublishNotification(ctx, tenantID, pubsub.NotificationData{
				Title:    "Task Blocked",
				Message:  fmt.Sprintf("Task '%s' blocked due to machine error on %s", task.Title, machineName),
				Severity: "warning",
				Source:   "task",
				SourceID: &task.ID,
			})
		}
	}

	return nil
}

// GetActiveCommand returns the active command for a task, if any.
func (s *AutomationService) GetActiveCommand(ctx context.Context, taskID uuid.UUID) (*repositories.TaskCommand, error) {
	return s.taskCmdRepo.GetActiveByTaskID(ctx, taskID)
}

// GetCommandHistory returns the command history for a task.
func (s *AutomationService) GetCommandHistory(ctx context.Context, taskID uuid.UUID) ([]repositories.TaskCommand, error) {
	return s.taskCmdRepo.GetByTaskID(ctx, taskID)
}
