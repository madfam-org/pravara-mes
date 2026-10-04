// Package dispatchfakes provides httptest stand-ins for the services
// fabrication dispatch calls, following their real API shapes:
//
//   - Janua      POST /api/v1/oauth/token (client_credentials, HTTP Basic)
//   - yantra4d   POST /api/render, GET /static/<artifact>.variables.json
//   - fabrication-prep  POST/GET /v1/slice-jobs, GET /v1/profiles,
//     GET /v1/artifacts/{sha256}?exp&kid&sig
//   - asset-shells  GET /api/v3.1/shells[...], POST /madfam/v1/instances,
//     POST /madfam/v1/instances/{uuid}/passport-events
//
// Every fake checks the bearer token's audience-bound client, so a test fails
// when pravara calls a service with the wrong machine client. Used by tests
// only; it moves nothing and reaches no network beyond loopback.
package dispatchfakes

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Client is one registered Janua confidential client.
type Client struct {
	ID, Secret, Scope, Audience string
}

// Janua mints opaque tokens "<audience>|<client id>|<n>".
type Janua struct {
	Server  *httptest.Server
	mu      sync.Mutex
	clients map[string]Client
	Issued  map[string]int // client id → tokens issued
}

// NewJanua starts the token endpoint.
func NewJanua(t testing.TB, clients ...Client) *Janua {
	j := &Janua{clients: map[string]Client{}, Issued: map[string]int{}}
	for _, c := range clients {
		j.clients[c.ID] = c
	}
	j.Server = httptest.NewServer(http.HandlerFunc(j.token))
	t.Cleanup(j.Server.Close)
	return j
}

// TokenURL is the endpoint URL.
func (j *Janua) TokenURL() string { return j.Server.URL + "/api/v1/oauth/token" }

func (j *Janua) token(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	_ = r.ParseForm()
	j.mu.Lock()
	defer j.mu.Unlock()
	c, known := j.clients[id]
	switch {
	case r.URL.Path != "/api/v1/oauth/token" || r.Method != http.MethodPost:
		http.NotFound(w, r)
	case !ok || !known || c.Secret != secret:
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"invalid_client: Unknown client"}`))
	case r.PostForm.Get("grant_type") != "client_credentials" || r.PostForm.Get("scope") != c.Scope:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"invalid_scope"}`))
	default:
		j.Issued[id]++
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": fmt.Sprintf("%s|%s|%d", c.Audience, id, j.Issued[id]),
			"token_type": "Bearer", "expires_in": 3600, "scope": c.Scope})
	}
}

// Audience returns the audience a bearer token was minted for ("" if none).
func Audience(r *http.Request) string {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return strings.SplitN(tok, "|", 2)[0]
}

// Digest is a hex sha256.
func Digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// EncodeID is the Part 2 identifier encoding.
func EncodeID(id string) string { return base64.RawURLEncoding.EncodeToString([]byte(id)) }

// ---------------------------------------------------------------- yantra4d

// Yantra4D renders one part of a cartridge deterministically.
type Yantra4D struct {
	Server     *httptest.Server
	TreeSHA256 string // GOC-1 tree digest the render reports
	Part       string
	Format     string
	Geometry   []byte
	Sidecar    []byte
	mu         sync.Mutex
	Requests   []map[string]any
}

// NewYantra4D starts the fake. treeSHA must start with the type shell's tree16.
func NewYantra4D(t testing.TB, cartridge, mode, part, treeSHA string) *Yantra4D {
	y := &Yantra4D{TreeSHA256: treeSHA, Part: part, Format: "3mf", Geometry: []byte("PK\x03\x04 fake 3mf bytes for " + cartridge)}
	geo := Digest(y.Geometry)
	instance := Digest([]byte(cartridge + mode + part))
	doc := map[string]any{
		"format": "hyperobjects.generator-output", "format_version": "1.0.0", "kind": "solid",
		"generator": map[string]any{"platform": "yantra4d", "cartridge": cartridge, "mode": mode, "part": part,
			"engine": "cadquery", "source": map[string]any{"tree_sha256": treeSHA}},
		"variables":        []any{map[string]any{"id": "size", "value": 40, "type": "number", "source": "request"}},
		"variables_sha256": Digest([]byte("vars")), "complete": true, "instance_id": instance,
		"geometry": []any{map[string]any{"path": "x.3mf", "media_type": "model/3mf", "sha256": geo,
			"bytes": len(y.Geometry), "units": "mm", "role": "primary"}},
	}
	y.Sidecar, _ = json.Marshal(doc)
	y.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Audience(r) != "yantra4d-api" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing or wrong token"})
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/render":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			y.mu.Lock()
			y.Requests = append(y.Requests, body)
			y.mu.Unlock()
			if _, nested := body["parameters"].(map[string]any); !nested || body["export_format"] != y.Format {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "parameters must be nested; export_format " + y.Format})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"status": "success", "request_id": "r1",
				"parts": []any{map[string]any{"type": part, "url": "/static/" + cartridge + "_" + part + ".3mf",
					"size_bytes": len(y.Geometry), "sha256": geo, "media_type": "model/3mf", "instance_id": instance,
					"variables_url": "/static/" + cartridge + "_" + part + ".3mf.variables.json"}},
				"generator_output": map[string]any{"format_version": "1.0.0", "complete": true, "variables_sha256": Digest([]byte("vars"))}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, ".variables.json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(y.Sidecar)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(y.Server.Close)
	return y
}

