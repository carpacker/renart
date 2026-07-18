package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/scheduler"
)

type schedulerHandlerStub struct {
	updateCalls       int
	updatePipelineID  string
	updateRequest     scheduler.UpdateScheduleRequest
	triggerCalls      int
	triggerPipelineID string
	triggerRequest    scheduler.TriggerRequest
	updateErr         error
	triggerErr        error
	runPlan           scheduler.PipelineRunPlan
	hasRunPlan        bool
	runPlanErr        error
	runUnits          []scheduler.PipelineRunUnit
}

func (s *schedulerHandlerStub) ListSchedules(context.Context) ([]scheduler.PipelineSchedule, error) {
	return nil, nil
}

func (s *schedulerHandlerStub) GetPipelineSchedule(context.Context, string) (scheduler.PipelineSchedule, error) {
	return scheduler.PipelineSchedule{}, nil
}

func (s *schedulerHandlerStub) UpdatePipelineSchedule(_ context.Context, pipelineID string, req scheduler.UpdateScheduleRequest) (scheduler.PipelineSchedule, error) {
	s.updateCalls++
	s.updatePipelineID = pipelineID
	s.updateRequest = req
	return scheduler.PipelineSchedule{}, s.updateErr
}

func (s *schedulerHandlerStub) TriggerPipeline(_ context.Context, pipelineID string, req scheduler.TriggerRequest) (scheduler.PipelineRun, error) {
	s.triggerCalls++
	s.triggerPipelineID = pipelineID
	s.triggerRequest = req
	if s.triggerErr != nil {
		return scheduler.PipelineRun{}, s.triggerErr
	}
	return scheduler.PipelineRun{ID: "run-id", Trigger: scheduler.RunTriggerManual}, nil
}

func (s *schedulerHandlerStub) ListRuns(context.Context, scheduler.RunFilter) (scheduler.RunList, error) {
	return scheduler.RunList{}, nil
}

func (s *schedulerHandlerStub) GetRun(context.Context, string) (scheduler.PipelineRun, []scheduler.LogLine, []scheduler.PipelineRunStep, error) {
	return scheduler.PipelineRun{}, nil, nil, nil
}

func (s *schedulerHandlerStub) GetRunPlan(context.Context, string) (scheduler.PipelineRunPlan, bool, error) {
	return s.runPlan, s.hasRunPlan, s.runPlanErr
}

func (s *schedulerHandlerStub) ListRunUnits(context.Context, string) ([]scheduler.PipelineRunUnit, error) {
	return s.runUnits, nil
}

func TestHandleTriggerPipelineRejectsInvalidOrClientOwnedContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{"environment":`},
		{name: "client-owned trigger", body: `{"trigger":"schedule"}`},
		{name: "multiple objects", body: `{}` + "\n" + `{}`},
		{name: "null", body: `null`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &schedulerHandlerStub{}
			response := triggerRequest(stub, strings.NewReader(tt.body))

			assert.Equal(t, http.StatusBadRequest, response.Code)
			assert.Contains(t, response.Body.String(), "invalid_request_body")
			assert.Zero(t, stub.triggerCalls)
		})
	}
}

func TestHandleGetRunIncludesReviewedPlanWhenPresent(t *testing.T) {
	t.Parallel()
	stub := &schedulerHandlerStub{
		hasRunPlan: true,
		runUnits: []scheduler.PipelineRunUnit{{
			Position: 0, AssetName: "analytics.orders", Status: scheduler.PipelineRunUnitQueued,
		}},
		runPlan: scheduler.PipelineRunPlan{
			Version:  scheduler.PipelineRunPlanVersionV1,
			PlanID:   strings.Repeat("a", 64),
			Artifact: []byte(`{"id":"reviewed-plan"}`),
		},
	}
	router := chi.NewRouter()
	RegisterSchedulerRoutes(router, &SchedulerAPI{Service: stub})
	request := httptest.NewRequest(http.MethodGet, "/api/runs/run-id", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"plan":{"version":1`)
	assert.Contains(t, response.Body.String(), `"artifact":{"id":"reviewed-plan"}`)
	assert.Contains(t, response.Body.String(), `"units":[{"position":0`)
}

