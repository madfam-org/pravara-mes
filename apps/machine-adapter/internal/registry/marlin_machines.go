package registry

import "time"

// GraphtecCE8000Definition returns the machine definition for the Graphtec CE8000-60 Plus.
// This vinyl cutter uses the Graphtec GP-GL protocol over serial.
func GraphtecCE8000Definition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Graphtec",
		Model:        "CE8000-60 Plus",
		Type:         MachineTypeVinylCutter,
		Protocol:     ProtocolGPGL,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 606.0,
				"y_mm": 50000.0,
				"z_mm": 0.0,
			},
			"cutting_force": map[string]interface{}{
				"max_grams": 600,
			},
		},
		Commands: map[string]CommandDef{
			"home": {
				Name:     "Home",
				Template: "H",
				Response: "",
				Timeout:  10 * time.Second,
			},
			"status": {
				Name:     "Status",
				Template: "\x05", // ENQ
				Response: "",
				Timeout:  2 * time.Second,
			},
		},
		StatusMapping: map[string]string{
			"0": "idle",
			"1": "running",
			"2": "error",
		},
		TelemetryParse: map[string]TelemetryDef{},
	}
}
