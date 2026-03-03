package registry

import "time"

// DuetGenericDefinition returns the machine definition for a generic Duet 3
// controller running RepRapFirmware. The work volume defaults to 300x300x300mm
// and should be overridden per-machine during registration.
func DuetGenericDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Duet3D",
		Model:        "Duet 3 Generic",
		Type:         MachineType3DPrinterFDM,
		Protocol:     ProtocolDuet,
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
				"max_celsius": 120.0,
			},
		},
		Commands: map[string]CommandDef{
			"home": {
				Name:     "Home All Axes",
				Template: "GET /rr_gcode?gcode=G28",
				Response: "ok",
				Timeout:  60 * time.Second,
			},
			"pause": {
				Name:     "Pause Print",
				Template: "GET /rr_gcode?gcode=M25",
				Response: "ok",
				Timeout:  5 * time.Second,
			},
			"resume": {
				Name:     "Resume Print",
				Template: "GET /rr_gcode?gcode=M24",
				Response: "ok",
				Timeout:  5 * time.Second,
			},
			"stop": {
				Name:     "Stop Print",
				Template: "GET /rr_gcode?gcode=M0",
				Response: "ok",
				Timeout:  10 * time.Second,
			},
			"emergency_stop": {
				Name:     "Emergency Stop",
				Template: "GET /rr_gcode?gcode=M112",
				Response: "",
				Timeout:  1 * time.Second,
			},
			"gcode_line": {
				Name:       "Execute G-code",
				Template:   "GET /rr_gcode?gcode={gcode}",
				Parameters: []string{"gcode"},
				Response:   "ok",
				Timeout:    30 * time.Second,
			},
			"get_temperature": {
				Name:     "Get Temperatures",
				Template: "GET /rr_status?type=3",
				Response: "json",
				Timeout:  2 * time.Second,
			},
			"set_temp_extruder": {
				Name:       "Set Extruder Temperature",
				Template:   "GET /rr_gcode?gcode=M104+S{temp}",
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
				Name:       "Set Bed Temperature",
				Template:   "GET /rr_gcode?gcode=M140+S{temp}",
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
		StatusMapping: map[string]string{
			"I": "idle",
			"P": "running",
			"S": "paused",
			"D": "paused",
			"H": "error",
		},
		TelemetryParse: map[string]TelemetryDef{
			"extruder_temp": {
				Pattern:    `"current":\[([\d.]+),([\d.]+)`,
				MetricType: "temperature_extruder",
				Unit:       "celsius",
				ValueIndex: 2, // Index 0=bed, 1=extruder0
			},
			"bed_temp": {
				Pattern:    `"current":\[([\d.]+)`,
				MetricType: "temperature_bed",
				Unit:       "celsius",
				ValueIndex: 1,
			},
			"print_progress": {
				Pattern:    `"fractionPrinted":([\d.]+)`,
				MetricType: "print_progress",
				Unit:       "percent",
				ValueIndex: 1,
			},
		},
	}
}
