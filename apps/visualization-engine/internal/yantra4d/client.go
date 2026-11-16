// Package yantra4d is the visualization engine's client for Yantra4D's public
// render API (https://api.yantra4d.com).
//
// It speaks Yantra4D's current render contract (checked against Yantra4D's
// routes/engine/render.py and services/engine/render_orchestrator.py):
//
//	POST /api/render {project, mode, parameters, export_format, ignore_cache?}
//	→ 200 {status, parts[{type, url, size_bytes, sha256, media_type,
//	       instance_id, variables_url}], log, request_id,
//	       generator_output{format_version, complete, variables_sha256}}
//
// The geometry is not in the answer: each part's url points at a file the API
// serves under /static/, next to its GOC-1 sidecar (variables_url).
//
// The client fails closed. Yantra4D answers 200 "success" even when a part
// failed (the part is simply left out and the reason is only in the log), it
// silently renders STL for an export format it does not know, and it serves a
// file under the same name again after a later render. So a fetch is only a
// success when the requested part came back, the log does not report it as
// failed, its media type is the requested format, every URL in the answer
// points back at the configured https host, and the downloaded bytes match the
// digest in the answer and in the part's GOC-1 sidecar.
package yantra4d

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// DefaultBaseURL is Yantra4D's public API.
const DefaultBaseURL = "https://api.yantra4d.com"

// GOC1Format is the generator-output document format (GOC-1 §2).
const GOC1Format = "hyperobjects.generator-output"

// Download limits. A rendered part is a few MB; the cap only stops a runaway
// answer from filling memory.
const (
	maxArtifactBytes = 256 << 20
	maxJSONBytes     = 8 << 20
)

var (
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// slugPattern is Yantra4D's cartridge id (GOC-1 _CARTRIDGE_RE).
	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	// failedPartLine is the line Yantra4D's render orchestrator writes to the
	// log for a part that failed or timed out: "[<part>] ERROR: <reason>".
	failedPartLine = regexp.MustCompile(`(?m)^\[([^\]\n]+)\] ERROR:`)
)

// mediaTypes maps the export formats Yantra4D renders to the media type its
// GOC-1 fields report (generator_output.py _MEDIA_TYPES).
var mediaTypes = map[string]string{
	"stl":  "model/stl",
	"3mf":  "model/3mf",
	"glb":  "model/gltf-binary",
	"gltf": "model/gltf+json",
	"step": "model/step",
	"obj":  "model/obj",
}

// Error is a failed or refused Yantra4D call.
type Error struct {
	Op string
	// Status is the HTTP status when Yantra4D answered, 0 otherwise.
	Status int
	// Code is Yantra4D's error_code for an error answer, or one of this
	// package's codes for a refused answer (part_missing, part_failed,
	// url_rejected, digest_mismatch, sidecar_mismatch, goc1_missing,
	// media_type_mismatch, render_failed, invalid_request, invalid_answer).
	Code    string
	Message string
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("yantra4d ")
	b.WriteString(e.Op)
	if e.Status != 0 {
		fmt.Fprintf(&b, " returned %d", e.Status)
	}
	if e.Code != "" {
		fmt.Fprintf(&b, " [%s]", e.Code)
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	return b.String()
}

// Client calls Yantra4D's API.
type Client struct {
	baseURL *url.URL
	http    *http.Client
}

// NewClient creates a client for baseURL ("" means DefaultBaseURL). The base
// URL must be https with no path, query or credentials; plain http is accepted
// only for a loopback host (tests). Redirects are refused: Yantra4D serves its
// answers and files itself.
func NewClient(baseURL string, timeout time.Duration) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("yantra4d: invalid base URL %q", baseURL)
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return nil, fmt.Errorf("yantra4d: base URL %q must use https", baseURL)
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("yantra4d: base URL %q must be a bare origin (no credentials, path, query or fragment)", baseURL)
	}
	u.Path = ""
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &Client{
		baseURL: u,
		http: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, _ []*http.Request) error {
				return fmt.Errorf("yantra4d: refusing redirect to %s", req.URL.Redacted())
			},
		},
	}, nil
}

