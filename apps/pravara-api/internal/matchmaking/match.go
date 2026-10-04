package matchmaking

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/fabrication"
)

// Inputs of one match.
type Inputs struct {
	Requirements fabrication.RequirementSet
	BoundingBox  *fabrication.BoundingBox
	// RequireBoundingBox fails the build-volume check when no box is known.
	RequireBoundingBox bool
	// Catalog is fabrication-prep's profile catalog; nil reports the slicing
	// check as unknown (dry runs without the slicing client).
	Catalog []SlicingProfile
}

// Rank evaluates every machine and ranks the eligible ones. live may lack
// entries (no birth seen); those machines are not eligible.
func Rank(in Inputs, machines []Machine, live map[uuid.UUID]LiveState) Result {
	res := Result{}
	if len(in.Requirements.Process) == 0 && in.Requirements.Materials.Empty() && len(in.Requirements.ProcessParameters) == 0 {
		res.Gaps = append(res.Gaps, GapNoRequirements)
	}
	if !in.BoundingBox.Valid() {
		res.Gaps = append(res.Gaps, GapBoundingBox)
	}
	if in.Catalog == nil {
		res.Gaps = append(res.Gaps, GapSlicingCatalog)
	}
	for _, m := range machines {
		state, known := live[m.ID]
		res.Candidates = append(res.Candidates, evaluate(in, m, state, known))
	}
	sort.SliceStable(res.Candidates, func(i, j int) bool {
		a, b := res.Candidates[i], res.Candidates[j]
		if a.Eligible != b.Eligible {
			return a.Eligible
		}
		if a.ActiveTasks != b.ActiveTasks {
			return a.ActiveTasks < b.ActiveTasks
		}
		return a.MachineCode < b.MachineCode
	})
	rank := 0
	for i := range res.Candidates {
		if res.Candidates[i].Eligible {
			rank++
			res.Candidates[i].Rank = rank
		}
	}
	return res
}

func evaluate(in Inputs, m Machine, state LiveState, known bool) Candidate {
	c := Candidate{MachineID: m.ID, MachineCode: m.Code, MachineName: m.Name, ActiveTasks: m.ActiveTasks}
	caps := mergeCapabilities(&c, m.Capabilities, state.Capabilities)
	add := func(name, result, detail string) { c.Checks = append(c.Checks, Check{name, result, detail}) }

	switch m.RegistryStatus {
	case "error", "maintenance":
		add("registry_status", Fail, "machine is in "+m.RegistryStatus)
	default:
		add("registry_status", Pass, m.RegistryStatus)
	}

	switch {
	case !known:
		add("live_state", Fail, "no Sparkplug birth recorded for this device")
	case !state.Born:
		add("live_state", Fail, "device is not born (DDEATH/NDEATH or no DBIRTH in this session)")
	case state.Status != StateIdle:
		add("live_state", Fail, fmt.Sprintf("State/Status is %q, not idle", state.Status))
	default:
		add("live_state", Pass, "born and idle")
	}

	if m.ReservedBy != nil {
		add("reservation", Fail, "reserved by dispatch "+m.ReservedBy.String())
	} else {
		add("reservation", Pass, "not reserved")
	}

	checkProcess(&c, in.Requirements.Process, caps)
	slot := checkMaterials(&c, in.Requirements.Materials, state.Materials)
	if slot != nil {
		c.Selection.MaterialClass = slot.Class
		c.Selection.MaterialSlot = slot.Slot
		c.Selection.MaterialLot = m.MaterialLots[slot.Slot]
	}
	checkNozzle(&c, in.Requirements.ProcessParameters, caps)
	checkMinAgainstCapability(&c, "hotend_temperature", "nozzle_temperature", "max_hotend_temp_c", in.Requirements.ProcessParameters, caps)
	checkMinAgainstCapability(&c, "bed_temperature", "bed_temperature", "max_bed_temp_c", in.Requirements.ProcessParameters, caps)
	checkBuildVolume(&c, in.BoundingBox, in.RequireBoundingBox, caps)
	checkSlicing(&c, m, in.Catalog)

	c.Eligible = true
	for _, ch := range c.Checks {
		if ch.Result == Fail {
			c.Eligible = false
		}
	}
	return c
}

// mergeCapabilities overlays device-declared (DBIRTH) values on the registry
// and records each value's source and any disagreement.
func mergeCapabilities(c *Candidate, registry, device map[string]any) map[string]any {
	out := map[string]any{}
	c.CapabilitySources = map[string]string{}
	for k, v := range registry {
		out[k] = v
		c.CapabilitySources[k] = "registry"
	}
	for k, v := range device {
		if prev, ok := out[k]; ok && fmt.Sprint(normalise(prev)) != fmt.Sprint(normalise(v)) {
			c.CapabilityConflicts = append(c.CapabilityConflicts,
				fmt.Sprintf("%s: registry %v, device %v (device value used)", k, prev, v))
		}
		out[k] = v
		c.CapabilitySources[k] = "device_birth"
	}
	sort.Strings(c.CapabilityConflicts)
	return out
}

