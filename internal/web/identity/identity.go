// Package identity provides stable, durable identifiers for pipelines and
// assets. Every durable record (schedules, run history, materialization
// facts, snapshots) hangs off these IDs rather than filesystem paths, so
// moving a pipeline directory does not orphan its history.
package identity

import (
	"fmt"
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
