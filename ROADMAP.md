# PravaraMES Roadmap

Cloud-native Manufacturing Execution System for the MADFAM ecosystem.

## Current Status

**Version**: Phase 2 Real-Time Foundation (Complete)
**Last Updated**: March 1, 2026

| Component | Status | Progress |
|-----------|--------|----------|
| pravara-api | Complete | 100% |
| pravara-ui | Active Development | 85% |
| telemetry-worker | Complete | 100% |
| pravara-gateway | Complete | 100% |
| Infrastructure | Complete | 100% |

---

## Release Timeline

```
Q1 2026                         Q2 2026                    Q3 2026
├── Phase 0: Stabilize ✅       ├── Phase 2.5: Production  ├── Phase 3.0: Compliance
├── Phase 1: MVP Complete ✅    │   - CI/CD Pipeline       │   - CFDI 4.0 (Mexico)
├── Phase 2: Real-Time ✅       │   - Observability        │   - Annex 24
│   - Centrifugo Gateway        │   - Network Security     │   - Tezca Integration
│   - Redis Pub/Sub             │   - Quality Management   │
│   - WebSocket Hooks           │   - Dhanam Billing       └── Phase 4.0: AI (Future)
│   - Live UI Updates           │
```

---

## Phase 0: Stabilization
> **Status**: Complete | **Timeline**: 1 day

Fix critical build issues to ensure codebase compiles and tests pass.

### Deliverables
- [x] Fix OIDC verifier signature mismatch
- [x] Add missing Machine type fields (Description, Metadata)
- [x] Fix config test field references
- [x] All tests passing

---

## Phase 1: MVP Completion
> **Status**: Complete ✅ | **Timeline**: 1-2 weeks

Complete all core MVP features per the PRD.

### Backend (pravara-api)
- [x] Cotiza webhook handler (`POST /v1/webhooks/cotiza`)
- [x] Order items endpoints (`GET/POST /v1/orders/:id/items`)
- [x] Telemetry query endpoints (`GET /v1/telemetry`, `/aggregated`, `/latest`)
- [x] Telemetry batch insert (`POST /v1/telemetry/batch`)

### Frontend (pravara-ui)
- [ ] Task detail modal with edit capability
- [ ] Create order dialog
- [ ] Create task dialog
- [ ] Create machine dialog
- [ ] Error toast notifications
- [ ] Token refresh handling

### Telemetry Worker
- [x] MQTT connection management
- [x] Batch processing
- [x] Database integration
- [x] Retry logic for failed writes (exponential backoff)

---

## Phase 2: Real-Time Foundation
> **Status**: Complete ✅ | **Timeline**: 1-2 weeks

Live machine status, real-time UI updates, WebSocket infrastructure.

### WebSocket Gateway (pravara-gateway)
- [x] Centrifugo v5 deployment configuration
- [x] Redis Pub/Sub engine integration
- [x] Tenant-scoped channel namespaces (machines, tasks, orders, telemetry, notifications)
- [x] Proxy authentication via pravara-api

### Backend (pravara-api)
- [x] Redis event publisher (`internal/pubsub/`)
- [x] Real-time token endpoint (`GET /v1/realtime/token`)
- [x] Centrifugo proxy auth (`POST /v1/realtime/auth`, `/subscribe`)
- [x] Event publishing from handlers (tasks, orders, machines)

### Backend (telemetry-worker)
- [x] Redis event publisher for telemetry batches
- [x] Machine heartbeat event publishing

### Frontend (pravara-ui)
- [x] Centrifuge-js client integration (`lib/realtime/`)
- [x] Real-time connection hook (`useRealtimeConnection`)
- [x] Machine updates hook (`useMachineUpdates`)
- [x] Task updates hook (`useTaskUpdates`)
- [x] Order updates hook (`useOrderUpdates`)
- [x] Telemetry updates hook (`useTelemetryUpdates`)
- [x] Zustand connection state store

### Infrastructure
- [x] Centrifugo Kubernetes deployment (`centrifugo.yaml`)
- [x] Ingress configuration for WSS routing (`ingress.yaml`)
- [x] Centrifugo secrets management

---

## Phase 2.5: Production Readiness
> **Status**: Planned | **Timeline**: 2-3 weeks

Enterprise-grade infrastructure and monitoring.

### CI/CD Pipeline
- [ ] GitHub Actions workflow
- [ ] Automated testing
- [ ] Security scanning (Trivy, gosec)
- [ ] Docker image builds
- [ ] Canary deployments via enclii

### Observability
- [ ] Prometheus metrics collection
- [ ] Grafana dashboards
- [ ] Loki log aggregation
- [ ] AlertManager rules
- [ ] Per-tenant metrics isolation

