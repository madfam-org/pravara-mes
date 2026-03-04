# PravaraMES

Cloud-native Manufacturing Execution System (MES) for the MADFAM ecosystem.

## Overview

PravaraMES is a unified, event-driven platform optimized for phygital (physical+digital) manufacturing workflows. It replaces fragmented, vendor-locked legacy MES systems with a modern, cloud-native architecture.

### 🎯 Digital Twin Completion: 97%

The system now features complete multi-tool 3D printing support with real-time physics simulation, printer connection management, and comprehensive visualization for FDM, laser, CNC, and pen plotting operations.

### Key Features

- **Universal Machine Connectivity** - Support for 95%+ of digital fabrication machines
- **Protocol Adapters** - GRBL, Marlin, OctoPrint, Ruida, LinuxCNC, Industrial CNCs
- **Agent-Based Task Management** - Intelligent task routing with human-in-the-loop control
- **Machine Discovery** - Automatic detection via mDNS, USB, and network scanning
- **Unified Namespace (UNS)** - Event-driven architecture replacing ISA-95 hierarchy
- **Multi-Tenant** - PostgreSQL Row-Level Security for secure data isolation
- **Real-Time Telemetry** - MQTT-based machine data ingestion via EMQX
- **Real-Time Updates** - WebSocket-based live UI updates via Centrifugo
- **Kanban Scheduling** - Visual drag-and-drop work-in-progress tracking
- **Quality Management** - COC/COA certificates, inspections, batch traceability
- **Usage-Based Billing** - Per-tenant resource tracking and billing metrics
- **Janua SSO** - OAuth 2.0/OIDC authentication with RS256 JWT
- **Zero-Trust Security** - Network policies, RBAC, pod security standards, rate limiting
- **Digital Twin** - Real-time 3D factory floor visualization with physics simulation
- **Video Streaming** - WebRTC-based live camera feeds with recording capabilities
- **AI/ML Orchestration** - Predictive maintenance, anomaly detection, quality prediction
- **Process Optimization** - AI-driven parameter optimization for efficiency gains
- **Snapmaker/Luban Integration** - Native support for Snapmaker 3D printers with Luban slicing
- **OctoPrint Connectivity** - Full integration with OctoPrint-managed 3D printers
- **FullControl GCODE** - Advanced G-code visualization with material physics simulation
- **Yantra4D Integration** - Import parametric hyperobjects, auto-create products/BOM/work instructions
- **Dynamic Machine Registration** - Runtime-registerable machine definitions with DB persistence
- **Tezca Legal Intelligence** - Mexican law compliance via Tezca API with webhook-driven updates

## Architecture

```
┌───────────────────────────────────────────────────────────────────────────┐
│                              PravaraMES                                    │
├───────────────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  │
│  │ pravara-api │  │ pravara-ui  │  │  telemetry- │  │ pravara-gateway │  │
│  │  (Go/Gin)   │  │ (Next.js)   │  │   worker    │  │  (Centrifugo)   │  │
│  │  :4500      │  │  :4501      │  │  (Go/MQTT)  │  │     :8000       │  │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └───────┬─────────┘  │
│         │                │                │                  │            │
│         │                │ WebSocket      │                  │            │
│         │                └────────────────┼──────────────────┘            │
│         │                                 │                               │
│         └─────────────────────────────────┘                               │
│                          │                                                │
│  ┌───────────────────────┴────────────────────────────────────────────┐  │
│  │            PostgreSQL + Redis (Pub/Sub) + EMQX (MQTT)               │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────────────────────┘
```

### Tech Stack

| Layer | Technology |
|-------|------------|
| **Backend API** | Go 1.24 + Gin |
| **Frontend** | Next.js 15 + React 19 + Radix UI + Tailwind CSS |
| **3D Visualization** | Three.js + React Three Fiber |
| **Video Streaming** | WebRTC + FFmpeg |
| **ML/AI** | Python + FastAPI + TensorFlow + Scikit-learn |
| **Database** | PostgreSQL 16 with Row-Level Security |
| **Cache/Pub-Sub** | Redis 7 |
| **Real-Time Gateway** | Centrifugo v5 (WebSocket) |
| **Auth** | Janua SSO (OIDC, RS256 JWT) |
| **Parametric Design** | Yantra4D (hyperobject import) |
| **IIoT Broker** | EMQX (MQTT 5.0) |
| **Storage** | Cloudflare R2 (S3-compatible) |
| **Metrics** | Prometheus + AlertManager |
| **Deploy** | enclii GitOps + Kubernetes |

