package registry

import "time"

// BambuP1SDefinition returns the machine definition for the Bambu Lab P1S.
func BambuP1SDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Bambu Lab",
		Model:        "P1S",
		Type:         MachineType3DPrinterFDM,
		Protocol:     ProtocolBambuMQTT,
		Connection:   ConnectionMQTT,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 256.0,
				"y_mm": 256.0,
				"z_mm": 256.0,
			},
			"nozzle_temp": map[string]interface{}{
				"min_celsius": 0.0,
				"max_celsius": 300.0,
			},
			"bed_temp": map[string]interface{}{
				"min_celsius": 0.0,
				"max_celsius": 110.0,
			},
			"chamber_temp": map[string]interface{}{
				"type": "passive",
			},
			"print_speed": map[string]interface{}{
				"max_mm_per_sec": 500.0,
			},
			"camera": map[string]interface{}{
				"supported":  true,
				"resolution": "1080p",
			},
			"filament_system": map[string]interface{}{
				"type":  "AMS",
				"slots": 4,
			},
			"input_shaping": map[string]interface{}{
				"supported": true,
			},
			"multi_material": map[string]interface{}{
				"supported": true,
				"colors":    4,
			},
		},
		Commands: bambuCommonCommands(),
		StatusMapping: map[string]string{
			"IDLE":    "idle",
			"RUNNING": "running",
			"PAUSE":   "paused",
			"FAILED":  "error",
			"FINISH":  "idle",
		},
		TelemetryParse: bambuCommonTelemetry(),
	}
}

// bambuCommonCommands returns the command definitions shared across Bambu Lab printers.
func bambuCommonCommands() map[string]CommandDef {
	return map[string]CommandDef{
		"home": {
			Name:     "Home All Axes",
			Template: `{"print":{"command":"gcode_line","param":"G28\n","sequence_id":"{seq}"}}`,
			Response: "",
			Timeout:  60 * time.Second,
		},
		"pause": {
			Name:     "Pause Print",
			Template: `{"print":{"command":"pause","sequence_id":"{seq}"}}`,
			Response: "",
			Timeout:  5 * time.Second,
		},
		"resume": {
			Name:     "Resume Print",
			Template: `{"print":{"command":"resume","sequence_id":"{seq}"}}`,
			Response: "",
			Timeout:  5 * time.Second,
		},
		"stop": {
			Name:     "Stop Print",
			Template: `{"print":{"command":"stop","sequence_id":"{seq}"}}`,
			Response: "",
			Timeout:  5 * time.Second,
		},
		"push_status": {
			Name:     "Request Status Update",
			Template: `{"print":{"command":"push_status","sequence_id":"{seq}"}}`,
			Response: "",
			Timeout:  5 * time.Second,
		},
		"gcode_line": {
			Name:       "Send G-code Line",
			Template:   `{"print":{"command":"gcode_line","param":"{gcode}\n","sequence_id":"{seq}"}}`,
			Parameters: []string{"gcode"},
			Response:   "",
			Timeout:    30 * time.Second,
		},
		"emergency_stop": {
			Name:     "Emergency Stop",
			Template: `{"print":{"command":"gcode_line","param":"M112\n","sequence_id":"{seq}"}}`,
			Response: "",
			Timeout:  1 * time.Second,
		},
	}
}

// bambuCommonTelemetry returns the telemetry parse definitions shared across Bambu Lab printers.
func bambuCommonTelemetry() map[string]TelemetryDef {
	return map[string]TelemetryDef{
		"nozzle_temp": {
			Pattern:    `"nozzle_temper":\s*([\d.]+)`,
			MetricType: "nozzle_temp",
			Unit:       "celsius",
			ValueIndex: 1,
		},
		"bed_temp": {
			Pattern:    `"bed_temper":\s*([\d.]+)`,
			MetricType: "bed_temp",
			Unit:       "celsius",
			ValueIndex: 1,
		},
		"chamber_temp": {
			Pattern:    `"chamber_temper":\s*([\d.]+)`,
			MetricType: "chamber_temp",
			Unit:       "celsius",
			ValueIndex: 1,
		},
		"print_percent": {
			Pattern:    `"mc_percent":\s*(\d+)`,
			MetricType: "print_percent",
			Unit:       "percent",
			ValueIndex: 1,
		},
	}
}
