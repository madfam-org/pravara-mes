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
| ACL | `EdgeNodeACL`, `Permits`, `FilterCovers`, `FormatACL` |

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
