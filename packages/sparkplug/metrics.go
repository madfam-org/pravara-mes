package sparkplug

import (
	"fmt"
	"strconv"
	"strings"

	pb "github.com/madfam-org/pravara-mes/packages/sparkplug/sparkplugpb"
)

// MetricName is a Sparkplug metric name. The constants below are the MES-1 §1
// contract; changing one is a breaking change for the host application.
type MetricName string

// Sparkplug-defined node metrics.
const (
	MetricBdSeq              MetricName = "bdSeq"
	MetricNodeControlRebirth MetricName = "Node Control/Rebirth"
)

// MES-1 device metrics reported by the edge node.
const (
	MetricPropertiesModel        MetricName = "Properties/Model"
	MetricPropertiesFirmware     MetricName = "Properties/Firmware"
	MetricPropertiesConnectivity MetricName = "Properties/Connectivity"
	MetricStateStatus            MetricName = "State/Status"
	MetricStateProgress          MetricName = "State/Progress"
	MetricTempsHotend            MetricName = "Temps/Hotend"
	MetricTempsBed               MetricName = "Temps/Bed"
	MetricJobID                  MetricName = "Job/Id"
	MetricJobStatus              MetricName = "Job/Status"
	MetricCommandLastID          MetricName = "Command/LastId"
	MetricCommandStatus          MetricName = "Command/Status"
	MetricCommandError           MetricName = "Command/Error"
)

// MES-1 DCMD metrics written by the host application.
const (
	MetricCommandID                MetricName = "Command/Id"
	MetricCommandName              MetricName = "Command/Name"
	MetricCommandTaskID            MetricName = "Command/TaskId"
	MetricCommandArtifactURL       MetricName = "Command/Artifact/Url"
	MetricCommandArtifactSHA256    MetricName = "Command/Artifact/Sha256"
	MetricCommandArtifactMediaType MetricName = "Command/Artifact/MediaType"
)

// Metric name prefixes for the parameterised MES-1 metrics.
const (
	CapabilitiesPrefix = "Capabilities/"
	MaterialsPrefix    = "Materials/Slot"
)

// CapabilityKey is a key of the fabrication-capabilities vocabulary.
type CapabilityKey string

// fabrication-capabilities vocabulary keys (SEM-1 §4).
const (
	CapabilityProcess           CapabilityKey = "process"
	CapabilityBuildVolumeXMM    CapabilityKey = "build_volume_x_mm"
	CapabilityBuildVolumeYMM    CapabilityKey = "build_volume_y_mm"
	CapabilityBuildVolumeZMM    CapabilityKey = "build_volume_z_mm"
	CapabilityNozzleDiametersMM CapabilityKey = "nozzle_diameters_mm"
	CapabilityMaxHotendTempC    CapabilityKey = "max_hotend_temp_c"
	CapabilityMaxBedTempC       CapabilityKey = "max_bed_temp_c"
	CapabilityEnclosure         CapabilityKey = "enclosure"
	CapabilityHeatedChamber     CapabilityKey = "heated_chamber"
	CapabilityToolheadCount     CapabilityKey = "toolhead_count"
	CapabilityMaterialSlots     CapabilityKey = "material_slots"
	CapabilityFirmware          CapabilityKey = "firmware"
	CapabilityConnectivity      CapabilityKey = "connectivity"
)

// CapabilityDatatype returns the Sparkplug datatype pravara uses for a capability key.
func CapabilityDatatype(k CapabilityKey) pb.DataType {
	switch k {
	case CapabilityProcess, CapabilityFirmware, CapabilityConnectivity:
		return pb.DataType_String
	case CapabilityEnclosure, CapabilityHeatedChamber:
		return pb.DataType_Boolean
	case CapabilityToolheadCount, CapabilityMaterialSlots:
		return pb.DataType_Int64
	case CapabilityNozzleDiametersMM:
		return pb.DataType_DoubleArray
	default:
		return pb.DataType_Double
	}
}

// Metric returns "Capabilities/<key>".
func (k CapabilityKey) Metric() MetricName { return MetricName(CapabilitiesPrefix + string(k)) }

// MaterialSlotClassMetric returns "Materials/Slot<n>/Class" (n starts at 1).
func MaterialSlotClassMetric(slot int) MetricName {
	return MetricName(fmt.Sprintf("%s%d/Class", MaterialsPrefix, slot))
}

// MaterialSlotLoadedMetric returns "Materials/Slot<n>/Loaded" (n starts at 1).
func MaterialSlotLoadedMetric(slot int) MetricName {
	return MetricName(fmt.Sprintf("%s%d/Loaded", MaterialsPrefix, slot))
}

