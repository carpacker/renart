package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/service"
)

type strictPipelineExecutionStub struct {
	materializeCalls int
}

func (s *strictPipelineExecutionStub) GetPipelineMaterialization(context.Context, string, string) (PipelineMaterializationResponse, *APIError) {
	return PipelineMaterializationResponse{}, nil
}

func (s *strictPipelineExecutionStub) MaterializePipelineStreamWithSensorMode(context.Context, string, string, bool, bool, bool, string, string, string, string, func([]byte)) MaterializeExecutionEvent {
	s.materializeCalls++
	return MaterializeExecutionEvent{Status: "ok"}
}

func (s *strictPipelineExecutionStub) ResolvePipelineRunTarget(string) error { return nil }

type strictAssetExecutionStub struct {
	materializeCalls int
}

func (s *strictAssetExecutionStub) InspectAsset(context.Context, string, string, string, string, string) InspectExecutionResult {
	return InspectExecutionResult{}
}

func (s *strictAssetExecutionStub) MaterializeAssetStreamWithSensorMode(context.Context, string, string, string, string, string, bool, bool, string, string, func([]byte)) MaterializeExecutionEvent {
	s.materializeCalls++
	return MaterializeExecutionEvent{Status: "ok"}
}

type strictBuildStaleExecutionStub struct {
	materializeCalls int
}

func (s *strictBuildStaleExecutionStub) MaterializeStaleAssetsStream(context.Context, string, string, []service.StaleAssetPlan, string, string, func([]byte), func(service.StaleBuildEvent)) service.MaterializeResult {
	s.materializeCalls++
	return service.MaterializeResult{Status: "ok"}
}

func TestPipelineMaterializeRejectsMalformedDryRunBeforeStreaming(t *testing.T) {
	t.Parallel()

	stub := &strictPipelineExecutionStub{}
	router := chi.NewRouter()
	RegisterPipelineExecutionRoutes(router, &PipelineExecutionAPI{Service: stub})
	request := httptest.NewRequest(http.MethodPost, "/api/pipelines/pipeline-id/materialize/stream?dry_run=tru", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	assert.Contains(t, response.Body.String(), "invalid_execution_context")
	assert.NotContains(t, response.Body.String(), "event: start")
	assert.Zero(t, stub.materializeCalls)
}

func TestPipelineMaterializeRejectsDryRunContextItCannotPreserveBeforeStreaming(t *testing.T) {
	t.Parallel()

	stub := &strictPipelineExecutionStub{}
	router := chi.NewRouter()
	RegisterPipelineExecutionRoutes(router, &PipelineExecutionAPI{Service: stub})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/pipelines/pipeline-id/materialize/stream?dry_run=true&start_date=2026-07-16T08%3A00%3A00Z&end_date=2026-07-16T09%3A00%3A00Z",
		nil,
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	assert.Contains(t, response.Body.String(), "unsupported_dry_run_context")
	assert.NotContains(t, response.Body.String(), "event: start")
	assert.Zero(t, stub.materializeCalls)
}

func TestAssetMaterializeRejectsDryRunBeforeStreaming(t *testing.T) {
	t.Parallel()

	stub := &strictAssetExecutionStub{}
	router := chi.NewRouter()
	RegisterExecutionRoutes(router, &ExecutionAPI{Service: stub})
	request := httptest.NewRequest(http.MethodPost, "/api/assets/asset-id/materialize/stream?dry_run=true", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	assert.Contains(t, response.Body.String(), "asset_dry_run_unsupported")
	assert.NotContains(t, response.Body.String(), "event: start")
	assert.Zero(t, stub.materializeCalls)
}

func TestMaterializeEndpointsRejectMalformedWindowsBeforeWork(t *testing.T) {
	t.Parallel()

	t.Run("pipeline", func(t *testing.T) {
		t.Parallel()
		stub := &strictPipelineExecutionStub{}
		router := chi.NewRouter()
		RegisterPipelineExecutionRoutes(router, &PipelineExecutionAPI{Service: stub})
		request := httptest.NewRequest(http.MethodPost, "/api/pipelines/pipeline-id/materialize/stream?start_date=not-a-time&end_date=2026-07-16T09%3A00%3A00Z", nil)
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)

		require.Equal(t, http.StatusBadRequest, response.Code)
		assert.Zero(t, stub.materializeCalls)
		assert.NotContains(t, response.Body.String(), "event: start")
	})

	t.Run("asset", func(t *testing.T) {
		t.Parallel()
		stub := &strictAssetExecutionStub{}
		router := chi.NewRouter()
		RegisterExecutionRoutes(router, &ExecutionAPI{Service: stub})
		request := httptest.NewRequest(http.MethodPost, "/api/assets/asset-id/materialize/stream?start_date=not-a-time&end_date=2026-07-16T09%3A00%3A00Z", nil)
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)

		require.Equal(t, http.StatusBadRequest, response.Code)
		assert.Zero(t, stub.materializeCalls)
		assert.NotContains(t, response.Body.String(), "event: start")
	})

	t.Run("build stale", func(t *testing.T) {
		t.Parallel()
		stub := &strictBuildStaleExecutionStub{}
		resolvedPipeline := false
		router := chi.NewRouter()
		RegisterBuildStaleRoutes(router, &BuildStaleAPI{
			ResolvePipelineUUID: func(string) (string, bool) {
				resolvedPipeline = true
				return "pipeline-uuid", true
			},
			Execution: stub,
		})
		request := httptest.NewRequest(http.MethodPost, "/api/pipelines/pipeline-id/build-stale/stream?start=not-a-time&end=2026-07-16T09%3A00%3A00Z", nil)
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)

		require.Equal(t, http.StatusBadRequest, response.Code)
		assert.False(t, resolvedPipeline, "invalid context should be rejected before planning")
		assert.Zero(t, stub.materializeCalls)
		assert.NotContains(t, response.Body.String(), "event: start")
	})
}

func TestMaterializeEndpointsRejectMalformedModeBooleansBeforeWork(t *testing.T) {
	t.Parallel()

	stub := &strictAssetExecutionStub{}
	router := chi.NewRouter()
	RegisterExecutionRoutes(router, &ExecutionAPI{Service: stub})
	request := httptest.NewRequest(http.MethodPost, "/api/assets/asset-id/materialize/stream?backfill=tru", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	assert.Zero(t, stub.materializeCalls)
}
