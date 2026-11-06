package mcp

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/observability"
)

// auditEntry is what one request leaves in the audit line.
type auditEntry struct {
	start      time.Time
	caller     middleware.Caller
	method     string
	tool       string
	outcome    string
	argsSHA256 string
	err        error
}

// AuditMessage is the message of the one structured log line written per
// request that reaches Handle (RFC 0051 §7): client id, tool, outcome,
// latency and the SHA-256 of the arguments, never the arguments.
const AuditMessage = "mcp.call"

// knownMethods bounds the metric's method label.
var knownMethods = map[string]bool{
	"initialize": true, "ping": true, "tools/list": true, "tools/call": true,
	"notifications/initialized": true, "notifications/cancelled": true,
}

func (s *Server) audit(c *gin.Context, a *auditEntry) {
	latency := s.now().Sub(a.start)
	tenantID, _ := middleware.GetTenantID(c)
	fields := logrus.Fields{
		"caller_kind": a.caller.Kind,
		"tenant_id":   tenantID,
		"rpc_method":  a.method,
		"tool":        a.tool,
		"outcome":     a.outcome,
		"latency_ms":  float64(latency.Microseconds()) / 1000,
		"http_status": c.Writer.Status(),
	}
	// A person's subject is not logged: only machine clients are named.
	if a.caller.Kind == middleware.CallerServiceAccount {
		fields["mcp_client_id"] = a.caller.ID
	}
	if a.argsSHA256 != "" {
		fields["args_sha256"] = a.argsSHA256
	}
	entry := s.cfg.Log.WithFields(fields)
	if a.err != nil {
		entry.WithError(a.err).Error(AuditMessage)
	} else {
		entry.Info(AuditMessage)
	}

	method := a.method
	if method != "" && !knownMethods[method] {
		method = "other"
	}
	observability.MCPCallsTotal.WithLabelValues(method, a.tool, a.outcome).Inc()
	observability.MCPCallDuration.WithLabelValues(method, a.tool).Observe(latency.Seconds())
}
