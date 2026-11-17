package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/artifactstore"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatch"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

// DispatchHandler serves matchmaking (dry run) and fabrication dispatch.
//
// Scope decision (route_scopes.go): POST /v1/match and the reads accept
// pravara-mes:read or pravara-mes:jobs; POST /v1/dispatches is for people
// only (and wildcard API keys), like POST /v1/machines/:id/command, because
// it ends in start_job on a printer. Intake clients hold pravara-mes:jobs to
// file orders, not to start machines.
type DispatchHandler struct {
	svc       *dispatch.Service
	dispatch  *repositories.DispatchRepository
	passports *repositories.PassportRepository
	log       *logrus.Logger
	// artifactReads bounds concurrent kept-bytes downloads per process:
	// each holds a temporary file up to DISPATCH_ARTIFACT_MAX_BYTES and a
	// store connection.
	artifactReads chan struct{}
}

// DefaultArtifactReads is how many kept-bytes downloads one process serves
// at once when nothing else is configured.
const DefaultArtifactReads = 2

// NewDispatchHandler creates the handler.
func NewDispatchHandler(svc *dispatch.Service, d *repositories.DispatchRepository, p *repositories.PassportRepository, log *logrus.Logger) *DispatchHandler {
	return &DispatchHandler{svc: svc, dispatch: d, passports: p, log: log, artifactReads: make(chan struct{}, DefaultArtifactReads)}
}

// WithArtifactReads sets the per-process limit of concurrent kept-bytes
// downloads (n <= 0 keeps the default).
func (h *DispatchHandler) WithArtifactReads(n int) *DispatchHandler {
	if n > 0 {
		h.artifactReads = make(chan struct{}, n)
	}
	return h
}

// artifactWriteDeadline lifts the server's write timeout for one download.
// A variable so tests can observe the call.
var artifactWriteDeadline = func(w http.ResponseWriter, deadline time.Time) error {
	return http.NewResponseController(w).SetWriteDeadline(deadline)
}

// artifactReadBound is how long one kept-bytes download may take, reading
// the object from the store and writing it to the client: two minutes plus
// one second per MiB (at least 1 MiB/s end to end).
func artifactReadBound(size int64) time.Duration {
	return 2*time.Minute + time.Duration(size/(1<<20)+1)*time.Second
}

type taskRef struct {
	TaskID string `json:"task_id" binding:"required"`
}

func (h *DispatchHandler) tenant(c *gin.Context) (uuid.UUID, bool) {
	raw, _ := middleware.GetTenantID(c)
	id, err := uuid.Parse(raw)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden", "message": "no tenant context"})
		return uuid.Nil, false
	}
	return id, true
}

func (h *DispatchHandler) taskID(c *gin.Context) (uuid.UUID, bool) {
	var body taskRef
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "body must be {\"task_id\": \"<uuid>\"}"})
		return uuid.Nil, false
	}
	id, err := uuid.Parse(body.TaskID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "task_id is not a UUID"})
		return uuid.Nil, false
	}
	return id, true
}

