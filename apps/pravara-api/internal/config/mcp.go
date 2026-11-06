package config

import "github.com/spf13/viper"

// MCPConfig configures the native MCP endpoint, POST /mcp (Streamable HTTP,
// JSON-RPC 2.0), for agents reading the cost ledger's inputs (RFC 0051).
type MCPConfig struct {
	// Enabled mounts POST /mcp (env PRAVARA_MCP_ENABLED). Default false: the
	// route is not registered and answers 404. The owner turns it on; agents
	// never do.
	Enabled bool `mapstructure:"enabled"`
	// RatePerMinute is each Janua client's sustained call rate
	// (env PRAVARA_MCP_RATE_PER_MINUTE).
	RatePerMinute int `mapstructure:"rate_per_minute"`
	// Burst is how many calls a client may make at once before the rate
	// applies (env PRAVARA_MCP_BURST).
	Burst int `mapstructure:"burst"`
}

func setMCPDefaults(v *viper.Viper) {
	v.SetDefault("mcp.enabled", false)
	v.SetDefault("mcp.rate_per_minute", 60)
	v.SetDefault("mcp.burst", 10)
}

func bindMCPEnv(v *viper.Viper) {
	_ = v.BindEnv("mcp.enabled", "PRAVARA_MCP_ENABLED")
	_ = v.BindEnv("mcp.rate_per_minute", "PRAVARA_MCP_RATE_PER_MINUTE")
	_ = v.BindEnv("mcp.burst", "PRAVARA_MCP_BURST")
}
