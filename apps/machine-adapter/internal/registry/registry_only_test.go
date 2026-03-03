package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegistryOnlyDefinitions_BasicFields(t *testing.T) {
	tests := []struct {
		name         string
		def          *MachineDefinition
		manufacturer string
		model        string
		machineType  MachineType
		protocol     Protocol
		connection   ConnectionType
	}{
		// Resin printers
		{
			name:         "ElegooSaturn4Ultra",
			def:          ElegooSaturn4UltraDefinition(),
			manufacturer: "Elegoo",
			model:        "Saturn 4 Ultra",
			machineType:  MachineType3DPrinterSLA,
			protocol:     ProtocolCustom,
			connection:   ConnectionSerial,
		},
		{
			name:         "AnycubicPhotonMonoM7",
			def:          AnycubicPhotonMonoM7Definition(),
			manufacturer: "Anycubic",
			model:        "Photon Mono M7",
			machineType:  MachineType3DPrinterSLA,
			protocol:     ProtocolCustom,
			connection:   ConnectionSerial,
		},
		{
			name:         "PhrozenSonicMighty8K",
			def:          PhrozenSonicMighty8KDefinition(),
			manufacturer: "Phrozen",
			model:        "Sonic Mighty 8K",
			machineType:  MachineType3DPrinterSLA,
			protocol:     ProtocolCustom,
			connection:   ConnectionSerial,
		},
		// Laser machines
		{
			name:         "GlowforgePro",
			def:          GlowforgeProDefinition(),
			manufacturer: "Glowforge",
			model:        "Pro",
			machineType:  MachineTypeLaserCutter,
			protocol:     ProtocolCustom,
			connection:   ConnectionHTTP,
		},
		{
			name:         "TrotecSpeedy360",
			def:          TrotecSpeedyDefinition(),
			manufacturer: "Trotec",
			model:        "Speedy 360",
			machineType:  MachineTypeLaserCutter,
			protocol:     ProtocolCustom,
			connection:   ConnectionSerial,
		},
		// Specialty machines
		{
			name:         "SilhouetteCameo5",
			def:          SilhouetteCameo5Definition(),
			manufacturer: "Silhouette",
			model:        "Cameo 5",
			machineType:  MachineTypeVinylCutter,
			protocol:     ProtocolCustom,
			connection:   ConnectionSerial,
		},
		{
			name:         "NeodenYY1",
			def:          NeodenYY1Definition(),
			manufacturer: "Neoden",
			model:        "YY1",
			machineType:  MachineTypePickAndPlace,
			protocol:     ProtocolCustom,
			connection:   ConnectionSerial,
		},
		{
			name:         "BantamToolsPCBMill",
			def:          BantamToolsPCBMillDefinition(),
			manufacturer: "Bantam Tools",
			model:        "Desktop PCB Milling Machine",
			machineType:  MachineTypePCBMill,
			protocol:     ProtocolCustom,
			connection:   ConnectionSerial,
		},
		{
			name:         "BrotherPE800",
			def:          BrotherPE800Definition(),
			manufacturer: "Brother",
			model:        "PE800",
			machineType:  MachineTypeEmbroidery,
			protocol:     ProtocolCustom,
			connection:   ConnectionSerial,
		},
		// CNC tier 3
		{
			name:         "OneFinityWoodworker",
			def:          OneFinityWoodworkerDefinition(),
			manufacturer: "Onefinity",
			model:        "Woodworker X-50",
			machineType:  MachineTypeCNC3Axis,
			protocol:     ProtocolBuildbotics,
			connection:   ConnectionHTTP,
		},
		{
			name:         "LinuxCNCGeneric",
			def:          LinuxCNCGenericDefinition(),
			manufacturer: "Generic",
			model:        "LinuxCNC Machine",
			machineType:  MachineTypeCNC3Axis,
			protocol:     ProtocolLinuxCNC,
			connection:   ConnectionTCP,
		},
		// Pick-and-place and robots
		{
			name:         "LumenPnP",
			def:          LumenPnPDefinition(),
			manufacturer: "Opulo",
			model:        "LumenPnP",
			machineType:  MachineTypePickAndPlace,
			protocol:     ProtocolOpenPnP,
			connection:   ConnectionHTTP,
		},
		{
			name:         "IndexPnP",
			def:          IndexPnPDefinition(),
			manufacturer: "Index",
			model:        "Pick and Place",
			machineType:  MachineTypePickAndPlace,
			protocol:     ProtocolOpenPnP,
			connection:   ConnectionHTTP,
		},
		{
			name:         "URGeneric",
			def:          URGenericDefinition(),
			manufacturer: "Universal Robots",
			model:        "UR5e",
			machineType:  MachineTypeRobotArm,
			protocol:     ProtocolURScript,
			connection:   ConnectionTCP,
		},
		{
			name:         "DobotMagician",
			def:          DobotMagicianDefinition(),
			manufacturer: "Dobot",
			model:        "Magician",
			machineType:  MachineTypeRobotArm,
			protocol:     ProtocolDobot,
			connection:   ConnectionSerial,
		},
		// GRBL machines
		{
			name:         "ShapeokoHDM",
			def:          ShapeokoHDMDefinition(),
			manufacturer: "Carbide 3D",
			model:        "Shapeoko HDM",
			machineType:  MachineTypeCNC3Axis,
			protocol:     ProtocolGRBL,
			connection:   ConnectionSerial,
		},
		{
			name:         "XCarvePro",
			def:          XCarveProDefinition(),
			manufacturer: "Inventables",
			model:        "X-Carve Pro",
			machineType:  MachineTypeCNC3Axis,
			protocol:     ProtocolGRBL,
			connection:   ConnectionSerial,
		},
		{
			name:         "OpenBuildsLead",
			def:          OpenBuildsLeadDefinition(),
			manufacturer: "OpenBuilds",
			model:        "LEAD CNC",
			machineType:  MachineTypeCNC3Axis,
			protocol:     ProtocolGRBL,
			connection:   ConnectionSerial,
		},
		{
			name:         "SienciLongMillMK2",
			def:          SienciLongMillMK2Definition(),
			manufacturer: "Sienci Labs",
			model:        "LongMill MK2",
			machineType:  MachineTypeCNC3Axis,
			protocol:     ProtocolGRBL,
			connection:   ConnectionSerial,
		},
		{
			name:         "StepcraftD840",
			def:          StepcraftD840Definition(),
			manufacturer: "Stepcraft",
			model:        "D.840",
			machineType:  MachineTypeCNC3Axis,
			protocol:     ProtocolGRBL,
			connection:   ConnectionSerial,
		},
		{
			name:         "CrealityFalcon2",
			def:          CrealityFalcon2Definition(),
			manufacturer: "Creality",
			model:        "Falcon2 22W",
			machineType:  MachineTypeLaserEngraver,
			protocol:     ProtocolGRBL,
			connection:   ConnectionSerial,
		},
		// Marlin/GPGL machines
		{
			name:         "GraphtecCE8000",
			def:          GraphtecCE8000Definition(),
			manufacturer: "Graphtec",
			model:        "CE8000-60 Plus",
			machineType:  MachineTypeVinylCutter,
			protocol:     ProtocolGPGL,
			connection:   ConnectionSerial,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.manufacturer, tc.def.Manufacturer)
			assert.Equal(t, tc.model, tc.def.Model)
			assert.Equal(t, tc.machineType, tc.def.Type)
			assert.Equal(t, tc.protocol, tc.def.Protocol)
			assert.Equal(t, tc.connection, tc.def.Connection)
		})
	}
}

