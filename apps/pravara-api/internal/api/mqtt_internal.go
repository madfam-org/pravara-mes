package api

import (
	"context"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/brokerauth"
)

// systemScopeCredentials looks broker credentials up in the read-only system
// scope: the broker knows a username, not a tenant.
type systemScopeCredentials struct {
	database *db.DB
	repo     *repositories.EdgeRegistryRepository
}

func (s systemScopeCredentials) LookupCredential(ctx context.Context, username string) (*brokerauth.Credential, error) {
	var cred *brokerauth.Credential
	err := db.RunInSystemScope(ctx, s.database.DB, "mqtt.credential_lookup", func(ctx context.Context) error {
		var err error
		cred, err = s.repo.LookupCredential(ctx, username)
		return err
	})
	return cred, err
}

// NewInternalRouter returns the in-cluster router that serves the EMQX HTTP
// authentication and authorization calls (MES-1 §3):
//
//	POST /v1/mqtt/auth   username + password  -> allow | deny | ignore
//	POST /v1/mqtt/acl    username + action + topic -> allow | deny | ignore
//
// It listens on its own port (config edge.internal_port), which no public
// ingress or tunnel routes to, and every call must carry the shared key from
// the Secret (header X-Pravara-Internal-Key). Without a configured key every
// call is refused. These routes take no JWT and no API key; they are listed
// in NonJWTRoutes.
func NewInternalRouter(database *db.DB, cfg *config.Config, log *logrus.Logger) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	repo := repositories.NewEdgeRegistryRepository(database.Tenant(log))
	handlers := &brokerauth.Handlers{
		Authorizer:  &brokerauth.Authorizer{Store: systemScopeCredentials{database: database, repo: repo}},
		InternalKey: cfg.Edge.MQTTAuthInternalKey,
		Log:         slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("component", "mqtt_auth"),
	}
	if cfg.Edge.MQTTAuthInternalKey == "" {
		log.Warn("MQTT_AUTH_INTERNAL_KEY is not set: broker authentication calls are refused")
	}

	router.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	router.POST("/v1/mqtt/auth", gin.WrapF(handlers.Auth))
	router.POST("/v1/mqtt/acl", gin.WrapF(handlers.ACL))
	return router
}
