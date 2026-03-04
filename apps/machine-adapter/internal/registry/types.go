// Package registry provides machine type definitions and capability management.
package registry

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// MachineType represents a category of fabrication machine.
type MachineType string

const (
	MachineTypeCNC3Axis       MachineType = "cnc_3axis"
	MachineTypeCNC5Axis       MachineType = "cnc_5axis"
	MachineTypeLaserCutter    MachineType = "laser_cutter"
	MachineType3DPrinterFDM   MachineType = "3d_printer_fdm"
	MachineType3DPrinterSLA   MachineType = "3d_printer_sla"
	MachineType3DPrinterSLS   MachineType = "3d_printer_sls"
	MachineTypeWaterjet       MachineType = "waterjet"
	MachineTypePlasmaCutter MachineType = "plasma_cutter"
	MachineTypeVinylCutter    MachineType = "vinyl_cutter"
	MachineTypeEmbroidery     MachineType = "embroidery"
	MachineTypePickAndPlace   MachineType = "pick_place"
	MachineTypeRobotArm       MachineType = "robot_arm"
	MachineTypePCBMill        MachineType = "pcb_mill"
	MachineTypeLaserEngraver  MachineType = "laser_engraver"
	MachineTypeMultiTool      MachineType = "multi_tool"
	MachineType3DPrinterResin MachineType = "3d_printer_resin"
)

// Protocol represents the communication protocol for a machine.
type Protocol string

const (
	ProtocolGCode     Protocol = "gcode"      // Standard G-code over serial/TCP
	ProtocolGRBL      Protocol = "grbl"       // GRBL-specific commands
	ProtocolMarlin    Protocol = "marlin"     // Marlin firmware (3D printers)
	ProtocolSmoothie  Protocol = "smoothie"   // Smoothieboard
	ProtocolDuet        Protocol = "duet"        // Duet3D RepRapFirmware
	ProtocolHaas        Protocol = "haas"        // Haas CNC protocol
	ProtocolFanuc       Protocol = "fanuc"       // Fanuc CNC protocol
	ProtocolMazak       Protocol = "mazak"       // Mazak CNC protocol
	ProtocolUniversal   Protocol = "universal"   // Universal Robots
	ProtocolModbus      Protocol = "modbus"      // Modbus TCP/RTU
	ProtocolOPCUA       Protocol = "opcua"       // OPC UA
	ProtocolMTConnect   Protocol = "mtconnect"   // MTConnect
	ProtocolCustom      Protocol = "custom"      // Vendor-specific
	ProtocolBambuMQTT   Protocol = "bambu_mqtt"  // Bambu Lab MQTT (TLS :8883)
	ProtocolMoonraker   Protocol = "moonraker"   // Klipper/Moonraker REST + WebSocket
	ProtocolOctoPrint   Protocol = "octoprint"   // OctoPrint REST + WebSocket
	ProtocolPrusaLink   Protocol = "prusalink"   // PrusaLink HTTP REST
	ProtocolRuida       Protocol = "ruida"       // Ruida laser controller UDP
	ProtocolCammGL      Protocol = "camm_gl"     // Roland CAMM-GL III serial
	ProtocolGPGL        Protocol = "gpgl"        // Graphtec GP-GL serial
	ProtocolOpenPnP     Protocol = "openpnp"     // OpenPnP G-code + HTTP
	ProtocolURScript    Protocol = "urscript"    // Universal Robots TCP :30002
	ProtocolXTool       Protocol = "xtool"       // xTool WiFi/Ethernet
	ProtocolFormlabs    Protocol = "formlabs"    // Formlabs Fleet Control REST
	ProtocolBuildbotics  Protocol = "buildbotics"  // Buildbotics controller REST
	ProtocolLinuxCNC    Protocol = "linuxcnc"    // LinuxCNC command API (TCP)
	ProtocolDobot       Protocol = "dobot"       // Dobot serial API
)

// ConnectionType represents how the machine connects.
type ConnectionType string

const (
	ConnectionSerial   ConnectionType = "serial"    // USB/RS232/RS485
	ConnectionTCP      ConnectionType = "tcp"       // Direct TCP/IP
	ConnectionHTTP     ConnectionType = "http"      // REST API
	ConnectionWebSocket ConnectionType = "websocket" // WebSocket
	ConnectionMQTT     ConnectionType = "mqtt"      // MQTT broker
	ConnectionUDP      ConnectionType = "udp"       // UDP datagrams (e.g. Ruida)
)