func TestRegistryOnlyDefinitions_WorkVolumes(t *testing.T) {
	tests := []struct {
		name string
		def  *MachineDefinition
		xMM  float64
		yMM  float64
		zMM  float64
	}{
		{"ElegooSaturn4Ultra", ElegooSaturn4UltraDefinition(), 218.88, 122.88, 260.0},
		{"AnycubicPhotonMonoM7", AnycubicPhotonMonoM7Definition(), 170.0, 107.0, 180.0},
		{"PhrozenSonicMighty8K", PhrozenSonicMighty8KDefinition(), 218.0, 123.0, 235.0},
		{"GlowforgePro", GlowforgeProDefinition(), 495.0, 279.0, 0.0},
		{"TrotecSpeedy360", TrotecSpeedyDefinition(), 813.0, 508.0, 0.0},
		{"SilhouetteCameo5", SilhouetteCameo5Definition(), 305.0, 3000.0, 0.0},
		{"BantamToolsPCBMill", BantamToolsPCBMillDefinition(), 152.0, 102.0, 38.0},
		{"BrotherPE800", BrotherPE800Definition(), 130.0, 180.0, 0.0},
		{"OneFinityWoodworker", OneFinityWoodworkerDefinition(), 816.0, 816.0, 133.0},
		{"LinuxCNCGeneric", LinuxCNCGenericDefinition(), 300.0, 300.0, 100.0},
		{"LumenPnP", LumenPnPDefinition(), 600.0, 400.0, 60.0},
		{"IndexPnP", IndexPnPDefinition(), 600.0, 400.0, 60.0},
		{"ShapeokoHDM", ShapeokoHDMDefinition(), 650.0, 650.0, 150.0},
		{"XCarvePro", XCarveProDefinition(), 610.0, 610.0, 95.0},
		{"OpenBuildsLead", OpenBuildsLeadDefinition(), 1000.0, 1000.0, 90.0},
		{"SienciLongMillMK2", SienciLongMillMK2Definition(), 762.0, 762.0, 114.0},
		{"StepcraftD840", StepcraftD840Definition(), 840.0, 600.0, 140.0},
		{"CrealityFalcon2", CrealityFalcon2Definition(), 400.0, 415.0, 0.0},
		{"GraphtecCE8000", GraphtecCE8000Definition(), 606.0, 50000.0, 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wv, ok := tc.def.Capabilities["work_volume"].(map[string]interface{})
			assert.True(t, ok, "work_volume capability should exist")
			assert.Equal(t, tc.xMM, wv["x_mm"])
			assert.Equal(t, tc.yMM, wv["y_mm"])
			assert.Equal(t, tc.zMM, wv["z_mm"])
		})
	}
}

