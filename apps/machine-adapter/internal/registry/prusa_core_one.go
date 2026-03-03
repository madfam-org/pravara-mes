package registry

import "time"

// PrusaCoreOneDefinition returns the machine definition for the Prusa Core One.
func PrusaCoreOneDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Prusa",
		Model:        "Core One",
		Type:         MachineType3DPrinterFDM,
		Protocol:     ProtocolPrusaLink,
		Connection:   ConnectionHTTP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 250.0,
				"y_mm": 220.0,
				"z_mm": 270.0,
			},
			"nozzle_temp": map[string]interface{}{
				"min_celsius": 0.0,
				"max_celsius": 300.0,
			},
			"bed_temp": map[string]interface{}{
				"min_celsius": 0.0,
				"max_celsius": 120.0,
			},
			"print_speed": map[string]interface{}{
				"max_mm_per_sec": 200.0,
			},
			"input_shaping": map[string]interface{}{
				"supported": true,
			},
			"materials": map[string]interface{}{
				"materials": []string{"PLA", "PETG", "ASA", "ABS", "PA", "PC", "PVB", "PP", "HIPS", "FLEX"},
			},
		},
		Commands: map[string]CommandDef{
			"home": {
				Name:     "Home All Axes",
				Template: "POST /api/v1/gcode {\"command\":\"G28\"}",
				Response: "200 OK",
				Timeout:  60 * time.Second,
			},
			"pause": {
				Name:     "Pause Print",
				Template: "PUT /api/v1/job {\"command\":\"pause\"}",
				Response: "200 OK",
				Timeout:  5 * time.Second,
			},
			"resume": {
				Name:     "Resume Print",
				Template: "PUT /api/v1/job {\"command\":\"resume\"}",
				Response: "200 OK",
				Timeout:  5 * time.Second,
			},
			"stop": {
				Name:     "Stop Print",
				Template: "DELETE /api/v1/job",
				Response: "204 No Content",
				Timeout:  5 * time.Second,
			},
			"get_status": {
				Name:     "Get Printer Status",
				Template: "GET /api/v1/status",
				Response: "200 OK",
				Timeout:  5 * time.Second,
			},
			"get_temperature": {
				Name:     "Get Temperature",
				Template: "GET /api/v1/status",
				Response: "200 OK",
				Timeout:  5 * time.Second,
			},
		},
		StatusMapping: map[string]string{
			"IDLE":     "idle",
			"PRINTING": "running",
			"PAUSED":   "paused",
			"ERROR":    "error",
			"FINISHED": "idle",
		},
		TelemetryParse: map[string]TelemetryDef{
			"nozzle_temp": {
				Pattern:    `"temp_nozzle":\s*([\d.]+)`,
				MetricType: "temperature_nozzle",
				Unit:       "celsius",
				ValueIndex: 1,
			},
			"bed_temp": {
				Pattern:    `"temp_bed":\s*([\d.]+)`,
				MetricType: "temperature_bed",
				Unit:       "celsius",
				ValueIndex: 1,
			},
			"print_progress": {
				Pattern:    `"progress":\s*([\d.]+)`,
				MetricType: "print_progress",
				Unit:       "percent",
				ValueIndex: 1,
			},
		},
	}
}
