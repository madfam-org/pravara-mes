# Shop-floor terminal

> Public document. Placeholders only: no hostnames, credentials or
> environment-specific values belong here.

What a shop-floor terminal (a handheld an operator carries on the floor) may
read from and write to `pravara-api`, and under whose authority. Design:
RFC 0064 (tracked privately); rulings PV-1, PV-3, PV-4, PV-5 and PV-10.
The wire contract the terminal's software is written against is
[`docs/contracts/terminal.v1.json`](../contracts/terminal.v1.json); its drift
guard is `apps/pravara-api/internal/api/terminal_contract_test.go`.

## Two kinds of authority

| Token | Who | What it opens here |
|---|---|---|
| The terminal's device token: Janua `client_credentials`, `client_kind: "device"`, audience `pravara-api`, scope `pravara-mes:terminal` | The device | The reads below. Never a write |
| An operator's token | A person | The operator's acts (task moves and the rest). Not accepted from a terminal yet: see «Not built yet» |

**An operator's act always carries a person's authority (ruling PV-1).** No
machine scope, `pravara-mes:terminal` included, opens a task move, a
consumption or scrap report, a correction, an inspection or a lot write.
`TestTerminalScopeOpensNoWrite` holds this.

## The terminal's reads (`pravara-mes:terminal`)

Exactly these routes, and no other (`TerminalReadRoutes` in
`internal/api/route_scopes.go`; `TestTerminalScopeOpensExactlyTheTerminalReads`
sends a terminal-only caller to every registered route):

| Route | Notes |
|---|---|
| `GET /v1/orders`, `GET /v1/orders/:id`, `GET /v1/orders/:id/items` | Also open to `pravara-mes:read` and `pravara-mes:jobs`, as before. A terminal-only caller gets the **narrow view** below |
| `GET /v1/tasks`, `GET /v1/tasks/board`, `GET /v1/tasks/:id` | Terminal only: `pravara-mes:read` does not open them |
| `GET /v1/tasks/:id/work-instructions` | Terminal only. The instructions attached to a task and which steps were acknowledged |
| `GET /v1/quality/batches/lookup?lot_number=` | Terminal only. One lot by its exact number, with its stock item's SKU and registry material code when it is linked; 404 `lot_not_found` |
| `GET /v1/events`, `GET /v1/events/types`, `GET /v1/events/:id` | Also open to `pravara-mes:read`. The terminal follows changes from the history (`since`, `types`), not from the live stream |

**The terminal's order view** (owner ruling 2026-11-20: "Narrow the
terminal's order view, no customer fields"). A caller holding
`pravara-mes:terminal` and neither `pravara-mes:read` nor `pravara-mes:jobs`
gets `TerminalOrder` and `TerminalOrderItem` (`internal/api/terminal_view.go`):
an order's id, external id, status, priority, due date and times; an item's
id, product name and SKU, quantity and specifications. No customer name or
email, shipping address, engagement slug, amount, currency or metadata; no
unit price or CAD file URL. Every other caller gets the full order as before.

The same caller's **task reads** (`GET /v1/tasks`, `/v1/tasks/board`,
`/v1/tasks/:id`) carry only these metadata keys, the ones order
decomposition writes to describe the work: `source`, `product_name`,
`product_sku`, `quantity`, `specifications`, `assignment_basis`,
`assigned_machine_code`. Any other key, the CAD file URL included, is left
out (`terminalTaskMetadataKeys`).

The same caller's **event history** keeps every event type (ruling PV-10),
with the customer's name removed (owner ruling 2026-11-22: "Yes, remove
them"). Order events are narrowed by allow-list (`terminalEventRules` in
`internal/api/terminal_view.go`), so the customer's name (`data.name` of an
order event, `data.customer_name` of `order.status_changed`) and any key not
listed never reach a terminal:

| Event type | `data` keys a terminal gets |
|---|---|
| `order.created` | `entity_id`, `entity_type`, `created_by`, `created_at`, `metadata` (`external_id`, `item_count`, `source` only) |
| `order.updated` | `entity_id`, `entity_type`, `changed_fields`, `updated_by`, `updated_at`, `metadata` (as above) |
| `order.deleted` | `entity_id`, `entity_type`, `deleted_by`, `deleted_at` |
| `order.status_changed` | `order_id`, `order_external_id`, `old_status`, `new_status`, `updated_at` |
| any other `order.*` type | none: `data` is `{}` |
| `machine.command_sent` | every field, with `parameters` narrowed to the task metadata keys above (no file ids or names, no free-form parameters) |

No other event type carries customer data: task, machine, telemetry,
notification, maintenance, quality, inventory and genealogy events name
tasks, machines, items and people's ids, not customers, and pass unchanged.

