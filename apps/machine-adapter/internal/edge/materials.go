package edge

import "strings"

// materialClasses maps printer material strings to keys of the
// material-classes vocabulary (SEM-1 §4). Keys are normalised with
// normalizeMaterial. Only unambiguous names are mapped: an unknown or
// ambiguous material reports an empty class, so matchmaking never assumes a
// material the printer did not state.
var materialClasses = map[string]string{
	// Moonraker / Spoolman / slicer filament_type, Bambu AMS tray_type
	"PLA": "pla", "PLA+": "pla", "PLA BASIC": "pla", "PLA MATTE": "pla", "PLA SILK": "pla", "PLA-S": "pla",
	"PETG": "petg", "PETG HF": "petg", "PETG-HF": "petg", "PETG BASIC": "petg",
	"ABS":    "abs",
	"ABS-GF": "abs-gf",
	"ASA":    "asa",
	"PC":     "pc",
	"TPU":    "tpu-95a", "TPU 95A": "tpu-95a", "TPU-95A": "tpu-95a", "TPU 95A HF": "tpu-95a",
	"TPU 85A": "tpu-85a", "TPU-85A": "tpu-85a",
	"PA-CF": "pa-cf", "PA CF": "pa-cf", "PAHT-CF": "pa12-cf", "PA12-CF": "pa12-cf",
}

// MaterialClass returns the material-classes key for a printer material
// string, or "" when the string is empty, unknown or ambiguous.
func MaterialClass(vendor string) string {
	return materialClasses[normalizeMaterial(vendor)]
}

func normalizeMaterial(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.Join(strings.Fields(s), " ")
	return s
}
