# PravaraMES

Cloud-native Manufacturing Execution System (MES) for the MADFAM ecosystem.

## Overview

PravaraMES is a unified, event-driven platform optimized for phygital (physical+digital) manufacturing workflows. It replaces fragmented, vendor-locked legacy MES systems with a modern, cloud-native architecture.

### Key Features

- **Unified Namespace (UNS)** - Event-driven architecture replacing ISA-95 hierarchy
- **Multi-Tenant** - PostgreSQL Row-Level Security for secure data isolation
- **Real-Time Telemetry** - MQTT-based machine data ingestion via EMQX
- **Kanban Scheduling** - Visual drag-and-drop work-in-progress tracking
- **Janua SSO** - OAuth 2.0/OIDC authentication with RS256 JWT

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                           PravaraMES                                 │
├─────────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌───────────────────┐            │
│  │ pravara-api │  │ pravara-ui  │  │ telemetry-worker  │            │
│  │  (Go/Gin)   │  │ (Next.js)   │  │    (Go/MQTT)      │            │
│  │  :4500      │  │  :4501      │  │    :4502          │            │
│  └──────┬──────┘  └──────┬──────┘  └─────────┬─────────┘            │
│         │                │                    │                      │
│         └────────────────┼────────────────────┘                      │
│                          │                                           │
│  ┌───────────────────────┴───────────────────────────────────────┐  │
│  │                PostgreSQL + Redis + EMQX                       │  │
│  └────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
```

### Tech Stack

| Layer | Technology |
|-------|------------|
| **Backend API** | Go 1.24 + Gin |
| **Frontend** | Next.js 15 + React 19 + Radix UI + Tailwind CSS |
| **Database** | PostgreSQL 16 with Row-Level Security |
| **Cache** | Redis 7 |
| **Auth** | Janua SSO (OIDC, RS256 JWT) |
| **IIoT Broker** | EMQX (MQTT 5.0) |
| **Storage** | Cloudflare R2 (S3-compatible) |
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
│   │   └── lib/                  # Utilities & API client
│   │
│   └── telemetry-worker/         # MQTT processor
│       ├── cmd/worker/           # Entry point
│       └── internal/
│           ├── config/           # Worker configuration
│           ├── db/               # Database operations
│           └── mqtt/             # MQTT handler & processing
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
│       │   └── emqx.yaml
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

### Telemetry API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/telemetry` | List telemetry with filters |
| GET | `/v1/telemetry/aggregated` | Get aggregated metrics |
| GET | `/v1/telemetry/latest` | Get latest metric value |
| POST | `/v1/telemetry/batch` | Batch insert telemetry (max 1000) |

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
