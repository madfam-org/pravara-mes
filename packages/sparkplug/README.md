# packages/sparkplug

Eclipse Sparkplug 3.0 building blocks shared by pravara's edge node
(`apps/machine-adapter`) and its host application. Module:
`github.com/madfam-org/pravara-mes/packages/sparkplug`.

## Payload schema

- `sparkplugpb/sparkplug_b.proto` and `sparkplugpb/sparkplug_b.pb.go` are generated from the Go-declared
  descriptor in `internal/schema`. That descriptor was written from the payload chapter of the Sparkplug 3.0.0
  specification, with field numbers, wire types and cardinalities as specified, so the payloads are
  wire-compatible with conformant Sparkplug B implementations. The protobuf package is
  `pravara.sparkplug.b`; it is not on the wire.
- Regenerate with `go generate ./sparkplugpb`. No `protoc` is needed: the generator calls the
  `protoc-gen-go` library directly. `TestGeneratedCodeIsCurrent` fails if the generated code is stale, and
  `TestGeneratedSchemaMatchesSpecification` pins every field number and type.

## API

| Area | Functions |
|---|---|
| Topics | `NodeTopic`, `DeviceTopic`, `StateTopic`, `ParseTopic`, `ValidateID`, `SiteEdgeNodeID`, `PrimaryHostID` |
| Payloads | `Encode`, `Decode`, `NewMetric`, `NewContractMetric`, `SetValue`, `Value`, `EncodeDoubleArray`, `DecodeDoubleArray` |
| Sequence | `SeqCounter` (0..255 wrap), `NextBdSeq`, `SeqFollows` |
| Aliases | `AliasTable` (`Assign`, `Learn`, `LearnBirth`, `Lookup`, `Resolve`, `ResolverFor`) |
| Messages | `NodeBirth`, `NodeDeath`, `DeviceBirth`, `DeviceData`, `DeviceDeath`, `ValidateBirth`, `BdSeqOf` |
| Commands | `ParseDeviceCommand`, `BuildDeviceCommand`, `DeviceCommand.Validate`, `ParseRebirthRequest` |
| Host state | `HostState`, `EncodeState`, `DecodeState`, `HostTracker` |
| ACL | `EdgeNodeACL`, `Permits`, `FilterCovers`, `FormatACL`, `EdgeNodeUsername` |

## MES-1 metrics (typed constants in `metrics.go`)

| Metric | Datatype | Values |
|---|---|---|
| `Properties/Model`, `Properties/Firmware` | String | |
| `Properties/Connectivity` | String | `moonraker`, `bambu_lan_mqtt`, `octoprint` |
| `Capabilities/<fabrication-capabilities key>` | per key | Double (dimensions, temperatures); Int64 (`toolhead_count`, `material_slots`); Boolean (`enclosure`, `heated_chamber`); String (`process`, `firmware`, `connectivity`); DoubleArray (`nozzle_diameters_mm`) |
| `Materials/Slot<n>/Class` | String | a `material-classes` key, null when unknown |
| `Materials/Slot<n>/Loaded` | Boolean | |
| `State/Status` | String | `idle`, `printing`, `paused`, `error`, `offline` |
| `State/Progress` | Double | 0–100 |
| `Temps/Hotend`, `Temps/Bed` | Double | °C |
| `Job/Id` | String | the command's task id (or command id) |
| `Job/Status` | String | `queued`, `printing`, `complete`, `failed`, `cancelled` |
| `Command/LastId` | String | |
| `Command/Status` | String | `accepted`, `running`, `done`, `failed` |
| `Command/Error` | String | null unless failed |
| DCMD: `Command/Id`, `Command/Name`, `Command/TaskId`, `Command/Artifact/Url`, `Command/Artifact/Sha256`, `Command/Artifact/MediaType` | String | `Command/Name` ∈ `start_job`, `pause`, `resume`, `cancel` |

Node metrics: `bdSeq` (Int64) and `Node Control/Rebirth` (Boolean).

## Edge-node ACL (per credential)

```text
publish   spBv1.0/{tenant}/+/{edge}
publish   spBv1.0/{tenant}/+/{edge}/+
subscribe spBv1.0/{tenant}/NCMD/{edge}
subscribe spBv1.0/{tenant}/DCMD/{edge}/+
subscribe spBv1.0/STATE/+
```

Everything else is denied. `Permits` evaluates a publish topic or a subscribe filter against these rules, so a
broker authorization endpoint can reuse it.

## Primary host (`host`)

`host.Engine` is pravara's primary host application; the telemetry worker runs it with a database store, tests
with `host/hosttest.MemStore`.

- `Run` keeps one broker session at a time: the will is `spBv1.0/STATE/{host}` offline, the online STATE is
  published after subscribing with the same timestamp (retained, QoS 1), and a graceful stop publishes the
  offline STATE with that timestamp. Each reconnect uses a new timestamp.
- Subscriptions: `NBIRTH`, `NDEATH`, `NDATA` (`spBv1.0/+/{type}/+`) and `DBIRTH`, `DDATA`, `DDEATH`
  (`spBv1.0/+/{type}/+/+`). Received `NCMD`/`DCMD` are ignored.
- A message is accepted only from an edge node the `Store` resolves for the topic's group (the tenant). Per
  edge node the engine keeps the alias table, the NBIRTH `bdSeq` (an NDEATH with another `bdSeq` is stale) and
  the seq. A seq gap, an unknown alias or data before a birth ends the session and sends NCMD
  `Node Control/Rebirth` (at most once per `RebirthInterval`).
- `SendDeviceCommand` publishes a DCMD (QoS 0, no seq) to a device born in the current session and returns
  `Deferred` otherwise; after a DBIRTH the engine re-sends the commands the `Store` returns, with the same id.

## Broker credential decisions (`brokerauth`)

`brokerauth.Authorizer` decides EMQX HTTP authentication and authorization for edge-node credentials: bcrypt
password check, then `EdgeNodeACL` of the credential's own group and edge node. Unknown usernames get
`ignore`; disabled credentials get `deny`. `brokerauth.Handlers` serves the EMQX request and response shapes and
requires the shared `X-Pravara-Internal-Key` header. Edge credentials are generated by the site box
(`ValidatePassword`: 32 to 72 printable ASCII characters). See
[`docs/operations/sparkplug-broker-and-enrollment.md`](../../docs/operations/sparkplug-broker-and-enrollment.md).

