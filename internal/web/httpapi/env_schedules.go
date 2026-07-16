package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	"renart/internal/web/scheduler"
)

// EnvScheduleHandlers is the per-environment schedule surface: schedule
// identity is (pipeline, environment), with no implicit default.
type EnvScheduleHandlers interface {
	Ownership() scheduler.SchedulerOwnership
	ListAllEnvSchedules(ctx context.Context) (live []scheduler.EnvSchedule, archived []scheduler.EnvSchedule, err error)
	UpsertEnvSchedule(ctx context.Context, pipelineUUID string, req scheduler.UpsertEnvScheduleRequest) (scheduler.EnvSchedule, error)
	SetEnvScheduleLifecycle(ctx context.Context, pipelineUUID, environment string, status scheduler.ScheduleStatus) error
	ArchiveEnvSchedule(ctx context.Context, pipelineUUID, environment string) error
}

type EnvSchedulesAPI struct {
	Service EnvScheduleHandlers
	// ResolvePipelineUUID maps the path-encoded API pipeline ID to the
	// stable UUID.
	ResolvePipelineUUID func(pipelineID string) (string, bool)
}

func RegisterEnvScheduleRoutes(router chi.Router, handlers *EnvSchedulesAPI) {
	router.Get("/api/env-schedules", handlers.HandleList)
	router.Put("/api/pipelines/{id}/env-schedules/{environment}", handlers.HandleUpsert)
	router.Post("/api/pipelines/{id}/env-schedules/{environment}/status", handlers.HandleSetStatus)
	router.Delete("/api/pipelines/{id}/env-schedules/{environment}", handlers.HandleArchive)
}

func (h *EnvSchedulesAPI) resolve(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	pipelineUUID, ok := h.ResolvePipelineUUID(chi.URLParam(r, "id"))
	if !ok {
		webapi.WriteNotFound(w, "pipeline_not_found", "pipeline not found")
		return "", "", false
	}
	environment := strings.TrimSpace(chi.URLParam(r, "environment"))
	if environment == "" {
		webapi.WriteBadRequest(w, "environment_required", "environment is required")
		return "", "", false
	}
	return pipelineUUID, environment, true
}

func (h *EnvSchedulesAPI) HandleList(w http.ResponseWriter, r *http.Request) {
	live, archived, err := h.Service.ListAllEnvSchedules(r.Context())
	if err != nil {
		webapi.WriteInternalError(w, "env_schedules_list_failed", err.Error())
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"scheduler": h.Service.Ownership(),
		"schedules": live,
		"archived":  archived,
	})
}

func (h *EnvSchedulesAPI) HandleUpsert(w http.ResponseWriter, r *http.Request) {
	pipelineUUID, environment, ok := h.resolve(w, r)
	if !ok {
		return
	}
	req, err := decodeStrictJSONObject[scheduler.UpsertEnvScheduleRequest](r.Body)
	if err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	req.Environment = environment
	updated, err := h.Service.UpsertEnvSchedule(r.Context(), pipelineUUID, req)
	if err != nil {
		writeEnvScheduleMutationError(w, "env_schedule_upsert_failed", err)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "schedule": updated})
}

func (h *EnvSchedulesAPI) HandleSetStatus(w http.ResponseWriter, r *http.Request) {
	pipelineUUID, environment, ok := h.resolve(w, r)
	if !ok {
		return
	}
	type statusRequest struct {
		Status string `json:"status"`
	}
	req, err := decodeStrictJSONObject[statusRequest](r.Body)
	if err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	if err := h.Service.SetEnvScheduleLifecycle(r.Context(), pipelineUUID, environment, scheduler.ScheduleStatus(req.Status)); err != nil {
		writeEnvScheduleMutationError(w, "env_schedule_status_failed", err)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *EnvSchedulesAPI) HandleArchive(w http.ResponseWriter, r *http.Request) {
	pipelineUUID, environment, ok := h.resolve(w, r)
	if !ok {
		return
	}
	if err := h.Service.ArchiveEnvSchedule(r.Context(), pipelineUUID, environment); err != nil {
		writeEnvScheduleMutationError(w, "env_schedule_archive_failed", err)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func writeEnvScheduleMutationError(w http.ResponseWriter, fallbackCode string, err error) {
	if errors.Is(err, scheduler.ErrSchedulerNotOwner) {
		webapi.WriteConflict(w, "scheduler_not_owner", err.Error())
		return
	}
	webapi.WriteBadRequest(w, fallbackCode, err.Error())
}
