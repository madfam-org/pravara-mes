# Inventory: quantities you can trust

RFC 0058 wave 1 (internal RFC; tracked privately): the material code
registry, unit conversions, receipts as a record, batch lots linked to stock,
physical counts, and job consumption that resolves to registry codes.

**Quantities only.** Pravara owns quantity, never money (ruling I1): nothing
in this document reads or writes a cost, price, currency or FX value. Valuing
stock and consumption belongs to the cost ledger (Dhanam); purchase cost
comes from supplier invoices (Karafiel). The pre-existing `unit_cost` and
`currency` columns of `inventory_items` are untouched and unused here.

Schema: migration `035_inventory_registry_receipts_counts` (apply notes
below). Rules for job consumption: [job-consumption.md](./job-consumption.md).

## Rulings this implements

| Ruling | What it means here |
| --- | --- |
| I1 | Quantities only; no money column or payload key anywhere in wave 1 |
| I3 | Pravara owns the material code registry (`material_registry`); the cost ledger, quoting and benchmarks reference its codes |
| I4 | Only registered materials are stock: receipts and counts take a registry entry; a stock item links to one entry |
| I5 | Wave 1 records quantities only; scrap and failed-print disposition is wave 1 PR B (job-consumption.md) |
| I7 | Every receipt says whether it has an invoice: a CFDI UUID and line position, or an explicit `no_invoice`. One count can be the opening count |
| I8 | Count differences are their own `adjustment` movement (`reference_type = 'inventory_count'`), never folded into a job |

## Model

- **Material registry** (`material_registry`): `code` (1–64 letters, digits,
  `.`, `_`, `-`, starting with a letter or digit; unique per tenant ignoring
  case; never changes), `name`, `category` (`filament`, `resin`, `sheet`,
  `spare_part`, `consumable`, `packaging`, `other`), `base_unit`, `active`.
- **Stock item link**: `inventory_items.material_id`, one item per material.
- **Aliases** (`material_aliases`): other spellings a person attaches to a
  material (a print profile's material type such as `PLA`, a lot's product
  code). They change how consumption resolves from then on; nothing already
  recorded is re-settled.
- **Unit conversions** (`material_unit_conversions`): `1 from_unit = factor
  to_unit` per material (1 `spool` = 1000 `g`). Built in, for every material:
  same unit, kg↔g, l↔ml, m↔mm, cm↔mm. One factor per unit pair in either
  direction, enforced by a unique index (an inverse pair cannot be stored,
  so two factors can never disagree); conversions never change.
- **Conversion gaps** (`inventory_conversion_gaps`): a job consumption,
  receipt or count line whose unit has no conversion to its stock item's unit.
  It is recorded and **moves no stock** ("needs conversion"). Adding the
  conversion posts it in the same transaction.
- **Receipts** (`inventory_receipts`, append-only): material, quantity, unit,
  `received_at`, `received_by`, optional batch lot, invoice evidence, status
  `posted` or `needs_conversion`. Posting writes a `receipt` movement with
  `reference_type = 'inventory_receipt'` and the receipt as `reference_id`.
- **Batch lots** gain `inventory_item_id` (the stock item the lot belongs to).
- **Counts** (`inventory_counts`, `inventory_count_lines`): one line per
  material with the counted quantity, its unit and `counted_at`. Closing
  compares each line with the stock on hand **at `counted_at`** (today's on
  hand minus movements recorded after it) and posts the difference.

### How a consumption code resolves

A job consumption `material_code` resolves to a registry entry, in order: the
code itself, an alias, the SKU of a stock item linked to the registry. When
it resolves to nothing, an unlinked stock item whose SKU is the code is still
relieved (stock recorded before the registry), with only built-in
conversions. Resolution happens at read time (`registry_code` on
`GET /v1/jobs/consumption`), so aliasing a code counts every past row too.