## Quick Start

### Prerequisites

- Go 1.24+
- Node.js 20+
- pnpm 9+
- Docker & Docker Compose

### Development Setup

```bash
# Clone the repository
git clone https://github.com/madfam-org/pravara-mes.git
cd pravara-mes

# Copy environment configuration
cp .env.example .env

# Start infrastructure (PostgreSQL, Redis, EMQX)
make docker-up

# Run database migrations
make migrate

# Start all services in development mode
make dev
```

### Environment Variables

```bash
# API Configuration
PRAVARA_ENVIRONMENT=development
PRAVARA_LOG_LEVEL=info
PRAVARA_SERVER_PORT=4500

# Database
PRAVARA_DATABASE_HOST=localhost
PRAVARA_DATABASE_PORT=5432
PRAVARA_DATABASE_USER=pravara
PRAVARA_DATABASE_PASSWORD=pravara_secret
PRAVARA_DATABASE_NAME=pravara_mes
PRAVARA_DATABASE_SSLMODE=disable

# Auth (Janua SSO)
PRAVARA_AUTH_ISSUER=https://auth.janua.io/realms/madfam
PRAVARA_AUTH_AUDIENCE=pravara-api

# MQTT (EMQX)
PRAVARA_MQTT_BROKER=localhost
PRAVARA_MQTT_PORT=1883
PRAVARA_MQTT_USERNAME=pravara
PRAVARA_MQTT_PASSWORD=mqtt_secret

# Redis
PRAVARA_REDIS_HOST=localhost
PRAVARA_REDIS_PORT=6379
```

## Services

| Service | Port | Description |
|---------|------|-------------|
| pravara-api | 4500 | REST API (Go/Gin) |
| pravara-ui | 4501 | Web Dashboard (Next.js) |
| telemetry-worker | 4502 | MQTT Telemetry Processor |
| pravara-gateway | 8000 | Real-Time WebSocket Gateway (Centrifugo) |
| visualization-engine | 4205 | 3D Factory Visualization (Go) |
| video-streaming | 4206 | WebRTC Video Streaming (Go) |
| ml-orchestrator | 4207 | ML/AI Pipeline (Python/FastAPI) |
| luban-bridge | 4507 | Snapmaker/Luban Integration (Node.js) |
| octoprint-connector | 4508 | OctoPrint Manager (Python/FastAPI) |

## Project Structure

