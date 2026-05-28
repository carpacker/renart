package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	webmodel "renart/internal/web/model"
)

type InspectExecutionResult struct {
	Status                              string
	Columns                             []string
	Rows                                []map[string]any
	RawOutput                           string
	Operation                           webmodel.OperationMetadata
	Error                               string
	MissingUpstreamAssetIDs             []string
	MissingUpstreamAssetNames           []string
	MissingUpstreamAssetsMaterializable bool
	Attempts                            int
	Retryable                           bool
	HTTPStatus                          int
}

type MaterializeExecutionEvent struct {
	Status          string
	Operation       webmodel.OperationMetadata
	Output          string
	Error           string
	ExitCode        int
	ChangedAssetIDs []string
	MaterializedAt  *time.Time
}

type ExecutionHandlers interface {
	InspectAsset(ctx context.Context, assetID, limit, environment, startDate, endDate string) InspectExecutionResult
	MaterializeAssetStream(ctx context.Context, assetID, environment, scope, startDate, endDate string, onChunk func([]byte)) MaterializeExecutionEvent
}

type ExecutionAPI struct {
	Service ExecutionHandlers
}

func RegisterExecutionRoutes(router chi.Router, handlers *ExecutionAPI) {
	router.Get("/api/assets/{assetID}/inspect", handlers.HandleInspectAsset)
	router.Post("/api/assets/{assetID}/materialize/stream", handlers.HandleMaterializeAssetStream)
}

func (h *ExecutionAPI) HandleInspectAsset(w http.ResponseWriter, r *http.Request) {
	assetID := chi.URLParam(r, "assetID")
	limit := r.URL.Query().Get("limit")
	environment := r.URL.Query().Get("environment")
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	if limit == "" {
		limit = "200"
	}

	result := h.Service.InspectAsset(r.Context(), assetID, limit, environment, startDate, endDate)
	webapi.WriteJSON(w, result.HTTPStatus, map[string]any{
		"status":                                 result.Status,
		"columns":                                result.Columns,
		"rows":                                   result.Rows,
		"raw_output":                             result.RawOutput,
		"operation":                              result.Operation,
		"error":                                  result.Error,
		"missing_upstream_asset_ids":             result.MissingUpstreamAssetIDs,
		"missing_upstream_asset_names":           result.MissingUpstreamAssetNames,
		"missing_upstream_assets_materializable": result.MissingUpstreamAssetsMaterializable,
		"attempts":                               result.Attempts,
		"retryable":                              result.Retryable,
	})
}

func (h *ExecutionAPI) HandleMaterializeAssetStream(w http.ResponseWriter, r *http.Request) {
	assetID := chi.URLParam(r, "assetID")
	environment := r.URL.Query().Get("environment")
	scope := r.URL.Query().Get("scope")
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	flusher, ok := w.(http.Flusher)
	if !ok {
		webapi.WriteInternalError(w, "streaming_unsupported", "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	_ = WriteSSEJSON(w, flusher, "start", map[string]any{"operation": webmodel.OperationMetadata{Type: "run", AssetPath: assetID, Target: assetID}})
	result := h.Service.MaterializeAssetStream(r.Context(), assetID, environment, scope, startDate, endDate, func(chunk []byte) {
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
	})
}

func WriteSSEJSON(w http.ResponseWriter, flusher http.Flusher, event string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}

	flusher.Flush()
	return nil
}
