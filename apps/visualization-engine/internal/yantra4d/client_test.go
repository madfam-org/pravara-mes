package yantra4d

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testTree = "9de820d73e02774a8237748189ef4c3171de989d0009c2ca9020d9e6301349d7"
	testVars = "5f0c1a8e2b3d4c6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6"
)

// fakeYantra4D answers like Yantra4D's current API for project "multiboard",
// mode "tile", part "tile". Each test mutates the answer to break one thing.
type fakeYantra4D struct {
	t      *testing.T
	Server *httptest.Server

	mu       sync.Mutex
	Requests []map[string]any
	Auth     []string

	Artifact []byte
	// Envelope and Sidecar are built by reset and may be edited by a test.
	Envelope map[string]any
	Sidecar  map[string]any
	// Served overrides the bytes served for the artifact (digest mismatch).
	Served      []byte
	RenderCode  int
	RenderBody  string
	RedirectTo  string
	SidecarBody []byte
}

func newFake(t *testing.T) *fakeYantra4D {
	f := &fakeYantra4D{t: t, Artifact: []byte("solid tile\nendsolid tile\n")}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Server.Close)
	f.reset()
	return f
}

func digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func (f *fakeYantra4D) reset() {
	part := "tile"
	id, err := InstanceID("multiboard", "tile", &part, testTree, testVars)
	require.NoError(f.t, err)
	sha := digest(f.Artifact)
	f.Sidecar = map[string]any{
		"format": GOC1Format, "format_version": "1.0.0", "kind": "solid",
		"instance_id": id, "variables_sha256": testVars, "complete": true,
		"generator": map[string]any{"platform": "yantra4d", "cartridge": "multiboard", "mode": "tile", "part": "tile",
			"engine": "openscad", "source": map[string]any{"tree_sha256": testTree}},
		"geometry": []any{
			map[string]any{"path": "multiboard_preview_abc_tile.stl", "media_type": "model/stl", "sha256": sha, "bytes": len(f.Artifact), "role": "primary"},
			map[string]any{"path": "multiboard_preview_abc_tile.glb", "media_type": "model/gltf-binary", "sha256": strings.Repeat("d", 64), "bytes": 10, "role": "viewer"},
		},
	}
	f.Envelope = map[string]any{
		"status": "success",
		"parts": []any{map[string]any{
			"type": "tile", "url": "/static/multiboard_preview_abc_tile.stl", "size_bytes": len(f.Artifact),
			"sha256": sha, "media_type": "model/stl", "instance_id": id,
			"variables_url": "/static/multiboard_preview_abc_tile.stl.variables.json",
		}},
		"log":              "[tile] rendered\n",
		"request_id":       "req-1",
		"generator_output": map[string]any{"format_version": "1.0.0", "complete": true, "variables_sha256": testVars},
	}
}

func (f *fakeYantra4D) part() map[string]any {
	return f.Envelope["parts"].([]any)[0].(map[string]any)
}

func (f *fakeYantra4D) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Auth = append(f.Auth, r.Header.Get("Authorization"))
	if f.RedirectTo != "" {
		http.Redirect(w, r, f.RedirectTo, http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/projects/multiboard/manifest":
		_, _ = io.WriteString(w, currentManifest)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/projects/"):
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"status":"error","error":"Project not found","error_code":"project_not_found"}`)
	case r.Method == http.MethodPost && r.URL.Path == "/api/render":
		var body map[string]any
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		f.Requests = append(f.Requests, body)
		if f.RenderCode != 0 {
			w.WriteHeader(f.RenderCode)
			_, _ = io.WriteString(w, f.RenderBody)
			return
		}
		_ = json.NewEncoder(w).Encode(f.Envelope)
	case r.URL.Path == "/static/multiboard_preview_abc_tile.stl":
		w.Header().Set("Content-Type", "model/stl")
		w.Header().Set("Last-Modified", "Mon, 16 Nov 2026 10:00:00 GMT")
		if f.Served != nil {
			_, _ = w.Write(f.Served)
			return
		}
		_, _ = w.Write(f.Artifact)
	case r.URL.Path == "/static/multiboard_preview_abc_tile.stl.variables.json":
		if f.SidecarBody != nil {
			_, _ = w.Write(f.SidecarBody)
			return
		}
		_ = json.NewEncoder(w).Encode(f.Sidecar)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"status":"error","error":"Not found"}`)
	}
}

