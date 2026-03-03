package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBambuP1SDefinition_BasicFields(t *testing.T) {
	def := BambuP1SDefinition()

	assert.Equal(t, "Bambu Lab", def.Manufacturer)
	assert.Equal(t, "P1S", def.Model)
	assert.Equal(t, MachineType3DPrinterFDM, def.Type)
	assert.Equal(t, ProtocolBambuMQTT, def.Protocol)
	assert.Equal(t, ConnectionMQTT, def.Connection)
}

func TestBambuA1Definition_BasicFields(t *testing.T) {
	def := BambuA1Definition()

	assert.Equal(t, "Bambu Lab", def.Manufacturer)
	assert.Equal(t, "A1", def.Model)
	assert.Equal(t, MachineType3DPrinterFDM, def.Type)
	assert.Equal(t, ProtocolBambuMQTT, def.Protocol)
	assert.Equal(t, ConnectionMQTT, def.Connection)
}

func TestBambuX1CDefinition_BasicFields(t *testing.T) {
	def := BambuX1CDefinition()

	assert.Equal(t, "Bambu Lab", def.Manufacturer)
	assert.Equal(t, "X1 Carbon", def.Model)
	assert.Equal(t, MachineType3DPrinterFDM, def.Type)
	assert.Equal(t, ProtocolBambuMQTT, def.Protocol)
	assert.Equal(t, ConnectionMQTT, def.Connection)
}

func TestBambuDefinitions_BuildVolume(t *testing.T) {
	definitions := []struct {
		name string
		def  *MachineDefinition
	}{
		{"P1S", BambuP1SDefinition()},
		{"A1", BambuA1Definition()},
		{"X1C", BambuX1CDefinition()},
	}

	for _, tc := range definitions {
		t.Run(tc.name, func(t *testing.T) {
			workVolume, ok := tc.def.Capabilities["work_volume"].(map[string]interface{})
			assert.True(t, ok, "work_volume capability should exist")
			assert.Equal(t, 256.0, workVolume["x_mm"])
			assert.Equal(t, 256.0, workVolume["y_mm"])
			assert.Equal(t, 256.0, workVolume["z_mm"])
		})
	}
}

func TestBambuDefinitions_Commands(t *testing.T) {
	definitions := []struct {
		name string
		def  *MachineDefinition
	}{
		{"P1S", BambuP1SDefinition()},
		{"A1", BambuA1Definition()},
		{"X1C", BambuX1CDefinition()},
	}

	expectedCommands := []string{
		"home", "pause", "resume", "stop",
		"push_status", "gcode_line", "emergency_stop",
	}

	for _, dc := range definitions {
		t.Run(dc.name, func(t *testing.T) {
			for _, cmdKey := range expectedCommands {
				t.Run(cmdKey, func(t *testing.T) {
					cmd, ok := dc.def.Commands[cmdKey]
					assert.True(t, ok, "command %q should exist", cmdKey)
					assert.NotEmpty(t, cmd.Name, "command %q should have a name", cmdKey)
					assert.NotEmpty(t, cmd.Template, "command %q should have a template", cmdKey)
					assert.Greater(t, cmd.Timeout.Seconds(), 0.0, "command %q should have a positive timeout", cmdKey)
				})
			}
		})
	}
}

func TestBambuDefinitions_TelemetryParse(t *testing.T) {
	definitions := []struct {
		name string
		def  *MachineDefinition
	}{
		{"P1S", BambuP1SDefinition()},
		{"A1", BambuA1Definition()},
		{"X1C", BambuX1CDefinition()},
	}

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"nozzle_temp", "nozzle_temp", "celsius"},
		{"bed_temp", "bed_temp", "celsius"},
		{"chamber_temp", "chamber_temp", "celsius"},
		{"print_percent", "print_percent", "percent"},
	}

	for _, dc := range definitions {
		t.Run(dc.name, func(t *testing.T) {
			for _, tc := range expectedTelemetry {
				t.Run(tc.key, func(t *testing.T) {
					td, ok := dc.def.TelemetryParse[tc.key]
					assert.True(t, ok, "telemetry %q should exist", tc.key)
					assert.Equal(t, tc.metricType, td.MetricType)
					assert.Equal(t, tc.unit, td.Unit)
					assert.NotEmpty(t, td.Pattern)
					assert.Greater(t, td.ValueIndex, 0)
				})
			}
		})
	}
}

func TestBambuDefinitions_StatusMapping(t *testing.T) {
	definitions := []struct {
		name string
		def  *MachineDefinition
	}{
		{"P1S", BambuP1SDefinition()},
		{"A1", BambuA1Definition()},
		{"X1C", BambuX1CDefinition()},
	}

	expectedMappings := []struct {
		raw      string
		expected string
	}{
		{"IDLE", "idle"},
		{"RUNNING", "running"},
		{"PAUSE", "paused"},
		{"FAILED", "error"},
		{"FINISH", "idle"},
	}

	for _, dc := range definitions {
		t.Run(dc.name, func(t *testing.T) {
			for _, tc := range expectedMappings {
				t.Run(tc.raw, func(t *testing.T) {
					mapped, ok := dc.def.StatusMapping[tc.raw]
					assert.True(t, ok, "status %q should be mapped", tc.raw)
					assert.Equal(t, tc.expected, mapped)
				})
			}
		})
	}
}

func TestBambuX1CDefinition_VisionSystem(t *testing.T) {
	def := BambuX1CDefinition()

	vision, ok := def.Capabilities["vision_system"].(map[string]interface{})
	assert.True(t, ok, "vision_system capability should exist on X1C")
	assert.Equal(t, true, vision["supported"])
	assert.Equal(t, "lidar", vision["type"])
}

func TestBambuA1Definition_NoChamberHeating(t *testing.T) {
	def := BambuA1Definition()

	chamber, ok := def.Capabilities["chamber_temp"].(map[string]interface{})
	assert.True(t, ok, "chamber_temp capability should exist on A1")
	assert.Equal(t, "none", chamber["type"])
}

func TestBambuP1SDefinition_PassiveChamber(t *testing.T) {
	def := BambuP1SDefinition()

	chamber, ok := def.Capabilities["chamber_temp"].(map[string]interface{})
	assert.True(t, ok, "chamber_temp capability should exist on P1S")
	assert.Equal(t, "passive", chamber["type"])
}

func TestBambuX1CDefinition_ActiveChamber(t *testing.T) {
	def := BambuX1CDefinition()

	chamber, ok := def.Capabilities["chamber_temp"].(map[string]interface{})
	assert.True(t, ok, "chamber_temp capability should exist on X1C")
	assert.Equal(t, 0.0, chamber["min_celsius"])
	assert.Equal(t, 60.0, chamber["max_celsius"])
}
