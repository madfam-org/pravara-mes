package matchmaking

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/fabrication"
)

func f(v float64) *float64 { return &v }

// The fabrication-prep catalog entries pravara relies on (p5fab report).
var catalog = []SlicingProfile{
	{Ref: "klipper-corexy-350-0.4@1", ID: "klipper-corexy-350-0.4", Kind: "printer", Target: "klipper_gcode"},
	{Ref: "bambu-a1-0.4@1", ID: "bambu-a1-0.4", Kind: "printer", Target: "bambu_3mf"},
	{Ref: "petg-generic-klipper@1", ID: "petg-generic-klipper", Kind: "filament", MaterialClass: "petg", Printers: []string{"klipper-corexy-350-0.4"}},
	{Ref: "tpu-95a-klipper@1", ID: "tpu-95a-klipper", Kind: "filament", MaterialClass: "tpu-95a", RequiresProcessTag: "tpu-safe", Printers: []string{"klipper-corexy-350-0.4"}},
	{Ref: "tpu-95a-bambu-a1@1", ID: "tpu-95a-bambu-a1", Kind: "filament", MaterialClass: "tpu-95a", RequiresProcessTag: "tpu-safe", Printers: []string{"bambu-a1-0.4"}},
	{Ref: "standard-0.20-klipper@1", ID: "standard-0.20-klipper", Kind: "process", Printers: []string{"klipper-corexy-350-0.4"}, Tags: []string{"standard"}},
	{Ref: "tpu-safe-0.20-klipper@1", ID: "tpu-safe-0.20-klipper", Kind: "process", Printers: []string{"klipper-corexy-350-0.4"}, Tags: []string{"tpu-safe"}},
}

func voron(code string) Machine {
	return Machine{ID: uuid.New(), Code: code, Name: code, RegistryStatus: "online",
		Capabilities: map[string]any{"process": "fff", "build_volume_x_mm": 350.0, "build_volume_y_mm": 350.0,
			"build_volume_z_mm": 340.0, "nozzle_diameters_mm": []any{0.4}, "max_hotend_temp_c": 300.0, "max_bed_temp_c": 120.0},
		PrinterProfile: "klipper-corexy-350-0.4@1", Target: "klipper_gcode", MaterialLots: map[int]string{1: "LOT-TPU-7"}}
}

func idle(slots ...MaterialSlot) LiveState {
	return LiveState{Born: true, Status: StateIdle, Materials: slots}
}

func requirements() fabrication.RequirementSet {
	return fabrication.RequirementSet{
		Process:   []string{"fff"},
		Materials: &fabrication.Materials{AnyOf: []string{"tpu-95a", "petg"}, NoneOf: []string{"pla"}},
		ProcessParameters: map[string]fabrication.Bound{
			"nozzle_diameter":    {Value: 0.4, Unit: "mm"},
			"nozzle_temperature": {Min: f(220), Max: f(250)},
			"wall_loops":         {Min: f(3)},
		},
	}
}

func check(c Candidate, name string) Check {
	for _, ch := range c.Checks {
		if ch.Name == name {
			return ch
		}
	}
	return Check{}
}

