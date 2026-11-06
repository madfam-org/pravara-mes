package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

const (
	testScope  = "pravara-cogs:read"
	testTenant = "11111111-1111-1111-1111-111111111111"
)

type echoResult struct {
	Got map[string]any `json:"got"`
}

func testTools() []Tool {
	return []Tool{
		{Name: "t.echo", Title: "Echo", Description: "echoes", InputSchema: map[string]any{"type": "object"},
			Call: func(_ context.Context, req Request) (any, error) {
				if req.TenantID != testTenant {
					return nil, fmt.Errorf("tenant %q", req.TenantID)
				}
				return echoResult{Got: req.Args}, nil
			}},
		{Name: "t.invalid", InputSchema: map[string]any{"type": "object"},
			Call: func(context.Context, Request) (any, error) { return nil, Invalid("from is required") }},
		{Name: "t.broken", InputSchema: map[string]any{"type": "object"},
			Call: func(context.Context, Request) (any, error) {
				return nil, errors.New("pq: relation does not exist (internal detail)")
			}},
	}
}

type rig struct {
	router *gin.Engine
	server *Server
	logs   *logtest.Hook
	caller *middleware.Caller
}

func newRig(t *testing.T, perMinute, burst int) *rig {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log, hook := logtest.NewNullLogger()
	log.SetLevel(logrus.InfoLevel)
	s, err := NewServer(Config{Name: "pravara-mes", Version: "1.0.0", Instructions: "hi", Scope: testScope,
		RatePerMinute: perMinute, Burst: burst, Log: log}, testTools()...)
	require.NoError(t, err)
	r := &rig{server: s, logs: hook,
		caller: &middleware.Caller{Kind: middleware.CallerServiceAccount, ID: "jnc_agent", Scopes: []string{testScope}}}
	router := gin.New()
	router.POST("/mcp", s.Prelude, func(c *gin.Context) {
		if r.caller != nil {
			c.Set(string(middleware.ContextKeyCaller), *r.caller)
			c.Set(string(middleware.ContextKeyTenantID), testTenant)
		}
		c.Next()
	}, s.Handle)
	router.GET("/mcp", MethodNotAllowed)
	r.router = router
	return r
}

func (r *rig) post(body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	r.router.ServeHTTP(w, req)
	return w
}

type rpcReply struct {
	ID     any            `json:"id"`
	Result map[string]any `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decode(t *testing.T, w *httptest.ResponseRecorder) rpcReply {
	t.Helper()
	var r rpcReply
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &r), w.Body.String())
	return r
}

func call(id int, method, params string) string {
	if params == "" {
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q}`, id, method)
	}
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, id, method, params)
}

func TestInitializeNegotiatesTheProtocolVersion(t *testing.T) {
	r := newRig(t, 600, 100)
	for requested, want := range map[string]string{
		"2025-06-18": "2025-06-18", "2025-03-26": "2025-03-26", "2024-11-05": LatestProtocolVersion, "": LatestProtocolVersion,
	} {
		w := r.post(call(1, "initialize", fmt.Sprintf(`{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"t","version":"1"}}`, requested)))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		res := decode(t, w).Result
		assert.Equal(t, want, res["protocolVersion"], requested)
		assert.Equal(t, map[string]any{"tools": map[string]any{"listChanged": false}}, res["capabilities"])
		assert.Equal(t, "pravara-mes", res["serverInfo"].(map[string]any)["name"])
		assert.Empty(t, w.Header().Get("Mcp-Session-Id"), "stateless: no sessions")
		assert.Empty(t, w.Header().Get("WWW-Authenticate"), "the 401 challenge is cleared once authenticated")
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
}

func TestPingAndToolsList(t *testing.T) {
	r := newRig(t, 600, 100)
	w := r.post(call(2, "ping", ""))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `{"id":2,"jsonrpc":"2.0","result":{}}`, w.Body.String())

	w = r.post(call(3, "tools/list", `{}`))
	require.Equal(t, http.StatusOK, w.Code)
	tools := decode(t, w).Result["tools"].([]any)
	require.Len(t, tools, 3)
	for _, raw := range tools {
		tool := raw.(map[string]any)
		assert.Equal(t, true, tool["annotations"].(map[string]any)["readOnlyHint"], tool["name"])
		assert.NotNil(t, tool["inputSchema"], tool["name"])
	}
}

