# API Handlers

HTTP handlers and routing for the PravaraMES REST API.

## Overview

This package provides Gin HTTP handlers following the repository pattern. Each handler group maintains its own struct with injected dependencies.

## Handler Groups

| Handler | Endpoints | Description |
|---------|-----------|-------------|
| `HealthHandler` | 3 | Health checks and probes |
| `OrderHandler` | 7 | Order CRUD and items |
| `TaskHandler` | 8 | Task CRUD and Kanban operations |
| `MachineHandler` | 8 | Machine CRUD and commands |
| `TelemetryHandler` | 4 | Telemetry query and batch insert |
| `QualityHandler` | 16 | Certificates, inspections, batch lots |
| `BillingHandler` | 3 | Usage tracking |
| `RealtimeHandler` | 3 | WebSocket token generation |
| `WebhookHandler` | 1 | External webhook processing |

## Request/Response Pattern

### Request Structs
```go
type CreateTaskRequest struct {
    OrderID     uuid.UUID `json:"order_id" binding:"required"`
    Title       string    `json:"title" binding:"required"`
    Description string    `json:"description"`
    // ...
}
```

### Error Response
```go
c.JSON(http.StatusBadRequest, gin.H{
    "error":   "validation_error",
    "message": "Title is required",
})
```

### Success with Data
```go
c.JSON(http.StatusOK, task)
```

### List Response
```go
c.JSON(http.StatusOK, ListResponse{
    Data:   tasks,
    Total:  total,
    Limit:  limit,
    Offset: offset,
})
```

## Handler Pattern

```go
type TaskHandler struct {
    repo       *repositories.TaskRepository
    log        *logrus.Logger
    publisher  *pubsub.Publisher
    automation *services.AutomationService
}

func (h *TaskHandler) Create(c *gin.Context) {
    // 1. Bind and validate request
    var req CreateTaskRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{...})
        return
    }

    // 2. Get tenant from context
    tenantID, _ := middleware.GetTenantID(c)

    // 3. Create entity
    task := &types.Task{...}
    if err := h.repo.Create(ctx, task); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{...})
        return
    }

    // 4. Publish event (if configured)
    if h.publisher != nil {
        h.publisher.Publish(ctx, "task_created", task)
    }

    // 5. Return response
    c.JSON(http.StatusCreated, task)
}
```

## Routing

Routes are registered in `routes.go`:

```go
func RegisterRoutes(r *gin.Engine, db *db.DB, cfg *config.Config, log *logrus.Logger) {
    v1 := r.Group("/v1")

    // Health (no auth)
    health := NewHealthHandler(db, log)
    r.GET("/health", health.Health)
    r.GET("/health/live", health.Liveness)
    r.GET("/health/ready", health.Readiness)

    // Protected routes
    protected := v1.Group("")
    protected.Use(middleware.Auth(cfg.Auth))

    tasks := NewTaskHandler(taskRepo, log, publisher, automation)
    protected.GET("/tasks", tasks.List)
    protected.POST("/tasks", tasks.Create)
    // ...
}
```

## OpenAPI Annotations

Handlers use swaggo annotations for OpenAPI generation:

```go
// Create creates a new task.
// @Summary Create task
// @Description Creates a new task for the Kanban board
// @Tags tasks
// @Accept json
// @Produce json
// @Param body body CreateTaskRequest true "Task data"
// @Success 201 {object} types.Task "Created task"
// @Failure 400 {object} map[string]string "Validation error"
// @Security BearerAuth
// @Router /tasks [post]
func (h *TaskHandler) Create(c *gin.Context) {
```

## Files

| File | Description |
|------|-------------|
| `routes.go` | Route registration |
| `health_handlers.go` | Health check endpoints |
| `order_handlers.go` | Order management |
| `task_handlers.go` | Task and Kanban operations |
| `machine_handlers.go` | Machine management |
| `telemetry_handlers.go` | Telemetry queries |
| `quality_handlers.go` | Quality management |
| `billing_handlers.go` | Usage tracking |
| `realtime_handlers.go` | WebSocket authentication |
| `webhook_handlers.go` | External webhooks |
