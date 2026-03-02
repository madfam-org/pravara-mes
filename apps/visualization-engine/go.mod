module github.com/madfam-org/pravara-mes/apps/visualization-engine

go 1.24.0

require (
	github.com/gin-gonic/gin v1.10.0
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.1
	github.com/lib/pq v1.11.2
	github.com/madfam-org/pravara-mes/packages/sdk-go v0.0.0-00010101000000-000000000000
	github.com/redis/go-redis/v9 v9.7.1
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/viper v1.19.0
)

replace github.com/madfam-org/pravara-mes/packages/sdk-go => ../../packages/sdk-go