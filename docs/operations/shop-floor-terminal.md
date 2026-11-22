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
them"): `data.name` of `order.created`, `order.updated` and `order.deleted`
(the order's customer name) and `data.customer_name` of
`order.status_changed`. No other event type carries customer data: task,
machine, telemetry, notification, maintenance, quality, inventory and
genealogy events name tasks, machines, items and people's ids, not
customers (`terminalEventRedactions`).

`GET /v1/events` takes `limit` 1-1000 (default 50) and `offset` from 0, for
every caller; anything else is 400 `invalid_limit` / `invalid_offset`.

`pravara-mes:terminal` is not an API-key scope: it reaches a terminal only
on its Janua device token, and an API-key row that carries it anyway gains
nothing from it. People are unaffected (their roles govern them),
and so are API keys holding the wildcard scope.

## Not built yet

- The operator's token as a third class of caller (RFC 0064 §9 item 2): it
  waits for Janua's shared-device token class. Until it exists Pravara
  refuses that token.
- The audit row naming the operator, the device and the session for each
  write from a terminal (§9 item 5).
- Late delivery of an act recorded while its operator's token expired
  (ruling PV-2, second step).
