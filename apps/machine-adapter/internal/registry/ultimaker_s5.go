package registry

import "time"

// UltimakerS5Definition returns the machine definition for the Ultimaker S5.
func UltimakerS5Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Ultimaker",
		Model:        "S5",
		Type:         MachineType3DPrinterFDM,
		Protocol:     ProtocolCustom, // Ultimaker-specific REST API
		Connection:   ConnectionHTTP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 330.0,
				"y_mm": 240.0,
				"z_mm": 300.0,
			},
			"nozzle_temp": map[string]interface{}{
				"min_celsius": 0.0,
				"max_celsius": 280.0,
			},
			"bed_temp": map[string]interface{}{
				"min_celsius": 0.0,
				"max_celsius": 140.0,
			},
			"dual_extrusion": map[string]interface{}{
				"supported":      true,
				"extruder_count": 2,
			},
			"materials": map[string]interface{}{
				"materials": []string{
					"PLA", "ABS", "CPE", "CPE+", "Nylon",
					"PC", "TPU 95A", "PP", "PVA", "Breakaway",
				},
			},
			"camera": map[string]interface{}{
				"supported":  true,
				"resolution": "720p",
			},
		},
		Commands: map[string]CommandDef{
			"home": {
				Name:     "Home (Managed by Firmware)",
				Template: "GET /api/v1/printer",
				Response: "json",
				Timeout:  5 * time.Second,
			},
			"pause": {
				Name:     "Pause Print",
				Template: "PUT /api/v1/print_job/state {\"target\":\"pause\"}",
				Response: "200",
				Timeout:  5 * time.Second,
			},
			"resume": {
				Name:     "Resume Print",
				Template: "PUT /api/v1/print_job/state {\"target\":\"print\"}",
				Response: "200",
				Timeout:  5 * time.Second,
			},
			"stop": {
				Name:     "Abort Print",
				Template: "PUT /api/v1/print_job/state {\"target\":\"abort\"}",
				Response: "200",
				Timeout:  10 * time.Second,
			},
			"get_status": {
				Name:     "Get Printer Status",
				Template: "GET /api/v1/printer",
				Response: "json",
				Timeout:  2 * time.Second,
			},
			"get_temperature": {
				Name:     "Get Hotend Temperature",
				Template: "GET /api/v1/printer/heads/0/extruders/0/hotend/temperature",
				Response: "json",
				Timeout:  2 * time.Second,
			},
			"set_temp": {
				Name:       "Set Hotend Target Temperature",
				Template:   "POST /api/v1/printer/heads/0/extruders/0/hotend/temperature/target {\"target\":{temp}}",
				Parameters: []string{"temp"},
				Response:   "200",
				Timeout:    5 * time.Second,
				Validation: map[string]interface{}{
					"temp": map[string]interface{}{
						"min": 0,
						"max": 280,
					},
				},
			},
		},
		StatusMapping: map[string]string{
			"idle":        "idle",
			"printing":    "running",
			"paused":      "paused",
			"error":       "error",
			"maintenance": "idle",
			"booting":     "idle",
		},
		TelemetryParse: map[string]TelemetryDef{
			"extruder_temp": {
				Pattern:    `"current":([\d.]+)`,
				MetricType: "temperature_extruder",
				Unit:       "celsius",
				ValueIndex: 1,
			},
			"bed_temp": {
				Pattern:    `"current":([\d.]+)`,
				MetricType: "temperature_bed",
				Unit:       "celsius",
				ValueIndex: 1,
			},
			"print_progress": {
				Pattern:    `"progress":([\d.]+)`,
				MetricType: "print_progress",
				Unit:       "percent",
				ValueIndex: 1,
			},
			"print_state": {
				Pattern:    `"state":"(\w+)"`,
				MetricType: "print_state",
				Unit:       "enum",
				ValueIndex: 1,
			},
		},
	}
}
