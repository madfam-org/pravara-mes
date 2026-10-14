package assetshells

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/fabrication"
)

// Identifier scheme (SEM-1 §1) and the MADFAM submodel templates pravara
// publishes. The DigitalProductPassport submodel does not claim the IDTA
// 02099-1 template: it does not carry every element that template marks
// mandatory, so only the MADFAM template id is used (SEM-1 §1, no unproven
// conformance claims).
const (
	idBase                      = "https://id.madfam.io/"
	ManufacturingRecordTemplate = idBase + "smt/manufacturing-record/1/0"
	DigitalProductPassportTmpl  = idBase + "smt/digital-product-passport/1/0"
	PassportEventsSubmodel      = "DigitalProductPassport"
)

var typeShellPattern = regexp.MustCompile(`^https://id\.madfam\.io/aas/(solid|soft)/[a-z0-9][a-z0-9_-]*/[0-9a-f]{16}$`)

// IsTypeShellID reports whether id is a MADFAM type shell id (SEM-1 §1).
func IsTypeShellID(id string) bool { return typeShellPattern.MatchString(id) }

// TypeShellTree16 returns the tree16 segment of a type shell id.
func TypeShellTree16(id string) string {
	if !IsTypeShellID(id) {
		return ""
	}
	return id[strings.LastIndex(id, "/")+1:]
}

// InstanceShellID and friends build the instance identifiers.
func InstanceShellID(uuid string) string { return idBase + "aas/instance/" + uuid }
func InstanceAssetID(uuid string) string { return idBase + "asset/instance/" + uuid }
func instanceSubmodelID(uuid, idShort string) string {
	return idBase + "sm/instance/" + uuid + "/" + idShort
}

func extRef(v string) map[string]any {
	return map[string]any{"type": "ExternalReference", "keys": []any{map[string]any{"type": "GlobalReference", "value": v}}}
}

func modelRef(keyType, v string) map[string]any {
	return map[string]any{"type": "ModelReference", "keys": []any{map[string]any{"type": keyType, "value": v}}}
}

func prop(idShort, value, valueType string) map[string]any {
	if valueType == "" {
		valueType = "xs:string"
	}
	p := map[string]any{"modelType": "Property", "valueType": valueType, "value": value}
	if idShort != "" {
		p["idShort"] = idShort
	}
	return p
}

func smc(idShort string, children ...map[string]any) map[string]any {
	list := make([]any, 0, len(children))
	for _, c := range children {
		if c != nil {
			list = append(list, c)
		}
	}
	return map[string]any{"modelType": "SubmodelElementCollection", "idShort": idShort, "value": list}
}

func optProp(idShort, value string) map[string]any {
	if value == "" {
		return nil
	}
	return prop(idShort, value, "")
}

// DateTime formats an xs:dateTime in UTC.
func DateTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func optTime(idShort string, t *time.Time) map[string]any {
	if t == nil {
		return nil
	}
	return prop(idShort, DateTime(*t), "xs:dateTime")
}

func profileElement(idShort string, p fabrication.ProfileDigest) map[string]any {
	if p.ID == "" {
		return nil
	}
	return smc(idShort, prop("ProfileId", p.ID, ""), prop("Version", strconv.Itoa(p.Version), "xs:int"), prop("Sha256", p.SHA256, ""))
}

