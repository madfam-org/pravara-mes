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

Command channel (see [`internal/command/README.md`](internal/command/README.md)):

| Variable | Description | Default |
|----------|-------------|---------|
| `PRAVARA_COMMAND_ENABLED` | Consume and dispatch machine commands | `true` |
| `PRAVARA_COMMAND_STREAM_KEY` | Redis stream the API appends commands to | `pravara:commands` |
| `PRAVARA_COMMAND_CONSUMER_GROUP` | Stream consumer group shared by replicas | `telemetry-worker` |
| `PRAVARA_COMMAND_CONSUMER_NAME` | Consumer name of this replica | host name |
| `PRAVARA_COMMAND_MAX_ATTEMPTS` | MQTT publish attempts before a command fails | `3` |
| `PRAVARA_COMMAND_RETRY_IDLE_SECONDS` | Idle time before an unacknowledged entry is reclaimed | `30` |
| `PRAVARA_COMMAND_ACK_TIMEOUT_SECONDS` | Deadline for a machine to acknowledge a sent command | `120` |
| `PRAVARA_COMMAND_DISPATCH_TIMEOUT_SECONDS` | Deadline for a queued command to be sent | `600` |
| `PRAVARA_COMMAND_SWEEP_INTERVAL_SECONDS` | Deadline sweep interval | `15` |

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

### Command Channel
Commands arrive on a Redis stream (consumer group, at-least-once) and every
outcome is recorded in the `task_commands` ledger: sent, acknowledged,
completed, failed (bounded retries) or timeout. Acks only apply to commands
issued to the machine whose topic they arrive on. Command-channel topics
(`…/cmd`, `…/ack`) are not ingested as telemetry and do not refresh liveness.

### Graceful Shutdown
The worker handles SIGINT/SIGTERM for clean shutdown, flushing pending batches and closing connections.

## Development

```bash
# Run tests
go test ./...

# Also run the PostgreSQL / Redis tests against throwaway instances
PRAVARA_TEST_DATABASE_URL=postgres://... PRAVARA_TEST_REDIS_URL=redis://... go test ./...

# Run with environment file
source .env && go run ./cmd/worker

# Run with Docker
docker build -t telemetry-worker .
docker run --env-file .env telemetry-worker
```

## Metrics

Prometheus metrics available at `/metrics`. Request-scoped metrics include a `tenant_id` label for per-tenant observability:

| Metric | Type | Description |
|--------|------|-------------|
| `telemetry_messages_received_total` | Counter | Total messages received |
| `telemetry_batches_written_total` | Counter | Total batches written to DB |
| `telemetry_batch_write_duration_seconds` | Histogram | Batch write latency |
| `pravara_command_dispatch_total{outcome}` | Counter | Stream entries by outcome: published, retry, failed, duplicate, rejected |
| `pravara_command_stream_reclaimed_total` | Counter | Entries reclaimed for redelivery |
| `pravara_command_acks_total{disposition}` | Counter | Acks: applied, unknown_machine, unknown_command, machine_mismatch, already_final, invalid |
| `pravara_command_timeouts_total{stage}` | Counter | Commands timed out while pending or sent |
| `pravara_command_completion_hook_errors_total` | Counter | Job completion hook errors |
| `pravara_telemetry_mqtt_control_topics_skipped_total{channel}` | Counter | `cmd`/`ack` messages excluded from telemetry |

## Health

- `GET /health` - Worker health status
- `GET /metrics` - Prometheus metrics
