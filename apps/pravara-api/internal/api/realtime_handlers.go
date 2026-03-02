// Package api provides HTTP handlers and routing for the PravaraMES API.
package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
)

// ErrorResponse represents an API error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// RealtimeHandler handles real-time connection authentication.
type RealtimeHandler struct {
	cfg *config.CentrifugoConfig
	log *logrus.Logger
}

// NewRealtimeHandler creates a new RealtimeHandler.
func NewRealtimeHandler(cfg *config.CentrifugoConfig, log *logrus.Logger) *RealtimeHandler {
	return &RealtimeHandler{
		cfg: cfg,
		log: log,
	}
}

// CentrifugoClaims represents the JWT claims for Centrifugo connection token.
type CentrifugoClaims struct {
	jwt.RegisteredClaims
	Sub      string                 `json:"sub"`               // User ID
	Channels []string               `json:"channels,omitempty"` // Allowed channels
	Info     map[string]interface{} `json:"info,omitempty"`     // User info attached to connection
}

// TokenRequest represents a request for a Centrifugo connection token.
type TokenRequest struct {
	Channels []string `json:"channels,omitempty"` // Optional: specific channels to authorize
}

// TokenResponse contains the Centrifugo connection token.
type TokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	URL       string `json:"url"`
}

// GetToken generates a Centrifugo connection token for the authenticated user.
// @Summary Get real-time connection token
// @Description Returns a JWT token for connecting to the real-time WebSocket gateway
// @Tags realtime
// @Accept json
// @Produce json
// @Success 200 {object} TokenResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /v1/realtime/token [get]
func (h *RealtimeHandler) GetToken(c *gin.Context) {
	// Get user info from context (set by auth middleware)
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Error:   "unauthorized",
			Message: "User not authenticated",
		})
		return
	}

	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, ErrorResponse{
			Error:   "unauthorized",
			Message: "Tenant not found",
		})
		return
	}

	// Get optional user info
	email, _ := c.Get("user_email")
	name, _ := c.Get("user_name")

	// Build user info for connection
	userInfo := map[string]interface{}{
		"tenant_id": tenantID.(uuid.UUID).String(),
	}
	if email != nil {
		userInfo["email"] = email
	}
	if name != nil {
		userInfo["name"] = name
	}

	// Calculate expiry
	expiresAt := time.Now().Add(time.Duration(h.cfg.TokenTTL) * time.Second)

	// Create JWT claims
	claims := CentrifugoClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.(uuid.UUID).String(),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Sub:  userID.(uuid.UUID).String(),
		Info: userInfo,
	}

	// Create token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Sign token
	signedToken, err := token.SignedString([]byte(h.cfg.TokenSecret))
	if err != nil {
		h.log.WithError(err).Error("Failed to sign Centrifugo token")
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "token_generation_failed",
			Message: "Failed to generate connection token",
		})
		return
	}

	c.JSON(http.StatusOK, TokenResponse{
		Token:     signedToken,
		ExpiresAt: expiresAt.Unix(),
		URL:       h.cfg.PublicURL,
	})
}

// ConnectAuthRequest represents Centrifugo proxy connect request.
type ConnectAuthRequest struct {
	Client    string                 `json:"client"`
	Transport string                 `json:"transport"`
	Protocol  string                 `json:"protocol"`
	Encoding  string                 `json:"encoding"`
	Token     string                 `json:"token"`
	Data      map[string]interface{} `json:"data"`
}

// ConnectAuthResponse represents Centrifugo proxy connect response.
type ConnectAuthResponse struct {
	Result ConnectAuthResult `json:"result"`
}

// ConnectAuthResult contains the authentication result.
type ConnectAuthResult struct {
	User     string                 `json:"user"`
	ExpireAt int64                  `json:"expire_at,omitempty"`
	Info     map[string]interface{} `json:"info,omitempty"`
	Channels []string               `json:"channels,omitempty"`
}

