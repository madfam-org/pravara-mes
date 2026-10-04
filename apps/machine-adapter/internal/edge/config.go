// Package edge runs the machine adapter as a Sparkplug B Edge Node (MES-1 §2):
// one edge node per site box, the site's printers as Sparkplug devices.
package edge

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

// Config is the edge node configuration (the `edge:` section of the config file).
// Secrets are never inline: they are read from the files named by *_file.
type Config struct {
	Enabled       bool   `mapstructure:"enabled"`
	GroupID       string `mapstructure:"group_id"`        // pravara tenant slug
	EdgeNodeID    string `mapstructure:"edge_node_id"`    // site-<slug>
	PrimaryHostID string `mapstructure:"primary_host_id"` // "" = do not wait for a host

	// BrokerURL is the MQTT endpoint, e.g. ssl://127.0.0.1:8883 when
	// `cloudflared access tcp` provides a local listener for the broker.
	BrokerURL         string `mapstructure:"broker_url"`
	TLSCAFile         string `mapstructure:"tls_ca_file"`     // CA bundle for the broker certificate ("" = system roots)
	TLSServerName     string `mapstructure:"tls_server_name"` // broker certificate name when dialing a local endpoint
	TLSClientCertFile string `mapstructure:"tls_client_cert_file"`
	TLSClientKeyFile  string `mapstructure:"tls_client_key_file"`
	// AllowPlaintextBroker permits tcp:// brokers on loopback only (tests and simulators).
	AllowPlaintextBroker bool   `mapstructure:"allow_plaintext_broker"`
	ClientID             string `mapstructure:"client_id"` // defaults to edge_node_id
	Username             string `mapstructure:"username"`
	PasswordFile         string `mapstructure:"password_file"`

	StateDir         string        `mapstructure:"state_dir"` // bdSeq + artifact scratch
	PollInterval     time.Duration `mapstructure:"poll_interval"`
	OfflineAfter     int           `mapstructure:"offline_after"` // failed polls before DDEATH
	ReconnectDelay   time.Duration `mapstructure:"reconnect_delay"`
	ArtifactMaxBytes int64         `mapstructure:"artifact_max_bytes"`
	ArtifactTimeout  time.Duration `mapstructure:"artifact_timeout"`
	JobStartTimeout  time.Duration `mapstructure:"job_start_timeout"`

	// Simulate replaces every device with a local protocol simulator. No
	// printer is contacted.
	Simulate bool `mapstructure:"simulate"`

	Devices []DeviceConfig `mapstructure:"devices"`
}

// DeviceConfig declares one printer at the site.
type DeviceConfig struct {
	DeviceID     string `mapstructure:"device_id"`  // pravara machine code
	Definition   string `mapstructure:"definition"` // registry id, e.g. voron_2_4, bambu_a1
	Protocol     string `mapstructure:"protocol"`   // moonraker | bambu_mqtt (defaults from the definition)
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	FTPSPort     int    `mapstructure:"ftps_port"`
	Serial       string `mapstructure:"serial"`
	SecretFile   string `mapstructure:"secret_file"` // Moonraker API key or Bambu access code
	TLSPinSHA256 string `mapstructure:"tls_pin_sha256"`

	Model         string `mapstructure:"model"`
	Firmware      string `mapstructure:"firmware"`
	MaterialSlots int    `mapstructure:"material_slots"`
	// Capabilities override or extend the fabrication-capabilities derived from the definition.
	Capabilities map[string]interface{} `mapstructure:"capabilities"`
}

// ApplyDefaults fills unset tunables.
func (c *Config) ApplyDefaults() {
	if c.PrimaryHostID == "" {
		c.PrimaryHostID = sparkplug.PrimaryHostID
	}
	if c.ClientID == "" {
		c.ClientID = c.EdgeNodeID
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
	if c.OfflineAfter <= 0 {
		c.OfflineAfter = 3
	}
	if c.ReconnectDelay <= 0 {
		c.ReconnectDelay = 5 * time.Second
	}
	if c.ArtifactMaxBytes <= 0 {
		c.ArtifactMaxBytes = 512 << 20
	}
	if c.ArtifactTimeout <= 0 {
		c.ArtifactTimeout = 10 * time.Minute
	}
	if c.JobStartTimeout <= 0 {
		c.JobStartTimeout = 2 * time.Minute
	}
}

// Validate checks identities, the broker endpoint and devices.
func (c *Config) Validate() error {
	if _, err := sparkplug.NodeTopic(c.GroupID, sparkplug.NBIRTH, c.EdgeNodeID); err != nil {
		return fmt.Errorf("edge identity: %w", err)
	}
	if c.PrimaryHostID != "none" {
		if err := sparkplug.ValidateID("primary_host_id", c.PrimaryHostID); err != nil {
			return err
		}
	}
	u, err := url.Parse(c.BrokerURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("broker_url %q is not a URL", c.BrokerURL)
	}
	switch u.Scheme {
	case "ssl", "tls", "mqtts":
	case "tcp", "mqtt":
		host := u.Hostname()
		if !c.AllowPlaintextBroker || (host != "127.0.0.1" && host != "::1" && host != "localhost") {
			return fmt.Errorf("broker_url must use TLS (ssl://); plaintext is allowed only on loopback with allow_plaintext_broker")
		}
	default:
		return fmt.Errorf("broker_url scheme %q is not supported", u.Scheme)
	}
	if (c.TLSClientCertFile == "") != (c.TLSClientKeyFile == "") {
		return fmt.Errorf("tls_client_cert_file and tls_client_key_file go together")
	}
	if len(c.Devices) == 0 {
		return fmt.Errorf("at least one device is required")
	}
	seen := map[string]bool{}
	for i, d := range c.Devices {
		if err := sparkplug.ValidateID("device_id", d.DeviceID); err != nil {
			return fmt.Errorf("devices[%d]: %w", i, err)
		}
		if seen[d.DeviceID] {
			return fmt.Errorf("devices[%d]: duplicate device_id %q", i, d.DeviceID)
		}
		seen[d.DeviceID] = true
		if d.Definition == "" && d.Protocol == "" {
			return fmt.Errorf("devices[%d]: definition or protocol is required", i)
		}
		if !c.Simulate && d.Host == "" {
			return fmt.Errorf("devices[%d]: host is required", i)
		}
	}
	return nil
}

// WaitsForPrimaryHost reports whether births wait for the primary host's STATE.
func (c *Config) WaitsForPrimaryHost() bool { return c.PrimaryHostID != "none" }

// readSecret reads a secret file, trimming a trailing newline. An empty path yields "".
func readSecret(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read secret file: %w", err)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}