```
pravara-mes/
├── apps/
│   ├── pravara-api/              # Go API server
│   │   ├── cmd/api/              # Entry point
│   │   └── internal/
│   │       ├── api/              # HTTP handlers
│   │       ├── auth/             # Janua OIDC integration
│   │       ├── config/           # Configuration
│   │       ├── db/               # Database layer
│   │       │   ├── migrations/   # SQL migrations
│   │       │   └── repositories/ # Data access
│   │       ├── middleware/       # HTTP middleware
│   │       ├── pubsub/           # Redis event publishing
│   │       └── services/         # Business logic
│   │
│   ├── pravara-ui/               # Next.js dashboard
│   │   ├── app/                  # App router pages
│   │   │   ├── (protected)/      # Auth-required pages
│   │   │   │   ├── kanban/       # Kanban board
│   │   │   │   ├── orders/       # Order management
│   │   │   │   └── machines/     # Machine monitoring
│   │   │   └── login/            # Authentication
│   │   ├── components/           # React components
│   │   │   ├── kanban/           # Kanban board components
│   │   │   └── ui/               # Radix UI primitives
│   │   ├── lib/                  # Utilities & API client
│   │   │   └── realtime/         # WebSocket client & types
│   │   ├── hooks/                # React hooks (incl. realtime)
│   │   └── stores/               # Zustand state stores
│   │
│   ├── telemetry-worker/         # MQTT processor
│   │   ├── cmd/worker/           # Entry point
│   │   └── internal/
│   │       ├── config/           # Worker configuration
│   │       ├── db/               # Database operations
│   │       └── mqtt/             # MQTT handler & event publishing
│   │
│   ├── visualization-engine/     # 3D visualization & physics
│   │   ├── cmd/server/           # HTTP/WebSocket server
│   │   └── internal/
│   │       ├── models/           # 3D model management
│   │       ├── physics/          # Physics simulation engine
│   │       ├── yantra4d/         # Yantra4D API client
│   │       └── websocket/        # Real-time updates
│   │
│   ├── video-streaming/          # WebRTC video service
│   │   ├── cmd/server/           # WebRTC signaling server
│   │   └── internal/
│   │       ├── camera/           # Camera discovery & management
│   │       ├── rtc/              # WebRTC peer connections
│   │       └── recording/        # Video recording service
│   │
│   ├── ml-orchestrator/          # AI/ML service (Python)
│   │   ├── models/               # ML models
│   │   │   ├── predictive_maintenance.py
│   │   │   ├── anomaly_detection.py
│   │   │   ├── quality_prediction.py
│   │   │   └── process_optimizer.py
│   │   ├── services/             # ML services
│   │   │   ├── training_service.py
│   │   │   ├── inference_service.py
│   │   │   └── telemetry_service.py
│   │   └── main.py               # FastAPI application
│   │
│   └── pravara-gateway/          # Real-time WebSocket gateway
│       ├── config.json           # Centrifugo configuration
│       └── Dockerfile
│
├── packages/
│   ├── sdk-go/                   # Shared Go types
│   │   └── pkg/types/            # Domain models
│   └── ui-components/            # Shared React components
│
├── infra/
│   └── k8s/                      # Kubernetes manifests
│       ├── base/                 # Kustomize base
│       │   ├── namespace.yaml
│       │   ├── configmap.yaml
│       │   ├── secrets.yaml
│       │   ├── postgres.yaml
│       │   ├── redis.yaml
│       │   ├── emqx.yaml
│       │   ├── centrifugo.yaml   # WebSocket gateway
│       │   └── ingress.yaml      # External routing
│       └── production/           # Production overlays
│
├── scripts/                      # Automation scripts
├── docker-compose.yml            # Local development stack
├── go.work                       # Go workspace
├── Makefile                      # Build commands
└── services.json                 # CI/CD service registry
```

## API Documentation

### Health Endpoints (No Auth Required)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Full health check (DB, Redis, MQTT) |
| GET | `/health/live` | Kubernetes liveness probe |
| GET | `/health/ready` | Kubernetes readiness probe |

### Orders API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/orders` | List orders with pagination |
| POST | `/v1/orders` | Create new order |
| GET | `/v1/orders/:id` | Get order by ID |
| PATCH | `/v1/orders/:id` | Update order |
| DELETE | `/v1/orders/:id` | Delete order |
| GET | `/v1/orders/:id/items` | List order items |
| POST | `/v1/orders/:id/items` | Add item to order |

### Webhooks API

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/v1/webhooks/cotiza` | Cotiza Studio order webhook |
| POST | `/v1/webhooks/forgesight` | ForgeSight integration (planned) |

### Tasks API (Kanban)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/tasks` | List tasks |
| GET | `/v1/tasks/board` | Get Kanban board data |
| POST | `/v1/tasks` | Create task |
| GET | `/v1/tasks/:id` | Get task by ID |
| PATCH | `/v1/tasks/:id` | Update task |
| POST | `/v1/tasks/:id/move` | Move task (status/position) |
| POST | `/v1/tasks/:id/assign` | Assign task to user |
| DELETE | `/v1/tasks/:id` | Delete task |
| GET | `/v1/tasks/:id/commands` | Get command history for task |

#### Kanban-Machine Automation

When a task with an assigned machine is moved to `in_progress`, PravaraMES automatically:

1. **Validates the machine** - Checks machine status and MQTT topic configuration
2. **Dispatches `start_job` command** - Sends command via Redis → MQTT pipeline
3. **Tracks command lifecycle** - Records command in `task_commands` table
4. **Updates UI in real-time** - Publishes events via Centrifugo

When the machine completes the job (sends ACK with `job_completed: true`):

1. **Updates command status** - Marks command as `completed`
2. **Moves task automatically** - Transitions task to `quality_check` status
3. **Notifies UI** - Publishes `task.job_completed` event

**Machine Validation Rules**:

| Machine Status | Can Start Job? | Behavior |
|----------------|----------------|----------|
| `online` / `idle` | Yes | Command dispatched immediately |
| `offline` | Yes (with warning) | Command queued for when machine connects |
| `running` | Yes (with warning) | Command queued after current job |
| `error` | No | Blocked - requires maintenance |
| `maintenance` | No | Blocked - machine unavailable |

