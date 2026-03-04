// Package integrations provides HTTP clients for external MADFAM services.
package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// TezcaClient is an HTTP client for the Tezca Mexican-law REST API.
// Default domains: manufacturing, professional_services (SCIAN 31-33, 54).
type TezcaClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewTezcaClient creates a Tezca client.
func NewTezcaClient(baseURL, apiKey string) *TezcaClient {
	return &TezcaClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *TezcaClient) doGet(ctx context.Context, path string, params url.Values) (map[string]interface{}, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("tezca: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tezca: request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tezca: %s returned %d: %s", path, resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("tezca: decode response: %w", err)
	}
	return result, nil
}

// SearchLaws searches law metadata via GET /laws/?search=<query>&domain=<domain>.
func (c *TezcaClient) SearchLaws(ctx context.Context, query, domain string) (map[string]interface{}, error) {
	params := url.Values{"search": {query}}
	if domain != "" {
		params.Set("domain", domain)
	}
	return c.doGet(ctx, "/laws/", params)
}

// SearchArticles does full-text search via GET /search/?q=<query>&domain=<domain>.
func (c *TezcaClient) SearchArticles(ctx context.Context, query, domain string) (map[string]interface{}, error) {
	params := url.Values{"q": {query}}
	if domain != "" {
		params.Set("domain", domain)
	}
	return c.doGet(ctx, "/search/", params)
}

// GetLawDetail fetches a single law by official_id slug.
func (c *TezcaClient) GetLawDetail(ctx context.Context, lawID string) (map[string]interface{}, error) {
	return c.doGet(ctx, "/laws/"+lawID+"/", nil)
}

// GetLawArticles fetches paginated articles for a law.
func (c *TezcaClient) GetLawArticles(ctx context.Context, lawID string, page int) (map[string]interface{}, error) {
	params := url.Values{"page": {fmt.Sprintf("%d", page)}}
	return c.doGet(ctx, "/laws/"+lawID+"/articles/", params)
}

// GetChangelog fetches recent law changes since the given ISO date.
func (c *TezcaClient) GetChangelog(ctx context.Context, since string) (map[string]interface{}, error) {
	params := url.Values{}
	if since != "" {
		params.Set("since", since)
	}
	return c.doGet(ctx, "/changelog/", params)
}
