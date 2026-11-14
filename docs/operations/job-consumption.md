# Job consumption and engagement attribution

What each completed job consumed, as quantities, and which engagement its
order belongs to. Pravara records **quantities only**: no unit cost, no
money. Valuing consumption (inventory method, cost of goods sold) belongs to
the downstream cost ledger, which reads `GET /v1/jobs/consumption`.

Schema: migration `033_job_consumption_engagement` (apply it before deploying
the code that reads `orders.engagement_slug`).

## Engagement on orders

`orders.engagement_slug` attributes an order to an engagement. Format:
`^[a-z0-9][a-z0-9-]{0,62}$` (lowercase letters, digits and hyphens, 1–63
characters, starting with a letter or digit), enforced by the API (400
`invalid_engagement_slug`) and by a database CHECK.

- `POST /v1/orders`: optional `engagement_slug`.
- `PATCH /v1/orders/:id`: `engagement_slug` sets it, `""` clears it, an absent
  field leaves it unchanged.
- Every order read returns it (omitted when unset).

Consumption rows carry the order's **current** slug when read, so setting the
engagement after a job completed still attributes that job.

## What is recorded

`job_consumption` is append-only. One row is one fact:

| Column | Values |
| --- | --- |
| `job_kind`, `job_id` | `dispatch` (fabrication dispatch), `print_job`, or `task` (operator report on a task without a completed dispatch) |
| `kind` | `material` or `machine_time` |
| `material_code`, `material_lot` | material only (see «Material code») |
| `quantity`, `unit` | material in `g`, `mm`, `ml` or `sheet`; machine time in `min` |
| `basis` | `actual` or `estimate` |
| `source` | `printer`, `operator` or `slicer` |
| `note` | how the quantity was derived |
| `registry_code` (read only) | the material registry code `material_code` resolves to, at read time ([inventory.md](./inventory.md)) |
| `occurred_at` | the job's completion (period cut-off) |
| `disposition` | `good`, `scrap` or `failed_print` (ruling I5; migration 036). Default `good` |
| `supersedes_id` | on a correction: the row it replaces (migration 036) |

A fact is keyed by job, kind, basis, source and material: recording the same
fact again is a no-op, so replays never duplicate.

### Sources per path

| Path | Estimate (`slicer`) | Actual |
| --- | --- | --- |
| Fabrication dispatch completion | fabrication-prep `filament_g` (g) and `print_time_s` (min) | machine time from the `start_job` acknowledgement to the completion report (`printer`). The Sparkplug completion carries no filament used, so actual material comes from an operator report. |
| Print job (`ConsumptionService.RecordPrintJob`) | `estimated_filament_g`, else `estimated_filament_mm`; `estimated_time_seconds` | `actual_filament_mm` (`printer`), converted to g only when the material profile gives diameter **and** density, otherwise kept in mm with the note «kept in mm»; `actual_time_seconds`, else `completed_at − started_at` |
| `POST /v1/tasks/:id/consumption` | — | operator-reported material or minutes (`operator`) |
| `POST /v1/print-jobs/:id/complete` | as print job | as print job, plus the operator's weighed material when sent (`operator`) |
| `POST /v1/jobs/consumption/:id/corrections` | — | an operator correction of any recorded row (`operator`) |

The print-job recorder runs when an operator marks a print job finished
(«Print-job completion» below). Nothing else in pravara-api marks a print
job completed: no machine adapter writes `print_jobs`.

Dispatch completion records consumption inside a savepoint of the completion
transaction. A failure is logged (`dispatch: job consumption not recorded`)
and never blocks the manufacturing record or the passport.

### Material code

Pravara owns the material code registry (RFC 0058 ruling I3;
[inventory.md](./inventory.md)). The code recorded with a fact is still:

- dispatch: the `product_code` of the batch lot named by the machine's
  material lot (`batch_lots.lot_number`), else the material class;
- print job: the material profile's `material_type`;
- operator report: what the operator sends (use the code the dispatch
  recorded, shown by `GET /v1/jobs/consumption`, so the report settles it).

Each recorded code resolves to a registry entry at read time: the code
itself, an alias a person added, or the SKU of a linked stock item.
`GET /v1/jobs/consumption/coverage` counts rows with and without a registry
code and lists the unresolved codes (the wave 1 gate: none left).

## Stock and genealogy

- **Stock relief.** A material code names a stock item when it resolves to a
  registry entry with a linked item, or (not registered) is the SKU of an
  unlinked item. The stock relieved for a job and item always equals the
  job's effective quantity across every code and unit naming that item (the
  latest actual, else the latest estimate), converted to the item's unit:
  same unit, the built-in kg↔g, l↔ml, m↔mm, cm↔mm, or the material's own
  conversion. The first relief is a `consumption` movement; a later actual
  posts the difference as an `adjustment` (for example estimate 5.2 g, then
  operator actual 6 g: −5.2, then −0.8). Movements carry
  `reference_type = 'job_consumption'`, the consumption row as
  `reference_id`, and the running balance. Recording the same fact again
  relieves nothing.
