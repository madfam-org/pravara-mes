package main

import (
	"testing"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/config"
)

func TestSparkplugClientConfig(t *testing.T) {
	ok := config.SparkplugConfig{BrokerURL: "ssl://broker.example.test:8883", Username: "pravara-mes-host", TLSServerName: "broker.example.test"}
	cc, err := sparkplugClientConfig(ok)
	if err != nil || cc.TLSConfig == nil || cc.TLSConfig.ServerName != "broker.example.test" {
		t.Fatalf("ssl config: %+v %v", cc, err)
	}
	for _, bad := range []config.SparkplugConfig{
		{BrokerURL: "ssl://b:8883"},                                        // no host credential
		{BrokerURL: "http://b:8883", Username: "u"},                        // unsupported scheme
		{BrokerURL: "ssl://b:8883", Username: "u", CAFile: "/nonexistent"}, // unreadable CA
	} {
		if _, err := sparkplugClientConfig(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}
