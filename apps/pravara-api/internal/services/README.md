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

## OEEService

Computes Overall Equipment Effectiveness (availability x performance x quality) for machines:

- Daily OEE snapshots per machine based on telemetry and task data
- Fleet-wide OEE computation across all machines for a tenant
- Stores results as `OEESnapshot` records for trend analysis

## MaintenanceService

Manages CMMS work order lifecycle and schedule advancement:

- Completes work orders and advances the parent schedule's next due date
- Supports calendar, runtime_hours, cycle_count, and condition-based triggers
- Publishes maintenance events on status transitions

## GenealogyService

Auto-creates product genealogy records from task completion:

- Builds traceability chain: product definition -> order -> task -> machine -> quality certificate
- Generates digital birth certificates with full production lineage
- Seals genealogy records with SHA-256 hash for tamper detection

## WorkInstructionService

Auto-attaches work instructions when tasks are queued:

- Matches instructions to tasks by product or machine type
- Tracks step-by-step acknowledgement by operators
- Publishes acknowledgement events for compliance auditing

## SPCService

Statistical Process Control with Western Electric rules:

- Computes control limits (UCL/LCL = mean +/- 3 sigma) from telemetry history
- Checks for violations: above_ucl, below_lcl, run_of_7, trend
- Publishes `analytics.spc_violation` events for real-time alerting

## InventoryService

Stock management with low-stock alerting and ForgeSight integration:

- Adjusts quantity on hand with transaction logging
- Triggers `inventory.low_stock` events when quantity falls below reorder point
- Accepts ForgeSight webhook payloads for external inventory sync

## Files

| File | Description |
|------|-------------|
| `automation.go` | Kanban-machine automation |
| `oee_service.go` | OEE computation |
| `maintenance_service.go` | CMMS work order lifecycle |
| `genealogy_service.go` | Product genealogy and sealing |
| `work_instruction_service.go` | Work instruction attachment and acknowledgement |
| `spc_service.go` | SPC violation detection |
| `inventory_service.go` | Inventory adjustment and alerts |

## Future Services

Placeholder for additional business logic services:
- `NotificationService` - Email/SMS notifications
- `SchedulingService` - Task scheduling optimization
- `ReportingService` - Report generation
