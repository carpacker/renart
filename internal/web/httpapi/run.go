package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	"renart/internal/web/service"
)

type RunHandlers interface {
	Run(ctx context.Context, req service.RunRequest) service.RunResult
}

type RunAPI struct {
	Service RunHandlers
}

func RegisterRunRoutes(router chi.Router, handlers *RunAPI) {
	router.Post("/api/run", handlers.HandleRun)
}

func (h *RunAPI) HandleRun(w http.ResponseWriter, r *http.Request) {
	var req service.RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	result := h.Service.Run(r.Context(), req)
	webapi.WriteJSON(w, result.HTTPCode, map[string]any{
		"status":    result.Status,
		"operation": result.Operation,
		"output":    result.Output,
		"error":     result.Error,
		"exit_code": result.ExitCode,
	})
}
