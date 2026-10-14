package dispatch

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/fabrication"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/assetshells"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/remote"
)

// ProductSpec is what dispatch fabricates for a task.
//
// Sources, in priority order: the order item's specifications, then the
// product definition (metadata written by the yantra4d import, and
// parametric_specs values):
//
//	cartridge        product metadata.slug
//	commons          product metadata.commons (default solid-hyperobjects)
//	mode             specifications.mode / metadata.mode
//	part             specifications.part / metadata.part (optional)
//	parameters       parametric_specs.<id>.value, overlaid by specifications.parameters
//	type_shell_id    specifications.type_shell_id / metadata.type_shell_id; else the one
//	                 type shell asset-shells holds for (commons, slug)
//	bounding_box_mm  specifications.bounding_box_mm / metadata.bounding_box_mm {x,y,z}
//	process_overrides specifications.process_overrides (validated by fabrication-prep)
type ProductSpec struct {
	Cartridge   string                   `json:"cartridge"`
	Commons     string                   `json:"commons"`
	Mode        string                   `json:"mode"`
	Part        string                   `json:"part,omitempty"`
	Parameters  map[string]any           `json:"parameters"`
	TypeShellID string                   `json:"type_shell_id"`
	TypeShellBy string                   `json:"type_shell_resolved_by"`
	BoundingBox *fabrication.BoundingBox `json:"bounding_box_mm,omitempty"`
	Overrides   map[string]any           `json:"process_overrides,omitempty"`
}

var cartridgePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func firstStr(key string, maps ...map[string]any) string {
	for _, m := range maps {
		if v := str(m, key); v != "" {
			return v
		}
	}
	return ""
}

func (s *Service) resolveProduct(ctx context.Context, tc *repositories.TaskContext) (*ProductSpec, *stepError) {
	if tc.ProductID == nil {
		return nil, terminal("product_not_found",
			"order item SKU %q matches no active product definition", tc.ProductSKU)
	}
	spec := &ProductSpec{
		Cartridge:  str(tc.ProductMeta, "slug"),
		Commons:    firstStr("commons", tc.ProductMeta),
		Mode:       firstStr("mode", tc.ItemSpecs, tc.ProductMeta),
		Part:       firstStr("part", tc.ItemSpecs, tc.ProductMeta),
		Parameters: map[string]any{},
	}
	if spec.Commons == "" {
		spec.Commons = "solid-hyperobjects"
	}
	if !cartridgePattern.MatchString(spec.Cartridge) {
		return nil, terminal("product_without_cartridge", "product has no yantra4d cartridge slug (metadata.slug)")
	}
	if spec.Mode == "" {
		return nil, terminal("product_without_mode", "neither the order item nor the product names a render mode")
	}
	for id, v := range tc.ParametricSpec {
		if m, ok := v.(map[string]any); ok {
			if val, ok := m["value"]; ok {
				spec.Parameters[id] = val
			}
		}
	}
	if p, ok := tc.ItemSpecs["parameters"].(map[string]any); ok {
		for id, v := range p {
			spec.Parameters[id] = v
		}
	}
	if o, ok := tc.ItemSpecs["process_overrides"].(map[string]any); ok && len(o) > 0 {
		spec.Overrides = o
	}
	for _, src := range []struct {
		name string
		m    map[string]any
	}{{"order_item.specifications", tc.ItemSpecs}, {"product.metadata", tc.ProductMeta}} {
		if b := boxFrom(src.m["bounding_box_mm"], src.name); b.Valid() {
			spec.BoundingBox = b
			break
		}
	}
	pinned := firstStr("type_shell_id", tc.ItemSpecs, tc.ProductMeta)
	if pinned != "" {
		if !assetshells.IsTypeShellID(pinned) {
			return nil, terminal("invalid_type_shell_id", "%q is not a MADFAM type shell id", pinned)
		}
		spec.TypeShellID, spec.TypeShellBy = pinned, "pinned"
		return spec, nil
	}
	if s.Shells == nil {
		return nil, terminal("asset_shells_unavailable", "no asset-shells client to resolve the type shell")
	}
	shells, err := s.Shells.FindTypeShells(ctx, map[string]string{"commons": spec.Commons, "slug": spec.Cartridge})
	if err != nil {
		return nil, fromRemote("type_shell_lookup_failed", err)
	}
	switch len(shells) {
	case 0:
		return nil, terminal("type_shell_not_found", "asset-shells holds no type shell for %s/%s", spec.Commons, spec.Cartridge)
	case 1:
		spec.TypeShellID, spec.TypeShellBy = shells[0].ID, "asset-shells lookup (commons, slug)"
	default:
		ids := make([]string, 0, len(shells))
		for _, sh := range shells {
			ids = append(ids, sh.ID)
		}
		sort.Strings(ids)
		return nil, terminal("type_shell_ambiguous",
			"%d design revisions exist for %s; pin metadata.type_shell_id to one of %v", len(shells), spec.Cartridge, ids)
	}
	return spec, nil
}

func boxFrom(v any, source string) *fabrication.BoundingBox {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	f := func(k string) float64 {
		switch n := m[k].(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
		return 0
	}
	return &fabrication.BoundingBox{X: f("x"), Y: f("y"), Z: f("z"), Source: source}
}

// requirementProfile reads the RequirementProfile submodel of a type shell.
// A shell without that submodel yields an empty profile (reported as a gap).
func (s *Service) requirementProfile(ctx context.Context, shellID string) (*fabrication.RequirementProfile, *stepError) {
	if s.Shells == nil {
		return nil, terminal("asset_shells_unavailable", "no asset-shells client")
	}
	shell, err := s.Shells.GetShell(ctx, shellID)
	if err != nil {
		return nil, fromRemote("type_shell_read_failed", err)
	}
	if shell.AssetInformation.AssetKind != "Type" {
		return nil, terminal("not_a_type_shell", "%s is not a type shell", shellID)
	}
	smID := shell.SubmodelID("RequirementProfile")
	if smID == "" {
		return &fabrication.RequirementProfile{}, nil
	}
	sm, err := s.Shells.GetSubmodel(ctx, shellID, smID)
	if err != nil {
		return nil, fromRemote("requirement_profile_read_failed", err)
	}
	profile, err := assetshells.ParseRequirementProfile(sm)
	if err != nil {
		return nil, terminal("requirement_profile_invalid", "%v", err)
	}
	return profile, nil
}

// fromRemote classifies a client error as a step error.
func fromRemote(code string, err error) *stepError {
	var re *remote.Error
	msg := err.Error()
	if errors.As(err, &re) && re.Code != "" {
		code = code + ":" + re.Code
	}
	if remote.IsRetryable(err) {
		return retryable(code, "%s", msg)
	}
	return terminal(code, "%s", msg)
}