func TestRankExplainsAndOrdersCandidates(t *testing.T) {
	good, busy, pla, loaded := voron("voron-a"), voron("voron-b"), voron("voron-c"), voron("voron-d")
	loaded.ActiveTasks = 2
	live := map[uuid.UUID]LiveState{
		good.ID:   idle(MaterialSlot{Slot: 1, Class: "tpu-95a", Loaded: true}),
		busy.ID:   {Born: true, Status: StatePrinting, Materials: []MaterialSlot{{Slot: 1, Class: "tpu-95a", Loaded: true}}},
		pla.ID:    idle(MaterialSlot{Slot: 1, Class: "pla", Loaded: true}),
		loaded.ID: idle(MaterialSlot{Slot: 1, Class: "petg", Loaded: true}),
	}
	box := &fabrication.BoundingBox{X: 80, Y: 60, Z: 40, Source: "order_item.specifications"}
	res := Rank(Inputs{Requirements: requirements(), BoundingBox: box, Catalog: catalog}, []Machine{loaded, pla, busy, good}, live)

	require.Len(t, res.Candidates, 4)
	assert.Equal(t, "voron-a", res.Candidates[0].MachineCode)
	assert.Equal(t, 1, res.Candidates[0].Rank)
	assert.Equal(t, "voron-d", res.Candidates[1].MachineCode, "eligible but busier ranks second")
	assert.Equal(t, 2, res.Candidates[1].Rank)
	assert.False(t, res.Candidates[2].Eligible)
	assert.Empty(t, res.Gaps)

	best := res.Best()
	require.NotNil(t, best)
	assert.Equal(t, Selection{MaterialClass: "tpu-95a", MaterialSlot: 1, MaterialLot: "LOT-TPU-7",
		PrinterProfile: "klipper-corexy-350-0.4@1", FilamentProfile: "tpu-95a-klipper@1",
		ProcessProfile: "tpu-safe-0.20-klipper@1", Target: "klipper_gcode"}, best.Selection)
	for _, name := range []string{"registry_status", "live_state", "reservation", "process", "material", "nozzle",
		"hotend_temperature", "build_volume", "slicing_profiles"} {
		assert.Equal(t, Pass, check(*best, name).Result, name)
	}
	assert.Equal(t, NotRequired, check(*best, "bed_temperature").Result)
	assert.Equal(t, "petg-generic-klipper@1", res.Candidates[1].Selection.FilamentProfile)
	assert.Equal(t, "standard-0.20-klipper@1", res.Candidates[1].Selection.ProcessProfile)

	byCode := map[string]Candidate{}
	for _, c := range res.Candidates {
		byCode[c.MachineCode] = c
	}
	assert.Equal(t, Fail, check(byCode["voron-b"], "live_state").Result)
	assert.Contains(t, check(byCode["voron-b"], "live_state").Detail, "printing")
	assert.Equal(t, Fail, check(byCode["voron-c"], "material").Result)
	assert.Contains(t, check(byCode["voron-c"], "material").Detail, "slot 1: pla")
}

func TestHardChecksFailClosed(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Machine, *LiveState)
		check  string
	}{
		"no birth":               {func(_ *Machine, l *LiveState) { l.Born = false }, "live_state"},
		"maintenance":            {func(m *Machine, _ *LiveState) { m.RegistryStatus = "maintenance" }, "registry_status"},
		"reserved":               {func(m *Machine, _ *LiveState) { id := uuid.New(); m.ReservedBy = &id }, "reservation"},
		"wrong process":          {func(m *Machine, _ *LiveState) { m.Capabilities["process"] = "sla" }, "process"},
		"no nozzle declared":     {func(m *Machine, _ *LiveState) { delete(m.Capabilities, "nozzle_diameters_mm") }, "nozzle"},
		"0.6 nozzle":             {func(m *Machine, _ *LiveState) { m.Capabilities["nozzle_diameters_mm"] = []any{0.6} }, "nozzle"},
		"cold hotend":            {func(m *Machine, _ *LiveState) { m.Capabilities["max_hotend_temp_c"] = 210.0 }, "hotend_temperature"},
		"small bed":              {func(m *Machine, _ *LiveState) { m.Capabilities["build_volume_z_mm"] = 30.0 }, "build_volume"},
		"unloaded":               {func(_ *Machine, l *LiveState) { l.Materials[0].Loaded = false }, "material"},
		"unknown class":          {func(_ *Machine, l *LiveState) { l.Materials[0].Class = "" }, "material"},
		"no profile":             {func(m *Machine, _ *LiveState) { m.PrinterProfile = "" }, "slicing_profiles"},
		"profile not in catalog": {func(m *Machine, _ *LiveState) { m.PrinterProfile = "prusa-mk4@1" }, "slicing_profiles"},
		"target mismatch":        {func(m *Machine, _ *LiveState) { m.Target = "bambu_3mf" }, "slicing_profiles"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := voron("v")
			l := idle(MaterialSlot{Slot: 1, Class: "tpu-95a", Loaded: true})
			tc.mutate(&m, &l)
			res := Rank(Inputs{Requirements: requirements(), BoundingBox: &fabrication.BoundingBox{X: 50, Y: 50, Z: 50},
				Catalog: catalog}, []Machine{m}, map[uuid.UUID]LiveState{m.ID: l})
			c := res.Candidates[0]
			assert.False(t, c.Eligible)
			assert.Equal(t, 0, c.Rank)
			assert.Equal(t, Fail, check(c, tc.check).Result, "%+v", c.Checks)
			assert.Nil(t, res.Best())
		})
	}
}

