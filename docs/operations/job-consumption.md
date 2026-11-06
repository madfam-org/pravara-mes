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
| `occurred_at` | the job's completion (period cut-off) |

A fact is keyed by job, kind, basis, source and material: recording the same
fact again is a no-op, so replays never duplicate.

### Sources per path

| Path | Estimate (`slicer`) | Actual |
| --- | --- | --- |
| Fabrication dispatch completion | fabrication-prep `filament_g` (g) and `print_time_s` (min) | machine time from the `start_job` acknowledgement to the completion report (`printer`). The Sparkplug completion carries no filament used, so actual material comes from an operator report. |
| Print job (`ConsumptionService.RecordPrintJob`) | `estimated_filament_g`, else `estimated_filament_mm`; `estimated_time_seconds` | `actual_filament_mm` (`printer`), converted to g only when the material profile gives diameter **and** density, otherwise kept in mm with the note «kept in mm»; `actual_time_seconds`, else `completed_at − started_at` |
| `POST /v1/tasks/:id/consumption` | — | operator-reported material or minutes (`operator`) |

The print-job recorder has no caller yet: no code path in pravara-api marks a
print job completed today.

Dispatch completion records consumption inside a savepoint of the completion
transaction. A failure is logged (`dispatch: job consumption not recorded`)
and never blocks the manufacturing record or the passport.

### Material code

Until an owner names the registry for material codes, the code is:

- dispatch: the `product_code` of the batch lot named by the machine's
  material lot (`batch_lots.lot_number`), else the material class;
- print job: the material profile's `material_type`;
- operator report: what the operator sends (use the code the dispatch
  recorded, shown by `GET /v1/jobs/consumption`, so the report settles it).

## Stock and genealogy

- **Stock relief.** When `material_code` is the SKU of an inventory item in the
  same unit, the stock relieved for a job and material always equals its
  effective quantity: the latest actual, else the latest estimate. The first
  relief is a `consumption` movement; a later actual posts the difference as
  an `adjustment` (for example estimate 5.2 g, then operator actual 6 g: −5.2,
  then −0.8). Movements carry `reference_type = 'job_consumption'`, the
  consumption row as `reference_id`, and the running balance. Recording the
  same fact again relieves nothing.
- **Genealogy.** When the lot is known and the job has an unsealed genealogy
  record, the material is written to it once
  (`genealogy_material_consumption`, batch lot resolved by lot number).

## Operator report

`POST /v1/tasks/:id/consumption` (people and wildcard API keys only; it moves
stock):

```json
{"material_code": "FIL-PLA-BLK", "material_lot": "LOT-0042", "quantity": 6, "unit": "g"}
{"quantity": 95, "unit": "min"}
```

It belongs to the task's latest completed dispatch when there is one. The lot
is inherited from earlier facts of the same job and material when omitted.
201 returns `{consumption, stock_movements}`; the same actual for the same job
and material returns 409 `already_recorded` (corrections are not supported
yet; use the inventory adjust endpoint for stock).

## Read for cost ledgers

`GET /v1/jobs/consumption?from=&to=&task_id=&order_id=&engagement_slug=&limit=&offset=`

- `from` inclusive, `to` exclusive, RFC 3339, on `occurred_at`.
- `limit` 1–1000 (default 100).
- Machine callers need `pravara-mes:read`, `pravara-mes:jobs` or
  `pravara-cogs:read` (the cost ledger's scope). The same read is the MCP
  tool `mes.job_consumption` ([native-mcp.md](./native-mcp.md)).
- Response: `{data: [row], total, limit, offset}`; each row has `job_kind`,
  `job_id`, `dispatch_id`, `print_job_id`, `task_id`, `order_id`,
  `engagement_slug`, `machine_id`, `kind`, `material_code`, `material_lot`,
  `quantity`, `unit`, `basis`, `source`, `note`, `occurred_at`, `created_at`.
  Machine minutes are the `machine_time` rows (`unit = min`).
