package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/scheduler"
)

type envScheduleHandlerStub struct {
	ownership   scheduler.SchedulerOwnership
	mutationErr error
	upsertCalls int
	upsertReq   scheduler.UpsertEnvScheduleRequest
	statusCalls int
	status      scheduler.ScheduleStatus
	promoteReq  scheduler.PromoteEnvSchedulesRequest
	runCalls    int
	runPipeline string
	runUUID     string
	runEnv      string
	live        []scheduler.EnvSchedule
	archived    []scheduler.EnvSchedule
}

func (s *envScheduleHandlerStub) TriggerEnvSchedule(_ context.Context, pipelineID, pipelineUUID, environment string) (scheduler.PipelineRun, error) {
	s.runCalls++
	s.runPipeline, s.runUUID, s.runEnv = pipelineID, pipelineUUID, environment
	if s.mutationErr != nil {
		return scheduler.PipelineRun{}, s.mutationErr
	}
	return scheduler.PipelineRun{ID: "run-id", PipelineID: pipelineID, Environment: environment}, nil
}

func (s *envScheduleHandlerStub) PromoteEnvSchedules(_ context.Context, _ string, req scheduler.PromoteEnvSchedulesRequest) ([]scheduler.EnvSchedule, error) {
	s.promoteReq = req
	return nil, s.mutationErr
}

func (s *envScheduleHandlerStub) Ownership() scheduler.SchedulerOwnership {
	if s.ownership.State == "" {
		return scheduler.SchedulerOwnership{State: scheduler.SchedulerOwnershipOwner}
	}
	return s.ownership
}

func (s *envScheduleHandlerStub) ListAllEnvSchedules(context.Context) ([]scheduler.EnvSchedule, []scheduler.EnvSchedule, error) {
	return s.live, s.archived, nil
}

func (s *envScheduleHandlerStub) UpsertEnvSchedule(_ context.Context, pipelineUUID string, req scheduler.UpsertEnvScheduleRequest) (scheduler.EnvSchedule, error) {
	s.upsertCalls++
	s.upsertReq = req
	if s.mutationErr != nil {
		return scheduler.EnvSchedule{}, s.mutationErr
	}
	return scheduler.EnvSchedule{
		PipelineUUID: pipelineUUID, Environment: req.Environment,
		Vars: req.Vars, SecretRefs: req.SecretRefs, DeclarationManaged: true,
	}, nil
}

func (s *envScheduleHandlerStub) SetEnvScheduleLifecycle(_ context.Context, _, _ string, status scheduler.ScheduleStatus) error {
	s.statusCalls++
	s.status = status
	return s.mutationErr
}

func (s *envScheduleHandlerStub) ArchiveEnvSchedule(context.Context, string, string) error {
	return s.mutationErr
}

