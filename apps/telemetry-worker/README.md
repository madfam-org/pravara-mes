# PravaraMES Telemetry Worker

MQTT message processor for machine telemetry and command dispatch.

## Overview

The telemetry worker:
- **Subscribes** to MQTT topics for machine telemetry data
- **Batches** incoming data for efficient database writes
- **Dispatches** commands to machines via MQTT
- **Tracks** command acknowledgments and timeouts
- **Exposes** Prometheus metrics for monitoring

## Quick Start

```bash
# Start with Docker Compose (recommended)
cd infra && docker-compose up -d telemetry-worker

# Or run locally
cd apps/telemetry-worker
go run ./cmd/worker

# Worker connects to MQTT broker at startup
```

## Configuration

Environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `MQTT_BROKER_URL` | MQTT broker URL | tcp://localhost:1883 |
| `MQTT_CLIENT_ID` | MQTT client identifier | telemetry-worker |
| `MQTT_USERNAME` | MQTT authentication username | optional |
| `MQTT_PASSWORD` | MQTT authentication password | optional |
| `DATABASE_URL` | PostgreSQL connection string | required |
| `REDIS_URL` | Redis URL for command tracking | required |
| `METRICS_PORT` | Prometheus metrics port | 4502 |
| `BATCH_SIZE` | Telemetry batch size | 100 |
| `BATCH_TIMEOUT` | Batch flush timeout | 5s |

## MQTT Topics

### Telemetry (Subscribe)
```
pravara/{tenant_id}/machines/{machine_id}/telemetry
```
Payload:
```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "metric_type": "temperature",
  "value": 42.5,
  "unit": "celsius",
  "metadata": {}
}
```

### Commands (Publish)
```
pravara/{tenant_id}/machines/{machine_id}/commands
```
Payload:
```json
{
  "command_id": "uuid",
  "command": "start_job",
  "parameters": {
    "task_id": "uuid",
    "gcode_url": "https://..."
  },
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Acknowledgments (Subscribe)
```
pravara/{tenant_id}/machines/{machine_id}/ack
```
Payload:
```json
{
  "command_id": "uuid",
  "status": "received|completed|failed",
  "message": "optional error message"
}
```

## Directory Structure

```
apps/telemetry-worker/
├── cmd/worker/       # Application entry point
├── internal/
│   ├── command/      # Command dispatch and tracking
│   ├── db/           # Database connection and telemetry storage
│   └── mqtt/         # MQTT client and message handling
└── tests/            # Integration tests
```

## Key Patterns

### Batched Writes
Telemetry data is batched in memory and flushed to the database periodically or when the batch reaches the configured size.

### Command Tracking
Commands are tracked in Redis with TTL. The worker listens for acknowledgments and updates command status.

### Graceful Shutdown
The worker handles SIGINT/SIGTERM for clean shutdown, flushing pending batches and closing connections.

## Development

```bash
# Run tests
go test ./...

# Run with environment file
source .env && go run ./cmd/worker

# Run with Docker
docker build -t telemetry-worker .
docker run --env-file .env telemetry-worker
```

## Metrics

Prometheus metrics available at `/metrics`:

| Metric | Type | Description |
|--------|------|-------------|
| `telemetry_messages_received_total` | Counter | Total messages received |
| `telemetry_batches_written_total` | Counter | Total batches written to DB |
| `telemetry_batch_write_duration_seconds` | Histogram | Batch write latency |
| `commands_dispatched_total` | Counter | Total commands sent |
| `commands_acknowledged_total` | Counter | Commands acknowledged |
| `commands_timeout_total` | Counter | Commands that timed out |

## Health

- `GET /health` - Worker health status
- `GET /metrics` - Prometheus metrics
