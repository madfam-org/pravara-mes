module github.com/madfam-org/pravara-mes/apps/machine-adapter

go 1.24

require (
	github.com/eclipse/paho.mqtt.golang v1.4.3
	github.com/gin-gonic/gin v1.10.0
	github.com/google/uuid v1.6.0
	github.com/madfam-org/pravara-mes/packages/sdk-go v0.0.0
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/viper v1.18.2
)

replace github.com/madfam-org/pravara-mes/packages/sdk-go => ../../packages/sdk-go