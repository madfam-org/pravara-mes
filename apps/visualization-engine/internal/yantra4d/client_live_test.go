package yantra4d

// Live checks against Yantra4D's public API. They run only with
// YANTRA4D_LIVE=1, anonymously (guest tier), and make five render calls
// (one answered 404 before Yantra4D's rate limiter counts it), spaced out.
// The guest tier allows 10 renders an hour per address.
//
//	YANTRA4D_LIVE=1 go test ./internal/yantra4d/ -run Live -v -count=1

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The live target: a small OpenSCAD cartridge (the guest tier may not use
// CadQuery) with one mode and one part, rendered at a small size.
const (
	liveProject  = "multiboard"
	liveMode     = "tile"
	livePart     = "tile"
	liveFormat   = "stl" // the only format the guest tier may export
	liveDenied   = "3mf" // needs the essentials tier
	liveSpacing  = 3 * time.Second
	liveNoSuchID = "no-such-project-pravara-live-check"
)

func liveClient(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("YANTRA4D_LIVE") != "1" {
		t.Skip("set YANTRA4D_LIVE=1 to call Yantra4D's public API")
	}
	c, err := NewClient(os.Getenv("YANTRA4D_LIVE_BASE_URL"), 5*time.Minute)
	require.NoError(t, err)
	return c
}

func liveRequest() RenderRequest {
	return RenderRequest{Project: liveProject, Mode: liveMode, ExportFormat: liveFormat,
		Parameters: map[string]any{"x_cells": 2, "y_cells": 2}, IgnoreCache: true}
}

func TestLiveVisualizationClient(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()
	t.Logf("base URL %s", c.BaseURL())

	m, err := c.GetManifest(ctx, liveProject, "")
	require.NoError(t, err)
	mode, ok := m.ModeByID(liveMode)
	require.True(t, ok, "manifest has mode %q", liveMode)
	require.Contains(t, mode.Parts, livePart)
	t.Logf("manifest: %s %s engine=%s mode %s parts=%v", m.Project.Slug, m.Project.Version, m.Project.Engine, mode.ID, mode.Parts)

	first, err := c.Fetch(ctx, liveRequest(), livePart, "")
	require.NoError(t, err, "success path")
	t.Logf("render 1: part %q present, %d bytes, %s, sha256 %s, instance_id %s, variables_sha256 %s, complete=%v, sidecar_sha256 %s, last_modified %s, request_id %s",
		first.Part, len(first.Data), first.MediaType, first.SHA256, first.InstanceID, first.VariablesSHA256, first.Complete,
		first.SidecarSHA256, first.LastModified.Format(time.RFC3339), first.RequestID)

	time.Sleep(liveSpacing)
	second, err := c.Fetch(ctx, liveRequest(), livePart, "")
	require.NoError(t, err, "second identical render")
	t.Logf("render 2: sha256 %s, instance_id %s, last_modified %s", second.SHA256, second.InstanceID, second.LastModified.Format(time.RFC3339))
	assert.Equal(t, first.InstanceID, second.InstanceID, "same inputs, same GOC-1 identity")
	if first.SHA256 == second.SHA256 {
		t.Logf("reproducibility: identical renders gave the same digest")
	} else {
		t.Logf("reproducibility: identical renders gave DIFFERENT digests (%s vs %s)", first.SHA256, second.SHA256)
	}

	time.Sleep(liveSpacing)
	req := liveRequest()
	req.IgnoreCache = false
	_, err = c.Fetch(ctx, req, "no_such_part", "")
	logRefusal(t, "unknown part", err)
	requireCode(t, err, "part_missing")

	time.Sleep(liveSpacing)
	req = liveRequest()
	req.Project = liveNoSuchID
	_, err = c.Fetch(ctx, req, livePart, "")
	logRefusal(t, "unknown project", err)
	requireCode(t, err, "project_not_found")

	time.Sleep(liveSpacing)
	req = liveRequest()
	req.ExportFormat = liveDenied
	req.IgnoreCache = false
	_, err = c.Fetch(ctx, req, livePart, "")
	logRefusal(t, "export format "+liveDenied+" as guest", err)
	var e *Error
	require.True(t, errors.As(err, &e))
	assert.Equal(t, 403, e.Status)
}

func logRefusal(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Logf("%s: NO ERROR (fail-open)", what)
		return
	}
	t.Logf("%s: refused: %v", what, err)
}
