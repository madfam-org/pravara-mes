// Package machineclients holds the Janua machine clients pravara uses to call
// other MADFAM services (client_credentials grant), with a token cache that
// refreshes before expiry.
//
// Each client has a fixed audience and scope set in Janua; pravara only asks
// for the scope. Secrets come from the pravara-service-clients Secret and are
// never logged, returned in errors, or written anywhere else.
package machineclients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Credentials identify one Janua confidential client.
type Credentials struct {
	// Name is a stable label for logs and errors (never the client id).
	Name         string
	TokenURL     string
	ClientID     string
	ClientSecret string
	// Scope is the space-separated scope requested on every token.
	Scope string
	// Audience is the audience Janua binds to the client. It is documentation
	// for operators; Janua decides the audience, pravara does not request it.
	Audience string
}

// Configured reports whether the credentials are complete.
func (c Credentials) Configured() bool {
	return strings.TrimSpace(c.TokenURL) != "" && strings.TrimSpace(c.ClientID) != "" && c.ClientSecret != ""
}

// ErrNotConfigured is returned when a client has no credentials.
var ErrNotConfigured = errors.New("machine client not configured")

// TokenError is a failed token request. It never carries the secret.
type TokenError struct {
	Client string
	Status int
	Detail string
}

func (e *TokenError) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("machine client %s: token request failed: %s", e.Client, e.Detail)
	}
	return fmt.Sprintf("machine client %s: token endpoint returned %d: %s", e.Client, e.Status, e.Detail)
}

// Retryable reports whether a later attempt may succeed (transport errors,
// rate limiting and server errors). Rejected credentials are not retryable.
func (e *TokenError) Retryable() bool {
	return e.Status == 0 || e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// TokenSource mints and caches access tokens for one client. It is safe for
// concurrent use; concurrent callers share one token request.
type TokenSource struct {
	creds Credentials
	http  *http.Client
	now   func() time.Time

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// NewTokenSource creates a token source. httpClient may be nil.
func NewTokenSource(creds Credentials, httpClient *http.Client) *TokenSource {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &TokenSource{creds: creds, http: httpClient, now: time.Now}
}

// Name returns the client label.
func (s *TokenSource) Name() string { return s.creds.Name }

// Configured reports whether the source has credentials.
func (s *TokenSource) Configured() bool { return s != nil && s.creds.Configured() }

// refreshMargin is how long before expiry a cached token is replaced: a fifth
// of its lifetime, between 30 seconds and 5 minutes.
func refreshMargin(lifetime time.Duration) time.Duration {
	m := lifetime / 5
	if m < 30*time.Second {
		m = 30 * time.Second
	}
	if m > 5*time.Minute {
		m = 5 * time.Minute
	}
	return m
}

// Token returns a valid access token, requesting a new one when the cached
// token is missing or close to expiry.
func (s *TokenSource) Token(ctx context.Context) (string, error) {
	if !s.Configured() {
		name := "unknown"
		if s != nil {
			name = s.creds.Name
		}
		return "", fmt.Errorf("%w: %s", ErrNotConfigured, name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && s.now().Before(s.expiry) {
		return s.token, nil
	}
	token, lifetime, err := s.fetch(ctx)
	if err != nil {
		return "", err
	}
	s.token = token
	s.expiry = s.now().Add(lifetime - refreshMargin(lifetime))
	return token, nil
}

// Invalidate drops the cached token (after a 401 from the callee).
func (s *TokenSource) Invalidate() {
	s.mu.Lock()
	s.token = ""
	s.expiry = time.Time{}
	s.mu.Unlock()
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope"`
}

func (s *TokenSource) fetch(ctx context.Context) (string, time.Duration, error) {
	form := url.Values{"grant_type": {"client_credentials"}}
	if s.creds.Scope != "" {
		form.Set("scope", s.creds.Scope)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.creds.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, &TokenError{Client: s.creds.Name, Detail: "invalid token URL"}
	}
	// Janua decodes Basic credentials without form-unescaping them, so they
	// are sent as is.
	req.SetBasicAuth(s.creds.ClientID, s.creds.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return "", 0, &TokenError{Client: s.creds.Name, Detail: scrubURL(err)}
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return "", 0, &TokenError{Client: s.creds.Name, Status: resp.StatusCode, Detail: errorDetail(body)}
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil || tr.AccessToken == "" {
		return "", 0, &TokenError{Client: s.creds.Name, Status: resp.StatusCode, Detail: "token response has no access_token"}
	}
	if tr.TokenType != "" && !strings.EqualFold(tr.TokenType, "bearer") {
		return "", 0, &TokenError{Client: s.creds.Name, Status: resp.StatusCode, Detail: "token type is not Bearer"}
	}
	if want := strings.Fields(s.creds.Scope); len(want) > 0 && tr.Scope != "" {
		granted := map[string]bool{}
		for _, g := range strings.Fields(tr.Scope) {
			granted[g] = true
		}
		for _, w := range want {
			if !granted[w] {
				return "", 0, &TokenError{Client: s.creds.Name, Status: resp.StatusCode,
					Detail: fmt.Sprintf("granted scope lacks %q", w)}
			}
		}
	}
	lifetime := time.Duration(tr.ExpiresIn) * time.Second
	if lifetime <= 0 {
		lifetime = 5 * time.Minute
	}
	return tr.AccessToken, lifetime, nil
}

// errorDetail extracts an OAuth error or a FastAPI detail, truncated.
func errorDetail(body []byte) string {
	var e struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
		Detail      any    `json:"detail"`
	}
	if json.Unmarshal(body, &e) == nil {
		switch {
		case e.Error != "":
			return truncate(strings.TrimSpace(e.Error + " " + e.Description))
		case e.Detail != nil:
			return truncate(fmt.Sprint(e.Detail))
		}
	}
	return truncate(strings.TrimSpace(string(body)))
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

// scrubURL drops URLs from transport errors; signed URLs must not reach logs.
func scrubURL(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return uerr.Op + ": " + uerr.Err.Error()
	}
	return err.Error()
}

// Do sends a request built by build with the source's bearer token. On a 401
// it drops the cached token and retries once with a fresh one. build must
// return a new request each call (bodies are consumed).
func Do(ctx context.Context, httpClient *http.Client, ts *TokenSource, build func(ctx context.Context) (*http.Request, error)) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		token, err := ts.Token(ctx)
		if err != nil {
			return nil, err
		}
		req, err := build(ctx)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %s", req.Method, req.URL.Path, scrubURL(err))
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
			ts.Invalidate()
			continue
		}
		return resp, nil
	}
}
