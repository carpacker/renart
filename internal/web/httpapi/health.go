package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
)

// HealthInfo identifies a server (or one project runtime of it) to CLI
// clients: the version powers the skew warning, the workspace root lets the
// client confirm it found a server for *its* workspace.
type HealthInfo struct {
	Version       string `json:"version"`
	WorkspaceRoot string `json:"workspace_root"`
	ProjectID     string `json:"project_id,omitempty"`
}

// RegisterHealthRoutes exposes GET /api/health for CLI server discovery
// (plans/cli-v1.md §2.1).
func RegisterHealthRoutes(router chi.Router, info func() HealthInfo) {
	router.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		payload := info()
		webapi.WriteJSON(w, http.StatusOK, map[string]any{
			"status":         "ok",
			"version":        payload.Version,
			"workspace_root": payload.WorkspaceRoot,
			"project_id":     payload.ProjectID,
		})
	})
}
