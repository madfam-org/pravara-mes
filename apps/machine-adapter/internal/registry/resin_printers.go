package registry

// ElegooSaturn4UltraDefinition returns the machine definition for the Elegoo Saturn 4 Ultra.
// This is a registry-only definition; the Saturn 4 Ultra uses a proprietary serial
// protocol with no public API documentation, so commands are minimal.
func ElegooSaturn4UltraDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Elegoo",
		Model:        "Saturn 4 Ultra",
		Type:         MachineType3DPrinterSLA,
		Protocol:     ProtocolCustom,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 218.88,
				"y_mm": 122.88,
				"z_mm": 260.0,
			},
			"resolution": map[string]interface{}{
				"xy_microns": 18.0,
				"z_microns":  10.0,
			},
		},
		Commands:       map[string]CommandDef{},
		StatusMapping:  map[string]string{},
		TelemetryParse: map[string]TelemetryDef{},
	}
}

// AnycubicPhotonMonoM7Definition returns the machine definition for the Anycubic Photon Mono M7.
// Registry-only: proprietary serial protocol with no public API.
func AnycubicPhotonMonoM7Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Anycubic",
		Model:        "Photon Mono M7",
		Type:         MachineType3DPrinterSLA,
		Protocol:     ProtocolCustom,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 170.0,
				"y_mm": 107.0,
				"z_mm": 180.0,
			},
			"resolution": map[string]interface{}{
				"xy_microns": 40.0,
				"z_microns":  10.0,
			},
		},
		Commands:       map[string]CommandDef{},
		StatusMapping:  map[string]string{},
		TelemetryParse: map[string]TelemetryDef{},
	}
}

// PhrozenSonicMighty8KDefinition returns the machine definition for the Phrozen Sonic Mighty 8K.
// Registry-only: proprietary serial protocol with no public API.
func PhrozenSonicMighty8KDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Phrozen",
		Model:        "Sonic Mighty 8K",
		Type:         MachineType3DPrinterSLA,
		Protocol:     ProtocolCustom,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 218.0,
				"y_mm": 123.0,
				"z_mm": 235.0,
			},
			"resolution": map[string]interface{}{
				"xy_microns": 28.0,
				"z_microns":  10.0,
			},
		},
		Commands:       map[string]CommandDef{},
		StatusMapping:  map[string]string{},
		TelemetryParse: map[string]TelemetryDef{},
	}
}
