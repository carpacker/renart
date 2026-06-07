package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	"renart/internal/web/scheduler"
)

type SchedulerHandlers interface {
	ListSchedules(ctx context.Context) ([]scheduler.PipelineSchedule, error)
	GetPipelineSchedule(ctx context.Context, pipelineID string) (scheduler.PipelineSchedule, error)
	UpdatePipelineSchedule(ctx context.Context, pipelineID string, req scheduler.UpdateScheduleRequest) (scheduler.PipelineSchedule, error)
	TriggerPipeline(ctx context.Context, pipelineID string, req scheduler.TriggerRequest) (scheduler.PipelineRun, error)
	ListRuns(ctx context.Context, filter scheduler.RunFilter) ([]scheduler.PipelineRun, error)
	GetRun(ctx context.Context, runID string) (scheduler.PipelineRun, []scheduler.LogLine, error)
}

type SchedulerAPI struct {
	Service SchedulerHandlers
}

func RegisterSchedulerRoutes(router chi.Router, handlers *SchedulerAPI) {
	router.Get("/api/schedules", handlers.HandleListSchedules)
	router.Get("/api/pipelines/{id}/schedule", handlers.HandleGetPipelineSchedule)
	router.Put("/api/pipelines/{id}/schedule", handlers.HandleUpdatePipelineSchedule)
	router.Post("/api/pipelines/{id}/trigger", handlers.HandleTriggerPipeline)
	router.Get("/api/runs", handlers.HandleListRuns)
	router.Get("/api/runs/{id}", handlers.HandleGetRun)
}

func (h *SchedulerAPI) HandleListSchedules(w http.ResponseWriter, r *http.Request) {
	items, err := h.Service.ListSchedules(r.Context())
	if err != nil {
		webapi.WriteInternalError(w, "schedules_list_failed", err.Error())
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "schedules": items})
}

func (h *SchedulerAPI) HandleGetPipelineSchedule(w http.ResponseWriter, r *http.Request) {
	item, err := h.Service.GetPipelineSchedule(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		webapi.WriteBadRequest(w, "schedule_get_failed", err.Error())
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "schedule": item})
}

func (h *SchedulerAPI) HandleUpdatePipelineSchedule(w http.ResponseWriter, r *http.Request) {
	var req scheduler.UpdateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	item, err := h.Service.UpdatePipelineSchedule(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		webapi.WriteBadRequest(w, "schedule_update_failed", err.Error())
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "schedule": item})
}

func (h *SchedulerAPI) HandleTriggerPipeline(w http.ResponseWriter, r *http.Request) {
	var req scheduler.TriggerRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	run, err := h.Service.TriggerPipeline(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		webapi.WriteBadRequest(w, "pipeline_trigger_failed", err.Error())
		return
	}
	webapi.WriteJSON(w, http.StatusAccepted, map[string]any{"status": "ok", "run": run})
}

func (h *SchedulerAPI) HandleListRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := h.Service.ListRuns(r.Context(), scheduler.RunFilter{PipelineID: r.URL.Query().Get("pipeline_id"), Limit: limit})
	if err != nil {
		webapi.WriteInternalError(w, "runs_list_failed", err.Error())
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "runs": runs})
}

func (h *SchedulerAPI) HandleGetRun(w http.ResponseWriter, r *http.Request) {
	run, logs, err := h.Service.GetRun(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		webapi.WriteBadRequest(w, "run_get_failed", err.Error())
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "run": run, "logs": logs})
}
