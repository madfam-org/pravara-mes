# Digital Fabrication Machine Universe

## Overview

This document is the authoritative quick-reference for all 50 digital fabrication machines supported by PravaraMES. Each machine entry includes its category, protocol, adapter status, registry key, connection method, and current implementation status.

For the full benchmarking report with implementation statistics, competitive analysis, and architecture details, see [claudedocs/machine-benchmarking.md](../claudedocs/machine-benchmarking.md).

---

## Machine Reference Table

### A. FDM 3D Printers

| # | Machine | Protocol | Adapter | Registry Key | Connection | Status | Notes |
|---|---------|----------|---------|--------------|------------|--------|-------|
| 1 | Bambu Lab P1S | Bambu MQTT | bambu_adapter.go | `bambu_p1s` | MQTT TLS :8883 | Implemented | AMS 4-slot, 1080p camera, input shaping |
| 2 | Bambu Lab A1 | Bambu MQTT | bambu_adapter.go | `bambu_a1` | MQTT TLS :8883 | Implemented | AMS Lite, 1080p camera, open frame |
| 3 | Bambu Lab X1 Carbon | Bambu MQTT | bambu_adapter.go | `bambu_x1c` | MQTT TLS :8883 | Implemented | AMS, lidar, active chamber to 60C |
| 4 | Creality K1 Max | Moonraker | moonraker_adapter.go | `creality_k1_max` | HTTP :7125 | Implemented | AI camera, 600mm/s, input shaping |
| 5 | Voron 2.4 | Moonraker | moonraker_adapter.go | `voron_2_4` | HTTP :7125 | Implemented | CoreXY kit, enclosed, Klipper firmware |
| 6 | RatRig V-Core 4 | Moonraker | moonraker_adapter.go | `ratrig_vcore4` | HTTP :7125 | Implemented | CoreXY kit, 400mm build, Klipper |
| 7 | Prusa Core One | PrusaLink | prusalink_adapter.go | `prusa_core_one` | HTTP :80 | Implemented | Enclosed, input shaping, PrusaConnect |
| 8 | Prusa MK4 | Marlin/PrusaLink | marlin_adapter.go | `prusa_mk4` | Serial 250000 / HTTP | Implemented | Dual protocol, proven workhorse |
| 9 | Prusa MINI+ | PrusaLink | prusalink_adapter.go | `prusa_mini_plus` | HTTP :80 | Implemented | Compact, PrusaLink native |
| 10 | Ultimaker S5 | Ultimaker REST | ultimaker_adapter.go | `ultimaker_s5` | HTTP :80 (digest) | Implemented | Dual extrusion, 720p camera, NFC materials |
| 11 | Duet3D Generic | Duet/RRF | duet_adapter.go | `duet_generic` | HTTP + WebSocket | Implemented | RepRapFirmware, DWC web interface |
| 12 | OctoPrint Gateway | OctoPrint | octoprint_adapter.go | N/A (gateway) | HTTP :5000 | Implemented | Universal serial bridge, 300+ plugins |
| 13 | Ender 3 S1 Pro | Marlin | marlin_adapter.go | N/A (generic Marlin) | Serial 115200 | Implemented | Uses generic Marlin adapter |
| 14 | Anycubic Kobra 2 Max | Marlin | marlin_adapter.go | N/A (generic Marlin) | Serial 115200 | Implemented | Large format 420x420x500 |
| 15 | Creality CR-10 Smart Pro | Marlin | marlin_adapter.go | N/A (generic Marlin) | Serial / WiFi | Implemented | WiFi-capable, large format |

### B. Resin 3D Printers

