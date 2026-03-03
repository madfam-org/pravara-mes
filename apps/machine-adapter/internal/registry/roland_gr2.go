package registry

import "time"

// RolandGR2Definition returns the machine definition for the Roland CAMM-1 GR2-640
// professional vinyl cutter.
func RolandGR2Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Roland DG",
		Model:        "CAMM-1 GR2-640",
		Type:         MachineTypeVinylCutter,
		Protocol:     ProtocolCammGL,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 1651.0,
				"y_mm": 50000.0,
				"z_mm": 0.0,
			},
			"cutting_force": map[string]interface{}{
				"max_grams": 600,
			},
		},
		Commands: rolandCommonCommands(),
		StatusMapping: map[string]string{
			"ready":   "idle",
			"cutting": "running",
			"error":   "error",
		},
		TelemetryParse: rolandCommonTelemetry(),
	}
}

// rolandCommonCommands returns the command definitions shared across Roland CAMM-GL cutters.
func rolandCommonCommands() map[string]CommandDef {
	return map[string]CommandDef{
		"init": {
			Name:     "Initialize",
			Template: "IN;",
			Response: "",
			Timeout:  5 * time.Second,
		},
		"move": {
			Name:       "Pen Up Move",
			Template:   "PU {x},{y};",
			Parameters: []string{"x", "y"},
			Response:   "",
			Timeout:    10 * time.Second,
		},
		"cut": {
			Name:       "Pen Down Cut",
			Template:   "PD {x},{y};",
			Parameters: []string{"x", "y"},
			Response:   "",
			Timeout:    30 * time.Second,
		},
		"set_speed": {
			Name:       "Set Velocity",
			Template:   "VS {speed};",
			Parameters: []string{"speed"},
			Response:   "",
			Timeout:    2 * time.Second,
		},
		"set_force": {
			Name:       "Set Force",
			Template:   "FP {force};",
			Parameters: []string{"force"},
			Response:   "",
			Timeout:    2 * time.Second,
		},
		"select_pen": {
			Name:       "Select Pen",
			Template:   "SP {pen};",
			Parameters: []string{"pen"},
			Response:   "",
			Timeout:    2 * time.Second,
		},
		"get_position": {
			Name:     "Output Actual Position",
			Template: "OA;",
			Response: `\d+,\d+`,
			Timeout:  2 * time.Second,
		},
		"home": {
			Name:     "Home",
			Template: "IN;PU 0,0;",
			Response: "",
			Timeout:  10 * time.Second,
		},
	}
}

// rolandCommonTelemetry returns the telemetry parse definitions shared across Roland CAMM-GL cutters.
func rolandCommonTelemetry() map[string]TelemetryDef {
	return map[string]TelemetryDef{
		"position_x": {
			Pattern:    `^([-\d.]+),`,
			MetricType: "position_x",
			Unit:       "plotter_units",
			ValueIndex: 1,
		},
		"position_y": {
			Pattern:    `,([-\d.]+)$`,
			MetricType: "position_y",
			Unit:       "plotter_units",
			ValueIndex: 1,
		},
	}
}
