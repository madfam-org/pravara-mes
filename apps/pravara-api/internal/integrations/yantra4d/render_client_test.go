package yantra4d

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
