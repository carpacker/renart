package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	"renart/internal/web/model"
	"renart/internal/web/service"
)

// NotebookHandlers is the service surface the notebook routes need.
type NotebookHandlers interface {
	Get(notebookID string) (model.Notebook, *service.APIError)
	Create(req service.CreateNotebookRequest) (model.Notebook, *service.APIError)
	Delete(notebookID string) *service.APIError
	CloseSession(notebookID string) *service.APIError
	CreateCell(notebookID string, req service.CreateCellRequest) (model.Notebook, *service.APIError)
	UpdateCell(notebookID, cellID, content string) (model.Notebook, *service.APIError)
	RenameCell(notebookID, cellID, newName string) (model.Notebook, *service.APIError)
	DeleteCell(notebookID, cellID string) (model.Notebook, *service.APIError)
	UpdateBlocks(notebookID string, blocks []model.NotebookBlock) (model.Notebook, *service.APIError)
	UpdateDependencies(notebookID, content string) (model.Notebook, *service.APIError)
	PromoteCell(notebookID, cellID string, req service.PromoteCellRequest) (service.PromoteCellResult, *service.APIError)
	Run(ctx context.Context, notebookID string, req service.RunNotebookRequest) (service.RunNotebookResult, *service.APIError)
	Runtime(notebookID string) (service.NotebookRuntimeSnapshot, *service.APIError)
	SetAutoRecompute(notebookID string, enabled bool, environment string) *service.APIError
	CancelAutoRecompute(notebookID string) *service.APIError
}

// NotebookAPI exposes notebook CRUD and execution.
type NotebookAPI struct {
	Service NotebookHandlers
}

// RegisterNotebookRoutes mounts the notebook endpoints.
func RegisterNotebookRoutes(router chi.Router, handlers *NotebookAPI) {
	router.Post("/api/notebooks", handlers.HandleCreate)
	router.Get("/api/notebooks/{id}", handlers.HandleGet)
	router.Delete("/api/notebooks/{id}", handlers.HandleDelete)
	router.Delete("/api/notebooks/{id}/session", handlers.HandleCloseSession)
	router.Post("/api/notebooks/{id}/cells", handlers.HandleCreateCell)
	router.Put("/api/notebooks/{id}/cells/{cellID}", handlers.HandleUpdateCell)
	router.Post("/api/notebooks/{id}/cells/{cellID}/rename", handlers.HandleRenameCell)
	router.Delete("/api/notebooks/{id}/cells/{cellID}", handlers.HandleDeleteCell)
	router.Put("/api/notebooks/{id}/blocks", handlers.HandleUpdateBlocks)
	router.Put("/api/notebooks/{id}/dependencies", handlers.HandleUpdateDependencies)
	router.Post("/api/notebooks/{id}/cells/{cellID}/promote", handlers.HandlePromoteCell)
	router.Post("/api/notebooks/{id}/run", handlers.HandleRun)
	router.Get("/api/notebooks/{id}/runtime", handlers.HandleRuntime)
	router.Put("/api/notebooks/{id}/settings", handlers.HandleSettings)
	router.Post("/api/notebooks/{id}/cancel", handlers.HandleCancel)
}

func writeNotebookError(w http.ResponseWriter, apiErr *service.APIError) {
	webapi.WriteJSON(w, apiErr.Status, map[string]any{
		"status": "error",
		"error":  map[string]string{"code": apiErr.Code, "message": apiErr.Message},
	})
}

func (h *NotebookAPI) HandleGet(w http.ResponseWriter, r *http.Request) {
	nb, apiErr := h.Service.Get(chi.URLParam(r, "id"))
	if apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "notebook": nb})
}

func (h *NotebookAPI) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req service.CreateNotebookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	nb, apiErr := h.Service.Create(req)
	if apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusCreated, map[string]any{"status": "ok", "notebook": nb})
}

