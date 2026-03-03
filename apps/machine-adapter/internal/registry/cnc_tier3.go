package registry

import "time"

// OneFinityWoodworkerDefinition returns the machine definition for the Onefinity
// Woodworker X-50. Uses the Buildbotics controller with an HTTP REST API.
func OneFinityWoodworkerDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Onefinity",
		Model:        "Woodworker X-50",
		Type:         MachineTypeCNC3Axis,
		Protocol:     ProtocolBuildbotics,
		Connection:   ConnectionHTTP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 816.0,
				"y_mm": 816.0,
				"z_mm": 133.0,
			},
		},
		Commands: map[string]CommandDef{
			"home": {
				Name:     "Home",
				Template: "PUT /api/home",
				Response: "",
				Timeout:  30 * time.Second,
			},
			"pause": {
				Name:     "Pause",
				Template: "PUT /api/pause",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"resume": {
				Name:     "Resume",
				Template: "PUT /api/unpause",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"stop": {
				Name:     "Stop",
				Template: "PUT /api/stop",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"gcode_line": {
				Name:       "Send G-code",
				Template:   "PUT /api/command {\"command\":\"{gcode}\"}",
				Parameters: []string{"gcode"},
				Response:   "",
				Timeout:    5 * time.Second,
			},
			"get_status": {
				Name:     "Get Status",
				Template: "GET /api/state",
				Response: "",
				Timeout:  2 * time.Second,
			},
		},
		StatusMapping: map[string]string{
			"READY":    "idle",
			"RUNNING":  "running",
			"HOLDING":  "paused",
			"ESTOPPED": "error",
		},
		TelemetryParse: map[string]TelemetryDef{
			"position": {
				Pattern:    `"x":([-\d.]+),"y":([-\d.]+),"z":([-\d.]+)`,
				MetricType: "position",
				Unit:       "mm",
				ValueIndex: 1,
			},
		},
	}
}

// LinuxCNCGenericDefinition returns the machine definition for a generic LinuxCNC
// machine. LinuxCNC exposes a command API over TCP for status and control.
// Work volume is configurable; defaults represent a common desktop setup.
func LinuxCNCGenericDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Generic",
		Model:        "LinuxCNC Machine",
		Type:         MachineTypeCNC3Axis,
		Protocol:     ProtocolLinuxCNC,
		Connection:   ConnectionTCP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 300.0,
				"y_mm": 300.0,
				"z_mm": 100.0,
			},
		},
		Commands: map[string]CommandDef{
			"home": {
				Name:     "Home All",
				Template: "home -1",
				Response: "",
				Timeout:  30 * time.Second,
			},
			"gcode_line": {
				Name:       "Send G-code",
				Template:   "mdi {gcode}",
				Parameters: []string{"gcode"},
				Response:   "",
				Timeout:    5 * time.Second,
			},
			"pause": {
				Name:     "Pause",
				Template: "pause",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"resume": {
				Name:     "Resume",
				Template: "resume",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"stop": {
				Name:     "Stop",
				Template: "abort",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"emergency_stop": {
				Name:     "Emergency Stop",
				Template: "estop",
				Response: "",
				Timeout:  1 * time.Second,
			},
			"get_status": {
				Name:     "Get Status",
				Template: "get interp_state",
				Response: "",
				Timeout:  2 * time.Second,
			},
		},
		StatusMapping: map[string]string{
			"IDLE":   "idle",
			"PAUSED": "paused",
			"EXEC":   "running",
			"ERROR":  "error",
		},
		TelemetryParse: map[string]TelemetryDef{
			"position": {
				Pattern:    `x:([-\d.]+)\s+y:([-\d.]+)\s+z:([-\d.]+)`,
				MetricType: "position",
				Unit:       "mm",
				ValueIndex: 1,
			},
		},
	}
}
