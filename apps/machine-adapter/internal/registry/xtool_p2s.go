package registry

import "time"

// XToolP2SDefinition returns the machine definition for the xTool P2S CO2 laser cutter.
func XToolP2SDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "xTool",
		Model:        "P2S",
		Type:         MachineTypeLaserCutter,
		Protocol:     ProtocolXTool,
		Connection:   ConnectionHTTP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 600.0,
				"y_mm": 400.0,
				"z_mm": 0.0,
			},
			"laser_power": map[string]interface{}{
				"watts": 55.0,
				"type":  "CO2",
			},
			"camera": map[string]interface{}{
				"supported":  true,
				"resolution": "16MP",
			},
		},
		Commands: xtoolCommonCommands(),
		StatusMapping: map[string]string{
			"idle":    "idle",
			"running": "running",
			"paused":  "paused",
			"error":   "error",
		},
		TelemetryParse: xtoolCommonTelemetry(),
	}
}

// xtoolCommonCommands returns the command definitions shared across xTool laser machines.
func xtoolCommonCommands() map[string]CommandDef {
	return map[string]CommandDef{
		"home": {
			Name:     "Home",
			Template: "G28",
			Response: "ok",
			Timeout:  30 * time.Second,
		},
		"pause": {
			Name:     "Pause Job",
			Template: "POST /api/pause",
			Response: "",
			Timeout:  5 * time.Second,
		},
		"resume": {
			Name:     "Resume Job",
			Template: "POST /api/start",
			Response: "",
			Timeout:  5 * time.Second,
		},
		"stop": {
			Name:     "Stop Job",
			Template: "POST /api/stop",
			Response: "",
			Timeout:  5 * time.Second,
		},
		"gcode_line": {
			Name:       "Send G-code Line",
			Template:   `POST /api/gcode {"gcode":"{gcode}"}`,
			Parameters: []string{"gcode"},
			Response:   "",
			Timeout:    30 * time.Second,
		},
		"get_status": {
			Name:     "Get Status",
			Template: "GET /api/status",
			Response: "",
			Timeout:  5 * time.Second,
		},
	}
}

// xtoolCommonTelemetry returns the telemetry parse definitions shared across xTool laser machines.
func xtoolCommonTelemetry() map[string]TelemetryDef {
	return map[string]TelemetryDef{
		"machine_state": {
			Pattern:    `"state":\s*"(\w+)"`,
			MetricType: "machine_state",
			Unit:       "state",
			ValueIndex: 1,
		},
		"laser_temp": {
			Pattern:    `"laser_temp":\s*([\d.]+)`,
			MetricType: "laser_temp",
			Unit:       "celsius",
			ValueIndex: 1,
		},
		"job_progress": {
			Pattern:    `"job_progress":\s*([\d.]+)`,
			MetricType: "job_progress",
			Unit:       "percent",
			ValueIndex: 1,
		},
	}
}
