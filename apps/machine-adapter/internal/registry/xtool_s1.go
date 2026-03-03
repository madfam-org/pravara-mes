package registry

// XToolS1Definition returns the machine definition for the xTool S1 diode laser engraver.
func XToolS1Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "xTool",
		Model:        "S1",
		Type:         MachineTypeLaserEngraver,
		Protocol:     ProtocolXTool,
		Connection:   ConnectionHTTP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 498.0,
				"y_mm": 319.0,
				"z_mm": 0.0,
			},
			"laser_power": map[string]interface{}{
				"watts": 40.0,
				"type":  "diode",
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
