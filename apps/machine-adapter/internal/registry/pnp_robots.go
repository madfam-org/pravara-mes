package registry

import "time"

// LumenPnPDefinition returns the machine definition for the Opulo LumenPnP
// pick-and-place machine. Uses OpenPnP protocol over HTTP.
func LumenPnPDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Opulo",
		Model:        "LumenPnP",
		Type:         MachineTypePickAndPlace,
		Protocol:     ProtocolOpenPnP,
		Connection:   ConnectionHTTP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 600.0,
				"y_mm": 400.0,
				"z_mm": 60.0,
			},
			"feeder_count": map[string]interface{}{
				"count": 48,
			},
			"vision_system": map[string]interface{}{
				"supported": true,
				"type":      "bottom + top cameras",
			},
		},
		Commands: openPnPCommands(),
		StatusMapping: map[string]string{
			"IDLE":    "idle",
			"RUNNING": "running",
			"PAUSED":  "paused",
			"ERROR":   "error",
		},
		TelemetryParse: map[string]TelemetryDef{
			"position": {
				Pattern:    `X:([-\d.]+)\s+Y:([-\d.]+)\s+Z:([-\d.]+)`,
				MetricType: "position",
				Unit:       "mm",
				ValueIndex: 1,
			},
		},
	}
}

// IndexPnPDefinition returns the machine definition for the Index Pick and Place.
// Uses OpenPnP protocol over HTTP.
func IndexPnPDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Index",
		Model:        "Pick and Place",
		Type:         MachineTypePickAndPlace,
		Protocol:     ProtocolOpenPnP,
		Connection:   ConnectionHTTP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"x_mm": 600.0,
				"y_mm": 400.0,
				"z_mm": 60.0,
			},
			"feeder_count": map[string]interface{}{
				"count": 20,
			},
		},
		Commands: openPnPCommands(),
		StatusMapping: map[string]string{
			"IDLE":    "idle",
			"RUNNING": "running",
			"PAUSED":  "paused",
			"ERROR":   "error",
		},
		TelemetryParse: map[string]TelemetryDef{
			"position": {
				Pattern:    `X:([-\d.]+)\s+Y:([-\d.]+)\s+Z:([-\d.]+)`,
				MetricType: "position",
				Unit:       "mm",
				ValueIndex: 1,
			},
		},
	}
}

// openPnPCommands returns the shared command set for OpenPnP-based machines.
func openPnPCommands() map[string]CommandDef {
	return map[string]CommandDef{
		"home": {
			Name:     "Home",
			Template: "G28",
			Response: "ok",
			Timeout:  30 * time.Second,
		},
		"move_to": {
			Name:       "Move To",
			Template:   "G1 X{x} Y{y} Z{z} F{feed}",
			Parameters: []string{"x", "y", "z", "feed"},
			Response:   "ok",
			Timeout:    10 * time.Second,
		},
		"pick": {
			Name:     "Pick Component",
			Template: "M800",
			Response: "ok",
			Timeout:  5 * time.Second,
		},
		"place": {
			Name:     "Place Component",
			Template: "M801",
			Response: "ok",
			Timeout:  5 * time.Second,
		},
		"get_position": {
			Name:     "Get Position",
			Template: "M114",
			Response: "ok",
			Timeout:  2 * time.Second,
		},
	}
}

