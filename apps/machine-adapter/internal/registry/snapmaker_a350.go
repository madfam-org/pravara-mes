package registry

import "time"

func init() {
	// Register Snapmaker A350 definition at package load.
	// The registry's loadBuiltinDefinitions calls this implicitly via the init chain.
}

// SnapmakerA350Definition returns the machine definition for the Snapmaker 2.0 A350.
func SnapmakerA350Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Snapmaker",
		Model:        "2.0 A350",
		Type:         MachineType3DPrinterFDM, // Primary mode; also supports laser + CNC
		Protocol:     ProtocolMarlin,
		Connection:   ConnectionSerial, // Also supports HTTP via WiFi (luban-bridge)
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 320.0,
				"y_mm": 350.0,
				"z_mm": 330.0,
			},
			"nozzle_temp": map[string]interface{}{
				"min_celsius": 0.0,
				"max_celsius": 275.0,
			},
			"bed_temp": map[string]interface{}{
				"min_celsius": 0.0,
				"max_celsius": 80.0,
			},
			"multi_tool": map[string]interface{}{
				"modes":  []string{"3dp", "laser", "cnc"},
				"detect": "M1005",
				"switch": "M605",
			},
			"laser_power": map[string]interface{}{
				"watts": 1.6,
			},
			"enclosure": map[string]interface{}{
				"supported": true,
				"led":       true,
				"fan":       true,
			},
			"wifi": map[string]interface{}{
				"supported": true,
				"protocol":  "http",
				"port":      8080,
			},
			"materials": map[string]interface{}{
				"materials": []string{"PLA", "ABS", "PETG", "TPU", "Wood", "Carbon Fiber"},
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
				Template: "M25",
				Response: "ok",
				Timeout:  5 * time.Second,
			},
			"resume": {
				Name:     "Resume Print",
				Template: "M24",
				Response: "ok",
				Timeout:  5 * time.Second,
			},
			"stop": {
				Name:     "Stop Print",
				Template: "M524",
				Response: "ok",
				Timeout:  5 * time.Second,
			},
			"emergency_stop": {
				Name:     "Emergency Stop",
				Template: "M112",
				Response: "",
				Timeout:  1 * time.Second,
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
						"max": 275,
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
						"max": 80,
					},
				},
			},
			"detect_toolhead": {
				Name:     "Detect Tool Head",
				Template: "M1005",
				Response: "ok",
				Timeout:  5 * time.Second,
			},
			"switch_mode": {
				Name:       "Switch Tool Mode",
				Template:   "M605 S{mode}",
				Parameters: []string{"mode"},
				Response:   "ok",
				Timeout:    10 * time.Second,
			},
			"enclosure_led": {
				Name:       "Enclosure LED",
				Template:   "M2000 L{brightness}",
				Parameters: []string{"brightness"},
				Response:   "ok",
				Timeout:    2 * time.Second,
			},
			"enclosure_fan": {
				Name:       "Enclosure Fan",
				Template:   "M2000 F{speed}",
				Parameters: []string{"speed"},
				Response:   "ok",
				Timeout:    2 * time.Second,
			},
			"auto_level": {
				Name:     "Auto Bed Level",
				Template: "G29",
				Response: "ok",
				Timeout:  120 * time.Second,
			},
			"get_position": {
				Name:     "Get Position",
				Template: "M114",
				Response: "ok",
				Timeout:  2 * time.Second,
			},
			"get_temperature": {
				Name:     "Get Temperature",
				Template: "M105",
				Response: "ok",
				Timeout:  2 * time.Second,
			},
		},
		StatusMapping: map[string]string{
			"Idle":     "idle",
			"Printing": "running",
			"Paused":   "paused",
			"Error":    "error",
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
			"position_x": {
				Pattern:    `X:([-\d.]+)`,
				MetricType: "position_x",
				Unit:       "mm",
				ValueIndex: 1,
			},
			"position_y": {
				Pattern:    `Y:([-\d.]+)`,
				MetricType: "position_y",
				Unit:       "mm",
				ValueIndex: 1,
			},
			"position_z": {
				Pattern:    `Z:([-\d.]+)`,
				MetricType: "position_z",
				Unit:       "mm",
				ValueIndex: 1,
			},
		},
	}
}
