package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	"renart/internal/web/service"
)

type SuggestionsHandlers interface {
	Ingestr(ctx context.Context, connectionName, prefix, environment string) (service.IngestrSuggestionsResult, *service.SuggestionAPIError)
	SQLPath(ctx context.Context, assetID, prefix, environment string) (service.SQLPathSuggestionsResult, *service.SuggestionAPIError)
}

type SuggestionsAPI struct {
	Service SuggestionsHandlers
}

func RegisterSuggestionRoutes(router chi.Router, handlers *SuggestionsAPI) {
	router.Get("/api/ingestr/suggestions", handlers.HandleGetIngestrSuggestions)
	router.Get("/api/assets/{assetID}/sql-path-suggestions", handlers.HandleGetSQLPathSuggestions)
}

func (h *SuggestionsAPI) HandleGetIngestrSuggestions(w http.ResponseWriter, r *http.Request) {
	connectionName := strings.TrimSpace(r.URL.Query().Get("connection"))
	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	environment := strings.TrimSpace(r.URL.Query().Get("environment"))
	if connectionName == "" {
		webapi.WriteBadRequest(w, "connection_required", "connection query parameter is required")
		return
	}

	result, apiErr := h.Service.Ingestr(r.Context(), connectionName, prefix, environment)
	if apiErr != nil {
		webapi.WriteJSON(w, apiErr.Status, map[string]any{
			"status": "error",
			"error":  map[string]string{"code": apiErr.Code, "message": apiErr.Message},
		})
		return
	}

	webapi.WriteJSON(w, http.StatusOK, result)
}

func (h *SuggestionsAPI) HandleGetSQLPathSuggestions(w http.ResponseWriter, r *http.Request) {
	assetID := strings.TrimSpace(chi.URLParam(r, "assetID"))
	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	environment := strings.TrimSpace(r.URL.Query().Get("environment"))
	if assetID == "" {
		webapi.WriteBadRequest(w, "asset_id_required", "asset ID is required")
		return
	}

	result, apiErr := h.Service.SQLPath(r.Context(), assetID, prefix, environment)
	if apiErr != nil {
		webapi.WriteJSON(w, apiErr.Status, map[string]any{
			"status": "error",
			"error":  map[string]string{"code": apiErr.Code, "message": apiErr.Message},
		})
		return
	}

	webapi.WriteJSON(w, http.StatusOK, result)
}
