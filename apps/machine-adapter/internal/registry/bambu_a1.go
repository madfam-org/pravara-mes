package registry

// BambuA1Definition returns the machine definition for the Bambu Lab A1.
func BambuA1Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Bambu Lab",
		Model:        "A1",
		Type:         MachineType3DPrinterFDM,
		Protocol:     ProtocolBambuMQTT,
		Connection:   ConnectionMQTT,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 256.0,
				"y_mm": 256.0,
				"z_mm": 256.0,
			},
			"nozzle_temp": map[string]interface{}{
				"min_celsius": 0.0,
				"max_celsius": 300.0,
			},
			"bed_temp": map[string]interface{}{
				"min_celsius": 0.0,
				"max_celsius": 100.0,
			},
			"chamber_temp": map[string]interface{}{
				"type": "none",
			},
			"print_speed": map[string]interface{}{
				"max_mm_per_sec": 500.0,
			},
			"camera": map[string]interface{}{
				"supported":  true,
				"resolution": "1080p",
			},
			"filament_system": map[string]interface{}{
				"type":  "AMS Lite",
				"slots": 4,
			},
			"input_shaping": map[string]interface{}{
				"supported": true,
			},
			"multi_material": map[string]interface{}{
				"supported": true,
				"colors":    4,
			},
		},
		Commands: bambuCommonCommands(),
		StatusMapping: map[string]string{
			"IDLE":    "idle",
			"RUNNING": "running",
			"PAUSE":   "paused",
			"FAILED":  "error",
			"FINISH":  "idle",
		},
		TelemetryParse: bambuCommonTelemetry(),
	}
}
