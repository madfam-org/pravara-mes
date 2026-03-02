# PravaraMES Roadmap

Cloud-native Manufacturing Execution System for the MADFAM ecosystem.

## Current Status

**Version**: MVP Phase 1 (In Development)
**Last Updated**: March 1, 2026

| Component | Status | Progress |
|-----------|--------|----------|
| pravara-api | Active Development | 90% |
| pravara-ui | Active Development | 75% |
| telemetry-worker | Complete | 100% |
| Infrastructure | Ready | 70% |

---

## Release Timeline

```
Q1 2026                    Q2 2026                    Q3 2026
├── Phase 0: Stabilize     ├── Phase 1.5: Production  ├── Phase 2.0: Compliance
├── Phase 1: MVP Complete  │   - CI/CD Pipeline       │   - CFDI 4.0 (Mexico)
│   - Cotiza Integration   │   - Observability        │   - Annex 24
│   - Kanban Board         │   - Network Security     │   - Tezca Integration
│   - Telemetry Pipeline   │   - Quality Management   │
│   - CRUD Operations      │   - Dhanam Billing       └── Phase 3.0: AI (Future)
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
> **Status**: In Progress | **Timeline**: 1-2 weeks

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

## Phase 1.5: Production Readiness
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

## Phase 2.0: Mexican Compliance
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

## Phase 3.0: AI Orchestration
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
┌─────────────────────────────────────────────────────────────────┐
│                        PravaraMES                                │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐ ┌─────────────┐ ┌───────────────┐              │
│  │ pravara-api │ │ pravara-ui  │ │ telemetry-    │              │
│  │  (Go/Gin)   │ │ (Next.js)   │ │ worker        │              │
│  │  :4500      │ │  :4501      │ │  :4502        │              │
│  └──────┬──────┘ └──────┬──────┘ └───────┬───────┘              │
│         │               │                 │                      │
│  ┌──────┴───────────────┴─────────────────┴──────────────────┐  │
│  │                    Shared Infrastructure                   │  │
│  │  PostgreSQL (RLS) │ Redis │ EMQX (MQTT) │ Janua SSO       │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                  │
│  Future Services:                                                │
│  ┌─────────────────┐ ┌─────────────────┐                        │
│  │ compliance-     │ │ ml-orchestrator │                        │
│  │ engine (v2.0)   │ │ (v3.0)          │                        │
│  └─────────────────┘ └─────────────────┘                        │
└─────────────────────────────────────────────────────────────────┘
```

---

## MADFAM Ecosystem Integrations

| Integration | Phase | Status |
|-------------|-------|--------|
| **Janua SSO** | 1.0 | Implemented |
| **Cloudflare R2** | 1.0 | Configured |
| **ForgeSight** | 1.5 | Planned |
| **Dhanam Billing** | 1.5 | Planned |
| **Tezca Labs** | 2.0 | Planned |

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

| Metric | Phase 1 | Phase 1.5 | Phase 2.0 |
|--------|---------|-----------|-----------|
| Build Status | Passing | Passing | Passing |
| Test Coverage | >60% | >80% | >85% |
| API Uptime | - | 99.9% | 99.9% |
| p95 Latency | - | <200ms | <200ms |
| CFDI Compliance | - | - | 100% |

---

## Contact

**Project**: PravaraMES
**Organization**: MADFAM
**Documentation**: See `PRD.md` and `README.md`