### Security
- [ ] External Secrets Operator
- [ ] Network policies (pod isolation)
- [ ] RBAC for service accounts
- [ ] Rate limiting

### Quality Management
- [ ] Quality certificate types
- [ ] Inspection workflows
- [ ] Batch lot traceability

### Billing (Dhanam)
- [ ] Usage event recording
- [ ] Tenant usage tracking
- [ ] Invoice generation hooks

---

## Phase 3.0: Mexican Compliance
> **Status**: Planned | **Timeline**: 3-4 weeks

Full regulatory compliance for Mexican market.

### CFDI 4.0 Integration
- [ ] Invoice XML generation
- [ ] SAT PAC validation via Tezca
- [ ] Digital signature handling
- [ ] Carta Porte complement

### IMMEX/Annex 24 Compliance
- [ ] 48-hour compliance window tracking
- [ ] Material entry/exit logging
- [ ] SACI synchronization
- [ ] Transformation tracking

### New Service: compliance-engine
```
apps/compliance-engine/
├── cmd/engine/main.go
├── internal/
│   ├── cfdi/      # CFDI 4.0 handling
│   ├── annex24/   # Inventory compliance
│   └── tezca/     # Tezca API client
```

---

## Phase 4.0: AI Orchestration
> **Status**: Future | **Timeline**: TBD

Intelligent manufacturing operations.

### Predictive Maintenance
- [ ] Anomaly detection from telemetry
- [ ] Failure prediction models
- [ ] Maintenance scheduling optimization

### Intelligent Scheduling
- [ ] Dynamic task allocation
- [ ] Material clustering for efficiency
- [ ] Capacity optimization

### New Service: ml-orchestrator
- Python/FastAPI for inference
- Model versioning
- A/B testing framework

---

## Architecture Overview

```
┌────────────────────────────────────────────────────────────────────────┐
│                            PravaraMES                                   │
├────────────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌─────────────────┐  │
│  │ pravara-api │ │ pravara-ui  │ │ telemetry-  │ │ pravara-gateway │  │
│  │  (Go/Gin)   │ │ (Next.js)   │ │   worker    │ │  (Centrifugo)   │  │
│  │  :4500      │ │  :4501      │ │  (Go/MQTT)  │ │     :8000       │  │
│  └──────┬──────┘ └──────┬──────┘ └──────┬──────┘ └───────┬─────────┘  │
│         │               │                │                │           │
│         │               │ WebSocket      │                │           │
│         │               └────────────────┼────────────────┘           │
│         │                                │                            │
│  ┌──────┴────────────────────────────────┴─────────────────────────┐  │
│  │                    Shared Infrastructure                         │  │
│  │  PostgreSQL (RLS) │ Redis (Pub/Sub) │ EMQX (MQTT) │ Janua SSO   │  │
│  └──────────────────────────────────────────────────────────────────┘  │
│                                                                        │
│  Future Services:                                                      │
│  ┌─────────────────┐ ┌─────────────────┐                              │
│  │ compliance-     │ │ ml-orchestrator │                              │
│  │ engine (v3.0)   │ │ (v4.0)          │                              │
│  └─────────────────┘ └─────────────────┘                              │
└────────────────────────────────────────────────────────────────────────┘
```

---

## MADFAM Ecosystem Integrations

| Integration | Phase | Status |
|-------------|-------|--------|
| **Janua SSO** | 1.0 | ✅ Implemented |
| **Cloudflare R2** | 1.0 | ✅ Configured |
| **Centrifugo** | 2.0 | ✅ Implemented |
| **ForgeSight** | 2.5 | Planned |
| **Dhanam Billing** | 2.5 | Planned |
| **Tezca Labs** | 3.0 | Planned |

---

## Contributing

See [PRD.md](./PRD.md) for detailed product requirements and technical specifications.

### Development
```bash
# Start local infrastructure
make docker-up

# Run all services
make dev

# Run tests
make test
```

### Deployment
```bash
# Deploy via enclii
enclii deploy --service pravara-api --env production
```

---

## Success Metrics

| Metric | Phase 1 | Phase 2 | Phase 2.5 | Phase 3.0 |
|--------|---------|---------|-----------|-----------|
| Build Status | ✅ Passing | ✅ Passing | Passing | Passing |
| Test Coverage | >60% | >65% | >80% | >85% |
| API Uptime | - | - | 99.9% | 99.9% |
| p95 Latency | - | - | <200ms | <200ms |
| Real-Time Latency | - | <500ms | <300ms | <300ms |
| WebSocket Connections | - | 100+ | 1000+ | 1000+ |
| CFDI Compliance | - | - | - | 100% |

---

## Contact

**Project**: PravaraMES
**Organization**: MADFAM
**Documentation**: See `PRD.md` and `README.md`
