package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	webmodel "renart/internal/web/model"
	"renart/internal/web/service"
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
	router.Get("/api/pipelines/{id}/config", handlers.HandleGetPipelineConfig)
	router.Put("/api/pipelines/{id}/config", handlers.HandleUpdatePipelineConfig)
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
	webapi.WriteJSON(w, http.StatusCreated, StatusResponse{Status: "ok"})
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
	webapi.WriteJSON(w, http.StatusOK, StatusResponse{Status: "ok"})
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
	webapi.WriteJSON(w, http.StatusOK, StatusResponse{Status: "ok"})
}

func (h *PipelineHandlers) HandleGetPipelineConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	resp, err := h.Service.GetConfig(r.Context(), id)
	if err != nil {
		h.writePipelineConfigError(w, err)
		return
	}

	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *PipelineHandlers) HandleUpdatePipelineConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req webmodel.UpdatePipelineConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	relPath, resp, err := h.Service.UpdateConfig(r.Context(), id, req)
	if err != nil {
		h.writePipelineConfigError(w, err)
		return
	}

	if h.Publisher != nil {
		h.Publisher.WorkspaceChanged(r.Context(), relPath, "pipeline.updated")
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *PipelineHandlers) writePipelineConfigError(w http.ResponseWriter, err error) {
	message := err.Error()
	switch {
	case strings.Contains(message, "illegal base64"):
		webapi.WriteBadRequest(w, "invalid_pipeline_id", "invalid pipeline id")
	case strings.Contains(message, "invalid path"):
		webapi.WriteBadRequest(w, "invalid_pipeline_path", message)
	case strings.Contains(message, "yaml") || strings.Contains(message, "parse") || strings.Contains(message, "variable") || strings.Contains(message, "interval modifier"):
		webapi.WriteBadRequest(w, "pipeline_config_invalid", message)
	default:
		webapi.WriteInternalError(w, "pipeline_config_failed", message)
	}
}
