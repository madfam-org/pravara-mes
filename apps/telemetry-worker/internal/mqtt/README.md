# MQTT Handler

MQTT client and message processing for machine telemetry.

## Overview

This package provides:
- **Client** - MQTT connection and subscription management
- **Handler** - Message parsing and routing
- **Batching** - Efficient batch writes to database

## Client

```go
client, err := mqtt.NewClient(mqtt.Config{
    BrokerURL:  cfg.MQTT.BrokerURL,
    ClientID:   cfg.MQTT.ClientID,
    Username:   cfg.MQTT.Username,
    Password:   cfg.MQTT.Password,
})
if err != nil {
    log.Fatal(err)
}
defer client.Disconnect()
```

### Subscriptions

```go
// Subscribe to telemetry topics
client.Subscribe("pravara/+/machines/+/telemetry", handler.OnTelemetry)

// Subscribe to command acknowledgments
client.Subscribe("pravara/+/machines/+/ack", handler.OnAck)
```

## Message Handler

```go
type Handler struct {
    repo         *db.TelemetryRepository
    batcher      *Batcher
    commandTrack *command.Tracker
    log          *logrus.Logger
}

func (h *Handler) OnTelemetry(client mqtt.Client, msg mqtt.Message) {
    // 1. Parse topic for tenant and machine ID
    tenantID, machineID := parseTopic(msg.Topic())

    // 2. Parse payload
    var payload TelemetryPayload
    if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
        h.log.WithError(err).Warn("Invalid telemetry payload")
        return
    }

    // 3. Add to batch
    h.batcher.Add(Telemetry{
        TenantID:   tenantID,
        MachineID:  machineID,
        Timestamp:  payload.Timestamp,
        MetricType: payload.MetricType,
        Value:      payload.Value,
        Unit:       payload.Unit,
        Metadata:   payload.Metadata,
    })
}
```

## Topic Structure

```
pravara/{tenant_id}/machines/{machine_id}/telemetry
pravara/{tenant_id}/machines/{machine_id}/commands
pravara/{tenant_id}/machines/{machine_id}/ack
```

## Telemetry Payload

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "metric_type": "temperature",
  "value": 42.5,
  "unit": "celsius",
  "metadata": {
    "sensor_id": "temp_1",
    "location": "extruder"
  }
}
```

## Batching

Telemetry is batched for efficient database writes:

```go
type Batcher struct {
    records     []Telemetry
    mu          sync.Mutex
    size        int
    timeout     time.Duration
    flushChan   chan struct{}
    repo        *db.TelemetryRepository
}

func (b *Batcher) Add(t Telemetry) {
    b.mu.Lock()
    b.records = append(b.records, t)
    shouldFlush := len(b.records) >= b.size
    b.mu.Unlock()

    if shouldFlush {
        b.flushChan <- struct{}{}
    }
}

func (b *Batcher) Run(ctx context.Context) {
    ticker := time.NewTicker(b.timeout)
    for {
        select {
        case <-ctx.Done():
            b.flush(ctx)
            return
        case <-ticker.C:
            b.flush(ctx)
        case <-b.flushChan:
            b.flush(ctx)
        }
    }
}
```

## Files

| File | Description |
|------|-------------|
| `client.go` | MQTT connection management |
| `handler.go` | Message processing |
| `batcher.go` | Telemetry batching |
| `topics.go` | Topic parsing utilities |
