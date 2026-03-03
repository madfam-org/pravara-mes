package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- xTool P2S ---

func TestXToolP2SDefinition_BasicFields(t *testing.T) {
	def := XToolP2SDefinition()

	assert.Equal(t, "xTool", def.Manufacturer)
	assert.Equal(t, "P2S", def.Model)
	assert.Equal(t, MachineTypeLaserCutter, def.Type)
	assert.Equal(t, ProtocolXTool, def.Protocol)
	assert.Equal(t, ConnectionHTTP, def.Connection)
}

func TestXToolP2SDefinition_Capabilities(t *testing.T) {
	def := XToolP2SDefinition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 600.0, workVolume["x_mm"])
	assert.Equal(t, 400.0, workVolume["y_mm"])
	assert.Equal(t, 0.0, workVolume["z_mm"])

	laserPower, ok := def.Capabilities["laser_power"].(map[string]interface{})
	assert.True(t, ok, "laser_power capability should exist")
	assert.Equal(t, 55.0, laserPower["watts"])
	assert.Equal(t, "CO2", laserPower["type"])

	camera, ok := def.Capabilities["camera"].(map[string]interface{})
	assert.True(t, ok, "camera capability should exist")
	assert.Equal(t, true, camera["supported"])
	assert.Equal(t, "16MP", camera["resolution"])
}

// --- xTool S1 ---

func TestXToolS1Definition_BasicFields(t *testing.T) {
	def := XToolS1Definition()

	assert.Equal(t, "xTool", def.Manufacturer)
	assert.Equal(t, "S1", def.Model)
	assert.Equal(t, MachineTypeLaserEngraver, def.Type)
	assert.Equal(t, ProtocolXTool, def.Protocol)
	assert.Equal(t, ConnectionHTTP, def.Connection)
}

func TestXToolS1Definition_Capabilities(t *testing.T) {
	def := XToolS1Definition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 498.0, workVolume["x_mm"])
	assert.Equal(t, 319.0, workVolume["y_mm"])

	laserPower, ok := def.Capabilities["laser_power"].(map[string]interface{})
	assert.True(t, ok, "laser_power capability should exist")
	assert.Equal(t, 40.0, laserPower["watts"])
	assert.Equal(t, "diode", laserPower["type"])
}

// --- xTool F1 Ultra ---

func TestXToolF1UltraDefinition_BasicFields(t *testing.T) {
	def := XToolF1UltraDefinition()

	assert.Equal(t, "xTool", def.Manufacturer)
	assert.Equal(t, "F1 Ultra", def.Model)
	assert.Equal(t, MachineTypeLaserEngraver, def.Type)
	assert.Equal(t, ProtocolXTool, def.Protocol)
	assert.Equal(t, ConnectionHTTP, def.Connection)
}

func TestXToolF1UltraDefinition_Capabilities(t *testing.T) {
	def := XToolF1UltraDefinition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 115.0, workVolume["x_mm"])
	assert.Equal(t, 115.0, workVolume["y_mm"])

	laserPower, ok := def.Capabilities["laser_power"].(map[string]interface{})
	assert.True(t, ok, "laser_power capability should exist")
	assert.Equal(t, 20.0, laserPower["watts"])
	assert.Equal(t, "diode", laserPower["type"])
	assert.Equal(t, 2.0, laserPower["ir_watts"])
}

// --- xTool Common Commands (table-driven across all 3 models) ---

func TestXToolDefinitions_Commands(t *testing.T) {
	definitions := []struct {
		name string
		def  *MachineDefinition
	}{
		{"P2S", XToolP2SDefinition()},
		{"S1", XToolS1Definition()},
		{"F1 Ultra", XToolF1UltraDefinition()},
	}

	expectedCommands := []struct {
		key      string
		name     string
		hasParam bool
	}{
		{"home", "Home", false},
		{"pause", "Pause Job", false},
		{"resume", "Resume Job", false},
		{"stop", "Stop Job", false},
		{"gcode_line", "Send G-code Line", true},
		{"get_status", "Get Status", false},
	}

	for _, d := range definitions {
		t.Run(d.name, func(t *testing.T) {
			for _, tc := range expectedCommands {
				t.Run(tc.key, func(t *testing.T) {
					cmd, ok := d.def.Commands[tc.key]
					assert.True(t, ok, "command %q should exist", tc.key)
					assert.Equal(t, tc.name, cmd.Name)
					assert.Greater(t, cmd.Timeout.Seconds(), 0.0)
					if tc.hasParam {
						assert.NotEmpty(t, cmd.Parameters)
					}
				})
			}
		})
	}
}

func TestXToolDefinitions_StatusMapping(t *testing.T) {
	definitions := []struct {
		name string
		def  *MachineDefinition
	}{
		{"P2S", XToolP2SDefinition()},
		{"S1", XToolS1Definition()},
		{"F1 Ultra", XToolF1UltraDefinition()},
	}

	tests := []struct {
		raw      string
		expected string
	}{
		{"idle", "idle"},
		{"running", "running"},
		{"paused", "paused"},
		{"error", "error"},
	}

	for _, d := range definitions {
		t.Run(d.name, func(t *testing.T) {
			for _, tc := range tests {
				t.Run(tc.raw, func(t *testing.T) {
					mapped, ok := d.def.StatusMapping[tc.raw]
					assert.True(t, ok)
					assert.Equal(t, tc.expected, mapped)
				})
			}
		})
	}
}