func normalise(v any) any {
	if fs, ok := toFloats(v); ok {
		return fs
	}
	return v
}

func checkProcess(c *Candidate, required []string, caps map[string]any) {
	if len(required) == 0 {
		c.Checks = append(c.Checks, Check{"process", NotRequired, "no process required"})
		return
	}
	have := toStrings(caps["process"])
	if len(have) == 0 {
		c.Checks = append(c.Checks, Check{"process", Fail, "machine declares no process capability"})
		return
	}
	for _, r := range required {
		for _, h := range have {
			if r == h {
				c.Checks = append(c.Checks, Check{"process", Pass, "process " + h})
				return
			}
		}
	}
	c.Checks = append(c.Checks, Check{"process", Fail,
		fmt.Sprintf("machine process %v is not in required %v", have, required)})
}

func checkMaterials(c *Candidate, req *fabrication.Materials, slots []MaterialSlot) *MaterialSlot {
	var loaded []string
	for i := range slots {
		s := slots[i]
		if !s.Loaded {
			continue
		}
		class := s.Class
		if class == "" {
			class = "unknown"
		}
		loaded = append(loaded, fmt.Sprintf("slot %d: %s", s.Slot, class))
		if s.Class != "" && req.Allows(s.Class) {
			detail := fmt.Sprintf("slot %d holds %s", s.Slot, s.Class)
			if req.Empty() {
				detail += " (no material required; loaded class used)"
			}
			c.Checks = append(c.Checks, Check{"material", Pass, detail})
			return &s
		}
	}
	want := "any known class"
	if req != nil && !req.Empty() {
		want = fmt.Sprintf("any_of %v, none_of %v", req.AnyOf, req.NoneOf)
	}
	if len(loaded) == 0 {
		c.Checks = append(c.Checks, Check{"material", Fail, "no material loaded (required " + want + ")"})
	} else {
		c.Checks = append(c.Checks, Check{"material", Fail,
			fmt.Sprintf("loaded [%s] does not satisfy %s", strings.Join(loaded, "; "), want)})
	}
	return nil
}

func checkNozzle(c *Candidate, params map[string]fabrication.Bound, caps map[string]any) {
	b, ok := params["nozzle_diameter"]
	if !ok {
		c.Checks = append(c.Checks, Check{"nozzle", NotRequired, "no nozzle_diameter requirement"})
		return
	}
	have, ok := toFloats(caps["nozzle_diameters_mm"])
	if !ok || len(have) == 0 {
		c.Checks = append(c.Checks, Check{"nozzle", Fail, "machine declares no nozzle_diameters_mm"})
		return
	}
	for _, d := range have {
		if satisfies(b, d) {
			c.Checks = append(c.Checks, Check{"nozzle", Pass, fmt.Sprintf("nozzle %g mm satisfies %s", d, describe(b))})
			return
		}
	}
	c.Checks = append(c.Checks, Check{"nozzle", Fail, fmt.Sprintf("nozzles %v mm do not satisfy %s", have, describe(b))})
}

// checkMinAgainstCapability passes when the machine's maximum reaches the
// requirement's minimum (or exact value).
func checkMinAgainstCapability(c *Candidate, name, param, capKey string, params map[string]fabrication.Bound, caps map[string]any) {
	b, ok := params[param]
	if !ok {
		c.Checks = append(c.Checks, Check{name, NotRequired, "no " + param + " requirement"})
		return
	}
	need := b.Min
	if need == nil {
		if v, ok := toFloat(b.Value); ok {
			need = &v
		}
	}
	if need == nil {
		c.Checks = append(c.Checks, Check{name, NotRequired, param + " has no lower bound"})
		return
	}
	max, ok := toFloat(caps[capKey])
	if !ok {
		c.Checks = append(c.Checks, Check{name, Fail, "machine declares no " + capKey})
		return
	}
	if max >= *need {
		c.Checks = append(c.Checks, Check{name, Pass, fmt.Sprintf("%s %g ≥ required %g", capKey, max, *need)})
		return
	}
	c.Checks = append(c.Checks, Check{name, Fail, fmt.Sprintf("%s %g < required %g", capKey, max, *need)})
}

