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
- **Event Outbox** - Persistent event history with queryable API and real-time SSE streaming
- **API Key Authentication** - Dual auth support (API keys + JWT) with key management for external consumers
- **Webhook Subscriptions** - Outbound webhook delivery with HMAC-SHA256 signatures and exponential backoff
- **Server-Sent Events (SSE)** - Real-time event streaming over HTTP for lightweight consumers
- **CRM Feed** - Order progress tracking and timeline views for CRM integration
- **Social Media Feed** - Production milestones, statistics, and curated highlights for social content
- **Public Status Page** - System health monitoring with 90-day uptime history (no auth required)
- **CORS Support** - Configurable cross-origin resource sharing for external consumers
- **Health Monitoring** - Background health recorder with per-component checks and incident tracking
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
| `PRAVARA_CENTRIFUGO_API_URL` | Centrifugo API URL (used for health checks) | `http://pravara-gateway:9000` |
| `PRAVARA_WEBHOOKS_DISPATCH_INTERVAL` | Seconds between webhook dispatch cycles | 5 |
| `PRAVARA_WEBHOOKS_MAX_RETRIES` | Maximum delivery retry attempts per webhook | 5 |
| `PRAVARA_WEBHOOKS_RETENTION_DAYS` | Days to retain outbox events and delivery logs | 30 |
| `PRAVARA_SSE_MAX_CONNECTIONS` | Maximum concurrent SSE connections | 1000 |
| `PRAVARA_SSE_KEEPALIVE_SECONDS` | Interval between SSE keepalive pings | 30 |
| `PRAVARA_CORS_ALLOWED_ORIGINS` | Comma-separated allowed CORS origins | `https://mes-app.madfam.io,https://mes-admin.madfam.io` |
| `PRAVARA_CORS_STATUS_PUBLIC` | Allow unauthenticated access to `/status` endpoints | true |

## API Documentation

- **OpenAPI Spec**: `/docs/swagger.yaml`
- **Swagger UI**: Available when running with dev tools

## Directory Structure

```
apps/pravara-api/
├── cmd/api/              # Application entry point
├── internal/
│   ├── api/              # HTTP handlers and routing
│   │   ├── apikey_handlers.go            # API key management (admin)
│   │   ├── event_history_handlers.go     # Event outbox query API
│   │   ├── feed_handlers.go              # CRM and social media feeds
│   │   ├── sse_handlers.go               # Server-Sent Events streaming
│   │   ├── status_handlers.go            # Public status page
│   │   └── webhook_subscription_handlers.go  # Outbound webhook subscriptions
│   ├── auth/             # OIDC verification
│   ├── billing/          # Usage tracking and billing
│   ├── config/           # Configuration loading
│   ├── db/               # Database connection and repositories
│   │   └── repositories/
│   │       ├── apikey_repository.go      # API key storage
│   │       ├── feed_repository.go        # CRM/social feed queries
│   │       ├── outbox_repository.go      # Event outbox persistence
│   │       └── webhook_repository.go     # Webhook subscriptions and deliveries
│   ├── integrations/     # External service clients (Tezca, etc.)
│   ├── middleware/        # Auth, CORS, rate limiting, metrics
│   │   ├── apikey.go                     # API key + JWT dual auth
│   │   ├── cors.go                       # CORS configuration
│   │   └── scopes.go                     # Scope-based authorization
│   ├── observability/    # Prometheus metrics
│   ├── pubsub/           # Redis pub/sub for real-time events
│   └── services/         # Business logic and background workers
│       ├── health_recorder.go            # Background health monitoring
│       └── webhook_dispatcher.go         # Outbound webhook delivery engine
└── tests/                # Integration tests
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

### Event Outbox
All domain events (order updates, task transitions, machine telemetry, etc.) are persisted to an `event_outbox` table before being published to Redis. This provides a durable event log that can be queried via the `/v1/events` API and streamed in real time via SSE at `/v1/events/stream`.

### Dual Authentication
The API supports two authentication methods that can be used interchangeably:
- **JWT tokens** from the OIDC provider (Janua SSO) for user-facing clients
- **API keys** with SHA-256 hashed storage for machine-to-machine integrations

API keys are passed via the `X-API-Key` header. The middleware tries API key auth first and falls back to JWT validation. Both methods extract the same tenant context for Row-Level Security.

### Outbound Webhooks
Tenants can register webhook subscriptions for specific event types. The webhook dispatcher runs as a background goroutine that:
1. Polls the outbox for undelivered events matching active subscriptions
2. Delivers payloads with HMAC-SHA256 signatures in the `X-Signature-256` header
3. Retries failed deliveries with exponential backoff (configurable max retries)
4. Records delivery attempts with status codes and response bodies for debugging

### Health Monitoring
A background health recorder periodically checks the status of all system components (PostgreSQL, Redis, Centrifugo) and writes snapshots to the `health_snapshots` table. The public `/status` endpoint aggregates these snapshots into a composite health score, and `/status/history` provides 90 days of uptime data.

## New API Endpoints

### Public Endpoints (no authentication required)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/status` | Composite system health status |
| `GET` | `/status/history` | 90-day uptime history |