**Command Lifecycle Events**:

| Event Type | Description |
|------------|-------------|
| `task.job_started` | Command sent to machine |
| `task.job_completed` | Machine finished job successfully |
| `task.job_failed` | Machine reported job failure |
| `task.blocked` | Task blocked due to machine error |

### Machines API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/machines` | List machines |
| POST | `/v1/machines` | Register machine |
| GET | `/v1/machines/:id` | Get machine by ID |
| PATCH | `/v1/machines/:id` | Update machine |
| DELETE | `/v1/machines/:id` | Delete machine |
| GET | `/v1/machines/:id/telemetry` | Get telemetry data |
| POST | `/v1/machines/:id/heartbeat` | Update heartbeat |
| POST | `/v1/machines/:id/command` | Send command to machine |

#### Machine Commands

Send commands to digital fabrication machines (3D printers, CNC, laser cutters):

```json
POST /v1/machines/:id/command
{
  "command": "start_job",
  "parameters": {
    "file_path": "/gcode/part001.gcode"
  },
  "task_id": "optional-task-uuid",
  "order_id": "optional-order-uuid"
}
```

**Supported Commands**:

| Command | Description | Parameters |
|---------|-------------|------------|
| `start_job` | Start a job | `file_path` (optional) |
| `pause` | Pause current job | - |
| `resume` | Resume paused job | - |
| `stop` | Stop current job | - |
| `home` | Home all axes | - |
| `calibrate` | Run calibration | - |
| `emergency_stop` | Emergency stop | - |
| `preheat` | Preheat (3D printers) | `temperature`, `bed_temp` |
| `cooldown` | Cooldown (3D printers) | - |
| `load_file` | Load file to machine | `file_path` |
| `unload_file` | Unload current file | - |
| `set_origin` | Set work origin (CNC) | `x`, `y`, `z` (optional) |
| `probe` | Run probe cycle (CNC) | - |

### Real-Time API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/realtime/token` | Get Centrifugo connection token |
| POST | `/v1/realtime/auth` | Proxy auth for Centrifugo connect |
| POST | `/v1/realtime/subscribe` | Proxy auth for channel subscription |

### Telemetry API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/telemetry` | List telemetry with filters |
| GET | `/v1/telemetry/aggregated` | Get aggregated metrics |
| GET | `/v1/telemetry/latest` | Get latest metric value |
| POST | `/v1/telemetry/batch` | Batch insert telemetry (max 1000) |

### Quality Management API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/quality/certificates` | List quality certificates |
| POST | `/v1/quality/certificates` | Create certificate |
| GET | `/v1/quality/certificates/:id` | Get certificate by ID |
| PATCH | `/v1/quality/certificates/:id` | Update certificate |
| DELETE | `/v1/quality/certificates/:id` | Delete certificate |
| GET | `/v1/quality/inspections` | List inspections |
| POST | `/v1/quality/inspections` | Create inspection |
| GET | `/v1/quality/inspections/:id` | Get inspection by ID |
| PATCH | `/v1/quality/inspections/:id` | Update inspection |
| POST | `/v1/quality/inspections/:id/complete` | Complete inspection with result |
| GET | `/v1/quality/batches` | List batch lots |
| POST | `/v1/quality/batches` | Create batch lot |
| GET | `/v1/quality/batches/:id` | Get batch lot by ID |
| PATCH | `/v1/quality/batches/:id` | Update batch lot |

### Billing API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/billing/usage` | Get tenant usage summary |
| GET | `/v1/billing/usage/daily` | Get daily usage breakdown |

### 3D Printing APIs

#### Luban Bridge API (Port 4507)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/project/import` | Import Luban project file |
| POST | `/api/project/slice` | Slice STL to G-code |
| POST | `/api/machine/discover` | Discover Snapmaker machines |
| POST | `/api/machine/:id/connect` | Connect to Snapmaker printer |
| POST | `/api/machine/:id/command` | Send G-code command |
| POST | `/api/gcode/analyze` | Analyze G-code file |
| WS | `/ws?machineId=<id>` | Real-time machine telemetry |