// instanceIDShort makes an AAS idShort from the cartridge slug.
func instanceIDShort(cartridge string) string {
	var b strings.Builder
	b.WriteString("part_")
	for _, r := range cartridge {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// BuildInstanceEnvironment renders the AAS v3.1 environment pravara
// publishes for a completed job: one instance shell derivedFrom the type
// shell, with the ManufacturingRecord and DigitalProductPassport submodels.
// The passport carries an empty Events list that receives later facts.
func BuildInstanceEnvironment(instanceUUID string, rec *fabrication.ManufacturingRecord, recordSHA256 string) ([]byte, error) {
	if !IsTypeShellID(rec.TypeShellID) {
		return nil, fmt.Errorf("asset-shells: %q is not a type shell id", rec.TypeShellID)
	}
	shellID := InstanceShellID(instanceUUID)
	mrID := instanceSubmodelID(instanceUUID, "ManufacturingRecord")
	dppID := instanceSubmodelID(instanceUUID, PassportEventsSubmodel)

	profiles := smc("SlicerProfiles",
		profileElement("PrinterProfile", rec.SlicerProfiles["printer"]),
		profileElement("FilamentProfile", rec.SlicerProfiles["filament"]),
		profileElement("ProcessProfile", rec.SlicerProfiles["process"]),
	)
	var gaps map[string]any
	if len(rec.Gaps) > 0 {
		items := make([]any, 0, len(rec.Gaps))
		for _, g := range rec.Gaps {
			items = append(items, prop("", g, ""))
		}
		gaps = map[string]any{"modelType": "SubmodelElementList", "idShort": "Gaps",
			"typeValueListElement": "Property", "valueTypeListElement": "xs:string", "value": items}
	}
	mrElements := []map[string]any{
		prop("RecordFormat", rec.Format+"/"+rec.FormatVersion, ""),
		prop("RecordSha256", recordSHA256, ""),
		prop("Goc1InstanceId", rec.GOC1InstanceID, ""),
		prop("VariablesSha256", rec.VariablesSHA256, ""),
		prop("TreeSha256", rec.TreeSHA256, ""),
		prop("GeometrySha256", rec.GeometrySHA256, ""),
		prop("GeometryMediaType", rec.GeometryMedia, ""),
		optProp("Part", rec.Part),
		profiles,
		optProp("Slicer", rec.Slicer),
		optProp("EffectiveSettingsSha256", rec.EffectiveSHA256),
		prop("SlicerVariablesSha256", rec.SlicerVariablesSHA256, ""),
		prop("ArtifactSha256", rec.ArtifactSHA256, ""),
		prop("ArtifactMediaType", rec.ArtifactMediaType, ""),
		optProp("GcodeSha256", rec.GcodeSHA256),
		prop("MachineId", rec.MachineID, ""),
		prop("MachineCode", rec.MachineCode, ""),
		prop("MaterialClass", rec.MaterialClass, ""),
		optProp("MaterialLot", rec.MaterialLot),
		prop("CommandId", rec.CommandID, ""),
		optTime("PrinterReportedAt", rec.PrinterReportedAt),
		optTime("BrokerReceivedAt", rec.BrokerReceivedAt),
		prop("ServerRecordedAt", DateTime(rec.ServerRecordedAt), "xs:dateTime"),
		gaps,
	}
	record := map[string]any{
		"modelType": "Submodel", "id": mrID, "idShort": "ManufacturingRecord", "kind": "Instance",
		"semanticId": extRef(ManufacturingRecordTemplate), "submodelElements": compact(mrElements),
	}
	passport := map[string]any{
		"modelType": "Submodel", "id": dppID, "idShort": PassportEventsSubmodel, "kind": "Instance",
		"semanticId": extRef(DigitalProductPassportTmpl),
		"submodelElements": compact([]map[string]any{
			prop("ProductInstance", InstanceAssetID(instanceUUID), ""),
			{"modelType": "ReferenceElement", "idShort": "TypeShell", "value": modelRef("AssetAdministrationShell", rec.TypeShellID)},
			{"modelType": "ReferenceElement", "idShort": "ManufacturingRecord", "value": modelRef("Submodel", mrID)},
			prop("MaterialClass", rec.MaterialClass, ""),
			prop("ManufacturedAt", DateTime(rec.ServerRecordedAt), "xs:dateTime"),
			{"modelType": "SubmodelElementList", "idShort": "Events",
				"typeValueListElement": "SubmodelElementCollection", "orderRelevant": true},
		}),
	}
	shell := map[string]any{
		"modelType": "AssetAdministrationShell",
		"id":        shellID,
		"idShort":   instanceIDShort(rec.Cartridge),
		"assetInformation": map[string]any{
			"assetKind":     "Instance",
			"globalAssetId": InstanceAssetID(instanceUUID),
			"specificAssetIds": []any{
				map[string]any{"name": "goc1_instance_id", "value": rec.GOC1InstanceID},
				map[string]any{"name": "manufacturing_record_sha256", "value": recordSHA256},
			},
		},
		"derivedFrom": modelRef("AssetAdministrationShell", rec.TypeShellID),
		"submodels":   []any{modelRef("Submodel", mrID), modelRef("Submodel", dppID)},
	}
	return json.Marshal(map[string]any{
		"assetAdministrationShells": []any{shell},
		"submodels":                 []any{record, passport},
	})
}

func compact(in []map[string]any) []any {
	out := make([]any, 0, len(in))
	for _, el := range in {
		if el != nil {
			out = append(out, el)
		}
	}
	return out
}

// PassportEvent is one append-only fact about an instance.
type PassportEvent struct {
	EventID    string
	Type       string
	OccurredAt time.Time
	// Facts are extra xs:string properties (idShort → value), sorted on output.
	Facts map[string]string
}

// BuildPassportEventBody renders the POST /madfam/v1/instances/{uuid}/passport-events body.
func BuildPassportEventBody(ev PassportEvent) ([]byte, error) {
	if ev.EventID == "" || ev.Type == "" {
		return nil, fmt.Errorf("asset-shells: a passport event needs an id and a type")
	}
	children := []map[string]any{prop("EventType", ev.Type, ""), prop("OccurredAt", DateTime(ev.OccurredAt), "xs:dateTime")}
	keys := make([]string, 0, len(ev.Facts))
	for k := range ev.Facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		children = append(children, prop(k, ev.Facts[k], ""))
	}
	event := map[string]any{"modelType": "SubmodelElementCollection", "value": compact(children)}
	return json.Marshal(map[string]any{
		"eventId": ev.EventID, "submodelIdShort": PassportEventsSubmodel, "event": event,
	})
}
