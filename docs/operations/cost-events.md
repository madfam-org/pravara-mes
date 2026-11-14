# Job consumption to the cost ledger (cost events)

> Public document. Placeholders only: no hostnames, credentials or
> environment-specific values belong here.

Pravara sends what each completed job consumed to Dhanam, the single writer
of the ecosystem cost ledger (RFC 0050), as **quantity-only**
`cost.event.v1` `consumption` events. Pravara never values consumption:
Dhanam does (owner ruling C12). Until Dhanam values it, the ledger reports it
as a coverage gap (`unvalued_consumption_count`), never as cost.

It is **off by default**. With `PRAVARA_COST_EVENTS_ENABLED` unset or
`false`, nothing is built, stored or sent, and no other setting is required.
The owner turns it on (see «Turning it on»); agents never do.

Code: `apps/pravara-api/internal/costevents` (builder, contract rules,
outbox store, Dhanam client, emitter). Schema: migration
`034_cost_event_outbox`. Source data: `job_consumption`
([job-consumption.md](./job-consumption.md)).

## What is sent

One event per **job and material code** (a "line"), built from all of that
line's material facts:

```json
{
  "schema": "cost.event.v1",
  "id": "pravara.consumption.dispatch.<job uuid>.<16 hex of sha256(material code)>",
  "source": "pravara",
  "kind": "consumption",
  "status": "active",
  "occurred_at": "2026-11-03T18:20:00Z",
  "cost_class": "cogs",
  "basis": "actual",
  "quantity": "6",
  "unit": "g",
  "item": {"material_code": "FIL-PLA-BLK"},
  "attribution": {"engagement": "eng:<orders.engagement_slug>"},
  "evidence": {"job_id": "<job uuid>"}
}
```

| Field | Rule |
| --- | --- |
| `id` | Deterministic: job kind, job id and a hash of the line key. A correction re-sends the same id; Dhanam (idempotent on `(source, id)`) reports it `updated`. When a code is registered or aliased after its line's event was stored, the line moves to the material's key: the sweep re-sends the old id as `cancelled` and the material's line carries the quantity, so nothing is counted twice. |
| `quantity`, `unit`, `basis` | The line's **effective** fact, as stock relief uses it: the latest `actual`, else the latest `estimate`. Dhanam stores `estimate` events but never counts them. |
| `occurred_at` | The job's completion: the earliest fact of the line (an operator's later actual must not move the period). UTC, RFC 3339. |
| `attribution.engagement` | `eng:` + the order's current `engagement_slug`. **Omitted** when the order has none (never guessed). |
| `item.material_code` | The line key: the material registry code the recorded codes resolve to (the code, an alias or a linked SKU; RFC 0058 ruling I3, [inventory.md](./inventory.md)), else the recorded code. **A line is one job and material**: facts recorded under different codes of the same material (a printer actual under a class aliased to the material, an operator actual under its code) are one line and one event, whose quantity is the effective fact, exactly what stock relief settled. No other `item` key is sent. |
| `cost_class` | `PRAVARA_COST_EVENTS_COST_CLASS` (default `cogs`). |
| `status` | `active`; `cancelled` when the line's effective quantity became 0, or its dispatch / print job is no longer `completed` (its completion was withdrawn). A cancellation re-sends the last stored body with `status: cancelled` (Dhanam needs a non-zero quantity), and only for a line stored before; a line never stored is not sent. A task, or a job whose row was deleted, keeps its consumption: what was consumed was consumed. |

Never sent: `amount_minor`, `currency`, `fx`, `unit_value_minor`, `taxes`.
The event type has no such fields, and the outbox table refuses a body that
carries one (CHECK). Machine minutes (`machine_time` rows) are not sent:
quantity-only consumption needs a material code or SKU, and valuing machine
time is not ruled yet.

Each event is checked against the Dhanam contract v1.1 rules before it is
stored (`costevents.Validate`, pinned by tests). One that breaks them (for
example a material code longer than 64 characters) is stored `rejected`,
logged at error level, and never sent.

