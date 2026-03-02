# Services

Business logic and automation services.

## Overview

This package contains services that orchestrate complex business operations spanning multiple repositories or external systems.

## AutomationService

Handles Kanban-Machine control plane automation:

```go
automation := services.NewAutomationService(
    machineRepo,
    publisher,
    log,
)
```

### Task Automation

When a task moves to `in_progress` with an assigned machine:

```go
func (s *AutomationService) OnTaskMoved(ctx context.Context, task *types.Task) error {
    // 1. Check if task has assigned machine
    if task.MachineID == nil {
        return nil
    }

    // 2. Get machine details
    machine, err := s.machineRepo.GetByID(ctx, *task.MachineID)
    if err != nil {
        return err
    }

    // 3. Validate machine state
    if machine.Status != types.MachineStatusIdle {
        return ErrMachineNotReady
    }

    // 4. Dispatch start_job command
    cmd := Command{
        Type: "start_job",
        Parameters: map[string]interface{}{
            "task_id": task.ID,
            "order_id": task.OrderID,
        },
    }

    return s.publisher.PublishCommand(ctx, machine.TenantID, machine.ID, cmd)
}
```

### Machine Command Flow

```
Task moved to in_progress
    ↓
AutomationService.OnTaskMoved()
    ↓
Validate machine state (idle)
    ↓
Publish start_job command to Redis
    ↓
Telemetry Worker dispatches to MQTT
    ↓
Machine receives command
```

## Usage in Handlers

```go
type TaskHandler struct {
    repo       *repositories.TaskRepository
    automation *services.AutomationService
}

func (h *TaskHandler) Move(c *gin.Context) {
    // ... update task status ...

    // Trigger automation if moving to in_progress
    if req.Status == types.TaskStatusInProgress && task.MachineID != nil {
        if err := h.automation.OnTaskMoved(ctx, task); err != nil {
            h.log.WithError(err).Warn("Automation trigger failed")
            // Non-blocking - task still moved
        }
    }
}
```

## Files

| File | Description |
|------|-------------|
| `automation.go` | Kanban-machine automation |

## Future Services

Placeholder for additional business logic services:
- `NotificationService` - Email/SMS notifications
- `SchedulingService` - Task scheduling optimization
- `ReportingService` - Report generation
