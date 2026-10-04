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
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

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

// NewClient creates a client for baseURL (e.g. https://yantra4d.madfam.io).
func NewClient(baseURL string, tokens *machineclients.TokenSource, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("yantra4d: invalid base URL %q", baseURL)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	return &Client{baseURL: u, http: httpClient, tokens: tokens}, nil
}

// RenderRequest is yantra4d's documented render contract: parameters nested
// under "parameters".
type RenderRequest struct {
	Project      string         `json:"project"`
	Mode         string         `json:"mode"`
	Parameters   map[string]any `json:"parameters"`
	ExportFormat string         `json:"export_format"`
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
	Status          string `json:"status"`
	Parts           []Part `json:"parts"`
	RequestID       string `json:"request_id"`
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
	return &out, nil
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
	resp, err := machineclients.Do(ctx, c.http, c.tokens, func(ctx context.Context) (*http.Request, error) {
		r, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolve(part.VariablesURL), nil)
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
	return &doc, hex.EncodeToString(sum[:]), nil
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

// AbsoluteURL resolves a URL the envelope returns (often "/static/…").
func (c *Client) AbsoluteURL(ref string) string { return c.resolve(ref) }

func (c *Client) resolve(ref string) string {
	u, err := url.Parse(ref)
	if err != nil {
		return c.baseURL.String() + ref
	}
	return c.baseURL.ResolveReference(u).String()
}

func wrap(op string, err error) error { return remote.Wrap(service, op, err) }
