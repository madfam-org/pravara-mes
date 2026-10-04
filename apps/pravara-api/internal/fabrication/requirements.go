// Package fabrication holds the fabrication vocabulary types shared by
// matchmaking, the dispatcher and the asset-shells client: the product
// RequirementProfile (SEM-1 §2.4 / §5 RequirementProfile submodel) and the
// producer CapabilityProfile (SEM-1 §4 fabrication-capabilities keys).
//
// Keys are the lexicon's controlled vocabularies (processes,
// material-classes, process-parameters, fabrication-capabilities). pravara
// compares declared values; it never derives geometry.
package fabrication

import (
	"sort"
)

// Bound is one process-parameter bound: min, max and/or an exact value.
type Bound struct {
	Min   *float64 `json:"min,omitempty"`
	Max   *float64 `json:"max,omitempty"`
	Value any      `json:"value,omitempty"`
	Unit  string   `json:"unit,omitempty"`
}

// Materials is a material-classes constraint.
type Materials struct {
	AnyOf  []string `json:"any_of,omitempty"`
	NoneOf []string `json:"none_of,omitempty"`
}

// Empty reports whether the constraint allows every class.
func (m *Materials) Empty() bool { return m == nil || (len(m.AnyOf) == 0 && len(m.NoneOf) == 0) }

// Allows reports whether class satisfies the constraint.
func (m *Materials) Allows(class string) bool {
	if class == "" {
		return false
	}
	if m == nil {
		return true
	}
	for _, n := range m.NoneOf {
		if n == class {
			return false
		}
	}
	if len(m.AnyOf) == 0 {
		return true
	}
	for _, a := range m.AnyOf {
		if a == class {
			return true
		}
	}
	return false
}

// RequirementSet is the constraint block shared by the profile and its parts.
type RequirementSet struct {
	Process           []string         `json:"process,omitempty"`
	Materials         *Materials       `json:"materials,omitempty"`
	ProcessParameters map[string]Bound `json:"process_parameters,omitempty"`
}

// RequirementProfile is a product's requirements, in the exact shape
// fabrication-prep accepts as `requirements` (extra members are refused
// there, so this type carries only these).
type RequirementProfile struct {
	RequirementSet
	Rationale map[string]string         `json:"rationale,omitempty"`
	Parts     map[string]RequirementSet `json:"parts,omitempty"`
}

// ForPart returns the effective requirements of one part: a part's process
// or materials replace the product-level ones; process parameters stay
// product-level (SEM-1 §2.4 declares parts with process and materials only).
func (r *RequirementProfile) ForPart(part string) RequirementSet {
	if r == nil {
		return RequirementSet{}
	}
	eff := r.RequirementSet
	if p, ok := r.Parts[part]; ok && part != "" {
		if len(p.Process) > 0 {
			eff.Process = p.Process
		}
		if p.Materials != nil {
			eff.Materials = p.Materials
		}
	}
	return eff
}

// Empty reports whether the profile constrains nothing.
func (r *RequirementProfile) Empty() bool {
	return r == nil || (len(r.Process) == 0 && r.Materials.Empty() && len(r.ProcessParameters) == 0 && len(r.Parts) == 0)
}

// ParameterKeys lists the constrained process-parameters keys, sorted.
func (s RequirementSet) ParameterKeys() []string {
	keys := make([]string, 0, len(s.ProcessParameters))
	for k := range s.ProcessParameters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// BoundingBox is a part's axis-aligned extent in millimetres, as provided by
// yantra4d or a quote. pravara compares it to build volumes; it never
// computes one.
type BoundingBox struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
	// Source names where the box came from (e.g. "order_item.specifications").
	Source string `json:"source,omitempty"`
}

// Valid reports whether every extent is positive (a zero box, as yantra4d's
// geometry fallback returns, counts as missing).
func (b *BoundingBox) Valid() bool {
	return b != nil && b.X > 0 && b.Y > 0 && b.Z > 0
}
