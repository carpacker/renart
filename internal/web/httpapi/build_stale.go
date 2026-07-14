package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	webmodel "renart/internal/web/model"
	"renart/internal/web/service"
	"renart/internal/web/staleness"
)

// BuildStaleAPI implements the server-side "build stale assets" operation:
// it computes the stale set for the current selection and rebuilds it in one
// streamed run (topological order, single combined log), so the client no
// longer orchestrates per-asset builds.
type BuildStaleAPI struct {
	Staleness *staleness.Service
	// ResolvePipelineUUID maps the path-encoded API pipeline ID to the
	// stable pipeline UUID.
	ResolvePipelineUUID func(pipelineID string) (string, bool)
	// ResolveUpstreamAssetNames returns the target asset's transitive
	// in-pipeline upstream closure. It is used by the CLI's explicit
	// --refresh-upstreams mode; ordinary Build stale requests leave it unused.
	ResolveUpstreamAssetNames func(pipelineID, assetName string) (map[string]struct{}, bool)
	Execution                 BuildStaleExecutionHandlers
}

type BuildStaleExecutionHandlers interface {
	MaterializeStaleAssetsStream(
		ctx context.Context,
		pipelineID, environment string,
		plan []service.StaleAssetPlan,
		startDate, endDate string,
		onChunk func([]byte),
		onEvent func(service.StaleBuildEvent),
	) service.MaterializeResult
}

func RegisterBuildStaleRoutes(router chi.Router, handlers *BuildStaleAPI) {
	router.Post("/api/pipelines/{id}/build-stale/stream", handlers.HandleBuildStaleStream)
}

func (h *BuildStaleAPI) HandleBuildStaleStream(w http.ResponseWriter, r *http.Request) {
	pipelineID := chi.URLParam(r, "id")
	pipelineUUID, ok := h.ResolvePipelineUUID(pipelineID)
	if !ok {
		webapi.WriteNotFound(w, "pipeline_not_found", "pipeline not found")
		return
	}
	var include map[string]struct{}
	if upstreamOf := strings.TrimSpace(r.URL.Query().Get("upstream_of")); upstreamOf != "" {
		if h.ResolveUpstreamAssetNames == nil {
			webapi.WriteInternalError(w, "upstream_resolution_unavailable", "upstream resolution is unavailable")
			return
		}
		var found bool
		include, found = h.ResolveUpstreamAssetNames(pipelineID, upstreamOf)
		if !found {
			webapi.WriteNotFound(w, "asset_not_found", "asset not found in pipeline")
			return
		}
	}

	selection := staleness.Selection{
		PipelineUUID:      pipelineUUID,
		EncodedPipelineID: pipelineID,
		Environment:       r.URL.Query().Get("environment"),
	}
	startDate := ""
	endDate := ""
	if start, ok := parseQueryTime(r, "start"); ok {
		selection.Start = &start
		startDate = start.Format(time.RFC3339)
	}
	if end, ok := parseQueryTime(r, "end"); ok {
		selection.End = &end
		endDate = end.Format(time.RFC3339)
	}

	statuses, err := h.Staleness.Statuses(r.Context(), selection)
	if err != nil {
		webapi.WriteInternalError(w, "staleness_compute_failed", err.Error())
		return
	}
	plan := service.BuildStalePlan(statuses, include)

	flusher, ok := w.(http.Flusher)
	if !ok {
		webapi.WriteInternalError(w, "streaming_unsupported", "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	_ = WriteSSEJSON(w, flusher, "start", map[string]any{
		"operation": webmodel.OperationMetadata{Type: "run", PipelineID: pipelineID, Target: pipelineID},
	})
	result := h.Execution.MaterializeStaleAssetsStream(
		r.Context(), pipelineID, selection.Environment, plan, startDate, endDate,
		func(chunk []byte) {
			_ = WriteSSEJSON(w, flusher, "output", map[string]any{"chunk": string(chunk)})
		},
		func(event service.StaleBuildEvent) {
			_ = WriteSSEJSON(w, flusher, "asset", event)
		},
	)
	_ = WriteSSEJSON(w, flusher, "done", map[string]any{
		"status":            result.Status,
		"operation":         result.Operation,
		"output":            result.Output,
		"error":             result.Error,
		"exit_code":         result.ExitCode,
		"changed_asset_ids": result.ChangedAssetIDs,
		"materialized_at":   result.MaterializedAt,
	})
}
