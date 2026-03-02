# Digital Fabrication Machine Universe

## Overview
This document comprehensively catalogs the entire ecosystem of digital fabrication machines, their control systems, protocols, and standards. It serves as the authoritative reference for PravaraMES adapter implementation.

## Table of Contents
1. [CNC Mills & Routers](#cnc-mills--routers)
2. [3D Printers](#3d-printers)
3. [Laser Cutters & Engravers](#laser-cutters--engravers)
4. [Waterjet & Plasma Cutters](#waterjet--plasma-cutters)
5. [Vinyl & Knife Cutters](#vinyl--knife-cutters)
6. [PCB Fabrication](#pcb-fabrication)
7. [Embroidery Machines](#embroidery-machines)
8. [Robot Arms](#robot-arms)
9. [Official Standards](#official-standards)

---

## CNC Mills & Routers

### Open Source Firmware

#### GRBL
- **Version**: 0.9, 1.1 (current)
- **Platform**: 8-bit Arduino (ATmega328P)
- **Protocol**: Serial ASCII, 115200 baud default
- **G-code**: Subset of ISO 6983
- **Market Share**: ~40% of hobby CNCs
- **Machines**: Shapeoko, X-Carve, OpenBuilds, generic Chinese CNCs

#### grblHAL
- **Version**: Edge release
- **Platform**: 32-bit ARM (STM32, ESP32, Teensy, etc.)
- **Protocol**: Serial, Ethernet, WiFi
- **G-code**: Extended GRBL with canned cycles
- **Features**: Tool changing, backlash compensation

#### LinuxCNC
- **Version**: 2.8.x (stable), 2.9.x (development)
- **Platform**: PC-based real-time Linux
- **Protocol**: Parallel port, Mesa cards, EtherCAT
- **G-code**: Full ISO 6983 + extensions
- **Market**: Professional/industrial retrofits

#### FluidNC
- **Platform**: ESP32
- **Protocol**: WiFi, Bluetooth, Serial
- **Features**: Web interface, SD card support
- **Target**: Modern WiFi-enabled CNCs

### Industrial Controllers

#### Fanuc
- **Series**: 0i, 30i, 31i, 35i
- **Protocol**: FOCAS2 (Fanuc Open CNC API Specification)
- **Communication**: Ethernet, HSSB
- **G-code**: Fanuc dialect (G-code + custom M-codes)
- **Market Share**: ~50% of industrial CNCs globally

#### Siemens SINUMERIK
- **Series**: 808D, 828D, 840D sl
- **Protocol**: OPC UA, SINUMERIK Integrate
- **Communication**: PROFINET, Ethernet
- **Programming**: ISO G-code, ShopMill, ShopTurn

#### Haas
- **Control**: Haas NGC (Next Generation Control)
- **Protocol**: Proprietary, RS-232, Ethernet
- **Features**: Wireless Intuitive Probing System
- **API**: HaasConnect for IoT

#### Heidenhain
- **Series**: TNC 640, TNC 530, TNC 320
- **Protocol**: LSV2, DNC
- **Programming**: Conversational, Klartext, ISO
- **Specialty**: 5-axis machining

#### Mazak
- **Control**: Mazatrol Matrix, Smooth
- **Protocol**: MTConnect compliant
- **Features**: AI-assisted programming
- **Integration**: Mazak SmartBox

---

## 3D Printers

### FDM/FFF Firmware

#### Marlin
- **Version**: 1.1.x (legacy), 2.0.x, 2.1.x (current)
- **Platforms**: AVR, ARM32 (STM32, LPC, SAM)
- **Protocol**: Serial G-code, 250000 baud common
- **Market Share**: ~90% of consumer FDM printers
- **Machines**: Prusa, Creality, Anycubic, Ender series
- **Features**: Auto-leveling, Linear Advance, Input Shaping

#### Klipper
- **Architecture**: Raspberry Pi + MCU
- **Protocol**: Moonraker API (REST + WebSocket)
- **Features**: Input shaping, pressure advance
- **Performance**: High-speed printing (>500mm/s)
- **Growing Market**: Voron, RatRig, conversions

#### RepRapFirmware (Duet)
- **Boards**: Duet 2, Duet 3
- **Protocol**: HTTP REST API, WebSocket
- **Interface**: Duet Web Control (DWC)
- **Features**: Advanced kinematics, CNC mode
- **Market**: Premium printers, custom builds

#### Prusa Firmware
- **Base**: Marlin fork
- **Additions**: Prusa-specific features
- **Protocol**: Serial, PrusaLink (HTTP)
- **Cloud**: PrusaConnect
- **Machines**: Original Prusa i3 MK3/MK4, MINI

### 3D Printer Control Interfaces

#### OctoPrint
- **Protocol**: REST API + WebSocket
- **Port**: 5000 (default)
- **Authentication**: API key, user sessions
- **Plugins**: 300+ extensions available
- **Camera**: Webcam streaming, timelapse
- **Market**: Universal 3D printer interface

#### Moonraker (Klipper)
- **Protocol**: JSON-RPC over WebSocket
- **Port**: 7125 (default)
- **Features**: Multi-printer support
- **Frontends**: Mainsail, Fluidd, KlipperScreen

#### PrusaLink/PrusaConnect
- **Local**: PrusaLink (LAN control)
- **Cloud**: PrusaConnect (remote)
- **Protocol**: HTTP REST API
- **Features**: Camera, job queue

### Commercial 3D Printer APIs

#### Bambu Lab
- **Protocol**: MQTT over TCP
- **Port**: 8883 (TLS), 1883 (plain)
- **Authentication**: Access code, cloud token
- **Features**: Full control, camera stream
- **Machines**: X1 Carbon, P1P, A1

#### Ultimaker
- **API**: Ultimaker Digital Factory
- **Protocol**: REST API
- **Cloud**: Cura Connect
- **Features**: Fleet management

#### Formlabs (SLA)
- **Software**: PreForm
- **API**: Fleet Control API
- **Protocol**: HTTP REST
- **Features**: Remote job submission

---

## Laser Cutters & Engravers

### Hobby/Prosumer Controllers

#### GRBL-Based Lasers
- **Variants**: GRBL-LPC, LaserGRBL
- **PWM Control**: Spindle => Laser power
- **Safety**: Door interlock, emergency stop
- **Software**: LightBurn, LaserGRBL, LaserWeb

#### Ruida Controllers
- **Models**: RDC6442G, RDC6445G, RDC6442S
- **Protocol**: RD-UDP (proprietary UDP)
- **Port**: 50200 (UDP)
- **Software**: RDWorks, LightBurn
- **Market**: 70% of Chinese CO2 lasers
- **Features**: Auto-focus, rotary axis

#### Trocen Controllers
- **Models**: AWC608, AWC708C
- **Protocol**: Serial, Ethernet
- **Software**: LaserCAD, LightBurn
- **Market**: Budget CO2 lasers

### Professional Laser Systems

#### Epilog
- **Series**: Fusion, Zing, Mini/Helix
- **Protocol**: Print driver (Windows/Mac)
- **Interface**: Epilog Job Manager
- **Features**: 3D engraving, PhotoLaser Plus

#### Trotec
- **Series**: Speedy, SP series
- **Software**: JobControl
- **Protocol**: Proprietary, network
- **Features**: Vision system, automation

#### Universal Laser Systems
- **Control**: Universal Control Panel (UCP)
- **Protocol**: Print driver, direct
- **Features**: Multi-wavelength, automation

#### Glowforge
- **Protocol**: Cloud-only (no local control)
- **API**: Limited, cloud-dependent
- **Features**: Camera alignment, material detection

---

## Waterjet & Plasma Cutters

### Waterjet Systems

#### OMAX
- **Software**: Intelli-MAX, Make
- **Protocol**: Proprietary
- **Features**: Tilt-A-Jet, predictive models
- **Control**: PC-based

#### Flow/Hypertherm
- **Software**: FlowXpert
- **Protocol**: Proprietary
- **Features**: Dynamic Waterjet

### Plasma Systems

#### Hypertherm
- **Series**: EDGE Connect, MAXPRO
- **Protocol**: Proprietary, MTConnect option
- **Features**: True Hole technology

#### PlasmaCAM
- **Software**: DesignEdge
- **Control**: Integrated CNC

---

## Vinyl & Knife Cutters

### Consumer Cutters

#### Cricut
- **Models**: Maker 3, Explore 3
- **Protocol**: Proprietary, cloud-based
- **Software**: Design Space (cloud)
- **API**: None (closed system)

#### Silhouette
- **Models**: Cameo 4, Portrait 3
- **Software**: Silhouette Studio
- **Protocol**: GPGL (Graphtec)
- **Features**: Print & Cut

### Professional Cutters

#### Roland
- **Series**: CAMM-1, VersaCAMM
- **Software**: VersaWorks, CutStudio
- **Protocol**: CAMM-GL III

#### Graphtec
- **Series**: CE7000, FCX series
- **Protocol**: GP-GL, HP-GL
- **Software**: Graphtec Pro Studio

---

## PCB Fabrication

### PCB Mills

#### LPKF
- **Software**: CircuitPro PM
- **Protocol**: Proprietary
- **Features**: Multilayer, solder paste

#### Bantam Tools
- **Software**: Bantam Tools Desktop
- **Protocol**: G-code based
- **Features**: Tool changing, probing

### Pick & Place

#### OpenPnP
- **Type**: Open source
- **Protocol**: G-code, custom commands
- **Vision**: OpenCV-based

---

## Embroidery Machines

### Commercial Embroidery

#### Brother
- **Format**: PES, DST
- **Software**: PE-Design
- **Connection**: USB, card

#### Tajima
- **Format**: DST (Tajima standard)
- **Software**: DG/ML by Pulse
- **Features**: Network capability

---

## Robot Arms

### Collaborative Robots

#### Universal Robots
- **Series**: UR3e, UR5e, UR10e, UR16e
- **Protocol**: URScript, Modbus TCP
- **Port**: 30001-30004 (various interfaces)
- **API**: Dashboard Server, RTDE

#### ABB
- **Series**: YuMi, GoFa, CRB
- **Language**: RAPID
- **Protocol**: Robot Web Services
- **Interface**: RobotStudio

### Open Standards

#### ROS/ROS2
- **Protocol**: DDS (Data Distribution Service)
- **Messages**: sensor_msgs, geometry_msgs
- **Control**: MoveIt motion planning
- **Hardware**: Universal Robots, ABB, Fanuc drivers

---

## Official Standards

### G-code Standards
- **ISO 6983-1:2009**: Numerical control - Program format and definition
- **RS-274D**: Original US standard (basis for most implementations)
- **DIN 66025**: German G-code standard
- **ISO 14649 (STEP-NC)**: Next-generation CNC programming

### Communication Protocols
- **MTConnect**: ANSI/MTC1.4-2018
- **OPC UA**: IEC 62541 series
- **umati**: Universal machine tool interface
- **MQTT**: ISO/IEC 20922:2016
- **Modbus**: De facto standard (Modbus Organization)

### Safety Standards
- **ISO 13849**: Safety of machinery - Safety-related parts of control systems
- **IEC 61508**: Functional safety of electrical/electronic systems
- **ANSI/RIA R15.06**: Industrial robots and robot systems
- **ISO 10218**: Robots and robotic devices - Safety requirements

### File Format Standards
- **STL**: De facto for 3D printing (ASCII/Binary)
- **3MF**: 3D Manufacturing Format (3MF Consortium)
- **AMF**: ISO/ASTM 52915:2016
- **STEP**: ISO 10303 (CAD data exchange)
- **G-code**: ISO 6983-1:2009

---

## Implementation Priority Matrix

### Tier 1 - Essential (80% Coverage)
1. **GRBL/grblHAL**: Hobby CNCs, lasers, routers
2. **Marlin 2.0+**: Consumer 3D printers
3. **OctoPrint API**: Universal 3D printer control
4. **Ruida/RDWorks**: Chinese laser cutters
5. **Serial G-code**: Universal fallback

### Tier 2 - High Value (15% Coverage)
1. **Klipper/Moonraker**: Advanced 3D printing
2. **RepRapFirmware**: Premium printers
3. **LinuxCNC**: Professional CNCs
4. **MTConnect**: Industrial standard
5. **Bambu Lab MQTT**: Popular new ecosystem

### Tier 3 - Specialized (5% Coverage)
1. **Industrial CNCs**: Fanuc, Haas, Siemens
2. **Professional lasers**: Epilog, Trotec
3. **Waterjet/Plasma**: OMAX, Hypertherm
4. **Robot arms**: UR, ABB, ROS
5. **Specialty**: Vinyl, embroidery, PCB

---

## Compliance Tracking

Each adapter implementation must track compliance with relevant standards:

- ✅ **Full Compliance**: 100% standard implementation
- ⚠️ **Core Compliance**: Essential features (80%)
- ⚡ **Basic Compliance**: Minimum viable (60%)
- 🚧 **In Development**: Under active development
- 📋 **Planned**: On roadmap

See [PROTOCOL_COMPLIANCE_MATRIX.md](PROTOCOL_COMPLIANCE_MATRIX.md) for detailed tracking.