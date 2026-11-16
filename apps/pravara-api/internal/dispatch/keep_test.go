package dispatch

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/artifactstore"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatchfakes"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/yantra4d"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
)

const keepTree = "9de820d73e02774a8237748189ef4c3171de989d0009c2ca9020d9e6301349d7"

type keepRig struct {
	svc    *Service
	yantra *dispatchfakes.Yantra4D
	store  *artifactstore.Memory
	client *yantra4d.Client
	tenant uuid.UUID
}

func newKeepRig(t *testing.T, maxBytes int64) *keepRig {
	t.Helper()
	janua := dispatchfakes.NewJanua(t, dispatchfakes.Client{ID: "jnc_y", Secret: "jns_y",
		Scope: machineclients.Yantra4DScope, Audience: machineclients.Yantra4DAudience})
	y := dispatchfakes.NewYantra4D(t, "motor-soft-mount", "assembled", "body", keepTree)
	ts := machineclients.NewTokenSource(machineclients.Credentials{Name: "y", TokenURL: janua.TokenURL(),
		ClientID: "jnc_y", ClientSecret: "jns_y", Scope: machineclients.Yantra4DScope}, nil)
	c, err := yantra4d.NewClient(y.Server.URL, ts, nil)
	require.NoError(t, err)
	store := artifactstore.NewMemory()
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	svc := NewService(Deps{Renderer: c, Store: store}, Settings{ArtifactMaxBytes: maxBytes}, log)
	return &keepRig{svc: svc, yantra: y, store: store, client: c, tenant: uuid.New()}
}

// render asks the fake for a render and builds the bundle the way stageRender
// does, returning it with the sidecar's raw bytes and the declared size.
func (r *keepRig) render(t *testing.T) (RenderBundle, []byte, *int64) {
	t.Helper()
	ctx := context.Background()
	resp, err := r.client.Render(ctx, yantra4d.RenderRequest{Project: "motor-soft-mount", Mode: "assembled", ExportFormat: "3mf"})
	require.NoError(t, err)
	part := resp.Parts[0]
	sidecar, sidecarSHA, err := r.client.FetchSidecar(ctx, part)
	require.NoError(t, err)
	partURL, err := r.client.OriginURL(part.URL)
	require.NoError(t, err)
	variablesURL, err := r.client.OriginURL(part.VariablesURL)
	require.NoError(t, err)
	return RenderBundle{Part: part.Type, URL: partURL, SHA256: part.SHA256, MediaType: part.MediaType,
		InstanceID: sidecar.InstanceID, VariablesURL: variablesURL, SidecarSHA256: sidecarSHA}, sidecar.Raw, part.SizeBytes
}

func (r *keepRig) key(t *testing.T, digest string) string {
	key, err := artifactstore.Key(r.tenant, digest)
	require.NoError(t, err)
	return key
}

func TestKeepRenderStoresVerifiedBytes(t *testing.T) {
	r := newKeepRig(t, 0)
	b, raw, size := r.render(t)
	require.Nil(t, r.svc.keepRender(context.Background(), r.tenant, &b, raw, size))

	require.Len(t, b.Kept, 2)
	art, side := b.Kept[0], b.Kept[1]
	assert.Equal(t, KindArtifact, art.Kind)
	assert.Equal(t, b.SHA256, art.SHA256)
	assert.Equal(t, r.key(t, b.SHA256), art.Key)
	assert.Equal(t, int64(len(r.yantra.Geometry)), art.Bytes)
	assert.Equal(t, "model/3mf", art.MediaType)
	assert.WithinDuration(t, time.Now(), art.KeptAt, time.Minute)
	assert.Equal(t, KindSidecar, side.Kind)
	assert.Equal(t, b.SidecarSHA256, side.SHA256)
	assert.Equal(t, "application/json", side.MediaType)
	assert.True(t, keptComplete(b))

	geo, ok := r.store.Bytes(art.Key)
	require.True(t, ok)
	assert.Equal(t, r.yantra.Geometry, geo, "the bytes yantra4d served, verbatim")
	doc, ok := r.store.Bytes(side.Key)
	require.True(t, ok)
	assert.Equal(t, raw, doc, "the sidecar exactly as it was checked")
	assert.Equal(t, 1, r.yantra.Downloads, "the geometry is downloaded once; the sidecar is not fetched again")
}

func TestKeepRenderDownloadsTheSidecarWhenItsBytesAreNotAtHand(t *testing.T) {
	r := newKeepRig(t, 0)
	b, raw, _ := r.render(t)
	require.Nil(t, r.svc.keepRender(context.Background(), r.tenant, &b, nil, nil))
	doc, ok := r.store.Bytes(r.key(t, b.SidecarSHA256))
	require.True(t, ok)
	assert.Equal(t, raw, doc)
}