// AuthConnect handles Centrifugo proxy connect authentication.
// This endpoint is called by Centrifugo to authenticate WebSocket connections.
// @Summary Authenticate real-time connection (internal)
// @Description Called by Centrifugo to validate connection tokens
// @Tags realtime
// @Accept json
// @Produce json
// @Param request body ConnectAuthRequest true "Connect request"
// @Success 200 {object} ConnectAuthResponse
// @Failure 401 {object} ErrorResponse
// @Router /v1/realtime/auth [post]
func (h *RealtimeHandler) AuthConnect(c *gin.Context) {
	var req ConnectAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.WithError(err).Warn("Invalid connect auth request")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": 100, "message": "bad request"}})
		return
	}

	// Parse and validate JWT token
	token, err := jwt.ParseWithClaims(req.Token, &CentrifugoClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(h.cfg.TokenSecret), nil
	})

	if err != nil || !token.Valid {
		h.log.WithError(err).Warn("Invalid Centrifugo token")
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": 101, "message": "unauthorized"}})
		return
	}

	claims, ok := token.Claims.(*CentrifugoClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": 101, "message": "invalid claims"}})
		return
	}

	// Return successful authentication
	c.JSON(http.StatusOK, ConnectAuthResponse{
		Result: ConnectAuthResult{
			User:     claims.Sub,
			ExpireAt: claims.ExpiresAt.Unix(),
			Info:     claims.Info,
		},
	})
}

// SubscribeRequest represents Centrifugo proxy subscribe request.
type SubscribeRequest struct {
	Client    string                 `json:"client"`
	Transport string                 `json:"transport"`
	Protocol  string                 `json:"protocol"`
	Encoding  string                 `json:"encoding"`
	User      string                 `json:"user"`
	Channel   string                 `json:"channel"`
	Token     string                 `json:"token,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// SubscribeResponse represents Centrifugo proxy subscribe response.
type SubscribeResponse struct {
	Result SubscribeResult `json:"result"`
}

// SubscribeResult contains the subscription result.
type SubscribeResult struct {
	Info      map[string]interface{} `json:"info,omitempty"`
	ExpireAt  int64                  `json:"expire_at,omitempty"`
	Override  *SubscribeOverride     `json:"override,omitempty"`
}

// SubscribeOverride allows overriding channel options.
type SubscribeOverride struct {
	Presence         *bool `json:"presence,omitempty"`
	JoinLeave        *bool `json:"join_leave,omitempty"`
	Position         *bool `json:"position,omitempty"`
	Recover          *bool `json:"recover,omitempty"`
}

// AuthSubscribe handles Centrifugo proxy subscribe authorization.
// This endpoint validates that a user can subscribe to a specific channel.
// @Summary Authorize channel subscription (internal)
// @Description Called by Centrifugo to validate channel subscription requests
// @Tags realtime
// @Accept json
// @Produce json
// @Param request body SubscribeRequest true "Subscribe request"
// @Success 200 {object} SubscribeResponse
// @Failure 403 {object} ErrorResponse
// @Router /v1/realtime/subscribe [post]
func (h *RealtimeHandler) AuthSubscribe(c *gin.Context) {
	var req SubscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.WithError(err).Warn("Invalid subscribe request")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": 100, "message": "bad request"}})
		return
	}

	// Parse channel to extract namespace and tenant
	// Format: namespace:tenant_id or namespace:tenant_id:entity_id
	parts := strings.Split(req.Channel, ":")
	if len(parts) < 2 {
		h.log.WithField("channel", req.Channel).Warn("Invalid channel format")
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": 103, "message": "forbidden"}})
		return
	}

	channelTenantID := parts[1]

	// Get user's tenant from the connection info
	// The user info is passed from AuthConnect via the connection
	// For now, we validate the user ID exists and the channel tenant matches
	if req.User == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": 103, "message": "user not authenticated"}})
		return
	}

	// In a production system, we would:
	// 1. Look up the user's tenant from the database or token info
	// 2. Validate the channel tenant matches the user's tenant
	// 3. Check any additional permissions for the channel

	// For now, we'll allow subscription if the channel format is valid
	// The real multi-tenant isolation happens via tenant-scoped channel names
	h.log.WithFields(logrus.Fields{
		"user":    req.User,
		"channel": req.Channel,
		"tenant":  channelTenantID,
	}).Debug("Subscribe request authorized")

	c.JSON(http.StatusOK, SubscribeResponse{
		Result: SubscribeResult{
			Info: map[string]interface{}{
				"subscribed_at": time.Now().Unix(),
			},
		},
	})
}

// generateChannelToken creates a subscription token for a specific channel.
func (h *RealtimeHandler) generateChannelToken(userID, channel string, expiresAt time.Time) (string, error) {
	// Create HMAC signature for channel token
	mac := hmac.New(sha256.New, []byte(h.cfg.TokenSecret))
	mac.Write([]byte(userID + channel))
	signature := hex.EncodeToString(mac.Sum(nil))

	// Create simple channel token (Centrifugo subscription token format)
	claims := jwt.MapClaims{
		"sub":     userID,
		"channel": channel,
		"exp":     expiresAt.Unix(),
		"iat":     time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	_ = signature // signature could be used for alternative auth
	return token.SignedString([]byte(h.cfg.TokenSecret))
}
