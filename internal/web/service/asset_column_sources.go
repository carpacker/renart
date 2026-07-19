package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"

	webmodel "renart/internal/web/model"
)

const (
	columnSourceDefinition   = "definition"
	columnSourceLiveResponse = "live_response"
	columnSourceMaterialized = "materialized"
)

func columnInferenceSourcesForAsset(asset *pipeline.Asset, connectionName string) []webmodel.ColumnInferenceSource {
	if asset == nil || isSensorAssetType(asset.Type) {
		return []webmodel.ColumnInferenceSource{}
	}

	sources := make([]webmodel.ColumnInferenceSource, 0, 3)
	typeName := strings.ToLower(strings.TrimSpace(string(asset.Type)))
	switch {
	case isAPIAsset(asset):
		sources = append(sources,
			webmodel.ColumnInferenceSource{
				ID: columnSourceDefinition, Label: "Asset definition", Category: "definition",
				Description: "Declared response fields or the selected OpenAPI response schema.",
			},
			webmodel.ColumnInferenceSource{
				ID: columnSourceLiveResponse, Label: "Live request", Category: "observed",
				Description:    "A sampled API response using the asset's current request settings.",
				MayOmitColumns: true,
			},
		)
	case isLoadAsset(asset):
		sources = append(sources, webmodel.ColumnInferenceSource{
			ID: columnSourceDefinition, Label: "Source asset", Category: "definition",
			Description: "The declared schema of the load asset's upstream source.",
		})
	case strings.HasSuffix(typeName, ".seed"):
		seedPath, _ := asset.Parameters.GetString("path")
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(seedPath)), "http://") &&
			!strings.HasPrefix(strings.ToLower(strings.TrimSpace(seedPath)), "https://") {
			sources = append(sources, webmodel.ColumnInferenceSource{
				ID: columnSourceDefinition, Label: "Seed file", Category: "definition",
				Description: "The schema Sling detects in the local seed file.",
			})
		}
	case strings.HasSuffix(typeName, ".sql"):
		sources = append(sources, webmodel.ColumnInferenceSource{
			ID: columnSourceDefinition, Label: "SQL query", Category: "definition",
			Description: "The rendered query's output schema using declared upstream columns.",
		})
	}

	if strings.TrimSpace(connectionName) != "" {
		sources = append(sources, webmodel.ColumnInferenceSource{
			ID: columnSourceMaterialized, Label: "Current table", Category: "observed",
			Description: "The schema currently reported by the asset's warehouse relation.",
		})
	}
	return sources
}

func columnInferenceSourcesForPipelineAsset(asset *pipeline.Asset, parsedPipeline *pipeline.Pipeline) []webmodel.ColumnInferenceSource {
	connectionName := ""
	if parsedPipeline != nil {
		connectionName, _ = targetConnectionNameForAsset(asset, parsedPipeline)
	}
	return columnInferenceSourcesForAsset(asset, connectionName)
}

// PreviewAssetColumns observes one advertised schema source without mutating
// the asset, then compares it with the saved metadata.
func (s *AssetService) PreviewAssetColumns(
	ctx context.Context,
	assetID string,
	sourceID string,
	environment string,
) (webmodel.ColumnInferencePreview, *APIError) {
	_, parsedPipeline, asset, err := s.deps.ResolveAssetByID(ctx, assetID)
	if err != nil {
		return webmodel.ColumnInferencePreview{}, badRequestError("asset_resolve_failed", err.Error())
	}

	sources := columnInferenceSourcesForPipelineAsset(asset, parsedPipeline)
	var source *webmodel.ColumnInferenceSource
	for index := range sources {
		if sources[index].ID == strings.TrimSpace(sourceID) {
			source = &sources[index]
			break
		}
	}
	if source == nil {
		return webmodel.ColumnInferencePreview{}, badRequestError("unsupported_column_source", "this schema source is not available for the asset")
	}

	columns, notes, sampleRecords, apiErr := s.observeAssetColumnSource(
		ctx,
		assetID,
		parsedPipeline,
		asset,
		*source,
		environment,
	)
	if apiErr != nil {
		return webmodel.ColumnInferencePreview{}, apiErr
	}

	return webmodel.ColumnInferencePreview{
		Status:        "ok",
		Source:        *source,
		Columns:       columns,
		Drift:         compareColumnSchemas(asset.Columns, columns),
		Notes:         notes,
		SampleRecords: sampleRecords,
	}, nil
}

