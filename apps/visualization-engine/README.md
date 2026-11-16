# visualization-engine

3D factory-floor visualization and physics simulation (Go, port 4205).

**Not deployed.** No workflow builds or deploys this service (see the
repository README, «Deployment»). Nothing here runs in production.

## Yantra4D import

`POST /v1/models/import/yantra4d` imports one part of a configured
Yantra4D project as a GLB machine model:

```json
{"slug": "gear-reducer", "mode": "gears", "part": "input_gear", "params": {"teeth": 20}}
```

`mode` may be omitted only when the project has one mode, `part` only when
the mode has one part. The importer fetches the manifest, renders the part as
GLB, downloads and verifies it, stores it in S3 under its sha256 and returns
the model. Any doubt is an error and nothing is stored.

### Yantra4D contract (checked against Yantra4D `main`, 2026-11-16)

| Call | Contract |
| --- | --- |
| `GET /api/projects/<slug>/manifest` | The project's `project.json`. The client reads `project`, `modes[].{id,parts,engine}`, `parts[].id`, `parameters[].{id,type,default}`, `export_formats` and ignores the rest. |
| `POST /api/render` | Body `{project, mode, parameters, export_format, ignore_cache?}`, parameters nested. Answer `{status, parts[{type, url, size_bytes, sha256, media_type, instance_id, variables_url}], log, request_id, generator_output{format_version, complete, variables_sha256}}`. |
| `GET /static/<file>` | The rendered part and, next to it, its GOC-1 sidecar (`<file>.variables.json`). Deleted 24 h after it was made; `Last-Modified` is when it was rendered. |

Behaviour of Yantra4D the client guards against (`internal/yantra4d`):

- A part that fails or times out is left out of a `200 "success"` answer and
  reported only in `log` as `[<part>] ERROR: …`. A missing part, or one the log
  reports as failed, is an error.
- An export format Yantra4D does not know is rendered as STL. The client only
  sends known formats and refuses a part whose `media_type` is not the one
  requested.
- Export formats are gated by tier: anonymous callers are the `guest` tier
  (STL only); GLB needs `pro`. A refused format is a `403`.
- The caller's bearer token is forwarded. Yantra4D honours only a Janua token
  issued for its own audience (`yantra4d-api`); any other token is silently
  treated as anonymous, so a GLB import then fails with `403`.
- Every URL in the answer must be on the configured origin (https), redirects
  are refused, the downloaded bytes must match `sha256` and `size_bytes`, and
  the GOC-1 sidecar must describe the same cartridge, mode and part with the
  same `instance_id` (recomputed per GOC-1 §3.4), the same `variables_sha256` as
  the answer, and a primary geometry entry with the same digest and size.

The base URL is `yantra4d.base_url` (default `https://api.yantra4d.com`). It
must be an https origin; plain http is accepted only for a loopback host.

Known gaps: the model's bounding box is left empty (Yantra4D's manifest no
longer carries part dimensions), and the material, BOM and assembly-step
calls of the old client were removed (unused, and their answers had changed
shape); re-add them against the current contract when something needs them.

### Live check

```sh
YANTRA4D_LIVE=1 go test ./internal/yantra4d/ -run Live -v -count=1
```

Calls Yantra4D's public API anonymously (four render calls plus one refused
before the rate limiter; the guest tier allows 10 an hour). Skipped without
`YANTRA4D_LIVE=1`, so CI never calls the network. See also
`apps/pravara-api/internal/integrations/yantra4d/README.md`.

## Tests

```sh
go vet ./... && go test ./...
```
