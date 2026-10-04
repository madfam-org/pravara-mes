package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/brokerauth"
)

// EdgeHandler serves Sparkplug edge-node enrollment, the edge-node registry,
// machine Sparkplug bindings and machine live state.
//
// Enrollment needs no secret held by a person: the site box generates its
// own MQTT password, registers it (pravara stores only a bcrypt hash) under
// a short non-secret user code, a tenant admin approves that code through a
// people-only route, and the box polls until it is active.
type EdgeHandler struct {
	repo *repositories.EdgeRegistryRepository
	pool *sql.DB
	cfg  config.EdgeConfig
	log  *logrus.Logger
	now  func() time.Time
}

// NewEdgeHandler creates the handler. pool is used for the scopes the
// public enrollment routes open themselves.
func NewEdgeHandler(repo *repositories.EdgeRegistryRepository, pool *sql.DB, cfg config.EdgeConfig, log *logrus.Logger) *EdgeHandler {
	if cfg.CredentialHashCost == 0 {
		cfg.CredentialHashCost = brokerauth.DefaultCost
	}
	if cfg.EnrollmentTTLSeconds <= 0 {
		cfg.EnrollmentTTLSeconds = 900
	}
	if cfg.MaxPendingEnrollments <= 0 {
		cfg.MaxPendingEnrollments = 20
	}
	return &EdgeHandler{repo: repo, pool: pool, cfg: cfg, log: log, now: time.Now}
}

// userCodeAlphabet has no vowels (no words) and no look-alike characters.
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

// newUserCode returns a code such as "BCDF-GHJK" (20^8 combinations).
func newUserCode() (string, error) {
	b := make([]byte, 0, 9)
	for i := 0; i < 8; i++ {
		if i == 4 {
			b = append(b, '-')
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(userCodeAlphabet))))
		if err != nil {
			return "", err
		}
		b = append(b, userCodeAlphabet[n.Int64()])
	}
	return string(b), nil
}

// normalizeUserCode accepts "bcdf ghjk", "BCDFGHJK" or "BCDF-GHJK".
func normalizeUserCode(s string) string {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(s)))
	if len(s) != 8 {
		return s
	}
	return s[:4] + "-" + s[4:]
}

type createEnrollmentRequest struct {
	GroupID    string `json:"group_id" binding:"required"`
	EdgeNodeID string `json:"edge_node_id" binding:"required"`
	Password   string `json:"password" binding:"required"`
}

// CreateEnrollment handles POST /v1/edge/enrollments (public; called by the
// site box, which has no credential yet).
func (h *EdgeHandler) CreateEnrollment(c *gin.Context) {
	var req createEnrollmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "group_id, edge_node_id and password are required"})
		return
	}
	if _, err := sparkplug.EdgeNodeACL(req.GroupID, req.EdgeNodeID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "group_id or edge_node_id is not a valid Sparkplug id"})
		return
	}
	hash, err := brokerauth.HashPassword(req.Password, h.cfg.CredentialHashCost)
	if errors.Is(err, brokerauth.ErrWeakPassword) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
		return
	}
	if err != nil {
		h.fail(c, err, "hash credential")
		return
	}

	ctx := c.Request.Context()
	var tenantID uuid.UUID
	err = db.RunInSystemScope(ctx, h.pool, "edge.enrollment.group", func(ctx context.Context) error {
		var err error
		tenantID, err = h.repo.TenantIDBySlug(ctx, req.GroupID)
		return err
	})
	if err != nil {
		h.fail(c, err, "resolve group")
		return
	}
	if tenantID == uuid.Nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown_group", "message": "group_id is not a tenant"})
		return
	}

	e := repositories.EdgeEnrollment{
		TenantID: tenantID, EdgeNodeID: req.EdgeNodeID, PasswordHash: hash,
		MQTTUsername: sparkplug.EdgeNodeUsername(req.GroupID, req.EdgeNodeID),
		ExpiresAt:    h.now().Add(time.Duration(h.cfg.EnrollmentTTLSeconds) * time.Second).UTC(),
	}
	for attempt := 0; ; attempt++ {
		if e.UserCode, err = newUserCode(); err != nil {
			h.fail(c, err, "user code")
			return
		}
		err = db.RunInTenantTx(ctx, h.pool, tenantID.String(), func(ctx context.Context) error {
			return h.repo.CreateEnrollment(ctx, &e, h.cfg.MaxPendingEnrollments)
		})
		if !errors.Is(err, repositories.ErrUserCodeTaken) || attempt == 4 {
			break
		}
	}
	switch {
	case errors.Is(err, repositories.ErrTooManyPending):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too_many_pending", "message": "too many enrollments are waiting for approval"})
		return
	case err != nil:
		h.fail(c, err, "create enrollment")
		return
	}
	h.log.WithFields(logrus.Fields{"tenant_id": tenantID, "edge_node_id": e.EdgeNodeID, "enrollment_id": e.ID}).
		Info("edge enrollment pending approval")
	c.JSON(http.StatusCreated, gin.H{
		"enrollment_id":         e.ID,
		"status":                e.Status,
		"user_code":             e.UserCode,
		"group_id":              req.GroupID,
		"edge_node_id":          e.EdgeNodeID,
		"mqtt_username":         e.MQTTUsername,
		"expires_at":            e.ExpiresAt,
		"poll_interval_seconds": 5,
	})
}

