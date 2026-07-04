# Runtime Verification Checklist

> **Why this document exists (RFC 0024 P3.3, internal-devops):** the roadmap
> and README self-report "Complete / 100%" for nearly every component, but
> those claims have never been verified against a running system by anyone
> other than the author. Per RFC 0024 P3.3 ("make CI actually gate what the
> repos claim"), every major completeness claim below gets an explicit,
> checkable runtime verification step: what to run or hit, and what result
> counts as "verified". A claim stays unchecked until someone performs the
> step against a real deployment (or `make docker-up` + `make dev` locally)
> and records the outcome.
>
> Sources for the claims: [ROADMAP.md](../ROADMAP.md) status table
> (2026-03-03) and phase sections, and [README.md](../README.md).
>
> Conventions: `$API` = pravara-api base URL (local default
> `http://localhost:4500`); `$TOKEN` = a valid Janua JWT or API key for a
> test tenant. Ports per README: ui 4501, worker 4502, gateway 8000,
> viz 4205, video 4206, ml 4207, luban 4507, octoprint 4508.

## Known discrepancies found while compiling this list

The ROADMAP status table says "Complete | 100%" for several rows whose own
phase sections still contain unchecked items. These should be reconciled,
not verified away:

- **Observability** — table says 100%, but Phase 2.5 leaves unchecked:
  Grafana dashboards, Loki log aggregation, per-tenant metrics isolation.
- **Security** — table says 100%, but External Secrets Operator is unchecked.
- **Billing Integration** — table says 100%, but invoice generation hooks
  (Dhanam API) are unchecked.
- **pravara-ui / Phase 1** — table says 100%, but token refresh handling is
  unchecked.

---

## 1. Services (ROADMAP: nine services "Complete | 100%")

### 1.1 pravara-api
- [ ] `curl -fsS $API/health` returns HTTP 200 with DB, Redis, and MQTT
      subsystems reported healthy.
- [ ] `curl -fsS $API/health/live` and `curl -fsS $API/health/ready` both
      return HTTP 200.
- [ ] `curl -fsS -H "Authorization: Bearer $TOKEN" $API/v1/orders` returns
      HTTP 200 with a JSON list (auth path works end to end).
- [ ] `make test-api` (`go test ./apps/pravara-api/...`) passes with zero
      failures.

### 1.2 pravara-ui
- [ ] `http://localhost:4501` loads the dashboard after Janua SSO login;
      orders, tasks (Kanban), and machines pages render real API data.
- [ ] Create an order via the UI dialog; it appears via
      `curl -H "Authorization: Bearer $TOKEN" $API/v1/orders`.
- [ ] Known gap (Phase 1 unchecked item): leave a session idle past token
      expiry and confirm behavior — token refresh handling is *not* claimed
      done; record what actually happens.

### 1.3 telemetry-worker
- [ ] `curl -fsS http://localhost:4502/health` returns HTTP 200.
- [ ] Publish a telemetry message to the EMQX broker (UNS topic per README),
      then confirm it lands: `curl -H "Authorization: Bearer $TOKEN"
      "$API/v1/telemetry/latest?machine_id=<id>"` returns the value.
- [ ] `make test-worker` (`go test ./apps/telemetry-worker/...`) passes.

### 1.4 pravara-gateway (Centrifugo)
- [ ] Gateway answers on port 8000 (Centrifugo health/info endpoint returns
      HTTP 200).
- [ ] `curl -H "Authorization: Bearer $TOKEN" $API/v1/realtime/token`
      returns a connection token, and a WebSocket client can connect and
      subscribe to a tenant-scoped channel with it.
- [ ] Update a task via `PATCH $API/v1/tasks/:id` and observe the event
      arrive on the subscribed channel in under a second.

### 1.5 visualization-engine
- [ ] `curl -fsS http://localhost:4205/health` returns HTTP 200.
- [ ] `curl -H "Authorization: Bearer $TOKEN" $API/v1/layouts/active`
      (API-to-viz-engine proxy) returns HTTP 200, not a proxy error.
- [ ] Upload/view a 3D model or G-code file and confirm it renders in the
      UI's digital-twin view.

### 1.6 video-streaming
- [ ] `curl -fsS http://localhost:4206/health` returns HTTP 200.
- [ ] Register a camera and confirm a WebRTC stream plays in the UI live
      monitoring feed (or via a test WebRTC client).

### 1.7 ml-orchestrator
- [ ] `curl -fsS http://localhost:4207/health` returns HTTP 200.
- [ ] Request a quality prediction / anomaly-detection inference against
      sample telemetry and receive a scored response (not a stub).
- [ ] `pytest apps/ml-orchestrator/tests/` passes.

### 1.8 luban-bridge
- [ ] `curl -fsS http://localhost:4507/health` returns HTTP 200.
- [ ] `POST /api/gcode/analyze` with a sample G-code file returns a real
      analysis (layer count, estimated time).
- [ ] With a Snapmaker on the network (or simulator): `POST
      /api/machine/discover` finds it and `/api/machine/:id/connect` succeeds.

### 1.9 octoprint-connector
- [ ] `curl -fsS http://localhost:4508/health` returns HTTP 200.
- [ ] `POST /instances` with a reachable OctoPrint instance succeeds and
      `GET /instances/:id/status` returns live printer state.

### 1.10 machine-adapter — roadmap says **70% / In Progress**; no runtime
verification owed yet. Listed here only so its later "complete" claim gets
steps added.

---

## 2. Cross-cutting claims (ROADMAP status table)

### 2.1 Infrastructure — "Complete | 100%"
- [ ] `make docker-up` brings up PostgreSQL, Redis, and EMQX with no failed
      containers (`docker compose ps` shows all healthy).
- [ ] `make db-migrate` runs all migrations cleanly on a fresh database.
- [ ] Kubernetes manifests in `infra/` apply cleanly to a test cluster
      (`kubectl apply --dry-run=server`), including centrifugo.yaml and
      ingress.yaml.

### 2.2 CI/CD Pipeline — "Complete | 100%"
- [ ] The five workflows in `.github/workflows/` (ci.yml, pr-validation.yml,
      build-deploy.yml, deploy-admin.yml, security-scan.yml) all show green
      runs on main within the last 30 days (`gh run list`).
- [ ] pr-validation actually blocks: open a PR with a failing Go test and
      confirm the merge is blocked by a required check.
- [ ] build-deploy publishes SHA-tagged images with SBOMs to GHCR for a
      merged commit.

### 2.3 Observability — "Complete | 100%" (see discrepancies above)
- [ ] `curl -fsS $API/metrics` (and the worker's metrics port) return
      Prometheus metrics.
- [ ] ServiceMonitors/PodMonitors are picked up: the API and worker targets
      show "UP" in Prometheus.
- [ ] The 12 AlertManager rules (6 critical, 6 warning) are loaded; trigger
      one (e.g. stop the worker) and confirm the alert fires.
- [ ] Grafana dashboards / Loki: NOT claimed complete — verify the status
      table is corrected rather than checking these off.

### 2.4 Security — "Complete | 100%" (see discrepancies above)
- [ ] Unauthenticated `curl $API/v1/orders` returns 401.
- [ ] A tenant-A token cannot read tenant-B data (RLS check): request a
      known tenant-B resource ID and confirm 404/403.
- [ ] Rate limiting: burst > limit requests from one IP and confirm 429s.
- [ ] NetworkPolicies and restricted Pod Security Standards are active in
      the cluster (`kubectl get networkpolicy`, pod securityContext audit).
- [ ] External Secrets Operator: NOT claimed complete — confirm the status
      table is corrected.

### 2.5 Quality Management — "Complete | 100%"
- [ ] `POST $API/v1/quality/certificates` creates a certificate of each
      claimed type (coc, coa, inspection, test_report, calibration);
      `GET` returns them.
- [ ] Inspection workflow: create via `POST /v1/quality/inspections` with a
      checklist, then `POST /v1/quality/inspections/:id/complete` transitions
      state correctly.
- [ ] Batch lot traceability: `POST /v1/quality/batches` with supplier data,
      then retrieve and confirm linkage to orders/tasks.

### 2.6 Billing Integration (Dhanam) — "Complete | 100%" (see discrepancies)
- [ ] Perform a billable action (e.g. telemetry batch insert), then
      `GET $API/v1/billing/usage` shows the usage event recorded.
- [ ] `GET $API/v1/billing/usage/daily` returns a per-day breakdown.
- [ ] `POST $API/v1/webhooks/dhanam` with a signed test payload is accepted.
- [ ] Invoice generation hooks: NOT claimed complete — confirm the status
      table is corrected.

### 2.7 OEE Analytics — "Complete | 100%"
- [ ] `POST $API/v1/analytics/oee/compute` for a machine with seeded
      telemetry/task data returns availability, performance, quality, and
      OEE = A×P×Q (spot-check the arithmetic).
- [ ] `GET $API/v1/analytics/oee/summary` returns fleet-wide OEE across all
      machines.
- [ ] Daily snapshots accumulate: compute on two consecutive days and
      confirm `GET /v1/analytics/oee` returns a trend.
- [ ] OEE gauge and trend chart render in the UI with the same numbers.

### 2.8 SPC Control Charts — "Complete | 100%"
- [ ] `POST $API/v1/analytics/spc/limits/compute` on a seeded metric returns
      UCL/LCL = mean ± 3σ (spot-check against a manual computation).
- [ ] Inject an out-of-limits point and confirm
      `GET /v1/analytics/spc/violations` reports `above_ucl`/`below_lcl`;
      inject 7 same-side points for `run_of_7`.
- [ ] `GET /v1/analytics/spc/chart` returns plottable series data.
- [ ] `POST /v1/analytics/spc/violations/:id/acknowledge` transitions the
      violation state.

### 2.9 Maintenance CMMS — "Complete | 100%"
- [ ] Create a schedule for each trigger type (calendar, runtime_hours,
      cycle_count, condition) via `POST $API/v1/maintenance/schedules`.
- [ ] Work order lifecycle: create via `POST /v1/maintenance/work-orders`,
      drive scheduled → in_progress → completed via PATCH/complete endpoints;
      confirm an overdue schedule flips a work order to `overdue`.
- [ ] `GET $API/v1/machines/:id/maintenance` returns the machine's history.
- [ ] A maintenance event arrives on the real-time notifications channel.

### 2.10 Products & BOM — "Complete | 100%"
- [ ] `POST $API/v1/products` with SKU, version, category; `GET /v1/products/:id`
      returns it.
- [ ] `POST /v1/products/:id/bom/items` builds a one-level BOM;
      `GET /v1/products/:id/bom` returns it; `DELETE .../bom/items/:itemId`
      removes an item.

### 2.11 Product Genealogy — "Complete | 100%"
- [ ] `POST $API/v1/genealogy` creates a record linking
      product → order → task → machine → quality → certificate;
      `GET /v1/genealogy/:id/tree` returns the full chain.
- [ ] `POST /v1/genealogy/:id/seal` seals the record; verify the SHA-256
      seal, and confirm a subsequent mutation attempt is rejected
      (tamper-proof claim).
- [ ] Genealogy timeline renders in the UI.

### 2.12 Work Instructions — "Complete | 100%"
- [ ] `POST $API/v1/work-instructions` with multiple steps; `GET` returns it.
- [ ] Auto-attachment: queue a task for a product with an instruction and
      confirm `GET /v1/tasks/:id/work-instructions` shows it attached
      without manual action.
- [ ] `POST /v1/tasks/:id/work-instructions/:wiId/acknowledge` records
      per-step operator acknowledgement.

### 2.13 Inventory Management — "Complete | 100%"
- [ ] `POST $API/v1/inventory` creates an item with quantity;
      `POST /v1/inventory/:id/adjust` changes stock and logs a transaction.
- [ ] Set a reorder point above current stock and confirm the item appears
      in `GET /v1/inventory/low-stock` (and a low-stock alert fires).
- [ ] `POST $API/v1/webhooks/forgesight` with a test payload syncs external
      inventory.

---

## 3. Success-metric claims (ROADMAP "Success Metrics" table)

- [ ] Test coverage ">80%": run `make test-coverage` and record the actual
      figure from `coverage.out`.
- [ ] p95 latency <200ms and real-time latency <300ms: capture from
      Prometheus histograms under representative load, not from a single
      request.
- [ ] 1000+ concurrent WebSocket connections: load-test the gateway
      (e.g. a Centrifugo bench client) and record the result.
- [ ] ML model accuracy >90%: locate the evaluation dataset and rerun the
      evaluation; record where the number comes from.

---

*Each unchecked box is an unverified claim. When you verify one, check the
box and append the date, environment (local / staging / production), and any
deviation observed. If a step fails, do not uncheck-and-forget: file an issue
and correct the corresponding ROADMAP/README claim.*