- **No conversion, no guess.** When the effective quantity's unit has no
  conversion to the item's unit (a print job's actual kept in mm for an item
  stocked in g), nothing more is relieved: the job is listed in
  `GET /v1/inventory/conversion-gaps` and settled in the same transaction as
  the conversion that makes it convertible.
- **Genealogy.** When the lot is known and the job has an unsealed genealogy
  record, the material is written to it once
  (`genealogy_material_consumption`, batch lot resolved by lot number).

## Operator report

`POST /v1/tasks/:id/consumption` (people and wildcard API keys only; it moves
stock):

```json
{"material_code": "FIL-PLA-BLK", "material_lot": "LOT-0042", "quantity": 6, "unit": "g", "disposition": "scrap"}
{"quantity": 95, "unit": "min"}
```

It belongs to the task's latest completed dispatch when there is one. The lot
is inherited from earlier facts of the same job and material when omitted.
201 returns `{consumption, stock_movements}`; the same actual for the same job
and material returns 409 `already_recorded` with `correct_with`, the route
that corrects the stored row.

## Corrections

`POST /v1/jobs/consumption/:id/corrections` (people only; it moves stock):

```json
{"quantity": 30, "unit": "g", "disposition": "scrap", "reason": "support material weighed"}
```

- **Append-only.** The correction is a new row of the same job, kind and
  material (`basis: actual`, `source: operator`, `supersedes_id` = the
  corrected row, `occurred_at` = the corrected row's, so the period never
  moves); nothing recorded is updated. `unit` and `disposition` default to
  the corrected row's.
- It becomes the job's effective row: rows a correction superseded never
  count, for stock relief or for cost events. Stock relieved for the job
  moves by the difference (an `adjustment`, for example 25 g corrected to
  30 g: −5).
- A row is corrected once. Correcting it again is 409 `already_superseded`
  with `superseded_by`: correct the latest correction instead.
- 201 `{consumption, superseded, stock_movements}`; 400 `invalid_id`,
  `validation_error`; 404 `consumption_not_found`.

## Print-job completion

`POST /v1/print-jobs/:id/complete` (people only). An operator marks a print
job finished: status `completed`, `progress_percent` 100, `completed_at` now
unless already set. **A record only: nothing is sent to any machine.** Then
its consumption is recorded as in «Sources per path», and, when sent, the
operator's actual material:

```json
{"material_quantity": 9, "material_unit": "g", "material_code": "optional", "disposition": "failed_print"}
```

- Every field is optional. `material_code` defaults to the print profile's
  material type (required with `material_quantity` when the job has none).
  `disposition` applies to the material recorded.
- Calling it again on a completed job records nothing new; a different
  weighed quantity then goes through a correction.
- 200 `{print_job_id, marked_completed, consumption, created,
  stock_movements, material_recorded}`; 404 `print_job_not_found`; 409
  `print_job_closed` (cancelled or failed print jobs); 400
  `validation_error`.
- A print that physically failed is recorded by completing it with
  `disposition: failed_print`: the cost-event emitter cancels the
  consumption of a print job whose status is not `completed`, and a failed
  print's material is COGS of the job (ruling I5).

## Read for cost ledgers

Pravara also **pushes** each job's material consumption to Dhanam as
quantity-only cost events, behind `PRAVARA_COST_EVENTS_ENABLED` (off by
default): [cost-events.md](./cost-events.md).

`GET /v1/jobs/consumption?from=&to=&task_id=&order_id=&engagement_slug=&limit=&offset=`

- `from` inclusive, `to` exclusive, RFC 3339, on `occurred_at`.
- `limit` 1–1000 (default 100).
- Machine callers need `pravara-mes:read`, `pravara-mes:jobs` or
  `pravara-cogs:read` (the cost ledger's scope). The same read is the MCP
  tool `mes_job_consumption` ([native-mcp.md](./native-mcp.md)).
- Response: `{data: [row], total, limit, offset}`; each row has `job_kind`,
  `job_id`, `dispatch_id`, `print_job_id`, `task_id`, `order_id`,
  `engagement_slug`, `machine_id`, `kind`, `material_code`, `material_lot`,
  `quantity`, `unit`, `basis`, `source`, `note`, `occurred_at`, `created_at`,
  `disposition`, `supersedes_id` and `superseded_by` (corrections), and
  `registry_code` (omitted when the code is unregistered).
  Machine minutes are the `machine_time` rows (`unit = min`).
