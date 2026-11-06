# Native MCP (cost ledger reads)

`POST /mcp` on the pravara-api host is a native
[Model Context Protocol](https://modelcontextprotocol.io) endpoint. Agents and
agent fleets use it to read what the cost ledger needs from Pravara. It
serves **quantities only, never money**: the cost ledger values consumption.

It is **off by default**. With `PRAVARA_MCP_ENABLED` unset or `false`, the
route is not registered and answers 404. The owner turns it on (see
«Enabling it»); agents never do.

Code: `apps/pravara-api/internal/mcp` (protocol, gates, audit line) and
`apps/pravara-api/internal/api/mcp_tools.go`, `mcp_routes.go` (tools,
mounting, coverage contract).

## Transport

- Streamable HTTP, JSON-RPC 2.0. MCP revisions `2025-11-25` (preferred),
  `2025-06-18` and `2025-03-26`.
- Stateless: no `Mcp-Session-Id`, no server-sent streams. Every response is
  one `application/json` body. `GET` and `DELETE /mcp` answer 405.
- Methods: `initialize`, `ping`, `tools/list`, `tools/call`. Notifications
  answer 202. Batches are refused (400).
- Request bodies are `application/json` (415 otherwise) and at most 64 KiB
  (413).

## Who may call it

Only **Janua machine tokens** (`client_credentials`). The token is verified by
the same middleware as the HTTP API: issuer, JWKS signature, expiry, audience
`pravara-api` (`OIDC_AUDIENCE`), and the `tenant_id` claim. Every tool runs
in that tenant's row-level-security transaction, exactly like an HTTP
request. A machine client must therefore be **org-bound**: Janua sets
`tenant_id` only on an org-bound client.

The gates, in order:

| Condition | Answer |
| --- | --- |
| `Origin` header present (browser) | 403 |
| No token, invalid token, wrong audience, expired | 401, `WWW-Authenticate: Bearer realm="pravara-mcp"` |
| No `tenant_id` claim | 403 |
| A person's token, or an API key | 403 or 401 (people come in Phase B; API keys are not Janua tokens) |
| Machine token without `pravara-cogs:read` | 403 `insufficient_scope` |
| Over the client's rate | 429 with `Retry-After` |

Scope: **`pravara-cogs:read`** (RFC 0051 M2). The same scope also reads
`GET /v1/jobs/consumption` over HTTP, so a cost-ledger client sees the same
rows (and engagement slugs) on both surfaces. The `pravara-mes:*` scopes do
not open the MCP.

## Tools

Both tools are read-only (`readOnlyHint: true`). There are no write tools.

| Tool | Arguments | Returns | Same read as |
| --- | --- | --- | --- |
| `mes.job_consumption` | `from`, `to` (RFC 3339, required; `from` inclusive, `to` exclusive), `task_id?`, `order_id?`, `engagement_slug?`, `limit?` (1–1000, default 100), `offset?` | `{data, total, limit, offset}`: per-job material (g, mm, ml, sheet) and machine time (min), with basis, source and the order's engagement slug | `GET /v1/jobs/consumption` (same filter parser and repository) |
| `mes.inventory_levels` | `material_code?` (exact SKU), `limit?` (1–1000, default 100), `offset?` | `{data, total, limit, offset}`: SKU, name, category, unit, on hand, reserved, available, reorder point | `GET /v1/inventory` (same repository; `material_code` is the search narrowed to the exact SKU) |

What the tools leave out:
- `mes.job_consumption` omits `recorded_by` (the id of the person who
  reported an actual). Engagement slugs are returned only to callers whose
  scopes read them over HTTP (the `GET /v1/jobs/consumption` scope
  requirement decides).
- `mes.inventory_levels` omits `unit_cost`, `currency`, `forgesight_id`,
  `description` and `metadata`.

An argument the caller can fix (bad timestamp, unknown argument, limit out
of range) comes back as a tool result with `isError: true`. An unknown tool
is JSON-RPC `-32602`. A server failure is HTTP 500 with `-32603` and no
detail; the request transaction is rolled back.

## Coverage contract

`MCPCoverage()` in `mcp_routes.go` classifies every route in the cost-ledger
families (`/v1/jobs`, `/v1/inventory`, `/v1/tasks/:id/consumption`,
`/v1/match`, `/v1/dispatches`, `/v1/machines/:id/command`,
`/v1/webhooks/forgesight`) as COVERED by a tool or EXEMPTED with a reason:

| Route | Status |
| --- | --- |
| `GET /v1/jobs/consumption` | COVERED by `mes.job_consumption` |
| `GET /v1/inventory`, `GET /v1/inventory/:id` | COVERED by `mes.inventory_levels` |
| `GET /v1/inventory/low-stock` | EXEMPTED: reorder alerting, not a cost input |
| `POST /v1/inventory`, `PATCH /v1/inventory/:id` | EXEMPTED: writes, people only |
| `POST /v1/inventory/:id/adjust` | EXEMPTED: manual adjust moves stock |
| `POST /v1/tasks/:id/consumption` | EXEMPTED: an operator actual moves stock |
| `POST /v1/match`, `GET /v1/dispatches`, `GET /v1/dispatches/:id` | EXEMPTED: dispatch planning and control plane |
| `POST /v1/dispatches` | EXEMPTED: ends in `start_job` on a printer |
| `POST /v1/machines/:id/command` | EXEMPTED: machine control |
| `POST /v1/webhooks/forgesight` | EXEMPTED: inbound webhook |

The drift guard (`mcp_coverage_test.go`) fails CI when:
- a route in those families is added without an entry;
- an entry matches no route;
- a tool covers no route, or a COVERED route is not a machine-readable GET;
- `mes.job_consumption`'s arguments differ from the published OpenAPI
  parameters of `GET /jobs/consumption` (`docs/swagger.json`);
- a tool returns a field its HTTP route does not, or returns an excluded
  field.

## Audit and limits

- One structured log line per request that reaches the MCP handler, message
  `mcp.call`, with `mcp_client_id` (machine clients only), `tenant_id`,
  `rpc_method`, `tool`, `outcome`, `latency_ms`, `http_status` and
  `args_sha256`. That last field is the SHA-256 of the arguments' canonical
  JSON. The arguments themselves are never logged.
- Metrics: `pravara_api_mcp_calls_total{method,tool,outcome}` and
  `pravara_api_mcp_call_duration_seconds{method,tool}`, beside the API's HTTP
  metrics.
- Rate limit per Janua client (token bucket): `PRAVARA_MCP_RATE_PER_MINUTE`
  (default 60) and `PRAVARA_MCP_BURST` (default 10). The API's per-address
  and per-tenant limits still apply.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `PRAVARA_MCP_ENABLED` | `false` | Mounts `POST /mcp`. Off: 404. |
| `PRAVARA_MCP_RATE_PER_MINUTE` | `60` | Sustained calls per minute per client. |
| `PRAVARA_MCP_BURST` | `10` | Calls a client may make at once. |

## Enabling it (owner steps)

All of these belong to the owner or the platform; no agent runs them.

1. **Reserve the scope in Janua.** Add `pravara-cogs:read` to Janua's
   reserved Pravara machine scopes, next to `pravara-mes:read`, so that only
   a platform admin can grant it.
2. **Provision one client per consumer** (RFC 0051 M3: Selva's agent fleet,
   the ops agents). Declare each in enclii
   `config/ecosystem-oidc-provision.yaml`, as Pravara's existing inbound
   machine edges are declared: `client_credentials` only, confidential,
   audience `pravara-api`, `allowed_scopes: [pravara-cogs:read]`, org-bound
   to the tenant's Janua organization. `enclii secrets intake` delivers it to
   the consumer's namespace.
3. **Turn the switch on.** Set `PRAVARA_MCP_ENABLED=true` in pravara-api's
   environment, either the `pravara-config` ConfigMap or the Enclii-managed
   equivalent. Merging that change is a deploy.
4. **Smoke test with an agent.** Call `initialize`, then `tools/list` (two
   tools), then `mes.job_consumption` for a closed month. Then check the
   `mcp.call` lines and `pravara_api_mcp_calls_total`.

To turn it off, unset the variable (or set it to `false`). The route then
answers 404 again.

Follow-ups (Phase B for people through Claude connectors, the ecosystem
registry listing) are in [ROADMAP.md](../../ROADMAP.md#pending-work-and-roadmap-ahead),
item 23.
