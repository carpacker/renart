package service

import (
	"context"

	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"

	"renart/internal/web/sqlintelligence"
)

// InferAssetColumnsFromDefinition derives a SQL asset's output columns (name +
// type) from its rendered definition and the declared columns of the pipeline's
// assets — the assets are the source of truth, not the materialized warehouse
// tables. It renders the asset's SQL (Jinja + variables + dates + macros), then
// asks the polyglot engine to annotate the projection's types against the
// upstream asset schema.
func (s *AssetService) InferAssetColumnsFromDefinition(ctx context.Context, assetID string) ([]WorkspaceColumn, *APIError) {
	_, parsedPipeline, asset, err := s.deps.ResolveAssetByID(ctx, assetID)
	if err != nil {
		return nil, badRequestError("asset_resolve_failed", err.Error())
	}

	if isAPIAsset(asset) {
		columns := apiResponseFieldColumns(ctx, asset)
		if len(columns) == 0 {
			return nil, badRequestError("column_inference_failed", "API asset columns could not be inferred from response.fields or OpenAPI metadata")
		}
		result := make([]WorkspaceColumn, 0, len(columns))
		for _, column := range columns {
			result = append(result, WorkspaceColumn{Name: column.Name, Type: column.Type})
		}
		return result, nil
	}

	dialect, dialectErr := AssetTypeToDialect(asset.Type)
	if dialectErr != nil {
		return nil, badRequestError("unsupported_asset_type", "column inference from definition is supported for SQL assets only")
	}

	rendered, renderErr := s.renderAssetQuery(ctx, parsedPipeline, asset)
	if renderErr != nil {
		return nil, badRequestError("query_render_failed", renderErr.Error())
	}

	schema := buildDefinitionSchema(ctx, parsedPipeline)
	columns, inferErr := sqlintelligence.AnnotateOutputColumns(ctx, rendered, dialect, schema)
	if inferErr != nil {
		return nil, badRequestError("column_inference_failed", inferErr.Error())
	}

	result := make([]WorkspaceColumn, 0, len(columns))
	for _, column := range columns {
		result = append(result, WorkspaceColumn{Name: column.Name, Type: column.Type})
	}
	return result, nil
}

// RefreshAssetColumnsFromDefinition infers columns from the asset definition and
// reconciles them into the asset, preserving user-authored metadata. This is the
// definition-driven counterpart to the warehouse-driven fill paths.
func (s *AssetService) RefreshAssetColumnsFromDefinition(ctx context.Context, assetID string) (ColumnReconcileResult, *APIError) {
	_, parsedPipeline, asset, err := s.deps.ResolveAssetByID(ctx, assetID)
	if err != nil {
		return ColumnReconcileResult{}, badRequestError("asset_resolve_failed", err.Error())
	}

	var inferred []WorkspaceColumn
	var apiErr *APIError
	if isLoadAsset(asset) {
		// Load assets mirror their upstream's declared columns rather than a SQL
		// projection.
		inferred, apiErr = s.inferLoadColumnsFromUpstream(parsedPipeline, asset)
	} else {
		inferred, apiErr = s.InferAssetColumnsFromDefinition(ctx, assetID)
	}
	if apiErr != nil {
		return ColumnReconcileResult{}, apiErr
	}
	return s.ReconcileAssetColumns(ctx, assetID, inferred)
}

// renderAssetQuery renders an asset's SQL with the same Jinja context the
// dependency reconcile uses (pipeline variables, run dates, macros), so column
// inference sees the real query.
func (s *AssetService) renderAssetQuery(ctx context.Context, parsedPipeline *pipeline.Pipeline, asset *pipeline.Asset) (string, error) {
	renderer := jinja.NewRendererWithYesterday(parsedPipeline.Name, "web-column-infer")
	assetRenderer, err := renderer.CloneForAsset(ctx, parsedPipeline, asset)
	if err != nil {
		return "", err
	}
	return assetRenderer.Render(mergeAssetMacrosWithQuery(asset.ExecutableFile.Content, parsedPipeline.Macros))
}

// buildDefinitionSchema builds a polyglot schema from the declared columns of
// every asset in the pipeline (keyed by asset name). Upstream assets that carry
// no declared columns contribute nothing — infer their columns first to resolve
// types through multiple hops.
func buildDefinitionSchema(ctx context.Context, parsedPipeline *pipeline.Pipeline) sqlintelligence.Schema {
	schema := sqlintelligence.Schema{}
	if parsedPipeline == nil {
		return schema
	}
	for _, asset := range parsedPipeline.Assets {
		if asset == nil {
			continue
		}
		assetColumns := asset.Columns
		if len(assetColumns) == 0 && isAPIAsset(asset) {
			assetColumns = apiResponseFieldColumns(ctx, asset)
		}
		if len(assetColumns) == 0 {
			continue
		}
		columns := make(map[string]string, len(assetColumns))
		for _, column := range assetColumns {
			if column.Name == "" {
				continue
			}
			columns[column.Name] = column.Type
		}
		if len(columns) > 0 {
			schema[asset.Name] = columns
		}
	}
	return schema
}
