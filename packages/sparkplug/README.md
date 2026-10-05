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

### Motion metrics (MES-1 §1 amendment, Phase 7)

Moonraker devices declare these in DBIRTH when the edge config sets `motion.enabled: true` (off by default).
Values are the printer's own, unchanged: no rounding, no kinematics.

| Metric | Datatype | Source (Klipper Status Reference) | Unit |
|---|---|---|---|
| `Motion/Position/X`, `/Y`, `/Z`, `/E` | Double | `motion_report.live_position` (falls back to `toolhead.position` only when the printer has no `motion_report`) | mm, printer coordinates |
| `Motion/Velocity` | Double | `motion_report.live_velocity` | mm/s |
| `Motion/Homed` | String | `toolhead.homed_axes`, verbatim (`""`, `"xy"`, `"xyz"`) | |

Values are null until the printer first reports them. The metric timestamp is the edge box's receipt time of the
printer update. DDATA rules: at most one motion DDATA per `min_interval` (default 250 ms, Klipper's subscription
refresh interval). A sample is published whole, meaning every differing field, when an axis moved at least
`position_deadband_mm` (convention 0.05), the velocity changed by `velocity_deadband_mm_s` (convention 1.0) or the
homed axes changed. At rest (velocity 0) any difference is published, so the last value equals the printer's.

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
