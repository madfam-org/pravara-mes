package dispatch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/yantra4d"
)

func TestPickPart(t *testing.T) {
	body := yantra4d.Part{Type: "body"}
	lid := yantra4d.Part{Type: "lid"}

	t.Run("the named part", func(t *testing.T) {
		part, serr := pickPart([]yantra4d.Part{body, lid}, "lid", "")
		require.Nil(t, serr)
		assert.Equal(t, "lid", part.Type)
	})

	t.Run("the only part when none is named", func(t *testing.T) {
		part, serr := pickPart([]yantra4d.Part{body}, "", "")
		require.Nil(t, serr)
		assert.Equal(t, "body", part.Type)
	})

	// yantra4d omits a part that failed or timed out from an otherwise
	// successful answer, so a missing part is retried, with its reason.
	t.Run("a named part that is missing is retried with yantra4d's reason", func(t *testing.T) {
		_, serr := pickPart([]yantra4d.Part{body}, "lid", "[lid] Error: Render timed out after 120s")
		require.NotNil(t, serr)
		assert.Equal(t, "render_part_missing", serr.code)
		assert.True(t, serr.retryable)
		assert.Contains(t, serr.msg, `no part "lid"`)
		assert.Contains(t, serr.msg, "Render timed out after 120s")
	})

	t.Run("several parts and none named is a configuration error", func(t *testing.T) {
		_, serr := pickPart([]yantra4d.Part{body, lid}, "", "")
		require.NotNil(t, serr)
		assert.Equal(t, "render_part_ambiguous", serr.code)
		assert.False(t, serr.retryable)
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

func TestSettingsDefaultRenderFreshFor(t *testing.T) {
	var s Settings
	s.defaults()
	assert.Equal(t, 6*time.Hour, s.RenderFreshFor)
}
