package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"renart/internal/web/service"
)

type pipelineAssetRenderHandlerStub struct {
	renderCalls  int
	compareCalls int
	pipelineID   string
	renderReq    service.PipelineAssetRenderRequest
	compareReq   service.PipelineAssetRenderComparisonRequest
	result       service.AssetRenderResult
	comparison   service.PipelineAssetRenderComparison
	err          *service.APIError
}

func (s *pipelineAssetRenderHandlerStub) RenderPipelineAsset(
	_ context.Context,
	pipelineID string,
	req service.PipelineAssetRenderRequest,
) (service.AssetRenderResult, *service.APIError) {
	s.renderCalls++
	s.pipelineID = pipelineID
	s.renderReq = req
	return s.result, s.err
}

func (s *pipelineAssetRenderHandlerStub) ComparePipelineAssetRenders(
	_ context.Context,
	pipelineID string,
	req service.PipelineAssetRenderComparisonRequest,
) (service.PipelineAssetRenderComparison, *service.APIError) {
	s.compareCalls++
	s.pipelineID = pipelineID
	s.compareReq = req
	return s.comparison, s.err
}

func TestHandleRenderPipelineAssetAcceptsServerOwnedSourceSelector(t *testing.T) {
	t.Parallel()
	stub := &pipelineAssetRenderHandlerStub{result: service.AssetRenderResult{Status: service.AssetRenderStatusOK}}
	response := pipelineAssetRenderRequest(stub, "/api/pipelines/pipeline-id/assets/render", `{
		"asset_name":"analytics.orders",
		"source":{"kind":"snapshot","version_id":"snapshot-7"},
		"environment":"prod",
		"start_date":"2026-07-19T00:00:00Z",
		"end_date":"2026-07-20T00:00:00Z"
	}`)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, 1, stub.renderCalls)
	assert.Equal(t, "pipeline-id", stub.pipelineID)
	assert.Equal(t, "analytics.orders", stub.renderReq.AssetName)
	assert.Equal(t, "snapshot-7", stub.renderReq.Source.VersionID)
}

func TestHandleComparePipelineAssetRendersAcceptsExactSnapshot(t *testing.T) {
	t.Parallel()
	stub := &pipelineAssetRenderHandlerStub{comparison: service.PipelineAssetRenderComparison{Status: "changed"}}
	response := pipelineAssetRenderRequest(stub, "/api/pipelines/pipeline-id/assets/render/compare", `{
		"asset_name":"analytics.orders",
		"snapshot_version_id":"snapshot-7",
		"environment":"prod"
	}`)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, 1, stub.compareCalls)
	assert.Equal(t, "snapshot-7", stub.compareReq.SnapshotVersionID)
}

func TestHandlePipelineAssetRenderRejectsClientOwnedPaths(t *testing.T) {
	t.Parallel()
	for _, route := range []string{
		"/api/pipelines/pipeline-id/assets/render",
		"/api/pipelines/pipeline-id/assets/render/compare",
	} {
		for _, body := range []string{
			`{"asset_name":"analytics.orders","workspace_root":"/tmp/client"}`,
			`{"asset_name":"analytics.orders","source":{"kind":"snapshot","path":"/tmp/client"}}`,
			`null`,
			`[]`,
			`{"asset_name":`,
		} {
			stub := &pipelineAssetRenderHandlerStub{}
			response := pipelineAssetRenderRequest(stub, route, body)
			assert.Equal(t, http.StatusBadRequest, response.Code, "%s %s", route, body)
			assert.Zero(t, stub.renderCalls+stub.compareCalls)
		}
	}
}

func pipelineAssetRenderRequest(stub *pipelineAssetRenderHandlerStub, route, body string) *httptest.ResponseRecorder {
	router := chi.NewRouter()
	RegisterPipelineAssetRenderRoutes(router, &PipelineAssetRenderAPI{Service: stub})
	request := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
