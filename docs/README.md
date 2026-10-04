# PravaraMES Documentation

Central documentation index for the PravaraMES manufacturing execution system.

## Quick Links

| Document | Description |
|----------|-------------|
| [README.md](../README.md) | Project overview and quick start |
| [PRD.md](../PRD.md) | Product requirements document |
| [ROADMAP.md](../ROADMAP.md) | Status table (with what is deployed), phases, and the canonical [pending-work list](../ROADMAP.md#pending-work-and-roadmap-ahead) |
| [RUNTIME_VERIFICATION_CHECKLIST.md](./RUNTIME_VERIFICATION_CHECKLIST.md) | Runtime checks that gate the roadmap's "Complete" claims |
| [ECOSYSTEM.md](../ECOSYSTEM.md) | MADFAM ecosystem and Enclii operations context (generated) |
| [README «Related repositories / contracts»](../README.md#related-repositories--contracts) | Cross-repo contracts, linked to the other side's doc |
| [OBSERVABILITY.md](../OBSERVABILITY.md) | Metrics, logging, and monitoring |
| [deploy/edge/README.md](../deploy/edge/README.md) | Site box kit: machine adapter as the Sparkplug B edge node |
| [packages/sparkplug/README.md](../packages/sparkplug/README.md) | Sparkplug B namespace, metrics, commands and ACL (MES-1) |

## API Documentation

- **OpenAPI Specification**: [swagger.yaml](./swagger.yaml) / [swagger.json](./swagger.json)
- **Swagger UI**: Available at `http://localhost:4500/swagger` (development only)

### API Endpoints Overview

| Endpoint Group | Base Path | Description |
|----------------|-----------|-------------|
| Health | `/health` | Health checks and probes |
| Orders | `/v1/orders` | Order management |
| Tasks | `/v1/tasks` | Kanban board operations |
| Machines | `/v1/machines` | Machine management |
| Telemetry | `/v1/telemetry` | Machine telemetry data |
| Quality | `/v1/quality/*` | Certificates, inspections, batch lots |
| Billing | `/v1/billing` | Usage tracking |
| Realtime | `/v1/realtime` | WebSocket authentication |
| Webhooks (inbound) | `/v1/webhooks/{cotiza,dhanam,forgesight,tezca}` | Signed inbound events; the Cotiza contract has drifted (see README «Related repositories / contracts») |
| Webhook subscriptions | `/v1/webhooks/subscriptions` | Outbound webhooks, signed `X-Pravara-Signature` |
| Yantra4D Import | `/v1/import/yantra4d` | Hyperobject import from Yantra4D |

## Application READMEs

| Application | README | Description | Deployed |
|-------------|--------|-------------|----------|
| pravara-api | [README](../apps/pravara-api/README.md) | REST API server | Yes |
| pravara-ui | [README](../apps/pravara-ui/README.md) | Next.js dashboard | Yes |
| pravara-landing | [README](../apps/pravara-landing/README.md) | Public marketing site | Yes |
| admin (pravara-admin) | — | Admin console (Next 16) | Yes |
| telemetry-worker | [README](../apps/telemetry-worker/README.md) | MQTT processor | Yes |
| machine-adapter | — | Machine connectivity adapter with dynamic registration | No |
| visualization-engine | — | 3D factory floor visualization with Yantra4D import | No |
| luban-bridge | [README](../apps/luban-bridge/README.md) | Snapmaker/Luban bridge | No |
| octoprint-connector | — | OctoPrint manager | No |
| ml-orchestrator | — | ML/AI pipeline (`replicas: 0`) | No |
| video-streaming | — | WebRTC streaming (does not build today) | No |
| sdk-go | [README](../packages/sdk-go/README.md) | Shared Go types | — |

## Internal Package Documentation

### pravara-api/internal

| Package | README | Description |
|---------|--------|-------------|
| api | [README](../apps/pravara-api/internal/api/README.md) | HTTP handlers |
| db | [README](../apps/pravara-api/internal/db/README.md) | Database layer |
| middleware | [README](../apps/pravara-api/internal/middleware/README.md) | Auth, rate limiting |
| pubsub | [README](../apps/pravara-api/internal/pubsub/README.md) | Real-time events |
| services | [README](../apps/pravara-api/internal/services/README.md) | Business logic |
| billing | [README](../apps/pravara-api/internal/billing/README.md) | Usage tracking |

### telemetry-worker/internal

| Package | README | Description |
|---------|--------|-------------|
| mqtt | [README](../apps/telemetry-worker/internal/mqtt/README.md) | MQTT handler |
| command | [README](../apps/telemetry-worker/internal/command/README.md) | Command dispatch |

## AI/Agent Documentation

| File | Description |
|------|-------------|
| [AGENTS.md](../AGENTS.md) | Canonical agent instructions and verified repo facts |
| [llms.txt](../llms.txt) | Compact LLM context index |
| [llms-full.txt](../llms-full.txt) | Full LLM context map |
| [.cursorrules](../.cursorrules) | Cursor AI code patterns |

## Machine Adapter Documentation

| Document | Description |
|----------|-------------|
| [MACHINE_UNIVERSE.md](./MACHINE_UNIVERSE.md) | Complete catalog of 50 supported machines |
| [PROTOCOL_COMPLIANCE_MATRIX.md](./PROTOCOL_COMPLIANCE_MATRIX.md) | Protocol compliance tracking across all adapters |

## Infrastructure Documentation

| Document | Description |
|----------|-------------|
| [Security README](../infra/k8s/base/security/README.md) | Security hardening |
| [OBSERVABILITY.md](../OBSERVABILITY.md) | Metrics, alerts, per-tenant metrics, Grafana dashboards |
| [OBSERVABILITY_DEPLOYMENT.md](../OBSERVABILITY_DEPLOYMENT.md) | Deployment checklist for observability stack |

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   pravara-ui    │────▶│   pravara-api   │────▶│   PostgreSQL    │
│   (Next.js)     │     │   (Go/Gin)      │     │   (RLS)         │
│   :4501         │     │   :4500         │     │   :5432         │
└─────────────────┘     └─────────────────┘     └─────────────────┘
        │                       │                       │
        │                       ▼                       │
        │               ┌───────────────┐               │
        │               │    Redis      │◀──────────────┘
        │               │    :6379      │
        │               └───────────────┘
        │                       │
        │                       ▼
        │               ┌───────────────┐     ┌─────────────────┐
        └──────────────▶│  Centrifugo   │◀────│ telemetry-worker│
                        │  (WebSocket)  │     │   :4502         │
                        │  :8000        │     └─────────────────┘
                        └───────────────┘               │
                                                        ▼
                                                ┌───────────────┐
                                                │  MQTT Broker  │
                                                │  :1883        │
                                                └───────────────┘
                                                        │
                                                        ▼
                                                ┌───────────────┐
                                                │   Machines    │
                                                └───────────────┘
```

## Development Commands

```bash
# Start all services
cd infra && docker-compose up -d

# Run API server
cd apps/pravara-api && go run ./cmd/api

# Run UI dev server
cd apps/pravara-ui && npm run dev

# Run telemetry worker
cd apps/telemetry-worker && go run ./cmd/worker

# Generate OpenAPI spec
make docs-openapi

# Run all tests
make test

# Type check TypeScript
npm run typecheck
```

## Contributing

1. Follow patterns in existing code
2. Add tests for new functionality
3. Update relevant READMEs
4. Add OpenAPI annotations for new endpoints
5. Run `make docs` before committing