func TestRegistryOnlyDefinitions_CommandsExist(t *testing.T) {
	tests := []struct {
		name            string
		def             *MachineDefinition
		expectedMinCmds int
	}{
		// Proprietary machines with no/empty commands
		{"ElegooSaturn4Ultra", ElegooSaturn4UltraDefinition(), 0},
		{"AnycubicPhotonMonoM7", AnycubicPhotonMonoM7Definition(), 0},
		{"PhrozenSonicMighty8K", PhrozenSonicMighty8KDefinition(), 0},
		{"TrotecSpeedy360", TrotecSpeedyDefinition(), 0},
		{"SilhouetteCameo5", SilhouetteCameo5Definition(), 0},
		{"NeodenYY1", NeodenYY1Definition(), 0},
		{"BantamToolsPCBMill", BantamToolsPCBMillDefinition(), 0},
		{"BrotherPE800", BrotherPE800Definition(), 0},
		// Cloud-limited machines with minimal commands
		{"GlowforgePro", GlowforgeProDefinition(), 1},
		// Machines with full command sets
		{"OneFinityWoodworker", OneFinityWoodworkerDefinition(), 6},
		{"LinuxCNCGeneric", LinuxCNCGenericDefinition(), 7},
		{"LumenPnP", LumenPnPDefinition(), 5},
		{"IndexPnP", IndexPnPDefinition(), 5},
		{"URGeneric", URGenericDefinition(), 8},
		{"DobotMagician", DobotMagicianDefinition(), 4},
		// GRBL machines (5 standard commands)
		{"ShapeokoHDM", ShapeokoHDMDefinition(), 5},
		{"XCarvePro", XCarveProDefinition(), 5},
		{"OpenBuildsLead", OpenBuildsLeadDefinition(), 5},
		{"SienciLongMillMK2", SienciLongMillMK2Definition(), 5},
		{"StepcraftD840", StepcraftD840Definition(), 5},
		{"CrealityFalcon2", CrealityFalcon2Definition(), 5},
		// GPGL machine
		{"GraphtecCE8000", GraphtecCE8000Definition(), 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.NotNil(t, tc.def.Commands, "Commands map should not be nil")
			assert.GreaterOrEqual(t, len(tc.def.Commands), tc.expectedMinCmds,
				"expected at least %d commands", tc.expectedMinCmds)
		})
	}
}

