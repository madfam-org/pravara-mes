package assetshells

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/fabrication"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
)

// Fixtures: the phase-3 keystone projections (hyperobjects_aas, SEM-1 §5)
// of motor-soft-mount, tslot-corner and a-line-skirt, plus a copy of
// motor-soft-mount whose RequirementProfile elements were produced by the
// keystone's own requirements_elements() for a requirements block.
func loadEnv(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	var env map[string]any
	require.NoError(t, json.Unmarshal(raw, &env))
	return env
}

func submodel(t *testing.T, env map[string]any, idShort string) map[string]any {
	t.Helper()
	for _, sm := range env["submodels"].([]any) {
		m := sm.(map[string]any)
		if m["idShort"] == idShort {
			return m
		}
	}
	return nil
}

func TestParseRequirementProfileFromKeystoneProjection(t *testing.T) {
	env := loadEnv(t, "motor-soft-mount.requirements.aas.json")
	rp, err := ParseRequirementProfile(submodel(t, env, "RequirementProfile"))
	require.NoError(t, err)

	assert.Equal(t, []string{"fff"}, rp.Process)
	assert.Equal(t, []string{"tpu-95a", "petg"}, rp.Materials.AnyOf)
	assert.Equal(t, []string{"pla"}, rp.Materials.NoneOf)
	require.Contains(t, rp.ProcessParameters, "wall_loops")
	assert.Equal(t, 3.0, *rp.ProcessParameters["wall_loops"].Min)
	assert.Equal(t, 0.4, rp.ProcessParameters["nozzle_diameter"].Value)
	assert.Equal(t, "mm", rp.ProcessParameters["nozzle_diameter"].Unit)
	assert.Equal(t, 220.0, *rp.ProcessParameters["nozzle_temperature"].Min)
	assert.Equal(t, 250.0, *rp.ProcessParameters["nozzle_temperature"].Max)
	assert.Equal(t, false, rp.ProcessParameters["enable_support"].Value)
	assert.Equal(t, "Vibration damping needs a flexible material", rp.Rationale["en"])
	assert.Equal(t, []string{"tpu-95a"}, rp.Parts["body"].Materials.AnyOf)

	// The part narrows materials; parameters stay product-level.
	body := rp.ForPart("body")
	assert.Equal(t, []string{"tpu-95a"}, body.Materials.AnyOf)
	assert.Equal(t, []string{"fff"}, body.Process)
	assert.Contains(t, body.ProcessParameters, "wall_loops")

	// It serialises to exactly the requirements shape fabrication-prep takes.
	out, err := json.Marshal(rp)
	require.NoError(t, err)
	var generic map[string]any
	require.NoError(t, json.Unmarshal(out, &generic))
	for k := range generic {
		assert.Contains(t, []string{"process", "materials", "process_parameters", "rationale", "parts"}, k)
	}
	assert.Equal(t, false, generic["process_parameters"].(map[string]any)["enable_support"].(map[string]any)["value"],
		"a false bound value must survive omitempty")
}

func TestParseRequirementProfileWithoutRequirementsIsEmpty(t *testing.T) {
	for _, name := range []string{"motor-soft-mount.aas.json", "tslot-corner.aas.json"} {
		env := loadEnv(t, name)
		rp, err := ParseRequirementProfile(submodel(t, env, "RequirementProfile"))
		require.NoError(t, err, name)
		assert.True(t, rp.Empty(), name)
	}
	// The soft sample has no RequirementProfile submodel at all.
	assert.Nil(t, submodel(t, loadEnv(t, "a-line-skirt.aas.json"), "RequirementProfile"))
}

func TestParseRequirementProfileRejectsOtherSubmodels(t *testing.T) {
	env := loadEnv(t, "tslot-corner.aas.json")
	_, err := ParseRequirementProfile(submodel(t, env, "Nameplate"))
	assert.Error(t, err)
	bad := map[string]any{"idShort": "RequirementProfile", "semanticId": map[string]any{"keys": []any{}}}
	_, err = ParseRequirementProfile(bad)
	assert.ErrorContains(t, err, "semanticId")
}

func TestShellHelpersOnSamples(t *testing.T) {
	env := loadEnv(t, "tslot-corner.aas.json")
	raw, _ := json.Marshal(env["assetAdministrationShells"].([]any)[0])
	var shell Shell
	require.NoError(t, json.Unmarshal(raw, &shell))
	assert.True(t, IsTypeShellID(shell.ID))
	assert.Equal(t, "ea6e816f0d426758", TypeShellTree16(shell.ID))
	assert.True(t, strings.HasPrefix(shell.SpecificAssetID("tree_sha256"), TypeShellTree16(shell.ID)))
	assert.Equal(t, "https://id.madfam.io/sm/solid/tslot-corner/ea6e816f0d426758/RequirementProfile", shell.SubmodelID("RequirementProfile"))
	assert.False(t, IsTypeShellID("https://id.madfam.io/aas/instance/0f8fad5b-d9cb-469f-a165-70867728950e"))
}

var idShortPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*[a-zA-Z0-9_]+$`)

func sampleRecord() *fabrication.ManufacturingRecord {
	at := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	return &fabrication.ManufacturingRecord{
		Format: fabrication.ManufacturingRecordFormat, FormatVersion: "1.0.0",
		DispatchID: "d", CommandID: "c", TaskID: "t",
		TypeShellID: "https://id.madfam.io/aas/solid/motor-soft-mount/9de820d73e02774a", Cartridge: "motor-soft-mount",
		Mode: "assembled", Part: "body", GOC1InstanceID: strings.Repeat("a", 64), VariablesSHA256: strings.Repeat("b", 64),
		TreeSHA256: "9de820d73e02774a" + strings.Repeat("0", 48), GeometrySHA256: strings.Repeat("c", 64), GeometryMedia: "model/3mf",
		SlicerProfiles:        map[string]fabrication.ProfileDigest{"printer": {ID: "klipper-corexy-350-0.4", Version: 1, SHA256: strings.Repeat("d", 64)}},
		SlicerVariablesSHA256: strings.Repeat("e", 64), ArtifactSHA256: strings.Repeat("f", 64), ArtifactMediaType: "text/x-gcode",
		MachineID: "m", MachineCode: "voron-01", MaterialClass: "tpu-95a", ServerRecordedAt: at, PrinterReportedAt: &at,
		Gaps: []string{"material_lot_not_recorded"},
	}
}

func walkElements(t *testing.T, elements []any, inList bool) {
	for _, e := range elements {
		el := e.(map[string]any)
		id, has := el["idShort"].(string)
		if inList {
			assert.False(t, has, "list items carry no idShort (AASd-120): %v", el)
		} else {
			assert.True(t, idShortPattern.MatchString(id), "idShort %q", id)
		}
		if el["modelType"] == "Property" {
			assert.NotEmpty(t, el["valueType"])
			_, isString := el["value"].(string)
			assert.True(t, isString, "property values are strings: %v", el)
		}
		if children, ok := el["value"].([]any); ok && (el["modelType"] == "SubmodelElementCollection" || el["modelType"] == "SubmodelElementList") {
			walkElements(t, children, el["modelType"] == "SubmodelElementList")
		}
	}
}

// The instance environment follows asset-shells' instance rules
// (scheme.instance_environment_problems): one shell, id scheme, Instance
// kind, globalAssetId, derivedFrom a type shell, submodel ids and idShorts,
// every submodel referenced, no concept descriptions.
func TestBuildInstanceEnvironmentFollowsInstanceRules(t *testing.T) {
	uuid := "0f8fad5b-d9cb-469f-a165-70867728950e"
	raw, err := BuildInstanceEnvironment(uuid, sampleRecord(), strings.Repeat("9", 64))
	require.NoError(t, err)
	var env map[string]any
	require.NoError(t, json.Unmarshal(raw, &env))
	assert.NotContains(t, env, "conceptDescriptions")
	shells := env["assetAdministrationShells"].([]any)
	require.Len(t, shells, 1)
	shell := shells[0].(map[string]any)
	assert.Equal(t, "https://id.madfam.io/aas/instance/"+uuid, shell["id"])
	assert.True(t, idShortPattern.MatchString(shell["idShort"].(string)))
	info := shell["assetInformation"].(map[string]any)
	assert.Equal(t, "Instance", info["assetKind"])
	assert.Equal(t, "https://id.madfam.io/asset/instance/"+uuid, info["globalAssetId"])
	derived := shell["derivedFrom"].(map[string]any)
	assert.Equal(t, "ModelReference", derived["type"])
	key := derived["keys"].([]any)[0].(map[string]any)
	assert.Equal(t, "AssetAdministrationShell", key["type"])
	assert.Equal(t, sampleRecord().TypeShellID, key["value"])

	refs := map[string]bool{}
	for _, r := range shell["submodels"].([]any) {
		refs[r.(map[string]any)["keys"].([]any)[0].(map[string]any)["value"].(string)] = true
	}
	sms := env["submodels"].([]any)
	require.Len(t, sms, 2)
	for _, s := range sms {
		sm := s.(map[string]any)
		id := sm["id"].(string)
		assert.True(t, refs[id], "submodel %s referenced", id)
		assert.Equal(t, "https://id.madfam.io/sm/instance/"+uuid+"/"+sm["idShort"].(string), id)
		walkElements(t, sm["submodelElements"].([]any), false)
		sem := sm["semanticId"].(map[string]any)["keys"].([]any)[0].(map[string]any)["value"].(string)
		assert.True(t, strings.HasPrefix(sem, "https://id.madfam.io/smt/"), "MADFAM template only, no IDTA claim: %s", sem)
		assert.NotContains(t, sm, "supplementalSemanticIds")
	}
	dpp := submodel(t, env, PassportEventsSubmodel)
	var events map[string]any
	for _, e := range dpp["submodelElements"].([]any) {
		if e.(map[string]any)["idShort"] == "Events" {
			events = e.(map[string]any)
		}
	}
	require.NotNil(t, events, "the passport carries the Events list that receives later facts")
	assert.Equal(t, "SubmodelElementList", events["modelType"])
	assert.Equal(t, "SubmodelElementCollection", events["typeValueListElement"])
	assert.Contains(t, string(raw), `"Gaps"`)
	assert.Contains(t, string(raw), `"PrinterReportedAt"`)
	assert.NotContains(t, string(raw), `"BrokerReceivedAt"`, "absent timestamps are omitted, and listed as gaps by the record")

	if dir := os.Getenv("PRAVARA_WRITE_PASSPORT_GOLDEN"); dir != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "instance-env.json"), raw, 0o600))
		ev, err := BuildPassportEventBody(PassportEvent{EventID: "6f2a7c1e-3b4d-4e5f-8a9b-0c1d2e3f4a5b", Type: "manufactured",
			OccurredAt: time.Unix(0, 0), Facts: map[string]string{"MachineCode": "voron-01"}})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "passport-event.json"), ev, 0o600))
	}
}

func TestBuildInstanceEnvironmentRefusesNonTypeShell(t *testing.T) {
	rec := sampleRecord()
	rec.TypeShellID = "https://example.test/aas/x"
	_, err := BuildInstanceEnvironment("0f8fad5b-d9cb-469f-a165-70867728950e", rec, "x")
	assert.Error(t, err)
}

func TestPassportEventBody(t *testing.T) {
	raw, err := BuildPassportEventBody(PassportEvent{EventID: "6f2a7c1e-3b4d-4e5f-8a9b-0c1d2e3f4a5b", Type: "inspected",
		OccurredAt: time.Date(2026, 10, 4, 1, 2, 3, 0, time.UTC), Facts: map[string]string{"Zeta": "1", "Alpha": "2"}})
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	assert.ElementsMatch(t, []string{"eventId", "submodelIdShort", "event"}, keys(body))
	assert.Equal(t, PassportEventsSubmodel, body["submodelIdShort"])
	ev := body["event"].(map[string]any)
	assert.NotContains(t, ev, "idShort")
	vals := ev["value"].([]any)
	assert.Equal(t, "EventType", vals[0].(map[string]any)["idShort"])
	assert.Equal(t, "2026-10-04T01:02:03.000Z", vals[1].(map[string]any)["value"])
	assert.Equal(t, "Alpha", vals[2].(map[string]any)["idShort"])
	_, err = BuildPassportEventBody(PassportEvent{})
	assert.Error(t, err)
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestClientReadAndPublish(t *testing.T) {
	env := loadEnv(t, "motor-soft-mount.requirements.aas.json")
	shell := env["assetAdministrationShells"].([]any)[0].(map[string]any)
	shellID := shell["id"].(string)
	rpID := submodel(t, env, "RequirementProfile")["id"].(string)
	var published []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3.1/shells":
			assert.NotEmpty(t, r.URL.Query().Get("assetIds"))
			_ = json.NewEncoder(w).Encode(map[string]any{"result": []any{shell}, "paging_metadata": map[string]any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3.1/shells/"+EncodeID(shellID):
			_ = json.NewEncoder(w).Encode(shell)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3.1/shells/"+EncodeID(shellID)+"/submodels/"+EncodeID(rpID):
			_ = json.NewEncoder(w).Encode(submodel(t, env, "RequirementProfile"))
		case r.Method == http.MethodPost && r.URL.Path == "/madfam/v1/instances":
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			published = []byte("x")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"messages":[{"code":"identifier_unavailable","text":"taken","path":"/"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	janua := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","expires_in":3600}`))
	}))
	defer janua.Close()

	c, err := NewClient(srv.URL, nil)
	require.NoError(t, err)
	ctx := t.Context()
	found, err := c.FindTypeShells(ctx, map[string]string{"commons": "solid-hyperobjects", "slug": "motor-soft-mount"})
	require.NoError(t, err)
	require.Len(t, found, 1)
	got, err := c.GetShell(ctx, shellID)
	require.NoError(t, err)
	sm, err := c.GetSubmodel(ctx, shellID, got.SubmodelID("RequirementProfile"))
	require.NoError(t, err)
	rp, err := ParseRequirementProfile(sm)
	require.NoError(t, err)
	assert.Equal(t, []string{"fff"}, rp.Process)

	_, err = c.GetShell(ctx, "https://id.madfam.io/aas/solid/missing/0000000000000000")
	assert.ErrorContains(t, err, "not_found")

	ts := machineclients.NewTokenSource(machineclients.Credentials{Name: "p", TokenURL: janua.URL, ClientID: "id", ClientSecret: "s"}, nil)
	_, err = c.PublishInstance(ctx, ts, []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "409")
	assert.Contains(t, err.Error(), "identifier_unavailable")
	assert.NotNil(t, published)
}
