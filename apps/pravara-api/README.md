# PravaraMES API

REST API server for the PravaraMES manufacturing execution system.

## Overview

The API provides endpoints for:
- **Orders** - Manufacturing order lifecycle management
- **Tasks** - Kanban board operations and task assignment
- **Machines** - Machine registration, control, and telemetry
- **Quality** - Certificates, inspections, and batch lot tracking
- **Billing** - Usage tracking, tenant billing, and Dhanam invoice webhooks
- **Webhooks** - Inbound integrations (Cotiza orders, Dhanam invoices, Tezca law changes — all with HMAC-SHA256 verification)
- **Realtime** - WebSocket token generation for live updates
- **Analytics** - OEE computation and SPC control charts
- **Maintenance** - CMMS scheduling and work orders
- **Products** - Product catalog and bill of materials
- **Genealogy** - Product traceability and digital birth certificates
- **Work Instructions** - Step-by-step production procedures
- **Inventory** - Stock tracking and ForgeSight integration
- **Yantra4D Import** - Import parametric hyperobjects as products with BOM and work instructions
- **Tezca Integration** - Mexican law search, article lookup, and real-time compliance webhooks

## Quick Start

```bash
# Start with Docker Compose (recommended)
cd infra && docker-compose up -d

# Or run locally
cd apps/pravara-api
go run ./cmd/api

# API available at http://localhost:4500
```

## Configuration

Environment variables (set in `.env` or container):

| Variable | Description | Default |
|----------|-------------|---------|
| `APP_ENV` | Environment (development/production) | development |
| `APP_LOG_LEVEL` | Log level (debug/info/warn/error) | info |
| `SERVER_PORT` | HTTP server port | 4500 |
| `DATABASE_URL` | PostgreSQL connection string | required |
| `REDIS_URL` | Redis URL for pub/sub and caching | optional |
| `OIDC_ISSUER_URL` | OIDC provider URL | required |
| `CENTRIFUGO_TOKEN_SECRET` | JWT secret for WebSocket tokens | required |

## API Documentation

- **OpenAPI Spec**: `/docs/swagger.yaml`
- **Swagger UI**: Available when running with dev tools

## Directory Structure

```
apps/pravara-api/
├── cmd/api/          # Application entry point
├── internal/
│   ├── api/          # HTTP handlers and routing
│   ├── billing/      # Usage tracking and billing
│   ├── config/       # Configuration loading
│   ├── db/           # Database connection and repositories
│   ├── middleware/   # Auth, rate limiting, metrics
│   ├── observability/# Prometheus metrics
│   ├── pubsub/       # Redis pub/sub for real-time events
│   └── services/     # Business logic, automation, and Yantra4D mapping
└── tests/            # Integration tests
```

## Key Patterns

### Multi-Tenant Architecture
All data is isolated by `tenant_id` using PostgreSQL Row-Level Security (RLS). The tenant is extracted from the JWT token and set in the database connection.

### Repository Pattern
Database access is abstracted through repository interfaces in `internal/db/repositories/`. Each entity has its own repository with CRUD operations and filtering.

### Event-Driven Updates
State changes publish events to Redis channels. The UI subscribes via Centrifugo WebSocket for real-time updates without polling.

### Automation Service
When tasks move to `in_progress` with an assigned machine, the automation service dispatches commands via MQTT.

## Development

```bash
# Run tests
go test ./...

# Run with hot reload
air

# Generate OpenAPI spec
swag init -g cmd/api/main.go -o ../../docs --outputTypes yaml
```

## Health Endpoints

- `GET /health` - Comprehensive health check
- `GET /health/live` - Kubernetes liveness probe
- `GET /health/ready` - Kubernetes readiness probe

## Metrics

Prometheus metrics available at `/metrics`:
- HTTP request duration and count (with `tenant_id` label)
- Database connection pool stats
- Real-time event publishing stats
- Usage tracking metrics