func TestGRBLMachines_StandardCommands(t *testing.T) {
	grblDefs := []struct {
		name string
		def  *MachineDefinition
	}{
		{"ShapeokoHDM", ShapeokoHDMDefinition()},
		{"XCarvePro", XCarveProDefinition()},
		{"OpenBuildsLead", OpenBuildsLeadDefinition()},
		{"SienciLongMillMK2", SienciLongMillMK2Definition()},
		{"StepcraftD840", StepcraftD840Definition()},
		{"CrealityFalcon2", CrealityFalcon2Definition()},
	}

	expectedCommands := []string{"home", "status", "pause", "resume", "stop"}

	for _, dc := range grblDefs {
		t.Run(dc.name, func(t *testing.T) {
			for _, cmdKey := range expectedCommands {
				t.Run(cmdKey, func(t *testing.T) {
					cmd, ok := dc.def.Commands[cmdKey]
					assert.True(t, ok, "command %q should exist", cmdKey)
					assert.NotEmpty(t, cmd.Name)
					assert.NotEmpty(t, cmd.Template)
					assert.Greater(t, cmd.Timeout.Seconds(), 0.0)
				})
			}
		})
	}
}

func TestGRBLMachines_StatusMapping(t *testing.T) {
	grblDefs := []struct {
		name string
		def  *MachineDefinition
	}{
		{"ShapeokoHDM", ShapeokoHDMDefinition()},
		{"XCarvePro", XCarveProDefinition()},
		{"OpenBuildsLead", OpenBuildsLeadDefinition()},
		{"SienciLongMillMK2", SienciLongMillMK2Definition()},
		{"StepcraftD840", StepcraftD840Definition()},
		{"CrealityFalcon2", CrealityFalcon2Definition()},
	}

	expectedMappings := map[string]string{
		"Idle":  "idle",
		"Run":   "running",
		"Hold":  "paused",
		"Alarm": "error",
	}

	for _, dc := range grblDefs {
		t.Run(dc.name, func(t *testing.T) {
			for raw, expected := range expectedMappings {
				mapped, ok := dc.def.StatusMapping[raw]
				assert.True(t, ok, "status %q should be mapped", raw)
				assert.Equal(t, expected, mapped)
			}
		})
	}
}

func TestGRBLMachines_TelemetryParse(t *testing.T) {
	grblDefs := []struct {
		name string
		def  *MachineDefinition
	}{
		{"ShapeokoHDM", ShapeokoHDMDefinition()},
		{"XCarvePro", XCarveProDefinition()},
		{"OpenBuildsLead", OpenBuildsLeadDefinition()},
		{"SienciLongMillMK2", SienciLongMillMK2Definition()},
		{"StepcraftD840", StepcraftD840Definition()},
		{"CrealityFalcon2", CrealityFalcon2Definition()},
	}

	for _, dc := range grblDefs {
		t.Run(dc.name, func(t *testing.T) {
			pos, ok := dc.def.TelemetryParse["position"]
			assert.True(t, ok, "position telemetry should exist")
			assert.Equal(t, "position", pos.MetricType)
			assert.Equal(t, "mm", pos.Unit)
			assert.NotEmpty(t, pos.Pattern)
			assert.Greater(t, pos.ValueIndex, 0)
		})
	}
}

