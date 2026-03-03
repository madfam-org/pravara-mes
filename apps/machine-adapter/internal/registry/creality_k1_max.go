package registry

import "time"

// CrealityK1MaxDefinition returns the machine definition for the Creality K1 Max.
func CrealityK1MaxDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Creality",
		Model:        "K1 Max",
		Type:         MachineType3DPrinterFDM,
		Protocol:     ProtocolMoonraker,
		Connection:   ConnectionHTTP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 300.0,
				"y_mm": 300.0,
				"z_mm": 300.0,
			},
			"nozzle_temp": map[string]interface{}{
				"min_celsius": 0.0,
				"max_celsius": 300.0,
			},
			"bed_temp": map[string]interface{}{
				"min_celsius": 0.0,
				"max_celsius": 100.0,
			},
			"print_speed": map[string]interface{}{
				"max_mm_per_sec": 600.0,
			},
			"camera": map[string]interface{}{
				"supported":  true,
				"resolution": "1080p",
				"ai":         true,
			},
			"input_shaping": map[string]interface{}{
				"supported": true,
			},
			"materials": map[string]interface{}{
				"materials": []string{"PLA", "ABS", "PETG", "TPU", "PA", "ASA"},
			},
		},
		Commands: map[string]CommandDef{
			"home": {
				Name:     "Home All Axes",
				Template: "G28",
				Response: "ok",
				Timeout:  60 * time.Second,
			},
			"pause": {
				Name:     "Pause Print",
				Template: "PAUSE",
				Response: "ok",
				Timeout:  10 * time.Second,
			},
			"resume": {
				Name:     "Resume Print",
				Template: "RESUME",
				Response: "ok",
				Timeout:  10 * time.Second,
			},
			"stop": {
				Name:     "Cancel Print",
				Template: "CANCEL_PRINT",
				Response: "ok",
				Timeout:  10 * time.Second,
			},
			"emergency_stop": {
				Name:     "Emergency Stop",
				Template: "M112",
				Response: "",
				Timeout:  1 * time.Second,
			},
			"gcode_line": {
				Name:       "Send G-code Line",
				Template:   "{line}",
				Parameters: []string{"line"},
				Response:   "ok",
				Timeout:    30 * time.Second,
			},
			"get_temperature": {
				Name:     "Get Temperature",
				Template: "M105",
				Response: "ok",
				Timeout:  2 * time.Second,
			},
			"set_temp_extruder": {
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
			"set_temp_bed": {
				Name:       "Set Bed Temp",
				Template:   "M140 S{temp}",
				Parameters: []string{"temp"},
				Response:   "ok",
				Timeout:    5 * time.Second,
				Validation: map[string]interface{}{
					"temp": map[string]interface{}{
						"min": 0,
						"max": 100,
					},
				},
			},
		},
		StatusMapping: map[string]string{
			"standby":  "idle",
			"printing": "running",
			"paused":   "paused",
			"error":    "error",
			"complete": "idle",
		},
		TelemetryParse: map[string]TelemetryDef{
			"extruder_temp": {
				Pattern:    `T:([\d.]+)\s*/\s*([\d.]+)`,
				MetricType: "extruder_temp",
				Unit:       "celsius",
				ValueIndex: 1,
			},
			"bed_temp": {
				Pattern:    `B:([\d.]+)\s*/\s*([\d.]+)`,
				MetricType: "bed_temp",
				Unit:       "celsius",
				ValueIndex: 1,
			},
			"print_progress": {
				Pattern:    `progress:([\d.]+)`,
				MetricType: "print_progress",
				Unit:       "ratio",
				ValueIndex: 1,
			},
		},
	}
}