#### OctoPrint Connector API (Port 4508)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/instances` | Add OctoPrint instance |
| GET | `/instances/:id/status` | Get printer status |
| POST | `/instances/:id/print/start` | Start print |
| POST | `/instances/:id/print/pause` | Pause print |
| WS | `/ws/:instance_id` | Real-time status updates |

## MQTT Topics (Unified Namespace)

PravaraMES uses the Unified Namespace (UNS) pattern for MQTT topics:

```
{tenant}/{site}/{area}/{line}/{machine}/{metric}

# Examples
madfam/hel/production/line-1/cnc-01/temperature
madfam/hel/production/line-1/cnc-01/spindle_speed
madfam/hel/production/line-1/cnc-01/power

# Commands
madfam/hel/production/line-1/cnc-01/cmd/start

# Events
madfam/hel/production/line-1/cnc-01/event/job_completed
```

### Telemetry Payload Schema

```json
{
  "timestamp": "2026-03-01T15:30:00.000Z",
  "machine_id": "cnc-01",
  "metric_type": "temperature",
  "value": 45.2,
  "unit": "celsius",
  "metadata": {
    "sensor_id": "S001",
    "location": "spindle"
  }
}
```

### Supported Metric Types

| Metric Type | Units | Description |
|-------------|-------|-------------|
| temperature | celsius, fahrenheit | Machine temperature |
| power | watts, kilowatts | Power consumption |
| spindle_speed | rpm | Spindle rotation speed |
| feed_rate | mm/min | Feed rate |
| vibration | g | Vibration level |
| current | amps | Electrical current |
| voltage | volts | Electrical voltage |
| pressure | psi, bar | Hydraulic/pneumatic pressure |
| humidity | percent | Ambient humidity |
| cycle_count | count | Production cycles |
| uptime | hours | Machine uptime |

## Real-Time WebSocket (Centrifugo)

PravaraMES uses Centrifugo for real-time WebSocket communication. Events are published via Redis Pub/Sub.

### Channel Namespaces

| Channel | Events | Description |
|---------|--------|-------------|
| `machines:{tenant_id}` | status_changed, heartbeat, created, updated, deleted, command_sent, command_ack, command_failed | Machine status and command updates |
| `machines:{tenant_id}:{machine_id}` | command_sent, command_ack, command_failed | Machine-specific command events |
| `tasks:{tenant_id}` | moved, assigned, created, updated, deleted, completed | Task/Kanban updates |
| `orders:{tenant_id}` | status_changed, created, updated, deleted | Order status changes |
| `telemetry:{tenant_id}` | telemetry_batch | Real-time telemetry data |
| `notifications:{tenant_id}` | alert, warning, info | System notifications |

### Event Payload Schema

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "type": "machine.status_changed",
  "tenant_id": "123e4567-e89b-12d3-a456-426614174000",
  "timestamp": "2026-03-01T15:30:00.000Z",
  "data": {
    "machine_id": "cnc-01-uuid",
    "machine_name": "CNC-01",
    "old_status": "idle",
    "new_status": "running",
    "updated_at": "2026-03-01T15:30:00.000Z"
  }
}
```

### Frontend Integration

```typescript
// Connect to real-time updates
import { useRealtimeConnection, useMachineUpdates } from '@/hooks';

function Dashboard() {
  const { isConnected } = useRealtimeConnection();

  // Auto-updates React Query cache on machine events
  useMachineUpdates({
    onStatusChange: (data) => console.log('Machine status:', data),
  });

  return <div>Connected: {isConnected ? 'Yes' : 'No'}</div>;
}
```

## Database Schema

### Multi-Tenancy with Row-Level Security

```sql
-- All tables include tenant_id for RLS
ALTER TABLE orders ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON orders
    USING (tenant_id = current_setting('app.current_tenant_id')::UUID);
