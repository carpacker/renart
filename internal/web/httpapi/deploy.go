package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	"renart/internal/web/snapshot"
)

// DeployAPI exposes snapshot deploys and drift inspection.
type DeployAPI struct {
	Snapshots *snapshot.Store
	// ResolvePipeline maps the path-encoded API pipeline ID to the stable
	// UUID and the absolute pipeline directory.
	ResolvePipeline func(pipelineID string) (pipelineUUID, absDir string, ok bool)
}

func RegisterDeployRoutes(router chi.Router, handlers *DeployAPI) {
	router.Post("/api/pipelines/{id}/deploy", handlers.HandleDeploy)
	router.Get("/api/pipelines/{id}/deploy/status", handlers.HandleDeployStatus)
	router.Get("/api/pipelines/{id}/snapshots", handlers.HandleListSnapshots)
	router.Get("/api/snapshots/{versionId}/file", handlers.HandleSnapshotFile)
}

func (h *DeployAPI) HandleDeploy(w http.ResponseWriter, r *http.Request) {
	pipelineUUID, absDir, ok := h.ResolvePipeline(chi.URLParam(r, "id"))
	if !ok {
		webapi.WriteNotFound(w, "pipeline_not_found", "pipeline not found")
		return
	}
	deployed, created, err := h.Snapshots.Deploy(r.Context(), pipelineUUID, absDir, "web")
	if err != nil {
		webapi.WriteInternalError(w, "deploy_failed", err.Error())
		return
	}
	message := "deployed new version"
	if !created {
		message = "already up to date with the latest snapshot"
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"created":  created,
		"message":  message,
		"snapshot": snapshotSummary(deployed),
	})
}

func (h *DeployAPI) HandleDeployStatus(w http.ResponseWriter, r *http.Request) {
	pipelineUUID, absDir, ok := h.ResolvePipeline(chi.URLParam(r, "id"))
	if !ok {
		webapi.WriteNotFound(w, "pipeline_not_found", "pipeline not found")
		return
	}
	report, err := h.Snapshots.Drift(r.Context(), pipelineUUID, absDir)
	if err != nil {
		webapi.WriteInternalError(w, "deploy_status_failed", err.Error())
		return
	}
	webapi.WriteJSON(w, http.StatusOK, report)
}

func (h *DeployAPI) HandleListSnapshots(w http.ResponseWriter, r *http.Request) {
	pipelineUUID, _, ok := h.ResolvePipeline(chi.URLParam(r, "id"))
	if !ok {
		webapi.WriteNotFound(w, "pipeline_not_found", "pipeline not found")
		return
	}
	snapshots, err := h.Snapshots.List(r.Context(), pipelineUUID)
	if err != nil {
		webapi.WriteInternalError(w, "snapshot_list_failed", err.Error())
		return
	}
	summaries := make([]map[string]any, 0, len(snapshots))
	for _, item := range snapshots {
		summaries = append(summaries, snapshotSummary(item))
	}
	webapi.WriteJSON(w, http.StatusOK, map[string]any{"snapshots": summaries})
}

// HandleSnapshotFile serves one file's deployed content (the per-asset diff
// view's "deployed side").
func (h *DeployAPI) HandleSnapshotFile(w http.ResponseWriter, r *http.Request) {
	versionID := chi.URLParam(r, "versionId")
	relPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if relPath == "" {
		webapi.WriteBadRequest(w, "path_required", "path query parameter is required")
		return
	}
	deployed, err := h.Snapshots.Get(r.Context(), versionID)
	if err != nil {
		webapi.WriteNotFound(w, "snapshot_not_found", "snapshot not found")
		return
	}
	hash, ok := deployed.Manifest[relPath]
	if !ok {
		webapi.WriteNotFound(w, "file_not_in_snapshot", "file is not part of this snapshot")
		return
	}
	content, err := h.Snapshots.BlobContent(r.Context(), hash)
	if err != nil {
		webapi.WriteInternalError(w, "blob_read_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(content)
}

func snapshotSummary(item snapshot.Snapshot) map[string]any {
	return map[string]any{
		"version_id":  item.VersionID,
		"pipeline_id": item.PipelineUUID,
		"merkle_root": item.MerkleRoot,
		"file_count":  len(item.Manifest),
		"git_sha":     item.GitSHA,
		"git_dirty":   item.GitDirty,
		"created_at":  item.CreatedAt,
		"created_by":  item.CreatedBy,
	}
}
