package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/artifactstore"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
)

func storeConfig() config.ArtifactStoreConfig {
	return config.ArtifactStoreConfig{Backend: "r2", EndpointURL: "https://account.r2.example.test",
		BucketName: "pravara-dispatch-artifacts", AccessKeyID: "placeholder-id", SecretAccessKey: "placeholder-secret-value"}
}

// RFC 0059 S12: pravara does not dispatch bytes it cannot keep. With
// dispatch enabled, a missing or unusable artifact store refuses to build the
// service (main exits, the previous pod keeps serving); with dispatch
// disabled it changes nothing.
func TestDispatchWiringRefusesToDispatchWithoutTheArtifactStore(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	build := func(enabled bool, store config.ArtifactStoreConfig) error {
		cfg := &config.Config{}
		cfg.Dispatch.Enabled = enabled
		cfg.ArtifactStore = store
		_, err := BuildDispatchService(cfg, nil, nil, log)
		return err
	}

	t.Run("enabled, nothing configured", func(t *testing.T) {
		err := build(true, config.ArtifactStoreConfig{})
		require.Error(t, err)
		for _, want := range []string{"STORAGE_BACKEND", "R2_ENDPOINT_URL", "R2_BUCKET_NAME", "R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY",
			"pravara-dispatch-artifacts", "enclii buckets create"} {
			assert.Contains(t, err.Error(), want)
		}
	})

	broken := map[string]func(c *config.ArtifactStoreConfig){
		"no secret key":     func(c *config.ArtifactStoreConfig) { c.SecretAccessKey = "" },
		"no bucket":         func(c *config.ArtifactStoreConfig) { c.BucketName = "" },
		"another backend":   func(c *config.ArtifactStoreConfig) { c.Backend = "local" },
		"clear-text remote": func(c *config.ArtifactStoreConfig) { c.EndpointURL = "http://account.r2.example.test" },
	}
	for name, mutate := range broken {
		t.Run("enabled, "+name, func(t *testing.T) {
			c := storeConfig()
			mutate(&c)
			err := build(true, c)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "dispatch is enabled")
			assert.NotContains(t, err.Error(), "placeholder-secret-value", "the secret never reaches a log line")
		})
		t.Run("disabled, "+name, func(t *testing.T) {
			c := storeConfig()
			mutate(&c)
			assert.NoError(t, build(false, c), "dispatch is off: the store is not needed")
		})
	}

	t.Run("disabled, nothing configured", func(t *testing.T) {
		cfg := &config.Config{}
		svc, err := BuildDispatchService(cfg, nil, nil, log)
		require.NoError(t, err)
		assert.Nil(t, svc.Store)
		assert.Contains(t, svc.Unavailable(), "DISPATCH_ENABLED")
	})

	t.Run("enabled, fully configured", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Dispatch.Enabled = true
		cfg.ArtifactStore = storeConfig()
		svc, err := buildDispatchService(cfg, nil, nil, nil, log)
		require.NoError(t, err, "building the store makes no call to the bucket")
		assert.NotNil(t, svc.Store)
	})

	t.Run("enabled, the bucket fails the start-up self-check", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Dispatch.Enabled = true
		cfg.ArtifactStore = storeConfig() // an endpoint that does not resolve
		_, err := BuildDispatchService(cfg, nil, nil, log)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dispatch is enabled but artifact store self-check")
		assert.NotContains(t, err.Error(), "placeholder-secret-value", "the secret never reaches a log line")
	})

	t.Run("a negative size bound is refused", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Dispatch.ArtifactMaxBytes = -1
		_, err := BuildDispatchService(cfg, nil, nil, log)
		assert.ErrorContains(t, err, "artifact_max_bytes")
	})
}

type fakeChecker struct {
	artifactstore.Store
	err   error
	calls chan struct{}
}

func (f *fakeChecker) SelfCheck(context.Context) error {
	if f.calls != nil {
		f.calls <- struct{}{}
	}
	return f.err
}

// With dispatch enabled, a bucket that fails the self-check stops dispatch
// from being built; one that passes does not.
func TestArtifactSelfCheckGatesEnabledDispatch(t *testing.T) {
	log := logrus.New()
	err := checkArtifactStore(context.Background(), &fakeChecker{Store: artifactstore.NewMemory(), err: errors.New("If-None-Match is not enforced")}, true, log)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dispatch is enabled")
	assert.Contains(t, err.Error(), "If-None-Match is not enforced")

	require.NoError(t, checkArtifactStore(context.Background(), &fakeChecker{Store: artifactstore.NewMemory()}, true, log))
}

// With dispatch disabled the self-check still runs (so the bucket is proven
// before anyone enables dispatch) but never stops the API from starting.
func TestArtifactSelfCheckOnlyLogsWhileDispatchIsDisabled(t *testing.T) {
	calls := make(chan struct{}, 1)
	err := checkArtifactStore(context.Background(), &fakeChecker{Store: artifactstore.NewMemory(), err: errors.New("boom"), calls: calls}, false, logrus.New())
	require.NoError(t, err)
	select {
	case <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("the self-check did not run while dispatch was disabled")
	}
}

// A store with no self-check (the in-memory store, no store at all) passes.
func TestArtifactSelfCheckSkipsStoresWithoutOne(t *testing.T) {
	require.NoError(t, checkArtifactStore(context.Background(), artifactstore.NewMemory(), true, logrus.New()))
	require.NoError(t, checkArtifactStore(context.Background(), nil, true, logrus.New()))
}
