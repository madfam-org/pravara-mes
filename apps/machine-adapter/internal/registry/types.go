// Package registry provides machine type definitions and capability management.
package registry

import (
	"time"

	"github.com/google/uuid"
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
)

// Protocol represents the communication protocol for a machine.
type Protocol string

const (
	ProtocolGCode     Protocol = "gcode"      // Standard G-code over serial/TCP
	ProtocolGRBL      Protocol = "grbl"       // GRBL-specific commands
	ProtocolMarlin    Protocol = "marlin"     // Marlin firmware (3D printers)
	ProtocolSmoothie  Protocol = "smoothie"   // Smoothieboard
	ProtocolDuet      Protocol = "duet"       // Duet3D RepRapFirmware
	ProtocolHaas      Protocol = "haas"       // Haas CNC protocol
	ProtocolFanuc     Protocol = "fanuc"      // Fanuc CNC protocol
	ProtocolMazak     Protocol = "mazak"      // Mazak CNC protocol
	ProtocolUniversal Protocol = "universal"  // Universal Robots
	ProtocolModbus    Protocol = "modbus"     // Modbus TCP/RTU
	ProtocolOPCUA     Protocol = "opcua"      // OPC UA
	ProtocolMTConnect Protocol = "mtconnect"  // MTConnect
	ProtocolCustom    Protocol = "custom"     // Vendor-specific
)

// ConnectionType represents how the machine connects.
type ConnectionType string

const (
	ConnectionSerial   ConnectionType = "serial"    // USB/RS232/RS485
	ConnectionTCP      ConnectionType = "tcp"       // Direct TCP/IP
	ConnectionHTTP     ConnectionType = "http"      // REST API
	ConnectionWebSocket ConnectionType = "websocket" // WebSocket
	ConnectionMQTT     ConnectionType = "mqtt"      // MQTT broker
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
	definitions map[string]*MachineDefinition
}

// NewRegistry creates a new machine registry.
func NewRegistry() *Registry {
	r := &Registry{
		definitions: make(map[string]*MachineDefinition),
	}
	r.loadBuiltinDefinitions()
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
	def, ok := r.definitions[id]
	return def, ok
}

// ListDefinitions returns all available machine definitions.
func (r *Registry) ListDefinitions() map[string]*MachineDefinition {
	return r.definitions
}

// RegisterDefinition adds a new machine definition.
func (r *Registry) RegisterDefinition(id string, def *MachineDefinition) {
	r.definitions[id] = def
}