// BaseURL returns the configured origin.
func (c *Client) BaseURL() string { return c.baseURL.String() }

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// --- Manifest ---

// Manifest is the subset of a Yantra4D project manifest (its project.json,
// served by GET /api/projects/<slug>/manifest) the engine reads. Everything
// else in the document is ignored, so new fields there do not break decoding.
type Manifest struct {
	Project       ProjectMeta `json:"project"`
	Modes         []Mode      `json:"modes"`
	Parts         []PartDef   `json:"parts"`
	Parameters    []Parameter `json:"parameters"`
	ExportFormats []string    `json:"export_formats"`
}

// ProjectMeta holds project-level metadata.
type ProjectMeta struct {
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Version string `json:"version"`
	// Engine is the project's default engine (openscad, cadquery, ...). A
	// mode may override it.
	Engine string `json:"engine"`
}

// Mode is a render mode: the unit Yantra4D renders. A render request names a
// mode and gets back every part of that mode.
type Mode struct {
	ID     string            `json:"id"`
	Label  map[string]string `json:"label"`
	Parts  []string          `json:"parts"`
	Engine string            `json:"engine,omitempty"`
}

// PartDef is a part declared by the manifest.
type PartDef struct {
	ID    string            `json:"id"`
	Label map[string]string `json:"label"`
}

// Parameter is a configurable parameter. Options vary by parameter type
// (Yantra4D sends objects for select parameters), so they are kept raw.
type Parameter struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Default any             `json:"default"`
	Options json.RawMessage `json:"options,omitempty"`
}

// ModeByID returns the mode with that id.
func (m *Manifest) ModeByID(id string) (*Mode, bool) {
	for i := range m.Modes {
		if m.Modes[i].ID == id {
			return &m.Modes[i], true
		}
	}
	return nil, false
}

