package yantra4d

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatchfakes"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/remote"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
)

const tree = "9de820d73e02774a8237748189ef4c3171de989d0009c2ca9020d9e6301349d7"

func setup(t *testing.T, audience string) (*Client, *dispatchfakes.Yantra4D) {
	janua := dispatchfakes.NewJanua(t, dispatchfakes.Client{ID: "jnc_y", Secret: "jns_y", Scope: machineclients.Yantra4DScope, Audience: audience})
	y := dispatchfakes.NewYantra4D(t, "motor-soft-mount", "assembled", "body", tree)
	ts := machineclients.NewTokenSource(machineclients.Credentials{Name: "y", TokenURL: janua.TokenURL(),
		ClientID: "jnc_y", ClientSecret: "jns_y", Scope: machineclients.Yantra4DScope}, nil)
	c, err := NewClient(y.Server.URL, ts, nil)
	require.NoError(t, err)
	return c, y
}

func TestRenderAndSidecarWithTheMachineClient(t *testing.T) {
	c, y := setup(t, "yantra4d-api")
	resp, err := c.Render(context.Background(), RenderRequest{Project: "motor-soft-mount", Mode: "assembled",
		Parameters: map[string]any{"size": 40}, ExportFormat: "3mf"})
	require.NoError(t, err)
	require.Len(t, resp.Parts, 1)
	assert.Equal(t, map[string]any{"size": float64(40)}, y.Requests[0]["parameters"], "parameters are nested (documented contract)")
	part := resp.Parts[0]
	assert.Equal(t, "model/3mf", part.MediaType)

	doc, sha, err := c.FetchSidecar(context.Background(), part)
	require.NoError(t, err)
	assert.Equal(t, dispatchfakes.Digest(y.Sidecar), sha)
	assert.Equal(t, tree, doc.Generator.Source.TreeSHA256)
	abs, err := c.OriginURL(part.URL)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(abs, y.Server.URL+"/static/"))
}