| # | Machine | Protocol | Adapter | Registry Key | Connection | Status | Notes |
|---|---------|----------|---------|--------------|------------|--------|-------|
| 16 | Formlabs Form 4 | Formlabs REST | formlabs_adapter.go | `formlabs_form4` | HTTPS (OAuth2) | Implemented | Fleet Control, 8 resin families |
| 17 | Prusa SL1S | PrusaLink | prusalink_adapter.go | `prusa_sl1s` | HTTP :80 | Implemented | MSLA, 47um XY, tilt mechanism |
| 18 | Elegoo Saturn 4 Ultra | Custom | -- | `elegoo_saturn4_ultra` | Serial (proprietary) | Registry-only | 18um XY, no public API |
| 19 | Anycubic Photon Mono M7 | Custom | -- | `anycubic_photon_mono_m7` | Serial (proprietary) | Registry-only | 40um XY, no public API |
| 20 | Phrozen Sonic Mighty 8K | Custom | -- | `phrozen_sonic_mighty_8k` | Serial (proprietary) | Registry-only | 28um XY, 8K resolution |

### C. Laser Cutters / Engravers

| # | Machine | Protocol | Adapter | Registry Key | Connection | Status | Notes |
|---|---------|----------|---------|--------------|------------|--------|-------|
| 21 | Ruida RDC6445 Generic | Ruida UDP | ruida_adapter.go | `ruida_generic` | UDP :50200 | Implemented | Binary protocol, 70% of Chinese CO2 lasers |
| 22 | xTool P2S | xTool HTTP | xtool_adapter.go | `xtool_p2s` | HTTP :8080 | Implemented | 55W CO2, passthrough mode |
| 23 | xTool S1 | xTool HTTP | xtool_adapter.go | `xtool_s1` | HTTP :8080 | Implemented | 40W diode, enclosed, air assist |
| 24 | xTool F1 Ultra | xTool HTTP | xtool_adapter.go | `xtool_f1_ultra` | HTTP :8080 | Implemented | Galvo + diode dual laser, ultra-fast |
| 25 | Creality Falcon2 22W | GRBL | grbl_adapter.go | `creality_falcon2` | Serial 115200 | Implemented | Diode 22W, LightBurn compatible |
| 26 | Glowforge Pro | Custom (Cloud) | -- | `glowforge_pro` | HTTPS (cloud-only) | Registry-only | 45W CO2, no local API |
| 27 | Trotec Speedy 360 | Custom | -- | `trotec_speedy_360` | Serial (proprietary) | Registry-only | 120W CO2, JobControl software |

### D. CNC Routers / Mills

| # | Machine | Protocol | Adapter | Registry Key | Connection | Status | Notes |
|---|---------|----------|---------|--------------|------------|--------|-------|
| 28 | Generic GRBL CNC | GRBL | grbl_adapter.go | `grbl_generic` | Serial 115200 | Implemented | Universal GRBL reference definition |
| 29 | Carbide 3D Shapeoko HDM | GRBL | grbl_adapter.go | `shapeoko_hdm` | Serial 115200 | Implemented | 650x650x150, 24k RPM spindle |
| 30 | Inventables X-Carve Pro | GRBL | grbl_adapter.go | `xcarve_pro` | Serial 115200 | Implemented | 610x610x95, Easel software |
| 31 | OpenBuilds LEAD CNC | GRBL | grbl_adapter.go | `openbuilds_lead` | Serial 115200 | Implemented | 1000x1000x90, open-source |
| 32 | Sienci Labs LongMill MK2 | GRBL | grbl_adapter.go | `sienci_longmill_mk2` | Serial 115200 | Implemented | 762x762x114, gSender |
| 33 | Stepcraft D.840 | GRBL | grbl_adapter.go | `stepcraft_d840` | Serial 115200 | Implemented | 840x600x140, multi-function |
| 34 | Onefinity Woodworker X-50 | Buildbotics | buildbotics_adapter.go | `onefinity_woodworker` | HTTP REST | Implemented | 816x816x133, Buildbotics controller |
| 35 | Generic LinuxCNC Machine | LinuxCNC | linuxcnc_adapter.go | `linuxcnc_generic` | TCP :5007 | Implemented | Full ISO 6983 G-code, professional |

### E. Multi-Tool Platforms

