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

type FormatPythonAssetRequest struct {
	Content string `json:"content"`
}

type FormatPythonAssetResponse struct {
	Status  string `json:"status"`
	AssetID string `json:"asset_id"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

type PythonDiagnosticsRequest struct {
	Content string `json:"content"`
}

type PythonCompletionsRequest struct {
	Content  string `json:"content"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Snippets bool   `json:"snippets"`
}

type PythonDiagnosticsResponse struct {
	Status      string             `json:"status"`
	AssetID     string             `json:"asset_id"`
	Diagnostics []PythonDiagnostic `json:"diagnostics,omitempty"`
	Error       string             `json:"error,omitempty"`
}

type PythonDiagnostic struct {
	ID       string       `json:"id"`
	Message  string       `json:"message"`
	Severity string       `json:"severity"`
	Range    *PythonRange `json:"range,omitempty"`
	Display  string       `json:"display,omitempty"`
}

type PythonRange struct {
	Start PythonPosition `json:"start"`
	End   PythonPosition `json:"end"`
}

type PythonPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type PythonCompletionsResponse struct {
	Status      string             `json:"status"`
	AssetID     string             `json:"asset_id"`
	Completions []PythonCompletion `json:"completions,omitempty"`
	Error       string             `json:"error,omitempty"`
}

type PythonCompletion struct {
	Label               string           `json:"label"`
	Kind                string           `json:"kind,omitempty"`
	Detail              string           `json:"detail,omitempty"`
	InsertText          string           `json:"insert_text,omitempty"`
	InsertTextFormat    string           `json:"insert_text_format"`
	Documentation       string           `json:"documentation,omitempty"`
	ModuleName          string           `json:"module_name,omitempty"`
	AdditionalTextEdits []PythonTextEdit `json:"additional_text_edits,omitempty"`
}

type PythonTextEdit struct {
	Range PythonRange `json:"range"`
	Text  string      `json:"text"`
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
	FormatPythonAsset(ctx context.Context, assetID string, req FormatPythonAssetRequest) (FormatPythonAssetResponse, *APIError)
	PythonDiagnostics(ctx context.Context, assetID string, req PythonDiagnosticsRequest) (PythonDiagnosticsResponse, *APIError)
	PythonCompletions(ctx context.Context, assetID string, req PythonCompletionsRequest) (PythonCompletionsResponse, *APIError)
}

type AssetsAPI struct {
	Service AssetHandlers
}

func RegisterAssetRoutes(router chi.Router, handlers *AssetsAPI) {
	router.Post("/api/pipelines/{id}/assets", handlers.HandleCreateAsset)
	router.Put("/api/pipelines/{pipelineID}/assets/{assetID}", handlers.HandleUpdateAsset)
	router.Delete("/api/pipelines/{pipelineID}/assets/{assetID}", handlers.HandleDeleteAsset)
	router.Post("/api/assets/{assetID}/format-sql", handlers.HandleFormatSQLAsset)
	router.Post("/api/assets/{assetID}/format-python", handlers.HandleFormatPythonAsset)
	router.Post("/api/assets/{assetID}/python-diagnostics", handlers.HandlePythonDiagnostics)
	router.Post("/api/assets/{assetID}/python-completions", handlers.HandlePythonCompletions)
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

func (h *AssetsAPI) HandleFormatPythonAsset(w http.ResponseWriter, r *http.Request) {
	var req FormatPythonAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.FormatPythonAsset(r.Context(), chi.URLParam(r, "assetID"), req)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *AssetsAPI) HandlePythonDiagnostics(w http.ResponseWriter, r *http.Request) {
	var req PythonDiagnosticsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.PythonDiagnostics(r.Context(), chi.URLParam(r, "assetID"), req)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *AssetsAPI) HandlePythonCompletions(w http.ResponseWriter, r *http.Request) {
	var req PythonCompletionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.PythonCompletions(r.Context(), chi.URLParam(r, "assetID"), req)
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