func TestWrongAudienceIsRejected(t *testing.T) {
	c, _ := setup(t, "pravara-api")
	_, err := c.Render(context.Background(), RenderRequest{Project: "p", Mode: "m", ExportFormat: "3mf"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestSidecarFailsClosed(t *testing.T) {
	c, _ := setup(t, "yantra4d-api")
	_, _, err := c.FetchSidecar(context.Background(), Part{Type: "body"})
	var re *remote.Error
	require.ErrorAs(t, err, &re)
	assert.Equal(t, "sidecar_missing", re.Code)
	assert.False(t, re.Retryable)

	good := func() *Sidecar {
		var s Sidecar
		raw := `{"format":"hyperobjects.generator-output","format_version":"1.0.2","instance_id":"` + strings.Repeat("a", 64) +
			`","variables_sha256":"` + strings.Repeat("b", 64) + `","generator":{"source":{"tree_sha256":"` + tree +
			`"}},"geometry":[{"sha256":"` + strings.Repeat("c", 64) + `","role":"primary"},{"sha256":"` + strings.Repeat("d", 64) + `","role":"viewer"}]}`
		require.NoError(t, json.Unmarshal([]byte(raw), &s))
		return &s
	}
	part := Part{SHA256: strings.Repeat("c", 64), InstanceID: strings.Repeat("a", 64)}
	assert.NoError(t, CheckSidecar(good(), part))
	assert.Error(t, CheckSidecar(good(), Part{SHA256: strings.Repeat("d", 64)}), "viewer digest is not the primary")
	assert.Error(t, CheckSidecar(good(), Part{SHA256: part.SHA256, InstanceID: strings.Repeat("e", 64)}))
	bad := good()
	bad.Format = "other"
	assert.Error(t, CheckSidecar(bad, part))
	bad = good()
	bad.Generator.Source.TreeSHA256 = "short"
	assert.Error(t, CheckSidecar(bad, part))
}

// yantra4d answers 200 "success" with the failed part simply absent. The
// client reports that as a retryable failure carrying yantra4d's reason.
func TestRenderWithNoPartsIsARetryableFailure(t *testing.T) {
	c, y := setup(t, "yantra4d-api")
	y.FailingRenders = 1
	_, err := c.Render(context.Background(), RenderRequest{Project: "motor-soft-mount", Mode: "assembled",
		Parameters: map[string]any{"size": 40}, ExportFormat: "3mf"})
	var re *remote.Error
	require.ErrorAs(t, err, &re)
	assert.True(t, re.Retryable)
	assert.Contains(t, re.Message, "render returned no parts")
	assert.Contains(t, re.Message, "ERROR: Render timed out while waiting for completion")

	// The next attempt succeeds.
	resp, err := c.Render(context.Background(), RenderRequest{Project: "motor-soft-mount", Mode: "assembled",
		Parameters: map[string]any{"size": 40}, ExportFormat: "3mf"})
	require.NoError(t, err)
	assert.Len(t, resp.Parts, 1)
}

func TestLogExcerpt(t *testing.T) {
	assert.Equal(t, "", LogExcerpt(""))
	assert.Equal(t, "", LogExcerpt("  \n "))
	assert.Equal(t, ": queued [body] Error: boom", LogExcerpt("queued\n[body]   Error: boom\n"))

	long := strings.Repeat("x", 500) + " the end"
	got := LogExcerpt(long)
	assert.True(t, strings.HasPrefix(got, ": …"))
	assert.True(t, strings.HasSuffix(got, "the end"), "the end of the log is kept: that is where a failure is reported")
	assert.LessOrEqual(t, len([]rune(got)), 303)
}

// The excerpt is stored in a PostgreSQL text column and returned by the API,
// and it is another service's output.
func TestLogExcerptIsSafeToStore(t *testing.T) {
	// Cut on a character boundary: 400 two-byte characters, kept tail of 300.
	got := LogExcerpt(strings.Repeat("ñ", 400))
	assert.True(t, utf8.ValidString(got), "never split a multi-byte character")
	assert.Equal(t, ": …"+strings.Repeat("ñ", 300), got)

	// Invalid UTF-8 and control characters are dropped; NUL would be refused by PostgreSQL.
	got = LogExcerpt("ok\x00 \xff\xfe bad \x1b[31mred\x1b[0m \u0007 end")
	assert.True(t, utf8.ValidString(got))
	assert.NotContains(t, got, "\x00")
	assert.NotContains(t, got, "\x1b")
	assert.Equal(t, ": ok bad [31mred[0m end", got)

	// Only junk: nothing to say.
	assert.Equal(t, "", LogExcerpt("\x00\xff\x01"))
}

// On a cache hit yantra4d serves an artifact made earlier; its Last-Modified
// says when. The sidecar fetch reports it so dispatch can judge freshness by
// the artifact, not by the request.
func TestFetchSidecarReportsWhenTheArtifactWasMade(t *testing.T) {
	c, y := setup(t, "yantra4d-api")
	resp, err := c.Render(context.Background(), RenderRequest{Project: "motor-soft-mount", Mode: "assembled",
		Parameters: map[string]any{"size": 40}, ExportFormat: "3mf"})
	require.NoError(t, err)

	doc, _, err := c.FetchSidecar(context.Background(), resp.Parts[0])
	require.NoError(t, err)
	assert.True(t, doc.ArtifactModified.IsZero(), "unknown when yantra4d sends no Last-Modified")

	made := time.Date(2026, 11, 13, 8, 30, 0, 0, time.UTC)
	y.Set(func(y *dispatchfakes.Yantra4D) { y.ArtifactModified = made })
	doc, _, err = c.FetchSidecar(context.Background(), resp.Parts[0])
	require.NoError(t, err)
	assert.True(t, doc.ArtifactModified.Equal(made))
}

func TestRenderRequestCarriesIgnoreCacheOnlyWhenAsked(t *testing.T) {
	c, y := setup(t, "yantra4d-api")
	req := RenderRequest{Project: "motor-soft-mount", Mode: "assembled", Parameters: map[string]any{"size": 40}, ExportFormat: "3mf"}
	_, err := c.Render(context.Background(), req)
	require.NoError(t, err)
	_, sent := y.Requests[0]["ignore_cache"]
	assert.False(t, sent, "an ordinary render may be answered from yantra4d's cache")

	req.IgnoreCache = true
	_, err = c.Render(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, true, y.Requests[1]["ignore_cache"])
}

// elsewhere is another host that must never be reached: it records every
// request and whether it carried an Authorization header.
type elsewhere struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests int
	bearers  int
}

func newElsewhere(t *testing.T) *elsewhere {
	e := &elsewhere{}
	e.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.requests++
		if r.Header.Get("Authorization") != "" {
			e.bearers++
		}
		_, _ = w.Write([]byte("PK\x03\x04 from elsewhere"))
	}))
	t.Cleanup(e.server.Close)
	return e
}

func (e *elsewhere) untouched(t *testing.T) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	assert.Zero(t, e.requests, "no request reached the other host")
	assert.Zero(t, e.bearers, "no bearer left for the other host")
}

func offOrigin(t *testing.T, err error) {
	t.Helper()
	var re *remote.Error
	require.ErrorAs(t, err, &re)
	assert.Equal(t, CodeOffOrigin, re.Code)
	assert.False(t, remote.IsRetryable(err))
}

