package service

import (
	"path/filepath"
	"strings"

	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
)

// parseErrorMetaKey carries an asset's parse error on the placeholder asset the
// tolerant builder emits, so the workspace DTO can surface it without inventing a
// new field on bruin's Asset.
const parseErrorMetaKey = "renart_parse_error"

// errorTolerantTaskCreator wraps a task creator so a single unparseable asset
// file yields a placeholder asset (carrying the error) instead of failing the
// whole pipeline build. bruin's CreatePipelineFromPath returns on the first
// CreateAssetFromFile error, which would otherwise hide the entire pipeline — and
// every sibling asset — from the UI.
func errorTolerantTaskCreator(fs afero.Fs, inner pipeline.TaskCreator) pipeline.TaskCreator {
	return func(filePath string) (*pipeline.Asset, error) {
		asset, err := inner(filePath)
		if err == nil {
			return asset, nil
		}

		content := ""
		if raw, readErr := afero.ReadFile(fs, filePath); readErr == nil {
			content = string(raw)
		}
		absPath, absErr := filepath.Abs(filePath)
		if absErr != nil {
			absPath = filePath
		}
		name := rawTopLevelYAMLScalar(content, "name")
		assetType := rawTopLevelYAMLScalar(content, "type")

		// When the broken file still has simple top-level name/type scalars, keep
		// them on the placeholder so the UI can preserve the right canvas/editor
		// affordances while the user fixes an unrelated YAML syntax error. If name
		// is absent, SetNameFromPath still derives a stable fallback from the path.
		return &pipeline.Asset{
			Name: name,
			Type: pipeline.AssetType(assetType),
			Meta: pipeline.EmptyStringMap{parseErrorMetaKey: err.Error()},
			ExecutableFile: pipeline.ExecutableFile{
				Name:    filepath.Base(absPath),
				Path:    absPath,
				Content: content,
			},
		}, nil
	}
}

func rawTopLevelYAMLScalar(content, key string) string {
	prefix := key + ":"
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.TrimLeft(line, " \t") != line {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		if value == "" || strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
			return ""
		}
		if comment := strings.Index(value, " #"); comment >= 0 {
			value = strings.TrimSpace(value[:comment])
		}
		return strings.Trim(value, `"'`)
	}
	return ""
}

// NewRenartTolerantPipelineBuilder builds pipelines that survive individual asset
// parse errors. It is used for read-only workspace and planning views so one
// broken asset does not hide its valid siblings. Execution and asset resolution
// keep the strict builder so a broken asset still fails loudly where correctness
// matters.
func NewRenartTolerantPipelineBuilder(fs afero.Fs) *pipeline.Builder {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	return pipeline.NewBuilder(
		BuilderConfig,
		errorTolerantTaskCreator(fs, apiAwareYamlTaskCreator(fs)),
		errorTolerantTaskCreator(fs, pipeline.CreateTaskFromFileComments(fs)),
		fs,
		DefaultGlossaryReader,
		jinja.VariantRendererFactory,
	)
}