For each job and stock item, the stock relieved equals the job's effective
quantity (latest actual, else latest estimate) across every code and unit
naming that item, converted to the item's unit, **taken from that item
alone**: when a code named another item when the job was last settled (a
material re-linked to another item, an item unlinked), that relief is
returned to the old item first, so a job is never relieved twice. A job
whose effective quantity cannot be converted relieves nothing more on its
item and opens a gap. A code that names a registered material with no stock
item relieves nothing (and returns any relief it took before).

A code or alias that is the SKU of an **unlinked** stock item is refused
(409 `material_code_taken`, `alias_taken`): consumption under that SKU
relieves that item today, and the new spelling would silently re-point it.
Register the code with `inventory_item_id` set to that item to register and
link it in one transaction.

### Known behaviour: unlinking returns old relief

Unlinking a stock item from its material (`PATCH /v1/inventory/{id}` with
`material_id: ""`), or linking the material to another item, changes the
item a code names. The next time any old job of that material is settled
(a new fact, a correction, a conversion arriving), its relief is returned
to the old item and taken from the new one, or, when the material has no
item any more, only returned: the old item's stock rises although the
material was consumed from it. This is deliberate for now (relief lives
where the code points when last settled) and is an open question for the
owner (ROADMAP item 22).

## HTTP contract

All routes are under `/v1`, need a signed-in person (Janua) or an API key,
and run in the caller's tenant. Errors are `{"error": "<code>", "message":
"<text>"}`. A request that fails (4xx/5xx) changes nothing. Lists are
`{"data": [...], "total": n, "limit": n, "offset": n}` with `limit` 1–1000
(default 100) and `offset` ≥ 0 (`invalid_limit`, `invalid_offset`).
Timestamps are RFC 3339. Quantities are numbers with up to 4 decimals.

Machine callers (Janua `client_credentials` tokens, scoped API keys) may only
read: `pravara-mes:read` for every GET below; the registry GETs also accept
`pravara-cogs:read`; coverage accepts `pravara-mes:read`, `pravara-mes:jobs`
or `pravara-cogs:read`.

**Who may write (owner ruling, 2026-11-14).** Every write in this document
(the routes marked **role** below) needs a person signed in through SSO
holding the `admin` or `inventory_manager` role:

- a machine token or any API key (wildcard included): 403
  `{"error": "forbidden", "code": "human_only", …}`;
- a person without either role: 403 `{"error": "forbidden", "message":
  "Insufficient permissions"}`.

`inventory_manager` is not issued by the identity provider yet, so **today
only admins pass**. Linking or unlinking a stock item to a material on the
pre-existing item routes (`material_id` on `POST /v1/inventory` and
`PATCH /v1/inventory/{id}`) needs the same role (403 `forbidden`); the rest
of those routes is unchanged. Reads stay open to every signed-in tenant
user, and so do the shop-floor writes in [job-consumption.md](./job-consumption.md)
(operator reports, corrections, print-job completion).

### Material registry

| Method | Route | Body / query | 2xx | Errors |
| --- | --- | --- | --- | --- |
| GET | `/inventory/materials` | `active` (true/false), `category`, `search` (code or name), `limit`, `offset` | 200 list of Material | 400 `invalid_active`, `invalid_category` |
| POST (**role**) | `/inventory/materials` | `{code, name, category, base_unit, active?, notes?, inventory_item_id?}` | 201 Material | 400 `validation_error`; 409 `material_code_taken` (a code, alias or linked SKU already, ignoring case for codes; or an unlinked item's SKU without that item as `inventory_item_id`), `item_not_linkable` (`inventory_item_id` not found or already linked), `material_already_stocked` |
| GET | `/inventory/materials/unlinked` | — | 200 `{items, lots, consumption_codes}` | — |
| GET | `/inventory/materials/{id}` | — | 200 Material + `aliases`, `conversions` | 400 `invalid_id`; 404 `material_not_found` |
| PATCH (**role**) | `/inventory/materials/{id}` | `{name?, category?, base_unit?, active?, notes?}` (`notes: ""` clears) | 200 Material | 400 `validation_error`; 404 `material_not_found` |
| POST (**role**) | `/inventory/materials/{id}/aliases` | `{alias}` (1–100 characters) | 201 Alias | 400 `validation_error`; 404 `material_not_found`; 409 `alias_taken` (a code, alias, linked SKU, or an unlinked item's SKU) |
| POST (**role**) | `/inventory/materials/{id}/conversions` | `{from_unit, to_unit, factor, note?}` | 201 `{conversion, gaps_resolved, stock_movements}` | 400 `validation_error`; 404 `material_not_found`; 409 `conversion_exists`, `conversion_builtin` |
| GET | `/inventory/conversion-gaps` | `status` (`open` default, `resolved`, `superseded`, `all`), `limit`, `offset` | 200 list of Gap | 400 `invalid_status` |

Linking a stock item: `POST /v1/inventory` and `PATCH /v1/inventory/{id}`
take `material_id` (PATCH: `""` unlinks; absent leaves it). 404
`material_not_found`, 409 `material_already_stocked` (the material already
has an item). Item reads return `material_id` and `material_code`.

Units are 1–16 lowercase letters, digits or `_`, starting with a letter
(`g`, `kg`, `ml`, `mm`, `sheet`, `spool`, `pcs`); input is lowercased and
trimmed.

**Material**

```json
{"id": "uuid", "code": "FIL-PLA-BLK", "name": "PLA black 1 kg", "category": "filament",
 "base_unit": "g", "active": true, "notes": "optional",
 "inventory_item_id": "uuid", "stock_sku": "FIL-PLA-BLK", "stock_unit": "g", "quantity_on_hand": 2976,
 "created_by": "uuid", "created_at": "…", "updated_at": "…"}
