package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/httpapi"
	"renart/internal/web/scheduler"
	"renart/internal/web/snapshot"
)

func TestReviewedDeployRejectsSourceChangedAfterPlan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	schedulerStore, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = schedulerStore.Close() })
	snapshotStore := snapshot.NewStore(schedulerStore.DB())
	pipelineDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "pipeline.yml"), []byte("id: pipeline\n"), 0o644))
	manifest, err := snapshot.CollectManifestHashes(pipelineDir)
	require.NoError(t, err)
	reviewedRoot := snapshot.ManifestRoot(manifest)
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "pipeline.yml"), []byte("id: pipeline\nname: changed\n"), 0o644))

	router := chi.NewRouter()
	httpapi.RegisterDeployRoutes(router, &httpapi.DeployAPI{
		Snapshots: snapshotStore,
		ResolvePipeline: func(string) (string, string, bool) {
			return "pipeline", pipelineDir, true
		},
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/pipelines/pipeline/deploy", strings.NewReader(
		`{"expected_source_merkle":"`+reviewedRoot+`"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"code":"deployment_source_changed"`)
	deployments, err := snapshotStore.List(ctx, "pipeline")
	require.NoError(t, err)
	assert.Empty(t, deployments)
}

func TestReviewedDeployReturnsStableOrdinal(t *testing.T) {
	t.Parallel()
	schedulerStore, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = schedulerStore.Close() })
	snapshotStore := snapshot.NewStore(schedulerStore.DB())
	pipelineDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "pipeline.yml"), []byte("id: pipeline\n"), 0o644))
	manifest, err := snapshot.CollectManifestHashes(pipelineDir)
	require.NoError(t, err)
	reviewedRoot := snapshot.ManifestRoot(manifest)

	router := chi.NewRouter()
	httpapi.RegisterDeployRoutes(router, &httpapi.DeployAPI{
		Snapshots: snapshotStore,
		ResolvePipeline: func(string) (string, string, bool) {
			return "pipeline", pipelineDir, true
		},
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/pipelines/pipeline/deploy", strings.NewReader(
		`{"expected_source_merkle":"`+reviewedRoot+`"}`,
	))
	router.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"ordinal":1`)
}
