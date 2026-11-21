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
The event history is served as ruled (every event type).

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

A caller that sends `status` and `position`, as every caller did before, is
unaffected. The answer is `{"message", "status", "kanban_position",
"previous_status"}`. No transition table: which moves a terminal offers is
the terminal's business. Pravara's clock dates the move (ruling PV-5).

## Not built yet

- The operator's token as a third class of caller (RFC 0064 §9 item 2): it
  waits for Janua's shared-device token class. Until it exists Pravara
  refuses that token.
- The audit row naming the operator, the device and the session for each
  write from a terminal (§9 item 5).
- Late delivery of an act recorded while its operator's token expired
  (ruling PV-2, second step).
