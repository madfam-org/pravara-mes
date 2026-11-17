# Fabrication dispatch: matchmaking, dispatcher, passport updater

pravara-api turns a production task into a print on a MADFAM-operated
machine and records what happened. pravara does no geometry work: it routes
JSON, digests and state between the services that own each step.

```text
task → order item → product (cartridge, mode, parameters, type shell)
  → RequirementProfile from the type shell            (asset-shells, public read)
  → matchmaking v2 + TTL reservation                   (registry + Sparkplug live state)
  → render bundle: geometry + GOC-1 variables.json     (yantra4d, machine client)
  → keep those bytes, verified by sha256               (pravara's own bucket)
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
| `GET /v1/dispatches/:id/artifacts/:kind` (`artifact` or `sidecar`, optional `?sha256=`) | people; machine tokens with `jobs` only (not `read`: this is the geometry itself) | The bytes Pravara kept of the dispatch's render. 404 for another tenant's dispatch or a digest that was not kept. Checked against the recorded sha256 before anything is sent; a damaged copy is a 500 `artifact_digest_mismatch`, never a download. At most `DISPATCH_ARTIFACT_CONCURRENT_READS` downloads per process at once; more get 503 with `Retry-After: 5`. |

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

Pravara records the render's address and digests (the part's URL, its `sha256`, the GOC-1
`instance_id`, the sidecar's URL and digest) **and keeps the bytes** (owner ruling S12 of
2026-11-14, RFC 0059 wave 0). See [Kept manufacturing bytes](#kept-manufacturing-bytes).
fabrication-prep still fetches its input from yantra4d and checks it against the digest.

yantra4d deletes render artifacts 24 hours after they were made and on a pod restart, renames
them on every release, and answers a repeated request from its cache with the same artifact. So:

- **A render is stamped `rendered_at` with the artifact's own time**: the `Last-Modified` yantra4d
  sends with the sidecar when there is one (a cached answer points at an older artifact), else the
  time Pravara received the answer.
- **A dispatch that goes back to matching reuses its render only while it is fresh**
  (`DISPATCH_RENDER_FRESH_FOR_SECONDS`). An older one, one with no stamp, or one that cannot be
  read is requested again, with `ignore_cache` so yantra4d makes a new artifact instead of
  returning the expiring one.
- **The new render must be the same design instance** (`instance_id`). If it is not, the design
  changed while the dispatch waited: it fails with `render_instance_changed` and says to dispatch
  again.
- **The same instance may come back with different bytes.** yantra4d does not promise that a
  re-render reproduces them. The dispatch continues with the new render, keeps the earlier digest
  as `previous_sha256`, and logs a warning.
- **The slice request's idempotency key names the render as well as the machine**
  (`pravara-dispatch:<dispatch>:<machine>:<render>`), so slicing again after a re-render, even on
  the same machine, is a new request and not a reused key with another body.

### Parts yantra4d leaves out

yantra4d answers `200 "success"` with a failed or timed-out part simply absent, and writes
`[<part>] ERROR: <reason>` in its render log. Pravara reads that line:

| Situation | Outcome |
|---|---|
| No parts came back | `render_failed`, retried |
| The wanted part is absent and the log reports it failed | `render_part_missing`, retried, with the end of the log |
| The wanted part is absent and nothing failed | `render_part_missing`, terminal: the mode does not produce it |
| No part was named and the log reports a failed part | `render_incomplete`, retried. The part that did come back is **not** taken as "the only part" |
| No part was named and several came back | `render_part_ambiguous`, terminal |

Retries spend `DISPATCH_MAX_ATTEMPTS`.

## Kept manufacturing bytes

**Only yantra4d's own origin.** The yantra4d client sends a machine bearer with every call, so it
follows a URL from a render answer only when it resolves to the configured base URL's scheme and
host, never follows a redirect, and refuses a base URL that is not https (plain http only on a
loopback host, for tests). This holds for the sidecar fetch as well as the downloads below.

After the part is picked and its sidecar checked, and **before** the dispatch moves on to slicing,
Pravara downloads the rendered geometry and keeps it with the GOC-1 sidecar (`variables.json`, the
bytes it already downloaded and checked; it is the variables document). Each is checked against its
declared sha256 while it streams into a temporary file, bounded by `DISPATCH_ARTIFACT_MAX_BYTES`,
and only then stored. The render bundle (`dispatch_jobs.render_bundle`, JSON; no migration) records
each kept object under `kept`: `kind` (`artifact` or `sidecar`), `key`, `sha256`, `bytes`,
`media_type`, `kept_at`.

| Situation | Outcome |
|---|---|
| A part or sidecar URL in the render answer is not on `YANTRA4D_API_URL`'s scheme and host (another host, `//host/…`, plain http) | `render_url_off_origin`, terminal; nothing is fetched from it |
| yantra4d answers a download with a redirect | `render_download_failed` (`redirect_refused`), terminal; redirects are never followed |
| Download fails, or yantra4d answers 5xx, 429, 404 or 410 | `render_download_failed`, retried (the retry renders again) |
| yantra4d refuses the download for good (e.g. 403) | `render_download_failed`, terminal |
| The bytes do not match the declared sha256 | `render_digest_mismatch`, terminal; nothing is stored or sliced |
| Larger than `DISPATCH_ARTIFACT_MAX_BYTES` (declared or while streaming) | `render_artifact_too_large`, terminal |
| The bucket cannot be written | `artifact_store_failed`, retried. The stored message says what failed (HTTP status or "could not be reached"), never the endpoint, bucket or key; the full error is in the server log |
| Another object already sits at the content address, or one without the recorded `sha256` metadata | `artifact_store_conflict`, terminal; nothing is overwritten |

