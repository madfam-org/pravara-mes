# PravaraMES Roadmap

Cloud-native Manufacturing Execution System for the MADFAM ecosystem.

## Current Status

**Version**: Phase 2.6 MES Industry Standard Features (Complete)
**Last Updated**: March 3, 2026
**Total Services**: 10

| Component | Status | Progress |
|-----------|--------|----------|
| pravara-api | Complete | 100% |
| pravara-ui | Complete | 100% |
| telemetry-worker | Complete | 100% |
| pravara-gateway | Complete | 100% |
| visualization-engine | Complete | 100% |
| video-streaming | Complete | 100% |
| ml-orchestrator | Complete | 100% |
| luban-bridge | Complete | 100% |
| octoprint-connector | Complete | 100% |
| machine-adapter | In Progress | 70% |
| Infrastructure | Complete | 100% |
| CI/CD Pipeline | Complete | 100% |
| Observability | Complete | 100% |
| Security | Complete | 100% |
| Quality Management | Complete | 100% |
| Billing Integration | Complete | 100% |
| OEE Analytics | Complete | 100% |
| SPC Control Charts | Complete | 100% |
| Maintenance CMMS | Complete | 100% |
| Products & BOM | Complete | 100% |
| Product Genealogy | Complete | 100% |
| Work Instructions | Complete | 100% |
| Inventory Management | Complete | 100% |

---

## Release Timeline

