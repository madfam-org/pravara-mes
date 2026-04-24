package registry

import "time"

// GlowforgeProDefinition returns the machine definition for the Glowforge Pro.
// The Glowforge uses a cloud-only API with extremely limited local control.
// Only basic status polling may be available through the cloud endpoint.
func GlowforgeProDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Glowforge",
		Model:        "Pro",
		Type:         MachineTypeLaserCutter,
		Protocol:     ProtocolCustom,
		Connection:   ConnectionHTTP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 495.0,
				"y_mm": 279.0,
				"z_mm": 0.0,
			},
			"laser_power": map[string]interface{}{
				"watts":      45.0,
				"laser_type": "CO2",
			},
		},
		Commands: map[string]CommandDef{
			"get_status": {
				Name:     "Get Status",
				Template: "GET /api/v1/status",
				Response: "",
				Timeout:  5 * time.Second,
			},
		},
		StatusMapping: map[string]string{
			"ready":   "idle",
			"running": "running",
			"paused":  "paused",
			"error":   "error",
		},
		TelemetryParse: map[string]TelemetryDef{},
	}
}

// TrotecSpeedyDefinition returns the machine definition for the Trotec Speedy 360.
// This machine uses proprietary Trotec JobControl software with no open API.
// Registry-only: no commands are available for direct control.
func TrotecSpeedyDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Trotec",
		Model:        "Speedy 360",
		Type:         MachineTypeLaserCutter,
		Protocol:     ProtocolCustom,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 813.0,
				"y_mm": 508.0,
				"z_mm": 0.0,
			},
			"laser_power": map[string]interface{}{
				"watts":      120.0,
				"laser_type": "CO2",
			},
		},
		Commands:       map[string]CommandDef{},
		StatusMapping:  map[string]string{},
		TelemetryParse: map[string]TelemetryDef{},
	}
}
