// Package clientapi is the renart CLI's client side: discovery of a running
// server for a workspace (via .renart/server.json + a health check) and a
// thin HTTP client over the server's API. See plans/cli-v1.md §2.
package clientapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ServerFile is the discovery record a server writes into each open
// project's .renart/server.json and removes on shutdown. A stale file (dead
// server) is ignored by readers and overwritten by the next server.
type ServerFile struct {
	PID int `json:"pid"`
	// BaseURL is the server's root URL (http://127.0.0.1:8080).
	BaseURL string `json:"base_url"`
	// APIBaseURL is the API prefix serving THIS workspace: the project mount
	// (…/api/projects/{id}) on a multi-project server, or …/api when the
	// workspace is served unprefixed (standalone).
	APIBaseURL    string    `json:"api_base_url"`
	ProjectID     string    `json:"project_id,omitempty"`
	WorkspaceRoot string    `json:"workspace_root"`
	Version       string    `json:"version"`
	Token         string    `json:"token,omitempty"`
	StartedAt     time.Time `json:"started_at"`
}

func serverFilePath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".renart", "server.json")
}

// WriteServerFile records the discovery file for a workspace the server has
// open. Best-effort atomic: written to a temp file, then renamed.
func WriteServerFile(workspaceRoot string, file ServerFile) error {
	dir := filepath.Join(workspaceRoot, ".renart")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "server.json.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(payload, '\n')); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, serverFilePath(workspaceRoot))
}

// RemoveServerFile deletes the discovery file; only the writing server
// should call this (on shutdown or when it closes the project).
func RemoveServerFile(workspaceRoot string) {
	_ = os.Remove(serverFilePath(workspaceRoot))
}

// ReadServerFile loads the discovery file for a workspace. A missing file is
// (nil, nil): no server has the workspace open.
func ReadServerFile(workspaceRoot string) (*ServerFile, error) {
	raw, err := os.ReadFile(serverFilePath(workspaceRoot))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file ServerFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("malformed %s: %w", serverFilePath(workspaceRoot), err)
	}
	return &file, nil
}
