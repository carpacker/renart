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

type assetRenderHandlerStub struct {
	calls   int
	assetID string
	req     service.AssetRenderRequest
	result  service.AssetRenderResult
	err     *service.APIError
}

func (s *assetRenderHandlerStub) RenderAsset(_ context.Context, assetID string, req service.AssetRenderRequest) (service.AssetRenderResult, *service.APIError) {
	s.calls++
	s.assetID = assetID
	s.req = req
	return s.result, s.err
}

func TestHandleRenderAssetAcceptsOnlyDeclaredPreviewContext(t *testing.T) {
	t.Parallel()

	stub := &assetRenderHandlerStub{result: service.AssetRenderResult{Status: service.AssetRenderStatusOK}}
	response := assetRenderRequest(stub, `{
		"environment":"dev",
		"start_date":"2026-07-15T00:00:00Z",
		"end_date":"2026-07-16T00:00:00Z",
		"execution_time":"2026-07-16T12:00:00Z",
		"full_refresh":true
	}`)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, 1, stub.calls)
	assert.Equal(t, "asset-id", stub.assetID)
	assert.Equal(t, "dev", stub.req.Environment)
	assert.True(t, stub.req.FullRefresh)
	assert.Contains(t, response.Body.String(), `"status":"ok"`)
}

func TestHandleRenderAssetRejectsClientOwnedPathRunIDAndMalformedJSON(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{"asset_path":"pipeline/assets/report.sql"}`,
		`{"run_id":"client-run"}`,
		`{"unexpected":true}`,
		`null`,
		`[]`,
		`{}` + "\n" + `{}`,
		`{"environment":`,
	} {
		stub := &assetRenderHandlerStub{}
		response := assetRenderRequest(stub, body)
		assert.Equal(t, http.StatusBadRequest, response.Code, body)
		assert.Zero(t, stub.calls, body)
	}
}

func TestHandleRenderAssetRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	stub := &assetRenderHandlerStub{}
	body := `{"environment":"` + strings.Repeat("x", maxAssetRenderRequestBytes) + `"}`
	response := assetRenderRequest(stub, body)

	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Zero(t, stub.calls)
	assert.Contains(t, response.Body.String(), `"code":"invalid_request_body"`)
}

func TestHandleRenderAssetUsesSharedServiceErrorEnvelope(t *testing.T) {
	t.Parallel()

	stub := &assetRenderHandlerStub{err: &service.APIError{
		Status:  http.StatusNotFound,
		Code:    "asset_not_found",
		Message: "asset was not found",
	}}
	response := assetRenderRequest(stub, `{}`)

	require.Equal(t, http.StatusNotFound, response.Code)
	assert.Contains(t, response.Body.String(), `"code":"asset_not_found"`)
	assert.Contains(t, response.Body.String(), `"message":"asset was not found"`)
}

func TestHandleRenderAssetReturnsNotFoundForDeletedAsset(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	router := chi.NewRouter()
	RegisterAssetRenderRoutes(router, &AssetRenderAPI{Service: service.NewAssetRenderService(root)})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/assets/"+service.EncodeID("pipeline/assets/deleted.sql")+"/render",
		strings.NewReader(`{}`),
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
	assert.Contains(t, response.Body.String(), `"code":"asset_not_found"`)
}

func assetRenderRequest(stub *assetRenderHandlerStub, body string) *httptest.ResponseRecorder {
	router := chi.NewRouter()
	RegisterAssetRenderRoutes(router, &AssetRenderAPI{Service: stub})
	request := httptest.NewRequest(http.MethodPost, "/api/assets/asset-id/render", strings.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