func checkBuildVolume(c *Candidate, box *fabrication.BoundingBox, require bool, caps map[string]any) {
	if !box.Valid() {
		result := Unknown
		if require {
			result = Fail
		}
		c.Checks = append(c.Checks, Check{"build_volume", result,
			"no part bounding box was provided (yantra4d or the quote supplies it; pravara does not compute geometry)"})
		return
	}
	var vol [3]float64
	for i, k := range []string{"build_volume_x_mm", "build_volume_y_mm", "build_volume_z_mm"} {
		v, ok := toFloat(caps[k])
		if !ok {
			c.Checks = append(c.Checks, Check{"build_volume", Fail, "machine declares no " + k})
			return
		}
		vol[i] = v
	}
	if box.X <= vol[0] && box.Y <= vol[1] && box.Z <= vol[2] {
		c.Checks = append(c.Checks, Check{"build_volume", Pass, fmt.Sprintf(
			"part %gx%gx%g mm fits %gx%gx%g mm as provided (orientation not changed)", box.X, box.Y, box.Z, vol[0], vol[1], vol[2])})
		return
	}
	c.Checks = append(c.Checks, Check{"build_volume", Fail, fmt.Sprintf(
		"part %gx%gx%g mm exceeds %gx%gx%g mm on at least one axis as provided", box.X, box.Y, box.Z, vol[0], vol[1], vol[2])})
}

func checkSlicing(c *Candidate, m Machine, catalog []SlicingProfile) {
	if m.PrinterProfile == "" || m.Target == "" {
		c.Checks = append(c.Checks, Check{"slicing_profiles", Fail,
			"machine has no fabrication-prep printer profile and target (metadata.fabrication_prep)"})
		return
	}
	c.Selection.PrinterProfile, c.Selection.Target = m.PrinterProfile, m.Target
	if catalog == nil {
		c.Checks = append(c.Checks, Check{"slicing_profiles", Unknown, "profile catalog not consulted"})
		return
	}
	printerID := strings.SplitN(m.PrinterProfile, "@", 2)[0]
	var printer *SlicingProfile
	for i := range catalog {
		if catalog[i].Kind == "printer" && catalog[i].ID == printerID {
			printer = &catalog[i]
		}
	}
	if printer == nil {
		c.Checks = append(c.Checks, Check{"slicing_profiles", Fail, "printer profile " + m.PrinterProfile + " is not in the catalog"})
		return
	}
	if printer.Target != "" && printer.Target != m.Target {
		c.Checks = append(c.Checks, Check{"slicing_profiles", Fail,
			fmt.Sprintf("printer profile target %s differs from machine target %s", printer.Target, m.Target)})
		return
	}
	class := c.Selection.MaterialClass
	if class == "" {
		c.Checks = append(c.Checks, Check{"slicing_profiles", Fail, "no material selected"})
		return
	}
	var filament *SlicingProfile
	for i := range catalog {
		p := &catalog[i]
		if p.Kind == "filament" && p.MaterialClass == class && contains(p.Printers, printerID) {
			filament = p
			break
		}
	}
	if filament == nil {
		c.Checks = append(c.Checks, Check{"slicing_profiles", Fail,
			fmt.Sprintf("no %s filament profile for printer %s", class, printerID)})
		return
	}
	tag := filament.RequiresProcessTag
	if tag == "" {
		tag = "standard"
	}
	var process *SlicingProfile
	for i := range catalog {
		p := &catalog[i]
		if p.Kind == "process" && contains(p.Printers, printerID) && contains(p.Tags, tag) {
			process = p
			break
		}
	}
	if process == nil {
		c.Checks = append(c.Checks, Check{"slicing_profiles", Fail,
			fmt.Sprintf("no %q process profile for printer %s", tag, printerID)})
		return
	}
	c.Selection.FilamentProfile, c.Selection.ProcessProfile = filament.Ref, process.Ref
	c.Checks = append(c.Checks, Check{"slicing_profiles", Pass,
		fmt.Sprintf("printer %s, filament %s, process %s", m.PrinterProfile, filament.Ref, process.Ref)})
}

func satisfies(b fabrication.Bound, v float64) bool {
	const eps = 1e-9
	if b.Min != nil && v < *b.Min-eps {
		return false
	}
	if b.Max != nil && v > *b.Max+eps {
		return false
	}
	if want, ok := toFloat(b.Value); ok && (v < want-eps || v > want+eps) {
		return false
	}
	return true
}

func describe(b fabrication.Bound) string {
	var parts []string
	if b.Min != nil {
		parts = append(parts, fmt.Sprintf("min %g", *b.Min))
	}
	if b.Max != nil {
		parts = append(parts, fmt.Sprintf("max %g", *b.Max))
	}
	if b.Value != nil {
		parts = append(parts, fmt.Sprintf("value %v", b.Value))
	}
	s := strings.Join(parts, ", ")
	if b.Unit != "" {
		s += " " + b.Unit
	}
	return s
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case uint32:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}

func toFloats(v any) ([]float64, bool) {
	switch list := v.(type) {
	case []float64:
		return list, true
	case []any:
		out := make([]float64, 0, len(list))
		for _, x := range list {
			f, ok := toFloat(x)
			if !ok {
				return nil, false
			}
			out = append(out, f)
		}
		return out, true
	}
	if f, ok := toFloat(v); ok {
		return []float64{f}, true
	}
	return nil, false
}

func toStrings(v any) []string {
	switch list := v.(type) {
	case string:
		if list == "" {
			return nil
		}
		return []string{list}
	case []string:
		return list
	case []any:
		var out []string
		for _, x := range list {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