```

### Core Tables

- **tenants** - Multi-tenant organizations
- **users** - User accounts (linked to Janua SSO)
- **orders** - Customer orders (Cotiza integration)
- **tasks** - Kanban tasks with position management
- **machines** - Machine registry with MQTT topics
- **telemetry** - Time-series machine telemetry

### Status Enums

**Order Status**: `received` → `confirmed` → `in_production` → `quality_check` → `ready` → `shipped` → `delivered` | `cancelled`

**Task Status**: `backlog` → `queued` → `in_progress` → `quality_check` → `completed` | `blocked`

**Machine Status**: `offline` | `online` | `idle` | `running` | `maintenance` | `error`

**Certificate Type**: `coc` (Certificate of Conformance) | `coa` (Certificate of Analysis) | `inspection` | `test_report` | `calibration`

**Inspection Status**: `pending` → `in_progress` → `completed` | `failed`

## Quality Management

PravaraMES includes comprehensive quality management capabilities for manufacturing compliance and traceability.

### Certificate Types

- **COC (Certificate of Conformance)** - Confirms products meet specifications
- **COA (Certificate of Analysis)** - Detailed test results and material composition
- **Inspection Certificate** - Results from quality inspections
- **Test Report** - Specific test procedures and outcomes
- **Calibration Certificate** - Equipment calibration records

### Inspection Workflow

1. **Create Inspection** - Define inspection parameters, type, and assigned inspector
2. **In Progress** - Inspector performs checks and records measurements
3. **Complete** - Submit results with pass/fail status and notes
4. **Certificate Generation** - Automatically generate certificates from passed inspections

### Batch Lot Traceability

Track materials and products through manufacturing:

- Unique batch/lot numbers
- Parent-child batch relationships
- Expiration date tracking
- Material specifications and quantities
- Full audit trail of batch lifecycle

## Security Features

PravaraMES implements defense-in-depth security with multiple layers:

### Zero-Trust Network Policies

- Default deny-all traffic between pods
- Explicit allowlist for inter-service communication
- Namespace isolation with ingress/egress controls
- External traffic restricted to ingress controller only

### Role-Based Access Control (RBAC)

- Service account per microservice with minimal permissions
- Read-only access by default
- Write permissions only where required
- No cross-namespace access

### Pod Security Standards

- Enforced "restricted" security policy
- No privileged containers
- Read-only root filesystems
- Non-root user execution
- Drop all Linux capabilities by default

### Rate Limiting

- **Per-IP Rate Limiting**: 100 requests/minute per IP address
- **Per-Tenant Rate Limiting**: 1000 requests/minute per tenant
- Prevents abuse and ensures fair resource allocation
- Configurable limits via environment variables

## Observability

### Prometheus Metrics

All services expose Prometheus metrics at `/metrics` endpoint:

**API Metrics** (`pravara-api`):
- `pravara_http_requests_total` - HTTP request counter (method, path, status)
- `pravara_http_request_duration_seconds` - Request duration histogram
- `pravara_db_connections_active` - Active database connections
- `pravara_redis_operations_total` - Redis operation counter

**Telemetry Worker Metrics** (`telemetry-worker`):
- `pravara_mqtt_messages_received_total` - MQTT message counter
- `pravara_mqtt_message_processing_duration_seconds` - Processing time
- `pravara_telemetry_points_processed_total` - Telemetry data points

**Billing Metrics**:
- `pravara_usage_api_requests_total` - API usage by tenant
- `pravara_usage_storage_bytes` - Storage usage by tenant
- `pravara_usage_compute_seconds` - Compute time by tenant

### AlertManager Integration

Critical alerts configured for:
- Service health check failures
- Database connection pool exhaustion
- High error rates (>5% of requests)
- MQTT broker disconnection
- Resource quota exceeded per tenant

## Testing

```bash
# Run all tests
make test

# Run API tests
cd apps/pravara-api && go test ./...

# Run telemetry worker tests
cd apps/telemetry-worker && go test ./...

# Run SDK tests
cd packages/sdk-go && go test ./...

# Run with coverage
make test-coverage
```

## Deployment

PravaraMES is deployed via [enclii](https://github.com/madfam-org/enclii) GitOps platform.

### Kubernetes Deployment

```bash
# Apply base manifests
kubectl apply -k infra/k8s/base

# Apply production overlays
kubectl apply -k infra/k8s/production
```

### enclii Deployment

```bash
# Deploy API to production
enclii deploy --service pravara-api --env production

# Deploy UI to production
enclii deploy --service pravara-ui --env production

# Deploy telemetry worker
enclii deploy --service telemetry-worker --env production
```

## Development Commands

```bash
# Start local infrastructure
make docker-up

# Stop local infrastructure
make docker-down

# Run database migrations
make migrate

# Sync Go workspace
make workspace-sync

# Lint code
make lint

# Format code
make fmt

# Build all services
make build

# Run all services in dev mode
make dev
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

Copyright (c) 2026 MADFAM. All rights reserved.
