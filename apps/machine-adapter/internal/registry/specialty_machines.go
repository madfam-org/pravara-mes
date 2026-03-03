package registry

// SilhouetteCameo5Definition returns the machine definition for the Silhouette Cameo 5.
// Registry-only: USB proprietary protocol with no public API.
func SilhouetteCameo5Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Silhouette",
		Model:        "Cameo 5",
		Type:         MachineTypeVinylCutter,
		Protocol:     ProtocolCustom,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 305.0,
				"y_mm": 3000.0,
				"z_mm": 0.0,
			},
			"cutting_force": map[string]interface{}{
				"max_grams": 210,
			},
		},
		Commands:       map[string]CommandDef{},
		StatusMapping:  map[string]string{},
		TelemetryParse: map[string]TelemetryDef{},
	}
}

// NeodenYY1Definition returns the machine definition for the Neoden YY1
// pick-and-place machine. Registry-only: proprietary serial protocol.
func NeodenYY1Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Neoden",
		Model:        "YY1",
		Type:         MachineTypePickAndPlace,
		Protocol:     ProtocolCustom,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"feeder_count": map[string]interface{}{
				"count": 24,
			},
		},
		Commands:       map[string]CommandDef{},
		StatusMapping:  map[string]string{},
		TelemetryParse: map[string]TelemetryDef{},
	}
}

// BantamToolsPCBMillDefinition returns the machine definition for the
// Bantam Tools Desktop PCB Milling Machine. Registry-only: proprietary USB protocol.
func BantamToolsPCBMillDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Bantam Tools",
		Model:        "Desktop PCB Milling Machine",
		Type:         MachineTypePCBMill,
		Protocol:     ProtocolCustom,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 152.0,
				"y_mm": 102.0,
				"z_mm": 38.0,
			},
		},
		Commands:       map[string]CommandDef{},
		StatusMapping:  map[string]string{},
		TelemetryParse: map[string]TelemetryDef{},
	}
}

// BrotherPE800Definition returns the machine definition for the Brother PE800
// embroidery machine. Registry-only: USB proprietary protocol.
func BrotherPE800Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Brother",
		Model:        "PE800",
		Type:         MachineTypeEmbroidery,
		Protocol:     ProtocolCustom,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 130.0,
				"y_mm": 180.0,
				"z_mm": 0.0,
			},
		},
		Commands:       map[string]CommandDef{},
		StatusMapping:  map[string]string{},
		TelemetryParse: map[string]TelemetryDef{},
	}
}