func TestKeepRenderDigestMismatchIsTerminal(t *testing.T) {
	r := newKeepRig(t, 0)
	b, raw, size := r.render(t)
	r.yantra.Set(func(y *dispatchfakes.Yantra4D) { y.ArtifactBody = append([]byte("tampered "), y.Geometry...) })
	serr := r.svc.keepRender(context.Background(), r.tenant, &b, raw, size)
	require.NotNil(t, serr)
	assert.Equal(t, "render_digest_mismatch", serr.code)
	assert.False(t, serr.retryable)
	assert.Contains(t, serr.msg, b.SHA256)
	assert.Empty(t, r.store.Keys(), "nothing unverified is stored")
	assert.Empty(t, b.Kept)

	// A sidecar that is not the document that was checked.
	r2 := newKeepRig(t, 0)
	b2, raw2, size2 := r2.render(t)
	bad := append([]byte(nil), raw2...)
	bad[len(bad)-2] = ' '
	serr = r2.svc.keepRender(context.Background(), r2.tenant, &b2, bad, size2)
	require.NotNil(t, serr)
	assert.Equal(t, "render_digest_mismatch", serr.code)
	assert.Contains(t, serr.msg, "sidecar")
}

func TestKeepRenderDownloadFailures(t *testing.T) {
	cases := []struct {
		status    int
		retryable bool
	}{
		{http.StatusServiceUnavailable, true},
		{http.StatusBadGateway, true},
		{http.StatusTooManyRequests, true},
		{http.StatusNotFound, true}, // deleted: the retry renders again
		{http.StatusGone, true},
		{http.StatusForbidden, false},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			r := newKeepRig(t, 0)
			b, raw, size := r.render(t)
			r.yantra.Set(func(y *dispatchfakes.Yantra4D) { y.ArtifactFailures, y.ArtifactFailStatus = 1, tc.status })
			serr := r.svc.keepRender(context.Background(), r.tenant, &b, raw, size)
			require.NotNil(t, serr)
			assert.Equal(t, "render_download_failed", serr.code)
			assert.Equal(t, tc.retryable, serr.retryable)
			assert.Empty(t, r.store.Keys())
		})
	}
}

func TestKeepRenderStoreFailureIsRetryable(t *testing.T) {
	r := newKeepRig(t, 0)
	b, raw, size := r.render(t)
	r.store.PutErr = errors.New("bucket unavailable")
	serr := r.svc.keepRender(context.Background(), r.tenant, &b, raw, size)
	require.NotNil(t, serr)
	assert.Equal(t, "artifact_store_failed", serr.code)
	assert.True(t, serr.retryable)
}

func TestKeepRenderStoreConflictIsTerminal(t *testing.T) {
	r := newKeepRig(t, 0)
	b, raw, size := r.render(t)
	// Something else is already at the content address of the geometry.
	r.store.PutRaw(r.key(t, b.SHA256), []byte("not the geometry"), b.SHA256)
	serr := r.svc.keepRender(context.Background(), r.tenant, &b, raw, size)
	require.NotNil(t, serr)
	assert.Equal(t, "artifact_store_conflict", serr.code)
	assert.False(t, serr.retryable)
}

func TestKeepRenderTooLargeIsTerminal(t *testing.T) {
	t.Run("declared by the render envelope: not downloaded at all", func(t *testing.T) {
		r := newKeepRig(t, 8)
		b, raw, size := r.render(t)
		serr := r.svc.keepRender(context.Background(), r.tenant, &b, raw, size)
		require.NotNil(t, serr)
		assert.Equal(t, "render_artifact_too_large", serr.code)
		assert.False(t, serr.retryable)
		assert.Contains(t, serr.msg, "DISPATCH_ARTIFACT_MAX_BYTES")
		assert.Zero(t, r.yantra.Downloads)
	})
	t.Run("undeclared: cut off while streaming", func(t *testing.T) {
		r := newKeepRig(t, 8)
		b, raw, _ := r.render(t)
		serr := r.svc.keepRender(context.Background(), r.tenant, &b, raw, nil)
		require.NotNil(t, serr)
		assert.Equal(t, "render_artifact_too_large", serr.code)
		assert.Empty(t, r.store.Keys())
	})
}

func TestKeepRenderTwiceWritesOnce(t *testing.T) {
	r := newKeepRig(t, 0)
	b, raw, size := r.render(t)
	require.Nil(t, r.svc.keepRender(context.Background(), r.tenant, &b, raw, size))
	again := b
	again.Kept = nil
	require.Nil(t, r.svc.keepRender(context.Background(), r.tenant, &again, raw, size))
	assert.Equal(t, 2, r.store.Writes, "bytes already kept are not written again")
	assert.Equal(t, b.Kept[0].Key, again.Kept[0].Key)
}

