package registry

import "time"

// PrusaSL1SDefinition returns the machine definition for the Prusa SL1S Speed.
func PrusaSL1SDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Prusa",
		Model:        "SL1S Speed",
		Type:         MachineType3DPrinterSLA,
		Protocol:     ProtocolPrusaLink,
		Connection:   ConnectionHTTP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 127.0,
				"y_mm": 80.0,
				"z_mm": 150.0,
			},
			"resolution": map[string]interface{}{
				"xy_microns": 47.0,
			},
			"materials": map[string]interface{}{
				"materials": []string{"Standard Resin", "Tough Resin", "Flexible Resin", "Dental Resin", "Castable Resin"},
			},
		},
		Commands: map[string]CommandDef{
			"start_print": {
				Name:       "Start Print",
				Template:   "POST /api/v1/job",
				Parameters: []string{"file"},
				Response:   "200 OK",
				Timeout:    10 * time.Second,
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
		},
		StatusMapping: map[string]string{
			"IDLE":     "idle",
			"PRINTING": "running",
			"PAUSED":   "paused",
			"ERROR":    "error",
			"FINISHED": "idle",
		},
		TelemetryParse: map[string]TelemetryDef{
			"print_progress": {
				Pattern:    `"progress":\s*([\d.]+)`,
				MetricType: "print_progress",
				Unit:       "percent",
				ValueIndex: 1,
			},
			"uv_led_temp": {
				Pattern:    `"temp_uv_led":\s*([\d.]+)`,
				MetricType: "temperature_uv_led",
				Unit:       "celsius",
				ValueIndex: 1,
			},
			"ambient_temp": {
				Pattern:    `"temp_ambient":\s*([\d.]+)`,
				MetricType: "temperature_ambient",
				Unit:       "celsius",
				ValueIndex: 1,
			},
		},
	}
}