// GetEnrollment handles GET /v1/edge/enrollments/:id (public; the box polls
// it). It returns the status only, never credential material.
func (h *EdgeHandler) GetEnrollment(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	var e *repositories.EdgeEnrollment
	err = db.RunInSystemScope(c.Request.Context(), h.pool, "edge.enrollment.poll", func(ctx context.Context) error {
		var err error
		e, err = h.repo.GetEnrollmentStatus(ctx, id)
		return err
	})
	if errors.Is(err, repositories.ErrEnrollmentNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	if err != nil {
		h.fail(c, err, "get enrollment")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"enrollment_id": e.ID, "status": e.Status, "group_id": e.GroupID, "edge_node_id": e.EdgeNodeID,
		"mqtt_username": e.MQTTUsername, "expires_at": e.ExpiresAt,
	})
}

// ListEnrollments handles GET /v1/edge/enrollments (tenant admins).
func (h *EdgeHandler) ListEnrollments(c *gin.Context) {
	list, err := h.repo.ListPendingEnrollments(c.Request.Context())
	if err != nil {
		h.fail(c, err, "list enrollments")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

type approveEnrollmentRequest struct {
	UserCode   string `json:"user_code" binding:"required"`
	EdgeNodeID string `json:"edge_node_id" binding:"required"`
}

// ApproveEnrollment handles POST /v1/edge/enrollments/approve: a person (not
// a machine credential) with the admin role approves the code shown by the
// box. Only the approving tenant's enrollments can match.
func (h *EdgeHandler) ApproveEnrollment(c *gin.Context) {
	var req approveEnrollmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "user_code and edge_node_id are required"})
		return
	}
	caller, _ := middleware.GetCaller(c)
	node, err := h.repo.ApproveEnrollment(c.Request.Context(), normalizeUserCode(req.UserCode), req.EdgeNodeID, caller.Actor())
	switch {
	case errors.Is(err, repositories.ErrEnrollmentNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "no pending enrollment with this code"})
	case errors.Is(err, repositories.ErrEnrollmentMismatch):
		c.JSON(http.StatusConflict, gin.H{"error": "edge_node_mismatch", "message": "the code belongs to another edge node"})
	case errors.Is(err, repositories.ErrEnrollmentExpired):
		c.JSON(http.StatusGone, gin.H{"error": "expired", "message": "the enrollment expired; restart enrollment on the box"})
	case err != nil:
		h.fail(c, err, "approve enrollment")
	default:
		h.log.WithFields(logrus.Fields{"edge_node_id": node.EdgeNodeID, "approved_by": caller.Actor()}).Info("edge enrollment approved")
		c.JSON(http.StatusOK, gin.H{"data": node})
	}
}

// ListEdgeNodes handles GET /v1/edge/nodes.
func (h *EdgeHandler) ListEdgeNodes(c *gin.Context) {
	list, err := h.repo.ListEdgeNodes(c.Request.Context())
	if err != nil {
		h.fail(c, err, "list edge nodes")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// DisableEdgeNode handles POST /v1/edge/nodes/:id/disable (people only):
// the credential is revoked; re-enrollment issues a new one.
func (h *EdgeHandler) DisableEdgeNode(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "invalid id"})
		return
	}
	node, err := h.repo.DisableEdgeNode(c.Request.Context(), id)
	if errors.Is(err, repositories.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	if err != nil {
		h.fail(c, err, "disable edge node")
		return
	}
	caller, _ := middleware.GetCaller(c)
	h.log.WithFields(logrus.Fields{"edge_node_id": node.EdgeNodeID, "disabled_by": caller.Actor()}).Warn("edge node credential disabled")
	c.JSON(http.StatusOK, gin.H{"data": node})
}

type bindSparkplugRequest struct {
	// EdgeNodeID is the site edge node (site-<slug>); null detaches.
	EdgeNodeID *string `json:"edge_node_id"`
}

// BindSparkplug handles PUT /v1/machines/:id/sparkplug: pre-registers the
// machine as a Sparkplug device of an edge node (device_id = machine code).
func (h *EdgeHandler) BindSparkplug(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "invalid machine id"})
		return
	}
	var req bindSparkplugRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "edge_node_id must be a string or null"})
		return
	}
	ctx := c.Request.Context()
	if req.EdgeNodeID != nil {
		if err := sparkplug.ValidateID("edge_node_id", *req.EdgeNodeID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "edge_node_id is not a valid Sparkplug id"})
			return
		}
		code, err := h.repo.MachineCode(ctx, id)
		if errors.Is(err, repositories.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		if err != nil {
			h.fail(c, err, "machine code")
			return
		}
		if err := sparkplug.ValidateID("device_id", code); err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_device_id", "message": "the machine code cannot be a Sparkplug device_id"})
			return
		}
	}
	code, err := h.repo.BindMachine(ctx, id, req.EdgeNodeID)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
	case errors.Is(err, repositories.ErrSparkplugDeviceTaken):
		c.JSON(http.StatusConflict, gin.H{"error": "conflict", "message": err.Error()})
	case err != nil:
		h.fail(c, err, "bind machine")
	default:
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"machine_id": id, "device_id": code, "sparkplug_edge_id": req.EdgeNodeID}})
	}
}

// GetLiveState handles GET /v1/machines/:id/live-state.
func (h *EdgeHandler) GetLiveState(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "invalid machine id"})
		return
	}
	s, err := h.repo.GetLiveState(c.Request.Context(), id)
	if errors.Is(err, repositories.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	if err != nil {
		h.fail(c, err, "get live state")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": s})
}

// ListLiveStates handles GET /v1/edge/live-state: every Sparkplug-registered
// machine of the tenant with its live state.
func (h *EdgeHandler) ListLiveStates(c *gin.Context) {
	list, err := h.repo.ListLiveStates(c.Request.Context())
	if err != nil {
		h.fail(c, err, "list live states")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *EdgeHandler) fail(c *gin.Context, err error, op string) {
	h.log.WithError(err).WithField("op", op).Error("edge registry request failed")
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "request failed"})
}
