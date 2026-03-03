package registry

import "time"

// FormlabsForm4Definition returns the machine definition for the Formlabs Form 4.
func FormlabsForm4Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Formlabs",
		Model:        "Form 4",
		Type:         MachineType3DPrinterSLA,
		Protocol:     ProtocolFormlabs,
		Connection:   ConnectionHTTP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 200.0,
				"y_mm": 125.0,
				"z_mm": 210.0,
			},
			"resolution": map[string]interface{}{
				"xy_microns": 50.0,
				"z_microns":  25.0,
			},
			"materials": map[string]interface{}{
				"materials": []string{
					"Standard Resin", "Tough Resin", "Durable Resin",
					"Flexible Resin", "Castable Wax", "Dental Resin",
					"Biocompatible Resin", "ESD Resin",
				},
			},
		},
		Commands: map[string]CommandDef{
			"get_status": {
				Name:     "Get Printer Status",
				Template: "GET /api/v1/printers/{id}",
				Response: "json",
				Timeout:  5 * time.Second,
			},
			"pause": {
				Name:     "Pause Print",
				Template: "POST /api/v1/printers/{id}/command {\"action\":\"pause\"}",
				Response: "200",
				Timeout:  5 * time.Second,
			},
			"resume": {
				Name:     "Resume Print",
				Template: "POST /api/v1/printers/{id}/command {\"action\":\"resume\"}",
				Response: "200",
				Timeout:  5 * time.Second,
			},
			"stop": {
				Name:     "Cancel Print",
				Template: "POST /api/v1/printers/{id}/command {\"action\":\"cancel\"}",
				Response: "200",
				Timeout:  10 * time.Second,
			},
		},
		StatusMapping: map[string]string{
			"idle":     "idle",
			"printing": "running",
			"paused":   "paused",
			"error":    "error",
		},
		TelemetryParse: map[string]TelemetryDef{
			"print_progress": {
				Pattern:    `"progress":([\d.]+)`,
				MetricType: "print_progress",
				Unit:       "percent",
				ValueIndex: 1,
			},
			"printer_state": {
				Pattern:    `"state":"(\w+)"`,
				MetricType: "printer_state",
				Unit:       "enum",
				ValueIndex: 1,
			},
			"resin_temp": {
				Pattern:    `"resin_temperature":([\d.]+)`,
				MetricType: "resin_temperature",
				Unit:       "celsius",
				ValueIndex: 1,
			},
			"tank_level": {
				Pattern:    `"tank_level":([\d.]+)`,
				MetricType: "tank_level",
				Unit:       "percent",
				ValueIndex: 1,
			},
		},
	}
}
