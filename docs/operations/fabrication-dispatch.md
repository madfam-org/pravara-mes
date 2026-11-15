# Fabrication dispatch: matchmaking, dispatcher, passport updater

pravara-api turns a production task into a print on a MADFAM-operated
machine and records what happened. pravara does no geometry work: it routes
JSON, digests and state between the services that own each step.

```text
task → order item → product (cartridge, mode, parameters, type shell)
  → RequirementProfile from the type shell            (asset-shells, public read)
  → matchmaking v2 + TTL reservation                   (registry + Sparkplug live state)
  → render bundle: geometry + GOC-1 variables.json     (yantra4d, machine client)
  → slice job with the machine's profiles, polled      (fabrication-prep, machine client)
  → re-check the machine, then start_job (signed URL + sha256)
    on the durable command stream; ledger row first    (telemetry-worker → DCMD)
  → Job/Status = complete for that command and device
  → genealogy + ManufacturingRecord + instance shell and passport (outbox)
                                                       (asset-shells, org-bound client)
```

## Routes

| Route | Who | What |
|---|---|---|
| `POST /v1/match` `{task_id}` | people; machine tokens with `pravara-mes:read` or `pravara-mes:jobs` | Dry run: every machine with its checks, eligible ones ranked. Reserves nothing. |
| `POST /v1/dispatches` `{task_id}` | people only (and wildcard API keys) | Files a dispatch. 202; 409 if the task already has one in progress; 503 when dispatch is disabled or a machine client is missing. |
| `GET /v1/dispatches`, `GET /v1/dispatches/:id` | people; `read` or `jobs` | State, match explanation, render and slice digests, the manufacturing record and the passport deliveries. |

`POST /v1/dispatches` is people-only for the same reason as
`POST /v1/machines/:id/command`: it ends in `start_job` on a printer.
Intake clients hold `pravara-mes:jobs` to file orders, not to start machines.

## What each hop reads

**Product.** From the order item's `specifications` first, then the product
definition: `metadata.slug` (cartridge), `metadata.commons` (default
`solid-hyperobjects`), `mode`, `part`, `parametric_specs.<id>.value` overlaid
by `specifications.parameters`, `type_shell_id` (else the single type shell
asset-shells holds for the commons and slug), `bounding_box_mm {x,y,z}` and
`process_overrides` (validated by fabrication-prep).

**Machine registry.** `machines.specifications.fabrication_capabilities`
(fabrication-capabilities keys: `process`, `build_volume_{x,y,z}_mm`,
`nozzle_diameters_mm`, `max_hotend_temp_c`, `max_bed_temp_c`, …),
`machines.metadata.fabrication_prep {printer_profile, target}` and,
optionally, `machines.metadata.material_lots {"<slot>": "<lot>"}`.
Capabilities a device declares in its DBIRTH override the registry; the match
explanation lists each value's source and any disagreement.

**Live state.** `machine_live_state` (written by the Sparkplug primary host):
born, `State/Status = idle`, and a loaded slot whose material class satisfies
the requirement (`materials.any_of` / `none_of`, narrowed by `parts`).

**Checks** (each `pass`, `fail`, `unknown` or `not_required`, with a
reason): registry status, live state, reservation, process, material, nozzle,
hot-end and bed temperature limits, build volume, slicing profiles.
A missing part bounding box is reported as a gap, not computed; set
`DISPATCH_REQUIRE_BOUNDING_BOX=true` to refuse such matches. Slicing is the
backstop for an oversized part.

## Failure handling

Every hop records its state on `dispatch_jobs`. Failures are retryable
(backed off, at most `DISPATCH_MAX_ATTEMPTS`) or terminal, with a code and the
callee's reason. Waiting for an eligible machine spends no attempts and is
bounded by `DISPATCH_MATCH_WAIT_SECONDS`. Idempotency: the slice job carries
an `Idempotency-Key` per dispatch and machine; the start_job command id is
saved before the stream append, so a replay re-sends the same id (the edge
node de-duplicates). Before start_job the reserved machine is evaluated again;
if it no longer qualifies, the reservation is released and the dispatch
matches again (the render is reused).

The completion must name the dispatched command **and** machine; otherwise
the dispatch fails visibly and no passport is written. The manufacturing
record is append-only. Passport deliveries are rows of `passport_outbox`;
a failed publish keeps the row and retries, and content the service rejects
(400/409/422) is marked failed and kept.