**Key layout.** Content-addressed and namespaced by tenant:
`tenants/<tenant_id>/sha256/<hex digest>`, with the digest also recorded as object metadata
`sha256`. The same bytes are stored once per tenant; a second Put of them writes nothing. Writes are
conditional (`If-None-Match: *`), so nothing is ever replaced, and carry `x-amz-checksum-sha256`, so
the store itself refuses a body that is not the declared digest. An object at the key without the
`sha256` metadata is treated as a conflict, never as already stored.

**Nothing goes onward unkept.** Slicing and `start_job` only follow a render whose bytes are kept. A
dispatch recorded before this existed and found in `rendered` or `sliced` (or in `enqueuing` before
its ledger row is written) goes back to the render stage, which keeps the render in place while it is
fresh and renders again (and keeps) when it is not; a slice already made from the same render is found
again by its idempotency key. A dispatch whose `start_job` ledger row already exists goes on: that
command may already be on the stream and is re-sent from the ledger, so rewinding it would start a
second job.

**Time bounds.** The keep step is bounded to 12 minutes in all (downloads bounded by the yantra4d
client's 5-minute timeout, each store call by 2 minutes). Before it starts, the runner extends its
lease on the dispatch to 15 minutes and the machine reservation to cover it, so no other runner claims
the dispatch meanwhile. The runner also renews its lease before **every** hop: a claimed batch is
stepped one dispatch at a time, so a later dispatch whose claim ran out (and that another replica
claimed) is skipped rather than rendered, reserved or saved over the other runner's lease.

**Start-up self-check.** Whenever the bucket is configured, the API checks it against the real server
when it starts: a conditional put carrying a sha256 checksum is accepted, a second conditional put is
refused with 412, HEAD returns the `sha256` metadata, the object reads back intact, and the same bytes
again count as already stored. The probe lives under `selfcheck/` (never under `tenants/`) and is
removed afterwards. With dispatch disabled the check runs in the background and only logs
(`Artifact store self-check passed`, or the failing step); with dispatch enabled it runs before
dispatch starts and a failure stops the API from starting dispatch, so dispatch never runs on a bucket
that does not keep bytes as described here.

**Retention.** None: kept bytes are never deleted. They are the manufacturing record. The store
interface has no delete, and a stale render that is requested again keeps its new bytes **and**
leaves the earlier copy in place, listed on the bundle under `superseded_kept` and still readable with
`?sha256=`. A render kept across a re-match that was recorded before this existed gets its bytes kept
the next time it is reused while still fresh.

**What slicing reads.** Unchanged: fabrication-prep fetches inputs only from hosts on its
`INPUT_ALLOWED_HOSTS` (`api.yantra4d.com`) and follows no redirects, so the slice request still
carries the yantra4d URL and digest. The kept copy is Pravara's record, not slicing's input.

**Provisioning (owner).** The bucket and its bucket-scoped token (Object Read & Write; it cannot
change bucket settings) come from Enclii:

```bash
enclii buckets create pravara-dispatch-artifacts --project pravara-mes --namespace pravara-mes \
  --secret-name pravara-dispatch-artifacts
```

That writes the Secret `pravara-dispatch-artifacts` in namespace `pravara-mes` with
`STORAGE_BACKEND` (`r2`), `R2_ENDPOINT_URL`, `R2_BUCKET_NAME`, `R2_ACCESS_KEY_ID` and
`R2_SECRET_ACCESS_KEY`. pravara-api mounts it with `envFrom` (`optional: true`, so pods start before
it exists) and reads those exact names. The legacy `R2_ENDPOINT` / `R2_BUCKET` settings, and the
`R2_*` entries of `apps/pravara-api/.enclii.yml` that could have shadowed the Secret, were removed.
The pod's `/tmp` emptyDir is limited to 1Gi (with a matching `ephemeral-storage` limit): two
concurrent reads and one keep of 256 MiB each; raise it with either setting.

**Fail closed.** With `DISPATCH_ENABLED=true`, a missing or incomplete store (any of the five keys
absent, a backend other than `r2`, an endpoint that is not https) is a configuration error: the API
does not start, and the previous pod keeps serving. With dispatch disabled, a missing store changes
nothing (an incomplete one is logged as a warning). Create the bucket before turning dispatch on.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `DISPATCH_ENABLED` | `false` | Turns on dispatch and the runner. The dry run works either way. |
| `DISPATCH_RENDER_FORMAT` | `3mf` | `3mf` or `stl` (what fabrication-prep slices). |
| `DISPATCH_RESERVATION_TTL_SECONDS` | `900` | Reservation before start_job; extended by each hop. |
| `DISPATCH_RENDER_FRESH_FOR_SECONDS` | `21600` | How long a render kept across a re-match is reused before it is requested again. Must be under 86400 (yantra4d's artifact lifetime); the service refuses to start otherwise. |
| `DISPATCH_COMMAND_HOLD_SECONDS` | `21600` | Reservation after start_job, until the job ends. |
| `DISPATCH_MATCH_WAIT_SECONDS` | `86400` | How long to wait for an eligible machine. |
| `DISPATCH_MAX_ATTEMPTS` | `5` | Retries of retryable failures. |
| `DISPATCH_POLL_INTERVAL_SECONDS` | `10` | Runner tick and slice-job poll interval. |
| `DISPATCH_PASSPORT_MAX_ATTEMPTS` | `20` | Delivery attempts per passport row. |
| `DISPATCH_ARTIFACT_MAX_BYTES` | `268435456` (256 MiB) | Largest geometry or sidecar Pravara keeps; larger fails the dispatch. Downloads are spooled to `/tmp`, not memory. |
| `DISPATCH_ARTIFACT_CONCURRENT_READS` | `2` | Kept-bytes downloads one process serves at once; more get 503 with `Retry-After`. Each download's write deadline is lifted to 2 minutes plus a second per MiB, and the store read stops when the client goes away. |
| `STORAGE_BACKEND`, `R2_ENDPOINT_URL`, `R2_BUCKET_NAME`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` | unset | The kept-bytes bucket, from the `pravara-dispatch-artifacts` Secret. Required when dispatch is enabled. |
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
