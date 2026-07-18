package service

import (
	"context"
	"fmt"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
)

// renderAthenaExecution renders the final ordered statement list submitted by
// Bruin's direct Athena operator. The query-results location is read from the
// already selected, typed configuration; rendering must not construct a live
// Athena client merely to recover that public materializer input. The
// operatorFullRefresh flag is run-scoped after environment policy; the shared
// materializer still applies the asset-level refresh restriction at Render.
func renderAthenaExecution(
	renderCtx context.Context,
	info *directPipelineInfo,
	renderer jinja.RendererInterface,
	extractor query.QueryExtractor,
	compiledQuery string,
	operatorFullRefresh bool,
	effectiveFullRefresh bool,
) assetRenderSemanticOutcome {
	if info == nil || info.Asset == nil || info.Pipeline == nil || info.Asset.Type != pipeline.AssetTypeAthenaQuery {
		return assetRenderSemanticOutcome{}
	}

	resolvedHooks, err := resolveAssetHookTemplates(renderCtx, info.Pipeline, info.Asset, renderer)
	if err != nil {
		return athenaExecutionRenderError(err)
	}
	executionAsset := *info.Asset
	executionAsset.Hooks = resolvedHooks

	resultsLocation, err := selectedAthenaQueryResultsPath(info)
	if err != nil {
		return athenaExecutionRenderError(err)
	}
	stages, err := renderExactAthenaExecutionStages(
		&executionAsset,
		extractor,
		compiledQuery,
		resultsLocation,
		operatorFullRefresh,
	)
	if err != nil {
		return athenaExecutionRenderError(err)
	}

	fidelity := AssetRenderFidelityExact
	message := ""
	if info.Config != nil && info.Config.SelectedEnvironment != nil && info.Config.SelectedEnvironment.SchemaPrefix != "" {
		fidelity = AssetRenderFidelityRuntimeOnly
		message = "pre-rewrite materializer SQL; the developer-environment table rewrite depends on live warehouse state"
	}
	if athenaExecutionUsesEphemeralIdentifiers(info.Asset, effectiveFullRefresh) {
		fidelity = AssetRenderFidelityRuntimeOnly
		if message != "" {
			message += "; "
		}
		message += "temporary table identifiers are generated independently when execution starts"
	}

	status := AssetRenderStatusOK
	if fidelity == AssetRenderFidelityRuntimeOnly {
		status = AssetRenderStatusPartial
	}
	for index := range stages {
		stages[index].Fidelity = fidelity
		stages[index].Message = message
	}
	return assetRenderSemanticOutcome{
		handled: true,
		status:  status,
		stages:  stages,
	}
}

func renderExactAthenaExecutionStages(
	asset *pipeline.Asset,
	extractor query.QueryExtractor,
	compiledQuery string,
	resultsLocation string,
	operatorFullRefresh bool,
) ([]AssetRenderStage, error) {
	if asset == nil || asset.Type != pipeline.AssetTypeAthenaQuery {
		return nil, fmt.Errorf("Athena execution rendering requires an Athena asset")
	}

	materializer, err := newDirectAthenaExecutionMaterializer(operatorFullRefresh)
	if err != nil {
		return nil, err
	}
	statements, err := materializer.Render(asset, compiledQuery, resultsLocation)
	if err != nil {
		return nil, err
	}

	// Bruin re-renders every final list element for a declared time_interval
	// strategy. This happens after hook wrapping and DECLARE hoisting, even when
	// full refresh selected a different materialization strategy.
	if asset.Materialization.Strategy == pipeline.MaterializationStrategyTimeInterval {
		if extractor == nil {
			return nil, fmt.Errorf("time_interval Athena rendering requires a query extractor")
		}
		statements, err = extractor.ReextractQueriesFromSlice(statements)
		if err != nil {
			return nil, fmt.Errorf("re-render time_interval Athena execution SQL: %w", err)
		}
	}
	if len(statements) == 0 {
		return nil, fmt.Errorf("Athena execution SQL rendered empty")
	}

	return athenaExecutionStages(statements), nil
}

func selectedAthenaQueryResultsPath(info *directPipelineInfo) (string, error) {
	if info == nil || info.Config == nil || info.Config.SelectedEnvironment == nil || info.Config.SelectedEnvironment.Connections == nil {
		return "", fmt.Errorf("selected environment has no Athena connection configuration")
	}
	connectionName, err := assetRenderConnectionName(info)
	if err != nil {
		return "", err
	}
	connection, ok := info.Config.SelectedEnvironment.Connections.GetConnection(connectionName).(*config.AthenaConnection)
	if !ok || connection == nil {
		return "", fmt.Errorf("selected connection %q is not an Athena connection", connectionName)
	}
	if connection.QueryResultsPath == "" {
		return "", fmt.Errorf("selected Athena connection %q has no query results path", connectionName)
	}
	return connection.QueryResultsPath, nil
}

func athenaExecutionStages(statements []string) []AssetRenderStage {
	stages := make([]AssetRenderStage, 0, len(statements))
	for index, statement := range statements {
		stages = append(stages, AssetRenderStage{
			Kind:     "execution_sql",
			Label:    fmt.Sprintf("Execution SQL %d", index+1),
			Language: "sql",
			Content:  statement,
			Status:   AssetRenderStageStatusOK,
			Fidelity: AssetRenderFidelityExact,
		})
	}
	return stages
}

func athenaExecutionUsesEphemeralIdentifiers(asset *pipeline.Asset, effectiveFullRefresh bool) bool {
	if asset == nil || asset.Materialization.Type != pipeline.MaterializationTypeTable {
		return false
	}
	if effectiveFullRefresh && asset.Materialization.Strategy != pipeline.MaterializationStrategyDDL {
		return true
	}
	switch asset.Materialization.Strategy {
	case pipeline.MaterializationStrategyNone,
		pipeline.MaterializationStrategyCreateReplace,
		pipeline.MaterializationStrategyDeleteInsert,
		pipeline.MaterializationStrategySCD2ByColumn,
		pipeline.MaterializationStrategySCD2ByTime:
		return true
	default:
		return false
	}
}

// Athena's WholeFileExtractor returns one query even when the executable body
// is empty, and Bruin's DDL materializer derives its SQL entirely from columns.
// The main render path can use this predicate to preserve that runtime path.
func athenaExecutionAllowsEmptyCompiledQuery(asset *pipeline.Asset) bool {
	return asset != nil &&
		asset.Type == pipeline.AssetTypeAthenaQuery &&
		asset.Materialization.Type == pipeline.MaterializationTypeTable &&
		asset.Materialization.Strategy == pipeline.MaterializationStrategyDDL
}

func athenaExecutionRenderError(err error) assetRenderSemanticOutcome {
	return assetRenderSemanticOutcome{
		handled: true,
		status:  AssetRenderStatusPartial,
		stages:  []AssetRenderStage{failedExactRenderStage("execution_sql", err)},
	}
}
