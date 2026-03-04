package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

// APIKeyHandler handles API key management endpoints.
type APIKeyHandler struct {
	repo *repositories.APIKeyRepository
	log  *logrus.Logger
}

// NewAPIKeyHandler creates a new API key handler.
func NewAPIKeyHandler(repo *repositories.APIKeyRepository, log *logrus.Logger) *APIKeyHandler {
	return &APIKeyHandler{repo: repo, log: log}
}

type createAPIKeyRequest struct {
	Name      string   `json:"name" binding:"required"`
	Scopes    []string `json:"scopes"`
	RateLimit *int     `json:"rate_limit,omitempty"`
	ExpiresIn *int     `json:"expires_in_days,omitempty"` // days until expiration
}

type createAPIKeyResponse struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Key       string     `json:"key"` // Only returned once at creation
	KeyPrefix string     `json:"key_prefix"`
	Scopes    []string   `json:"scopes"`
	RateLimit int        `json:"rate_limit"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Create generates a new API key. The raw key is returned only once.
func (h *APIKeyHandler) Create(c *gin.Context) {
	var req createAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
		return
	}

	tenantID, ok := middleware.GetTenantID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	userID, _ := middleware.GetUserID(c)

	// Generate random API key: prv_ + 32 random bytes (hex)
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		h.log.WithError(err).Error("Failed to generate random bytes for API key")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to generate API key"})
		return
	}

	rawKey := "prv_" + hex.EncodeToString(randomBytes)
	keyPrefix := rawKey[:12] // prv_ + 8 hex chars

	// Hash for storage
	hash := sha256.Sum256([]byte(rawKey))
	keyHash := fmt.Sprintf("%x", hash)

	// Parse tenant and user IDs
	tid, err := uuid.Parse(tenantID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_tenant_id"})
		return
	}

	uid, _ := uuid.Parse(userID)

	// Set defaults
	scopes := req.Scopes
	if scopes == nil {
		scopes = []string{"read:events", "read:feeds"}
	}
	rateLimit := 1000
	if req.RateLimit != nil {
		rateLimit = *req.RateLimit
	}

	var expiresAt *time.Time
	if req.ExpiresIn != nil {
		t := time.Now().AddDate(0, 0, *req.ExpiresIn)
		expiresAt = &t
	}

	key := &repositories.APIKey{
		TenantID:  tid,
		Name:      req.Name,
		KeyHash:   keyHash,
		KeyPrefix: keyPrefix,
		Scopes:    scopes,
		RateLimit: rateLimit,
		IsActive:  true,
		ExpiresAt: expiresAt,
		CreatedBy: &uid,
	}

	if err := h.repo.Create(c.Request.Context(), key); err != nil {
		h.log.WithError(err).Error("Failed to create API key")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to create API key"})
		return
	}

	c.JSON(http.StatusCreated, createAPIKeyResponse{
		ID:        key.ID,
		Name:      key.Name,
		Key:       rawKey,
		KeyPrefix: key.KeyPrefix,
		Scopes:    key.Scopes,
		RateLimit: key.RateLimit,
		ExpiresAt: key.ExpiresAt,
		CreatedAt: key.CreatedAt,
	})
}

// List returns all API keys for the current tenant (key values are not returned).
func (h *APIKeyHandler) List(c *gin.Context) {
	keys, err := h.repo.List(c.Request.Context())
	if err != nil {
		h.log.WithError(err).Error("Failed to list API keys")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to list API keys"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"api_keys": keys})
}

// Revoke deactivates an API key.
func (h *APIKeyHandler) Revoke(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id", "message": "Invalid API key ID"})
		return
	}

	if err := h.repo.Revoke(c.Request.Context(), id); err != nil {
		if err == repositories.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "API key not found"})
			return
		}
		h.log.WithError(err).Error("Failed to revoke API key")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to revoke API key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "API key revoked"})
}