// ParseMaterialSlotMetric splits "Materials/Slot<n>/<field>" into slot and field.
func ParseMaterialSlotMetric(name MetricName) (slot int, field string, ok bool) {
	rest, found := strings.CutPrefix(string(name), MaterialsPrefix)
	if !found {
		return 0, "", false
	}
	num, field, found := strings.Cut(rest, "/")
	if !found || (field != "Class" && field != "Loaded") {
		return 0, "", false
	}
	n, err := strconv.Atoi(num)
	if err != nil || n < 1 {
		return 0, "", false
	}
	return n, field, true
}

// DeviceStatus is the MES-1 State/Status value set.
type DeviceStatus string

// State/Status values.
const (
	StatusIdle     DeviceStatus = "idle"
	StatusPrinting DeviceStatus = "printing"
	StatusPaused   DeviceStatus = "paused"
	StatusError    DeviceStatus = "error"
	StatusOffline  DeviceStatus = "offline"
)

// JobStatus is the MES-1 Job/Status value set.
type JobStatus string

// Job/Status values.
const (
	JobQueued    JobStatus = "queued"
	JobPrinting  JobStatus = "printing"
	JobComplete  JobStatus = "complete"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

// IsTerminal reports whether no further transition is expected.
func (s JobStatus) IsTerminal() bool {
	return s == JobComplete || s == JobFailed || s == JobCancelled
}

// CommandStatus is the MES-1 Command/Status value set.
type CommandStatus string

// Command/Status values.
const (
	CommandAccepted CommandStatus = "accepted"
	CommandRunning  CommandStatus = "running"
	CommandDone     CommandStatus = "done"
	CommandFailed   CommandStatus = "failed"
)

// CommandName is the MES-1 Command/Name value set.
type CommandName string

// Command/Name values.
const (
	CommandStartJob CommandName = "start_job"
	CommandPause    CommandName = "pause"
	CommandResume   CommandName = "resume"
	CommandCancel   CommandName = "cancel"
)

// Valid reports whether the name is one of the MES-1 commands.
func (c CommandName) Valid() bool {
	switch c {
	case CommandStartJob, CommandPause, CommandResume, CommandCancel:
		return true
	}
	return false
}

// Connectivity is the MES-1 Properties/Connectivity value set.
type Connectivity string

// Properties/Connectivity values.
const (
	ConnectivityMoonraker    Connectivity = "moonraker"
	ConnectivityBambuLANMQTT Connectivity = "bambu_lan_mqtt"
	ConnectivityOctoPrint    Connectivity = "octoprint"
)

// fixedDatatypes is the datatype of every fixed-name MES-1 metric.
var fixedDatatypes = map[MetricName]pb.DataType{
	MetricBdSeq:                    pb.DataType_Int64,
	MetricNodeControlRebirth:       pb.DataType_Boolean,
	MetricPropertiesModel:          pb.DataType_String,
	MetricPropertiesFirmware:       pb.DataType_String,
	MetricPropertiesConnectivity:   pb.DataType_String,
	MetricStateStatus:              pb.DataType_String,
	MetricStateProgress:            pb.DataType_Double,
	MetricTempsHotend:              pb.DataType_Double,
	MetricTempsBed:                 pb.DataType_Double,
	MetricJobID:                    pb.DataType_String,
	MetricJobStatus:                pb.DataType_String,
	MetricCommandLastID:            pb.DataType_String,
	MetricCommandStatus:            pb.DataType_String,
	MetricCommandError:             pb.DataType_String,
	MetricCommandID:                pb.DataType_String,
	MetricCommandName:              pb.DataType_String,
	MetricCommandTaskID:            pb.DataType_String,
	MetricCommandArtifactURL:       pb.DataType_String,
	MetricCommandArtifactSHA256:    pb.DataType_String,
	MetricCommandArtifactMediaType: pb.DataType_String,
}

// DatatypeOf returns the contract datatype of a MES-1 metric name, including
// the parameterised Capabilities/* and Materials/Slot<n>/* families.
func DatatypeOf(name MetricName) (pb.DataType, bool) {
	if dt, ok := fixedDatatypes[name]; ok {
		return dt, true
	}
	if key, ok := strings.CutPrefix(string(name), CapabilitiesPrefix); ok && key != "" {
		return CapabilityDatatype(CapabilityKey(key)), true
	}
	if _, field, ok := ParseMaterialSlotMetric(name); ok {
		if field == "Loaded" {
			return pb.DataType_Boolean, true
		}
		return pb.DataType_String, true
	}
	return pb.DataType_Unknown, false
}

// CommandInputMetrics lists the DCMD metrics a device declares in its DBIRTH
// so that the host can address them by name or alias.
var CommandInputMetrics = []MetricName{
	MetricCommandID, MetricCommandName, MetricCommandTaskID,
	MetricCommandArtifactURL, MetricCommandArtifactSHA256, MetricCommandArtifactMediaType,
}