```

The `inventory_item_id`, `stock_sku`, `stock_unit` and `quantity_on_hand`
fields are omitted until a stock item is linked.

**Alias** `{id, material_id, alias, created_by?, created_at}`.
**Conversion** `{id, material_id, from_unit, to_unit, factor, note?, created_by?, created_at}`.

**Unlinked**

```json
{"items": [{"id": "uuid", "sku": "bad sku", "name": "…", "unit": "pcs", "reason": "sku_is_not_a_valid_code"}],
 "lots": [{"id": "uuid", "lot_number": "LOT-2", "product_code": "NOPE", "product_name": "…"}],
 "consumption_codes": [{"material_code": "PLA", "rows": 12, "units": ["g", "mm"], "last_occurred_at": "…"}]}
```

`reason` is one of `sku_is_not_a_valid_code`, `unit_is_not_a_valid_unit`,
`sku_differs_from_another_only_in_case`, `code_registered_for_another_material`,
`not_linked`. At most 500 of each list.

**Gap**

```json
{"id": "uuid", "inventory_item_id": "uuid", "sku": "SHT-ACR-3MM", "material_id": "uuid",
 "material_code": "SHT-ACR-3MM", "source_type": "job_consumption", "source_id": "uuid",
 "job_kind": "task", "job_id": "uuid", "quantity": 2, "from_unit": "sheet", "to_unit": "pcs",
 "status": "open", "resolved_at": null, "created_at": "…"}