| # | Machine | Protocol | Adapter | Registry Key | Connection | Status | Notes |
|---|---------|----------|---------|--------------|------------|--------|-------|
| 36 | Snapmaker 2.0 A350 | Marlin + HTTP | snapmaker_adapter.go | `snapmaker_a350` | Serial + WiFi :8080 | Implemented | 3DP/Laser/CNC, M1005 tool detect |
| 37 | Snapmaker 2.0 A250 | Marlin + HTTP | snapmaker_adapter.go | (shared) | Serial + WiFi :8080 | Implemented | Same protocol, 230x250x235 |
| 38 | Snapmaker J1 | Marlin + HTTP | snapmaker_adapter.go | (shared) | Serial + WiFi :8080 | Implemented | IDEX dual extrusion |

### F. Vinyl Cutters

| # | Machine | Protocol | Adapter | Registry Key | Connection | Status | Notes |
|---|---------|----------|---------|--------------|------------|--------|-------|
| 39 | Roland GR2-640 | CAMM-GL III | roland_adapter.go | `roland_gr2_640` | Serial 9600 | Implemented | Professional, 640mm width |
| 40 | Roland GS2-24 | CAMM-GL III | roland_adapter.go | `roland_gs2_24` | Serial 9600 | Implemented | Desktop, 584mm width |
| 41 | Graphtec CE8000-60 Plus | GP-GL | graphtec_adapter.go | `graphtec_ce8000` | Serial 9600 | Implemented | 606mm, 600g force, print-and-cut |
| 42 | Silhouette Cameo 5 | Custom | -- | `silhouette_cameo_5` | USB (proprietary) | Registry-only | 305mm width, 210g force, no public API |

### G. Pick and Place

| # | Machine | Protocol | Adapter | Registry Key | Connection | Status | Notes |
|---|---------|----------|---------|--------------|------------|--------|-------|
| 43 | Opulo LumenPnP | OpenPnP | openpnp_adapter.go | `lumen_pnp` | HTTP + G-code | Implemented | 48 feeders, dual cameras, open-source |
| 44 | Index Pick and Place | OpenPnP | openpnp_adapter.go | `index_pnp` | HTTP + G-code | Implemented | 20 feeders, open-source hardware |
| 45 | Neoden YY1 | Custom | -- | `neoden_yy1` | Serial (proprietary) | Registry-only | 24 feeders, proprietary protocol |

### H. PCB Mills

| # | Machine | Protocol | Adapter | Registry Key | Connection | Status | Notes |
|---|---------|----------|---------|--------------|------------|--------|-------|
| 46 | Bantam Tools Desktop PCB Mill | Custom | -- | `bantam_tools_pcb_mill` | USB (proprietary) | Registry-only | 152x102x38, proprietary desktop app |
| 47 | LPKF ProtoMat S64 | Custom | -- | (planned) | Proprietary | Registry-only | Professional, multilayer capable |

### I. Specialty

| # | Machine | Protocol | Adapter | Registry Key | Connection | Status | Notes |
|---|---------|----------|---------|--------------|------------|--------|-------|
| 48 | Universal Robots UR5e | URScript | urscript_adapter.go | `ur_generic` | TCP :30002 | Implemented | 6-DOF, 850mm reach, joint+linear moves |
| 49 | Dobot Magician | Dobot | dobot_adapter.go | `dobot_magician` | Serial 115200 | Implemented | 4-DOF, 320mm reach, suction cup |
| 50 | Brother PE800 | Custom | -- | `brother_pe800` | USB (proprietary) | Registry-only | Embroidery, 130x180mm hoop, PES/DST |

---

## Registry Keys

All registered machine definitions can be accessed programmatically through the `Registry` API. The following keys are loaded at initialization.

### Programmatic Access

```go
import "pravara-mes/apps/machine-adapter/internal/registry"

reg := registry.NewRegistry()
def, ok := reg.GetDefinition("bambu_p1s")

// List all definitions
allDefs := reg.ListDefinitions()
```

### Complete Key List

