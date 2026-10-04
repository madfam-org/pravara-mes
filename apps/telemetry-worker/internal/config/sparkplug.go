package config

import "github.com/spf13/viper"

// SparkplugConfig configures the Sparkplug B primary host application
// (packages/sparkplug/host). It is off by default; enabling it needs the
// host's own broker credential (never an edge-node credential).
//
// Environment: PRAVARA_SPARKPLUG_ENABLED, PRAVARA_SPARKPLUG_BROKER_URL,
// PRAVARA_SPARKPLUG_CLIENT_ID, PRAVARA_SPARKPLUG_USERNAME,
// PRAVARA_SPARKPLUG_PASSWORD, PRAVARA_SPARKPLUG_CA_FILE,
// PRAVARA_SPARKPLUG_TLS_SERVER_NAME, PRAVARA_SPARKPLUG_HOST_ID.
type SparkplugConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// BrokerURL is ssl://host:port (tcp:// is accepted for an in-cluster
	// listener that is not reachable from outside the cluster).
	BrokerURL string `mapstructure:"broker_url"`
	ClientID  string `mapstructure:"client_id"`
	Username  string `mapstructure:"username"`
	Password  string `mapstructure:"password"`
	// CAFile is a PEM bundle for the broker certificate (system roots when empty).
	CAFile        string `mapstructure:"ca_file"`
	TLSServerName string `mapstructure:"tls_server_name"`
	// HostID is the Sparkplug host application id (STATE topic).
	HostID string `mapstructure:"host_id"`
}

func setSparkplugDefaults(v *viper.Viper) {
	v.SetDefault("sparkplug.enabled", false)
	v.SetDefault("sparkplug.broker_url", "")
	v.SetDefault("sparkplug.client_id", "pravara-mes-host")
	v.SetDefault("sparkplug.username", "")
	v.SetDefault("sparkplug.password", "")
	v.SetDefault("sparkplug.ca_file", "")
	v.SetDefault("sparkplug.tls_server_name", "")
	v.SetDefault("sparkplug.host_id", "pravara-mes")
}