func TestKeepRenderTenantsDoNotShareKeys(t *testing.T) {
	r := newKeepRig(t, 0)
	b, raw, size := r.render(t)
	require.Nil(t, r.svc.keepRender(context.Background(), r.tenant, &b, raw, size))
	other := b
	other.Kept = nil
	require.Nil(t, r.svc.keepRender(context.Background(), uuid.New(), &other, raw, size))
	assert.NotEqual(t, b.Kept[0].Key, other.Kept[0].Key, "the same bytes are kept once per tenant")
	assert.Equal(t, 4, r.store.Writes)
}

// A stale render requested again with other bytes: the new bytes are kept,
// the earlier ones stay in the store and on the record as superseded.
func TestKeepRenderStaleRerenderKeepsTheNewCopyAndTheOld(t *testing.T) {
	r := newKeepRig(t, 0)
	first, raw, size := r.render(t)
	require.Nil(t, r.svc.keepRender(context.Background(), r.tenant, &first, raw, size))

	r.yantra.Set(func(y *dispatchfakes.Yantra4D) {
		y.Geometry = append(append([]byte(nil), y.Geometry...), " rebuilt"...)
	})
	second, raw2, size2 := r.render(t)
	require.NotEqual(t, first.SHA256, second.SHA256)
	require.Nil(t, r.svc.keepRender(context.Background(), r.tenant, &second, raw2, size2))
	second.SupersededKept = supersededKept(first, second.Kept)

	require.Len(t, second.SupersededKept, 2, "the earlier geometry and sidecar")
	for _, k := range append(append([]KeptObject(nil), first.Kept...), second.Kept...) {
		_, ok := r.store.Bytes(k.Key)
		assert.True(t, ok, "%s %s is in the store", k.Kind, k.SHA256)
	}
	assert.Len(t, r.store.Keys(), 4, "nothing was deleted")

	// A third render that comes back to the first bytes: the trail does not
	// list the bytes in use as superseded, nor anything twice.
	third := first
	third.Kept = first.Kept
	trail := supersededKept(second, third.Kept)
	for _, k := range trail {
		assert.NotEqual(t, first.SHA256, k.SHA256)
	}
	assert.Len(t, trail, 2)
}

func TestKeepRenderWithoutAStoreFailsClosed(t *testing.T) {
	r := newKeepRig(t, 0)
	b, raw, size := r.render(t)
	r.svc.Store = nil
	serr := r.svc.keepRender(context.Background(), r.tenant, &b, raw, size)
	require.NotNil(t, serr)
	assert.Equal(t, "artifact_store_missing", serr.code)
	assert.False(t, serr.retryable)
}

func TestKeptComplete(t *testing.T) {
	b := RenderBundle{SHA256: "a", SidecarSHA256: "s"}
	assert.False(t, keptComplete(b), "a render recorded before bytes were kept")
	b.Kept = []KeptObject{{Kind: KindArtifact, SHA256: "a"}}
	assert.False(t, keptComplete(b))
	b.Kept = append(b.Kept, KeptObject{Kind: KindSidecar, SHA256: "s"})
	assert.True(t, keptComplete(b))
	b.SHA256 = "b"
	assert.False(t, keptComplete(b), "kept bytes of another render do not count")
}

// A store failure's message is stored on the dispatch, which tenants read:
// it names what failed, never the endpoint or the bucket.
func TestKeepRenderStoreErrorsAreScrubbed(t *testing.T) {
	r := newKeepRig(t, 0)
	b, raw, size := r.render(t)
	s3, err := artifactstore.NewS3(artifactstore.Config{Backend: artifactstore.BackendR2, EndpointURL: "http://127.0.0.1:1",
		BucketName: "pravara-dispatch-artifacts-private", AccessKeyID: "placeholder-id", SecretAccessKey: "placeholder-secret", MaxAttempts: 1}, nil)
	require.NoError(t, err)
	s3.CallTimeout = 30 * time.Second
	r.svc.Store = s3
	serr := r.svc.keepRender(context.Background(), r.tenant, &b, raw, size)
	require.NotNil(t, serr)
	assert.Equal(t, "artifact_store_failed", serr.code)
	assert.True(t, serr.retryable)
	for _, leak := range []string{"127.0.0.1", "pravara-dispatch-artifacts-private", "tenants/", "placeholder"} {
		assert.NotContains(t, serr.msg, leak)
	}
	assert.Contains(t, serr.msg, "the artifact store could not be reached")
}
