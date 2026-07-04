// Package registry maintains the global (per-user, outside any workspace)
// list of known projects in projects.json. Every server appends the
// workspaces it opens, so the project switcher can offer them later. Entries
// key on the stable project UUID from .renart/project.yml, which survives
// directory moves and renames; the path is just the last known location.
package registry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry is one known project. Type distinguishes local directories from
// future cloud workspaces so both can share the file without a format
// change.
type Entry struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	Type         string    `json:"type"`
	LastOpenedAt time.Time `json:"lastOpenedAt"`
}

type File struct {
	Projects []Entry `json:"projects"`
}

// DefaultPath returns the platform config location
// (~/.config/renart/projects.json on Linux, %AppData% on Windows).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "renart", "projects.json"), nil
}

// Registry serializes access to one projects.json within this process.
// Cross-process writes race benignly: last writer wins on a whole-file
// rewrite of advisory data.
type Registry struct {
	mu   sync.Mutex
	path string
}

func New(path string) *Registry {
	return &Registry{path: path}
}

func (r *Registry) Path() string {
	return r.path
}

// Load reads the registry; a missing file is an empty registry.
func (r *Registry) Load() (File, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return load(r.path)
}

// Upsert records a project, keyed by ID, updating name/path/lastOpenedAt of
// an existing entry. Entries stay sorted by most recently opened.
func (r *Registry) Upsert(entry Entry) (File, error) {
	if strings.TrimSpace(entry.ID) == "" {
		return File{}, errors.New("registry: entry id is required")
	}
	if entry.Type == "" {
		entry.Type = "local"
	}
	if entry.LastOpenedAt.IsZero() {
		entry.LastOpenedAt = time.Now().UTC()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	file, err := load(r.path)
	if err != nil {
		return File{}, err
	}

	replaced := false
	for i := range file.Projects {
		if file.Projects[i].ID == entry.ID {
			file.Projects[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		file.Projects = append(file.Projects, entry)
	}
	sort.SliceStable(file.Projects, func(i, j int) bool {
		return file.Projects[i].LastOpenedAt.After(file.Projects[j].LastOpenedAt)
	})

	if err := save(r.path, file); err != nil {
		return File{}, err
	}
	return file, nil
}

// Remove drops a project from the registry. Removing an unknown ID is a
// no-op.
func (r *Registry) Remove(id string) (File, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	file, err := load(r.path)
	if err != nil {
		return File{}, err
	}

	kept := file.Projects[:0]
	for _, entry := range file.Projects {
		if entry.ID != id {
			kept = append(kept, entry)
		}
	}
	if len(kept) == len(file.Projects) {
		return file, nil
	}
	file.Projects = kept

	if err := save(r.path, file); err != nil {
		return File{}, err
	}
	return file, nil
}

func load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return File{}, nil
		}
		return File{}, err
	}
	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		// A corrupt registry should not brick the server; start over but
		// keep the broken file around for inspection.
		_ = os.Rename(path, path+".corrupt")
		return File{}, nil
	}
	return file, nil
}

func save(path string, file File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