func TestHandleUpdatePipelineScheduleRequiresOneStrictJSONObject(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{"enabled":`},
		{name: "unknown field", body: `{"enabled":true,"unexpected":true}`},
		{name: "multiple objects", body: `{}` + "\n" + `{}`},
		{name: "null", body: `null`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &schedulerHandlerStub{}
			response := updateScheduleRequest(stub, tt.body)

			assert.Equal(t, http.StatusBadRequest, response.Code)
			assert.Contains(t, response.Body.String(), "invalid_request_body")
			assert.Zero(t, stub.updateCalls)
		})
	}
}

func TestHandleUpdatePipelineScheduleAcceptsDeclaredContext(t *testing.T) {
	t.Parallel()

	stub := &schedulerHandlerStub{}
	response := updateScheduleRequest(stub, `{"enabled":true,"schedule":"@daily","timezone":"UTC","catchup":true}`)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, 1, stub.updateCalls)
	assert.Equal(t, "pipeline-id", stub.updatePipelineID)
	assert.Equal(t, scheduler.UpdateScheduleRequest{
		Enabled: true, Schedule: "@daily", Timezone: "UTC", Catchup: true,
	}, stub.updateRequest)
}

func TestHandleTriggerPipelineAcceptsStrictContextAndEmptyBody(t *testing.T) {
	t.Parallel()

	stub := &schedulerHandlerStub{}
	response := triggerRequest(stub, strings.NewReader(`{"environment":"dev","start":"2026-07-16T08:00:00Z","end":"2026-07-16T09:00:00Z","full_refresh":true,"sensor_mode":"skip"}`))

	require.Equal(t, http.StatusAccepted, response.Code)
	assert.Equal(t, 1, stub.triggerCalls)
	assert.Equal(t, "pipeline-id", stub.triggerPipelineID)
	assert.Equal(t, "dev", stub.triggerRequest.Environment)
	assert.True(t, stub.triggerRequest.FullRefresh)
	assert.Equal(t, "skip", stub.triggerRequest.SensorMode)

	emptyStub := &schedulerHandlerStub{}
	emptyResponse := triggerRequest(emptyStub, nil)
	require.Equal(t, http.StatusAccepted, emptyResponse.Code)
	assert.Equal(t, 1, emptyStub.triggerCalls)

	legacyStub := &schedulerHandlerStub{}
	legacyResponse := triggerRequest(legacyStub, strings.NewReader(`{"trigger":"manual"}`))
	require.Equal(t, http.StatusAccepted, legacyResponse.Code)
	assert.Equal(t, 1, legacyStub.triggerCalls)
	assert.Empty(t, legacyStub.triggerRequest.LegacyTrigger)
}

func TestSchedulerMutationsReportFollowerAsConflict(t *testing.T) {
	t.Parallel()
	notOwner := fmt.Errorf("%w: managed by another process", scheduler.ErrSchedulerNotOwner)

	updateStub := &schedulerHandlerStub{updateErr: notOwner}
	updateRouter := chi.NewRouter()
	RegisterSchedulerRoutes(updateRouter, &SchedulerAPI{Service: updateStub})
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/pipelines/pipeline-id/schedule", strings.NewReader(`{"enabled":true,"schedule":"@daily"}`))
	updateResponse := httptest.NewRecorder()
	updateRouter.ServeHTTP(updateResponse, updateRequest)
	assert.Equal(t, http.StatusConflict, updateResponse.Code)
	assert.Contains(t, updateResponse.Body.String(), `"code":"scheduler_not_owner"`)

	triggerStub := &schedulerHandlerStub{triggerErr: notOwner}
	triggerResponse := triggerRequest(triggerStub, strings.NewReader(`{"source":"working_tree"}`))
	assert.Equal(t, http.StatusConflict, triggerResponse.Code)
	assert.Contains(t, triggerResponse.Body.String(), `"code":"scheduler_not_owner"`)
}

func TestTriggerPipelineReportsActiveRunConflictWithRunID(t *testing.T) {
	t.Parallel()
	stub := &schedulerHandlerStub{triggerErr: &scheduler.PipelineRunActiveError{
		PipelineID:  "pipeline-id",
		ActiveRunID: "active-run-id",
	}}

	response := triggerRequest(stub, strings.NewReader(`{"source":"working_tree"}`))

	require.Equal(t, http.StatusConflict, response.Code)
	assert.JSONEq(t, `{
		"status":"error",
		"error":{
			"code":"pipeline_run_active",
			"message":"pipeline pipeline-id already has active run active-run-id",
			"details":{"pipeline_id":"pipeline-id","active_run_id":"active-run-id"}
		}
	}`, response.Body.String())
}

func triggerRequest(stub *schedulerHandlerStub, body io.Reader) *httptest.ResponseRecorder {
	router := chi.NewRouter()
	RegisterSchedulerRoutes(router, &SchedulerAPI{Service: stub})
	request := httptest.NewRequest(http.MethodPost, "/api/pipelines/pipeline-id/trigger", body)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func updateScheduleRequest(stub *schedulerHandlerStub, body string) *httptest.ResponseRecorder {
	router := chi.NewRouter()
	RegisterSchedulerRoutes(router, &SchedulerAPI{Service: stub})
	request := httptest.NewRequest(http.MethodPut, "/api/pipelines/pipeline-id/schedule", strings.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
