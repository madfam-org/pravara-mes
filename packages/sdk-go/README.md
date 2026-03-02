# PravaraMES SDK (Go)

Shared Go types and utilities for PravaraMES services.

## Overview

This package provides:
- **Types** - Core domain types (Order, Task, Machine, etc.)
- **Status Enums** - Type-safe status values
- **Validation** - Common validation helpers

## Installation

```go
import "github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
```

## Types

### Order
```go
type Order struct {
    ID            uuid.UUID    `json:"id"`
    TenantID      uuid.UUID    `json:"tenant_id"`
    ExternalID    string       `json:"external_id"`
    CustomerName  string       `json:"customer_name"`
    Status        OrderStatus  `json:"status"`
    Priority      int          `json:"priority"`
    DueDate       *time.Time   `json:"due_date"`
    // ...
}
```

### Task
```go
type Task struct {
    ID          uuid.UUID   `json:"id"`
    TenantID    uuid.UUID   `json:"tenant_id"`
    OrderID     uuid.UUID   `json:"order_id"`
    Title       string      `json:"title"`
    Status      TaskStatus  `json:"status"`
    MachineID   *uuid.UUID  `json:"machine_id"`
    AssignedTo  *uuid.UUID  `json:"assigned_to"`
    // ...
}
```

### Machine
```go
type Machine struct {
    ID         uuid.UUID     `json:"id"`
    TenantID   uuid.UUID     `json:"tenant_id"`
    Name       string        `json:"name"`
    Type       string        `json:"type"`
    Status     MachineStatus `json:"status"`
    MQTTTopic  string        `json:"mqtt_topic"`
    // ...
}
```

## Status Enums

### OrderStatus
```go
const (
    OrderStatusReceived   OrderStatus = "received"
    OrderStatusPlanning   OrderStatus = "planning"
    OrderStatusInProgress OrderStatus = "in_progress"
    OrderStatusCompleted  OrderStatus = "completed"
    OrderStatusCancelled  OrderStatus = "cancelled"
)
```

### TaskStatus
```go
const (
    TaskStatusBacklog    TaskStatus = "backlog"
    TaskStatusTodo       TaskStatus = "todo"
    TaskStatusInProgress TaskStatus = "in_progress"
    TaskStatusReview     TaskStatus = "review"
    TaskStatusDone       TaskStatus = "done"
)
```

### MachineStatus
```go
const (
    MachineStatusOffline  MachineStatus = "offline"
    MachineStatusIdle     MachineStatus = "idle"
    MachineStatusRunning  MachineStatus = "running"
    MachineStatusPaused   MachineStatus = "paused"
    MachineStatusError    MachineStatus = "error"
)
```

## Quality Types

### QualityCertificate
```go
type QualityCertificate struct {
    ID                uuid.UUID           `json:"id"`
    TenantID          uuid.UUID           `json:"tenant_id"`
    CertificateNumber string              `json:"certificate_number"`
    Type              QualityCertType     `json:"type"`
    Status            QualityCertStatus   `json:"status"`
    // ...
}
```

### Inspection
```go
type Inspection struct {
    ID               uuid.UUID         `json:"id"`
    TenantID         uuid.UUID         `json:"tenant_id"`
    InspectionNumber string            `json:"inspection_number"`
    Type             string            `json:"type"`
    Result           InspectionResult  `json:"result"`
    // ...
}
```

### BatchLot
```go
type BatchLot struct {
    ID          uuid.UUID `json:"id"`
    TenantID    uuid.UUID `json:"tenant_id"`
    LotNumber   string    `json:"lot_number"`
    ProductName string    `json:"product_name"`
    Quantity    float64   `json:"quantity"`
    Unit        string    `json:"unit"`
    // ...
}
```

## Telemetry

```go
type Telemetry struct {
    ID         uuid.UUID      `json:"id"`
    TenantID   uuid.UUID      `json:"tenant_id"`
    MachineID  uuid.UUID      `json:"machine_id"`
    Timestamp  time.Time      `json:"timestamp"`
    MetricType string         `json:"metric_type"`
    Value      float64        `json:"value"`
    Unit       string         `json:"unit"`
    Metadata   map[string]any `json:"metadata"`
}
```

## Directory Structure

```
packages/sdk-go/
├── pkg/
│   └── types/
│       ├── order.go       # Order types
│       ├── task.go        # Task types
│       ├── machine.go     # Machine types
│       ├── quality.go     # Quality types
│       ├── telemetry.go   # Telemetry types
│       └── status.go      # Status enums
└── go.mod
```

## Usage in Services

```go
import "github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"

func HandleTask(task *types.Task) error {
    if task.Status == types.TaskStatusInProgress {
        // Dispatch command to machine
    }
    return nil
}
```
