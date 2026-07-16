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

type runHandlerStub struct {
	calls int
	req   service.RunRequest
}

func (s *runHandlerStub) Execute(_ context.Context, req service.RunRequest) service.RunResult {
	s.calls++
	s.req = req
	return service.RunResult{Status: "ok", HTTPCode: http.StatusOK}
}

func TestHandleRunRequiresOneStrictJSONObject(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{"pipeline_id":`},
		{name: "unknown field", body: `{"unexpected":true}`},
		{name: "multiple objects", body: `{}` + "\n" + `{}`},
		{name: "null", body: `null`},
		{name: "array", body: `[]`},
		{name: "scalar", body: `"pipeline"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &runHandlerStub{}
			response := runRequest(stub, tt.body)

			assert.Equal(t, http.StatusBadRequest, response.Code)
			assert.Contains(t, response.Body.String(), "invalid_request_body")
			assert.Zero(t, stub.calls)
		})
	}
}

func TestHandleRunAcceptsDeclaredExecutionContext(t *testing.T) {
	t.Parallel()

	stub := &runHandlerStub{}
	response := runRequest(stub, `{
		"pipeline_id":"pipeline-id",
		"environment":"prod",
		"start_date":"2026-07-16T08:00:00Z",
		"end_date":"2026-07-16T09:00:00Z",
		"backfill":true,
		"confirmed_environment":"prod",
		"sensor_mode":"skip"
	}`)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, 1, stub.calls)
	assert.Equal(t, "pipeline-id", stub.req.PipelineID)
	assert.Equal(t, "prod", stub.req.Environment)
	assert.True(t, stub.req.Backfill)
	assert.Equal(t, "prod", stub.req.ConfirmedEnvironment)
	assert.Equal(t, "skip", stub.req.SensorMode)
}

func TestHandleRunRejectsInvalidContextBeforeExecution(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		body string
		code string
	}{
		{
			name: "malformed window",
			body: `{"pipeline_id":"pipeline-id","start_date":"not-a-time","end_date":"2026-07-16T09:00:00Z"}`,
			code: "invalid_execution_context",
		},
		{
			name: "backfill without window",
			body: `{"pipeline_id":"pipeline-id","backfill":true}`,
			code: "invalid_execution_context",
		},
		{
			name: "asset dry run",
			body: `{"asset_path":"pipelines/orders/assets/orders.sql","dry_run":true}`,
			code: "asset_dry_run_unsupported",
		},
		{
			name: "invalid sensor mode",
			body: `{"pipeline_id":"pipeline-id","sensor_mode":"sometimes"}`,
			code: "invalid_execution_context",
		},
		{
			name: "dry run with explicit window",
			body: `{"pipeline_id":"pipeline-id","dry_run":true,"start_date":"2026-07-16T08:00:00Z","end_date":"2026-07-16T09:00:00Z"}`,
			code: "unsupported_dry_run_context",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &runHandlerStub{}
			response := runRequest(stub, tt.body)

			require.Equal(t, http.StatusBadRequest, response.Code)
			assert.Contains(t, response.Body.String(), tt.code)
			assert.Zero(t, stub.calls)
		})
	}
}

func runRequest(stub *runHandlerStub, body string) *httptest.ResponseRecorder {
	router := chi.NewRouter()
	RegisterRunRoutes(router, &RunAPI{Service: stub})
	request := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