## What the render step keeps, and for how long

Pravara stores the render's **address and digests**, not its bytes: the part's URL, its
`sha256`, the GOC-1 `instance_id`, and the sidecar's URL and digest. fabrication-prep fetches
the bytes from yantra4d and checks them against the digest.

yantra4d deletes render artifacts after 24 hours and on a pod restart, and renames them on every
release. So:

- A render is stamped `rendered_at`. A dispatch that goes back to matching reuses its render only
  while it is younger than `DISPATCH_RENDER_FRESH_FOR_SECONDS`. An older one, or one recorded
  before the stamp existed, is requested again.
- The new render must be the **same design instance** (`instance_id`). If it is not, the design
  changed while the dispatch waited and it fails with `render_instance_changed`; dispatch again to
  make the new one.
- If the same instance comes back with **different bytes**, the dispatch continues with the new
  render, records the earlier digest as `previous_sha256`, and logs a warning. yantra4d does not
  promise that a re-render reproduces bytes.
- yantra4d answers `200 "success"` with a failed or timed-out part simply left out. Pravara treats
  an answer with no parts, or without the part it asked for (`render_part_missing`), as a failure
  to retry within `DISPATCH_MAX_ATTEMPTS`, and puts the end of yantra4d's render log in the
  error.

**Not done:** Pravara does not yet keep the manufacturing bytes themselves (owner ruling S12 of
2026-11-14). That needs an object store, which Pravara has configured names for and no code. Until
then, a render older than yantra4d's lifetime can only be made again, never fetched again, and a
passport's geometry digest names bytes that only exist while yantra4d or fabrication-prep still
holds them.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `DISPATCH_ENABLED` | `false` | Turns on dispatch and the runner. The dry run works either way. |
| `DISPATCH_RENDER_FORMAT` | `3mf` | `3mf` or `stl` (what fabrication-prep slices). |
| `DISPATCH_RESERVATION_TTL_SECONDS` | `900` | Reservation before start_job; extended by each hop. |
| `DISPATCH_RENDER_FRESH_FOR_SECONDS` | `21600` | How long a render kept across a re-match is reused before it is requested again. Keep it well under yantra4d's 24-hour artifact lifetime. |
| `DISPATCH_COMMAND_HOLD_SECONDS` | `21600` | Reservation after start_job, until the job ends. |
| `DISPATCH_MATCH_WAIT_SECONDS` | `86400` | How long to wait for an eligible machine. |
| `DISPATCH_MAX_ATTEMPTS` | `5` | Retries of retryable failures. |
| `DISPATCH_POLL_INTERVAL_SECONDS` | `10` | Runner tick and slice-job poll interval. |
| `DISPATCH_PASSPORT_MAX_ATTEMPTS` | `20` | Delivery attempts per passport row. |
| `YANTRA4D_API_URL` | `https://api.yantra4d.com` | yantra4d's API. The default until 2026-11-14, `yantra4d.madfam.io`, does not resolve. |
| `FABRICATION_PREP_API_URL`, `ASSET_SHELLS_API_URL` | `fabrication-prep-api.madfam.io`, `asset-shells-api.madfam.io` | Service base URLs. Neither service is deployed yet, so these hosts are unverified. |
| `JANUA_TOKEN_URL` | Janua token endpoint | client_credentials endpoint. |
| `ASSET_SHELLS_PUBLISHER_TENANTS` | `madfam=ASSET_SHELLS_PUBLISHER_MADFAM_ECOSYSTEM` | pravara tenant slug → environment prefix of its asset-shells publisher client. |

Client credentials come from the `pravara-service-clients` Secret
(`infra/k8s/base/external-secrets/pravara-service-clients.yaml`), mounted
only into pravara-api: `YANTRA4D_STEP_READER_CLIENT_ID/_SECRET`,
`FABRICATION_PREP_CLIENT_ID/_SECRET` and
`<PREFIX>_CLIENT_ID/_SECRET` per mapped tenant. Tokens are cached and
refreshed before expiry; secrets are never logged.

The asset-shells publisher is bound to one Janua organisation, and
asset-shells files instances under that organisation. A pravara tenant id is
not necessarily the Janua organisation id, so each publishing tenant is mapped
explicitly.
