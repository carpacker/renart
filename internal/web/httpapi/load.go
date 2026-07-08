package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	"renart/internal/web/service"
)

type LoadHandlers interface {
	Discover(ctx context.Context, connectionName, pattern, environment string) (service.LoadDiscoveryResult, *service.APIError)
}

type LoadAPI struct {
	Service LoadHandlers
}

func RegisterLoadRoutes(router chi.Router, handlers *LoadAPI) {
	router.Get("/api/load/discover", handlers.HandleLoadDiscover)
}

func (h *LoadAPI) HandleLoadDiscover(w http.ResponseWriter, r *http.Request) {
	connectionName := strings.TrimSpace(r.URL.Query().Get("connection"))
	if connectionName == "" {
		webapi.WriteBadRequest(w, "connection_required", "connection query parameter is required")
		return
	}
	pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
	environment := strings.TrimSpace(r.URL.Query().Get("environment"))

	result, apiErr := h.Service.Discover(r.Context(), connectionName, pattern, environment)
	if apiErr != nil {
		webapi.WriteJSON(w, apiErr.Status, map[string]any{
			"status": "error",
			"error":  map[string]string{"code": apiErr.Code, "message": apiErr.Message},
		})
		return
	}

	webapi.WriteJSON(w, http.StatusOK, result)
}
