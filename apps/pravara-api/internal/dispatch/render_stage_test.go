package dispatch

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/yantra4d"
)

// yantra4d's own wording for a part it left out (render_orchestrator.py).
const (
	lidTimedOut = "render queued\n[lid] ERROR: Render timed out while waiting for completion\n"
	lidCrashed  = "[body] INFO: done\n[lid] ERROR: OpenSCAD exited with status 1\n"
)

func TestFailedParts(t *testing.T) {
	assert.Empty(t, failedParts(""))
	assert.Empty(t, failedParts("[body] INFO: part_done\nrendered 1 part with 0 errors\n"))
	assert.Equal(t, []string{"lid"}, failedParts(lidTimedOut))
	assert.Equal(t, []string{"lid"}, failedParts(lidCrashed))
	assert.Equal(t, []string{"a", "b"}, failedParts("[a] ERROR: x\n[b] ERROR: y\n"))
	// Only yantra4d's own line shape counts: an engine's "ERROR:" inside a
	// part's log, or one mid-line, is not a part that was left out.
	assert.Empty(t, failedParts("ECHO: ERROR: not a part line\nnote [lid] ERROR: mid-line\n"))
}

func TestPickPart(t *testing.T) {
	body := yantra4d.Part{Type: "body"}
	lid := yantra4d.Part{Type: "lid"}

	t.Run("the named part", func(t *testing.T) {
		part, serr := pickPart([]yantra4d.Part{body, lid}, "lid", "")
		require.Nil(t, serr)
		assert.Equal(t, "lid", part.Type)
	})

	t.Run("the named part, even when another part failed", func(t *testing.T) {
		part, serr := pickPart([]yantra4d.Part{body}, "body", lidTimedOut)
		require.Nil(t, serr)
		assert.Equal(t, "body", part.Type)
	})

	t.Run("the only part when none is named and nothing failed", func(t *testing.T) {
		part, serr := pickPart([]yantra4d.Part{body}, "", "[body] INFO: part_done\n")
		require.Nil(t, serr)
		assert.Equal(t, "body", part.Type)
	})

	t.Run("a named part that yantra4d failed to render is retried, with its reason", func(t *testing.T) {
		_, serr := pickPart([]yantra4d.Part{body}, "lid", lidTimedOut)
		require.NotNil(t, serr)
		assert.Equal(t, "render_part_missing", serr.code)
		assert.True(t, serr.retryable)
		assert.Contains(t, serr.msg, `failed to render part "lid"`)
		assert.Contains(t, serr.msg, "Render timed out while waiting for completion")
	})

	t.Run("a named part the mode does not produce is not retried", func(t *testing.T) {
		_, serr := pickPart([]yantra4d.Part{body}, "lid", "[body] INFO: part_done\n")
		require.NotNil(t, serr)
		assert.Equal(t, "render_part_missing", serr.code)
		assert.False(t, serr.retryable, "five more renders would not make the part exist")
		assert.Contains(t, serr.msg, "does not produce it")
	})

	// A two-part mode with no part named is a configuration error. If one
	// part times out, one part comes back; taking it as "the only part"
	// would start a job for a part nobody chose.
	t.Run("no part named and a part was left out: incomplete, never the survivor", func(t *testing.T) {
		_, serr := pickPart([]yantra4d.Part{body}, "", lidTimedOut)
		require.NotNil(t, serr)
		assert.Equal(t, "render_incomplete", serr.code)
		assert.True(t, serr.retryable)
		assert.Contains(t, serr.msg, "lid")
	})

	t.Run("several parts and none named is a configuration error", func(t *testing.T) {
		_, serr := pickPart([]yantra4d.Part{body, lid}, "", "")
		require.NotNil(t, serr)
		assert.Equal(t, "render_part_ambiguous", serr.code)
		assert.False(t, serr.retryable)
		assert.Contains(t, serr.msg, "renders 2 parts")
	})
}

