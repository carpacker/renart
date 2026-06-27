package service

import (
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
)

// inferSlingColumnsFromUpstream derives a Sling asset's columns from the declared
// columns of its source asset — the same "declared columns are the source of
// truth" model SQL assets use, but a Sling asset simply mirrors its upstream
// (there is no projection to type-annotate).
func (s *AssetService) inferSlingColumnsFromUpstream(parsedPipeline *pipeline.Pipeline, asset *pipeline.Asset) ([]WorkspaceColumn, *APIError) {
	source := resolveSlingSourceAsset(parsedPipeline, asset)
	if source == nil {
		return nil, badRequestError("sling_source_unknown", "could not resolve a source asset; declare the source as a dependency to infer columns from it")
	}

	columns := make([]WorkspaceColumn, 0, len(source.Columns))
	for _, column := range source.Columns {
		if strings.TrimSpace(column.Name) == "" {
			continue
		}
		columns = append(columns, WorkspaceColumn{Name: column.Name, Type: column.Type})
	}
	if len(columns) == 0 {
		return nil, badRequestError("sling_source_no_columns", "the source asset has no declared columns to infer from")
	}
	return columns, nil
}

// resolveSlingSourceAsset finds the asset a Sling asset reads from: first by
// matching the source_table parameter to an asset name, then by a single declared
// upstream asset.
func resolveSlingSourceAsset(parsedPipeline *pipeline.Pipeline, asset *pipeline.Asset) *pipeline.Asset {
	if parsedPipeline == nil || asset == nil {
		return nil
	}

	byName := make(map[string]*pipeline.Asset, len(parsedPipeline.Assets))
	for _, candidate := range parsedPipeline.Assets {
		if candidate != nil {
			byName[strings.TrimSpace(candidate.Name)] = candidate
		}
	}

	// 1. The source_table parameter names an existing asset.
	if sourceTable := strings.TrimSpace(slingParamsFromAsset(asset).SourceTable); sourceTable != "" {
		if match := byName[sourceTable]; match != nil {
			return match
		}
		for name, candidate := range byName {
			if strings.EqualFold(name, sourceTable) {
				return candidate
			}
		}
	}

	// 2. A single declared upstream asset.
	var upstreamAssets []*pipeline.Asset
	for _, upstream := range asset.Upstreams {
		if upstream.Type != "" && upstream.Type != "asset" {
			continue
		}
		if match := byName[strings.TrimSpace(upstream.Value)]; match != nil {
			upstreamAssets = append(upstreamAssets, match)
		}
	}
	if len(upstreamAssets) == 1 {
		return upstreamAssets[0]
	}
	return nil
}
