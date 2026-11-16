# yantra4d: dispatch render client

Pravara's client for Yantra4D renders during fabrication dispatch
(`render_client.go`): `POST /api/render` with its Janua machine client
(audience `yantra4d-api`, scope `yantra4d:render`) and the GOC-1 sidecar of the
part it dispatches. Picking the part out of the answer is dispatch's
`pickPart` (`internal/dispatch/stages.go`); operations notes are in
`docs/operations/fabrication-dispatch.md`.

## Checking the Yantra4D link

Two opt-in live tests call Yantra4D's public API (`https://api.yantra4d.com`,
override with `YANTRA4D_LIVE_BASE_URL`). They are skipped unless
`YANTRA4D_LIVE=1`, so CI never calls the network.

```sh
# this client (from apps/pravara-api)
YANTRA4D_LIVE=1 go test ./internal/integrations/yantra4d/ -run Live -v -count=1
# the visualization engine's client (from apps/visualization-engine)
YANTRA4D_LIVE=1 go test ./internal/yantra4d/ -run Live -v -count=1
```

Both run anonymously, on Yantra4D's guest tier, against project
`multiboard`, mode `tile`, part `tile` (a small OpenSCAD cartridge; the guest
tier may not use CadQuery) at 2 × 2 cells, exported as STL (the only format the
guest tier may export). They check:

- **success**: the render answers `success`, the part is present, the
  artifact downloads from the configured https origin, its sha256 equals the
  answer's, and the GOC-1 sidecar matches (instance id, variables digest,
  primary geometry digest). A second identical render with `ignore_cache` is
  compared with the first: same instance id required, same digest reported
  (reproducibility is logged, not asserted);
- **fail closed**: an unknown project (`404`), and an export format the
  guest tier may not use (`3mf`, `403`) are errors. An unknown part is an
  error from the visualization client; for this client the test checks the
  part is absent and not reported failed, which `pickPart` refuses.

The guest tier allows 10 renders an hour per address. The two tests make
seven counted renders between them (an unknown project is answered before
the rate limiter), spaced a few seconds apart. Run them sparingly.

Not covered: production's authenticated path. This client's machine token
comes from a local fake Janua and the test transport strips it, so the
`yantra4d:render` scope check and the `3mf` export dispatch actually uses are
not exercised. Checking those needs pravara's real machine client.
