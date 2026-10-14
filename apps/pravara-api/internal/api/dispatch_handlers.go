package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

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
}

// NewDispatchHandler creates the handler.
func NewDispatchHandler(svc *dispatch.Service, d *repositories.DispatchRepository, p *repositories.PassportRepository, log *logrus.Logger) *DispatchHandler {
	return &DispatchHandler{svc: svc, dispatch: d, passports: p, log: log}
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
