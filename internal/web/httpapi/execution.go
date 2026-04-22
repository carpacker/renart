package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	webapi "renart/internal/web/api"
	"github.com/go-chi/chi/v5"
)

type InspectExecutionResult struct {
	Status     string
	Columns    []string
	Rows       []map[string]any
	RawOutput  string
	Command    []string
	Error      string
	Attempts   int
	Retryable  bool
	HTTPStatus int
}

type MaterializeExecutionEvent struct {
	Status          string
	Command         []string
	Output          string
	Error           string
	ExitCode        int
	ChangedAssetIDs []string
	MaterializedAt  *time.Time
}

type ExecutionHandlers interface {
	InspectAsset(ctx context.Context, assetID, limit, environment string) InspectExecutionResult
	MaterializeAssetStream(ctx context.Context, assetID, environment string, onChunk func([]byte)) MaterializeExecutionEvent
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
	if limit == "" {
		limit = "200"
	}

	result := h.Service.InspectAsset(r.Context(), assetID, limit, environment)
	webapi.WriteJSON(w, result.HTTPStatus, map[string]any{
		"status":     result.Status,
		"columns":    result.Columns,
		"rows":       result.Rows,
		"raw_output": result.RawOutput,
		"command":    result.Command,
		"error":      result.Error,
		"attempts":   result.Attempts,
		"retryable":  result.Retryable,
	})
}

func (h *ExecutionAPI) HandleMaterializeAssetStream(w http.ResponseWriter, r *http.Request) {
	assetID := chi.URLParam(r, "assetID")
	environment := r.URL.Query().Get("environment")
	flusher, ok := w.(http.Flusher)
	if !ok {
		webapi.WriteInternalError(w, "streaming_unsupported", "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	_ = WriteSSEJSON(w, flusher, "start", map[string]any{"command": []string{"run", assetID}})
	result := h.Service.MaterializeAssetStream(r.Context(), assetID, environment, func(chunk []byte) {
		_ = WriteSSEJSON(w, flusher, "output", map[string]any{"chunk": string(chunk)})
	})
	_ = WriteSSEJSON(w, flusher, "done", map[string]any{
		"status":            result.Status,
		"command":           result.Command,
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
