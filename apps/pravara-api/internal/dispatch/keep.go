package dispatch

// Keeping the manufacturing bytes (RFC 0059 S12). yantra4d deletes a render
// 24 hours after it was made, so a URL is not a record of what was made.
// Before a render goes on to slicing, pravara downloads the geometry and its
// GOC-1 sidecar, checks each against the sha256 it was given, and stores them
// in its own bucket (internal/artifactstore). Nothing is ever deleted there.
//
// fabrication-prep still fetches its input from yantra4d: it accepts input
// only from hosts on its allow-list. The kept copy is pravara's record, not
// what slicing reads.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/artifactstore"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/remote"
)

// Kinds of kept bytes.
const (
	// KindArtifact is the rendered geometry (model/3mf or model/stl) sent to
	// slicing.
	KindArtifact = "artifact"
	// KindSidecar is its GOC-1 sidecar (variables.json): the variables and
	// digests the geometry was made from.
	KindSidecar = "sidecar"
)

// KeptObject is one set of bytes pravara keeps from a render.
type KeptObject struct {
	Kind      string    `json:"kind"`
	Key       string    `json:"key"`
	SHA256    string    `json:"sha256"`
	Bytes     int64     `json:"bytes"`
	MediaType string    `json:"media_type"`
	KeptAt    time.Time `json:"kept_at"`
}

// keepBudget bounds the whole keep step: the geometry download (and the
// sidecar's, for a render kept late), each bounded by the yantra4d client's
// own timeout, and two store writes, each bounded by the store's call timeout
// (artifactstore.DefaultCallTimeout). The step stops when the budget is
// spent, whatever its parts would allow.
const keepBudget = 12 * time.Minute

// keepLease is the runner's lease on a dispatch during the keep step: the
// budget plus a margin for saving the outcome, so no other runner claims the
// dispatch while it is being kept.
const keepLease = keepBudget + 3*time.Minute

// prepareKeep extends the runner's lease and the machine reservation to cover
// the keep step, and returns the context bounded by keepBudget.
func (s *Service) prepareKeep(ctx context.Context, d *repositories.DispatchJob) (context.Context, context.CancelFunc, *stepError) {
	err := s.inTenant(ctx, d.TenantID, func(ctx context.Context) error {
		return s.Dispatch.ExtendLease(ctx, d.ID, s.owner, keepLease)
	})
	if errors.Is(err, repositories.ErrLeaseLost) {
		return nil, nil, retryable("lease_lost", "another runner took the dispatch over")
	}
	if err != nil {
		return nil, nil, retryable("database", "%v", err)
	}
	if serr := s.holdReservation(ctx, d, keepLease+s.cfg.ReservationTTL); serr != nil {
		return nil, nil, serr
	}
	kctx, cancel := context.WithTimeout(ctx, keepBudget)
	return kctx, cancel, nil
}

// keepWithLease is keepRender for a dispatch the runner holds: the lease and
// the machine reservation are extended to cover the step, which is bounded
// by keepBudget.
func (s *Service) keepWithLease(ctx context.Context, d *repositories.DispatchJob, b *RenderBundle, sidecarRaw []byte, declaredSize *int64) *stepError {
	kctx, cancel, serr := s.prepareKeep(ctx, d)
	if serr != nil {
		return serr
	}
	defer cancel()
	return s.keepRender(kctx, d.TenantID, b, sidecarRaw, declaredSize)
}

// rewindUnkept enforces RFC 0059 S12 before work goes onward to slicing or
// to a machine: a dispatch whose render bytes are not kept (one recorded
// before pravara kept bytes) goes back to the render stage, and true is
// returned. That stage keeps the bytes while the render is fresh and renders
// again (and keeps) when it is not, so nothing is sliced or started from
// bytes pravara does not hold. A slice already made from the same render is
// found again by its idempotency key.
func (s *Service) rewindUnkept(d *repositories.DispatchJob) bool {
	if b, err := decode[RenderBundle](d.RenderBundle); err == nil && keptComplete(*b) {
		return false
	}
	s.log.WithFields(logrus.Fields{"dispatch_id": d.ID, "status": d.Status}).
		Warn("dispatch: the render's bytes are not kept; going back to the render stage to keep them before going on")
	d.SliceResult = nil
	d.CommandID = nil
	d.Status = repositories.DispatchReserved
	return true
}

// keptComplete reports whether a bundle holds both kinds for its own digests.
func keptComplete(b RenderBundle) bool {
	var art, side bool
	for _, k := range b.Kept {
		art = art || (k.Kind == KindArtifact && k.SHA256 == b.SHA256)
		side = side || (k.Kind == KindSidecar && k.SHA256 == b.SidecarSHA256)
	}
	return art && side
}

// supersededKept is the trail of kept bytes a new render replaces: what the
// earlier render had already superseded, plus its own kept bytes that the new
// render does not share. The objects stay in the store.
func supersededKept(earlier RenderBundle, now []KeptObject) []KeptObject {
	seen := map[string]bool{}
	for _, k := range now {
		seen[k.Kind+":"+k.SHA256] = true
	}
	var out []KeptObject
	for _, k := range append(append([]KeptObject(nil), earlier.SupersededKept...), earlier.Kept...) {
		if id := k.Kind + ":" + k.SHA256; !seen[id] {
			seen[id] = true
			out = append(out, k)
		}
	}
	return out
}

