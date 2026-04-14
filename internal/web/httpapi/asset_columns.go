package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	webapi "renart/internal/web/api"
	"github.com/go-chi/chi/v5"
)

type AssetColumnsHandlers interface {
	FillColumnsFromDB(ctx context.Context, assetID string) (int, map[string]any, *APIError)
	InferAssetColumns(ctx context.Context, assetID string) (int, map[string]any, *APIError)
	UpdateAssetColumns(ctx context.Context, assetID string, columns []any) (map[string]string, *APIError)
}

type AssetColumnsAPI struct {
	Service AssetColumnsHandlers
}

type UpdateAssetColumnsRequest struct {
	Columns []any `json:"columns"`
}

func RegisterAssetColumnRoutes(router chi.Router, handlers *AssetColumnsAPI) {
	router.Post("/api/assets/{assetID}/fill-columns-from-db", handlers.HandleFillColumnsFromDB)
	router.Get("/api/assets/{assetID}/columns/infer", handlers.HandleInferAssetColumns)
	router.Put("/api/assets/{assetID}/columns", handlers.HandleUpdateAssetColumns)
}

func (h *AssetColumnsAPI) HandleFillColumnsFromDB(w http.ResponseWriter, r *http.Request) {
	status, body, apiErr := h.Service.FillColumnsFromDB(r.Context(), chi.URLParam(r, "assetID"))
	if apiErr != nil {
		webapi.WriteJSON(w, apiErr.Status, map[string]any{
			"status": "error",
			"error":  map[string]string{"code": apiErr.Code, "message": apiErr.Message},
		})
		return
	}
	webapi.WriteJSON(w, status, body)
}

func (h *AssetColumnsAPI) HandleInferAssetColumns(w http.ResponseWriter, r *http.Request) {
	status, body, apiErr := h.Service.InferAssetColumns(r.Context(), chi.URLParam(r, "assetID"))
	if apiErr != nil {
		webapi.WriteJSON(w, apiErr.Status, map[string]any{
			"status": "error",
			"error":  map[string]string{"code": apiErr.Code, "message": apiErr.Message},
		})
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
		webapi.WriteJSON(w, apiErr.Status, map[string]any{
			"status": "error",
			"error":  map[string]string{"code": apiErr.Code, "message": apiErr.Message},
		})
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}