// ---------------------------------------------------------- fabrication-prep

// SliceJob is the fake's stored job.
type SliceJob struct {
	ID       string
	Key      string
	Body     map[string]any
	Polls    int
	Status   string
	Output   []byte
	Vars     []byte
	Profiles map[string]any
}

// FabricationPrep queues a job, reports it running for RunningPolls reads,
// then succeeded with signed output and slicer-variables.
type FabricationPrep struct {
	Server       *httptest.Server
	RunningPolls int
	FailWith     *struct{ Code, Message string }
	mu           sync.Mutex
	Jobs         map[string]*SliceJob
	byKey        map[string]*SliceJob
	Created      int
	artifacts    map[string][]byte
}

// Catalog is the profile catalog (subset of fabrication-prep's real one).
var Catalog = []map[string]any{
	{"id": "klipper-corexy-350-0.4", "version": 1, "ref": "klipper-corexy-350-0.4@1", "kind": "printer", "sha256": strings.Repeat("7a", 32), "target": "klipper_gcode"},
	{"id": "tpu-95a-klipper", "version": 1, "ref": "tpu-95a-klipper@1", "kind": "filament", "sha256": strings.Repeat("23", 32), "material_class": "tpu-95a", "requires_process_tag": "tpu-safe", "printers": []string{"klipper-corexy-350-0.4"}},
	{"id": "petg-generic-klipper", "version": 1, "ref": "petg-generic-klipper@1", "kind": "filament", "sha256": strings.Repeat("6e", 32), "material_class": "petg", "printers": []string{"klipper-corexy-350-0.4"}},
	{"id": "standard-0.20-klipper", "version": 1, "ref": "standard-0.20-klipper@1", "kind": "process", "sha256": strings.Repeat("dd", 32), "printers": []string{"klipper-corexy-350-0.4"}, "tags": []string{"standard"}},
	{"id": "tpu-safe-0.20-klipper", "version": 1, "ref": "tpu-safe-0.20-klipper@1", "kind": "process", "sha256": strings.Repeat("bb", 32), "printers": []string{"klipper-corexy-350-0.4"}, "tags": []string{"tpu-safe"}},
}

// NewFabricationPrep starts the fake.
func NewFabricationPrep(t testing.TB) *FabricationPrep {
	f := &FabricationPrep{Jobs: map[string]*SliceJob{}, byKey: map[string]*SliceJob{}, artifacts: map[string][]byte{}, RunningPolls: 1}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Server.Close)
	return f
}

func (f *FabricationPrep) signed(sha, media string, n int) map[string]any {
	return map[string]any{"url": f.Server.URL + "/v1/artifacts/" + sha + "?exp=9999999999&kid=k1&sig=placeholder",
		"sha256": sha, "media_type": media, "bytes": n, "expires_at": "2026-10-04T00:15:00Z"}
}

func (f *FabricationPrep) view(j *SliceJob) map[string]any {
	v := map[string]any{"id": j.ID, "status": j.Status, "target": j.Body["target"], "attempts": 1, "max_attempts": 3,
		"input":    map[string]any{"sha256": j.Body["input"].(map[string]any)["sha256"], "media_type": "model/3mf"},
		"profiles": j.Profiles, "error": nil, "output": nil, "slicer_variables": nil}
	switch j.Status {
	case "succeeded":
		out := f.signed(Digest(j.Output), "text/x-gcode", len(j.Output))
		out["filename"] = "plate_1.gcode"
		v["output"] = out
		v["slicer_variables"] = f.signed(Digest(j.Vars), "application/json", len(j.Vars))
		v["estimates"] = map[string]any{"print_time_s": 975, "filament_g": 5.2, "source": "gcode"}
	case "failed":
		v["error"] = map[string]any{"code": f.FailWith.Code, "message": f.FailWith.Message}
	}
	return v
}