## Delivery

- **Outbox.** `cost_event_outbox`, one row per event id (tenant-scoped, under
  row-level security). Each sweep rebuilds every line with a fact recorded in
  the last `PRAVARA_COST_EVENTS_LOOKBACK_DAYS` (default 62) and writes a row
  only when its body hash changed (then it becomes `pending` with attempts
  reset). An unchanged line writes and sends nothing.
- **Sending.** Due `pending` rows are leased for 2 minutes (`FOR UPDATE SKIP
  LOCKED`, so replicas do not send the same row together) and posted as
  `{"events": [...]}` in batches of `PRAVARA_COST_EVENTS_BATCH_SIZE` (default
  100, at most 500). A row is settled only for the body hash that was sent:
  a line rebuilt while its POST was in flight stays pending, and is not
  claimed again until that POST is settled (`leased_until`), so two bodies
  of one id are never in flight together.
- **Answers.**
  - 2xx: rows `delivered`, with Dhanam's outcome (`created`, `updated`,
    `unchanged`).
  - 400: the events Dhanam names are `rejected` (error codes only, never
    values); the rest of the batch is due again at once.
  - 401: the token is dropped and one fresh token is tried; a second 401 is
    an alert.
  - 403 (no writer grant for source `pravara`), 409 (an id belongs to another
    space), 429, 5xx, network or Janua unavailable: every row of the batch
    counts an attempt and backs off 30 s, 2 min, 15 min, 1 h, then every 6 h.
    Rows are never dropped. 401, 403 and 409 log `ALERT cost events: …`.
- **Restarts.** Everything is in the table: a restarted or new process
  continues where the last one stopped. A process that died mid-POST leaves
  its rows due when the lease ends; Dhanam's idempotency makes the re-send
  harmless.
- **One tenant.** Dhanam's `CostEventWriterGrant` binds the client to one
  space and one source, so only `PRAVARA_COST_EVENTS_TENANT_ID` is sent.
- **Limits.** A line whose last fact is older than the lookback is not
  rebuilt, so an order's engagement set, or a job reopened, more than 62
  days after its last fact does not reach Dhanam. Lines that completed before
  `PRAVARA_COST_EVENTS_START_DATE` are never sent.
- **Dry run.** `PRAVARA_COST_EVENTS_DRY_RUN=true` builds, validates and
  stores the rows but sends nothing and needs no credentials; each sweep logs
  a summary («dry run — events stored in cost_event_outbox, nothing sent»).
  Turning the dry run off sends what is stored.
- **Observability.** `pravara_api_cost_events_total{result}` (`delivered`,
  `dhanam_rejected`, `contract_rejected`, `retry`) and
  `pravara_api_cost_events_ready` (1 while the emitter runs). A switched-on
  but incomplete configuration logs the missing settings (names only) at
  error level and leaves the emitter, and only it, stopped. Nothing logged
  carries an event body, a token or a secret.

## Settings

| Variable | Default | Meaning |
| --- | --- | --- |
| `PRAVARA_COST_EVENTS_ENABLED` | `false` | The switch. Owner only. |
| `PRAVARA_COST_EVENTS_DRY_RUN` | `false` | Build and store, never send. |
| `PRAVARA_COST_EVENTS_TENANT_ID` | — | The Pravara tenant (UUID) whose consumption is sent. Required. |
| `PRAVARA_COST_EVENTS_START_DATE` | — | First completion date sent, `YYYY-MM-DD` in America/Mexico_City. Required. |
| `PRAVARA_COST_EVENTS_DHANAM_URL` | — | Dhanam API origin; events go to `<origin>/v1/cost-events`. Required unless dry run. |
| `PRAVARA_COST_EVENTS_TOKEN_URL` | `JANUA_TOKEN_URL` | Janua token endpoint. |
| `PRAVARA_COST_EVENTS_CLIENT_ID`, `PRAVARA_COST_EVENTS_CLIENT_SECRET` | — | The Janua client the writer grant binds. Secret: environment only, delivered by Enclii. Required unless dry run. |
| `PRAVARA_COST_EVENTS_COST_CLASS` | `cogs` | `cogs`, `opex`, `capex` or `financing`. |
| `PRAVARA_COST_EVENTS_INTERVAL_SECONDS` | `300` | Sweep period (at least 10). |
| `PRAVARA_COST_EVENTS_LOOKBACK_DAYS` | `62` | Rebuild window. |
| `PRAVARA_COST_EVENTS_BATCH_SIZE` | `100` | Events per POST, 1–500. |