// URGenericDefinition returns the machine definition for the Universal Robots UR5e.
// Uses URScript protocol over TCP port 30002.
func URGenericDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Universal Robots",
		Model:        "UR5e",
		Type:         MachineTypeRobotArm,
		Protocol:     ProtocolURScript,
		Connection:   ConnectionTCP,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"radius_mm": 850.0,
				"dof":       6,
			},
		},
		Commands: map[string]CommandDef{
			"home": {
				Name:     "Home",
				Template: "movej([0,0,0,0,0,0], a=1.0, v=0.5)",
				Response: "",
				Timeout:  30 * time.Second,
			},
			"move_joint": {
				Name:       "Move Joint",
				Template:   "movej([{j0},{j1},{j2},{j3},{j4},{j5}], a={accel}, v={vel})",
				Parameters: []string{"j0", "j1", "j2", "j3", "j4", "j5", "accel", "vel"},
				Response:   "",
				Timeout:    30 * time.Second,
			},
			"move_linear": {
				Name:       "Move Linear",
				Template:   "movel(p[{x},{y},{z},{rx},{ry},{rz}], a={accel}, v={vel})",
				Parameters: []string{"x", "y", "z", "rx", "ry", "rz", "accel", "vel"},
				Response:   "",
				Timeout:    30 * time.Second,
			},
			"set_digital_out": {
				Name:       "Set Digital Output",
				Template:   "set_digital_out({port},{value})",
				Parameters: []string{"port", "value"},
				Response:   "",
				Timeout:    2 * time.Second,
			},
			"get_position": {
				Name:     "Get Position",
				Template: "get_actual_tcp_pose()",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"stop": {
				Name:     "Stop",
				Template: "stopj(2.0)",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"pause": {
				Name:     "Pause",
				Template: "pause program",
				Response: "",
				Timeout:  2 * time.Second,
			},
			"resume": {
				Name:     "Resume",
				Template: "resume program",
				Response: "",
				Timeout:  2 * time.Second,
			},
		},
		StatusMapping: map[string]string{
			"RUNNING":            "running",
			"IDLE":               "idle",
			"PAUSED":             "paused",
			"PROTECTIVE_STOP":    "error",
			"EMERGENCY_STOPPED":  "error",
		},
		TelemetryParse: map[string]TelemetryDef{
			"joint_positions": {
				Pattern:    `q_actual:\[([-\d.]+),([-\d.]+),([-\d.]+),([-\d.]+),([-\d.]+),([-\d.]+)\]`,
				MetricType: "joint_positions",
				Unit:       "rad",
				ValueIndex: 1,
			},
			"tcp_position": {
				Pattern:    `tcp:\[([-\d.]+),([-\d.]+),([-\d.]+),([-\d.]+),([-\d.]+),([-\d.]+)\]`,
				MetricType: "tcp_position",
				Unit:       "m",
				ValueIndex: 1,
			},
		},
	}
}

// DobotMagicianDefinition returns the machine definition for the Dobot Magician.
// Uses the Dobot serial API for basic motion control.
func DobotMagicianDefinition() *MachineDefinition {
	return &MachineDefinition{
		Manufacturer: "Dobot",
		Model:        "Magician",
		Type:         MachineTypeRobotArm,
		Protocol:     ProtocolDobot,
		Connection:   ConnectionSerial,
		Capabilities: map[string]interface{}{
			"work_volume": map[string]interface{}{
				"radius_mm": 320.0,
				"dof":       4,
			},
		},
		Commands: map[string]CommandDef{
			"home": {
				Name:     "Home",
				Template: "HOME",
				Response: "",
				Timeout:  30 * time.Second,
			},
			"move_to": {
				Name:       "Move To",
				Template:   "MOVETO {x} {y} {z} {r}",
				Parameters: []string{"x", "y", "z", "r"},
				Response:   "",
				Timeout:    10 * time.Second,
			},
			"set_suction": {
				Name:       "Set Suction Cup",
				Template:   "SUCTION {state}",
				Parameters: []string{"state"},
				Response:   "",
				Timeout:    2 * time.Second,
			},
			"get_position": {
				Name:     "Get Position",
				Template: "GETPOSE",
				Response: "",
				Timeout:  2 * time.Second,
			},
		},
		StatusMapping: map[string]string{
			"IDLE":    "idle",
			"MOVING":  "running",
			"ALARM":   "error",
		},
		TelemetryParse: map[string]TelemetryDef{
			"position": {
				Pattern:    `X:([-\d.]+)\s+Y:([-\d.]+)\s+Z:([-\d.]+)\s+R:([-\d.]+)`,
				MetricType: "position",
				Unit:       "mm",
				ValueIndex: 1,
			},
		},
	}
}