func TestResinPrinters_Resolution(t *testing.T) {
	tests := []struct {
		name       string
		def        *MachineDefinition
		xyMicrons  float64
	}{
		{"ElegooSaturn4Ultra", ElegooSaturn4UltraDefinition(), 18.0},
		{"AnycubicPhotonMonoM7", AnycubicPhotonMonoM7Definition(), 40.0},
		{"PhrozenSonicMighty8K", PhrozenSonicMighty8KDefinition(), 28.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, ok := tc.def.Capabilities["resolution"].(map[string]interface{})
			assert.True(t, ok, "resolution capability should exist")
			assert.Equal(t, tc.xyMicrons, res["xy_microns"])
		})
	}
}

func TestLaserMachines_LaserPower(t *testing.T) {
	tests := []struct {
		name      string
		def       *MachineDefinition
		watts     float64
		laserType string
	}{
		{"GlowforgePro", GlowforgeProDefinition(), 45.0, "CO2"},
		{"TrotecSpeedy360", TrotecSpeedyDefinition(), 120.0, "CO2"},
		{"CrealityFalcon2", CrealityFalcon2Definition(), 22.0, "diode"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lp, ok := tc.def.Capabilities["laser_power"].(map[string]interface{})
			assert.True(t, ok, "laser_power capability should exist")
			assert.Equal(t, tc.watts, lp["watts"])
			assert.Equal(t, tc.laserType, lp["laser_type"])
		})
	}
}

func TestRobotArms_DOF(t *testing.T) {
	tests := []struct {
		name string
		def  *MachineDefinition
		dof  int
	}{
		{"URGeneric", URGenericDefinition(), 6},
		{"DobotMagician", DobotMagicianDefinition(), 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wv, ok := tc.def.Capabilities["work_volume"].(map[string]interface{})
			assert.True(t, ok, "work_volume capability should exist")
			assert.Equal(t, tc.dof, wv["dof"])
		})
	}
}

func TestPickAndPlace_FeederCount(t *testing.T) {
	tests := []struct {
		name  string
		def   *MachineDefinition
		count int
	}{
		{"LumenPnP", LumenPnPDefinition(), 48},
		{"IndexPnP", IndexPnPDefinition(), 20},
		{"NeodenYY1", NeodenYY1Definition(), 24},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc, ok := tc.def.Capabilities["feeder_count"].(map[string]interface{})
			assert.True(t, ok, "feeder_count capability should exist")
			assert.Equal(t, tc.count, fc["count"])
		})
	}
}

func TestVinylCutters_CuttingForce(t *testing.T) {
	tests := []struct {
		name     string
		def      *MachineDefinition
		maxGrams int
	}{
		{"SilhouetteCameo5", SilhouetteCameo5Definition(), 210},
		{"GraphtecCE8000", GraphtecCE8000Definition(), 600},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cf, ok := tc.def.Capabilities["cutting_force"].(map[string]interface{})
			assert.True(t, ok, "cutting_force capability should exist")
			assert.Equal(t, tc.maxGrams, cf["max_grams"])
		})
	}
}

func TestLumenPnP_VisionSystem(t *testing.T) {
	def := LumenPnPDefinition()
	vs, ok := def.Capabilities["vision_system"].(map[string]interface{})
	assert.True(t, ok, "vision_system capability should exist on LumenPnP")
	assert.Equal(t, true, vs["supported"])
	assert.Equal(t, "bottom + top cameras", vs["type"])
}

func TestShapeokoHDM_SpindleSpeed(t *testing.T) {
	def := ShapeokoHDMDefinition()
	ss, ok := def.Capabilities["spindle_speed"].(map[string]interface{})
	assert.True(t, ok, "spindle_speed capability should exist on Shapeoko HDM")
	assert.Equal(t, 0, ss["min_rpm"])
	assert.Equal(t, 24000, ss["max_rpm"])
}