### API Key Management (admin only)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/api-keys` | Generate a new API key |
| `GET` | `/v1/api-keys` | List all API keys for the tenant |
| `DELETE` | `/v1/api-keys/:id` | Revoke an API key |

### Event History (requires `read:events` scope)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/events` | Paginated event history (supports `?type=`, `?since=`, `?limit=`) |
| `GET` | `/v1/events/:id` | Retrieve a single event by ID |
| `GET` | `/v1/events/types` | List all known event type names |
| `GET` | `/v1/events/stream` | SSE real-time event stream (long-lived connection) |

### Webhook Subscriptions (authenticated)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/webhooks/subscriptions` | Create a webhook subscription |
| `GET` | `/v1/webhooks/subscriptions` | List webhook subscriptions |
| `GET` | `/v1/webhooks/subscriptions/:id` | Get subscription details |
| `PATCH` | `/v1/webhooks/subscriptions/:id` | Update a subscription |
| `DELETE` | `/v1/webhooks/subscriptions/:id` | Delete a subscription |
| `GET` | `/v1/webhooks/subscriptions/:id/deliveries` | View delivery log for a subscription |

### CRM Feed (requires `read:feeds` scope)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/feeds/crm/orders` | Order summaries with completion progress |
| `GET` | `/v1/feeds/crm/orders/:id/timeline` | Chronological event timeline for an order |
| `GET` | `/v1/feeds/crm/orders/:id/status` | Lightweight order status check |

### Social Media Feed (requires `read:feeds` scope)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/feeds/social/milestones` | Production milestones (order completions, quality certs) |
| `GET` | `/v1/feeds/social/stats` | Aggregate production statistics |
| `GET` | `/v1/feeds/social/highlights` | Curated highlights for social content |

### Status Feed (requires `read:status` scope)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/feeds/status/detailed` | Per-component health breakdown for the tenant |
| `GET` | `/v1/feeds/status/incidents` | Recent incidents and degradations |

## Database Migrations

Recent migrations that support the new features:

| Migration | Tables Created | Purpose |
|-----------|---------------|---------|
| `022_event_outbox` | `event_outbox`, `webhook_subscriptions`, `webhook_deliveries` | Persistent event log, outbound webhook configuration, and delivery tracking |
| `023_api_keys` | `api_keys` | Hashed API key storage with tenant association and revocation support |
| `024_health_snapshots` | `health_snapshots` | Time-series component health data for the public status page |

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

- `GET /health` - Comprehensive health check (internal, includes DB and Redis status)
- `GET /health/live` - Kubernetes liveness probe
- `GET /health/ready` - Kubernetes readiness probe
- `GET /status` - Public composite health status (no auth, CORS enabled)
- `GET /status/history` - Public 90-day uptime history (no auth, CORS enabled)

## Metrics

Prometheus metrics available at `/metrics`:
- HTTP request duration and count (with `tenant_id` label)
- Database connection pool stats
- Real-time event publishing stats
- Usage tracking metrics