// GetManifest fetches a project manifest. token may be "" (anonymous).
func (c *Client) GetManifest(ctx context.Context, project, token string) (*Manifest, error) {
	if !slugPattern.MatchString(project) {
		return nil, &Error{Op: "manifest", Code: "invalid_request", Message: fmt.Sprintf("invalid project slug %q", project)}
	}
	resp, err := c.do(ctx, http.MethodGet, "/api/projects/"+project+"/manifest", nil, token)
	if err != nil {
		return nil, &Error{Op: "manifest", Message: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errorAnswer("manifest", resp)
	}
	var m Manifest
	if err := decodeJSON(resp.Body, &m); err != nil {
		return nil, &Error{Op: "manifest", Status: resp.StatusCode, Code: "invalid_answer", Message: err.Error()}
	}
	if m.Project.Slug != project {
		return nil, &Error{Op: "manifest", Status: resp.StatusCode, Code: "invalid_answer",
			Message: fmt.Sprintf("manifest is for project %q, not %q", m.Project.Slug, project)}
	}
	return &m, nil
}

// --- Render ---

// RenderRequest is Yantra4D's documented render contract: parameters nested
// under "parameters", the mode named explicitly.
type RenderRequest struct {
	Project      string         `json:"project"`
	Mode         string         `json:"mode"`
	Parameters   map[string]any `json:"parameters"`
	ExportFormat string         `json:"export_format"`
	// IgnoreCache asks Yantra4D to render again rather than answer from its
	// cache (a cached answer may point at a file about to be deleted: render
	// artifacts are collected 24 hours after they are made).
	IgnoreCache bool `json:"ignore_cache,omitempty"`
}

// Part is one rendered part of the render answer, GOC-1 fields included.
type Part struct {
	Type         string `json:"type"`
	URL          string `json:"url"`
	SizeBytes    *int64 `json:"size_bytes,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	MediaType    string `json:"media_type,omitempty"`
	InstanceID   string `json:"instance_id,omitempty"`
	VariablesURL string `json:"variables_url,omitempty"`
}

// GeneratorOutput is the render answer's GOC-1 summary.
type GeneratorOutput struct {
	FormatVersion   string `json:"format_version"`
	Complete        bool   `json:"complete"`
	VariablesSHA256 string `json:"variables_sha256"`
}

// RenderResponse is the synchronous render answer.
type RenderResponse struct {
	Status    string `json:"status"`
	Parts     []Part `json:"parts"`
	RequestID string `json:"request_id"`
	// Log is Yantra4D's render log. A failed part is reported only here.
	Log             string           `json:"log"`
	GeneratorOutput *GeneratorOutput `json:"generator_output,omitempty"`
}

// FailedParts lists the parts the render log reports as failed.
func (r *RenderResponse) FailedParts() []string {
	var out []string
	for _, m := range failedPartLine.FindAllStringSubmatch(r.Log, -1) {
		out = append(out, m[1])
	}
	return out
}

// Render calls POST /api/render. token may be "" (anonymous: Yantra4D's guest
// tier, which may export STL only). An error answer, a status other than
// "success", an answer with no parts and an answer whose URLs point anywhere
// but the configured host are all errors.
func (c *Client) Render(ctx context.Context, req RenderRequest, token string) (*RenderResponse, error) {
	if !slugPattern.MatchString(req.Project) {
		return nil, &Error{Op: "render", Code: "invalid_request", Message: fmt.Sprintf("invalid project slug %q", req.Project)}
	}
	if req.Mode == "" {
		return nil, &Error{Op: "render", Code: "invalid_request",
			Message: "mode is required (Yantra4D renders the first mode of the project when it is missing)"}
	}
	if _, ok := mediaTypes[req.ExportFormat]; !ok {
		return nil, &Error{Op: "render", Code: "invalid_request",
			Message: fmt.Sprintf("unsupported export format %q (Yantra4D renders STL instead of a format it does not know)", req.ExportFormat)}
	}
	if req.Parameters == nil {
		req.Parameters = map[string]any{}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, &Error{Op: "render", Code: "invalid_request", Message: err.Error()}
	}
	resp, err := c.do(ctx, http.MethodPost, "/api/render", body, token)
	if err != nil {
		return nil, &Error{Op: "render", Message: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errorAnswer("render", resp)
	}
	var out RenderResponse
	if err := decodeJSON(resp.Body, &out); err != nil {
		return nil, &Error{Op: "render", Status: resp.StatusCode, Code: "invalid_answer", Message: err.Error()}
	}
	if out.Status != "success" {
		return nil, &Error{Op: "render", Status: resp.StatusCode, Code: "render_failed",
			Message: fmt.Sprintf("render status %q%s", out.Status, logExcerpt(out.Log))}
	}
	if len(out.Parts) == 0 {
		return nil, &Error{Op: "render", Status: resp.StatusCode, Code: "render_failed",
			Message: "render returned no parts" + logExcerpt(out.Log)}
	}
	for _, p := range out.Parts {
		for _, ref := range []string{p.URL, p.VariablesURL} {
			if ref == "" {
				continue
			}
			if _, err := c.sameOrigin(ref); err != nil {
				return nil, &Error{Op: "render", Status: resp.StatusCode, Code: "url_rejected",
					Message: fmt.Sprintf("part %q: %v", p.Type, err)}
			}
		}
	}
	return &out, nil
}

// --- Fetch: render + download + verify ---

// Artifact is a downloaded, verified part.
type Artifact struct {
	Project string
	Mode    string
	Part    string
	Data    []byte
	// MediaType is the GOC-1 media type of Data (matches the request).
	MediaType string
	// SHA256 is the digest of Data, equal to the answer's and the sidecar's.
	SHA256          string
	InstanceID      string
	VariablesSHA256 string
	// Complete reports whether every variable was supplied or defaulted by
	// the manifest (GOC-1 §2), as the sidecar states.
	Complete      bool
	SidecarSHA256 string
	RequestID     string
	// LastModified is the artifact's Last-Modified (when it was rendered;
	// earlier than the request on a cache hit). Zero when not sent.
	LastModified time.Time
}

// Sidecar is the subset of a GOC-1 variables.json document the client checks.
type Sidecar struct {
	Format          string `json:"format"`
	FormatVersion   string `json:"format_version"`
	InstanceID      string `json:"instance_id"`
	VariablesSHA256 string `json:"variables_sha256"`
	Complete        bool   `json:"complete"`
	Generator       struct {
		Platform  string  `json:"platform"`
		Cartridge string  `json:"cartridge"`
		Mode      string  `json:"mode"`
		Part      *string `json:"part"`
		Source    struct {
			TreeSHA256 string `json:"tree_sha256"`
		} `json:"source"`
	} `json:"generator"`
	Geometry []struct {
		Path      string `json:"path"`
		MediaType string `json:"media_type"`
		SHA256    string `json:"sha256"`
		Bytes     int64  `json:"bytes"`
		Role      string `json:"role"`
	} `json:"geometry"`
}

// Fetch renders req, selects part, downloads it and its GOC-1 sidecar, and
// verifies both. Any doubt is an error; nothing partial is returned.
func (c *Client) Fetch(ctx context.Context, req RenderRequest, part, token string) (*Artifact, error) {
	if part == "" {
		return nil, &Error{Op: "fetch", Code: "invalid_request", Message: "part is required"}
	}
	resp, err := c.Render(ctx, req, token)
	if err != nil {
		return nil, err
	}
	p, err := SelectPart(resp, part)
	if err != nil {
		return nil, err
	}
	want := mediaTypes[req.ExportFormat]
	if p.SHA256 == "" || p.InstanceID == "" || p.VariablesURL == "" || p.MediaType == "" {
		return nil, &Error{Op: "fetch", Code: "goc1_missing",
			Message: fmt.Sprintf("part %q has no GOC-1 fields (sha256, media_type, instance_id, variables_url); its bytes cannot be verified", part)}
	}
	if !sha256Pattern.MatchString(p.SHA256) || !sha256Pattern.MatchString(p.InstanceID) {
		return nil, &Error{Op: "fetch", Code: "invalid_answer", Message: fmt.Sprintf("part %q: sha256 or instance_id is not a sha256 hex digest", part)}
	}
	if p.MediaType != want {
		return nil, &Error{Op: "fetch", Code: "media_type_mismatch",
			Message: fmt.Sprintf("part %q came back as %s, %s was requested", part, p.MediaType, want)}
	}
	if resp.GeneratorOutput == nil || !sha256Pattern.MatchString(resp.GeneratorOutput.VariablesSHA256) {
		return nil, &Error{Op: "fetch", Code: "goc1_missing", Message: "render answer has no generator_output.variables_sha256"}
	}

	data, modified, err := c.download(ctx, "artifact", p.URL, token)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != p.SHA256 {
		return nil, &Error{Op: "artifact", Code: "digest_mismatch",
			Message: fmt.Sprintf("part %q: downloaded bytes have sha256 %s, the render answer says %s", part, got, p.SHA256)}
	}
	if p.SizeBytes != nil && *p.SizeBytes != int64(len(data)) {
		return nil, &Error{Op: "artifact", Code: "digest_mismatch",
			Message: fmt.Sprintf("part %q: downloaded %d bytes, the render answer says %d", part, len(data), *p.SizeBytes)}
	}

	raw, _, err := c.download(ctx, "sidecar", p.VariablesURL, token)
	if err != nil {
		return nil, err
	}
	var doc Sidecar
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, &Error{Op: "sidecar", Code: "sidecar_mismatch", Message: "not JSON: " + err.Error()}
	}
	if err := CheckSidecar(&doc, req.Project, req.Mode, *p, int64(len(data)), resp.GeneratorOutput.VariablesSHA256); err != nil {
		return nil, err
	}
	sidecarSum := sha256.Sum256(raw)
	return &Artifact{
		Project: req.Project, Mode: req.Mode, Part: part, Data: data,
		MediaType: p.MediaType, SHA256: got, InstanceID: doc.InstanceID,
		VariablesSHA256: doc.VariablesSHA256, Complete: doc.Complete,
		SidecarSHA256: hex.EncodeToString(sidecarSum[:]), RequestID: resp.RequestID,
		LastModified: modified,
	}, nil
}

// SelectPart returns the requested part of a render answer. A part the log
// reports as failed is an error even if something under its name came back;
// a part that is absent is an error whether or not the log explains why.
func SelectPart(resp *RenderResponse, part string) (*Part, error) {
	for _, f := range resp.FailedParts() {
		if f == part {
			return nil, &Error{Op: "render", Code: "part_failed",
				Message: fmt.Sprintf("Yantra4D reports part %q as failed%s", part, logExcerpt(resp.Log))}
		}
	}
	var found *Part
	for i := range resp.Parts {
		if resp.Parts[i].Type == part {
			if found != nil {
				return nil, &Error{Op: "render", Code: "invalid_answer", Message: fmt.Sprintf("part %q appears twice in the answer", part)}
			}
			found = &resp.Parts[i]
		}
	}
	if found == nil {
		names := make([]string, 0, len(resp.Parts))
		for _, p := range resp.Parts {
			names = append(names, p.Type)
		}
		return nil, &Error{Op: "render", Code: "part_missing",
			Message: fmt.Sprintf("render returned no part %q (got %s)%s", part, strings.Join(names, ", "), logExcerpt(resp.Log))}
	}
	return found, nil
}

// CheckSidecar verifies that a GOC-1 sidecar describes exactly this part of
// this render: format 1.x, same instance id as the answer (and the one GOC-1
// §3.4 derives from its own fields), same cartridge, mode and part, the same
// variables digest as the answer, and a primary geometry entry whose digest
// and size are those of the downloaded bytes.
func CheckSidecar(doc *Sidecar, project, mode string, part Part, size int64, variablesSHA256 string) error {
	bad := func(format string, a ...any) error {
		return &Error{Op: "sidecar", Code: "sidecar_mismatch", Message: fmt.Sprintf(format, a...)}
	}
	if doc.Format != GOC1Format || !strings.HasPrefix(doc.FormatVersion, "1.") {
		return bad("not a GOC-1 v1 document (format %q %q)", doc.Format, doc.FormatVersion)
	}
	for name, v := range map[string]string{"instance_id": doc.InstanceID, "variables_sha256": doc.VariablesSHA256,
		"generator.source.tree_sha256": doc.Generator.Source.TreeSHA256} {
		if !sha256Pattern.MatchString(v) {
			return bad("%s is not a sha256 hex digest", name)
		}
	}
	if doc.InstanceID != part.InstanceID {
		return bad("instance_id %s differs from the render answer's %s", doc.InstanceID, part.InstanceID)
	}
	if doc.Generator.Cartridge != project || doc.Generator.Mode != mode || doc.Generator.Part == nil || *doc.Generator.Part != part.Type {
		got := "<none>"
		if doc.Generator.Part != nil {
			got = *doc.Generator.Part
		}
		return bad("sidecar describes %s/%s/%s, not %s/%s/%s", doc.Generator.Cartridge, doc.Generator.Mode, got, project, mode, part.Type)
	}
	if doc.VariablesSHA256 != variablesSHA256 {
		return bad("variables_sha256 %s differs from the render answer's %s", doc.VariablesSHA256, variablesSHA256)
	}
	derived, err := InstanceID(doc.Generator.Cartridge, doc.Generator.Mode, doc.Generator.Part, doc.Generator.Source.TreeSHA256, doc.VariablesSHA256)
	if err != nil {
		return bad("derive instance_id: %v", err)
	}
	if derived != doc.InstanceID {
		return bad("instance_id %s is not the GOC-1 identity of its own fields (%s)", doc.InstanceID, derived)
	}
	for _, g := range doc.Geometry {
		if g.Role != "primary" {
			continue
		}
		if g.SHA256 != part.SHA256 {
			return bad("primary geometry sha256 %s differs from the downloaded part's %s", g.SHA256, part.SHA256)
		}
		if g.Bytes != size {
			return bad("primary geometry is %d bytes, the downloaded part is %d", g.Bytes, size)
		}
		return nil
	}
	return bad("sidecar lists no primary geometry")
}

// InstanceID is the GOC-1 §3.4 instance identity: the sha256 of the canonical
// JSON {cartridge, mode, part, tree_sha256, variables_sha256} (keys sorted,
// no whitespace, UTF-8, part null when absent).
func InstanceID(cartridge, mode string, part *string, treeSHA256, variablesSHA256 string) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// encoding/json writes map keys sorted, which is the canonical order.
	if err := enc.Encode(map[string]any{"cartridge": cartridge, "mode": mode, "part": part,
		"tree_sha256": treeSHA256, "variables_sha256": variablesSHA256}); err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
	return hex.EncodeToString(sum[:]), nil
}

// --- transport ---

func (c *Client) do(ctx context.Context, method, path string, body []byte, token string) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL.String()+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return c.http.Do(req)
}

// sameOrigin resolves a URL from an answer and refuses anything that does not
// point back at the configured scheme and host.
func (c *Client) sameOrigin(ref string) (*url.URL, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return nil, fmt.Errorf("unparseable URL %q", ref)
	}
	abs := c.baseURL.ResolveReference(u)
	if abs.Scheme != c.baseURL.Scheme || abs.Host != c.baseURL.Host || abs.User != nil {
		return nil, fmt.Errorf("URL %q is not on %s", ref, c.baseURL.String())
	}
	return abs, nil
}

func (c *Client) download(ctx context.Context, op, ref, token string) ([]byte, time.Time, error) {
	abs, err := c.sameOrigin(ref)
	if err != nil {
		return nil, time.Time{}, &Error{Op: op, Code: "url_rejected", Message: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, abs.String(), nil)
	if err != nil {
		return nil, time.Time{}, &Error{Op: op, Message: err.Error()}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, time.Time{}, &Error{Op: op, Message: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, errorAnswer(op, resp)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArtifactBytes+1))
	if err != nil {
		return nil, time.Time{}, &Error{Op: op, Status: resp.StatusCode, Message: err.Error()}
	}
	if len(data) > maxArtifactBytes {
		return nil, time.Time{}, &Error{Op: op, Status: resp.StatusCode, Code: "invalid_answer", Message: "file exceeds the download limit"}
	}
	var modified time.Time
	if t, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
		modified = t.UTC()
	}
	return data, modified, nil
}

func decodeJSON(r io.Reader, v any) error {
	raw, err := io.ReadAll(io.LimitReader(r, maxJSONBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxJSONBytes {
		return errors.New("answer exceeds the size limit")
	}
	return json.Unmarshal(raw, v)
}

// errorAnswer turns a non-200 answer into an Error, keeping Yantra4D's
// {"error", "error_code"} when the body has them.
func errorAnswer(op string, resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var body struct {
		Error     string `json:"error"`
		ErrorCode string `json:"error_code"`
	}
	e := &Error{Op: op, Status: resp.StatusCode}
	if json.Unmarshal(raw, &body) == nil && (body.Error != "" || body.ErrorCode != "") {
		e.Code, e.Message = body.ErrorCode, oneLine(body.Error)
	} else {
		e.Message = oneLine(string(raw))
	}
	return e
}

// logExcerpt returns the end of a render log (where failures are reported)
// as a one-line suffix for an error message.
func logExcerpt(log string) string {
	log = oneLine(log)
	if log == "" {
		return ""
	}
	return ": " + log
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(strings.ToValidUTF8(s, "")), " ")
	const maxRunes = 300
	if r := []rune(s); len(r) > maxRunes {
		s = "…" + string(r[len(r)-maxRunes:])
	}
	return s
}
