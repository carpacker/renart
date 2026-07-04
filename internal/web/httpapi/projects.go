package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
)

// ProjectInfo describes one project known to this server: a registry entry
// enriched with runtime state.
type ProjectInfo struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	Type         string    `json:"type"`
	LastOpenedAt time.Time `json:"last_opened_at"`
	Open         bool      `json:"open"`
	Exists       bool      `json:"exists"`
	Default      bool      `json:"default"`
}

type ProjectListResponse struct {
	Status           string        `json:"status"`
	DefaultProjectID string        `json:"default_project_id"`
	Projects         []ProjectInfo `json:"projects"`
}

type OpenProjectRequest struct {
	Path string `json:"path"`
}

type OpenProjectResponse struct {
	Status  string      `json:"status"`
	Project ProjectInfo `json:"project"`
}

type BrowseDirEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsProject bool   `json:"is_project"`
}

type BrowseDirsResponse struct {
	Status  string           `json:"status"`
	Path    string           `json:"path"`
	Parent  string           `json:"parent,omitempty"`
	Entries []BrowseDirEntry `json:"entries"`
}

// ProjectDirectory is the project manager as seen by the HTTP layer.
type ProjectDirectory interface {
	ListProjects() ProjectListResponse
	OpenProject(path string) (ProjectInfo, error)
	RemoveProject(id string) error
}

type ProjectsAPI struct {
	Directory ProjectDirectory
}

func RegisterProjectRoutes(router chi.Router, api *ProjectsAPI) {
	router.Get("/api/projects", api.HandleListProjects)
	router.Post("/api/projects/open", api.HandleOpenProject)
	router.Get("/api/projects/browse", api.HandleBrowseDirs)
	router.Delete("/api/projects/{projectID}", api.HandleRemoveProject)
}

func (a *ProjectsAPI) HandleListProjects(w http.ResponseWriter, _ *http.Request) {
	webapi.WriteJSON(w, http.StatusOK, a.Directory.ListProjects())
}

func (a *ProjectsAPI) HandleOpenProject(w http.ResponseWriter, r *http.Request) {
	var req OpenProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		webapi.WriteBadRequest(w, "missing_path", "path is required")
		return
	}

	info, err := a.Directory.OpenProject(path)
	if err != nil {
		webapi.WriteBadRequest(w, "project_open_failed", err.Error())
		return
	}
	webapi.WriteJSON(w, http.StatusOK, OpenProjectResponse{Status: "ok", Project: info})
}

func (a *ProjectsAPI) HandleRemoveProject(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "projectID"))
	if id == "" {
		webapi.WriteBadRequest(w, "missing_project_id", "project id is required")
		return
	}
	if err := a.Directory.RemoveProject(id); err != nil {
		webapi.WriteBadRequest(w, "project_remove_failed", err.Error())
		return
	}
	webapi.WriteJSON(w, http.StatusOK, a.Directory.ListProjects())
}

// HandleBrowseDirs lists subdirectories for the "Open project" picker. It
// only reveals directory names the OS user can read anyway; the server is
// loopback-bound and origin-guarded like every other route.
func (a *ProjectsAPI) HandleBrowseDirs(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			webapi.WriteInternalError(w, "home_dir_unavailable", err.Error())
			return
		}
		path = home
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		webapi.WriteBadRequest(w, "invalid_path", err.Error())
		return
	}

	dirEntries, err := os.ReadDir(absPath)
	if err != nil {
		webapi.WriteBadRequest(w, "path_unreadable", err.Error())
		return
	}

	response := BrowseDirsResponse{Status: "ok", Path: absPath, Entries: []BrowseDirEntry{}}
	if parent := filepath.Dir(absPath); parent != absPath {
		response.Parent = parent
	}
	for _, entry := range dirEntries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		childPath := filepath.Join(absPath, entry.Name())
		response.Entries = append(response.Entries, BrowseDirEntry{
			Name:      entry.Name(),
			Path:      childPath,
			IsProject: looksLikeProject(childPath),
		})
	}
	sort.Slice(response.Entries, func(i, j int) bool {
		return response.Entries[i].Name < response.Entries[j].Name
	})

	webapi.WriteJSON(w, http.StatusOK, response)
}

func looksLikeProject(dir string) bool {
	for _, marker := range []string{".bruin.yml", filepath.Join(".renart", "project.yml")} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}
