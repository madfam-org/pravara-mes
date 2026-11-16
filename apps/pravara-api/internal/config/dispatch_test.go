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

	assert.Equal(t, int64(256<<20), cfg.Dispatch.ArtifactMaxBytes)
	assert.Empty(t, cfg.ArtifactStore.Backend, "no artifact store unless its Secret is mounted")

	t.Setenv("DISPATCH_ENABLED", "")
	cfg, err = Load()
	require.NoError(t, err)
	assert.False(t, cfg.Dispatch.Enabled, "dispatch is off unless enabled")
}

// The kept-bytes bucket is read under the exact key names of the Secret
// `enclii buckets create` writes, not the legacy R2_ENDPOINT / R2_BUCKET.
func TestArtifactStoreReadsTheEncliiSecretKeys(t *testing.T) {
	t.Setenv("STORAGE_BACKEND", "r2")
	t.Setenv("R2_ENDPOINT_URL", "https://account.r2.example.test")
	t.Setenv("R2_BUCKET_NAME", "pravara-dispatch-artifacts")
	t.Setenv("R2_ACCESS_KEY_ID", "placeholder-id")
	t.Setenv("R2_SECRET_ACCESS_KEY", "placeholder-secret")
	t.Setenv("R2_ENDPOINT", "https://legacy.example.test")
	t.Setenv("R2_BUCKET", "legacy")
	t.Setenv("DISPATCH_ARTIFACT_MAX_BYTES", "1048576")
	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, ArtifactStoreConfig{Backend: "r2", EndpointURL: "https://account.r2.example.test",
		BucketName: "pravara-dispatch-artifacts", AccessKeyID: "placeholder-id", SecretAccessKey: "placeholder-secret"}, cfg.ArtifactStore)
	assert.Equal(t, int64(1<<20), cfg.Dispatch.ArtifactMaxBytes)
}
