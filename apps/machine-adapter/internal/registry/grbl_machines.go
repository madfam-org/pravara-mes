package registry

import "time"

// grblCommands returns the standard GRBL command set shared by all GRBL machines.
func grblCommands() map[string]CommandDef {
	return map[string]CommandDef{
		"home": {
			Name:     "Home",
			Template: "$H",
			Response: "ok",
			Timeout:  30 * time.Second,
		},
		"status": {
			Name:     "Status",
			Template: "?",
			Response: "<.*>",
			Timeout:  1 * time.Second,
		},
		"pause": {
			Name:     "Pause",
			Template: "!",
			Response: "ok",
			Timeout:  1 * time.Second,
		},
		"resume": {
			Name:     "Resume",
			Template: "~",
			Response: "ok",
			Timeout:  1 * time.Second,
		},
		"stop": {
			Name:     "Stop",
			Template: "\x18",
			Response: "ok",
			Timeout:  1 * time.Second,
		},
	}
}

// grblStatusMapping returns the standard GRBL status mapping.
func grblStatusMapping() map[string]string {
	return map[string]string{
		"Idle":  "idle",
		"Run":   "running",
		"Hold":  "paused",
		"Alarm": "error",
	}
}

// grblTelemetryParse returns the standard GRBL telemetry parsing definitions.
func grblTelemetryParse() map[string]TelemetryDef {
	return map[string]TelemetryDef{
		"position": {
			Pattern:    `MPos:([-\d.]+),([-\d.]+),([-\d.]+)`,
			MetricType: "position",
			Unit:       "mm",
			ValueIndex: 1,
		},
	}
}

// ShapeokoHDMDefinition returns the machine definition for the Carbide 3D Shapeoko HDM.
func ShapeokoHDMDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Carbide 3D",
		Model:        "Shapeoko HDM",
		Type:         MachineTypeCNC3Axis,
		Protocol:     ProtocolGRBL,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 650.0,
				"y_mm": 650.0,
				"z_mm": 150.0,
			},
			"spindle_speed": map[string]interface{}{
				"min_rpm": 0,
				"max_rpm": 24000,
			},
		},
		Commands:       grblCommands(),
		StatusMapping:  grblStatusMapping(),
		TelemetryParse: grblTelemetryParse(),
	}
}

// XCarveProDefinition returns the machine definition for the Inventables X-Carve Pro.
func XCarveProDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Inventables",
		Model:        "X-Carve Pro",
		Type:         MachineTypeCNC3Axis,
		Protocol:     ProtocolGRBL,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 610.0,
				"y_mm": 610.0,
				"z_mm": 95.0,
			},
		},
		Commands:       grblCommands(),
		StatusMapping:  grblStatusMapping(),
		TelemetryParse: grblTelemetryParse(),
	}
}

// OpenBuildsLeadDefinition returns the machine definition for the OpenBuilds LEAD CNC.
func OpenBuildsLeadDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "OpenBuilds",
		Model:        "LEAD CNC",
		Type:         MachineTypeCNC3Axis,
		Protocol:     ProtocolGRBL,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 1000.0,
				"y_mm": 1000.0,
				"z_mm": 90.0,
			},
		},
		Commands:       grblCommands(),
		StatusMapping:  grblStatusMapping(),
		TelemetryParse: grblTelemetryParse(),
	}
}

// SienciLongMillMK2Definition returns the machine definition for the Sienci Labs LongMill MK2.
func SienciLongMillMK2Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Sienci Labs",
		Model:        "LongMill MK2",
		Type:         MachineTypeCNC3Axis,
		Protocol:     ProtocolGRBL,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 762.0,
				"y_mm": 762.0,
				"z_mm": 114.0,
			},
		},
		Commands:       grblCommands(),
		StatusMapping:  grblStatusMapping(),
		TelemetryParse: grblTelemetryParse(),
	}
}

// StepcraftD840Definition returns the machine definition for the Stepcraft D.840.
func StepcraftD840Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Stepcraft",
		Model:        "D.840",
		Type:         MachineTypeCNC3Axis,
		Protocol:     ProtocolGRBL,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 840.0,
				"y_mm": 600.0,
				"z_mm": 140.0,
			},
		},
		Commands:       grblCommands(),
		StatusMapping:  grblStatusMapping(),
		TelemetryParse: grblTelemetryParse(),
	}
}

// CrealityFalcon2Definition returns the machine definition for the Creality Falcon2 22W
// laser engraver. Uses GRBL protocol for motion and laser control.
func CrealityFalcon2Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Creality",
		Model:        "Falcon2 22W",
		Type:         MachineTypeLaserEngraver,
		Protocol:     ProtocolGRBL,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 400.0,
				"y_mm": 415.0,
				"z_mm": 0.0,
			},
			"laser_power": map[string]interface{}{
				"watts":      22.0,
				"laser_type": "diode",
			},
		},
		Commands:       grblCommands(),
		StatusMapping:  grblStatusMapping(),
		TelemetryParse: grblTelemetryParse(),
	}
}
