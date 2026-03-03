package registry

// RolandGS2Definition returns the machine definition for the Roland CAMM-1 GS2-24
// desktop vinyl cutter.
func RolandGS2Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Roland DG",
		Model:        "CAMM-1 GS2-24",
		Type:         MachineTypeVinylCutter,
		Protocol:     ProtocolCammGL,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 584.0,
				"y_mm": 25000.0,
				"z_mm": 0.0,
			},
			"cutting_force": map[string]interface{}{
				"max_grams": 350,
			},
		},
		Commands:       rolandCommonCommands(),
		StatusMapping: map[string]string{
			"ready":   "idle",
			"cutting": "running",
			"error":   "error",
		},
		TelemetryParse: rolandCommonTelemetry(),
	}
}
