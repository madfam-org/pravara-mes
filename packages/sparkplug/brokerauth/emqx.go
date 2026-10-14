package brokerauth

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

// InternalKeyHeader carries the shared key the broker sends with every
// authentication and authorization call. The value lives in a Kubernetes
// Secret shared by the broker and pravara-api; it is never logged.
const InternalKeyHeader = "X-Pravara-Internal-Key"

// maxBody bounds the request body of a broker call.
const maxBody = 4 << 10

// authRequest is the body template configured on the EMQX HTTP
// authenticator: {"username": "${username}", "password": "${password}",
// "clientid": "${clientid}"}.
type authRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	ClientID string `json:"clientid"`
}

// aclRequest is the body template configured on the EMQX HTTP authorizer:
// {"username": "${username}", "topic": "${topic}", "action": "${action}",
// "clientid": "${clientid}"}.
type aclRequest struct {
	Username string `json:"username"`
	Topic    string `json:"topic"`
	Action   string `json:"action"`
	ClientID string `json:"clientid"`
}

type authResponse struct {
	Result      Decision `json:"result"`
	IsSuperuser bool     `json:"is_superuser"`
}

type aclResponse struct {
	Result Decision `json:"result"`
}

// Handlers serves the EMQX HTTP authentication (Auth) and authorization
// (ACL) calls. Both require InternalKeyHeader to equal InternalKey; with an
// empty InternalKey they refuse every call (503), so a missing Secret can
// never open the broker.
type Handlers struct {
	Authorizer  *Authorizer
	InternalKey string
	Log         *slog.Logger
}

func (h *Handlers) logger() *slog.Logger {
	if h.Log != nil {
		return h.Log
	}
	return slog.Default()
}

// authorized checks the shared key and writes the error response if it fails.
func (h *Handlers) authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if h.InternalKey == "" {
		h.logger().Error("broker auth call refused: internal key not configured")
		http.Error(w, "broker authentication is not configured", http.StatusServiceUnavailable)
		return false
	}
	got := r.Header.Get(InternalKeyHeader)
	if subtle.ConstantTimeCompare([]byte(got), []byte(h.InternalKey)) != 1 {
		h.logger().Warn("broker auth call refused: bad internal key", "remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// Auth serves POST /v1/mqtt/auth.
func (h *Handlers) Auth(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(w, r) {
		return
	}
	var req authRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	d, err := h.Authorizer.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		h.logger().Error("broker authentication failed", "username", req.Username, "error", err)
		http.Error(w, "credential lookup failed", http.StatusInternalServerError)
		return
	}
	if d == Deny {
		h.logger().Warn("broker authentication denied", "username", req.Username, "clientid", req.ClientID)
	}
	writeJSON(w, authResponse{Result: d})
}

// ACL serves POST /v1/mqtt/acl.
func (h *Handlers) ACL(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(w, r) {
		return
	}
	var req aclRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	d, err := h.Authorizer.Authorize(r.Context(), req.Username, sparkplug.ACLAction(req.Action), req.Topic)
	if err != nil {
		h.logger().Error("broker authorization failed", "username", req.Username, "error", err)
		http.Error(w, "credential lookup failed", http.StatusInternalServerError)
		return
	}
	if d == Deny {
		h.logger().Warn("broker authorization denied", "username", req.Username, "action", req.Action, "topic", req.Topic)
	}
	writeJSON(w, aclResponse{Result: d})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
