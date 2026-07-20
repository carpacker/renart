// Package identity provides stable, durable identifiers for pipelines and
// assets. Every durable record (schedules, run history, materialization
// facts, snapshots) hangs off these IDs rather than filesystem paths, so
// moving a pipeline directory does not orphan its history.
package identity

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

// EnsurePipelineID returns the stable UUID stored under the top-level `id:`
// key of the given pipeline.yml. When the key is missing it generates a new
// UUID and writes it back by prepending a single line, leaving the rest of
// the file byte-for-byte untouched.
func EnsurePipelineID(fs afero.Fs, pipelineYmlPath string) (id string, generated bool, err error) {
	content, err := afero.ReadFile(fs, pipelineYmlPath)
	if err != nil {
		return "", false, err
	}

	if existing := pipelineIDFromYAML(content); existing != "" {
		return existing, false, nil
	}

	id = uuid.NewString()
	updated := append([]byte(fmt.Sprintf("id: %s\n", id)), content...)
	if err := afero.WriteFile(fs, pipelineYmlPath, updated, 0o644); err != nil {
		return "", false, err
	}
	return id, true, nil
}

// pipelineIDFromYAML extracts the top-level `id:` scalar from pipeline.yml
// content, or "" when absent.
func pipelineIDFromYAML(content []byte) string {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return ""
	}
	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "id" {
			return strings.TrimSpace(root.Content[i+1].Value)
		}
	}
	return ""
}

// Project is the identity stored in .renart/project.yml. The UUID is what
// the project registry, per-project UI state, and later cloud workspace
// mapping key on; it survives directory moves and renames.
type Project struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name,omitempty"`
	// Features are project-scoped feature flags (e.g. "ingestr" re-enables
	// the ingestr connection types and asset options for bruin users).
	Features map[string]bool `yaml:"features,omitempty"`
	// Retention is optional on disk; callers normalize an absent or partial
	// policy through NormalizeRetentionSettings before using it.
	Retention *RetentionSettings `yaml:"retention,omitempty"`
}

// LoadProject reads .renart/project.yml without side effects (no UUID
// self-assignment, no writes). Missing or unparsable files return an error.
func LoadProject(fs afero.Fs, projectYmlPath string) (Project, error) {
	content, err := afero.ReadFile(fs, projectYmlPath)
	if err != nil {
		return Project{}, err
	}
	var project Project
	if err := yaml.Unmarshal(content, &project); err != nil {
		return Project{}, fmt.Errorf("parse %s: %w", projectYmlPath, err)
	}
	return project, nil
}

// EnsureProject returns the project identity from projectYmlPath,
// self-assigning a UUID (and defaultName) on first open. An existing file
// that parses but lacks an id gets one assigned while keeping its name; a
// file that fails to parse is left untouched and reported as an error.
func EnsureProject(fs afero.Fs, projectYmlPath, defaultName string) (Project, error) {
	content, err := afero.ReadFile(fs, projectYmlPath)
	if err == nil {
		var project Project
		if err := yaml.Unmarshal(content, &project); err != nil {
			return Project{}, fmt.Errorf("parse %s: %w", projectYmlPath, err)
		}
		project.ID = strings.TrimSpace(project.ID)
		project.Name = strings.TrimSpace(project.Name)
		if project.Name == "" {
			project.Name = defaultName
		}
		if project.ID != "" {
			return project, nil
		}
		project.ID = uuid.NewString()
		if err := SaveProject(fs, projectYmlPath, project); err != nil {
			return Project{}, err
		}
		return project, nil
	}

	project := Project{ID: uuid.NewString(), Name: defaultName}
	if err := SaveProject(fs, projectYmlPath, project); err != nil {
		return Project{}, err
	}
	return project, nil
}

// SaveProject writes .renart/project.yml, creating the parent directory when
// needed.
func SaveProject(fs afero.Fs, projectYmlPath string, project Project) error {
	data, err := yaml.Marshal(project)
	if err != nil {
		return err
	}
	if err := fs.MkdirAll(filepath.Dir(projectYmlPath), 0o755); err != nil {
		return err
	}
	return afero.WriteFile(fs, projectYmlPath, data, 0o644)
}

// AssetID derives the durable asset identifier from the owning pipeline's
// UUID and the asset name. Accepted v1 limitation: renaming an asset orphans
// its history (see architecture/staleness.md).
func AssetID(pipelineUUID, assetName string) string {
	return pipelineUUID + ":" + assetName
}

// SplitAssetID is the inverse of AssetID. ok is false when the value does
// not look like a durable asset ID.
func SplitAssetID(assetID string) (pipelineUUID, assetName string, ok bool) {
	idx := strings.IndexByte(assetID, ':')
	if idx <= 0 || idx == len(assetID)-1 {
		return "", "", false
	}
	return assetID[:idx], assetID[idx+1:], true
}