func TestURLsFromAnAnswerStayOnYantra4DsOrigin(t *testing.T) {
	c, y := setup(t, "yantra4d-api")
	evil := newElsewhere(t)
	evilHost := strings.TrimPrefix(evil.server.URL, "http://")
	ctx := context.Background()

	t.Run("off-origin part URL", func(t *testing.T) {
		y.Set(func(y *dispatchfakes.Yantra4D) { y.PartURL = evil.server.URL + "/static/x.3mf" })
		defer y.Set(func(y *dispatchfakes.Yantra4D) { y.PartURL = "" })
		resp, err := c.Render(ctx, RenderRequest{Project: "motor-soft-mount", Mode: "assembled", ExportFormat: "3mf"})
		require.NoError(t, err)
		_, err = c.OriginURL(resp.Parts[0].URL)
		offOrigin(t, err)
		_, _, err = c.OpenArtifact(ctx, resp.Parts[0].URL)
		offOrigin(t, err)
		evil.untouched(t)
	})

	t.Run("off-origin sidecar URL", func(t *testing.T) {
		y.Set(func(y *dispatchfakes.Yantra4D) { y.VariablesURL = evil.server.URL + "/static/x.3mf.variables.json" })
		defer y.Set(func(y *dispatchfakes.Yantra4D) { y.VariablesURL = "" })
		resp, err := c.Render(ctx, RenderRequest{Project: "motor-soft-mount", Mode: "assembled", ExportFormat: "3mf"})
		require.NoError(t, err)
		_, _, err = c.FetchSidecar(ctx, resp.Parts[0])
		offOrigin(t, err)
		_, _, err = c.OpenArtifact(ctx, resp.Parts[0].VariablesURL)
		offOrigin(t, err)
		evil.untouched(t)
	})

	t.Run("protocol-relative URL", func(t *testing.T) {
		for _, ref := range []string{"//" + evilHost + "/static/x.3mf", "///" + evilHost + "/x", `\\` + evilHost + "/x"} {
			got, err := c.OriginURL(ref)
			if err == nil {
				// A path that merely contains the other host's name is
				// fine, as long as it resolves on yantra4d itself.
				assert.True(t, strings.HasPrefix(got, y.Server.URL+"/"), "%q resolved to %q", ref, got)
				continue
			}
			offOrigin(t, err)
		}
		_, err := c.OriginURL("//" + evilHost + "/static/x.3mf")
		offOrigin(t, err)
		_, _, err = c.OpenArtifact(ctx, "//"+evilHost+"/static/x.3mf")
		offOrigin(t, err)
		evil.untouched(t)
	})

	t.Run("credentials, other ports, empty", func(t *testing.T) {
		port := y.Server.URL[strings.LastIndex(y.Server.URL, ":")+1:]
		for _, ref := range []string{"http://user:pw@127.0.0.1:" + port + "/x", "http://127.0.0.1:1/x", ""} {
			_, err := c.OriginURL(ref)
			offOrigin(t, err)
		}
		got, err := c.OriginURL(y.Server.URL + "/static/a.3mf")
		require.NoError(t, err, "an absolute URL on the origin itself is fine")
		assert.Equal(t, y.Server.URL+"/static/a.3mf", got)
	})

	t.Run("redirect on download is not followed", func(t *testing.T) {
		resp, err := c.Render(ctx, RenderRequest{Project: "motor-soft-mount", Mode: "assembled", ExportFormat: "3mf"})
		require.NoError(t, err)
		y.Set(func(y *dispatchfakes.Yantra4D) { y.RedirectTo = evil.server.URL + "/static/x.3mf" })
		defer y.Set(func(y *dispatchfakes.Yantra4D) { y.RedirectTo = "" })
		_, _, err = c.OpenArtifact(ctx, resp.Parts[0].URL)
		var re *remote.Error
		require.ErrorAs(t, err, &re)
		assert.Equal(t, "redirect_refused", re.Code)
		assert.Equal(t, http.StatusFound, re.Status)
		assert.False(t, remote.IsRetryable(err))
		_, _, err = c.FetchSidecar(ctx, resp.Parts[0])
		require.ErrorAs(t, err, &re)
		assert.Equal(t, "redirect_refused", re.Code)
		evil.untouched(t)
	})
}

// The client sends a machine bearer with every call: never in clear text,
// except to a loopback test server.
func TestNewClientRequiresHTTPS(t *testing.T) {
	for _, ok := range []string{"https://api.yantra4d.example", "http://127.0.0.1:9", "http://localhost:9", "http://[::1]:9"} {
		_, err := NewClient(ok, nil, nil)
		assert.NoError(t, err, ok)
	}
	for _, bad := range []string{"http://api.yantra4d.example", "ftp://api.yantra4d.example", "api.yantra4d.example",
		"https://user:pw@api.yantra4d.example", ""} {
		_, err := NewClient(bad, nil, nil)
		assert.Error(t, err, bad)
	}

	// Plain http on an https origin is a downgrade, refused.
	c, err := NewClient("https://api.yantra4d.example", nil, nil)
	require.NoError(t, err)
	_, err = c.OriginURL("http://api.yantra4d.example/static/a.3mf")
	offOrigin(t, err)
	got, err := c.OriginURL("/static/a.3mf")
	require.NoError(t, err)
	assert.Equal(t, "https://api.yantra4d.example/static/a.3mf", got)
}
