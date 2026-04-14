package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	webapi "renart/internal/web/api"
	"renart/internal/web/service"
	"github.com/go-chi/chi/v5"
)

type PipelineChangePublisher interface {
	WorkspaceChanged(ctx context.Context, relPath, eventType string)
}

type PipelineHandlers struct {
	Service   *service.PipelineService
	Publisher PipelineChangePublisher
}

type CreatePipelineRequest struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type UpdatePipelineRequest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func RegisterPipelineRoutes(router chi.Router, handlers *PipelineHandlers) {
	router.Post("/api/pipelines", handlers.HandleCreatePipeline)
	router.Put("/api/pipelines", handlers.HandleUpdatePipeline)
	router.Delete("/api/pipelines/{id}", handlers.HandleDeletePipeline)
}

func (h *PipelineHandlers) HandleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	var req CreatePipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		webapi.WriteBadRequest(w, "pipeline_path_required", "path is required")
		return
	}

	relPath, err := h.Service.Create(r.Context(), req.Path, req.Name, req.Content)
	if err != nil {
		if strings.Contains(err.Error(), "invalid path") {
			webapi.WriteBadRequest(w, "invalid_pipeline_path", err.Error())
			return
		}
		if strings.Contains(err.Error(), "mkdir") || strings.Contains(err.Error(), "permission") {
			webapi.WriteInternalError(w, "pipeline_create_failed", err.Error())
			return
		}
		webapi.WriteInternalError(w, "pipeline_write_failed", err.Error())
		return
	}

	if h.Publisher != nil {
		h.Publisher.WorkspaceChanged(r.Context(), relPath, "pipeline.created")
	}
	webapi.WriteJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *PipelineHandlers) HandleUpdatePipeline(w http.ResponseWriter, r *http.Request) {
	var req UpdatePipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	relPath, err := h.Service.Update(r.Context(), req.ID, req.Name, req.Content)
	if err != nil {
		message := err.Error()
		switch {
		case strings.Contains(message, "illegal base64"):
			webapi.WriteBadRequest(w, "invalid_pipeline_id", "invalid pipeline id")
		case strings.Contains(message, "invalid path"):
			webapi.WriteBadRequest(w, "invalid_pipeline_path", message)
		case strings.Contains(message, "yaml") || strings.Contains(message, "parse"):
			webapi.WriteBadRequest(w, "pipeline_parse_failed", message)
		default:
			webapi.WriteInternalError(w, "pipeline_write_failed", message)
		}
		return
	}

	if h.Publisher != nil {
		h.Publisher.WorkspaceChanged(r.Context(), relPath, "pipeline.updated")
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *PipelineHandlers) HandleDeletePipeline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	relPath, err := h.Service.Delete(id)
	if err != nil {
		message := err.Error()
		switch {
		case strings.Contains(message, "illegal base64"):
			webapi.WriteBadRequest(w, "invalid_pipeline_id", "invalid pipeline id")
		case strings.Contains(message, "invalid path"):
			webapi.WriteBadRequest(w, "invalid_pipeline_path", message)
		default:
			webapi.WriteInternalError(w, "pipeline_delete_failed", message)
		}
		return
	}

	if h.Publisher != nil {
		h.Publisher.WorkspaceChanged(r.Context(), relPath, "pipeline.deleted")
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