// MachineCapability describes what a machine can do.
type MachineCapability struct {
	ID          uuid.UUID              `json:"id"`
	MachineType MachineType            `json:"machine_type"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// Common capability definitions
var Capabilities = map[string]MachineCapability{
	"work_volume": {
		Name:        "Work Volume",
		Description: "Maximum working dimensions",
		Parameters: map[string]interface{}{
			"x_mm": 0.0,
			"y_mm": 0.0,
			"z_mm": 0.0,
		},
	},
	"spindle_speed": {
		Name:        "Spindle Speed",
		Description: "Spindle RPM range",
		Parameters: map[string]interface{}{
			"min_rpm": 0,
			"max_rpm": 0,
		},
	},
	"feed_rate": {
		Name:        "Feed Rate",
		Description: "Maximum feed rate",
		Parameters: map[string]interface{}{
			"max_mm_per_min": 0.0,
		},
	},
	"resolution": {
		Name:        "Resolution",
		Description: "Positioning accuracy",
		Parameters: map[string]interface{}{
			"xy_microns": 0.0,
			"z_microns":  0.0,
		},
	},
	"materials": {
		Name:        "Compatible Materials",
		Description: "Materials this machine can process",
		Parameters: map[string]interface{}{
			"materials": []string{},
		},
	},
	"tool_capacity": {
		Name:        "Tool Capacity",
		Description: "Automatic tool changer capacity",
		Parameters: map[string]interface{}{
			"positions": 0,
		},
	},
	"laser_power": {
		Name:        "Laser Power",
		Description: "Laser output power",
		Parameters: map[string]interface{}{
			"watts": 0.0,
		},
	},
	"nozzle_temp": {
		Name:        "Nozzle Temperature",
		Description: "3D printer nozzle temperature range",
		Parameters: map[string]interface{}{
			"min_celsius": 0.0,
			"max_celsius": 0.0,
		},
	},
	"bed_temp": {
		Name:        "Bed Temperature",
		Description: "3D printer bed temperature range",
		Parameters: map[string]interface{}{
			"min_celsius": 0.0,
			"max_celsius": 0.0,
		},
	},
	"print_speed": {
		Name:        "Print Speed",
		Description: "Maximum print speed",
		Parameters: map[string]interface{}{
			"max_mm_per_sec": 0.0,
		},
	},
	"camera": {
		Name:        "Camera",
		Description: "Built-in camera for monitoring",
		Parameters: map[string]interface{}{
			"supported":  false,
			"resolution": "",
		},
	},
	"filament_system": {
		Name:        "Filament System",
		Description: "Automatic filament management (AMS, MMU, etc.)",
		Parameters: map[string]interface{}{
			"type":  "",
			"slots": 0,
		},
	},
	"input_shaping": {
		Name:        "Input Shaping",
		Description: "Vibration compensation for high-speed printing",
		Parameters: map[string]interface{}{
			"supported": false,
		},
	},
	"multi_material": {
		Name:        "Multi-Material",
		Description: "Multi-material or multi-color printing support",
		Parameters: map[string]interface{}{
			"supported": false,
			"colors":    0,
		},
	},
	"cutting_force": {
		Name:        "Cutting Force",
		Description: "Maximum cutting force for vinyl/die cutters",
		Parameters: map[string]interface{}{
			"max_grams": 0,
		},
	},
	"vision_system": {
		Name:        "Vision System",
		Description: "Machine vision for alignment or inspection",
		Parameters: map[string]interface{}{
			"supported": false,
			"type":      "",
		},
	},
	"feeder_count": {
		Name:        "Feeder Count",
		Description: "Number of component feeders (pick-and-place)",
		Parameters: map[string]interface{}{
			"count": 0,
		},
	},
	"chamber_temp": {
		Name:        "Chamber Temperature",
		Description: "Heated chamber temperature range",
		Parameters: map[string]interface{}{
			"min_celsius": 0.0,
			"max_celsius": 0.0,
		},
	},
}

// MachineDefinition defines a specific machine model's capabilities.
type MachineDefinition struct {
	ID             uuid.UUID                `json:"id"`
	Manufacturer   string                   `json:"manufacturer"`
	Model          string                   `json:"model"`
	Type           MachineType              `json:"type"`
	Protocol       Protocol                 `json:"protocol"`
	Connection     ConnectionType           `json:"connection"`
	Capabilities   map[string]interface{}   `json:"capabilities"`
	Commands       map[string]CommandDef    `json:"commands"`
	StatusMapping  map[string]string        `json:"status_mapping"`
	TelemetryParse map[string]TelemetryDef  `json:"telemetry_parse"`
}

// CommandDef defines how to send a command to a machine.
type CommandDef struct {
	Name        string                 `json:"name"`
	Template    string                 `json:"template"`     // Command template with placeholders
	Parameters  []string               `json:"parameters"`   // Required parameters
	Response    string                 `json:"response"`     // Expected response pattern
	Timeout     time.Duration          `json:"timeout"`
	Validation  map[string]interface{} `json:"validation"`   // Parameter validation rules
}

// TelemetryDef defines how to parse telemetry data.
type TelemetryDef struct {
	Pattern    string `json:"pattern"`     // Regex pattern to match
	MetricType string `json:"metric_type"` // Metric name (temperature, position, etc.)
	Unit       string `json:"unit"`        // Unit of measurement
	ValueIndex int    `json:"value_index"` // Capture group index for value
}

// Registry manages machine definitions and capabilities.
type Registry struct {
	mu          sync.RWMutex
	definitions map[string]*MachineDefinition
	db          *sql.DB
	log         *logrus.Logger
}

// NewRegistry creates a new machine registry with builtin definitions.
func NewRegistry() *Registry {
	r := &Registry{
		definitions: make(map[string]*MachineDefinition),
	}
	r.loadBuiltinDefinitions()
	return r
}

// NewRegistryWithDB creates a registry that also loads persisted definitions from the database.
func NewRegistryWithDB(db *sql.DB, log *logrus.Logger) *Registry {
	r := &Registry{
		definitions: make(map[string]*MachineDefinition),
		db:          db,
		log:         log,
	}
	r.loadBuiltinDefinitions()
	if db != nil {
		r.loadPersistedDefinitions()
	}
	return r
}

// loadBuiltinDefinitions loads standard machine definitions.
func (r *Registry) loadBuiltinDefinitions() {
	// Example: Generic GRBL CNC
	r.definitions["grbl_generic"] = &MachineDefinition{
		Manufacturer: "Generic",
		Model:        "GRBL CNC",
		Type:         MachineTypeCNC3Axis,
		Protocol:     ProtocolGRBL,
		Connection:   ConnectionSerial,
		Commands: map[string]CommandDef{
			"home": {
				Name:     "Home",
				Template: "$H",
				Response: "ok",
				Timeout:  30 * time.Second,
			},
			"status": {
				Name:     "Status",
				Template: "?",
				Response: "<.*>",
				Timeout:  1 * time.Second,
			},
			"pause": {
				Name:     "Pause",
				Template: "!",
				Response: "ok",
				Timeout:  1 * time.Second,
			},
			"resume": {
				Name:     "Resume",
				Template: "~",
				Response: "ok",
				Timeout:  1 * time.Second,
			},
			"stop": {
				Name:     "Stop",
				Template: "\x18", // Ctrl-X
				Response: "ok",
				Timeout:  1 * time.Second,
			},
		},
		StatusMapping: map[string]string{
			"Idle":  "idle",
			"Run":   "running",
			"Hold":  "paused",
			"Alarm": "error",
		},
		TelemetryParse: map[string]TelemetryDef{
			"position": {
				Pattern:    `MPos:([-\d.]+),([-\d.]+),([-\d.]+)`,
				MetricType: "position",
				Unit:       "mm",
				ValueIndex: 1, // X position
			},
		},
	}

	// Snapmaker 2.0 A350 (multi-tool: 3DP + Laser + CNC)
	r.definitions["snapmaker_a350"] = SnapmakerA350Definition()

	// Bambu Lab printers (MQTT over TLS)
	r.definitions["bambu_p1s"] = BambuP1SDefinition()
	r.definitions["bambu_a1"] = BambuA1Definition()
	r.definitions["bambu_x1c"] = BambuX1CDefinition()

	// Ruida RDC6445 Generic (CO2 laser cutter)
	r.definitions["ruida_generic"] = RuidaGenericDefinition()

	// Klipper/Moonraker-based printers
	r.definitions["creality_k1_max"] = CrealityK1MaxDefinition()
	r.definitions["voron_2_4"] = Voron24Definition()
	r.definitions["ratrig_vcore4"] = RatRigVCore4Definition()

	// Prusa PrusaLink-based printers
	r.definitions["prusa_core_one"] = PrusaCoreOneDefinition()
	r.definitions["prusa_mini_plus"] = PrusaMiniPlusDefinition()
	r.definitions["prusa_sl1s"] = PrusaSL1SDefinition()

	// xTool laser machines (HTTP REST)
	r.definitions["xtool_p2s"] = XToolP2SDefinition()
	r.definitions["xtool_s1"] = XToolS1Definition()
	r.definitions["xtool_f1_ultra"] = XToolF1UltraDefinition()

	// Roland CAMM-GL vinyl cutters (serial)
	r.definitions["roland_gr2_640"] = RolandGR2Definition()
	r.definitions["roland_gs2_24"] = RolandGS2Definition()

	// Duet3D Generic (RepRapFirmware over HTTP)
	r.definitions["duet_generic"] = DuetGenericDefinition()

	// Ultimaker S5 (REST API)
	r.definitions["ultimaker_s5"] = UltimakerS5Definition()

	// Formlabs Form 4 (Fleet Control REST API)
	r.definitions["formlabs_form4"] = FormlabsForm4Definition()

	// Resin printers (registry-only, proprietary protocols)
	r.definitions["elegoo_saturn4_ultra"] = ElegooSaturn4UltraDefinition()
	r.definitions["anycubic_photon_mono_m7"] = AnycubicPhotonMonoM7Definition()
	r.definitions["phrozen_sonic_mighty_8k"] = PhrozenSonicMighty8KDefinition()

	// Laser machines (registry-only, limited/cloud APIs)
	r.definitions["glowforge_pro"] = GlowforgeProDefinition()
	r.definitions["trotec_speedy_360"] = TrotecSpeedyDefinition()

	// Specialty machines (registry-only, proprietary protocols)
	r.definitions["silhouette_cameo_5"] = SilhouetteCameo5Definition()
	r.definitions["neoden_yy1"] = NeodenYY1Definition()
	r.definitions["bantam_tools_pcb_mill"] = BantamToolsPCBMillDefinition()
	r.definitions["brother_pe800"] = BrotherPE800Definition()

	// CNC tier 3 (Buildbotics / LinuxCNC)
	r.definitions["onefinity_woodworker"] = OneFinityWoodworkerDefinition()
	r.definitions["linuxcnc_generic"] = LinuxCNCGenericDefinition()

	// Pick-and-place and robot arms
	r.definitions["lumen_pnp"] = LumenPnPDefinition()
	r.definitions["index_pnp"] = IndexPnPDefinition()
	r.definitions["ur_generic"] = URGenericDefinition()
	r.definitions["dobot_magician"] = DobotMagicianDefinition()

	// Additional GRBL CNC machines
	r.definitions["shapeoko_hdm"] = ShapeokoHDMDefinition()
	r.definitions["xcarve_pro"] = XCarveProDefinition()
	r.definitions["openbuilds_lead"] = OpenBuildsLeadDefinition()
	r.definitions["sienci_longmill_mk2"] = SienciLongMillMK2Definition()
	r.definitions["stepcraft_d840"] = StepcraftD840Definition()
	r.definitions["creality_falcon2"] = CrealityFalcon2Definition()

	// GPGL vinyl cutter
	r.definitions["graphtec_ce8000"] = GraphtecCE8000Definition()

	// Example: Prusa 3D Printer
	r.definitions["prusa_mk4"] = &MachineDefinition{
		Manufacturer: "Prusa",
		Model:        "MK4",
		Type:         MachineType3DPrinterFDM,
		Protocol:     ProtocolMarlin,
		Connection:   ConnectionSerial,
		Commands: map[string]CommandDef{
			"home": {
				Name:     "Home",
				Template: "G28",
				Response: "ok",
				Timeout:  60 * time.Second,
			},
			"temp_extruder": {
				Name:       "Set Extruder Temp",
				Template:   "M104 S{temp}",
				Parameters: []string{"temp"},
				Response:   "ok",
				Timeout:    5 * time.Second,
				Validation: map[string]interface{}{
					"temp": map[string]interface{}{
						"min": 0,
						"max": 300,
					},
				},
			},
			"temp_bed": {
				Name:       "Set Bed Temp",
				Template:   "M140 S{temp}",
				Parameters: []string{"temp"},
				Response:   "ok",
				Timeout:    5 * time.Second,
				Validation: map[string]interface{}{
					"temp": map[string]interface{}{
						"min": 0,
						"max": 120,
					},
				},
			},
		},
		TelemetryParse: map[string]TelemetryDef{
			"temp_extruder": {
				Pattern:    `T:([\d.]+) /([\d.]+)`,
				MetricType: "temperature_extruder",
				Unit:       "celsius",
				ValueIndex: 1,
			},
			"temp_bed": {
				Pattern:    `B:([\d.]+) /([\d.]+)`,
				MetricType: "temperature_bed",
				Unit:       "celsius",
				ValueIndex: 1,
			},
		},
	}
}

// GetDefinition retrieves a machine definition by ID.
func (r *Registry) GetDefinition(id string) (*MachineDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.definitions[id]
	return def, ok
}

// ListDefinitions returns all available machine definitions.
func (r *Registry) ListDefinitions() map[string]*MachineDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// Return a copy to prevent concurrent map access
	copy := make(map[string]*MachineDefinition, len(r.definitions))
	for k, v := range r.definitions {
		copy[k] = v
	}
	return copy
}

// RegisterDefinition adds a new machine definition at runtime.
func (r *Registry) RegisterDefinition(id string, def *MachineDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.definitions[id] = def
}

// DeleteDefinition removes a machine definition.
func (r *Registry) DeleteDefinition(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.definitions[id]; !ok {
		return false
	}
	delete(r.definitions, id)
	return true
}

// PersistDefinition saves a definition to the database for reload on restart.
func (r *Registry) PersistDefinition(id string, def *MachineDefinition) error {
	if r.db == nil {
		return nil
	}
	defJSON, err := json.Marshal(def)
	if err != nil {
		return fmt.Errorf("marshal definition: %w", err)
	}

	query := `INSERT INTO machine_protocols (id, protocol_id, definition)
		VALUES ($1, $2, $3)
		ON CONFLICT (protocol_id) DO UPDATE SET definition = $3, updated_at = NOW()`
	_, err = r.db.Exec(query, def.ID, id, defJSON)
	if err != nil {
		return fmt.Errorf("persist definition: %w", err)
	}
	return nil
}

// DeletePersistedDefinition removes a definition from the database.
func (r *Registry) DeletePersistedDefinition(id string) error {
	if r.db == nil {
		return nil
	}
	_, err := r.db.Exec(`DELETE FROM machine_protocols WHERE protocol_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete persisted definition: %w", err)
	}
	return nil
}

// loadPersistedDefinitions loads runtime-added definitions from the database.
func (r *Registry) loadPersistedDefinitions() {
	rows, err := r.db.Query(`SELECT protocol_id, definition FROM machine_protocols`)
	if err != nil {
		if r.log != nil {
			r.log.WithError(err).Warn("Failed to load persisted machine definitions (table may not exist)")
		}
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id string
		var defJSON []byte
		if err := rows.Scan(&id, &defJSON); err != nil {
			if r.log != nil {
				r.log.WithError(err).Warn("Failed to scan persisted definition")
			}
			continue
		}

		var def MachineDefinition
		if err := json.Unmarshal(defJSON, &def); err != nil {
			if r.log != nil {
				r.log.WithError(err).WithField("id", id).Warn("Failed to unmarshal persisted definition")
			}
			continue
		}

		r.definitions[id] = &def
		count++
	}

	if r.log != nil && count > 0 {
		r.log.WithField("count", count).Info("Loaded persisted machine definitions")
	}
}