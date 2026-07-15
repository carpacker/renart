package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	webmodel "renart/internal/web/model"
	"renart/internal/web/service"
)

type (
	PipelineMaterializationState    = service.PipelineMaterializationState
	PipelineMaterializationResponse = service.PipelineMaterializationResponse
)

type PipelineExecutionHandlers interface {
	GetPipelineMaterialization(ctx context.Context, pipelineID, environment string) (PipelineMaterializationResponse, *APIError)
	MaterializePipelineStreamWithSensorMode(ctx context.Context, pipelineID, environment string, dryRun, fullRefresh, backfill bool, startDate, endDate, confirmedEnvironment, sensorMode string, onChunk func([]byte)) MaterializeExecutionEvent
	ResolvePipelineRunTarget(pipelineID string) error
}

type PipelineExecutionAPI struct {
	Service PipelineExecutionHandlers
}

func RegisterPipelineExecutionRoutes(router chi.Router, handlers *PipelineExecutionAPI) {
	router.Get("/api/pipelines/{id}/materialization", handlers.HandleGetPipelineMaterialization)
	router.Post("/api/pipelines/{id}/materialize/stream", handlers.HandleMaterializePipelineStream)
}

func (h *PipelineExecutionAPI) HandleGetPipelineMaterialization(w http.ResponseWriter, r *http.Request) {
	pipelineID := chi.URLParam(r, "id")
	environment := r.URL.Query().Get("environment")
	resp, apiErr := h.Service.GetPipelineMaterialization(r.Context(), pipelineID, environment)
	if apiErr != nil {
		webapi.WriteJSON(w, apiErr.Status, map[string]any{
			"status": "error",
			"error":  map[string]string{"code": apiErr.Code, "message": apiErr.Message},
		})
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *PipelineExecutionAPI) HandleMaterializePipelineStream(w http.ResponseWriter, r *http.Request) {
	pipelineID := chi.URLParam(r, "id")
	environment := r.URL.Query().Get("environment")
	dryRun := r.URL.Query().Get("dry_run") == "true"
	fullRefresh := r.URL.Query().Get("full_refresh") == "true"
	backfill := r.URL.Query().Get("backfill") == "true"
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	confirmedEnvironment := r.URL.Query().Get("confirmed_environment")
	sensorMode := r.URL.Query().Get("sensor_mode")
	if err := h.Service.ResolvePipelineRunTarget(pipelineID); err != nil {
		webapi.WriteBadRequest(w, "invalid_pipeline_id", "invalid pipeline id")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		webapi.WriteInternalError(w, "streaming_unsupported", "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	_ = WriteSSEJSON(w, flusher, "start", map[string]any{"operation": webmodel.OperationMetadata{Type: "run", PipelineID: pipelineID, Target: pipelineID}})
	result := h.Service.MaterializePipelineStreamWithSensorMode(r.Context(), pipelineID, environment, dryRun, fullRefresh, backfill, startDate, endDate, confirmedEnvironment, sensorMode, func(chunk []byte) {
		_ = WriteSSEJSON(w, flusher, "output", map[string]any{"chunk": string(chunk)})
	})
	_ = WriteSSEJSON(w, flusher, "done", map[string]any{
		"status":            result.Status,
		"operation":         result.Operation,
		"output":            result.Output,
		"error":             result.Error,
		"exit_code":         result.ExitCode,
		"changed_asset_ids": result.ChangedAssetIDs,
		"materialized_at":   result.MaterializedAt,
		"warnings":          result.Warnings,
	})
}
