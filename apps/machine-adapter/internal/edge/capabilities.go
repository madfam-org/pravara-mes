package edge

import (
	"fmt"
	"sort"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

// connectivityFor maps an adapter protocol to the MES-1 Properties/Connectivity value.
func connectivityFor(protocol string) (sparkplug.Connectivity, error) {
	switch registry.Protocol(protocol) {
	case registry.ProtocolMoonraker:
		return sparkplug.ConnectivityMoonraker, nil
	case registry.ProtocolBambuMQTT:
		return sparkplug.ConnectivityBambuLANMQTT, nil
	case registry.ProtocolOctoPrint:
		return sparkplug.ConnectivityOctoPrint, nil
	}
	return "", fmt.Errorf("protocol %q has no MES-1 connectivity value", protocol)
}

// firmwareFor maps an adapter protocol to the fabrication-capabilities firmware value.
func firmwareFor(protocol string) string {
	switch registry.Protocol(protocol) {
	case registry.ProtocolMoonraker:
		return "klipper"
	case registry.ProtocolBambuMQTT:
		return "bambu"
	}
	return ""
}

// deriveCapabilities builds the fabrication-capabilities of a device from its
// registry definition, then applies the configured overrides. Values are
// normalised to the contract datatype of each key.
func deriveCapabilities(def *registry.MachineDefinition, protocol string, overrides map[string]interface{}) (map[sparkplug.CapabilityKey]any, error) {
	caps := map[sparkplug.CapabilityKey]any{}
	if def != nil {
		if def.Type == registry.MachineType3DPrinterFDM {
			caps[sparkplug.CapabilityProcess] = "fff"
		}
		if wv, ok := def.Capabilities["work_volume"].(map[string]interface{}); ok {
			setNum(caps, sparkplug.CapabilityBuildVolumeXMM, wv["x_mm"])
			setNum(caps, sparkplug.CapabilityBuildVolumeYMM, wv["y_mm"])
			setNum(caps, sparkplug.CapabilityBuildVolumeZMM, wv["z_mm"])
		}
		if nt, ok := def.Capabilities["nozzle_temp"].(map[string]interface{}); ok {
			setNum(caps, sparkplug.CapabilityMaxHotendTempC, nt["max_celsius"])
		}
		if bt, ok := def.Capabilities["bed_temp"].(map[string]interface{}); ok {
			setNum(caps, sparkplug.CapabilityMaxBedTempC, bt["max_celsius"])
		}
		if fs, ok := def.Capabilities["filament_system"].(map[string]interface{}); ok {
			if n, ok := toInt64(fs["slots"]); ok {
				caps[sparkplug.CapabilityMaterialSlots] = n
			}
		}
	}
	if fw := firmwareFor(protocol); fw != "" {
		caps[sparkplug.CapabilityFirmware] = fw
	}
	if conn, err := connectivityFor(protocol); err == nil {
		caps[sparkplug.CapabilityConnectivity] = string(conn)
	}
	for k, v := range overrides {
		key := sparkplug.CapabilityKey(k)
		norm, err := normalizeCapability(key, v)
		if err != nil {
			return nil, fmt.Errorf("capability %q: %w", k, err)
		}
		caps[key] = norm
	}
	return caps, nil
}

func setNum(caps map[sparkplug.CapabilityKey]any, k sparkplug.CapabilityKey, v interface{}) {
	if f, ok := toFloat(v); ok && f > 0 {
		caps[k] = f
	}
}

// normalizeCapability converts a config value to the contract datatype.
func normalizeCapability(k sparkplug.CapabilityKey, v interface{}) (any, error) {
	switch sparkplug.CapabilityDatatype(k) {
	case pb.DataType_String:
		if s, ok := v.(string); ok {
			return s, nil
		}
	case pb.DataType_Boolean:
		if b, ok := v.(bool); ok {
			return b, nil
		}
	case pb.DataType_Int64:
		if n, ok := toInt64(v); ok {
			return n, nil
		}
	case pb.DataType_DoubleArray:
		switch xs := v.(type) {
		case []float64:
			return xs, nil
		case []interface{}:
			out := make([]float64, 0, len(xs))
			for _, x := range xs {
				f, ok := toFloat(x)
				if !ok {
					return nil, fmt.Errorf("list element %v is not a number", x)
				}
				out = append(out, f)
			}
			return out, nil
		}
	default:
		if f, ok := toFloat(v); ok {
			return f, nil
		}
	}
	return nil, fmt.Errorf("value %v (%T) does not fit %s", v, v, sparkplug.CapabilityDatatype(k))
}

func toFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func toInt64(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		if x == float64(int64(x)) {
			return int64(x), true
		}
	}
	return 0, false
}

// sortedCapabilityKeys returns the keys in a stable order for births.
func sortedCapabilityKeys(caps map[sparkplug.CapabilityKey]any) []sparkplug.CapabilityKey {
	keys := make([]sparkplug.CapabilityKey, 0, len(caps))
	for k := range caps {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
