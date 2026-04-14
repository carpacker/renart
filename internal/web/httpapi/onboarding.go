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

type OnboardingImportService interface {
	ImportDatabase(ctx context.Context, req service.OnboardingImportRequest) service.OnboardingImportResult
	PreviewDiscovery(ctx context.Context, req service.OnboardingDiscoveryRequest) (service.OnboardingDiscoveryResult, int)
	PathSuggestions(prefix string) (service.OnboardingPathSuggestionsResult, int)
	GetState() (service.OnboardingSessionState, error)
	UpdateState(state service.OnboardingSessionState) error
}

type OnboardingChangePublisher interface {
	WorkspaceChanged(ctx context.Context, relPath, eventType string)
	ConfigChanged(ctx context.Context, relPath, eventType string)
}

type OnboardingAPI struct {
	Service   OnboardingImportService
	Publisher OnboardingChangePublisher
}

type ImportDatabaseRequest struct {
	ConnectionName  string `json:"connection_name"`
	EnvironmentName string `json:"environment_name"`
	PipelineName    string `json:"pipeline_name"`
	Schema          string `json:"schema"`
	Pattern         string `json:"pattern"`
	Tables          []string `json:"tables"`
	DisableColumns  bool   `json:"disable_columns"`
	CreateIfMissing bool   `json:"create_if_missing"`
}

type OnboardingDiscoveryRequest struct {
	EnvironmentName string         `json:"environment_name"`
	Type            string         `json:"type"`
	Values          map[string]any `json:"values"`
	Database        string         `json:"database"`
}

func RegisterOnboardingRoutes(router chi.Router, handlers *OnboardingAPI) {
	router.Get("/api/onboarding/state", handlers.HandleGetOnboardingState)
	router.Post("/api/onboarding/import", handlers.HandleImportDatabase)
	router.Post("/api/onboarding/discovery", handlers.HandlePreviewDiscovery)
	router.Get("/api/onboarding/path-suggestions", handlers.HandlePathSuggestions)
	router.Put("/api/onboarding/state", handlers.HandleUpdateOnboardingState)
}

func (h *OnboardingAPI) HandleGetOnboardingState(w http.ResponseWriter, _ *http.Request) {
	state, err := h.Service.GetState()
	if err != nil {
		webapi.WriteInternalError(w, "onboarding_state_load_failed", err.Error())
		return
	}

	webapi.WriteJSON(w, http.StatusOK, state)
}

func (h *OnboardingAPI) HandleImportDatabase(w http.ResponseWriter, r *http.Request) {
	var req ImportDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	result := h.Service.ImportDatabase(r.Context(), service.OnboardingImportRequest{
		ConnectionName:  strings.TrimSpace(req.ConnectionName),
		EnvironmentName: strings.TrimSpace(req.EnvironmentName),
		PipelineName:    strings.TrimSpace(req.PipelineName),
		Schema:          strings.TrimSpace(req.Schema),
		Pattern:         strings.TrimSpace(req.Pattern),
		Tables:          req.Tables,
		DisableColumns:  req.DisableColumns,
		CreateIfMissing: req.CreateIfMissing,
	})

	if result.Status == "ok" && h.Publisher != nil {
		h.Publisher.WorkspaceChanged(r.Context(), result.PipelinePath, "pipeline.imported")
	}

	webapi.WriteJSON(w, result.HTTPCode, map[string]any{
		"status":        result.Status,
		"command":       result.Command,
		"output":        result.Output,
		"error":         result.Error,
		"pipeline_path": result.PipelinePath,
		"asset_paths":   result.AssetPaths,
	})
}

func (h *OnboardingAPI) HandlePreviewDiscovery(w http.ResponseWriter, r *http.Request) {
	var req OnboardingDiscoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	result, status := h.Service.PreviewDiscovery(r.Context(), service.OnboardingDiscoveryRequest{
		EnvironmentName: strings.TrimSpace(req.EnvironmentName),
		Type:            strings.TrimSpace(req.Type),
		Values:          req.Values,
		Database:        strings.TrimSpace(req.Database),
	})
	webapi.WriteJSON(w, status, result)
}

func (h *OnboardingAPI) HandlePathSuggestions(w http.ResponseWriter, r *http.Request) {
	result, status := h.Service.PathSuggestions(strings.TrimSpace(r.URL.Query().Get("prefix")))
	webapi.WriteJSON(w, status, result)
}

func (h *OnboardingAPI) HandleUpdateOnboardingState(w http.ResponseWriter, r *http.Request) {
	var req service.OnboardingSessionState
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	if err := h.Service.UpdateState(req); err != nil {
		webapi.WriteInternalError(w, "onboarding_state_update_failed", err.Error())
		return
	}

	if h.Publisher != nil {
		h.Publisher.ConfigChanged(r.Context(), ".bruin-web-onboarding.json", "config.updated")
	}

	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