```
# FDM 3D Printers
bambu_p1s
bambu_a1
bambu_x1c
creality_k1_max
voron_2_4
ratrig_vcore4
prusa_core_one
prusa_mk4
prusa_mini_plus
ultimaker_s5
duet_generic
grbl_generic

# Resin 3D Printers
formlabs_form4
prusa_sl1s
elegoo_saturn4_ultra
anycubic_photon_mono_m7
phrozen_sonic_mighty_8k

# Laser Cutters / Engravers
ruida_generic
xtool_p2s
xtool_s1
xtool_f1_ultra
creality_falcon2
glowforge_pro
trotec_speedy_360

# CNC Routers / Mills
shapeoko_hdm
xcarve_pro
openbuilds_lead
sienci_longmill_mk2
stepcraft_d840
onefinity_woodworker
linuxcnc_generic

# Multi-Tool (uses Snapmaker adapter)
snapmaker_a350

# Vinyl Cutters
roland_gr2_640
roland_gs2_24
graphtec_ce8000
silhouette_cameo_5

# Pick and Place
lumen_pnp
index_pnp
neoden_yy1

# PCB Mills
bantam_tools_pcb_mill

# Specialty
ur_generic
dobot_magician
brother_pe800
```

---

## Protocol Documentation Sources

| Protocol | Primary Documentation | Notes |
|----------|----------------------|-------|
| GRBL | [github.com/gnea/grbl/wiki](https://github.com/gnea/grbl/wiki) | Serial command reference, status reporting |
| Marlin | [marlinfw.org/docs/gcode](https://marlinfw.org/docs/gcode/) | G-code dictionary, M-code extensions |
| Bambu MQTT | Community reverse-engineered; [bambulab wiki](https://wiki.bambulab.com/) | MQTT topics, JSON payloads, TLS setup |
| Moonraker | [moonraker.readthedocs.io](https://moonraker.readthedocs.io/) | REST endpoints, WebSocket JSON-RPC |
| OctoPrint | [docs.octoprint.org/en/master/api](https://docs.octoprint.org/en/master/api/) | REST API, SockJS events, plugin API |
| PrusaLink | [github.com/prusa3d/Prusa-Link-Web](https://github.com/prusa3d/Prusa-Link-Web) | HTTP API, status model, job control |
| Ruida | Community reverse-engineered; LightBurn wiki | UDP binary protocol, handshake sequence |
| Duet/RRF | [docs.duet3d.com](https://docs.duet3d.com/) | HTTP API (rr_* endpoints), object model |
| Ultimaker | [developer.ultimaker.com](https://developer.ultimaker.com/) | REST API, digest authentication |
| Formlabs | [developer.formlabs.com](https://developer.formlabs.com/) | Fleet Control API, OAuth2 flow |
| xTool | Community documentation | HTTP REST, JSON payloads |
| Roland CAMM-GL | Roland developer documentation | CAMM-GL III command reference |
| Graphtec GP-GL | Graphtec developer documentation | GP-GL command set |
| OpenPnP | [github.com/openpnp/openpnp/wiki](https://github.com/openpnp/openpnp/wiki) | G-code extensions, HTTP API |
| URScript | [universal-robots.com/academy](https://www.universal-robots.com/academy/) | URScript manual, TCP interface guide |
| LinuxCNC | [linuxcnc.org/docs](https://linuxcnc.org/docs/) | linuxcncrsh text protocol |
| Buildbotics | [buildbotics.com/docs](https://buildbotics.com/) | REST API endpoints |
| Dobot | Dobot SDK documentation | Binary serial protocol spec |

---

## Implementation Status Legend

| Status | Meaning |
|--------|---------|
| **Implemented** | Full adapter with connect, send, status, and telemetry support |
| **Registry-only** | Machine definition in registry but no adapter (proprietary/closed protocol) |
| **Planned** | On roadmap, no implementation yet |

---

*Last updated 2026-03-03. Source: `apps/machine-adapter/` in the PravaraMES monorepo.*
