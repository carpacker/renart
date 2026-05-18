package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
)

type APIError struct {
	Status  int
	Code    string
	Message string
}

type ErrorResponse struct {
	Status string            `json:"status"`
	Error  ErrorResponseBody `json:"error"`
}

type ErrorResponseBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type CreateAssetRequest struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Path            string `json:"path"`
	Content         string `json:"content"`
	SourceAssetID   string `json:"source_asset_id"`
	SeedFileName    string `json:"seed_file_name"`
	SeedFileContent string `json:"seed_file_content"`
}

type UpdateAssetRequest struct {
	Name                *string           `json:"name,omitempty"`
	Type                *string           `json:"type,omitempty"`
	Content             *string           `json:"content,omitempty"`
	MaterializationType *string           `json:"materialization_type,omitempty"`
	Meta                map[string]string `json:"meta,omitempty"`
	Upstreams           []string          `json:"upstreams,omitempty"`
}

type FormatSQLAssetRequest struct {
	Content string `json:"content"`
}

type FormatSQLAssetResponse struct {
	Status  string `json:"status"`
	AssetID string `json:"asset_id"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

type AssetMutationResponse struct {
	Status    string `json:"status"`
	AssetID   string `json:"asset_id,omitempty"`
	AssetPath string `json:"asset_path,omitempty"`
}

type StatusResponse struct {
	Status string `json:"status"`
}

type AssetHandlers interface {
	CreateAsset(ctx context.Context, pipelineID string, req CreateAssetRequest) (AssetMutationResponse, *APIError)
	UpdateAsset(ctx context.Context, assetID string, req UpdateAssetRequest) (AssetMutationResponse, *APIError)
	DeleteAsset(ctx context.Context, assetID string) (StatusResponse, *APIError)
	FormatSQLAsset(ctx context.Context, assetID string, req FormatSQLAssetRequest) (FormatSQLAssetResponse, *APIError)
}

type AssetsAPI struct {
	Service AssetHandlers
}

func RegisterAssetRoutes(router chi.Router, handlers *AssetsAPI) {
	router.Post("/api/pipelines/{id}/assets", handlers.HandleCreateAsset)
	router.Put("/api/pipelines/{pipelineID}/assets/{assetID}", handlers.HandleUpdateAsset)
	router.Delete("/api/pipelines/{pipelineID}/assets/{assetID}", handlers.HandleDeleteAsset)
	router.Post("/api/assets/{assetID}/format-sql", handlers.HandleFormatSQLAsset)
}

func (h *AssetsAPI) HandleCreateAsset(w http.ResponseWriter, r *http.Request) {
	var req CreateAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.CreateAsset(r.Context(), chi.URLParam(r, "id"), req)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusCreated, resp)
}

func (h *AssetsAPI) HandleUpdateAsset(w http.ResponseWriter, r *http.Request) {
	var req UpdateAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.UpdateAsset(r.Context(), chi.URLParam(r, "assetID"), req)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *AssetsAPI) HandleDeleteAsset(w http.ResponseWriter, r *http.Request) {
	resp, apiErr := h.Service.DeleteAsset(r.Context(), chi.URLParam(r, "assetID"))
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *AssetsAPI) HandleFormatSQLAsset(w http.ResponseWriter, r *http.Request) {
	var req FormatSQLAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.FormatSQLAsset(r.Context(), chi.URLParam(r, "assetID"), req)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func writeAPIError(w http.ResponseWriter, apiErr *APIError) {
	webapi.WriteJSON(w, apiErr.Status, ErrorResponse{
		Status: "error",
		Error: ErrorResponseBody{
			Code:    apiErr.Code,
			Message: apiErr.Message,
		},
	})
}
