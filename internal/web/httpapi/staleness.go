package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	"renart/internal/web/staleness"
)

// StalenessAPI serves per-asset staleness classifications for the current
// selection. Pushes after the initial fetch arrive over the workspace SSE
// stream as staleness.updated events.
type StalenessAPI struct {
	Service *staleness.Service
	// ResolvePipelineUUID maps the path-encoded API pipeline ID to the
	// stable pipeline UUID.
	ResolvePipelineUUID func(pipelineID string) (string, bool)
}

func RegisterStalenessRoutes(router chi.Router, handlers *StalenessAPI) {
	router.Get("/api/pipelines/{id}/staleness", handlers.HandleGetStaleness)
}

func (h *StalenessAPI) HandleGetStaleness(w http.ResponseWriter, r *http.Request) {
	pipelineID := chi.URLParam(r, "id")
	pipelineUUID, ok := h.ResolvePipelineUUID(pipelineID)
	if !ok {
		webapi.WriteNotFound(w, "pipeline_not_found", "pipeline not found")
		return
	}

	selection := staleness.Selection{
		PipelineUUID:      pipelineUUID,
		EncodedPipelineID: pipelineID,
		Environment:       strings.TrimSpace(r.URL.Query().Get("environment")),
	}
	if start, ok := parseQueryTime(r, "start"); ok {
		selection.Start = &start
	}
	if end, ok := parseQueryTime(r, "end"); ok {
		selection.End = &end
	}

	snapshot, err := h.Service.Snapshot(r.Context(), selection)
	if err != nil {
		webapi.WriteInternalError(w, "staleness_compute_failed", err.Error())
		return
	}

	webapi.WriteJSON(w, http.StatusOK, map[string]any{
		"pipeline_id":      pipelineID,
		"pipeline_uuid":    pipelineUUID,
		"environment":      selection.Environment,
		"data_state_token": snapshot.DataStateToken,
		"assets":           snapshot.Assets,
	})
}

func parseQueryTime(r *http.Request, key string) (time.Time, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}