func (f *FabricationPrep) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasPrefix(r.URL.Path, "/v1/artifacts/") {
		sha := strings.TrimPrefix(r.URL.Path, "/v1/artifacts/")
		b, ok := f.artifacts[sha]
		if !ok || r.URL.Query().Get("sig") == "" {
			writeJSON(w, http.StatusForbidden, map[string]any{"errors": []any{map[string]any{"code": "signature_invalid", "message": "bad signature"}}})
			return
		}
		w.Header().Set("X-Content-SHA256", sha)
		_, _ = w.Write(b)
		return
	}
	if Audience(r) != "fabrication-prep-api" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"errors": []any{map[string]any{"code": "invalid_token", "message": "wrong audience"}}})
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/profiles":
		writeJSON(w, http.StatusOK, map[string]any{"orcaslicer": map[string]any{"version": "2.4.2"}, "profiles": Catalog})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/slice-jobs":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		key := r.Header.Get("Idempotency-Key")
		canon, _ := json.Marshal(body)
		if j, ok := f.byKey[key]; ok && key != "" {
			prev, _ := json.Marshal(j.Body)
			if string(prev) != string(canon) {
				writeJSON(w, http.StatusConflict, map[string]any{"errors": []any{map[string]any{"code": "idempotency_conflict", "message": "key reused"}}})
				return
			}
			writeJSON(w, http.StatusOK, f.view(j))
			return
		}
		if ov, ok := body["overrides"].(map[string]any); ok {
			if v, ok := ov["outer_wall_speed"].(float64); ok && v > 150 {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": []any{map[string]any{"code": "override_out_of_range",
					"message": "outer_wall_speed=300 (override) is outside the required range [min 20, max 150] mm/s", "path": "/overrides/outer_wall_speed"}}})
				return
			}
		}
		f.Created++
		j := &SliceJob{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", f.Created), Key: key, Body: body, Status: "queued",
			Profiles: map[string]any{}}
		for kind, field := range map[string]string{"printer": "printer_profile", "filament": "filament_profile", "process": "process_profile"} {
			ref, _ := body[field].(string)
			id, _, _ := strings.Cut(ref, "@")
			for _, p := range Catalog {
				if p["id"] == id && p["kind"] == kind {
					j.Profiles[kind] = map[string]any{"id": id, "version": 1, "sha256": p["sha256"]}
				}
			}
		}
		j.Output = []byte("; generated by OrcaSlicer 2.4.2 (fake)\nG28\n; job " + j.ID + "\n")
		vars := map[string]any{"format": "madfam.fabrication-prep.slicer-variables", "format_version": "1.0.0", "job_id": j.ID,
			"target": body["target"], "profiles": j.Profiles, "material_class": "tpu-95a", "effective_sha256": Digest([]byte("effective")),
			"requirements_sha256": Digest([]byte("req")), "slicer": map[string]any{"name": "OrcaSlicer", "version": "2.4.2"},
			"output": map[string]any{"sha256": Digest(j.Output), "media_type": "text/x-gcode", "bytes": len(j.Output),
				"filename": "plate_1.gcode", "gcode_sha256": Digest(j.Output)}, "warnings": []any{}}
		j.Vars, _ = json.Marshal(vars)
		f.artifacts[Digest(j.Output)], f.artifacts[Digest(j.Vars)] = j.Output, j.Vars
		f.Jobs[j.ID] = j
		if key != "" {
			f.byKey[key] = j
		}
		w.Header().Set("Location", "/v1/slice-jobs/"+j.ID)
		writeJSON(w, http.StatusAccepted, f.view(j))
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/slice-jobs/"):
		j, ok := f.Jobs[strings.TrimPrefix(r.URL.Path, "/v1/slice-jobs/")]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"errors": []any{map[string]any{"code": "not_found", "message": "no job"}}})
			return
		}
		j.Polls++
		switch {
		case j.Status == "succeeded" || j.Status == "failed":
		case f.FailWith != nil:
			j.Status = "failed"
		case j.Polls > f.RunningPolls:
			j.Status = "succeeded"
		default:
			j.Status = "running"
		}
		writeJSON(w, http.StatusOK, f.view(j))
	default:
		http.NotFound(w, r)
	}
}

// ------------------------------------------------------------ asset-shells

// AssetShells serves one type environment and records instance publishes.
type AssetShells struct {
	Server    *httptest.Server
	Type      map[string]any // the type environment
	FailNext  int            // answer 503 to this many publishes first
	mu        sync.Mutex
	Instances map[string][]byte   // shell id → published environment
	Events    map[string][]string // shell id → event ids
	Tenants   map[string]string   // shell id → token client id
}

// NewAssetShells starts the fake with one type environment.
func NewAssetShells(t testing.TB, typeEnv map[string]any) *AssetShells {
	a := &AssetShells{Type: typeEnv, Instances: map[string][]byte{}, Events: map[string][]string{}, Tenants: map[string]string{}}
	a.Server = httptest.NewServer(http.HandlerFunc(a.serve))
	t.Cleanup(a.Server.Close)
	return a
}

