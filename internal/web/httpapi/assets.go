package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	"renart/internal/web/service"
)

// APIError is the shared service error shape, re-exported for handlers.
type APIError = service.APIError

type ErrorResponse struct {
	Status string            `json:"status"`
	Error  ErrorResponseBody `json:"error"`
}

type ErrorResponseBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Asset request/response DTOs are defined once in the service package;
// these aliases keep the httpapi names stable for handlers and consumers.
type (
	CreateAssetRequest           = service.CreateAssetParams
	UpdateAssetRequest           = service.AssetUpdateRequest
	WorkspaceColumn              = service.WorkspaceColumn
	ColumnReconcileResult        = service.ColumnReconcileResult
	AssetTransaction             = service.AssetTransaction
	AssetTransactionResult       = service.AssetTransactionResult
	FormatSQLAssetRequest        = service.FormatSQLAssetRequest
	FormatSQLAssetResponse       = service.FormatSQLAssetResponse
	FormatPythonAssetRequest     = service.FormatPythonAssetRequest
	FormatPythonAssetResponse    = service.FormatPythonAssetResponse
	PythonDiagnosticsRequest     = service.PythonDiagnosticsRequest
	PythonCompletionsRequest     = service.PythonCompletionsRequest
	PythonPositionRequest        = service.PythonPositionRequest
	PythonDiagnosticsResponse    = service.PythonDiagnosticsResponse
	PythonDiagnostic             = service.PythonDiagnostic
	PythonRange                  = service.PythonRange
	PythonPosition               = service.PythonPosition
	PythonCompletionsResponse    = service.PythonCompletionsResponse
	PythonCompletion             = service.PythonCompletion
	PythonTextEdit               = service.PythonTextEdit
	PythonHoverResponse          = service.PythonHoverResponse
	PythonHover                  = service.PythonHover
	PythonSignatureHelpResponse  = service.PythonSignatureHelpResponse
	PythonSignatureHelp          = service.PythonSignatureHelp
	PythonSignature              = service.PythonSignature
	PythonSignatureParameter     = service.PythonSignatureParameter
	PythonGotoDefinitionResponse = service.PythonGotoDefinitionResponse
	PythonGotoTarget             = service.PythonGotoTarget
	PythonDepsResponse           = service.PythonDepsResponse
	AddPythonDependencyRequest   = service.AddPythonDependencyRequest
	AssetMutationResponse        = service.AssetMutationResponse
	StatusResponse               = service.StatusResponse
)

type AssetHandlers interface {
	Create(ctx context.Context, pipelineID string, req CreateAssetRequest) (AssetMutationResponse, *APIError)
	Update(ctx context.Context, assetID string, req UpdateAssetRequest) (AssetMutationResponse, *APIError)
	Delete(ctx context.Context, assetID string) (StatusResponse, *APIError)
	FormatSQL(ctx context.Context, assetID string, req FormatSQLAssetRequest) (FormatSQLAssetResponse, *APIError)
	FormatPython(ctx context.Context, assetID string, req FormatPythonAssetRequest) (FormatPythonAssetResponse, *APIError)
	PythonDiagnostics(ctx context.Context, assetID string, req PythonDiagnosticsRequest) (PythonDiagnosticsResponse, *APIError)
	PythonCompletions(ctx context.Context, assetID string, req PythonCompletionsRequest) (PythonCompletionsResponse, *APIError)
	PythonHover(ctx context.Context, assetID string, req PythonPositionRequest) (PythonHoverResponse, *APIError)
	PythonSignatureHelp(ctx context.Context, assetID string, req PythonPositionRequest) (PythonSignatureHelpResponse, *APIError)
	PythonGotoDefinition(ctx context.Context, assetID string, req PythonPositionRequest) (PythonGotoDefinitionResponse, *APIError)
	PythonDeps(assetID string) (PythonDepsResponse, *APIError)
	AddPythonDependency(ctx context.Context, assetID string, req AddPythonDependencyRequest) (PythonDepsResponse, *APIError)
	ApplyAssetTransaction(ctx context.Context, assetID string, tx AssetTransaction) (AssetTransactionResult, *APIError)
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
	router.Post("/api/assets/{assetID}/python-hover", handlers.HandlePythonHover)
	router.Post("/api/assets/{assetID}/python-signature-help", handlers.HandlePythonSignatureHelp)
	router.Post("/api/assets/{assetID}/python-goto-definition", handlers.HandlePythonGotoDefinition)
	router.Get("/api/assets/{assetID}/python-deps", handlers.HandlePythonDeps)
	router.Post("/api/assets/{assetID}/python-deps", handlers.HandleAddPythonDependency)
	router.Post("/api/assets/{assetID}/transactions", handlers.HandleApplyAssetTransaction)
}

func (h *AssetsAPI) HandleCreateAsset(w http.ResponseWriter, r *http.Request) {
	var req CreateAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.Create(r.Context(), chi.URLParam(r, "id"), req)
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
	resp, apiErr := h.Service.Update(r.Context(), chi.URLParam(r, "assetID"), req)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *AssetsAPI) HandleApplyAssetTransaction(w http.ResponseWriter, r *http.Request) {
	var tx AssetTransaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.ApplyAssetTransaction(r.Context(), chi.URLParam(r, "assetID"), tx)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *AssetsAPI) HandleDeleteAsset(w http.ResponseWriter, r *http.Request) {
	resp, apiErr := h.Service.Delete(r.Context(), chi.URLParam(r, "assetID"))
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
	resp, apiErr := h.Service.FormatSQL(r.Context(), chi.URLParam(r, "assetID"), req)
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
	resp, apiErr := h.Service.FormatPython(r.Context(), chi.URLParam(r, "assetID"), req)
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

func (h *AssetsAPI) HandlePythonHover(w http.ResponseWriter, r *http.Request) {
	var req PythonPositionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.PythonHover(r.Context(), chi.URLParam(r, "assetID"), req)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *AssetsAPI) HandlePythonSignatureHelp(w http.ResponseWriter, r *http.Request) {
	var req PythonPositionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.PythonSignatureHelp(r.Context(), chi.URLParam(r, "assetID"), req)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *AssetsAPI) HandlePythonGotoDefinition(w http.ResponseWriter, r *http.Request) {
	var req PythonPositionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.PythonGotoDefinition(r.Context(), chi.URLParam(r, "assetID"), req)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *AssetsAPI) HandlePythonDeps(w http.ResponseWriter, r *http.Request) {
	resp, apiErr := h.Service.PythonDeps(chi.URLParam(r, "assetID"))
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	webapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *AssetsAPI) HandleAddPythonDependency(w http.ResponseWriter, r *http.Request) {
	var req AddPythonDependencyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	resp, apiErr := h.Service.AddPythonDependency(r.Context(), chi.URLParam(r, "assetID"), req)
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
