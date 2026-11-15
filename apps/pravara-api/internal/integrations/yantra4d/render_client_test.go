package yantra4d

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
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
	assert.True(t, strings.HasPrefix(c.AbsoluteURL(part.URL), y.Server.URL+"/static/"))
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
	assert.Contains(t, re.Message, "Render timed out after 120s")

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