func TestMissingInputsAreReportedAsGaps(t *testing.T) {
	m := voron("v")
	l := idle(MaterialSlot{Slot: 2, Class: "petg", Loaded: true})
	res := Rank(Inputs{}, []Machine{m}, map[uuid.UUID]LiveState{m.ID: l})
	assert.ElementsMatch(t, []string{GapNoRequirements, GapBoundingBox, GapSlicingCatalog}, res.Gaps)
	c := res.Candidates[0]
	assert.True(t, c.Eligible, "unknown bbox and catalog do not block a dry run: %+v", c.Checks)
	assert.Equal(t, Unknown, check(c, "build_volume").Result)
	assert.Contains(t, check(c, "build_volume").Detail, "does not compute geometry")
	assert.Equal(t, Unknown, check(c, "slicing_profiles").Result)
	assert.Equal(t, "petg", c.Selection.MaterialClass)
	assert.Empty(t, c.Selection.MaterialLot, "slot 2 has no recorded lot")

	strict := Rank(Inputs{RequireBoundingBox: true, Catalog: catalog}, []Machine{m}, map[uuid.UUID]LiveState{m.ID: l})
	assert.Equal(t, Fail, check(strict.Candidates[0], "build_volume").Result)
	// A zero box (yantra4d's analysis fallback) counts as missing.
	zero := Rank(Inputs{BoundingBox: &fabrication.BoundingBox{}}, []Machine{m}, map[uuid.UUID]LiveState{m.ID: l})
	assert.Contains(t, zero.Gaps, GapBoundingBox)
	// A machine without any live state row is not eligible.
	none := Rank(Inputs{}, []Machine{m}, nil)
	assert.False(t, none.Candidates[0].Eligible)
	assert.Contains(t, check(none.Candidates[0], "live_state").Detail, "no Sparkplug birth")
}

func TestDeviceBirthCapabilitiesOverrideRegistryAndConflictsAreExplained(t *testing.T) {
	m := voron("v")
	l := idle(MaterialSlot{Slot: 1, Class: "tpu-95a", Loaded: true})
	l.Capabilities = map[string]any{"max_hotend_temp_c": int64(200), "nozzle_diameters_mm": []float64{0.4}}
	res := Rank(Inputs{Requirements: requirements(), Catalog: catalog}, []Machine{m}, map[uuid.UUID]LiveState{m.ID: l})
	c := res.Candidates[0]
	assert.Equal(t, "device_birth", c.CapabilitySources["max_hotend_temp_c"])
	assert.Equal(t, "registry", c.CapabilitySources["build_volume_x_mm"])
	require.Len(t, c.CapabilityConflicts, 1)
	assert.True(t, strings.HasPrefix(c.CapabilityConflicts[0], "max_hotend_temp_c"))
	assert.Equal(t, Fail, check(c, "hotend_temperature").Result, "the device's own 200 °C limit wins")
}

func TestMaterialsAllowsAndPartOverrides(t *testing.T) {
	m := &fabrication.Materials{AnyOf: []string{"petg"}, NoneOf: []string{"pla"}}
	assert.True(t, m.Allows("petg"))
	assert.False(t, m.Allows("pla"))
	assert.False(t, m.Allows("tpu-95a"))
	assert.False(t, m.Allows(""))
	var none *fabrication.Materials
	assert.True(t, none.Allows("abs"))

	rp := &fabrication.RequirementProfile{RequirementSet: requirements(),
		Parts: map[string]fabrication.RequirementSet{"gasket": {Materials: &fabrication.Materials{AnyOf: []string{"tpu-95a"}}}}}
	assert.Equal(t, []string{"tpu-95a"}, rp.ForPart("gasket").Materials.AnyOf)
	assert.Equal(t, []string{"tpu-95a", "petg"}, rp.ForPart("frame").Materials.AnyOf)
	assert.Equal(t, []string{"nozzle_diameter", "nozzle_temperature", "wall_loops"}, rp.ForPart("gasket").ParameterKeys())
}