func (a *AssetShells) shell() map[string]any {
	return a.Type["assetAdministrationShells"].([]any)[0].(map[string]any)
}

func problem(w http.ResponseWriter, status int, code, text, path string) {
	writeJSON(w, status, map[string]any{"messages": []any{map[string]any{"code": code, "messageType": "Error", "text": text, "path": path}}})
}

func (a *AssetShells) serve(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	shell := a.shell()
	shellID := shell["id"].(string)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3.1/shells":
		raw, _ := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("assetIds"))
		var pairs []map[string]string
		_ = json.Unmarshal(raw, &pairs)
		have := map[string]string{}
		for _, s := range shell["assetInformation"].(map[string]any)["specificAssetIds"].([]any) {
			m := s.(map[string]any)
			have[m["name"].(string)] = m["value"].(string)
		}
		result := []any{shell}
		for _, p := range pairs {
			if have[p["name"]] != p["value"] {
				result = []any{}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"paging_metadata": map[string]any{}, "result": result})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3.1/shells/"+EncodeID(shellID):
		writeJSON(w, http.StatusOK, shell)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v3.1/shells/"+EncodeID(shellID)+"/submodels/"):
		raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(r.URL.Path, "/api/v3.1/shells/"+EncodeID(shellID)+"/submodels/"))
		for _, sm := range a.Type["submodels"].([]any) {
			if sm.(map[string]any)["id"] == string(raw) {
				writeJSON(w, http.StatusOK, sm)
				return
			}
		}
		problem(w, http.StatusNotFound, "not_found", "no submodel", "")
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v3.1/shells/"):
		problem(w, http.StatusNotFound, "not_found", "The requested resource does not exist", "")
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/madfam/v1/"):
		a.publish(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (a *AssetShells) publish(w http.ResponseWriter, r *http.Request) {
	if Audience(r) != "asset-shells-api" {
		problem(w, http.StatusUnauthorized, "invalid_token", "wrong audience", "")
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		problem(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "send Content-Type: application/json", "")
		return
	}
	if a.FailNext > 0 {
		a.FailNext--
		problem(w, http.StatusServiceUnavailable, "database_unavailable", "try later", "")
		return
	}
	client := strings.SplitN(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), "|", 3)[1]
	var body map[string]any
	raw := new(strings.Builder)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		problem(w, http.StatusBadRequest, "bad_json", "not JSON", "/")
		return
	}
	enc, _ := json.Marshal(body)
	raw.Write(enc)
	if r.URL.Path == "/madfam/v1/instances" {
		shells, _ := body["assetAdministrationShells"].([]any)
		if len(shells) != 1 {
			problem(w, http.StatusUnprocessableEntity, "environment", "exactly one shell", "/")
			return
		}
		sh := shells[0].(map[string]any)
		id, _ := sh["id"].(string)
		derived := sh["derivedFrom"].(map[string]any)["keys"].([]any)[0].(map[string]any)["value"]
		if !strings.HasPrefix(id, "https://id.madfam.io/aas/instance/") || derived != a.shell()["id"] {
			problem(w, http.StatusUnprocessableEntity, "unknown_derived_from", "type shell is not published", "/assetAdministrationShells/0/derivedFrom")
			return
		}
		if prev, ok := a.Instances[id]; ok {
			if string(prev) != raw.String() {
				problem(w, http.StatusConflict, "identifier_unavailable", "this instance id is already published with other content", "")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"id": id, "publishSha256": Digest(prev)})
			return
		}
		a.Instances[id], a.Tenants[id] = []byte(raw.String()), client
		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "publishSha256": Digest([]byte(raw.String()))})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/madfam/v1/instances/"), "/")
	shellID := "https://id.madfam.io/aas/instance/" + parts[0]
	if len(parts) != 2 || parts[1] != "passport-events" {
		http.NotFound(w, r)
		return
	}
	if _, ok := a.Instances[shellID]; !ok || a.Tenants[shellID] != client {
		problem(w, http.StatusNotFound, "not_found", "The instance shell does not exist or belongs to another tenant", "")
		return
	}
	if body["submodelIdShort"] != "DigitalProductPassport" {
		problem(w, http.StatusUnprocessableEntity, "no_events_list", "no Events list", "/submodelIdShort")
		return
	}
	eventID, _ := body["eventId"].(string)
	for _, e := range a.Events[shellID] {
		if e == eventID {
			writeJSON(w, http.StatusOK, map[string]any{"eventId": eventID})
			return
		}
	}
	a.Events[shellID] = append(a.Events[shellID], eventID)
	writeJSON(w, http.StatusCreated, map[string]any{"eventId": eventID, "position": len(a.Events[shellID]) - 1})
}