```
Q1 2026                         Q2 2026                    Q3 2026
├── Phase 0: Stabilize ✅       ├── Phase 2.5: Production  ├── Phase 3.0: Compliance
├── Phase 1: MVP Complete ✅    │   ✅ Complete             │   - CFDI 4.0 (Mexico)
├── Phase 2: Real-Time ✅       ├── Phase 2.5b: Digital    │   - Annex 24
│   - Centrifugo Gateway        │   Twin & Connectivity    │   - Tezca Integration
│   - Redis Pub/Sub             │   - 3D Visualization ✅  │
│   - WebSocket Hooks           │   - Video Streaming ✅   └── Phase 4.0: Future
│   - Live UI Updates           │   - ML Orchestrator ✅       - Predictive Maintenance
│                               │   - Luban Bridge ✅          - Intelligent Scheduling
│                               │   - OctoPrint ✅
│                               │   - Machine Adapter 🔄
│                               ├── Phase 2.6: MES Industry
│                               │   Standard Features ✅
│                               │   - OEE Dashboard ✅
│                               │   - Maintenance CMMS ✅
│                               │   - Product Genealogy ✅
│                               │   - Work Instructions ✅
│                               │   - SPC Control Charts ✅
│                               │   - Inventory Mgmt ✅
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
- [x] Task detail modal with edit capability
- [x] Create order dialog
- [x] Create task dialog
- [x] Create machine dialog
- [x] Error toast notifications
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
> **Status**: Complete ✅ | **Timeline**: 2-3 weeks

Enterprise-grade infrastructure and monitoring.

### CI/CD Pipeline ✅
- [x] GitHub Actions workflow (PR validation, build/deploy, security)
- [x] Automated testing (Go tests, TypeScript typecheck)
- [x] Security scanning (Trivy, gosec, npm audit, dependency-review)
- [x] Docker image builds (GHCR with SHA tags, SBOM)
- [x] Canary deployments via enclii

### Observability ✅
- [x] Prometheus metrics collection (API + Worker instrumented)
- [x] ServiceMonitors/PodMonitors for Prometheus Operator
- [x] AlertManager rules (12 alerts: 6 critical, 6 warning)
- [ ] Grafana dashboards (JSON configmap ready)
- [ ] Loki log aggregation
- [ ] Per-tenant metrics isolation

### Security ✅
- [ ] External Secrets Operator
- [x] Network policies (pod isolation)
- [x] RBAC for service accounts
- [x] Rate limiting (per-IP and per-tenant)
- [x] Pod Security Standards (restricted)

### Quality Management ✅
- [x] Quality certificate types (COC, COA, inspection, test_report, calibration)
- [x] Inspection workflows with checklist support
- [x] Batch lot traceability with supplier tracking

### Billing (Dhanam) ✅
- [x] Usage event recording (7 event types)
- [x] Tenant usage tracking (Redis-based)
- [x] Usage reporting API endpoints
- [ ] Invoice generation hooks (requires Dhanam API)

---

## Phase 2.5b: Digital Twin & Connectivity
> **Status**: In Progress | **Timeline**: 2-3 weeks

Digital twin visualization, ML-driven quality prediction, and multi-protocol machine connectivity.

### Visualization Engine ✅
- [x] 3D visualization and G-code simulation
- [x] Real-time digital twin rendering
- [x] Toolpath preview and layer analysis

### Video Streaming ✅
- [x] Camera management and WebRTC streaming
- [x] Multi-camera support with tenant isolation
- [x] Live monitoring feed integration

### ML Orchestrator ✅
- [x] ML quality prediction models
- [x] Process optimization recommendations
- [x] Anomaly detection from telemetry data
- [x] Model versioning and inference pipeline

### Luban Bridge ✅
- [x] Snapmaker/Luban integration
- [x] Job submission and status tracking
- [x] G-code transfer and machine control

### OctoPrint Connector ✅
- [x] OctoPrint 3D printer integration
- [x] Print job management and monitoring
- [x] Temperature and progress telemetry

### Machine Adapter (In Progress)
- [x] Multi-protocol architecture (OPC-UA, MQTT, Modbus)
- [x] Protocol abstraction layer
- [ ] Full OPC-UA implementation
- [ ] Modbus TCP/RTU driver completion
- [ ] Edge gateway deployment

---

## Phase 2.6: MES Industry Standard Features
> **Status**: Complete ✅ | **Timeline**: 1 week

Core MES capabilities aligned with MESA International standards.

### OEE Dashboard (MESA #11 - Performance Analysis) ✅
- [x] OEE computation (availability x performance x quality)
- [x] Fleet-wide OEE summary across all machines
- [x] Daily OEE snapshots with trend analysis
- [x] OEE gauge and trend chart UI components

### Maintenance CMMS (MESA #9 - Maintenance Management) ✅
- [x] Maintenance schedules with multiple trigger types (calendar, runtime_hours, cycle_count, condition)
- [x] Work order lifecycle (scheduled -> overdue -> in_progress -> completed | cancelled)
- [x] Machine maintenance history view
- [x] Real-time maintenance event notifications

### Product Genealogy & BOM (MESA #10 - Product Tracking) ✅
- [x] Product definitions with SKU, version, and category
- [x] Flat one-level bill of materials
- [x] Traceability chain: product -> order -> task -> machine -> quality -> certificate
- [x] Digital birth certificates with SHA-256 tamper-proof sealing
- [x] Genealogy timeline visualization

### Work Instructions (MESA #4 - Document Control) ✅
- [x] Step-by-step production procedures
- [x] Auto-attachment to tasks on queue
- [x] Operator acknowledgement tracking per step
- [x] Task-level work instruction management

### SPC Control Charts (MESA #7 - Quality Management, enhanced) ✅
- [x] Control limit computation (UCL/LCL = mean +/- 3 sigma)
- [x] Violation detection (above_ucl, below_lcl, run_of_7, trend)
- [x] SPC chart data endpoint for visualization
- [x] Violation acknowledgement workflow

### Inventory Management (MESA #1 - Resource Management) ✅
- [x] Inventory items with quantity tracking
- [x] Stock adjustment with transaction logging
- [x] Low-stock alerts with configurable reorder points
- [x] ForgeSight webhook integration for external inventory sync

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

## Phase 4.0: Advanced AI & Automation
> **Status**: Future | **Timeline**: TBD

Advanced intelligent manufacturing operations building on the ml-orchestrator foundation (deployed in Phase 2.5b).

### Predictive Maintenance (builds on Phase 2.6 OEE + Maintenance CMMS)
- [ ] Advanced failure prediction models using OEE trend data
- [ ] Maintenance scheduling optimization integrated with CMMS work orders
- [ ] Remaining useful life estimation from telemetry and maintenance history

### Finite Capacity Scheduling (builds on Phase 2.6 OEE + Maintenance)
- [ ] Dynamic task allocation considering OEE-weighted machine capacity
- [ ] Maintenance window awareness for schedule optimization
- [ ] Material clustering for efficiency using inventory data

### CAPA (Corrective and Preventive Action) (builds on Phase 2.6 SPC)
- [ ] Automatic CAPA creation from SPC violation patterns
- [ ] Root cause analysis workflows linked to genealogy records
- [ ] Preventive action tracking with effectiveness measurement

### ML Orchestrator Enhancements
- [ ] A/B testing framework for model variants
- [ ] Federated learning across tenant deployments
- [ ] AutoML pipeline for custom model training

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                              PravaraMES (10 Services)                        │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Core Services (Phase 1-2.5)                                                 │
│  ┌─────────────┐ ┌─────────────┐ ┌──────────────┐ ┌─────────────────┐       │
│  │ pravara-api │ │ pravara-ui  │ │ telemetry-   │ │ pravara-gateway │       │
│  │  (Go/Gin)   │ │ (Next.js)   │ │   worker     │ │  (Centrifugo)   │       │
│  │  :4500      │ │  :4501      │ │  (Go/MQTT)   │ │     :8000       │       │
│  └──────┬──────┘ └──────┬──────┘ └──────┬───────┘ └───────┬─────────┘       │
│         │               │               │                  │                 │
│  Digital Twin & Connectivity (Phase 2.5b)                                    │
│  ┌────────────────┐ ┌────────────────┐ ┌────────────────┐                    │
│  │ visualization- │ │ video-         │ │ ml-            │                    │
│  │ engine         │ │ streaming      │ │ orchestrator   │                    │
│  │ (3D/G-code)    │ │ (WebRTC)       │ │ (Python/ML)    │                    │
│  └────────┬───────┘ └────────┬───────┘ └────────┬───────┘                    │
│  ┌────────────────┐ ┌────────────────┐ ┌────────────────┐                    │
│  │ luban-bridge   │ │ octoprint-     │ │ machine-       │                    │
│  │ (Snapmaker)    │ │ connector      │ │ adapter  [WIP] │                    │
│  └────────┬───────┘ └────────┬───────┘ └────────┬───────┘                    │
│           │                  │                   │                            │
│  ┌───────────────────────────────────────────────────────────────────┐        │
│  │                    Shared Infrastructure                          │        │
│  │  PostgreSQL (RLS) │ Redis (Pub/Sub) │ EMQX (MQTT) │ Janua SSO    │        │
│  └───────────────────────────────────────────────────────────────────┘        │
│                                                                              │
│  Future Services:                                                            │
│  ┌─────────────────┐                                                         │
│  │ compliance-     │                                                         │
│  │ engine (v3.0)   │                                                         │
│  └─────────────────┘                                                         │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## MADFAM Ecosystem Integrations

| Integration | Phase | Status |
|-------------|-------|--------|
| **Janua SSO** | 1.0 | ✅ Implemented |
| **Cloudflare R2** | 1.0 | ✅ Configured |
| **Centrifugo** | 2.0 | ✅ Implemented |
| **Dhanam Billing** | 2.5 | ✅ Implemented |
| **Snapmaker/Luban** | 2.5b | ✅ Implemented |
| **OctoPrint** | 2.5b | ✅ Implemented |
| **ForgeSight** | 2.5b | ✅ Implemented |
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

| Metric | Phase 1 | Phase 2 | Phase 2.5 | Phase 2.5b | Phase 2.6 | Phase 3.0 |
|--------|---------|---------|-----------|------------|-----------|-----------|
| Build Status | ✅ Passing | ✅ Passing | ✅ Passing | ✅ Passing | ✅ Passing | Passing |
| Test Coverage | >60% | >65% | >80% | >80% | >80% | >85% |
| Total Services | 3 | 4 | 4 | 10 | 10 | 11 |
| API Uptime | - | - | 99.9% | 99.9% | 99.9% | 99.9% |
| p95 Latency | - | - | <200ms | <200ms | <200ms | <200ms |
| Real-Time Latency | - | <500ms | <300ms | <300ms | <300ms | <300ms |
| WebSocket Connections | - | 100+ | 1000+ | 1000+ | 1000+ | 1000+ |
| ML Model Accuracy | - | - | - | >90% | >90% | >90% |
| MESA Features | - | - | - | - | 6/11 | 6/11 |
| CFDI Compliance | - | - | - | - | - | 100% |

---

## Contact

**Project**: PravaraMES
**Organization**: MADFAM
**Documentation**: See `PRD.md` and `README.md`