// writeError maps classified dispatch errors: unknown task 404, product or
// requirement problems 422, upstream (asset-shells, fabrication-prep)
// unavailable 502.
func (h *DispatchHandler) writeError(c *gin.Context, err error) {
	code, msg, isRetryable, ok := dispatch.ErrorInfo(err)
	switch {
	case !ok:
		h.log.WithError(err).Error("dispatch request failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "dispatch request failed"})
	case code == "task_not_found":
		c.JSON(http.StatusNotFound, gin.H{"error": code, "message": msg})
	case isRetryable:
		c.JSON(http.StatusBadGateway, gin.H{"error": code, "message": msg})
	default:
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": code, "message": msg})
	}
}

// Match is POST /v1/match: rank every machine for a task, reserving nothing.
func (h *DispatchHandler) Match(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	taskID, ok := h.taskID(c)
	if !ok {
		return
	}
	out, err := h.svc.Match(c.Request.Context(), tenantID, taskID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// Create is POST /v1/dispatches: file a dispatch for a task. The runner
// matches, reserves, renders, slices and enqueues start_job.
func (h *DispatchHandler) Create(c *gin.Context) {
	if reason := h.svc.Unavailable(); reason != "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "dispatch_unavailable", "message": reason})
		return
	}
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	taskID, ok := h.taskID(c)
	if !ok {
		return
	}
	var userPtr, actorPtr *uuid.UUID
	if u := middleware.ActorUserUUID(c); u != uuid.Nil {
		userPtr = &u
	}
	if a := middleware.ActorUUID(c); a != uuid.Nil {
		actorPtr = &a
	}
	d, err := h.svc.Request(c.Request.Context(), tenantID, taskID, userPtr, actorPtr)
	if errors.Is(err, repositories.ErrDispatchExists) {
		c.JSON(http.StatusConflict, gin.H{"error": "dispatch_exists", "message": err.Error()})
		return
	}
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Location", "/v1/dispatches/"+d.ID.String())
	c.JSON(http.StatusAccepted, d)
}

// List is GET /v1/dispatches?task_id=&status=&limit=.
func (h *DispatchHandler) List(c *gin.Context) {
	f := repositories.DispatchFilter{Status: c.Query("status")}
	if raw := c.Query("task_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "task_id is not a UUID"})
			return
		}
		f.TaskID = &id
	}
	if n, err := strconv.Atoi(c.Query("limit")); err == nil {
		f.Limit = n
	}
	list, err := h.dispatch.List(c.Request.Context(), f)
	if err != nil {
		h.log.WithError(err).Error("list dispatches")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "failed to list dispatches"})
		return
	}
	if list == nil {
		list = []repositories.DispatchJob{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// Get is GET /v1/dispatches/:id: the dispatch, its manufacturing record and
// the passport deliveries.
func (h *DispatchHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "id is not a UUID"})
		return
	}
	ctx := c.Request.Context()
	d, err := h.dispatch.Get(ctx, id)
	if err != nil {
		h.log.WithError(err).Error("get dispatch")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "failed to read dispatch"})
		return
	}
	if d == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "dispatch not found"})
		return
	}
	out := gin.H{"dispatch": d}
	rec, err := h.passports.RecordByDispatch(ctx, id)
	if err == nil && rec != nil {
		out["manufacturing_record"] = rec
		if deliveries, err := h.passports.Deliveries(ctx, rec.ID); err == nil {
			out["passport_deliveries"] = deliveries
		}
	}
	c.JSON(http.StatusOK, out)
}

// Artifact is GET /v1/dispatches/:id/artifacts/:kind: the bytes pravara kept
// of the dispatch's render (RFC 0059 S12), kind "artifact" (the geometry) or
// "sidecar" (its GOC-1 variables.json). ?sha256= picks one of the kept or
// superseded copies of that kind; without it, the current render's.
//
// Same access as GET /v1/dispatches/:id: the dispatch is read in the
// caller's tenant scope, so another tenant's dispatch is 404. The bytes are
// read back in full and checked against the recorded sha256 and size before
// any of them is sent: a damaged copy is an error, never a download.
func (h *DispatchHandler) Artifact(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "id is not a UUID"})
		return
	}
	kind := c.Param("kind")
	if kind != dispatch.KindArtifact && kind != dispatch.KindSidecar {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "kind is artifact or sidecar"})
		return
	}
	ctx := c.Request.Context()
	d, err := h.dispatch.Get(ctx, id)
	if err != nil {
		h.log.WithError(err).Error("get dispatch")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "failed to read dispatch"})
		return
	}
	if d == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "dispatch not found"})
		return
	}
	kept, ok := keptObject(d, kind, c.Query("sha256"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "artifact_not_kept", "message": "this dispatch has no kept " + kind + " with that digest"})
		return
	}
	// The key is derived again from the dispatch's own tenant, never taken
	// from the record, so a record can only point into its tenant's space.
	key, err := artifactstore.Key(d.TenantID, kept.SHA256)
	if err != nil || key != kept.Key {
		h.log.WithField("dispatch_id", id).Error("dispatch: kept object key does not match its tenant and digest")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "artifact_record_invalid", "message": "the kept record is inconsistent"})
		return
	}
	if h.svc.Store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "artifact_store_unavailable", "message": "the artifact store is not configured"})
		return
	}
	// Everything needed from the database is read: end the request's tenant
	// transaction now, so no pooled connection is held during the store read
	// and the download. The scope (and RLS) stays; nothing else is queried.
	if err := db.ReleaseScope(ctx); err != nil {
		h.log.WithError(err).Error("dispatch: could not end the request transaction before the artifact read")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "failed to read dispatch"})
		return
	}
	select {
	case h.artifactReads <- struct{}{}:
		defer func() { <-h.artifactReads }()
	default:
		c.Header("Retry-After", "5")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "artifact_reads_busy",
			"message": "too many artifact downloads at once; retry shortly"})
		return
	}
	// The server's write timeout is short; this response may be large. Lift
	// it for this response only, to a bound from the object's size, and stop
	// reading the store when the client goes away (the request context).
	bound := artifactReadBound(kept.Bytes)
	if err := artifactWriteDeadline(c.Writer, time.Now().Add(bound)); err != nil {
		h.log.WithError(err).Warn("dispatch: could not lift the write deadline for an artifact download")
	}
	rctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	f, serr := h.verifiedCopy(rctx, key, kept)
	if serr != nil {
		serr(c)
		return
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	c.Header("X-Content-SHA256", kept.SHA256)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s%s"`, kind, kept.SHA256[:16], keptExtension(kept.MediaType)))
	c.Header("Cache-Control", "private, no-store")
	media := kept.MediaType
	if media == "" {
		media = "application/octet-stream"
	}
	c.DataFromReader(http.StatusOK, kept.Bytes, media, f, nil)
}

