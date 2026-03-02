# Command Dispatch

Machine command dispatch and acknowledgment tracking.

## Overview

This package provides:
- **Dispatcher** - Publishes commands to MQTT topics
- **Tracker** - Tracks command state in Redis
- **ACK Handler** - Processes acknowledgments from machines

## Architecture

```
API (command request)
    ↓
Redis Pub/Sub
    ↓
Telemetry Worker
    ↓
Command Dispatcher → MQTT
    ↓
Machine
    ↓
ACK via MQTT → ACK Handler → Redis (update state)
```

## Command Dispatcher

```go
dispatcher := command.NewDispatcher(command.Config{
    MQTTClient:  mqttClient,
    RedisClient: redisClient,
    Timeout:     30 * time.Second,
}, log)
```

### Dispatch Command

```go
cmd := Command{
    ID:         uuid.New(),
    TenantID:   tenantID,
    MachineID:  machineID,
    Type:       "start_job",
    Parameters: map[string]interface{}{
        "task_id":   taskID,
        "gcode_url": "https://...",
    },
    Timestamp: time.Now(),
}

if err := dispatcher.Dispatch(ctx, cmd); err != nil {
    log.WithError(err).Error("Command dispatch failed")
}
```

## Command Types

| Command | Parameters | Description |
|---------|------------|-------------|
| `start_job` | task_id, gcode_url | Start manufacturing job |
| `pause_job` | - | Pause current job |
| `resume_job` | - | Resume paused job |
| `stop_job` | - | Stop and cancel job |
| `home` | - | Home machine axes |
| `calibrate` | type | Run calibration |

## Command Tracking

Commands are tracked in Redis with TTL:

```go
// Key format
cmd:{command_id}

// Value
{
  "id": "uuid",
  "tenant_id": "uuid",
  "machine_id": "uuid",
  "type": "start_job",
  "status": "pending|received|completed|failed",
  "dispatched_at": "2024-01-15T10:30:00Z",
  "acked_at": "2024-01-15T10:30:05Z",
  "error": "optional error message"
}
```

### Command States

```
pending → received → completed
                  ↘ failed
          ↘ timeout
```

## ACK Handler

Processes acknowledgments from machines:

```go
func (h *Handler) OnAck(client mqtt.Client, msg mqtt.Message) {
    var ack AckPayload
    if err := json.Unmarshal(msg.Payload(), &ack); err != nil {
        return
    }

    // Update command state in Redis
    if err := h.tracker.UpdateStatus(ctx, ack.CommandID, ack.Status, ack.Message); err != nil {
        h.log.WithError(err).Warn("Failed to update command status")
    }

    // Publish event for API notification
    h.publisher.PublishCommandAck(ctx, ack)
}
```

### ACK Payload

```json
{
  "command_id": "uuid",
  "status": "received|completed|failed",
  "message": "optional message or error",
  "timestamp": "2024-01-15T10:30:05Z"
}
```

## Timeout Handling

Commands that don't receive ACK within timeout:

```go
func (t *Tracker) CheckTimeouts(ctx context.Context) error {
    // Get pending commands older than timeout
    expired, err := t.getExpired(ctx)
    if err != nil {
        return err
    }

    for _, cmd := range expired {
        t.UpdateStatus(ctx, cmd.ID, "timeout", "No acknowledgment received")
        t.publisher.PublishCommandTimeout(ctx, cmd)
    }

    return nil
}
```

## Files

| File | Description |
|------|-------------|
| `dispatcher.go` | Command dispatch to MQTT |
| `tracker.go` | Redis command tracking |
| `ack.go` | Acknowledgment processing |
| `types.go` | Command and ACK types |
