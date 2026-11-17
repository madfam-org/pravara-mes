// Package yantra4d is pravara's dispatch client for yantra4d renders. It
// authenticates with pravara's own Janua machine client (audience
// yantra4d-api, scope yantra4d:render) instead of forwarding a person's token,
// and it reads the GOC-1 generator-output sidecar (variables.json) of every
// part it dispatches. It does no geometry work: it only routes JSON and
// digests.
package yantra4d

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/remote"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
)

const service = "yantra4d"

// GOC1Format is the generator-output format identifier (GOC-1 §2).
const GOC1Format = "hyperobjects.generator-output"

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Client calls yantra4d's render API.
type Client struct {
	baseURL *url.URL
	http    *http.Client
	tokens  *machineclients.TokenSource
}

// NewClient creates a client for baseURL (e.g. https://api.yantra4d.com).
// The base URL must be https (plain http only on a loopback host, for
// tests): the client sends a machine bearer token with every call.
//
// Every URL a render answer carries is followed only when it has the base
// URL's scheme and host (OriginURL), and the client never follows a
// redirect: yantra4d serves files directly, and a bearer must not travel to
// any other host.
func NewClient(baseURL string, tokens *machineclients.TokenSource, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("yantra4d: invalid base URL %q", baseURL)
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !isLoopback(u.Hostname())) {
		return nil, fmt.Errorf("yantra4d: base URL %q must be https (plain http only on a loopback host)", baseURL)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	noRedirects := *httpClient
	noRedirects.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{baseURL: u, http: &noRedirects, tokens: tokens}, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CodeOffOrigin is the remote.Error code of a URL in a render answer that
// points away from yantra4d's own origin.
const CodeOffOrigin = "url_off_origin"

// OriginURL resolves a URL a render answer returned (often "/static/…")
// against the base URL and refuses it unless the result has the base URL's
// scheme and host. An absolute URL on another host, a protocol-relative
// "//host/…" and a downgrade to http are all refused: pravara would send its
// machine bearer there.
func (c *Client) OriginURL(ref string) (string, error) {
	off := func(why string) error {
		return &remote.Error{Service: service, Op: "url", Code: CodeOffOrigin,
			Message: fmt.Sprintf("%q %s; only %s://%s is followed", ref, why, c.baseURL.Scheme, c.baseURL.Host)}
	}
	if strings.TrimSpace(ref) == "" {
		return "", off("is empty")
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", off("is not a URL")
	}
	abs := c.baseURL.ResolveReference(u)
	switch {
	case abs.User != nil:
		return "", off("carries credentials")
	case abs.Scheme != c.baseURL.Scheme:
		return "", off("uses another scheme")
	case abs.Host != c.baseURL.Host:
		return "", off("is on another host")
	}
	return abs.String(), nil
}

// redirectRefused reports a 3xx answer, which the client does not follow.
func redirectRefused(op string, resp *http.Response) error {
	return &remote.Error{Service: service, Op: op, Status: resp.StatusCode, Code: "redirect_refused",
		Message: "yantra4d answered with a redirect; pravara does not follow redirects"}
}

// RenderRequest is yantra4d's documented render contract: parameters nested
// under "parameters".
type RenderRequest struct {
	Project      string         `json:"project"`
	Mode         string         `json:"mode"`
	Parameters   map[string]any `json:"parameters"`
	ExportFormat string         `json:"export_format"`
	// IgnoreCache asks yantra4d to render again instead of answering from
	// its cache. A cached answer can point at an artifact that is about to
	// be deleted (it is collected 24 hours after it was first made).
	IgnoreCache bool `json:"ignore_cache,omitempty"`
}

// Part is one rendered part of the envelope (GOC-1 §5 fields included).
type Part struct {
	Type         string `json:"type"`
	URL          string `json:"url"`
	SizeBytes    *int64 `json:"size_bytes,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	MediaType    string `json:"media_type,omitempty"`
	InstanceID   string `json:"instance_id,omitempty"`
	VariablesURL string `json:"variables_url,omitempty"`
}

// RenderResponse is the synchronous render envelope.
type RenderResponse struct {
	Status    string `json:"status"`
	Parts     []Part `json:"parts"`
	RequestID string `json:"request_id"`
	// Log is yantra4d's render log. A part that failed or timed out is left
	// out of Parts while Status stays "success"; its reason is only here.
	Log             string `json:"log"`
	GeneratorOutput *struct {
		FormatVersion   string `json:"format_version"`
		Complete        bool   `json:"complete"`
		VariablesSHA256 string `json:"variables_sha256"`
	} `json:"generator_output,omitempty"`
}

// Render calls POST /api/render.
func (c *Client) Render(ctx context.Context, req RenderRequest) (*RenderResponse, error) {
	if req.Parameters == nil {
		req.Parameters = map[string]any{}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("yantra4d: encode render request: %w", err)
	}
	resp, err := machineclients.Do(ctx, c.http, c.tokens, func(ctx context.Context) (*http.Request, error) {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.resolve("/api/render"), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json")
		return r, nil
	})
	if err != nil {
		return nil, wrap("render", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		e := remote.StatusError(service, "render", resp)
		// 400 (bad payload) and 403 (scope, tier or project access) are not
		// fixed by retrying; 503 (worker unavailable) is.
		return nil, e
	}
	var out RenderResponse
	if _, err := remote.ReadJSON(resp, &out); err != nil {
		return nil, &remote.Error{Service: service, Op: "render", Status: resp.StatusCode, Message: err.Error()}
	}
	if out.Status != "success" {
		return nil, &remote.Error{Service: service, Op: "render", Status: resp.StatusCode,
			Message: fmt.Sprintf("render status %q", out.Status), Retryable: true}
	}
	// yantra4d answers 200 "success" even when every part failed or timed
	// out: the parts are simply absent. Nothing to slice is not a success.
	if len(out.Parts) == 0 {
		return nil, &remote.Error{Service: service, Op: "render", Status: resp.StatusCode,
			Message: "render returned no parts" + LogExcerpt(out.Log), Retryable: true}
	}
	return &out, nil
}

// LogExcerpt returns the end of a render log as a suffix for an error
// message ("" when the log is empty). The end is where a failure is reported.
//
// The result is stored (a dispatch's error message is a text column) and
// returned by the API, and the log is another service's output: it is made
// valid UTF-8, stripped of control characters (PostgreSQL refuses NUL in
// text), collapsed to one line and cut on a character boundary.
func LogExcerpt(log string) string {
	log = strings.Map(func(r rune) rune {
		if r == utf8.RuneError || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(log, ""))
	log = strings.Join(strings.Fields(log), " ")
	if log == "" {
		return ""
	}
	const maxRunes = 300
	if runes := []rune(log); len(runes) > maxRunes {
		log = "…" + string(runes[len(runes)-maxRunes:])
	}
	return ": " + log
}

// Sidecar is the subset of a GOC-1 variables.json document pravara records.
type Sidecar struct {
	Format          string `json:"format"`
	FormatVersion   string `json:"format_version"`
	Kind            string `json:"kind"`
	InstanceID      string `json:"instance_id"`
	VariablesSHA256 string `json:"variables_sha256"`
	Complete        bool   `json:"complete"`
	Generator       struct {
		Platform  string  `json:"platform"`
		Cartridge string  `json:"cartridge"`
		Mode      string  `json:"mode"`
		Part      *string `json:"part"`
		Engine    string  `json:"engine"`
		Source    struct {
			TreeSHA256 string `json:"tree_sha256"`
		} `json:"source"`
	} `json:"generator"`
	// ArtifactModified is the sidecar file's Last-Modified, when yantra4d
	// sends one: when the render was actually made, which on a cache hit is
	// earlier than the request. Zero when unknown. Not part of the document.
	ArtifactModified time.Time `json:"-"`
	// Raw is the exact document as downloaded: the bytes whose sha256
	// FetchSidecar returns, kept by dispatch. Not part of the document.
	Raw      []byte `json:"-"`
	Geometry []struct {
		Path      string `json:"path"`
		MediaType string `json:"media_type"`
		SHA256    string `json:"sha256"`
		Bytes     int64  `json:"bytes"`
		Role      string `json:"role"`
	} `json:"geometry"`
}

// FetchSidecar downloads a part's variables.json, returns it with the sha256
// of the exact bytes, and checks it describes the part (fail closed: GOC-1
// §5 failure policy).
func (c *Client) FetchSidecar(ctx context.Context, part Part) (*Sidecar, string, error) {
	if part.VariablesURL == "" {
		return nil, "", &remote.Error{Service: service, Op: "sidecar", Code: "sidecar_missing",
			Message: fmt.Sprintf("part %q has no variables_url (GOC-1 sidecar); dispatch requires it", part.Type)}
	}
	target, err := c.OriginURL(part.VariablesURL)
	if err != nil {
		return nil, "", err
	}
	resp, err := machineclients.Do(ctx, c.http, c.tokens, func(ctx context.Context) (*http.Request, error) {
		r, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		r.Header.Set("Accept", "application/json")
		return r, nil
	})
	if err != nil {
		return nil, "", wrap("sidecar", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, "", redirectRefused("sidecar", resp)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", remote.StatusError(service, "sidecar", resp)
	}
	var doc Sidecar
	raw, err := remote.ReadJSON(resp, &doc)
	if err != nil {
		return nil, "", &remote.Error{Service: service, Op: "sidecar", Code: "sidecar_invalid", Message: err.Error()}
	}
	sum := sha256.Sum256(raw)
	if err := CheckSidecar(&doc, part); err != nil {
		return nil, "", err
	}
	if modified, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
		doc.ArtifactModified = modified.UTC()
	}
	doc.Raw = raw
	return &doc, hex.EncodeToString(sum[:]), nil
}

// OpenArtifact starts the download of a render artifact (a part's geometry
// or its sidecar) at ref, a URL the envelope returned, on yantra4d's own
// origin only (OriginURL). It returns the body and its declared length (-1
// when unknown); the caller bounds, verifies and closes it. A non-200 answer
// is a classified error (5xx and 429 retryable; a redirect is refused).
func (c *Client) OpenArtifact(ctx context.Context, ref string) (io.ReadCloser, int64, error) {
	target, err := c.OriginURL(ref)
	if err != nil {
		return nil, 0, err
	}
	resp, err := machineclients.Do(ctx, c.http, c.tokens, func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	})
	if err != nil {
		return nil, 0, wrap("artifact", err)
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		defer func() { _ = resp.Body.Close() }()
		return nil, 0, redirectRefused("artifact", resp)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		return nil, 0, remote.StatusError(service, "artifact", resp)
	}
	return resp.Body, resp.ContentLength, nil
}

// CheckSidecar verifies that a sidecar is a GOC-1 document for exactly this
// part: same instance id, primary geometry digest equal to the part digest.
func CheckSidecar(doc *Sidecar, part Part) error {
	bad := func(msg string) error {
		return &remote.Error{Service: service, Op: "sidecar", Code: "sidecar_mismatch", Message: msg}
	}
	if doc.Format != GOC1Format || !strings.HasPrefix(doc.FormatVersion, "1.") {
		return bad(fmt.Sprintf("not a GOC-1 v1 document (format %q %q)", doc.Format, doc.FormatVersion))
	}
	for name, v := range map[string]string{
		"instance_id": doc.InstanceID, "variables_sha256": doc.VariablesSHA256,
		"generator.source.tree_sha256": doc.Generator.Source.TreeSHA256, "part sha256": part.SHA256,
	} {
		if !sha256Pattern.MatchString(v) {
			return bad(name + " is not a sha256 hex digest")
		}
	}
	if part.InstanceID != "" && part.InstanceID != doc.InstanceID {
		return bad("sidecar instance_id differs from the render envelope")
	}
	for _, g := range doc.Geometry {
		if g.Role == "primary" || len(doc.Geometry) == 1 {
			if g.SHA256 != part.SHA256 {
				return bad("primary geometry sha256 in the sidecar differs from the rendered part")
			}
			return nil
		}
	}
	return bad("sidecar lists no primary geometry")
}

// resolve joins a path of pravara's own (never one from an answer) to the
// base URL.
func (c *Client) resolve(ref string) string {
	u, err := url.Parse(ref)
	if err != nil {
		return c.baseURL.String() + ref
	}
	return c.baseURL.ResolveReference(u).String()
}

func wrap(op string, err error) error { return remote.Wrap(service, op, err) }
