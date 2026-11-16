package models

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/visualization-engine/internal/yantra4d"
)

type fakeFetcher struct {
	manifest *yantra4d.Manifest
	artifact *yantra4d.Artifact
	err      error
	reqs     []yantra4d.RenderRequest
	parts    []string
	tokens   []string
}

func (f *fakeFetcher) GetManifest(_ context.Context, project, token string) (*yantra4d.Manifest, error) {
	f.tokens = append(f.tokens, token)
	return f.manifest, nil
}

func (f *fakeFetcher) Fetch(_ context.Context, req yantra4d.RenderRequest, part, token string) (*yantra4d.Artifact, error) {
	f.reqs = append(f.reqs, req)
	f.parts = append(f.parts, part)
	return f.artifact, f.err
}

type fakeStore struct{ keys []string }

func (s *fakeStore) UploadModel(_ context.Context, key string, data io.Reader, _ int64, _ string) (string, error) {
	_, _ = io.ReadAll(data)
	s.keys = append(s.keys, key)
	return "s3://bucket/" + key, nil
}

func (s *fakeStore) GetPresignedURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://s3.example/" + key, nil
}

func testManifest() *yantra4d.Manifest {
	return &yantra4d.Manifest{
		Project: yantra4d.ProjectMeta{Name: "Gear Reducer", Slug: "gear-reducer", Engine: "openscad"},
		Modes: []yantra4d.Mode{
			{ID: "assembly", Parts: []string{"housing_bottom", "housing_top"}},
			{ID: "gears", Parts: []string{"input_gear"}, Engine: "cadquery"},
		},
	}
}

func newTestImporter(f *fakeFetcher, s *fakeStore) *Yantra4DImporter {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return &Yantra4DImporter{client: f, store: s, log: log}
}

func TestYantra4DImportRendersVerifiedGLB(t *testing.T) {
	f := &fakeFetcher{manifest: testManifest(), artifact: &yantra4d.Artifact{
		Data: []byte("glTF"), MediaType: "model/gltf-binary", SHA256: "abc123", InstanceID: "id",
	}}
	s := &fakeStore{}
	m, err := newTestImporter(f, s).ImportFull(context.Background(),
		Yantra4DImportRequest{Slug: "gear-reducer", Mode: "gears", Params: map[string]any{"teeth": 20}}, "tok", ImportOptions{})
	require.NoError(t, err)
	require.Len(t, f.reqs, 1)
	assert.Equal(t, yantra4d.RenderRequest{Project: "gear-reducer", Mode: "gears", Parameters: map[string]any{"teeth": 20}, ExportFormat: "glb"}, f.reqs[0])
	assert.Equal(t, []string{"input_gear"}, f.parts, "the mode's only part")
	assert.Equal(t, []string{"models/yantra4d/gear-reducer/abc123.glb"}, s.keys, "stored under its digest")
	assert.Equal(t, "https://s3.example/models/yantra4d/gear-reducer/abc123.glb", m.ModelURL)
	assert.Equal(t, "cnc_3axis", m.MachineType, "mode engine wins over the project engine")
	assert.Equal(t, "Gear Reducer", m.Name)
}

func TestYantra4DImportRefusesWithoutUploading(t *testing.T) {
	cases := map[string]struct {
		req Yantra4DImportRequest
		err error
	}{
		"several modes, none named": {req: Yantra4DImportRequest{Slug: "gear-reducer"}},
		"unknown mode":              {req: Yantra4DImportRequest{Slug: "gear-reducer", Mode: "nope"}},
		"several parts, none named": {req: Yantra4DImportRequest{Slug: "gear-reducer", Mode: "assembly"}},
		"part not in the mode":      {req: Yantra4DImportRequest{Slug: "gear-reducer", Mode: "assembly", Part: "shaft"}},
		"fetch refused": {req: Yantra4DImportRequest{Slug: "gear-reducer", Mode: "gears"},
			err: &yantra4d.Error{Op: "render", Code: "part_missing", Message: "render returned no part"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeFetcher{manifest: testManifest(), err: tc.err}
			s := &fakeStore{}
			m, err := newTestImporter(f, s).ImportFull(context.Background(), tc.req, "", ImportOptions{})
			require.Error(t, err)
			assert.Nil(t, m)
			assert.Empty(t, s.keys, "nothing is uploaded")
			if tc.err != nil {
				var ye *yantra4d.Error
				require.True(t, errors.As(err, &ye))
				assert.Equal(t, "part_missing", ye.Code)
			} else {
				assert.Empty(t, f.reqs, "no render for an ambiguous or unknown target")
			}
		})
	}
}

func TestYantra4DImporterNotConfigured(t *testing.T) {
	imp := NewYantra4DImporter(nil, nil, logrus.New())
	assert.False(t, imp.IsConfigured())
	_, err := imp.Import(context.Background(), "gear-reducer", ImportOptions{})
	require.Error(t, err)
}
