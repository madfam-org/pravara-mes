package config

import "github.com/spf13/viper"

// EdgeConfig configures the Sparkplug edge-node registry: broker
// authentication for EMQX and edge enrollment.
type EdgeConfig struct {
	// MQTTAuthInternalKey is the shared key EMQX sends with every
	// /v1/mqtt/auth and /v1/mqtt/acl call (env MQTT_AUTH_INTERNAL_KEY, from
	// the Secret). Empty refuses every broker call.
	MQTTAuthInternalKey string `mapstructure:"mqtt_auth_internal_key"`
	// InternalPort is the in-cluster listener for the broker endpoints
	// (env INTERNAL_HTTP_PORT). It is not routed by the public ingress.
	InternalPort int `mapstructure:"internal_port"`
	// EnrollmentTTLSeconds is how long a pending enrollment can be approved.
	EnrollmentTTLSeconds int `mapstructure:"enrollment_ttl_seconds"`
	// MaxPendingEnrollments caps pending enrollments per tenant.
	MaxPendingEnrollments int `mapstructure:"max_pending_enrollments"`
	// CredentialHashCost is the bcrypt cost of stored edge credentials.
	CredentialHashCost int `mapstructure:"credential_hash_cost"`
}

func setEdgeDefaults(v *viper.Viper) {
	v.SetDefault("edge.mqtt_auth_internal_key", "")
	v.SetDefault("edge.internal_port", 4510)
	v.SetDefault("edge.enrollment_ttl_seconds", 900)
	v.SetDefault("edge.max_pending_enrollments", 20)
	v.SetDefault("edge.credential_hash_cost", 12)
}

func bindEdgeEnv(v *viper.Viper) {
	_ = v.BindEnv("edge.mqtt_auth_internal_key", "MQTT_AUTH_INTERNAL_KEY")
	_ = v.BindEnv("edge.internal_port", "INTERNAL_HTTP_PORT")
	_ = v.BindEnv("edge.enrollment_ttl_seconds", "EDGE_ENROLLMENT_TTL_SECONDS")
	_ = v.BindEnv("edge.max_pending_enrollments", "EDGE_MAX_PENDING_ENROLLMENTS")
	_ = v.BindEnv("edge.credential_hash_cost", "EDGE_CREDENTIAL_HASH_COST")
}