```

`source_type` is `job_consumption` (source_id: the effective
`job_consumption` row; `job_kind`, `job_id` set), `inventory_receipt` or
`inventory_count_line`. A newer fact of the same job supersedes an open gap
(`superseded`).

### Receipts

| Method | Route | Body / query | 2xx | Errors |
| --- | --- | --- | --- | --- |
| POST (**role**) | `/inventory/receipts` | see below | 201 `{receipt, stock_movement, replayed: false}`; 200 `{receipt, stock_movement: null, replayed: true}` for a retry (same `idempotency_key`, same request) | 400 `validation_error` (including a missing `idempotency_key`); 404 `material_not_found`, `batch_lot_not_found`; 409 `idempotency_key_reused` (same key, different request), `material_inactive`, `material_not_stocked`, `lot_belongs_to_other_item` |
| GET | `/inventory/receipts` | `material_id`, `invoice_status`, `status`, `from`, `to` (on `received_at`), `limit`, `offset` | 200 list of Receipt, newest received first | 400 `invalid_material_id`, `invalid_invoice_status`, `invalid_status`, `invalid_from`, `invalid_to` |
| GET | `/inventory/receipts/{id}` | — | 200 Receipt | 400 `invalid_id`; 404 `receipt_not_found` |

Request:

```json
{"material_id": "uuid", "quantity": 2, "unit": "kg",
 "received_at": "2026-11-14T10:00:00-06:00",
 "invoice_status": "invoiced", "cfdi_uuid": "uuid", "cfdi_line": 3,
 "lot_number": "LOT-0001", "notes": "optional", "idempotency_key": "required, 1-100 characters"}
```

- `invoice_status` is required: `invoiced` needs `cfdi_uuid` and `cfdi_line`
  (≥ 1, the line's position in the CFDI); `no_invoice` takes neither.
- `idempotency_key` is **required**. A retry with the same key and the same
  request (material, quantity, unit, `received_at` as sent or absent, lot,
  invoice evidence, notes) returns the first receipt (200, `replayed: true`);
  the same key with any other request is 409 `idempotency_key_reused`.
  Generate one key per receipt the person submits.
- `received_at` defaults to now and may not be in the future.
- The lot: `batch_lot_id` (an existing lot; linked to this material's stock
  item when it has none) or `lot_number` (found by number, else created for
  this stock item), not both.
- The material must be active and have a stock item.

Receipt:

```json
{"id": "uuid", "material_id": "uuid", "material_code": "FIL-PLA-BLK", "inventory_item_id": "uuid",
 "quantity": 2, "unit": "kg", "received_at": "…", "received_by": "uuid",
 "batch_lot_id": "uuid", "lot_number": "LOT-0001",
 "invoice_status": "invoiced", "cfdi_uuid": "uuid", "cfdi_line": 3,
 "status": "posted", "stock_quantity": 2000, "transaction_id": "uuid", "posted_at": "…",
 "notes": "…", "idempotency_key": "…", "created_at": "…"}