func (s *AssetService) observeAssetColumnSource(
	ctx context.Context,
	assetID string,
	parsedPipeline *pipeline.Pipeline,
	asset *pipeline.Asset,
	source webmodel.ColumnInferenceSource,
	environment string,
) ([]WorkspaceColumn, []string, *int, *APIError) {
	var (
		columns       []WorkspaceColumn
		notes         []string
		sampleRecords *int
		apiErr        *APIError
	)
	switch source.ID {
	case columnSourceDefinition:
		if isLoadAsset(asset) {
			columns, apiErr = s.inferLoadColumnsFromUpstream(parsedPipeline, asset)
		} else {
			columns, apiErr = s.InferAssetColumnsFromDefinition(ctx, assetID)
		}
	case columnSourceLiveResponse:
		_, sample, inferErr := s.InferAPIAsset(ctx, assetID)
		apiErr = inferErr
		if apiErr == nil {
			columns = sample.Columns
			notes = append(notes, sample.Warnings...)
			count := sample.RecordsCount
			sampleRecords = &count
		}
	case columnSourceMaterialized:
		columns, _, apiErr = s.inferMaterializedAssetColumns(ctx, parsedPipeline, asset, environment)
		if apiErr == nil && (isAPIAsset(asset) || isLoadAsset(asset)) {
			var removed bool
			columns, removed = withoutSlingLoadedAtColumn(columns)
			if removed {
				notes = append(notes, "Ignoring legacy Sling metadata column _sling_loaded_at; Renart materializations no longer include it.")
			}
		}
	default:
		apiErr = badRequestError("unsupported_column_source", fmt.Sprintf("unknown schema source %q", source.ID))
	}
	return columns, notes, sampleRecords, apiErr
}

func withoutSlingLoadedAtColumn(columns []WorkspaceColumn) ([]WorkspaceColumn, bool) {
	filtered := make([]WorkspaceColumn, 0, len(columns))
	removed := false
	for _, column := range columns {
		if strings.EqualFold(strings.TrimSpace(column.Name), slingLoadedAtColumn) {
			removed = true
			continue
		}
		filtered = append(filtered, column)
	}
	if !removed {
		return columns, false
	}
	return filtered, true
}

func compareColumnSchemas(current []pipeline.Column, inferred []WorkspaceColumn) webmodel.ColumnSchemaDrift {
	result := webmodel.ColumnSchemaDrift{Items: []webmodel.ColumnSchemaDriftItem{}}
	currentByName := make(map[string]pipeline.Column, len(current))
	seen := make(map[string]struct{}, len(inferred))
	for _, column := range current {
		currentByName[strings.ToLower(strings.TrimSpace(column.Name))] = column
	}

	for _, column := range inferred {
		key := strings.ToLower(strings.TrimSpace(column.Name))
		if key == "" {
			continue
		}
		seen[key] = struct{}{}
		existing, ok := currentByName[key]
		if !ok {
			result.Added++
			result.Items = append(result.Items, webmodel.ColumnSchemaDriftItem{
				Column: column.Name, Kind: "added", InferredType: column.Type,
			})
			continue
		}
		if equivalentColumnType(existing.Type, column.Type) {
			result.Unchanged++
			continue
		}
		result.TypeChanged++
		result.Items = append(result.Items, webmodel.ColumnSchemaDriftItem{
			Column: column.Name, Kind: "type_changed", CurrentType: existing.Type, InferredType: column.Type,
		})
	}

	for _, column := range current {
		key := strings.ToLower(strings.TrimSpace(column.Name))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		result.Removed++
		result.Items = append(result.Items, webmodel.ColumnSchemaDriftItem{
			Column: column.Name, Kind: "removed", CurrentType: column.Type,
		})
	}
	return result
}

func equivalentColumnType(left, right string) bool {
	return canonicalColumnType(left) == canonicalColumnType(right)
}

func canonicalColumnType(value string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
	switch normalized {
	case "char", "character", "character varying", "nvarchar", "string", "text", "varchar":
		return "string"
	case "bool", "boolean":
		return "boolean"
	case "int", "int4", "int32", "integer":
		return "integer"
	case "bigint", "int8", "int64":
		return "bigint"
	case "decimal", "number", "numeric":
		return "decimal"
	case "datetime", "timestamp", "timestamp without time zone":
		return "datetime"
	case "json", "jsonb", "object", "variant":
		return "json"
	default:
		return normalized
	}
}