func TestEnvScheduleMutationBodiesRequireOneStrictJSONObject(t *testing.T) {
	t.Parallel()

	invalidBodies := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{"cron":`},
		{name: "unknown field", body: `{"unexpected":true}`},
		{name: "body environment", body: `{"environment":"other"}`},
		{name: "multiple values", body: `{}` + "\n" + `{}`},
		{name: "null", body: `null`},
		{name: "array", body: `[]`},
		{name: "scalar", body: `"active"`},
	}

	for _, endpoint := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "upsert", method: http.MethodPut, path: "/api/pipelines/pipeline-id/env-schedules/prod"},
		{name: "status", method: http.MethodPost, path: "/api/pipelines/pipeline-id/env-schedules/prod/status"},
		{name: "promote", method: http.MethodPost, path: "/api/pipelines/pipeline-id/env-schedules/promote"},
	} {
		endpoint := endpoint
		t.Run(endpoint.name, func(t *testing.T) {
			t.Parallel()
			for _, tt := range invalidBodies {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					stub := &envScheduleHandlerStub{}
					response := envScheduleRequest(stub, endpoint.method, endpoint.path, tt.body)

					assert.Equal(t, http.StatusBadRequest, response.Code)
					assert.Contains(t, response.Body.String(), "invalid_request_body")
					assert.Zero(t, stub.upsertCalls)
					assert.Zero(t, stub.statusCalls)
				})
			}
		})
	}
}

func TestEnvScheduleMutationBodiesAcceptDeclaredFields(t *testing.T) {
	t.Parallel()

	upsertStub := &envScheduleHandlerStub{}
	upsertResponse := envScheduleRequest(
		upsertStub,
		http.MethodPut,
		"/api/pipelines/pipeline-id/env-schedules/prod",
		`{"cron":"@daily","timezone":"UTC","snapshot_version_id":"snapshot-id","vars":{"region":"private-value"},"secret_refs":{"token":"env:RENART_TOKEN"}}`,
	)
	require.Equal(t, http.StatusOK, upsertResponse.Code)
	assert.Equal(t, 1, upsertStub.upsertCalls)
	assert.Equal(t, "prod", upsertStub.upsertReq.Environment)
	assert.Equal(t, "snapshot-id", upsertStub.upsertReq.SnapshotVersionID)
	assert.Equal(t, "private-value", upsertStub.upsertReq.Vars["region"])
	assert.Equal(t, "env:RENART_TOKEN", upsertStub.upsertReq.SecretRefs["token"])
	assert.Contains(t, upsertResponse.Body.String(), `"variable_names":["region","token"]`)
	assert.Contains(t, upsertResponse.Body.String(), `"secret_reference_names":["token"]`)
	assert.Contains(t, upsertResponse.Body.String(), `"declaration_managed":true`)
	assert.NotContains(t, upsertResponse.Body.String(), "private-value")
	assert.NotContains(t, upsertResponse.Body.String(), "RENART_TOKEN")

	editStub := &envScheduleHandlerStub{}
	editResponse := envScheduleRequest(
		editStub,
		http.MethodPut,
		"/api/pipelines/pipeline-id/env-schedules/prod",
		`{"cron":"@hourly","preserve_snapshot":true,"preserve_variables":true}`,
	)
	require.Equal(t, http.StatusOK, editResponse.Code)
	assert.True(t, editStub.upsertReq.PreserveSnapshot)
	assert.True(t, editStub.upsertReq.PreserveVariables)

	statusStub := &envScheduleHandlerStub{}
	statusResponse := envScheduleRequest(
		statusStub,
		http.MethodPost,
		"/api/pipelines/pipeline-id/env-schedules/prod/status",
		`{"status":"paused"}`,
	)
	require.Equal(t, http.StatusOK, statusResponse.Code)
	assert.Equal(t, 1, statusStub.statusCalls)
	assert.Equal(t, scheduler.ScheduleStatusPaused, statusStub.status)

	runStub := &envScheduleHandlerStub{}
	runResponse := envScheduleRequest(
		runStub, http.MethodPost, "/api/pipelines/pipeline-id/env-schedules/prod/run", "",
	)
	require.Equal(t, http.StatusAccepted, runResponse.Code)
	assert.Equal(t, 1, runStub.runCalls)
	assert.Equal(t, "pipeline-id", runStub.runPipeline)
	assert.Equal(t, "pipeline-uuid", runStub.runUUID)
	assert.Equal(t, "prod", runStub.runEnv)
}

func TestEnvScheduleResponsesExposeOnlySortedVariableNames(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	stub := &envScheduleHandlerStub{live: []scheduler.EnvSchedule{{
		PipelineUUID: "pipeline-uuid", Environment: "prod", Cron: "@daily", Timezone: "UTC",
		Vars:       map[string]any{"region": "private-region", "limit": 25},
		SecretRefs: map[string]string{"token": "env:PRIVATE_TOKEN"}, DeclarationManaged: true,
		DeferredOccurrence: &scheduler.DeferredScheduleOccurrence{
			IntervalStart: start, IntervalEnd: end, AttemptCount: 1,
		},
	}}}
	response := envScheduleRequest(stub, http.MethodGet, "/api/env-schedules", "")
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"variable_names":["limit","region","token"]`)
	assert.Contains(t, response.Body.String(), `"secret_reference_names":["token"]`)
	assert.Contains(t, response.Body.String(), `"declaration_managed":true`)
	assert.Contains(t, response.Body.String(), `"deferred_occurrence"`)
	assert.Contains(t, response.Body.String(), `"attempt_count":1`)
	assert.NotContains(t, response.Body.String(), "private-region")
	assert.NotContains(t, response.Body.String(), "PRIVATE_TOKEN")
	assert.NotContains(t, response.Body.String(), `"vars"`)
	assert.NotContains(t, response.Body.String(), `"occurrence_key"`)
}

func TestEnvSchedulesExposeOwnershipAndRejectFollowerMutations(t *testing.T) {
	t.Parallel()

	stub := &envScheduleHandlerStub{
		ownership: scheduler.SchedulerOwnership{
			State:   scheduler.SchedulerOwnershipFollower,
			Message: "managed by another process",
		},
		mutationErr: fmt.Errorf("%w: managed by another process", scheduler.ErrSchedulerNotOwner),
	}
	listResponse := envScheduleRequest(stub, http.MethodGet, "/api/env-schedules", "")
	require.Equal(t, http.StatusOK, listResponse.Code)
	assert.JSONEq(t, `{
		"status":"ok",
		"scheduler":{"state":"follower","message":"managed by another process"},
		"schedules":[],
		"archived":[]
	}`, listResponse.Body.String())

	mutations := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPut, "/api/pipelines/pipeline-id/env-schedules/prod", `{"cron":"@daily","snapshot_version_id":"snapshot-id"}`},
		{http.MethodPost, "/api/pipelines/pipeline-id/env-schedules/prod/status", `{"status":"paused"}`},
		{http.MethodPost, "/api/pipelines/pipeline-id/env-schedules/prod/run", ""},
		{http.MethodDelete, "/api/pipelines/pipeline-id/env-schedules/prod", ""},
	}
	for _, mutation := range mutations {
		response := envScheduleRequest(stub, mutation.method, mutation.path, mutation.body)
		assert.Equal(t, http.StatusConflict, response.Code)
		assert.Contains(t, response.Body.String(), `"code":"scheduler_not_owner"`)
	}
}

func envScheduleRequest(stub *envScheduleHandlerStub, method, path, body string) *httptest.ResponseRecorder {
	router := chi.NewRouter()
	RegisterEnvScheduleRoutes(router, &EnvSchedulesAPI{
		Service:            stub,
		TriggerEnvSchedule: stub.TriggerEnvSchedule,
		ResolvePipelineUUID: func(pipelineID string) (string, bool) {
			return "pipeline-uuid", pipelineID == "pipeline-id"
		},
	})
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
