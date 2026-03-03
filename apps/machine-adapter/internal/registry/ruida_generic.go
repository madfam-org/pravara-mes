package registry

import "time"

// RuidaGenericDefinition returns the machine definition for a generic Ruida
// RDC6445-based laser cutter. This covers the RDC6442G, RDC6445, and RDC6445G
// controller boards commonly found in Chinese CO2 laser cutters.
func RuidaGenericDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Ruida",
		Model:        "RDC6445 Generic",
		Type:         MachineTypeLaserCutter,
		Protocol:     ProtocolRuida,
		Connection:   ConnectionUDP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 600.0,
				"y_mm": 400.0,
				"z_mm": 0.0,
			},
			"laser_power": map[string]interface{}{
				"watts": 100.0,
			},
		},
		Commands: map[string]CommandDef{
			"start": {
				Name:     "Start Job",
				Template: "D700",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"stop": {
				Name:     "Stop Job",
				Template: "D701",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"pause": {
				Name:     "Pause Job",
				Template: "D702",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"resume": {
				Name:     "Resume Job",
				Template: "D703",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"status": {
				Name:     "Query Status",
				Template: "DA0004",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"get_position": {
				Name:     "Get Position",
				Template: "DA0000",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"emergency_stop": {
				Name:     "Emergency Stop",
				Template: "D701",
				Response: "",
				Timeout:  1 * time.Second,
			},
		},
		StatusMapping: map[string]string{
			"idle":     "idle",
			"running":  "running",
			"paused":   "paused",
			"finished": "finished",
			"error":    "error",
		},
		TelemetryParse: map[string]TelemetryDef{
			"machine_state": {
				Pattern:    `state:(\w+)`,
				MetricType: "machine_state",
				Unit:       "enum",
				ValueIndex: 1,
			},
			"position_x": {
				Pattern:    `X:([-\d.]+)`,
				MetricType: "laser_position_x",
				Unit:       "mm",
				ValueIndex: 1,
			},
			"position_y": {
				Pattern:    `Y:([-\d.]+)`,
				MetricType: "laser_position_y",
				Unit:       "mm",
				ValueIndex: 1,
			},
		},
	}
}
