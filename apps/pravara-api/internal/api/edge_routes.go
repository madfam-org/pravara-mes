package api

import (
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/brokerauth"
)

// enrollmentRateLimit bounds unauthenticated enrollment registrations per
// client address (on top of the global limiter).
var enrollmentRateLimit = middleware.RateLimiterConfig{IPRateLimit: 10, TenantRateLimit: 1000, Burst: 5, Enabled: true}

// registerEdgeRoutes registers the Sparkplug edge-node registry routes.
//
// Public (no credential; the site box has none yet, see NonJWTRoutes):
//
//	POST /v1/edge/enrollments       register a self-generated credential
//	GET  /v1/edge/enrollments/:id   poll the enrollment status
//
// Authenticated (v1):
//
//	GET  /v1/edge/enrollments              pending enrollments (admins)
//	POST /v1/edge/enrollments/approve      people only, admin role
//	GET  /v1/edge/nodes                    edge nodes
//	POST /v1/edge/nodes/:id/disable        people only, admin role
//	GET  /v1/edge/live-state               live state of Sparkplug machines
//	PUT  /v1/machines/:id/sparkplug        attach a machine to an edge node
//	GET  /v1/machines/:id/live-state       one machine's live state
func registerEdgeRoutes(router *gin.Engine, v1 *gin.RouterGroup, database *db.DB, tdb *db.TenantDB, cfg *config.Config, log *logrus.Logger) {
	h := NewEdgeHandler(repositories.NewEdgeRegistryRepository(tdb), database.DB, cfg.Edge, log)
	if h.cfg.CredentialHashCost < brokerauth.DefaultCost {
		log.WithField("cost", h.cfg.CredentialHashCost).Warn("edge credential hash cost below the default")
	}

	public := router.Group("/v1/edge/enrollments")
	public.POST("", middleware.RateLimiterWithConfig(enrollmentRateLimit, log), h.CreateEnrollment)
	public.GET("/:id", h.GetEnrollment)

	edge := v1.Group("/edge")
	edge.GET("/enrollments", middleware.RequireRole("admin"), h.ListEnrollments)
	edge.POST("/enrollments/approve", middleware.RequireHumanCaller(), middleware.RequireRole("admin"), h.ApproveEnrollment)
	edge.GET("/nodes", h.ListEdgeNodes)
	edge.POST("/nodes/:id/disable", middleware.RequireHumanCaller(), middleware.RequireRole("admin"), h.DisableEdgeNode)
	edge.GET("/live-state", h.ListLiveStates)

	v1.PUT("/machines/:id/sparkplug", h.BindSparkplug)
	v1.GET("/machines/:id/live-state", h.GetLiveState)
}