// currentManifest is shaped like a live Yantra4D manifest (2026-11-16):
// select options are objects and verification is nested under stages.
const currentManifest = `{
 "project": {"name": "Multiboard Tile", "slug": "multiboard", "version": "1.0.0", "engine": "openscad", "force_backend": true,
  "description": {"en": "x"}, "attribution": {"license": "CERN-OHL-W-2.0"}},
 "modes": [{"id": "tile", "label": {"en": "Tile"}, "scad_file": "tile.scad", "parts": ["tile"],
  "estimate": {"base_units": "x_cells*y_cells", "formula": "grid"}}],
 "parts": [{"id": "tile", "label": {"en": "Tile"}, "color": "#888888", "render_mode": 0}],
 "parameters": [
  {"id": "x_cells", "type": "slider", "default": 4, "min": 1, "max": 12},
  {"id": "style", "type": "select", "default": "plain", "options": [{"label": {"en": "Plain"}, "value": "plain"}]}],
 "verification": {"stages": {"geometry": {"checks": {"watertight": {"enabled": true}}}}},
 "export_formats": ["stl", "3mf", "off", "glb", "obj"],
 "camera_views": [{"id": "iso", "label": {"en": "Iso"}, "position": [1, 2, 3]}]
}`

func newTestClient(t *testing.T, f *fakeYantra4D) *Client {
	c, err := NewClient(f.Server.URL, 10*time.Second)
	require.NoError(t, err)
	return c
}

func tileRequest() RenderRequest {
	return RenderRequest{Project: "multiboard", Mode: "tile", Parameters: map[string]any{"x_cells": 2}, ExportFormat: "stl"}
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var e *Error
	require.True(t, errors.As(err, &e), "want *yantra4d.Error, got %T: %v", err, err)
	assert.Equal(t, code, e.Code, err.Error())
}

func TestNewClientBaseURL(t *testing.T) {
	c, err := NewClient("", time.Second)
	require.NoError(t, err)
	assert.Equal(t, DefaultBaseURL, c.BaseURL())

	c, err = NewClient("https://api.yantra4d.com/", time.Second)
	require.NoError(t, err)
	assert.Equal(t, "https://api.yantra4d.com", c.BaseURL())

	for _, ok := range []string{"http://127.0.0.1:8080", "http://localhost:5000", "http://[::1]:9"} {
		_, err := NewClient(ok, time.Second)
		assert.NoError(t, err, ok)
	}
	for _, bad := range []string{
		"http://api.yantra4d.com", "ftp://api.yantra4d.com", "api.yantra4d.com", "https://",
		"https://user:pw@api.yantra4d.com", "https://api.yantra4d.com/api", "https://api.yantra4d.com?x=1",
	} {
		_, err := NewClient(bad, time.Second)
		assert.Error(t, err, bad)
	}
}

func TestGetManifestDecodesTheCurrentShape(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	m, err := c.GetManifest(context.Background(), "multiboard", "")
	require.NoError(t, err)
	assert.Equal(t, "Multiboard Tile", m.Project.Name)
	assert.Equal(t, "openscad", m.Project.Engine)
	mode, ok := m.ModeByID("tile")
	require.True(t, ok)
	assert.Equal(t, []string{"tile"}, mode.Parts)
	assert.Contains(t, m.ExportFormats, "glb")
	assert.Len(t, m.Parameters, 2)
	assert.Equal(t, []string{""}, f.Auth, "anonymous calls carry no Authorization header")

	_, err = c.GetManifest(context.Background(), "nonexistent", "tok")
	requireCode(t, err, "project_not_found")
	assert.Equal(t, "Bearer tok", f.Auth[1])

	_, err = c.GetManifest(context.Background(), "../etc", "")
	requireCode(t, err, "invalid_request")
}

func TestRenderSendsTheDocumentedContract(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	req := tileRequest()
	req.IgnoreCache = true
	resp, err := c.Render(context.Background(), req, "")
	require.NoError(t, err)
	assert.Equal(t, "req-1", resp.RequestID)
	require.Len(t, f.Requests, 1)
	assert.Equal(t, map[string]any{
		"project": "multiboard", "mode": "tile", "parameters": map[string]any{"x_cells": float64(2)},
		"export_format": "stl", "ignore_cache": true,
	}, f.Requests[0], "project and mode in the body, parameters nested (no ?slug=, no flat params)")
}

