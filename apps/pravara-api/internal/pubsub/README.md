# Pub/Sub

Redis-based publish/subscribe for real-time events.

## Overview

The pubsub package provides:
- **Publisher** - Publishes events to Redis channels
- **Event Types** - Structured event payloads
- **Centrifugo Integration** - Events forwarded to WebSocket clients

## Architecture

```
API Handler → Publisher → Redis → Centrifugo → WebSocket Clients
```

## Publisher

```go
publisher, err := pubsub.NewPublisher(pubsub.PublisherConfig{
    RedisURL: cfg.Redis.URL,
}, log)
if err != nil {
    log.Warn("Continuing without real-time events")
}
defer publisher.Close()
```

### Publishing Events

```go
// Publish task update
err := publisher.PublishTaskUpdate(ctx, tenantID, task)

// Publish machine status change
err := publisher.PublishMachineStatus(ctx, tenantID, machine)

// Publish order update
err := publisher.PublishOrderUpdate(ctx, tenantID, order)
```

## Channel Naming

Channels are namespaced by tenant for isolation:

```
pravara:{tenant_id}:tasks       # Task updates
pravara:{tenant_id}:machines    # Machine status
pravara:{tenant_id}:orders      # Order updates
pravara:{tenant_id}:telemetry   # Telemetry data
```

## Event Format

```go
type Event struct {
    Type      string      `json:"type"`      // e.g., "task_updated"
    Timestamp time.Time   `json:"timestamp"`
    TenantID  string      `json:"tenant_id"`
    Data      interface{} `json:"data"`      // Entity payload
}
```

### Event Types

| Type | Channel | Description |
|------|---------|-------------|
| `task_created` | tasks | New task created |
| `task_updated` | tasks | Task modified |
| `task_moved` | tasks | Task status/position changed |
| `task_deleted` | tasks | Task removed |
| `machine_status` | machines | Machine status changed |
| `machine_heartbeat` | machines | Machine heartbeat received |
| `order_created` | orders | New order created |
| `order_updated` | orders | Order modified |

## Centrifugo Integration

Events published to Redis are consumed by Centrifugo and broadcast to subscribed WebSocket clients. The Centrifugo Redis engine configuration:

```json
{
  "engine": "redis",
  "redis_address": "redis:6379",
  "redis_db": 0
}
```

## Usage in Handlers

```go
func (h *TaskHandler) Move(c *gin.Context) {
    // ... update task in database ...

    // Publish real-time event
    if h.publisher != nil {
        tenantID, _ := middleware.GetTenantID(c)
        if err := h.publisher.PublishTaskUpdate(ctx, tenantID, task); err != nil {
            h.log.WithError(err).Warn("Failed to publish task update")
            // Continue - real-time is best-effort
        }
    }

    c.JSON(http.StatusOK, task)
}
```

## Files

| File | Description |
|------|-------------|
| `publisher.go` | Redis publisher implementation |
| `events.go` | Event type definitions |