```

`status: needs_conversion` has no `stock_quantity`, `transaction_id` or
`posted_at`, and moved no stock; it is listed as a gap until the material
gets a conversion. `stock_movement` is the `inventory_transactions` row
(`transaction_type: receipt`, `reference_type: inventory_receipt`) or null.

### Physical counts

| Method | Route | Body / query | 2xx | Errors |
| --- | --- | --- | --- | --- |
| POST (**role**) | `/inventory/counts` | `{is_opening?, notes?}` (body optional) | 201 Count | 409 `opening_count_exists` |
| GET | `/inventory/counts` | `status` (`open`, `closed`), `limit`, `offset` | 200 list of Count (no lines) | 400 `invalid_status` |
| GET | `/inventory/counts/{id}` | — | 200 Count with `lines` | 400 `invalid_id`; 404 `count_not_found` |
| POST (**role**) | `/inventory/counts/{id}/lines` | `{material_id, counted_quantity, unit, counted_at?}` | 201 Line | 400 `validation_error`; 404 `count_not_found`, `material_not_found`; 409 `count_closed` |
| POST (**role**) | `/inventory/counts/{id}/close` | — | 200 `{count, already_closed}` | 404 `count_not_found`; 409 `newer_count_exists` `{error, message, materials: [{material_id, material_code, newer_count_id}]}` |

- One line per material: posting the same material again replaces it while
  the count is open. `counted_at` defaults to now, may not be in the future.
- Closing sets each line's `result`:
  - `matched`: counted equals expected; no movement.
  - `adjusted`: an `adjustment` of `counted − expected` (in the item's unit),
    `reference_type = 'inventory_count'`, `reference_id` = the line.
  - `needs_conversion`: no conversion to the item's unit; no movement; a gap
    is listed and applied when the conversion is added.
  - `not_stocked`: the material has no stock item.
  - `superseded`: the line waited for a conversion and, before it arrived,
    a newer count of the material closed; it never moves stock.
- **Closing is idempotent**: closing a closed count returns it with
  `already_closed: true` and moves nothing.
- **Which count wins: the one counted last.** The expected quantity of a
  line is the stock on hand at its `counted_at`, **plus** every count
  adjustment posted after `counted_at` by a line counted at or before it
  (an earlier count that closed late corrected a moment this count already
  saw). When another count already closed with a line for the same material
  counted **after** this line, closing is 409 `newer_count_exists` (applying
  the older difference on top would count it twice); recount the material in
  a new count instead. Applying a line once its conversion arrives follows
  the same rule (a line overtaken by a later count becomes `superseded`).
- Each applied line reports `movements_since_counted` and
  `quantity_since_counted`: the stock movements recorded between
  `counted_at` and the close (a receipt that may already have been on the
  shelf the count saw). They are not part of the difference.

Count `{id, status, is_opening, notes?, created_by?, created_at, closed_by?,
closed_at?, line_count, lines?}`. Line `{id, count_id, material_id,
material_code, counted_quantity, unit, counted_at, counted_by?, result?,
inventory_item_id?, expected_quantity?, counted_stock_quantity?, difference?,
transaction_id?, movements_since_counted?, quantity_since_counted?, updated_at}`.

### Consumption coverage

`GET /v1/jobs/consumption/coverage?from=&to=` (on `occurred_at`; 400
`invalid_from`, `invalid_to`):

```json
{"material_rows": 120, "with_registry_code": 118, "without_registry_code": 2,
 "unresolved_codes": [{"material_code": "PLA", "rows": 2, "units": ["g"], "last_occurred_at": "…"}],
 "from": "…", "to": "…"}
```

The wave 1 gate is `without_registry_code = 0`. Fix each unresolved code by
registering it, aliasing it, or linking the stock item whose SKU it is.
`GET /v1/jobs/consumption` rows carry `registry_code` (omitted when the row's
code is unregistered).

## Migration 035: apply notes (owner)

Production migrations are applied by hand; the deploy does not run them.

1. Apply after 034 (it only needs 028/029/033), before deploying the code of
   this wave: the item, consumption and cost-event reads select the new
   column and function.
2. `infra/db/migrate.sh up` as the schema owner. The migration grants the
   new tables to `pravara_app` when the role exists.
3. The backfill runs per tenant with the tenant set. It registers each item
   whose SKU is a valid code, whose unit is a valid unit and whose SKU is
   unique in its tenant ignoring case, and links it; it links each batch lot
   whose `product_code` is exactly an item's SKU. It bumps `updated_at` of
   the items it links. Then read `GET /v1/inventory/materials/unlinked` and
   `GET /v1/jobs/consumption/coverage` and resolve what is listed.
4. Down (`035_….down.sql`) drops the new tables and columns; movements already
   posted stay in `inventory_transactions` with a `reference_id` that no
   longer resolves, and `quantity_on_hand` is not rewound.

## Not in wave 1

- Valuation, average cost, COGS (Dhanam, wave 2). Proposals of receipts from
  invoice lines (wave 3).
- Voiding or correcting a receipt: a wrong receipt is corrected by a count
  (or the manual adjust endpoint), never by editing the receipt.
- A receipt for the same CFDI line twice is not refused (partial deliveries);
  see ROADMAP item 22.
- Multiple warehouses, purchase orders, per-spool remaining weight.