func TestRenderRefusesBadRequestsBeforeCalling(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	for name, req := range map[string]RenderRequest{
		"no mode":        {Project: "multiboard", ExportFormat: "stl"},
		"unknown format": {Project: "multiboard", Mode: "tile", ExportFormat: "fbx"},
		"bad slug":       {Project: "Multi Board", Mode: "tile", ExportFormat: "stl"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := c.Render(context.Background(), req, "")
			requireCode(t, err, "invalid_request")
		})
	}
	assert.Empty(t, f.Requests)
}

func TestFetchVerifiesAndReturnsThePart(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	a, err := c.Fetch(context.Background(), tileRequest(), "tile", "")
	require.NoError(t, err)
	assert.Equal(t, f.Artifact, a.Data)
	assert.Equal(t, digest(f.Artifact), a.SHA256)
	assert.Equal(t, f.part()["instance_id"], a.InstanceID)
	assert.Equal(t, testVars, a.VariablesSHA256)
	assert.Equal(t, "model/stl", a.MediaType)
	assert.True(t, a.Complete)
	assert.Equal(t, time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC), a.LastModified)
	assert.Len(t, a.SidecarSHA256, 64)
}

// Every way Yantra4D's answer can be incomplete or wrong is an error.
func TestFetchFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		part   string
		req    func(*RenderRequest)
		mutate func(f *fakeYantra4D)
		code   string
	}{
		{name: "error answer (tier refuses the format)", code: "", mutate: func(f *fakeYantra4D) {
			f.RenderCode = http.StatusForbidden
			f.RenderBody = `{"status":"error","error":"Export format 'glb' requires Pro tier or above."}`
		}},
		{name: "unknown project", code: "project_not_found", mutate: func(f *fakeYantra4D) {
			f.RenderCode = http.StatusNotFound
			f.RenderBody = `{"status":"error","error":"Project 'nope' not found","error_code":"project_not_found"}`
		}},
		{name: "status not success", code: "render_failed", mutate: func(f *fakeYantra4D) { f.Envelope["status"] = "partial" }},
		{name: "no parts at all", code: "render_failed", mutate: func(f *fakeYantra4D) {
			f.Envelope["parts"] = []any{}
			f.Envelope["log"] = "[tile] ERROR: Render timed out while waiting for completion\n"
		}},
		{name: "requested part missing", part: "frame", code: "part_missing", mutate: func(f *fakeYantra4D) {}},
		{name: "requested part failed (left out, reported in the log)", code: "part_failed", mutate: func(f *fakeYantra4D) {
			f.Envelope["parts"] = []any{map[string]any{"type": "other", "url": "/static/x.stl"}}
			f.Envelope["log"] = "[other] rendered\n[tile] ERROR: openscad exited 1\n"
		}},
		{name: "part reported failed even though present", code: "part_failed", mutate: func(f *fakeYantra4D) {
			f.Envelope["log"] = "[tile] ERROR: conversion failed\n"
		}},
		{name: "part URL on another host", code: "url_rejected", mutate: func(f *fakeYantra4D) {
			f.part()["url"] = "https://evil.example/static/multiboard_preview_abc_tile.stl"
		}},
		{name: "part URL plain http on another host", code: "url_rejected", mutate: func(f *fakeYantra4D) {
			f.part()["url"] = "http://api.yantra4d.com/static/multiboard_preview_abc_tile.stl"
		}},
		{name: "protocol-relative sidecar URL", code: "url_rejected", mutate: func(f *fakeYantra4D) {
			f.part()["variables_url"] = "//evil.example/v.json"
		}},
		{name: "downloaded bytes differ from the answer", code: "digest_mismatch", mutate: func(f *fakeYantra4D) {
			f.Served = []byte("solid other\nendsolid other\n")
		}},
		{name: "size differs from the answer", code: "digest_mismatch", mutate: func(f *fakeYantra4D) {
			f.part()["size_bytes"] = 3
		}},
		{name: "STL served for a 3MF request", code: "media_type_mismatch",
			req: func(r *RenderRequest) { r.ExportFormat = "3mf" }, mutate: func(f *fakeYantra4D) {}},
		{name: "no GOC-1 fields", code: "goc1_missing", mutate: func(f *fakeYantra4D) {
			delete(f.part(), "sha256")
			delete(f.part(), "variables_url")
		}},
		{name: "no generator_output summary", code: "goc1_missing", mutate: func(f *fakeYantra4D) {
			delete(f.Envelope, "generator_output")
		}},
		{name: "sidecar instance id differs", code: "sidecar_mismatch", mutate: func(f *fakeYantra4D) {
			f.Sidecar["instance_id"] = strings.Repeat("e", 64)
		}},
		{name: "sidecar instance id not derived from its fields", code: "sidecar_mismatch", mutate: func(f *fakeYantra4D) {
			f.Sidecar["instance_id"] = strings.Repeat("e", 64)
			f.part()["instance_id"] = strings.Repeat("e", 64)
		}},
		{name: "sidecar for another part", code: "sidecar_mismatch", mutate: func(f *fakeYantra4D) {
			f.Sidecar["generator"].(map[string]any)["part"] = "frame"
		}},
		{name: "sidecar variables differ from the answer", code: "sidecar_mismatch", mutate: func(f *fakeYantra4D) {
			f.Envelope["generator_output"].(map[string]any)["variables_sha256"] = strings.Repeat("f", 64)
		}},
		{name: "sidecar primary digest differs", code: "sidecar_mismatch", mutate: func(f *fakeYantra4D) {
			f.Sidecar["geometry"].([]any)[0].(map[string]any)["sha256"] = strings.Repeat("c", 64)
		}},
		{name: "sidecar without primary geometry", code: "sidecar_mismatch", mutate: func(f *fakeYantra4D) {
			f.Sidecar["geometry"] = []any{f.Sidecar["geometry"].([]any)[1]}
		}},
		{name: "sidecar not GOC-1", code: "sidecar_mismatch", mutate: func(f *fakeYantra4D) { f.Sidecar["format_version"] = "2.0.0" }},
		{name: "sidecar not JSON", code: "sidecar_mismatch", mutate: func(f *fakeYantra4D) { f.SidecarBody = []byte("<html>") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			tc.mutate(f)
			c := newTestClient(t, f)
			req := tileRequest()
			if tc.req != nil {
				tc.req(&req)
			}
			part := tc.part
			if part == "" {
				part = "tile"
			}
			a, err := c.Fetch(context.Background(), req, part, "")
			assert.Nil(t, a)
			if tc.code == "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "403")
				return
			}
			requireCode(t, err, tc.code)
		})
	}
}