// keptObject finds a kept object of a dispatch's render bundle.
func keptObject(d *repositories.DispatchJob, kind, digest string) (dispatch.KeptObject, bool) {
	if len(d.RenderBundle) == 0 {
		return dispatch.KeptObject{}, false
	}
	var bundle dispatch.RenderBundle
	if err := json.Unmarshal(d.RenderBundle, &bundle); err != nil {
		return dispatch.KeptObject{}, false
	}
	current := bundle.SHA256
	if kind == dispatch.KindSidecar {
		current = bundle.SidecarSHA256
	}
	if digest == "" {
		digest = current
	}
	for _, k := range append(append([]dispatch.KeptObject(nil), bundle.Kept...), bundle.SupersededKept...) {
		if k.Kind == kind && k.SHA256 == digest {
			return k, true
		}
	}
	return dispatch.KeptObject{}, false
}

// verifiedCopy reads a kept object into a temporary file and checks its size
// and sha256 against the record. On failure it returns the response to send.
func (h *DispatchHandler) verifiedCopy(ctx context.Context, key string, kept dispatch.KeptObject) (*os.File, func(*gin.Context)) {
	fail := func(status int, code, msg string) func(*gin.Context) {
		return func(c *gin.Context) { c.JSON(status, gin.H{"error": code, "message": msg}) }
	}
	body, _, err := h.svc.Store.Open(ctx, key)
	if errors.Is(err, artifactstore.ErrNotFound) {
		h.log.WithField("key", key).Error("dispatch: a kept object is missing from the artifact store")
		return nil, fail(http.StatusInternalServerError, "artifact_missing", "the kept bytes are recorded but not in the store")
	}
	if err != nil {
		h.log.WithError(err).WithField("key", key).Error("dispatch: artifact store read failed")
		return nil, fail(http.StatusBadGateway, "artifact_store_unavailable", "the artifact store could not be read")
	}
	defer func() { _ = body.Close() }()
	f, err := os.CreateTemp("", "pravara-kept-read-*")
	if err != nil {
		h.log.WithError(err).Error("dispatch: no temporary space to check a kept object")
		return nil, fail(http.StatusInternalServerError, "internal_error", "could not check the kept bytes")
	}
	discard := func() { _ = f.Close(); _ = os.Remove(f.Name()) }
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(ctxReader{ctx: ctx, r: body}, kept.Bytes+1))
	if err != nil {
		discard()
		if ctx.Err() != nil {
			h.log.WithError(err).WithField("key", key).Warn("dispatch: artifact read stopped (client gone or time bound reached)")
			return nil, fail(http.StatusServiceUnavailable, "artifact_read_cancelled", "the artifact read was stopped")
		}
		h.log.WithError(err).WithField("key", key).Error("dispatch: artifact store read failed")
		return nil, fail(http.StatusBadGateway, "artifact_store_unavailable", "the artifact store could not be read")
	}
	if got := hex.EncodeToString(sum.Sum(nil)); n != kept.Bytes || got != kept.SHA256 {
		discard()
		h.log.WithFields(logrus.Fields{"key": key, "bytes": n, "want_bytes": kept.Bytes, "sha256": got}).
			Error("dispatch: a kept object does not match its recorded digest")
		return nil, fail(http.StatusInternalServerError, "artifact_digest_mismatch", "the stored bytes do not match the recorded sha256; nothing was sent")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		discard()
		return nil, fail(http.StatusInternalServerError, "internal_error", "could not read the checked bytes")
	}
	return f, nil
}

// ctxReader stops a read as soon as ctx ends, whatever the underlying
// reader does with it.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

func keptExtension(mediaType string) string {
	switch mediaType {
	case "model/3mf":
		return ".3mf"
	case "model/stl":
		return ".stl"
	case "application/json":
		return ".json"
	}
	return ""
}
