# Database Layer

PostgreSQL database connection and repository implementations.

## Overview

This package provides:
- **Connection** - PostgreSQL connection with RLS support
- **Repositories** - Data access layer for each entity
- **Migrations** - Schema versioning (external tool)

## Connection

```go
db, err := db.NewConnection(cfg.Database)
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### Multi-Tenant RLS

Row-Level Security is enforced at the database level. The tenant ID is set on each connection:

```go
func (db *DB) SetTenant(ctx context.Context, tenantID string) error {
    _, err := db.ExecContext(ctx, "SET app.tenant_id = $1", tenantID)
    return err
}
```

### Health Check

```go
if err := db.Health(); err != nil {
    // Database unhealthy
}
```

### Connection Stats

```go
stats := db.Stats()
// stats.OpenConnections, stats.InUse, stats.Idle
```

## Repositories

### Pattern

```go
type TaskRepository struct {
    db *DB
}

func (r *TaskRepository) List(ctx context.Context, filter TaskFilter) ([]*types.Task, int, error) {
    // Build query with filter
    // Execute and scan results
    // Return tasks and total count
}

func (r *TaskRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.Task, error) {
    // Query by ID
    // Return task or nil if not found
}

func (r *TaskRepository) Create(ctx context.Context, task *types.Task) error {
    // Insert and set generated ID
}

func (r *TaskRepository) Update(ctx context.Context, task *types.Task) error {
    // Update existing record
}

func (r *TaskRepository) Delete(ctx context.Context, id uuid.UUID) error {
    // Soft delete or hard delete
}
```

### Filter Pattern

```go
type TaskFilter struct {
    OrderID   *uuid.UUID
    Status    *types.TaskStatus
    MachineID *uuid.UUID
    Limit     int
    Offset    int
}

func (r *TaskRepository) List(ctx context.Context, f TaskFilter) ([]*types.Task, int, error) {
    query := `SELECT * FROM tasks WHERE 1=1`
    args := []interface{}{}
    argIdx := 1

    if f.OrderID != nil {
        query += fmt.Sprintf(" AND order_id = $%d", argIdx)
        args = append(args, *f.OrderID)
        argIdx++
    }
    // ...
}
```

## Directory Structure

```
internal/db/
├── db.go              # Connection and health
├── repositories/
│   ├── order.go       # Order repository
│   ├── order_item.go  # Order item repository
│   ├── task.go        # Task repository
│   ├── machine.go     # Machine repository
│   ├── telemetry.go   # Telemetry repository
│   ├── quality.go     # Quality certificate repo
│   ├── inspection.go  # Inspection repository
│   ├── batch_lot.go   # Batch lot repository
│   ├── oee_repository.go
│   ├── maintenance_repository.go
│   ├── product_repository.go
│   ├── genealogy_repository.go
│   ├── work_instruction_repository.go
│   ├── spc_repository.go
│   └── inventory_repository.go
└── migrations/        # SQL migration files
```

## Repositories

| Repository | Entity | Special Methods |
|------------|--------|-----------------|
| `OrderRepository` | Order | `GetByExternalID`, `UpdateStatus` |
| `OrderItemRepository` | OrderItem | `ListByOrder` |
| `TaskRepository` | Task | `Move`, `GetKanbanBoard` |
| `MachineRepository` | Machine | `UpdateStatus`, `Heartbeat` |
| `TelemetryRepository` | Telemetry | `CreateBatch`, `GetAggregated` |
| `QualityCertificateRepository` | QualityCertificate | - |
| `InspectionRepository` | Inspection | `Complete` |
| `BatchLotRepository` | BatchLot | - |
| `OEERepository` | OEESnapshot | `ComputeForMachine`, `GetFleetSummary` |
| `MaintenanceRepository` | MaintenanceSchedule, WorkOrder | `ListSchedules`, `CompleteWorkOrder`, `GetByMachine` |
| `ProductRepository` | ProductDefinition, BOMItem | `GetBySKU`, `AddBOMItem`, `DeleteBOMItem` |
| `GenealogyRepository` | ProductGenealogy | `Seal`, `GetTree` |
| `WorkInstructionRepository` | WorkInstruction, TaskWorkInstruction | `AttachToTask`, `AcknowledgeStep` |
| `SPCRepository` | SPCControlLimit, SPCViolation | `ComputeLimits`, `UpsertLimit`, `AcknowledgeViolation` |
| `InventoryRepository` | InventoryItem, InventoryTransaction | `AdjustQuantity`, `UpsertByForgeSightID`, `GetLowStock` |

## Transactions

```go
tx, err := db.BeginTx(ctx, nil)
if err != nil {
    return err
}
defer tx.Rollback()

// Operations with tx
if err := orderRepo.WithTx(tx).Create(ctx, order); err != nil {
    return err
}

return tx.Commit()
```
