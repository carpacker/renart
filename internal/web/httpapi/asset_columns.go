package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	"renart/internal/web/service"
)

type AssetColumnsHandlers interface {
	FillColumnsFromDB(ctx context.Context, assetID string) (int, map[string]any, *APIError)
	InferAssetColumns(ctx context.Context, assetID string) (int, map[string]any, *APIError)
	InferAPIAsset(ctx context.Context, assetID string) (int, service.APIInferResult, *APIError)
	UpdateAssetColumns(ctx context.Context, assetID string, columns []any) (StatusResponse, *APIError)
	ReconcileAssetColumns(ctx context.Context, assetID string, inferred []WorkspaceColumn) (ColumnReconcileResult, *APIError)
	RefreshAssetColumnsFromDefinition(ctx context.Context, assetID string) (ColumnReconcileResult, *APIError)
	PreviewAssetColumns(ctx context.Context, assetID string, sourceID string, environment string) (service.ColumnInferencePreview, *APIError)
}

type AssetColumnsAPI struct {
	Service AssetColumnsHandlers
}

type UpdateAssetColumnsRequest struct {
	Columns []any `json:"columns"`
}

type ReconcileAssetColumnsRequest struct {
	// Columns is the freshly inferred column set (name + type) to merge into the
	// asset's existing columns, preserving user-authored metadata.
	Columns []WorkspaceColumn `json:"columns"`
}

type PreviewAssetColumnsRequest struct {
	Source      string `json:"source"`
	Environment string `json:"environment,omitempty"`
}

func RegisterAssetColumnRoutes(router chi.Router, handlers *AssetColumnsAPI) {
	router.Post("/api/assets/{assetID}/fill-columns-from-db", handlers.HandleFillColumnsFromDB)
	router.Get("/api/assets/{assetID}/columns/infer", handlers.HandleInferAssetColumns)
	router.Post("/api/assets/{assetID}/api-infer", handlers.HandleInferAPIAsset)
	router.Put("/api/assets/{assetID}/columns", handlers.HandleUpdateAssetColumns)
	router.Post("/api/assets/{assetID}/columns/reconcile", handlers.HandleReconcileAssetColumns)
	router.Post("/api/assets/{assetID}/columns/refresh-from-definition", handlers.HandleRefreshAssetColumnsFromDefinition)
	router.Post("/api/assets/{assetID}/columns/preview", handlers.HandlePreviewAssetColumns)
}

func (h *AssetColumnsAPI) HandlePreviewAssetColumns(w http.ResponseWriter, r *http.Request) {
	var req PreviewAssetColumnsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	preview, apiErr := h.Service.PreviewAssetColumns(
		r.Context(),
		chi.URLParam(r, "assetID"),
		req.Source,
		req.Environment,
	)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, preview)
}

func (h *AssetColumnsAPI) HandleFillColumnsFromDB(w http.ResponseWriter, r *http.Request) {
	status, body, apiErr := h.Service.FillColumnsFromDB(r.Context(), chi.URLParam(r, "assetID"))
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, status, body)
}

func (h *AssetColumnsAPI) HandleInferAssetColumns(w http.ResponseWriter, r *http.Request) {
	status, body, apiErr := h.Service.InferAssetColumns(r.Context(), chi.URLParam(r, "assetID"))
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, status, body)
}

func (h *AssetColumnsAPI) HandleInferAPIAsset(w http.ResponseWriter, r *http.Request) {
	status, body, apiErr := h.Service.InferAPIAsset(r.Context(), chi.URLParam(r, "assetID"))
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, status, body)
}

func (h *AssetColumnsAPI) HandleUpdateAssetColumns(w http.ResponseWriter, r *http.Request) {
	var req UpdateAssetColumnsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.UpdateAssetColumns(r.Context(), chi.URLParam(r, "assetID"), req.Columns)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *AssetColumnsAPI) HandleReconcileAssetColumns(w http.ResponseWriter, r *http.Request) {
	var req ReconcileAssetColumnsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.ReconcileAssetColumns(r.Context(), chi.URLParam(r, "assetID"), req.Columns)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"columns":         resp.Columns,
		"reconcile_items": resp.ReconcileItems,
	})
}

func (h *AssetColumnsAPI) HandleRefreshAssetColumnsFromDefinition(w http.ResponseWriter, r *http.Request) {
	resp, apiErr := h.Service.RefreshAssetColumnsFromDefinition(r.Context(), chi.URLParam(r, "assetID"))
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"columns":         resp.Columns,
		"reconcile_items": resp.ReconcileItems,
	})
}
