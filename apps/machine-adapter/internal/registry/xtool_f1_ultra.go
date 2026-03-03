package registry

// XToolF1UltraDefinition returns the machine definition for the xTool F1 Ultra
// compact dual-laser engraver (20W diode + 2W IR).
func XToolF1UltraDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "xTool",
		Model:        "F1 Ultra",
		Type:         MachineTypeLaserEngraver,
		Protocol:     ProtocolXTool,
		Connection:   ConnectionHTTP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 115.0,
				"y_mm": 115.0,
				"z_mm": 0.0,
			},
			"laser_power": map[string]interface{}{
				"watts":    20.0,
				"type":     "diode",
				"ir_watts": 2.0,
			},
			"camera": map[string]interface{}{
				"supported": true,
			},
		},
		Commands: xtoolCommonCommands(),
		StatusMapping: map[string]string{
			"idle":    "idle",
			"running": "running",
			"paused":  "paused",
			"error":   "error",
		},
		TelemetryParse: xtoolCommonTelemetry(),
	}
}
