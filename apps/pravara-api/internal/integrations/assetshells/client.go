// Package assetshells is pravara's client for the asset-shells service
// (SEM-1 §6): the Part 2 read API at /api/v3.1 (type shells are publicly
// readable) and the MADFAM publish API at /madfam/v1 (instance shells and
// passport events, with an org-bound publisher token).
package assetshells

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/remote"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
)

const service = "asset-shells"

// Client calls asset-shells.
type Client struct {
	baseURL *url.URL
	http    *http.Client
}

// NewClient creates a client for baseURL (e.g. https://asset-shells-api.madfam.io).
func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("asset-shells: invalid base URL %q", baseURL)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: u, http: httpClient}, nil
}

// EncodeID is the Part 2 UTF8-BASE64-URL identifier encoding (no padding).
func EncodeID(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}

// Shell is the part of an AssetAdministrationShell pravara reads.
type Shell struct {
	ID               string `json:"id"`
	IDShort          string `json:"idShort"`
	AssetInformation struct {
		AssetKind        string `json:"assetKind"`
		GlobalAssetID    string `json:"globalAssetId"`
		SpecificAssetIDs []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"specificAssetIds"`
	} `json:"assetInformation"`
	Submodels []Reference `json:"submodels"`
}

// Reference is an AAS reference.
type Reference struct {
	Type string `json:"type"`
	Keys []struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	} `json:"keys"`
}

// SpecificAssetID returns the value of a named specific asset id.
func (s *Shell) SpecificAssetID(name string) string {
	for _, a := range s.AssetInformation.SpecificAssetIDs {
		if a.Name == name {
			return a.Value
		}
	}
	return ""
}

// SubmodelID returns the id of the referenced submodel whose id ends with
// "/<idShort>" (SEM-1 §1 submodel ids), or "".
func (s *Shell) SubmodelID(idShort string) string {
	for _, ref := range s.Submodels {
		for _, k := range ref.Keys {
			if k.Type == "Submodel" && strings.HasSuffix(k.Value, "/"+idShort) {
				return k.Value
			}
		}
	}
	return ""
}

// GetShell reads one shell (anonymous: type shells are public).
func (c *Client) GetShell(ctx context.Context, shellID string) (*Shell, error) {
	var shell Shell
	if err := c.getJSON(ctx, "/api/v3.1/shells/"+EncodeID(shellID), "read shell", &shell); err != nil {
		return nil, err
	}
	return &shell, nil
}

// FindTypeShells lists shells whose specific asset ids match every pair.
func (c *Client) FindTypeShells(ctx context.Context, pairs map[string]string) ([]Shell, error) {
	ids := make([]map[string]string, 0, len(pairs))
	for name, value := range pairs {
		ids = append(ids, map[string]string{"name": name, "value": value})
	}
	raw, _ := json.Marshal(ids)
	q := url.Values{"assetIds": {base64.RawURLEncoding.EncodeToString(raw)}, "limit": {"20"}}
	var page struct {
		Result []Shell `json:"result"`
	}
	if err := c.getJSON(ctx, "/api/v3.1/shells?"+q.Encode(), "find shells", &page); err != nil {
		return nil, err
	}
	out := page.Result[:0]
	for _, s := range page.Result {
		if s.AssetInformation.AssetKind == "Type" {
			out = append(out, s)
		}
	}
	return out, nil
}

// GetSubmodel reads a submodel through its shell (Part 2 GetSubmodelById_AasRepository).
func (c *Client) GetSubmodel(ctx context.Context, shellID, submodelID string) (map[string]any, error) {
	var sm map[string]any
	path := "/api/v3.1/shells/" + EncodeID(shellID) + "/submodels/" + EncodeID(submodelID)
	if err := c.getJSON(ctx, path, "read submodel", &sm); err != nil {
		return nil, err
	}
	return sm, nil
}

func (c *Client) getJSON(ctx context.Context, path, op string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL.String()+path, nil)
	if err != nil {
		return fmt.Errorf("asset-shells: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return remote.Wrap(service, op, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		e := remote.StatusError(service, op, resp)
		if resp.StatusCode == http.StatusNotFound {
			e.Code, e.Retryable = "not_found", false
		}
		return e
	}
	if _, err := remote.ReadJSON(resp, out); err != nil {
		return &remote.Error{Service: service, Op: op, Status: resp.StatusCode, Message: err.Error(), Retryable: true}
	}
	return nil
}

// PublishResult is the publish API's answer.
type PublishResult struct {
	Created bool
	Status  int
	Body    map[string]any
}

// PublishInstance posts an instance environment (POST /madfam/v1/instances).
// Replaying the same environment is a 200; other content for an existing id
// is a 409 (terminal).
func (c *Client) PublishInstance(ctx context.Context, ts *machineclients.TokenSource, env []byte) (*PublishResult, error) {
	return c.post(ctx, ts, "/madfam/v1/instances", "publish instance", env)
}

// AppendPassportEvent posts one passport event to an instance. eventId makes
// it idempotent: a replay is a 200, a reused id with other content a 409.
func (c *Client) AppendPassportEvent(ctx context.Context, ts *machineclients.TokenSource, instanceUUID string, body []byte) (*PublishResult, error) {
	return c.post(ctx, ts, "/madfam/v1/instances/"+url.PathEscape(instanceUUID)+"/passport-events", "append passport event", body)
}

func (c *Client) post(ctx context.Context, ts *machineclients.TokenSource, path, op string, body []byte) (*PublishResult, error) {
	resp, err := machineclients.Do(ctx, c.http, ts, func(ctx context.Context) (*http.Request, error) {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL.String()+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json")
		return r, nil
	})
	if err != nil {
		return nil, remote.Wrap(service, op, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		e := remote.StatusError(service, op, resp)
		// 403 = token lacks scope or tenant: an operator must fix the client;
		// keep retrying slowly so the fix needs no replay.
		e.Retryable = e.Retryable || resp.StatusCode == http.StatusForbidden
		return nil, e
	}
	out := &PublishResult{Created: resp.StatusCode == http.StatusCreated, Status: resp.StatusCode}
	if _, err := remote.ReadJSON(resp, &out.Body); err != nil {
		out.Body = map[string]any{"unreadable_response": err.Error()}
	}
	return out, nil
}