func TestXToolDefinitions_TelemetryParse(t *testing.T) {
	definitions := []struct {
		name string
		def  *MachineDefinition
	}{
		{"P2S", XToolP2SDefinition()},
		{"S1", XToolS1Definition()},
		{"F1 Ultra", XToolF1UltraDefinition()},
	}

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"machine_state", "machine_state", "state"},
		{"laser_temp", "laser_temp", "celsius"},
		{"job_progress", "job_progress", "percent"},
	}

	for _, d := range definitions {
		t.Run(d.name, func(t *testing.T) {
			for _, tc := range expectedTelemetry {
				t.Run(tc.key, func(t *testing.T) {
					td, ok := d.def.TelemetryParse[tc.key]
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

// --- Roland GR2-640 ---

func TestRolandGR2Definition_BasicFields(t *testing.T) {
	def := RolandGR2Definition()

	assert.Equal(t, "Roland DG", def.Manufacturer)
	assert.Equal(t, "CAMM-1 GR2-640", def.Model)
	assert.Equal(t, MachineTypeVinylCutter, def.Type)
	assert.Equal(t, ProtocolCammGL, def.Protocol)
	assert.Equal(t, ConnectionSerial, def.Connection)
}

func TestRolandGR2Definition_Capabilities(t *testing.T) {
	def := RolandGR2Definition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 1651.0, workVolume["x_mm"])
	assert.Equal(t, 50000.0, workVolume["y_mm"])
	assert.Equal(t, 0.0, workVolume["z_mm"])

	cuttingForce, ok := def.Capabilities["cutting_force"].(map[string]interface{})
	assert.True(t, ok, "cutting_force capability should exist")
	assert.Equal(t, 600, cuttingForce["max_grams"])
}

// --- Roland GS2-24 ---

func TestRolandGS2Definition_BasicFields(t *testing.T) {
	def := RolandGS2Definition()

	assert.Equal(t, "Roland DG", def.Manufacturer)
	assert.Equal(t, "CAMM-1 GS2-24", def.Model)
	assert.Equal(t, MachineTypeVinylCutter, def.Type)
	assert.Equal(t, ProtocolCammGL, def.Protocol)
	assert.Equal(t, ConnectionSerial, def.Connection)
}

func TestRolandGS2Definition_Capabilities(t *testing.T) {
	def := RolandGS2Definition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 584.0, workVolume["x_mm"])
	assert.Equal(t, 25000.0, workVolume["y_mm"])

	cuttingForce, ok := def.Capabilities["cutting_force"].(map[string]interface{})
	assert.True(t, ok, "cutting_force capability should exist")
	assert.Equal(t, 350, cuttingForce["max_grams"])
}

// --- Roland Common Commands (table-driven across both models) ---

func TestRolandDefinitions_Commands(t *testing.T) {
	definitions := []struct {
		name string
		def  *MachineDefinition
	}{
		{"GR2-640", RolandGR2Definition()},
		{"GS2-24", RolandGS2Definition()},
	}

	expectedCommands := []struct {
		key      string
		name     string
		template string
	}{
		{"init", "Initialize", "IN;"},
		{"move", "Pen Up Move", "PU {x},{y};"},
		{"cut", "Pen Down Cut", "PD {x},{y};"},
		{"set_speed", "Set Velocity", "VS {speed};"},
		{"set_force", "Set Force", "FP {force};"},
		{"select_pen", "Select Pen", "SP {pen};"},
		{"get_position", "Output Actual Position", "OA;"},
		{"home", "Home", "IN;PU 0,0;"},
	}

	for _, d := range definitions {
		t.Run(d.name, func(t *testing.T) {
			for _, tc := range expectedCommands {
				t.Run(tc.key, func(t *testing.T) {
					cmd, ok := d.def.Commands[tc.key]
					assert.True(t, ok, "command %q should exist", tc.key)
					assert.Equal(t, tc.name, cmd.Name)
					assert.Equal(t, tc.template, cmd.Template)
					assert.Greater(t, cmd.Timeout.Seconds(), 0.0)
				})
			}
		})
	}
}

func TestRolandDefinitions_StatusMapping(t *testing.T) {
	definitions := []struct {
		name string
		def  *MachineDefinition
	}{
		{"GR2-640", RolandGR2Definition()},
		{"GS2-24", RolandGS2Definition()},
	}

	tests := []struct {
		raw      string
		expected string
	}{
		{"ready", "idle"},
		{"cutting", "running"},
		{"error", "error"},
	}

	for _, d := range definitions {
		t.Run(d.name, func(t *testing.T) {
			for _, tc := range tests {
				t.Run(tc.raw, func(t *testing.T) {
					mapped, ok := d.def.StatusMapping[tc.raw]
					assert.True(t, ok)
					assert.Equal(t, tc.expected, mapped)
				})
			}
		})
	}
}

func TestRolandDefinitions_TelemetryParse(t *testing.T) {
	definitions := []struct {
		name string
		def  *MachineDefinition
	}{
		{"GR2-640", RolandGR2Definition()},
		{"GS2-24", RolandGS2Definition()},
	}

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"position_x", "position_x", "plotter_units"},
		{"position_y", "position_y", "plotter_units"},
	}

	for _, d := range definitions {
		t.Run(d.name, func(t *testing.T) {
			for _, tc := range expectedTelemetry {
				t.Run(tc.key, func(t *testing.T) {
					td, ok := d.def.TelemetryParse[tc.key]
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

// --- Registry Integration ---

func TestRegistryContainsXToolAndRolandDefinitions(t *testing.T) {
	r := NewRegistry()

	expectedKeys := []string{
		"xtool_p2s",
		"xtool_s1",
		"xtool_f1_ultra",
		"roland_gr2_640",
		"roland_gs2_24",
	}

	for _, key := range expectedKeys {
		t.Run(key, func(t *testing.T) {
			def, ok := r.GetDefinition(key)
			assert.True(t, ok, "registry should contain %q", key)
			assert.NotNil(t, def)
			assert.NotEmpty(t, def.Manufacturer)
			assert.NotEmpty(t, def.Model)
		})
	}
}