// Against the production origin, only https URLs on that host are followed.
func TestSameOriginOnTheProductionHost(t *testing.T) {
	c, err := NewClient(DefaultBaseURL, time.Second)
	require.NoError(t, err)
	for _, ok := range []string{"/static/a.stl", "https://api.yantra4d.com/static/a.stl", "static/a.stl"} {
		u, err := c.sameOrigin(ok)
		require.NoError(t, err, ok)
		assert.Equal(t, "https", u.Scheme)
	}
	for _, bad := range []string{
		"http://api.yantra4d.com/static/a.stl", "https://cdn.example/static/a.stl", "//cdn.example/a.stl",
		"https://api.yantra4d.com.evil.example/a.stl", "https://u:p@api.yantra4d.com/a.stl", "https://api.yantra4d.com:8443/a.stl",
	} {
		_, err := c.sameOrigin(bad)
		assert.Error(t, err, bad)
	}
}

func TestRedirectsAreRefused(t *testing.T) {
	f := newFake(t)
	f.RedirectTo = "https://elsewhere.example/api/render"
	c := newTestClient(t, f)
	_, err := c.Render(context.Background(), tileRequest(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing redirect")
}

// InstanceID must equal Yantra4D's generator_output.instance_id (Python
// canonical JSON); the golden values were computed with that function.
func TestInstanceIDMatchesGOC1(t *testing.T) {
	part := "tile"
	id, err := InstanceID("multiboard", "tile", &part, strings.Repeat("a", 64), strings.Repeat("b", 64))
	require.NoError(t, err)
	assert.Equal(t, "bc3825452763e80e0db993066c8f569f2b6b1d38fe80f2e6ebebcf4efd05f7a6", id)
	id, err = InstanceID("multiboard", "tile", nil, strings.Repeat("a", 64), strings.Repeat("b", 64))
	require.NoError(t, err)
	assert.Equal(t, "bb169b613406b795ea218a73c70df9ed315e818787cef5d44550bf477183ca63", id)
}

func TestFailedParts(t *testing.T) {
	r := &RenderResponse{Log: "[a] cache HIT\n[b] ERROR: x\n[c] INFO: cancelled\n[d] ERROR: Render timed out\n"}
	assert.Equal(t, []string{"b", "d"}, r.FailedParts())
}
