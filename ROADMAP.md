# PravaraMES Roadmap

Cloud-native Manufacturing Execution System for the MADFAM ecosystem.

## Current Status

**Version**: Phase 2.6 MES Industry Standard Features (Complete) + Order→Dispatch Loop
**Last Updated**: October 2, 2026
**Apps in this repo**: 12, of which 6 are deployed to production by GitOps (see the
"Deployed" column and [Pending work and roadmap ahead](#pending-work-and-roadmap-ahead))

> **Verification gate:** completeness claims in this table are only as good
> as [docs/RUNTIME_VERIFICATION_CHECKLIST.md](docs/RUNTIME_VERIFICATION_CHECKLIST.md).
> A row may say "Complete" while its checklist boxes are still unchecked —
> the checklist, not this table, is the record of what has actually been
> verified against a running system.

| Component | Status | Progress | Deployed |
|-----------|--------|----------|----------|
| pravara-api | Complete* | 100%* | Yes |
| pravara-ui | Complete | 100% | Yes |
| pravara-admin | Live | — | Yes |
| pravara-landing | Live | — | Yes |
| telemetry-worker | Complete | 100% | Yes |
| pravara-gateway (Centrifugo) | Complete | 100% | Yes |
| visualization-engine | Code complete | — | No (pravara-api proxies to it; no image build) |
| video-streaming | **Does not build** | — | No (not in `go.work`, CI or deploy) |
| ml-orchestrator | Code complete, not enabled | — | No (`replicas: 0`, not in the base kustomization, no image build) |
| luban-bridge | Code complete, tests partly red | — | No |
| octoprint-connector | Code complete, tests partly red | — | No |
| machine-adapter | In Progress | 70% | No |
| Infrastructure | Complete | 100% | — |
| CI/CD Pipeline | Complete | 100% | — |
| Observability | Complete | 100% | — |
| Security | Complete | 100% | — |
| Quality Management | Complete | 100% | — |
| Billing Integration | Complete | 100% | — |
| OEE Analytics | Complete | 100% | — |
| SPC Control Charts | Complete | 100% | — |
| Maintenance CMMS | Complete | 100% | — |
| Products & BOM | Complete | 100% | — |
| Product Genealogy | Complete | 100% | — |
| Work Instructions | Complete | 100% | — |
| Inventory Management | Complete | 100% | — |
| Order→Dispatch Loop | Implemented, needs runtime verification | — | — |

\* pravara-api "Complete" previously overstated reality: until 2026-08 the
event outbox was never written (the OutboxPublisher was constructed and
discarded in `cmd/api/main.go`, so webhooks/`GET /v1/events`/CRM feeds ran on
an empty table), routing was dead code (a 473-line AgentService with zero
callers; `machines.capabilities` was never queried or even settable via the
API), orders never decomposed into tasks, and order status never advanced
from task state. The Order→Dispatch Loop work (below) closed those gaps; the
runtime checklist gates the claim.

**Deployed** means the image is built by `.github/workflows/build-deploy.yml`
(pravara-api, telemetry-worker, pravara-ui, pravara-landing, pravara-gateway)
or `.github/workflows/deploy-admin.yml` (pravara-admin), and its digest is
committed to `infra/k8s/production/kustomization.yaml`, which Argo CD
auto-syncs. No workflow builds or deploys anything else under `apps/`.

---

## Pending work and roadmap ahead

This is the canonical pending-work list for the repo. README.md, AGENTS.md,
`llms.txt` and `llms-full.txt` point here and do not keep their own copies.
It was last reconciled against `main` on 2026-10-02, after
[#45](https://github.com/madfam-org/pravara-mes/pull/45) and
[#46](https://github.com/madfam-org/pravara-mes/pull/46). When an item lands,
remove it here in the same PR.

- **Priority:** P0 blocks production now. P1 breaks a cross-repo flow or must
  land before the next enablement. P2 is quality, coverage or enablement debt.
  P3 is cleanup or future scope.
- **Type:** *Engineering* can be done in a PR. *Owner decision* needs a product
  or ownership call first.

No P0 items are open. The #45 "Build and Deploy" and "Deploy Admin" runs on
`main` succeeded on 2026-10-02. #46 deployed nothing, because ml-orchestrator
is not built.

### P1

1. **Cotiza → Pravara fabrication-dispatch contract drift.** *Owner decision,
   then engineering.* Why it matters: accepted Cotiza fabrication quotes do not
   become Pravara orders. Cotiza's dispatch is fire-and-forget, so the failure
   is only a log line on the Cotiza side. Both sides on `main` as of
   2026-10-02:

   | | Cotiza sends ([`pravara-dispatch.service.ts`](https://github.com/madfam-org/digifab-quoting/blob/main/apps/api/src/integrations/pravara/pravara-dispatch.service.ts)) | Pravara accepts ([`webhook_handlers.go`](apps/pravara-api/internal/api/webhook_handlers.go)) |
   |---|---|---|
   | Route | `POST {PRAVARA_API_URL}/api/v1/mes/jobs` | `POST /v1/webhooks/cotiza`; no `mes/jobs` route exists |
   | Signature header | `x-webhook-signature`, plus `x-webhook-timestamp` | `X-Cotiza-Signature` |
   | Signature value | hex HMAC-SHA256 of the raw body | hex HMAC-SHA256 of the raw body (matches) |
   | Shared secret env | `PRAVARA_DISPATCH_SECRET` | `COTIZA_WEBHOOK_SECRET` |
   | Caller auth | the HMAC only | the `/v1` group also requires a Janua JWT or a Pravara API key |
   | Payload | flat job: `orderId`, `externalId`, `engagement_id`, `currency`, `dueBy`, `items[]` (`quoteItemId`, `process`, `material`, `quantity`, `selections`, `files`, prices), `metadata` | envelope: `event` (`order.created`, `order.confirmed`, `order.updated`, `order.cancelled`), `timestamp`, `order` (`id` and `customer_name` required; `items[]` with `product_name` and `quantity` required) |

   Decide which side is canonical: a Pravara intake route for Cotiza's job
   shape, or Cotiza adopting Pravara's order envelope. Then change one side
   and add a contract test on both. The Cotiza-side record is the drift note in
   [digifab-quoting `AGENTS.md`, «Related repositories / contracts»](https://github.com/madfam-org/digifab-quoting/blob/main/AGENTS.md#related-repositories--contracts).
   Nothing has been changed in code on either side.
2. **Pravara → PhyndCRM status-webhook header drift.** *Engineering.* Why it
   matters: fabrication status changes may not reach the PhyndCRM client
   portal. The outbound dispatcher
   ([`webhook_dispatcher.go`](apps/pravara-api/internal/services/webhook_dispatcher.go))
   signs every delivery as `X-Pravara-Signature: sha256=<hex>`. PhyndCRM's
   [`/api/webhooks/pravara`](https://github.com/madfam-org/phynd-crm/blob/main/apps/web/src/app/api/webhooks/pravara/route.ts)
   verifies `x-webhook-signature` (it accepts the `sha256=` prefix) and would
   reject a delivery that carries only `X-Pravara-Signature`. forj's receiver
   verifies `X-Pravara-Signature`, so send both headers during any transition.
   Also check the fields PhyndCRM reads (`event`, `status`,
   `orderId`/`externalId`) against the outbox event payload. Event names:
   [phynd-crm `docs/ENGAGEMENT_EVENT_TAXONOMY.md`](https://github.com/madfam-org/phynd-crm/blob/main/docs/ENGAGEMENT_EVENT_TAXONOMY.md).
3. **Inbound webhook auth hardening checklist before relying on Cotiza intake
   (tracked privately).** *Engineering.* Do it together with item 1.

### P2

4. **ml-orchestrator: enable or retire.** *Owner decision, then engineering.*
   It is not deployed: `replicas: 0`, not listed in
   `infra/k8s/base/kustomization.yaml`, and no workflow builds it. Before
   enabling it:
   - complete a data-access hardening pass (tracked privately);
   - pin `xgboost`, `asyncpg` and `cachetools`, which the code imports but
     `requirements.txt` does not list;
   - fix the 8 pre-existing pytest failures recorded in #46 (anomaly_detection
     2, predictive_maintenance 2, process_optimizer 1, quality_prediction 3);
   - remove `torch`, which #46 moved to 2.14.1 but no code imports;
   - upgrade fastapi 0.109 / starlette 0.35 and keras 2.15 (via tensorflow
     2.15), which pip-audit still reports.

   The mlflow 3 contract from #46 is covered by
   `apps/ml-orchestrator/tests/test_training_service_mlflow.py`. The default
   tracking URI is `sqlite:///mlflow.db` and `MLFLOW_TRACKING_URI` overrides it.
   Models are logged with `log_model(name=…, serialization_format=cloudpickle)`.
5. **luban-bridge test and type debt.** *Engineering.*
   - `tsc --noEmit` reports 2 errors: `@types/cors` is missing, and the
     `MockSerialPort.list` typing in `machine-discovery.test.ts` is wrong.
   - jest has 3 failing tests: GCodeAnalyzer `validateGCode` «should detect
     missing start code» and «should detect temperature limit violations»,
     plus the Machine Routes discover test, which times out.
   - 2 suites fail to run: `machine-discovery.test.ts` (the TS error) and
     `snapmaker-protocol.test.ts` (its `jest.mock` factory references an
     import before initialization).

   The service is not in CI and not deployed. The multer 2.x upload path from
   #45 is covered by `src/routes/__tests__/gcode.test.ts`.
6. **octoprint-connector: 6 of 108 pytest tests fail.** *Engineering.* They
   fail the same way with python-multipart 0.0.26 and 0.0.32 (#45).
   fastapi 0.109 / starlette 0.35 need a fastapi upgrade. The service is not
   in CI and not deployed.
7. **CI does not gate several suites.** *Engineering.* `ci.yml` runs the
   pravara-ui `npm run test:run` step with `continue-on-error: true`. admin
   (vitest), luban-bridge (jest), octoprint-connector and ml-orchestrator
   (pytest) have no CI job at all. Add each job once its suite is green
   (items 4–6 and 8).
8. **admin `npm run lint` is broken.** *Engineering.* Next 16 (admin is on
   16.3.6) removed `next lint`. Move to the ESLint CLI with a flat config.
   pravara-landing (Next 15.5) still uses the deprecated `next lint`, which
   works there.
9. **Manifest coverage audit.** *Engineering.* Argo CD syncs
   `infra/k8s/production`, which renders only the resources listed in
   `infra/k8s/base/kustomization.yaml`. `infra/k8s/base/observability/`,
   `external-secrets/`, `ingress.yaml`, `ml-orchestrator.yaml` and other base
   sub-directories are not referenced. Confirm what the platform applies
   instead, then reference or retire each one (details tracked privately).
10. **Runtime verification.** *Owner decision (operator time).* All 90 checks
    in [docs/RUNTIME_VERIFICATION_CHECKLIST.md](docs/RUNTIME_VERIFICATION_CHECKLIST.md)
    are unchecked, including the Order→Dispatch loop (§4). Until they are
    checked, the "Complete" rows in the status table are self-reported.
11. **pravara-ui token refresh handling.** *Engineering.* This was the open
    Phase 1 item.

### P3

12. **video-streaming does not build.** *Owner decision (fix or retire).*
    `peer.VideoTrack.WriteSample` is undefined, because a
    `*webrtc.TrackLocalStaticRTP` has no `WriteSample`, and so is
    `manager.streams`. The app is not in `go.work`, CI or deploy; #45 only
    moved its `go.mod` pins.
13. **The vite dev-dependency bump is blocked.** *Engineering.* admin is on
    vite 8.0.0 and pravara-landing on 8.0.8. `npm update vite` fails with
    npm's arborist error «Cannot read properties of null (reading
    'edgesOut')». The only path that resolves pulls in vitest 4.1.11,
    rolldown 1.2.12 and lightningcss 1.33, and lightningcss is also used by the
    production Tailwind build. vite is dev-only.
14. **Remaining moderate/low npm advisories.** *Engineering.* admin has 12
    moderate (posthog-js → `@opentelemetry/*`, dompurify, fflate,
    baseline-browser-mapping). luban-bridge has qs/body-parser via express 4
    (1 low, 1 moderate).
15. **Observability backlog.** *Engineering.* Open Phase 2.5 items: Grafana
    dashboards (the JSON configmap exists), Loki log aggregation, per-tenant
    metrics isolation.
16. **External Secrets Operator.** *Engineering.* Open Phase 2.5 Security
    item; see item 9 for the unreferenced `external-secrets/` kustomization.
17. **Invoice generation hooks.** *Owner decision.* Open Phase 2.5 Billing
    item. Dhanam owns invoicing, and Pravara reports usage. Decide whether
    Pravara needs any hook beyond usage events.
18. **Phase 3.0 CFDI scope vs ecosystem ownership.** *Owner decision.* The
    Phase 3.0 checklist puts CFDI XML generation, PAC validation and signing in
    a Pravara `compliance-engine`. In the ecosystem, Karafiel issues CFDI and
    already lists Pravara's completed jobs as an input (see ECOSYSTEM.md).
    Either re-scope Phase 3.0 to emit completed-job events, keeping the
    IMMEX/Annex 24 inventory tracking, or record why Pravara needs its own CFDI
    path.
19. **The Cotiza line in ECOSYSTEM.md is stale.** *Engineering
    (enclii-owned).* It says no accepted-quote call exists yet. Cotiza does
    call, at the drifted route in item 1. ECOSYSTEM.md is generated: update
    [`docs/templates/ecosystem/metadata_fabrication.py`](https://github.com/madfam-org/enclii/blob/main/docs/templates/ecosystem/metadata_fabrication.py)
    in enclii and re-render. Do not hand-edit it.
20. **Landing demo-request endpoint.** *Owner decision (where leads go), then
    engineering.* The CTA uses `mailto:` until `/api/demo-request` exists.
21. **Close issue #38.** *Owner decision.*
    [#38](https://github.com/madfam-org/pravara-mes/issues/38) (Dependency
    graph) appears resolved: on 2026-10-02 the Dependency Review job ran on #45
    and passed with no high-severity findings.

### Roadmap ahead (feature work)

The open checkboxes in the phase sections below are the feature roadmap:
- machine-adapter completion (Phase 2.5b: full OPC-UA, Modbus TCP/RTU, edge
  gateway);
- Phase 3.0 Mexican compliance, after item 18;
- Phase 4.0 AI and automation, which depends on item 4.

The open Phase 1 and Phase 2.5 checkboxes have moved into the list above.

### Known flaky tests

No flaky test is known in the suites CI gates: the Go modules in `go.work`,
pravara-ui and pravara-landing. The red tests in items 4–6 fail on every run.
The one exception is the luban-bridge discover-route test, which fails by
timeout and may depend on the environment.

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
- [ ] Token refresh handling (tracked in [Pending work](#pending-work-and-roadmap-ahead), item 11)

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
- [x] Event publishing from handlers (tasks, orders, machines) — *corrected
      2026-08: order handlers published nothing (no order.created /
      order.status_changed) until the Order→Dispatch Loop work added them;
      this box was checked prematurely*

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

The three open items above are tracked in [Pending work](#pending-work-and-roadmap-ahead), items 9 and 15.

### Security ✅
- [ ] External Secrets Operator (tracked in [Pending work](#pending-work-and-roadmap-ahead), item 16)
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
- [ ] Invoice generation hooks (requires Dhanam API; tracked in [Pending work](#pending-work-and-roadmap-ahead), item 17)

---

## Phase 2.5b: Digital Twin & Connectivity
> **Status**: In Progress | **Timeline**: 2-3 weeks

Digital twin visualization, ML-driven quality prediction, and multi-protocol machine connectivity.

> The ✅ marks below mean the code was written. None of these services is
> deployed to production, and video-streaming does not build. See the status
> table's "Deployed" column and [Pending work](#pending-work-and-roadmap-ahead),
> items 4–6 and 12.

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

## Order→Dispatch Loop (2026-08)
> **Status**: Implemented; runtime verification pending (see
> [docs/RUNTIME_VERIFICATION_CHECKLIST.md](docs/RUNTIME_VERIFICATION_CHECKLIST.md) §4)

Makes order → task → route → dispatch → status real end-to-end. The dispatch
chain (task in_progress → task_commands → Redis → telemetry-worker → MQTT,
acks back) already worked; the pieces before and around it did not.

### Event outbox fix
- [x] Outbox persistence folded into `pubsub.Publisher` (`EnableOutbox`);
      previously the OutboxPublisher wrapper was constructed and discarded in
      `cmd/api/main.go`, and Go method promotion meant the wrapper could never
      have intercepted the `Publish*` helper methods anyway
- [x] `event_outbox` now receives every published business event, feeding the
      webhook dispatcher (HMAC, retries), `GET /v1/events`, and CRM feeds
- [x] order.created / order.status_changed / task.created / task.updated
      published from order+task handlers and the Cotiza webhook

### Order→task auto-decomposition
- [x] `OrderDecompositionService`: one production task per order item
      (title from product_name × qty, links order/order_item, specifications
      + cad_file_url carried into task metadata, backlog status)
- [x] Wired into both intake paths (`POST /v1/orders` with inline items;
      Cotiza webhook) and `POST /v1/orders/:id/items`
- [x] Config-gated: `orders.auto_decompose` (default ON — creates rows and
      events only; machine commands still dispatch exclusively on the
      explicit task → in_progress transition)

### Capability-based assignment (routing wired for real)
- [x] `MachineAssignmentService` with a documented capability-matching
      contract (item specifications → requirement tokens; machine
      capabilities superset match) and a transparent scorer
      (status weight − load penalty; least-recently-assigned tie-break)
- [x] `machines.capabilities` now settable via the machine create/update API
      and read by assignment (the column existed since genesis but was never
      queried nor writable)
- [x] No match → task stays unassigned + `task.assignment_failed` event +
      UI warning notification (fail visible, not silent); items with no
      declared requirements are left for human routing by design
- [x] Dead code deleted: `services/agent_service.go` (473-line operator
      scorer, zero callers, broken tenant plumbing) — "Intelligent
      Scheduling" below remains future work, now with an honest baseline
- [x] Config-gated: `orders.auto_assign` (default ON)

### Order status roll-up + vocabulary reconciliation
- [x] First task in_progress/quality_check → order `in_progress`; all tasks
      completed → order `completed` (guarded single-statement SQL; never
      regresses shipped/cancelled)
- [x] Status vocab reconciled: DB enum (received/validated/scheduled/
      in_progress/completed/shipped/cancelled) is the source of truth; legacy
      SDK/UI names (confirmed, in_production, quality_check, ready,
      delivered, processing) are normalized at the API boundary
      (`types.NormalizeOrderStatus`) instead of reaching Postgres and failing
- [x] CRM/social feed queries fixed: they compared against 'delivered' and
      'ready', values the order_status enum rejects, so the feed SQL errored
      at runtime; now canonical ('shipped'/'completed')
- [x] `orders.shipping_address` JSONB (migration 025) + API/SDK/UI wiring
- [x] Migration 026 adds `machine_status` value `'online'` — heartbeats
      already wrote it but the genesis enum never contained it
- [x] Migration runner created: `infra/db/migrate.sh` (`make db-migrate`
      pointed at it since the Makefile existed, but the script did not) —
      migrations 002–008 never existed; numbering continues from 024

## Phase 3.0: Mexican Compliance
> **Status**: Planned | **Timeline**: 3-4 weeks

Full regulatory compliance for Mexican market.

> **Scope under review:** in the MADFAM ecosystem, Karafiel owns CFDI
> issuance. The CFDI items below are kept until the owner decides
> ([Pending work](#pending-work-and-roadmap-ahead), item 18).

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

Advanced intelligent manufacturing operations building on the ml-orchestrator foundation (code-complete in Phase 2.5b but not deployed; see [Pending work](#pending-work-and-roadmap-ahead), item 4).

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
| **Tezca Labs** | 3.0 | Law-change webhook implemented (`/v1/webhooks/tezca`); compliance-engine planned |
| **Yantra4D** | — | ✅ Hyperobject import (`/v1/import/yantra4d`) |
| **Forj** | — | ✅ Orders in via API key; `order.status_changed` out via webhook subscriptions |
| **Cotiza** | — | ⚠️ Inbound webhook implemented; contract drift with Cotiza's dispatcher (Pending work, item 1) |
| **PhyndCRM** | — | ⚠️ Outbound status webhook; signature-header drift (Pending work, item 2) |

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
Merging to `main` is the deploy. `build-deploy.yml` (path-filtered per service)
and `deploy-admin.yml` build and sign the images and commit their digests to
`infra/k8s/production/kustomization.yaml`; Argo CD auto-syncs that overlay.
Docs-only changes (`**.md`, `docs/**`) do not trigger a build. Routine
production operations go through Enclii (see [ECOSYSTEM.md](./ECOSYSTEM.md)).

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