The token is a Janua `client_credentials` token requested with scope
`dhanam-cost-events:write`; Janua binds the audience `dhanam-api` to the
client. It is cached until shortly before it expires.

## Turning it on (owner steps, in order)

1. **Dhanam contract v1.1 is merged and deployed** (quantity-only
   consumption). Before that, Dhanam answers 400 to every event and the rows
   are stored `rejected`.
2. **Janua client.** One confidential client for "pravara writes cost events
   into Dhanam": audience `dhanam-api`, scope `dhanam-cost-events:write`,
   bound to the organization that owns the Dhanam space.
3. **Writer grant.** In Dhanam, a `CostEventWriterGrant` binding that client
   and organization to the ledger space and source `pravara` (Dhanam's
   operator script, dry-run first).
4. **Secret.** The client id and secret reach pravara-api as
   `PRAVARA_COST_EVENTS_CLIENT_ID` / `PRAVARA_COST_EVENTS_CLIENT_SECRET`
   through Enclii's secret intake. Nobody pastes them anywhere else.
5. **Migration 034.** Apply `034_cost_event_outbox.up.sql` as described in
   «Migration 034» below. The deploy does not apply migrations; with the
   switch off the code never touches the table, so deploying first is safe.
6. **Settings.** `PRAVARA_COST_EVENTS_TENANT_ID`, `PRAVARA_COST_EVENTS_START_DATE`,
   `PRAVARA_COST_EVENTS_DHANAM_URL`.
7. **Optional dry run.** `PRAVARA_COST_EVENTS_ENABLED=true` with
   `PRAVARA_COST_EVENTS_DRY_RUN=true`; read the summary line and the
   `cost_event_outbox` rows.
8. **The switch.** `PRAVARA_COST_EVENTS_ENABLED=true` (dry run off). Expect
   `cost events: emitter started`, then `cost events: run` lines with
   `delivered` counts, and Dhanam's `unvalued_consumption_count` to rise.

To stop: set `PRAVARA_COST_EVENTS_ENABLED=false`. Rows stay; turning it on
again continues from them.

## Migration 034

`034_cost_event_outbox` is forward-only and additive: one new table, no
existing row changes. Apply it with the procedure for one migration in the
production migration runbook, `docs/operations/applying-migrations.md`
([#66](https://github.com/madfam-org/pravara-mes/pull/66)): pinned commit,
size and SHA-256 checked inside the database pod, then a guarded record, the
same way as 033. Its
prerequisites are `tenants` and the functions `app_current_tenant_id()` and
`gen_random_uuid()`.

Guarded record after applying it:

```sql
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
                 WHERE n.nspname = 'public' AND c.relname = 'cost_event_outbox'
                   AND c.relrowsecurity AND c.relforcerowsecurity) THEN
    RAISE EXCEPTION '034 effect not present: cost_event_outbox missing or not under FORCE RLS';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public'
                 AND tablename = 'cost_event_outbox' AND policyname = 'tenant_isolation') THEN
    RAISE EXCEPTION '034 effect not present: tenant_isolation policy missing';
  END IF;
  INSERT INTO schema_migrations (version) VALUES ('034_cost_event_outbox');
  RAISE NOTICE 'recorded 034_cost_event_outbox';
END $$;
```

Rollback (switch off first): `034_cost_event_outbox.down.sql` drops the
table. Events Dhanam stored stay in Dhanam; turning the switch on again later
re-sends every line in the lookback, which Dhanam answers `unchanged`.
