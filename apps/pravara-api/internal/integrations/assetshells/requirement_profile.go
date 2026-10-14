package assetshells

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/fabrication"
)

// RequirementProfileTemplate is the MADFAM submodel template semanticId of
// the RequirementProfile submodel (SEM-1 §5).
const RequirementProfileTemplate = "https://id.madfam.io/smt/requirement-profile/1/0"

// ParseRequirementProfile reads the RequirementProfile submodel the keystone
// projection (hyperobjects_aas.solid.requirements_elements) writes:
//
//	Process            SubmodelElementList of xs:string Properties
//	Materials          SubmodelElementCollection {AnyOf, NoneOf lists}
//	ProcessParameters  SubmodelElementCollection of {ParameterKey, Min, Max, Value, Unit}
//	Rationale          MultiLanguageProperty
//	Parts              SubmodelElementCollection of {PartId, Process, Materials}
//
// DeclaredHints are informational and ignored. A submodel without these
// elements yields an empty profile (the product declared no requirements).
func ParseRequirementProfile(sm map[string]any) (*fabrication.RequirementProfile, error) {
	if sm == nil {
		return nil, fmt.Errorf("requirement profile: empty submodel")
	}
	if id, _ := sm["idShort"].(string); id != "RequirementProfile" {
		return nil, fmt.Errorf("requirement profile: submodel idShort is %q", id)
	}
	if !hasSemanticID(sm, RequirementProfileTemplate) {
		return nil, fmt.Errorf("requirement profile: semanticId is not %s", RequirementProfileTemplate)
	}
	rp := &fabrication.RequirementProfile{}
	for _, el := range elements(sm["submodelElements"]) {
		switch idShort(el) {
		case "Process":
			rp.Process = stringList(el)
		case "Materials":
			rp.Materials = materials(el)
		case "ProcessParameters":
			params, err := processParameters(el)
			if err != nil {
				return nil, err
			}
			rp.ProcessParameters = params
		case "Rationale":
			rp.Rationale = langStrings(el)
		case "Parts":
			for _, p := range elements(el["value"]) {
				var pid string
				set := fabrication.RequirementSet{}
				for _, child := range elements(p["value"]) {
					switch idShort(child) {
					case "PartId":
						pid = propString(child)
					case "Process":
						set.Process = stringList(child)
					case "Materials":
						set.Materials = materials(child)
					}
				}
				if pid == "" {
					return nil, fmt.Errorf("requirement profile: a Parts entry has no PartId")
				}
				if rp.Parts == nil {
					rp.Parts = map[string]fabrication.RequirementSet{}
				}
				rp.Parts[pid] = set
			}
		}
	}
	return rp, nil
}

func hasSemanticID(el map[string]any, want string) bool {
	refs := []any{el["semanticId"]}
	if sup, ok := el["supplementalSemanticIds"].([]any); ok {
		refs = append(refs, sup...)
	}
	for _, r := range refs {
		ref, _ := r.(map[string]any)
		for _, k := range elements(ref["keys"]) {
			if v, _ := k["value"].(string); v == want {
				return true
			}
		}
	}
	return false
}

func elements(v any) []map[string]any {
	list, _ := v.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func idShort(el map[string]any) string {
	s, _ := el["idShort"].(string)
	return s
}

func propString(el map[string]any) string {
	s, _ := el["value"].(string)
	return s
}

func stringList(el map[string]any) []string {
	var out []string
	for _, item := range elements(el["value"]) {
		if v := strings.TrimSpace(propString(item)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func materials(el map[string]any) *fabrication.Materials {
	m := &fabrication.Materials{}
	for _, child := range elements(el["value"]) {
		switch idShort(child) {
		case "AnyOf":
			m.AnyOf = stringList(child)
		case "NoneOf":
			m.NoneOf = stringList(child)
		}
	}
	return m
}

func langStrings(el map[string]any) map[string]string {
	out := map[string]string{}
	for _, t := range elements(el["value"]) {
		lang, _ := t["language"].(string)
		text, _ := t["text"].(string)
		if lang != "" {
			out[lang] = text
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func processParameters(el map[string]any) (map[string]fabrication.Bound, error) {
	out := map[string]fabrication.Bound{}
	for _, entry := range elements(el["value"]) {
		var key string
		b := fabrication.Bound{}
		for _, child := range elements(entry["value"]) {
			switch idShort(child) {
			case "ParameterKey":
				key = propString(child)
			case "Min":
				f, err := propNumber(child)
				if err != nil {
					return nil, fmt.Errorf("requirement profile: %s.Min: %w", idShort(entry), err)
				}
				b.Min = &f
			case "Max":
				f, err := propNumber(child)
				if err != nil {
					return nil, fmt.Errorf("requirement profile: %s.Max: %w", idShort(entry), err)
				}
				b.Max = &f
			case "Value":
				v, err := propTyped(child)
				if err != nil {
					return nil, fmt.Errorf("requirement profile: %s.Value: %w", idShort(entry), err)
				}
				b.Value = v
			case "Unit":
				b.Unit = propString(child)
			}
		}
		if key == "" {
			return nil, fmt.Errorf("requirement profile: parameter %q has no ParameterKey", idShort(entry))
		}
		if b.Min == nil && b.Max == nil && b.Value == nil {
			return nil, fmt.Errorf("requirement profile: parameter %q has no bound", key)
		}
		out[key] = b
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func propNumber(el map[string]any) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(propString(el)), 64)
	if err != nil {
		return 0, fmt.Errorf("not a number")
	}
	return f, nil
}

// propTyped converts an xsd-typed Property value to a JSON value.
func propTyped(el map[string]any) (any, error) {
	raw := propString(el)
	vt, _ := el["valueType"].(string)
	switch vt {
	case "xs:boolean":
		return strconv.ParseBool(raw)
	case "xs:double", "xs:float", "xs:decimal":
		return strconv.ParseFloat(raw, 64)
	case "xs:integer", "xs:int", "xs:long", "xs:short", "xs:nonNegativeInteger", "xs:positiveInteger":
		n, err := strconv.ParseInt(raw, 10, 64)
		return n, err
	default:
		return raw, nil
	}
}