`GET /v1/events` takes `limit` 1-1000 (default 50) and `offset` from 0, for
every caller; anything else is 400 `invalid_limit` / `invalid_offset`.

`pravara-mes:terminal` is not an API-key scope: it reaches a terminal only
on its Janua device token, and an API-key row that carries it anyway gains
nothing from it. People are unaffected (their roles govern them),
and so are API keys holding the wildcard scope.

## The task move (`POST /v1/tasks/:id/move`)

A person's write (no machine scope opens it). Body:

| Field | | |
|---|---|---|
| `status` | required | The status the task enters |
| `from_status` | optional | The status the caller says the task leaves. The task's row is locked and, when it is in any other status, nothing changes and the answer is **409** `{"error": "status_conflict", "current_status": ...}`. A move delivered twice, or after someone moved the task at a desk, is refused instead of applied |
| `position` | optional, at least 1 | The place in the target column. Absent, the task goes to the end of that column (a terminal does not know the board) |

Moves on one tenant's board run one at a time (an advisory lock), and the
task's row is locked `FOR NO KEY UPDATE`. Each of the two waits is bounded
at 5 s, so a move waits at most 10 s in all (5 s for the board, then 5 s for
the row) before the answer is **503** `{"error": "task_board_busy"}` with
`Retry-After: 1`, and nothing changed. Automation (a machine command when a
machine-assigned task enters `in_progress`) still runs inside the move's
request, so the lock is held until the response is written.

A caller that sends `status` and `position`, as every caller did before, is
unaffected. The answer is `{"message", "status", "kanban_position",
"previous_status"}`. No transition table: which moves a terminal offers is
the terminal's business. Pravara's clock dates the move (ruling PV-5).

## `Idempotency-Key` on the terminal's writes

A terminal delivers each record at least once, so its writes honour an
`Idempotency-Key` request header (ruling PV-4) with the semantics receipts
already have for their `idempotency_key` body field. The routes are
`IdempotentWriteRoutes` (`internal/api/route_scopes.go`):
`POST /v1/tasks/:id/move`, `POST /v1/tasks/:id/consumption` and
`POST /v1/tasks/:id/work-instructions/:wiId/acknowledge`.

| Request | Answer |
|---|---|
| No header | The write runs, as it always has |
| First use of a key | The write runs. An answer below 400 is stored with a fingerprint of the method, path, query and canonical JSON body |
| Same key, same request | The stored status, content type and body again, with `Idempotent-Replayed: true`. Nothing runs twice |
| Same key, another request | **409** `idempotency_key_reused` |
| The write answered 400 or more | Nothing is stored; the key stays free and the request may be retried |
| A key that is not 1-100 printable ASCII characters, or two keys | **400** `invalid_idempotency_key` |
| Another request holds the same key for more than 5 s | **503** `idempotency_key_busy` with `Retry-After: 1`; nothing ran |

- **Scope.** A key belongs to the tenant and to the client: the OAuth client
  the token was issued to (a terminal's device), else the caller (a person's
  own subject, an API key). Two clients never share a key; the operators of
  one device share the device's keys.
- **Atomic.** The key is claimed inside the request's transaction before the
  write runs, and its answer stored before that transaction commits; a
  concurrent request with the same key waits, then gets the stored answer.
- **What the key covers.** "Nothing runs twice" is about what the write
  does in Pravara's database transaction. What it does outside it is not
  undone or deduplicated by the key: a realtime publish, an automation
  command written in its own transaction and its append to the dispatch
  stream happen once per run of the write, and a run whose transaction then
  fails (a commit error, a 5xx after them) may already have done them.
- **Retention.** `PRAVARA_IDEMPOTENCY_RETENTION_DAYS` (default **35**, a
  placeholder spanning a month end until the terminal's outbox limits are
  measured). An older key counts as unused; expired rows are deleted by
  later requests of the same tenant.
- **Retry rule for the terminal.** A 5xx, a 429 or no answer: deliver again
  with the same key. Any other 4xx is final: the terminal keeps the record
  for a person.
- **Table.** `idempotency_keys`, migration 037 (tenant-scoped, forced
  row-level security). Apply it before the build that embeds it rolls out
  (`docs/operations/schema-guard.md`).

## Not built yet

- The operator's token as a third class of caller (RFC 0064 §9 item 2): it
  waits for Janua's shared-device token class. Until it exists Pravara
  refuses that token.
- The audit row naming the operator, the device and the session for each
  write from a terminal (§9 item 5).
- Late delivery of an act recorded while its operator's token expired
  (ruling PV-2, second step).
- The record's sequence number kept beside its key, which comes with late
  delivery.