func TestToolsCallResultsErrorsAndInternalFailures(t *testing.T) {
	r := newRig(t, 600, 100)

	w := r.post(call(4, "tools/call", `{"name":"t.echo","arguments":{"b":2,"a":"x"}}`))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	res := decode(t, w).Result
	assert.Equal(t, false, res["isError"])
	assert.Equal(t, map[string]any{"got": map[string]any{"a": "x", "b": float64(2)}}, res["structuredContent"])
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	assert.JSONEq(t, `{"got":{"a":"x","b":2}}`, text)

	w = r.post(call(5, "tools/call", `{"name":"t.invalid","arguments":{}}`))
	require.Equal(t, http.StatusOK, w.Code)
	res = decode(t, w).Result
	assert.Equal(t, true, res["isError"], "argument errors are tool results the model can correct")
	assert.Equal(t, "from is required", res["content"].([]any)[0].(map[string]any)["text"])

	w = r.post(call(6, "tools/call", `{"name":"t.broken"}`))
	assert.Equal(t, http.StatusInternalServerError, w.Code, "500 rolls the tenant transaction back")
	reply := decode(t, w)
	require.NotNil(t, reply.Error)
	assert.Equal(t, CodeInternalError, reply.Error.Code)
	assert.NotContains(t, w.Body.String(), "relation", "internal detail stays in the log")

	w = r.post(call(7, "tools/call", `{"name":"t.missing"}`))
	assert.Equal(t, CodeInvalidParams, decode(t, w).Error.Code)
	w = r.post(call(8, "tools/call", `{"name":"t.echo","arguments":[1]}`))
	assert.Equal(t, CodeInvalidParams, decode(t, w).Error.Code)
	w = r.post(call(9, "resources/list", ""))
	assert.Equal(t, CodeMethodNotFound, decode(t, w).Error.Code)
}

func TestMalformedMessages(t *testing.T) {
	r := newRig(t, 600, 100)
	cases := []struct {
		name, body string
		headers    []string
		status     int
		code       int
	}{
		{"parse error", `{"jsonrpc":`, nil, http.StatusBadRequest, CodeParseError},
		{"batch", `[` + call(1, "ping", "") + `]`, nil, http.StatusBadRequest, CodeInvalidRequest},
		{"wrong version", `{"jsonrpc":"1.0","id":1,"method":"ping"}`, nil, http.StatusBadRequest, CodeInvalidRequest},
		{"null id", `{"jsonrpc":"2.0","id":null,"method":"ping"}`, nil, http.StatusBadRequest, CodeInvalidRequest},
		{"object id", `{"jsonrpc":"2.0","id":{},"method":"ping"}`, nil, http.StatusBadRequest, CodeInvalidRequest},
		{"no method", `{"jsonrpc":"2.0","id":1}`, nil, http.StatusBadRequest, CodeInvalidRequest},
		{"media type", call(1, "ping", ""), []string{"Content-Type", "text/plain"}, http.StatusUnsupportedMediaType, CodeInvalidRequest},
		{"protocol version", call(1, "ping", ""), []string{"MCP-Protocol-Version", "1999-01-01"}, http.StatusBadRequest, CodeInvalidRequest},
		{"too large", `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("x", MaxBodyBytes) + `"}}`,
			nil, http.StatusRequestEntityTooLarge, CodeInvalidRequest},
	}
	for _, tc := range cases {
		w := r.post(tc.body, tc.headers...)
		assert.Equal(t, tc.status, w.Code, tc.name)
		reply := decode(t, w)
		require.NotNil(t, reply.Error, tc.name)
		assert.Equal(t, tc.code, reply.Error.Code, tc.name)
	}

	w := r.post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Empty(t, w.Body.String())
	w = r.post(call(1, "ping", ""), "Content-Type", "application/json; charset=utf-8", "MCP-Protocol-Version", "2025-06-18")
	assert.Equal(t, http.StatusOK, w.Code)

	g := httptest.NewRecorder()
	r.router.ServeHTTP(g, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, g.Code)
	assert.Equal(t, http.MethodPost, g.Header().Get("Allow"))
}

func TestGatesBeforeTheMessage(t *testing.T) {
	r := newRig(t, 600, 100)

	w := r.post(call(1, "ping", ""), "Origin", "https://evil.example")
	assert.Equal(t, http.StatusForbidden, w.Code, "browser origins are refused")

	r.caller = &middleware.Caller{Kind: middleware.CallerUser, ID: "00000000-0000-0000-0000-0000000000aa"}
	w = r.post(call(1, "ping", ""))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "machine_token_required")

	r.caller = &middleware.Caller{Kind: middleware.CallerServiceAccount, ID: "jnc_other", Scopes: []string{"pravara-mes:read"}}
	w = r.post(call(1, "ping", ""))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, `Bearer error="insufficient_scope", scope="pravara-cogs:read"`, w.Header().Get("WWW-Authenticate"))

	r.caller = &middleware.Caller{Kind: middleware.CallerAPIKey, ID: "k", Scopes: []string{"*", testScope}}
	w = r.post(call(1, "ping", ""))
	assert.Equal(t, http.StatusForbidden, w.Code, "API keys are not Janua machine tokens")

	r.caller = nil
	w = r.post(call(1, "ping", ""))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, Challenge, w.Header().Get("WWW-Authenticate"))
}