// keepRender downloads, verifies and stores a render's geometry and sidecar,
// and records them on b. sidecarRaw, when known, is the sidecar exactly as it
// was downloaded and checked; it is stored without fetching it again.
// declaredSize is the geometry's size_bytes from the render envelope.
func (s *Service) keepRender(ctx context.Context, tenantID uuid.UUID, b *RenderBundle, sidecarRaw []byte, declaredSize *int64) *stepError {
	if s.Store == nil {
		// The wiring refuses to build an enabled dispatch without a store;
		// this guards any other way of building the service.
		return terminal("artifact_store_missing", "the artifact store is not configured; pravara does not dispatch bytes it cannot keep")
	}
	art, serr := s.keepDownload(ctx, tenantID, KindArtifact, b.URL, b.SHA256, b.MediaType, declaredSize)
	if serr != nil {
		return serr
	}
	var side *KeptObject
	if sidecarRaw != nil {
		side, serr = s.keepVerified(ctx, tenantID, KindSidecar, sidecarRaw, b.SidecarSHA256)
	} else {
		side, serr = s.keepDownload(ctx, tenantID, KindSidecar, b.VariablesURL, b.SidecarSHA256, "application/json", nil)
	}
	if serr != nil {
		return serr
	}
	b.Kept = []KeptObject{*art, *side}
	return nil
}

// keepVerified stores bytes already in memory after checking their digest.
func (s *Service) keepVerified(ctx context.Context, tenantID uuid.UUID, kind string, data []byte, want string) (*KeptObject, *stepError) {
	if int64(len(data)) > s.cfg.ArtifactMaxBytes {
		return nil, tooLarge(kind, int64(len(data)), s.cfg.ArtifactMaxBytes)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, digestMismatch(kind, want, got)
	}
	return s.put(ctx, tenantID, kind, bytes.NewReader(data), int64(len(data)), want, "application/json")
}

// keepDownload streams ref from yantra4d into a temporary file while hashing
// it, bounded by ArtifactMaxBytes, and stores it only when the digest is the
// one declared. A temporary file, not memory: the pod's memory is small and
// geometry can be large.
func (s *Service) keepDownload(ctx context.Context, tenantID uuid.UUID, kind, ref, want, mediaType string, declared *int64) (*KeptObject, *stepError) {
	limit := s.cfg.ArtifactMaxBytes
	if declared != nil && *declared > limit {
		return nil, tooLarge(kind, *declared, limit)
	}
	body, length, err := s.Renderer.OpenArtifact(ctx, ref)
	if err != nil {
		return nil, downloadFailed(kind, err)
	}
	defer func() { _ = body.Close() }()
	if length > limit {
		return nil, tooLarge(kind, length, limit)
	}
	f, err := os.CreateTemp("", "pravara-kept-*")
	if err != nil {
		return nil, retryable("artifact_spool_failed", "no room to hold the %s while checking it: %v", kind, err)
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(body, limit+1))
	switch {
	case n > limit:
		return nil, tooLarge(kind, n, limit)
	case err != nil:
		return nil, retryable("render_download_failed", "downloading the %s from yantra4d: %v", kind, err)
	case length >= 0 && n != length:
		return nil, retryable("render_download_failed", "the %s download ended after %d of %d bytes", kind, n, length)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return nil, digestMismatch(kind, want, got)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, retryable("artifact_spool_failed", "rewinding the %s: %v", kind, err)
	}
	return s.put(ctx, tenantID, kind, f, n, want, mediaType)
}

// put stores verified bytes under their content address.
func (s *Service) put(ctx context.Context, tenantID uuid.UUID, kind string, body io.ReadSeeker, size int64, digest, mediaType string) (*KeptObject, *stepError) {
	key, err := artifactstore.Key(tenantID, digest)
	if err != nil {
		return nil, terminal("render_digest_invalid", "the %s digest: %v", kind, err)
	}
	if _, err := s.Store.Put(ctx, key, body, size, digest, mediaType); err != nil {
		// The full error names the endpoint and bucket: logged here only.
		// What is stored on the dispatch (tenants read it) is scrubbed.
		s.log.WithError(err).WithFields(logrus.Fields{"tenant_id": tenantID, "kind": kind, "sha256": digest}).
			Error("dispatch: keeping bytes in the artifact store failed")
		if errors.Is(err, artifactstore.ErrConflict) {
			return nil, terminal("artifact_store_conflict",
				"the object already stored for this %s (sha256 %s, %d bytes) is not these bytes or has no recorded digest; the kept record needs a look",
				kind, digest, size)
		}
		return nil, retryable("artifact_store_failed", "storing the %s: %s", kind, artifactstore.Describe(err))
	}
	return &KeptObject{Kind: kind, Key: key, SHA256: digest, Bytes: size, MediaType: mediaType, KeptAt: s.now()}, nil
}

func tooLarge(kind string, n, limit int64) *stepError {
	return terminal("render_artifact_too_large", "the %s is at least %d bytes; pravara keeps at most %d (DISPATCH_ARTIFACT_MAX_BYTES)", kind, n, limit)
}

func digestMismatch(kind, want, got string) *stepError {
	return terminal("render_digest_mismatch", "the %s yantra4d served has sha256 %s, not the declared %s", kind, got, want)
}

// downloadFailed classifies a failed download. A download is retried (the
// retry renders again) unless yantra4d refused for good: a deleted artifact
// (404, 410) is retried too, since a new render makes it again.
func downloadFailed(kind string, err error) *stepError {
	var re *remote.Error
	switch {
	case errors.As(err, &re):
		if re.Retryable || re.Status == http.StatusNotFound || re.Status == http.StatusGone {
			return retryable("render_download_failed", "downloading the %s from yantra4d: %v", kind, err)
		}
	case remote.IsRetryable(err):
		return retryable("render_download_failed", "downloading the %s from yantra4d: %v", kind, err)
	}
	return terminal("render_download_failed", "downloading the %s from yantra4d: %v", kind, err)
}