func (h *NotebookAPI) HandleDelete(w http.ResponseWriter, r *http.Request) {
	if apiErr := h.Service.Delete(chi.URLParam(r, "id")); apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *NotebookAPI) HandleCloseSession(w http.ResponseWriter, r *http.Request) {
	if apiErr := h.Service.CloseSession(chi.URLParam(r, "id")); apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *NotebookAPI) HandleCreateCell(w http.ResponseWriter, r *http.Request) {
	var req service.CreateCellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	nb, apiErr := h.Service.CreateCell(chi.URLParam(r, "id"), req)
	if apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusCreated, map[string]any{"status": "ok", "notebook": nb})
}

// UpdateCellRequest carries the full new cell file content.
type UpdateCellRequest struct {
	Content string `json:"content"`
}

func (h *NotebookAPI) HandleUpdateCell(w http.ResponseWriter, r *http.Request) {
	var req UpdateCellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	nb, apiErr := h.Service.UpdateCell(chi.URLParam(r, "id"), chi.URLParam(r, "cellID"), req.Content)
	if apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "notebook": nb})
}

// UpdateDependenciesRequest carries the newline-separated dependency list.
type UpdateDependenciesRequest struct {
	Content string `json:"content"`
}

func (h *NotebookAPI) HandleUpdateDependencies(w http.ResponseWriter, r *http.Request) {
	var req UpdateDependenciesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	nb, apiErr := h.Service.UpdateDependencies(chi.URLParam(r, "id"), req.Content)
	if apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "notebook": nb})
}

// RenameCellRequest carries the new display name for a cell.
type RenameCellRequest struct {
	Name string `json:"name"`
}

func (h *NotebookAPI) HandleRenameCell(w http.ResponseWriter, r *http.Request) {
	var req RenameCellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	nb, apiErr := h.Service.RenameCell(chi.URLParam(r, "id"), chi.URLParam(r, "cellID"), req.Name)
	if apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "notebook": nb})
}

func (h *NotebookAPI) HandleDeleteCell(w http.ResponseWriter, r *http.Request) {
	nb, apiErr := h.Service.DeleteCell(chi.URLParam(r, "id"), chi.URLParam(r, "cellID"))
	if apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "notebook": nb})
}

// UpdateBlocksRequest replaces the notebook's ordered blocks.
type UpdateBlocksRequest struct {
	Blocks []model.NotebookBlock `json:"blocks"`
}

func (h *NotebookAPI) HandleUpdateBlocks(w http.ResponseWriter, r *http.Request) {
	var req UpdateBlocksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	nb, apiErr := h.Service.UpdateBlocks(chi.URLParam(r, "id"), req.Blocks)
	if apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "notebook": nb})
}

func (h *NotebookAPI) HandlePromoteCell(w http.ResponseWriter, r *http.Request) {
	var req service.PromoteCellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	result, apiErr := h.Service.PromoteCell(chi.URLParam(r, "id"), chi.URLParam(r, "cellID"), req)
	if apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, result)
}

func (h *NotebookAPI) HandleRun(w http.ResponseWriter, r *http.Request) {
	var req service.RunNotebookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	if req.Environment == "" {
		req.Environment = r.URL.Query().Get("environment")
	}
	result, apiErr := h.Service.Run(r.Context(), chi.URLParam(r, "id"), req)
	if apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, result)
}

func (h *NotebookAPI) HandleRuntime(w http.ResponseWriter, r *http.Request) {
	snapshot, apiErr := h.Service.Runtime(chi.URLParam(r, "id"))
	if apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, snapshot)
}

func (h *NotebookAPI) HandleSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AutoRecompute bool   `json:"auto_recompute"`
		Environment   string `json:"environment,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	if apiErr := h.Service.SetAutoRecompute(chi.URLParam(r, "id"), req.AutoRecompute, req.Environment); apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *NotebookAPI) HandleCancel(w http.ResponseWriter, r *http.Request) {
	if apiErr := h.Service.CancelAutoRecompute(chi.URLParam(r, "id")); apiErr != nil {
		writeNotebookError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
