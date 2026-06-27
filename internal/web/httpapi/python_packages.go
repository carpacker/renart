package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	webapi "renart/internal/web/api"
	"renart/internal/web/service"
)

// PythonPackagesAPI exposes PyPI package lookup for the dependency suggestion.
type PythonPackagesAPI struct {
	Search func(ctx context.Context, importName string) []service.PyPIPackage
}

// RegisterPythonPackageRoutes mounts the PyPI package search endpoint.
func RegisterPythonPackageRoutes(router chi.Router, handlers *PythonPackagesAPI) {
	router.Get("/api/python/packages", handlers.HandleSearch)
}

func (h *PythonPackagesAPI) HandleSearch(w http.ResponseWriter, r *http.Request) {
	importName := strings.TrimSpace(r.URL.Query().Get("import"))
	if importName == "" {
		importName = strings.TrimSpace(r.URL.Query().Get("q"))
	}
	if importName == "" {
		webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "packages": []service.PyPIPackage{}})
		return
	}
	packages := h.Search(r.Context(), importName)
	if packages == nil {
		packages = []service.PyPIPackage{}
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "packages": packages})
}
