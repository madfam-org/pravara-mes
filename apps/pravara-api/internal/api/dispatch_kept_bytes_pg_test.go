package api

// RFC 0059 S12: dispatch keeps the manufacturing bytes it sent to be made,
// and serves them back to the dispatch's own tenant. PostgreSQL-backed
// (opt-in with PRAVARA_TEST_DATABASE_URL); the bucket is the in-memory store.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatch"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatchfakes"
)

func renderOf(t *testing.T, d *repositories.DispatchJob) dispatch.RenderBundle {
	t.Helper()
	var b dispatch.RenderBundle
	require.NoError(t, json.Unmarshal(d.RenderBundle, &b))
	return b
}

func hexSHA(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestDispatchKeepsTheBytesAndServesThemToItsTenant(t *testing.T) {
	r := newDispatchRig(t)
	r.seedMachine(t, "voron-01", "idle", "tpu-95a")
	id := r.create(t, r.seedTask(t, boxSpecs))
	d := r.runUntil(t, id, repositories.DispatchRendered)

	b := renderOf(t, d)
	require.Len(t, b.Kept, 2, "geometry and sidecar kept before slicing")
	assert.Empty(t, r.fabprep.Jobs, "nothing sliced yet")
	geo, ok := r.store.Bytes(b.Kept[0].Key)
	require.True(t, ok)
	assert.Equal(t, r.yantra.Geometry, geo)
	assert.Equal(t, "tenants/"+r.tenantID.String()+"/sha256/"+b.SHA256, b.Kept[0].Key)

	// Slicing still reads from yantra4d: fabrication-prep fetches only from
	// its allow-listed hosts.
	r.runUntil(t, id, repositories.DispatchSliced)
	for _, job := range r.fabprep.Jobs {
		assert.Equal(t, b.URL, job.Body["input"].(map[string]any)["url"])
	}

	person := r.userToken(t, "operator")
	base := "/v1/dispatches/" + id.String() + "/artifacts/"

	w := r.do(http.MethodGet, base+"artifact", person, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, r.yantra.Geometry, w.Body.Bytes())
	assert.Equal(t, b.SHA256, w.Header().Get("X-Content-SHA256"))
	assert.Equal(t, "model/3mf", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), ".3mf")

	w = r.do(http.MethodGet, base+"sidecar", person, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, b.SidecarSHA256, hexSHA(w.Body.Bytes()), "the sidecar exactly as checked")

	// Same access as reading the dispatch: a pravara-mes:read machine token.
	assert.Equal(t, http.StatusOK, r.do(http.MethodGet, base+"artifact", r.machineToken(t, "pravara-mes:read", nil), nil).Code)
	assert.Equal(t, http.StatusForbidden, r.do(http.MethodGet, base+"artifact", r.machineToken(t, "pravara-mes:nodes", nil), nil).Code)
	assert.Equal(t, http.StatusUnauthorized, r.do(http.MethodGet, base+"artifact", "", nil).Code)

	// Unknown kind, unknown digest, unknown dispatch.
	assert.Equal(t, http.StatusNotFound, r.do(http.MethodGet, base+"gcode", person, nil).Code)
	w = r.do(http.MethodGet, base+"artifact?sha256="+hexSHA([]byte("other")), person, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "artifact_not_kept")
	assert.Equal(t, http.StatusNotFound, r.do(http.MethodGet, "/v1/dispatches/"+uuid.NewString()+"/artifacts/artifact", person, nil).Code)

	// Another tenant: as if the dispatch did not exist.
	other := uuid.New()
	_, err := r.admin.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Other', 'other-kept')`, other)
	require.NoError(t, err)
	otherRig := *r.scopeRig
	otherRig.tenantID = other
	w = r.do(http.MethodGet, base+"artifact", otherRig.userToken(t, "operator"), nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NotContains(t, w.Body.String(), b.SHA256)

	// A damaged copy is never sent: the digest is checked before the first
	// byte goes out.
	r.store.Tamper(b.Kept[0].Key, append([]byte("x"), geo[1:]...))
	w = r.do(http.MethodGet, base+"artifact", person, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "artifact_digest_mismatch")
	assert.NotContains(t, w.Body.String(), "PK")
}

// A render whose geometry does not match its declared digest is never
// sliced; a download that fails with 503 is retried and then kept.
func TestDispatchRenderDownloadOutcomes(t *testing.T) {
	r := newDispatchRig(t)
	r.seedMachine(t, "voron-01", "idle", "tpu-95a")

	t.Run("503 then ok: retried, kept, sliced", func(t *testing.T) {
		r.yantra.Set(func(y *dispatchfakes.Yantra4D) {
			y.ArtifactFailures, y.ArtifactFailStatus = 1, http.StatusServiceUnavailable
		})
		before := len(r.fabprep.Jobs)
		id := r.create(t, r.seedTask(t, boxSpecs))
		d := r.runUntil(t, id, repositories.DispatchSliced)
		assert.Equal(t, 1, d.Attempts, "one retry spent")
		assert.Len(t, renderOf(t, d).Kept, 2)
		assert.Len(t, r.fabprep.Jobs, before+1)
		// Complete the slot so the next sub-test gets the machine.
		_, err := r.admin.Exec(`UPDATE dispatch_jobs SET status = 'cancelled' WHERE id = $1`, id)
		require.NoError(t, err)
		_, err = r.admin.Exec(`UPDATE machine_reservations SET status = 'released', released_at = NOW() WHERE dispatch_id = $1`, id)
		require.NoError(t, err)
	})

	t.Run("a part URL on another host: terminal, nothing fetched there", func(t *testing.T) {
		var reached atomic.Int32
		evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached.Add(1) }))
		defer evil.Close()
		r.yantra.Set(func(y *dispatchfakes.Yantra4D) { y.PartURL = evil.URL + "/static/x.3mf" })
		defer r.yantra.Set(func(y *dispatchfakes.Yantra4D) { y.PartURL = "" })
		jobs := len(r.fabprep.Jobs)
		id := r.create(t, r.seedTask(t, boxSpecs))
		d := r.runUntil(t, id, repositories.DispatchFailed)
		assert.Equal(t, "render_url_off_origin", d.ErrorCode)
		assert.False(t, *d.ErrorRetryable)
		assert.Zero(t, reached.Load(), "no request (and no bearer) reached the other host")
		assert.Len(t, r.fabprep.Jobs, jobs)
	})

	t.Run("digest mismatch: terminal, nothing sliced or kept", func(t *testing.T) {
		r.yantra.Set(func(y *dispatchfakes.Yantra4D) { y.ArtifactBody = []byte("not the declared geometry") })
		defer r.yantra.Set(func(y *dispatchfakes.Yantra4D) { y.ArtifactBody = nil })
		keys, jobs := len(r.store.Keys()), len(r.fabprep.Jobs)
		id := r.create(t, r.seedTask(t, boxSpecs))
		d := r.runUntil(t, id, repositories.DispatchFailed)
		assert.Equal(t, "render_digest_mismatch", d.ErrorCode)
		require.NotNil(t, d.ErrorRetryable)
		assert.False(t, *d.ErrorRetryable)
		assert.Len(t, r.fabprep.Jobs, jobs, "nothing was sliced")
		assert.Len(t, r.store.Keys(), keys, "nothing was kept")
	})
}
