package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
)

// inferSeedColumnsFromSource asks Sling for the schema of a local seed file.
// Unlike warehouse-backed non-SQL assets, a seed has a complete definition-time
// source, so users can refresh its columns before the first materialization.
func (s *AssetService) inferSeedColumnsFromSource(
	ctx context.Context,
	parsedPipeline *pipeline.Pipeline,
	asset *pipeline.Asset,
) ([]WorkspaceColumn, *APIError) {
	seedPath, ok := asset.Parameters.GetString("path")
	if !ok || strings.TrimSpace(seedPath) == "" {
		return nil, badRequestError("missing_seed_path", "seed path is required to infer columns")
	}

	pipelineName := ""
	if parsedPipeline != nil {
		pipelineName = parsedPipeline.Name
	}
	baseRenderer := jinja.NewRendererWithYesterday(pipelineName, "web-seed-column-infer")
	var renderer jinja.RendererInterface = baseRenderer
	if parsedPipeline != nil {
		assetRenderer, cloneErr := baseRenderer.CloneForAsset(ctx, parsedPipeline, asset)
		if cloneErr != nil {
			return nil, badRequestError("seed_path_render_failed", cloneErr.Error())
		}
		renderer = assetRenderer
	}
	renderedPath, renderErr := renderer.Render(seedPath)
	if renderErr != nil {
		return nil, badRequestError("seed_path_render_failed", renderErr.Error())
	}
	seedPath = renderedPath

	definitionPath := strings.TrimSpace(asset.DefinitionFile.Path)
	if definitionPath == "" {
		definitionPath = strings.TrimSpace(asset.ExecutableFile.Path)
	}
	fileType, _ := asset.Parameters.GetString("file_type")
	sourceStream, _, err := resolveSlingSeedSource(seedPath, fileType, filepath.Dir(definitionPath))
	if err != nil {
		return nil, badRequestError("seed_source_resolve_failed", err.Error())
	}
	if !strings.HasPrefix(strings.ToLower(sourceStream), "file://") {
		return nil, badRequestError(
			"remote_seed_column_inference_unsupported",
			"URL seed columns can be imported from the materialized output after the seed runs",
		)
	}

	pattern := filepath.FromSlash(strings.TrimPrefix(sourceStream, "file://"))
	output, runErr := runSlingSeedColumnDiscovery(ctx, s.deps.WorkspaceRoot, pattern)
	if runErr != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = runErr.Error()
		}
		return nil, badRequestError("seed_column_inference_failed", message)
	}
	columns := parseSlingSeedColumns(string(output))
	if len(columns) == 0 {
		return nil, badRequestError(
			"seed_column_inference_failed",
			fmt.Sprintf("Sling found no columns in %s", strings.TrimSpace(seedPath)),
		)
	}
	return columns, nil
}

func runSlingSeedColumnDiscovery(ctx context.Context, workspaceRoot, pattern string) ([]byte, error) {
	args := []string{
		"conns", "discover", "LOCAL",
		"--pattern", pattern,
		"--columns",
		"-o", "json",
	}
	cmdName, cmdArgs, err := loadCommand(ctx, args, nil)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	if strings.TrimSpace(workspaceRoot) != "" {
		cmd.Dir = workspaceRoot
	}
	cmd.Env = append(os.Environ(), loadBaseEnv()...)
	return cmd.CombinedOutput()
}

// parseSlingSeedColumns reads `sling conns discover LOCAL --columns -o json`.
// Sling may prefix the JSON with log lines, so it shares the tolerant decoder
// used by Load connection discovery.
func parseSlingSeedColumns(output string) []WorkspaceColumn {
	payload, ok := decodeLoadDiscoverPayload(output)
	if !ok {
		return []WorkspaceColumn{}
	}

	nameIndex, generalTypeIndex, nativeTypeIndex := -1, -1, -1
	for index, field := range payload.Fields {
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "column", "name":
			nameIndex = index
		case "general type", "general_type", "type":
			generalTypeIndex = index
		case "native type", "native_type":
			nativeTypeIndex = index
		}
	}
	if nameIndex < 0 {
		return []WorkspaceColumn{}
	}

	seen := make(map[string]struct{})
	columns := make([]WorkspaceColumn, 0, len(payload.Rows))
	for _, row := range payload.Rows {
		if nameIndex >= len(row) {
			continue
		}
		name := loadCellString(row[nameIndex])
		key := strings.ToLower(name)
		if name == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		columnType := ""
		if generalTypeIndex >= 0 && generalTypeIndex < len(row) {
			columnType = loadCellString(row[generalTypeIndex])
		}
		if (columnType == "" || columnType == "-") &&
			nativeTypeIndex >= 0 && nativeTypeIndex < len(row) {
			columnType = loadCellString(row[nativeTypeIndex])
		}
		if columnType == "-" {
			columnType = ""
		}
		columns = append(columns, WorkspaceColumn{Name: name, Type: columnType})
	}
	return columns
}