func TestRateLimitIsPerClient(t *testing.T) {
	r := newRig(t, 1, 2)
	start := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	r.server.now = func() time.Time { return start }

	for i := 0; i < 2; i++ {
		assert.Equal(t, http.StatusOK, r.post(call(i, "ping", "")).Code)
	}
	w := r.post(call(3, "ping", ""))
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "60", w.Header().Get("Retry-After"))
	assert.Equal(t, CodeRateLimited, decode(t, w).Error.Code)

	r.caller = &middleware.Caller{Kind: middleware.CallerServiceAccount, ID: "jnc_second", Scopes: []string{testScope}}
	assert.Equal(t, http.StatusOK, r.post(call(4, "ping", "")).Code, "another client has its own bucket")

	r.server.now = func() time.Time { return start.Add(61 * time.Second) }
	r.caller = &middleware.Caller{Kind: middleware.CallerServiceAccount, ID: "jnc_agent", Scopes: []string{testScope}}
	assert.Equal(t, http.StatusOK, r.post(call(5, "ping", "")).Code, "the bucket refills")
}

func TestOneAuditLinePerCallWithTheArgumentsHashedNeverLogged(t *testing.T) {
	r := newRig(t, 600, 100)
	const marker = "order-7f3a-do-not-log"

	r.post(call(1, "tools/call", fmt.Sprintf(`{"name":"t.echo","arguments":{"order_id":%q,"limit":5}}`, marker)))
	r.post(call(2, "tools/call", fmt.Sprintf(`{"name":"t.echo","arguments":{"limit":5,"order_id":%q}}`, marker)))
	r.post(call(3, "tools/list", ""))

	entries := r.logs.AllEntries()
	require.Len(t, entries, 3, "one line per call")
	for _, e := range entries {
		assert.Equal(t, AuditMessage, e.Message)
		assert.Equal(t, "jnc_agent", e.Data["mcp_client_id"])
		assert.Equal(t, testTenant, e.Data["tenant_id"])
		assert.Equal(t, "ok", e.Data["outcome"])
		assert.Contains(t, e.Data, "latency_ms")
		for k, v := range e.Data {
			assert.NotContains(t, fmt.Sprint(v), marker, "field %s carries an argument", k)
		}
	}
	assert.Equal(t, "t.echo", entries[0].Data["tool"])
	digest := entries[0].Data["args_sha256"].(string)
	assert.Len(t, digest, 64)
	assert.Equal(t, digest, entries[1].Data["args_sha256"], "the digest ignores key order")
	assert.NotContains(t, entries[2].Data, "args_sha256")

	r.logs.Reset()
	r.caller = &middleware.Caller{Kind: middleware.CallerServiceAccount, ID: "jnc_other", Scopes: nil}
	r.post(call(4, "ping", ""))
	require.Len(t, r.logs.AllEntries(), 1)
	assert.Equal(t, "denied_scope", r.logs.LastEntry().Data["outcome"])
	assert.Equal(t, "jnc_other", r.logs.LastEntry().Data["mcp_client_id"])
}

func TestNewServerRejectsBadToolSets(t *testing.T) {
	tool := Tool{Name: "a", InputSchema: map[string]any{}, Call: func(context.Context, Request) (any, error) { return nil, nil }}
	_, err := NewServer(Config{Scope: testScope}, tool, tool)
	assert.Error(t, err, "duplicate names")
	_, err = NewServer(Config{}, tool)
	assert.Error(t, err, "no scope")
	_, err = NewServer(Config{Scope: testScope}, Tool{Name: "b"})
	assert.Error(t, err, "no Call or schema")
}

func TestRequestArgumentHelpers(t *testing.T) {
	req := Request{Args: map[string]any{"s": "x", "n": json.Number("12"), "f": json.Number("1.5"), "bad": true, "nil": nil}}
	assert.Error(t, req.OnlyArgs("s"))
	assert.NoError(t, Request{Args: map[string]any{"s": "x"}}.OnlyArgs("s", "n"))

	v, ok, err := req.String("s")
	assert.Equal(t, []any{"x", true, nil}, []any{v, ok, err})
	_, ok, err = req.String("nil")
	assert.False(t, ok)
	assert.NoError(t, err)
	_, _, err = req.String("bad")
	assert.Error(t, err)

	v, ok, err = req.Integer("n")
	assert.Equal(t, []any{"12", true, nil}, []any{v, ok, err})
	_, _, err = req.Integer("f")
	assert.Error(t, err)
	_, _, err = req.Integer("s")
	assert.Error(t, err)
}
