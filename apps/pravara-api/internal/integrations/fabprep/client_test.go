package fabprep

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatchfakes"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/remote"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
)

func setup(t *testing.T) (*Client, *dispatchfakes.FabricationPrep) {
	janua := dispatchfakes.NewJanua(t, dispatchfakes.Client{ID: "jnc_f", Secret: "jns_f",
		Scope: machineclients.FabricationPrepScope, Audience: machineclients.FabricationPrepAudience})
	f := dispatchfakes.NewFabricationPrep(t)
	ts := machineclients.NewTokenSource(machineclients.Credentials{Name: "f", TokenURL: janua.TokenURL(),
		ClientID: "jnc_f", ClientSecret: "jns_f", Scope: machineclients.FabricationPrepScope}, nil)
	c, err := NewClient(f.Server.URL, ts, nil)
	require.NoError(t, err)
	return c, f
}

func request() SliceJobRequest {
	return SliceJobRequest{Input: Input{URL: "https://yantra4d.example.test/static/x.3mf", SHA256: strings.Repeat("a", 64),
		MediaType: "model/3mf", Variables: &Ref{URL: "https://yantra4d.example.test/static/x.3mf.variables.json", SHA256: strings.Repeat("b", 64)}},
		PrinterProfile: "klipper-corexy-350-0.4@1", FilamentProfile: "tpu-95a-klipper@1", ProcessProfile: "tpu-safe-0.20-klipper@1",
		Target: TargetKlipperGcode, Part: "body"}
}

func TestSliceJobLifecycleAndIdempotency(t *testing.T) {
	c, f := setup(t)
	ctx := context.Background()
	job, err := c.CreateSliceJob(ctx, request(), "pravara-dispatch:abc:12345678")
	require.NoError(t, err)
	assert.Equal(t, StatusQueued, job.Status)
	again, err := c.CreateSliceJob(ctx, request(), "pravara-dispatch:abc:12345678")
	require.NoError(t, err)
	assert.Equal(t, job.ID, again.ID, "same key and body: the same job")
	assert.Equal(t, 1, f.Created)

	other := request()
	other.FilamentProfile = "petg-generic-klipper@1"
	_, err = c.CreateSliceJob(ctx, other, "pravara-dispatch:abc:12345678")
	var re *remote.Error
	require.ErrorAs(t, err, &re)
	assert.Equal(t, 409, re.Status)
	assert.False(t, re.Retryable)

	j, err := c.GetSliceJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, j.Status)
	assert.False(t, j.Terminal())
	j, err = c.GetSliceJob(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, StatusSucceeded, j.Status)
	require.NotNil(t, j.Output)
	assert.Equal(t, "text/x-gcode", j.Output.MediaType)

	vars, err := c.FetchSlicerVariables(ctx, j.SlicerVariables)
	require.NoError(t, err)
	assert.Equal(t, j.Output.SHA256, vars.Output.SHA256)
	assert.Equal(t, strings.Repeat("23", 32), vars.Profiles["filament"].SHA256)

	tampered := *j.SlicerVariables
	tampered.SHA256 = strings.Repeat("0", 64)
	_, err = c.FetchSlicerVariables(ctx, &tampered)
	require.ErrorAs(t, err, &re)
	assert.Equal(t, "digest_mismatch", re.Code)
}

func TestOverrideOutOfRangeIsTerminalWithThePath(t *testing.T) {
	c, _ := setup(t)
	req := request()
	req.Overrides = map[string]any{"outer_wall_speed": 300.0}
	_, err := c.CreateSliceJob(context.Background(), req, "k1")
	var re *remote.Error
	require.ErrorAs(t, err, &re)
	assert.Equal(t, 422, re.Status)
	assert.Equal(t, "override_out_of_range", re.Code)
	assert.Contains(t, re.Message, "/overrides/outer_wall_speed")
	assert.False(t, re.Retryable)
	_, err = c.CreateSliceJob(context.Background(), req, "bad key with spaces")
	assert.Error(t, err)
}

func TestProfilesCatalog(t *testing.T) {
	c, _ := setup(t)
	cat, err := c.ListProfiles(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, cat.Profiles)
	assert.Equal(t, "klipper_gcode", cat.Profiles[0].Target)
}
