package costevents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
)

// Outcome is what one POST /v1/cost-events answer means for the batch.
//
//   - 2xx: every event is stored (created / updated / unchanged).
//   - 400: nothing stored; the per-index errors name the refused events.
//   - 401 twice (a fresh token is tried once): Dhanam refuses the token.
//   - 403: no CostEventWriterGrant binds this client to source pravara.
//   - 409: an event id already belongs to another space.
//   - 429 / 5xx / network / Janua unavailable: transient.
type Outcome string

// Outcomes.
const (
	OutcomeOK           Outcome = "ok"
	OutcomeRejected     Outcome = "rejected"
	OutcomeUnauthorized Outcome = "unauthorized"
	OutcomeForbidden    Outcome = "forbidden"
	OutcomeConflict     Outcome = "conflict"
	OutcomeTransient    Outcome = "transient"
	OutcomeUnexpected   Outcome = "unexpected"
)

// BatchResult is a POST's result. Nothing in it carries a token, a secret
// or an event body.
type BatchResult struct {
	Outcome Outcome
	Status  int
	// Code is a short reason for the whole batch ("http_503", "network").
	Code string
	// IndexErrors (400 only): batch index → error codes (the field each
	// Dhanam message names, never a value).
	IndexErrors map[int][]string
	// Results (2xx only): event id → created | updated | unchanged.
	Results map[string]string
}

// Poster sends one batch.
type Poster interface {
	Post(ctx context.Context, events []json.RawMessage) BatchResult
}

// Client posts batches to Dhanam with a Janua client_credentials token.
type Client struct {
	endpoint string
	http     *http.Client
	tokens   *machineclients.TokenSource
}

// NewClient creates the client. httpClient may be nil.
func NewClient(endpoint string, tokens *machineclients.TokenSource, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	// Never follow redirects: the bearer token must not travel elsewhere.
	c := *httpClient
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{endpoint: endpoint, http: &c, tokens: tokens}
}

var codeChars = regexp.MustCompile(`[^a-z0-9_.]`)

// ErrorCode is the stable part of one Dhanam validation message: the field
// it names ("amount_minor must be …" → "amount_minor"), never a value.
func ErrorCode(message string) string {
	text := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.HasPrefix(text, "unknown field"):
		return "unknown_field"
	case strings.HasPrefix(text, "duplicate"):
		return "duplicate_id"
	case text == "":
		return "unspecified"
	}
	head := strings.SplitN(text, " ", 2)[0]
	code := codeChars.ReplaceAllString(head, "")
	if len(code) > 40 {
		code = code[:40]
	}
	if code == "" {
		return "unspecified"
	}
	return code
}

// Post implements Poster.
func (c *Client) Post(ctx context.Context, events []json.RawMessage) BatchResult {
	if len(events) == 0 || len(events) > MaxBatch {
		return BatchResult{Outcome: OutcomeUnexpected, Code: "batch_size"}
	}
	payload, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		return BatchResult{Outcome: OutcomeUnexpected, Code: "marshal"}
	}
	resp, err := machineclients.Do(ctx, c.http, c.tokens, func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "pravara-api cost-events")
		return req, nil
	})
	if err != nil {
		var te *machineclients.TokenError
		switch {
		case errors.As(err, &te) && te.Retryable():
			return BatchResult{Outcome: OutcomeTransient, Code: "token_unavailable"}
		case errors.As(err, &te):
			return BatchResult{Outcome: OutcomeUnauthorized, Status: te.Status, Code: "token_refused"}
		case errors.Is(err, machineclients.ErrNotConfigured):
			return BatchResult{Outcome: OutcomeUnexpected, Code: "client_not_configured"}
		}
		return BatchResult{Outcome: OutcomeTransient, Code: "network"}
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	status := resp.StatusCode
	code := "http_" + strconv.Itoa(status)
	switch {
	case status >= 200 && status < 300:
		return BatchResult{Outcome: OutcomeOK, Status: status, Code: code, Results: results(body)}
	case status == http.StatusBadRequest:
		return BatchResult{Outcome: OutcomeRejected, Status: status, Code: code, IndexErrors: indexErrors(body)}
	case status == http.StatusUnauthorized:
		return BatchResult{Outcome: OutcomeUnauthorized, Status: status, Code: code}
	case status == http.StatusForbidden:
		return BatchResult{Outcome: OutcomeForbidden, Status: status, Code: code}
	case status == http.StatusConflict:
		return BatchResult{Outcome: OutcomeConflict, Status: status, Code: code}
	case status == http.StatusTooManyRequests || status >= 500:
		return BatchResult{Outcome: OutcomeTransient, Status: status, Code: code}
	}
	return BatchResult{Outcome: OutcomeUnexpected, Status: status, Code: code}
}

func results(body []byte) map[string]string {
	var r struct {
		Results []struct {
			ID      string `json:"id"`
			Outcome string `json:"outcome"`
		} `json:"results"`
	}
	out := map[string]string{}
	if json.Unmarshal(body, &r) == nil {
		for _, x := range r.Results {
			out[x.ID] = x.Outcome
		}
	}
	return out
}

func indexErrors(body []byte) map[int][]string {
	var r struct {
		Errors []struct {
			Index  *int     `json:"index"`
			Errors []string `json:"errors"`
		} `json:"errors"`
	}
	out := map[int][]string{}
	if json.Unmarshal(body, &r) != nil {
		return out
	}
	for _, e := range r.Errors {
		if e.Index == nil || *e.Index < 0 {
			continue
		}
		set := map[string]bool{}
		for _, c := range out[*e.Index] {
			set[c] = true
		}
		for _, m := range e.Errors {
			set[ErrorCode(m)] = true
		}
		if len(set) == 0 {
			set["unspecified"] = true
		}
		codes := make([]string, 0, len(set))
		for c := range set {
			codes = append(codes, c)
		}
		sort.Strings(codes)
		out[*e.Index] = codes
	}
	return out
}
