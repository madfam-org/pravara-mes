package yantra4d

// Live checks of the dispatch render client against Yantra4D's public API.
// They run only with YANTRA4D_LIVE=1 and call Yantra4D anonymously (guest
// tier): the machine token comes from a local fake Janua and the test
// transport strips it before the request leaves, so production's
// authenticated path (yantra4d:render scope, 3mf) is NOT exercised here.
// Three render calls (one more is answered 404 before Yantra4D's rate limiter
// counts it), spaced out; the guest tier allows 10 an hour per address.
//
//	YANTRA4D_LIVE=1 go test ./internal/integrations/yantra4d/ -run Live -v -count=1
//
// See README.md in this directory.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatchfakes"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/remote"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
)

const (
	liveProject = "multiboard" // small OpenSCAD cartridge (guest may not use CadQuery)
	liveMode    = "tile"
	livePart    = "tile"
	liveFormat  = "stl" // the only format the guest tier may export
	liveDenied  = "3mf" // what dispatch asks for; needs the essentials tier
	liveSpacing = 3 * time.Second
)

// anonymous drops the Authorization header: the bearer from the fake Janua
// never reaches Yantra4D.
type anonymous struct{ next http.RoundTripper }

func (a anonymous) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Del("Authorization")
	return a.next.RoundTrip(r)
}

func liveRenderClient(t *testing.T) (*Client, *http.Client, string) {
	t.Helper()
	if os.Getenv("YANTRA4D_LIVE") != "1" {
		t.Skip("set YANTRA4D_LIVE=1 to call Yantra4D's public API")
	}
	base := os.Getenv("YANTRA4D_LIVE_BASE_URL")
	if base == "" {
		base = "https://api.yantra4d.com"
	}
	janua := dispatchfakes.NewJanua(t, dispatchfakes.Client{ID: "jnc_live", Secret: "jns_live", Scope: machineclients.Yantra4DScope, Audience: "yantra4d-api"})
	ts := machineclients.NewTokenSource(machineclients.Credentials{Name: "live", TokenURL: janua.TokenURL(),
		ClientID: "jnc_live", ClientSecret: "jns_live", Scope: machineclients.Yantra4DScope}, nil)
	httpClient := &http.Client{Timeout: 5 * time.Minute, Transport: anonymous{next: http.DefaultTransport}}
	c, err := NewClient(base, ts, httpClient)
	require.NoError(t, err)
	return c, httpClient, base
}

func liveRender(t *testing.T, c *Client, httpClient *http.Client, base string) (*RenderResponse, Part, *Sidecar) {
	t.Helper()
	ctx := context.Background()
	resp, err := c.Render(ctx, RenderRequest{Project: liveProject, Mode: liveMode, ExportFormat: liveFormat,
		Parameters: map[string]any{"x_cells": 2, "y_cells": 2}, IgnoreCache: true})
	require.NoError(t, err)
	require.Equal(t, "success", resp.Status)
	var part *Part
	for i := range resp.Parts {
		if resp.Parts[i].Type == livePart {
			part = &resp.Parts[i]
		}
	}
	require.NotNil(t, part, "part %q present (log: %s)", livePart, LogExcerpt(resp.Log))

	// The artifact and sidecar must be on the configured origin.
	want, _ := url.Parse(base)
	for _, ref := range []string{part.URL, part.VariablesURL} {
		u, err := url.Parse(c.AbsoluteURL(ref))
		require.NoError(t, err)
		assert.Equal(t, want.Scheme, u.Scheme, ref)
		assert.Equal(t, want.Host, u.Host, ref)
	}

	// Download the bytes the slicer would fetch and check them.
	r, err := httpClient.Get(c.AbsoluteURL(part.URL))
	require.NoError(t, err)
	data, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, r.StatusCode)
	sum := sha256.Sum256(data)
	assert.Equal(t, part.SHA256, hex.EncodeToString(sum[:]), "downloaded bytes match the answer's sha256")

	doc, sidecarSHA, err := c.FetchSidecar(ctx, *part)
	require.NoError(t, err, "GOC-1 sidecar checks out (CheckSidecar)")
	assert.Equal(t, part.InstanceID, doc.InstanceID)
	require.NotNil(t, resp.GeneratorOutput)
	assert.Equal(t, resp.GeneratorOutput.VariablesSHA256, doc.VariablesSHA256)
	t.Logf("status %s, part %q present, %d bytes %s, sha256 %s, instance_id %s, variables_sha256 %s, complete=%v, sidecar_sha256 %s, tree_sha256 %s, artifact Last-Modified %s, request_id %s",
		resp.Status, part.Type, len(data), part.MediaType, part.SHA256, doc.InstanceID, doc.VariablesSHA256, doc.Complete,
		sidecarSHA, doc.Generator.Source.TreeSHA256, r.Header.Get("Last-Modified"), resp.RequestID)
	return resp, *part, doc
}

func TestLiveDispatchRenderClient(t *testing.T) {
	c, httpClient, base := liveRenderClient(t)
	ctx := context.Background()

	first, firstPart, firstDoc := liveRender(t, c, httpClient, base)
	time.Sleep(liveSpacing)
	_, secondPart, secondDoc := liveRender(t, c, httpClient, base)
	assert.Equal(t, firstDoc.InstanceID, secondDoc.InstanceID, "same inputs, same GOC-1 identity")
	if firstPart.SHA256 == secondPart.SHA256 {
		t.Logf("reproducibility: identical renders gave the same digest")
	} else {
		t.Logf("reproducibility: identical renders gave DIFFERENT digests (%s vs %s)", firstPart.SHA256, secondPart.SHA256)
	}

	// Unknown part: the render client returns every part of the mode; picking
	// one is dispatch's pickPart, which refuses a part that is absent and not
	// reported failed (terminal render_part_missing). Here: it is absent and
	// the log does not report it.
	for _, p := range first.Parts {
		assert.NotEqual(t, "no_such_part", p.Type)
	}
	assert.NotContains(t, first.Log, "[no_such_part] ERROR:")
	t.Logf("unknown part: absent from the answer (parts %d), not reported in the log → pickPart refuses it", len(first.Parts))

	time.Sleep(liveSpacing)
	_, err := c.Render(ctx, RenderRequest{Project: "no-such-project-pravara-live-check", Mode: liveMode, ExportFormat: liveFormat})
	t.Logf("unknown project: %v", err)
	var re *remote.Error
	require.True(t, errors.As(err, &re), "unknown project is an error")
	assert.Equal(t, http.StatusNotFound, re.Status)

	time.Sleep(liveSpacing)
	_, err = c.Render(ctx, RenderRequest{Project: liveProject, Mode: liveMode, ExportFormat: liveDenied,
		Parameters: map[string]any{"x_cells": 2, "y_cells": 2}})
	t.Logf("export format %s as guest: %v", liveDenied, err)
	require.True(t, errors.As(err, &re), "a format the tier may not export is an error")
	assert.Equal(t, http.StatusForbidden, re.Status)
}
