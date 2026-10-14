package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantClientPrefixes(t *testing.T) {
	got, err := MachineClientsConfig{AssetShellsPublisherTenants: " madfam=ASSET_SHELLS_PUBLISHER_MADFAM_ECOSYSTEM ; acme=ASSET_SHELLS_PUBLISHER_ACME,"}.TenantClientPrefixes()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"madfam": "ASSET_SHELLS_PUBLISHER_MADFAM_ECOSYSTEM", "acme": "ASSET_SHELLS_PUBLISHER_ACME"}, got)

	for _, bad := range []string{"madfam", "madfam=lower_case", "=X", "a=X,a=Y"} {
		_, err := MachineClientsConfig{AssetShellsPublisherTenants: bad}.TenantClientPrefixes()
		assert.Error(t, err, bad)
	}
	empty, err := MachineClientsConfig{}.TenantClientPrefixes()
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestDispatchDefaultsAndEnv(t *testing.T) {
	t.Setenv("DISPATCH_ENABLED", "true")
	t.Setenv("FABRICATION_PREP_CLIENT_ID", "jnc_placeholder")
	cfg, err := Load()
	require.NoError(t, err)
	assert.True(t, cfg.Dispatch.Enabled)
	assert.Equal(t, "3mf", cfg.Dispatch.RenderFormat)
	assert.Equal(t, 900, cfg.Dispatch.ReservationTTLSeconds)
	assert.Equal(t, "https://auth.madfam.io/api/v1/oauth/token", cfg.MachineClients.TokenURL)
	assert.Equal(t, "jnc_placeholder", cfg.MachineClients.FabricationPrepClientID)
	assert.Equal(t, "madfam=ASSET_SHELLS_PUBLISHER_MADFAM_ECOSYSTEM", cfg.MachineClients.AssetShellsPublisherTenants)

	t.Setenv("DISPATCH_ENABLED", "")
	cfg, err = Load()
	require.NoError(t, err)
	assert.False(t, cfg.Dispatch.Enabled, "dispatch is off unless enabled")
}