func TestRenderIsFresh(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	freshFor := 6 * time.Hour
	cases := []struct {
		name       string
		renderedAt time.Time
		want       bool
	}{
		{"just rendered", now, true},
		{"an hour old", now.Add(-time.Hour), true},
		{"a second inside the window", now.Add(-freshFor + time.Second), true},
		{"exactly at the window", now.Add(-freshFor), false},
		{"a day old (yantra4d has deleted it)", now.Add(-24 * time.Hour), false},
		{"no timestamp (recorded before the field existed)", time.Time{}, false},
		{"stamped in the future (clock skew): not trusted", now.Add(time.Minute), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, renderIsFresh(RenderBundle{RenderedAt: tc.renderedAt}, now, freshFor))
		})
	}
}

// A cached answer points at an artifact made earlier; that time, not the
// time of the request, decides how long its URL stays good.
func TestArtifactTime(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	made := now.Add(-20 * time.Hour)
	assert.Equal(t, made, artifactTime(made, now), "the artifact's own time when yantra4d reports it")
	assert.Equal(t, now, artifactTime(time.Time{}, now), "unknown: when the answer arrived")
	assert.Equal(t, now, artifactTime(now.Add(time.Hour), now), "a time in the future is not believed")

	// The case the freshness rule exists for: rendered 20 h ago by someone
	// else, answered from cache now. It must not count as new.
	assert.False(t, renderIsFresh(RenderBundle{RenderedAt: artifactTime(made, now)}, now, 6*time.Hour))
}

// fabrication-prep refuses for good a key reused with another body. A
// re-render after a re-match changes the body (URL and digests) even on the
// same machine, so the key must change with the render.
func TestSliceIdempotencyKey(t *testing.T) {
	dispatchID, machine, other := uuid.New(), uuid.New(), uuid.New()
	first := RenderBundle{URL: "https://y/static/a.3mf", SHA256: "aa", VariablesURL: "https://y/static/a.json", SidecarSHA256: "s1"}

	key := sliceIdempotencyKey(dispatchID, machine, first)
	assert.True(t, strings.HasPrefix(key, "pravara-dispatch:"+dispatchID.String()+":"+machine.String()[:8]+":"))
	assert.Equal(t, key, sliceIdempotencyKey(dispatchID, machine, first), "an equal request keeps its key (a retry is idempotent)")

	changes := map[string]RenderBundle{
		"geometry bytes":   {URL: first.URL, SHA256: "bb", VariablesURL: first.VariablesURL, SidecarSHA256: first.SidecarSHA256},
		"sidecar":          {URL: first.URL, SHA256: first.SHA256, VariablesURL: first.VariablesURL, SidecarSHA256: "s2"},
		"renamed artifact": {URL: "https://y/static/b.3mf", SHA256: first.SHA256, VariablesURL: first.VariablesURL, SidecarSHA256: first.SidecarSHA256},
	}
	for name, bundle := range changes {
		assert.NotEqual(t, key, sliceIdempotencyKey(dispatchID, machine, bundle), "same machine, changed %s", name)
	}
	assert.NotEqual(t, key, sliceIdempotencyKey(dispatchID, other, first), "another machine")
	assert.NotEqual(t, key, sliceIdempotencyKey(uuid.New(), machine, first), "another dispatch")

	// Fields that do not reach the slice request do not change the key.
	stamped := first
	stamped.RenderedAt, stamped.PreviousSHA256 = time.Now(), "zz"
	assert.Equal(t, key, sliceIdempotencyKey(dispatchID, machine, stamped))
}

// A dispatch's error message is a PostgreSQL text column and can quote
// another service. A refused save would replay the hop without end.
func TestStorableText(t *testing.T) {
	assert.Equal(t, "plain", storableText("plain"))
	assert.Equal(t, "line one\nline two", storableText("line one\nline two"), "ordinary whitespace is kept")

	got := storableText("nul\x00 esc\x1b bad\xff\xfe ok")
	assert.True(t, utf8.ValidString(got))
	assert.Equal(t, "nul esc bad ok", got)

	long := storableText(strings.Repeat("ñ", 2500))
	assert.True(t, utf8.ValidString(long), "cut on a character boundary")
	assert.Equal(t, 2001, len([]rune(long)))
	assert.True(t, strings.HasSuffix(long, "…"))
}

func TestSettingsDefaultRenderFreshFor(t *testing.T) {
	var s Settings
	s.defaults()
	assert.Equal(t, 6*time.Hour, s.RenderFreshFor)
}
