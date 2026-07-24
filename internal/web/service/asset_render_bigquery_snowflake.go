package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/bruin-data/bruin/pkg/ansisql"
	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/bruin-data/bruin/pkg/tablename"
)

// renderBigQuerySnowflakeExecution renders the connection-free portion of the
// two direct warehouse operators whose runtime also performs destination-aware
// preparation. The caller has already appended the compiled query stage. This
// helper deliberately keeps the post-environment operator mode and the
// per-asset effective mode separate: Bruin's materializer honors the latter,
// while live preflight gates use the former. The original user request remains
// in render provenance and is not reused after an environment-level restriction.
func renderBigQuerySnowflakeExecution(
	renderCtx context.Context,
	info *directPipelineInfo,
	renderer jinja.RendererInterface,
	extractor query.QueryExtractor,
	compiledQuery string,
	operatorFullRefresh bool,
	effectiveFullRefresh bool,
) assetRenderSemanticOutcome {
	if info == nil || info.Asset == nil || info.Pipeline == nil {
		return assetRenderSemanticOutcome{}
	}
	asset := info.Asset
	if asset.Type != pipeline.AssetTypeBigqueryQuery && asset.Type != pipeline.AssetTypeSnowflakeQuery {
		return assetRenderSemanticOutcome{}
	}

	resolvedHooks, err := resolveAssetHookTemplates(renderCtx, info.Pipeline, asset, renderer)
	if err != nil {
		return warehouseExecutionRenderError(err)
	}
	executionAsset := *asset
	executionAsset.Hooks = resolvedHooks

	executionSQL, err := renderBigQuerySnowflakeMaterializerSQL(&executionAsset, extractor, compiledQuery, effectiveFullRefresh)
	if err != nil {
		return warehouseExecutionRenderError(err)
	}

	// BigQuery adds query annotations as an in-band SQL comment immediately
	// before submission. Apply that same pure helper here so an "exact" stage is
	// byte-for-byte final SQL even when annotations are enabled. Snowflake query
	// tags are context metadata and do not mutate the submitted SQL string.
	if asset.Type == pipeline.AssetTypeBigqueryQuery {
		annotated, annotationErr := ansisql.AddAnnotationComment(
			renderCtx,
			&query.Query{Query: executionSQL},
			asset.Name,
			"main",
			info.Pipeline.Name,
		)
		if annotationErr != nil {
			return warehouseExecutionRenderError(annotationErr)
		}
		executionSQL = annotated.Query
	}

	outcome := assetRenderSemanticOutcome{
		handled: true,
		status:  AssetRenderStatusOK,
		stages:  make([]AssetRenderStage, 0, 6),
		issues:  make([]AssetRenderIssue, 0, 1),
	}
	if asset.Type == pipeline.AssetTypeBigqueryQuery {
		appendBigQueryOperatorStages(&outcome, info, operatorFullRefresh)
	} else {
		appendSnowflakeOperatorStages(&outcome, info, operatorFullRefresh)
	}

	if operatorFullRefresh != effectiveFullRefresh {
		outcome.status = mergeAssetRenderStatus(outcome.status, AssetRenderStatusPartial)
		message := "the materializer SQL uses the asset-restricted refresh mode, while live destination preflights still use the run-scoped full-refresh mode"
		if asset.Type == pipeline.AssetTypeSnowflakeQuery && asset.Materialization.IsSCD2() {
			message += "; Snowflake therefore skips its incremental SCD2 migration preflight"
		}
		outcome.issues = append(outcome.issues, AssetRenderIssue{
			Code:     "full_refresh_preflight_differs_from_materializer",
			Severity: "warning",
			Message:  message,
		})
	}

	executionFidelity := AssetRenderFidelityExact
	executionMessage := ""
	if info.Config != nil && info.Config.SelectedEnvironment != nil && info.Config.SelectedEnvironment.SchemaPrefix != "" {
		executionFidelity = AssetRenderFidelityRuntimeOnly
		executionMessage = "pre-rewrite materializer SQL; the developer-environment table rewrite depends on live warehouse state"
	}
	if executionMaterializationUsesEphemeralIdentifiers(asset, effectiveFullRefresh) {
		if executionMessage != "" {
			executionMessage += "; "
		}
		executionFidelity = AssetRenderFidelityRuntimeOnly
		executionMessage += "temporary table identifiers are generated independently when execution starts"
	}
	if executionFidelity == AssetRenderFidelityRuntimeOnly {
		outcome.status = mergeAssetRenderStatus(outcome.status, AssetRenderStatusPartial)
	}
	outcome.stages = append(outcome.stages, AssetRenderStage{
		Kind:     "execution_sql",
		Language: "sql",
		Content:  executionSQL,
		Status:   AssetRenderStageStatusOK,
		Fidelity: executionFidelity,
		Message:  executionMessage,
	})
	return outcome
}

func renderBigQuerySnowflakeMaterializerSQL(asset *pipeline.Asset, extractor query.QueryExtractor, compiledQuery string, effectiveFullRefresh bool) (string, error) {
	materializer, supported, err := newDirectStringExecutionMaterializer(asset.Type, effectiveFullRefresh)
	if err != nil {
		return "", err
	}
	if !supported {
		return "", fmt.Errorf("execution materializer is not available for %s", asset.Type)
	}
	executionSQL, err := materializer.Render(asset, compiledQuery)
	if err != nil {
		return "", err
	}

	// Both direct operators submit the first re-rendered statement for
	// time_interval materializations. Keep that exact behavior here rather than
	// splitting the materialized script ourselves.
	if asset.Materialization.Strategy == pipeline.MaterializationStrategyTimeInterval {
		if extractor == nil {
			return "", fmt.Errorf("time_interval execution rendering requires a query extractor")
		}
		rendered, renderErr := extractor.ExtractQueriesFromString(executionSQL)
		if renderErr != nil {
			return "", fmt.Errorf("re-render time_interval execution SQL: %w", renderErr)
		}
		if len(rendered) == 0 || rendered[0] == nil {
			return "", fmt.Errorf("time_interval execution SQL rendered empty")
		}
		executionSQL = rendered[0].Query
	}
	return executionSQL, nil
}

func appendBigQueryOperatorStages(outcome *assetRenderSemanticOutcome, info *directPipelineInfo, requestedFullRefresh bool) {
	asset := info.Asset
	appendBigQueryQueryCostGuard(outcome, info)

	connection, _ := selectedBigQueryRenderConnection(info)
	if asset.Materialization.Type != pipeline.MaterializationTypeNone {
		if operation, ok := bigQueryDatasetPreparationOperation(asset.Name, connection); ok {
			outcome.status = mergeAssetRenderStatus(outcome.status, AssetRenderStatusPartial)
			outcome.stages = append(outcome.stages, AssetRenderStage{
				Kind:        "dataset_preparation",
				Language:    "json",
				Content:     mustRenderOperationJSON(operation),
				Status:      AssetRenderStageStatusOK,
				Fidelity:    AssetRenderFidelitySemantic,
				Conditional: true,
				Message:     "checks live BigQuery dataset metadata and creates the dataset only when missing; Bruin also keeps a process-local dataset cache",
			})
		}
	}

	// Bruin gates this live metadata compatibility check with the global
	// materializer flag, even when a specific asset is refresh-restricted or has
	// no materialization declaration.
	if requestedFullRefresh && asset.Materialization.Strategy != pipeline.MaterializationStrategyDDL {
		appendRuntimeOnlyOperatorStage(outcome, AssetRenderStage{
			Kind:     "target_compatibility",
			Language: "json",
			Content: mustRenderOperationJSON(map[string]any{
				"checks":                 []string{"materialization_type", "partitioning", "clustering"},
				"operation":              "drop_bigquery_target_on_mismatch",
				"requested_full_refresh": true,
			}),
			Status:      AssetRenderStageStatusOK,
			Fidelity:    AssetRenderFidelityRuntimeOnly,
			Conditional: true,
			Message:     "reads live target metadata and deletes the target only when its type, partitioning, or clustering is incompatible",
		})
	}
}

// appendBigQueryQueryCostGuard mirrors the query-limit check shared by
// BigQuery SQL assets and both BigQuery sensor operators. The generated query
// for a table sensor depends on the selected live connection, so this stage is
// intentionally semantic/runtime-only and contains only configured hard
// limits, never credentials or endpoint details.
func appendBigQueryQueryCostGuard(outcome *assetRenderSemanticOutcome, info *directPipelineInfo) {
	connection, _ := selectedBigQueryRenderConnection(info)
	if connection != nil && (connection.MaxBillableBytes != nil || connection.MaxQueryCost != nil) {
		limits := make(map[string]any, 2)
		if connection.MaxBillableBytes != nil {
			limits["max_billable_bytes"] = *connection.MaxBillableBytes
		}
		if connection.MaxQueryCost != nil {
			limits["max_query_cost_usd"] = *connection.MaxQueryCost
		}
		appendRuntimeOnlyOperatorStage(outcome, AssetRenderStage{
			Kind:        "query_cost_guard",
			Language:    "json",
			Content:     mustRenderOperationJSON(map[string]any{"operation": "bigquery_dry_run_cost_guard", "limits": limits}),
			Status:      AssetRenderStageStatusOK,
			Fidelity:    AssetRenderFidelityRuntimeOnly,
			Conditional: true,
			Message:     "BigQuery performs a live dry run and blocks execution when a configured hard cost limit is exceeded",
		})
	}
}

func appendSnowflakeOperatorStages(outcome *assetRenderSemanticOutcome, info *directPipelineInfo, requestedFullRefresh bool) {
	asset := info.Asset
	if warehouse, ok := asset.Parameters.GetString("warehouse"); ok && strings.TrimSpace(warehouse) != "" {
		appendRuntimeOnlyOperatorStage(outcome, AssetRenderStage{
			Kind:        "warehouse_selection",
			Language:    "json",
			Content:     mustRenderOperationJSON(map[string]any{"fallback": "configured connection warehouse", "operation": "select_snowflake_warehouse_override"}),
			Status:      AssetRenderStageStatusOK,
			Fidelity:    AssetRenderFidelityRuntimeOnly,
			Conditional: true,
			Message:     "opens and pings the requested warehouse-specific client, then falls back to the configured connection warehouse when unavailable; the warehouse name is intentionally omitted",
		})
	}

	if asset.Materialization.Type != pipeline.MaterializationTypeNone {
		if container, ok := tablename.ContainerToCreate(asset.Name, strings.ToUpper); ok {
			appendSnowflakeSchemaStage(outcome, "Database", "CREATE DATABASE IF NOT EXISTS "+container)
		}
		if schema, ok := tablename.SchemaToCreate(asset.Name, strings.ToUpper); ok {
			appendSnowflakeSchemaStage(outcome, "Schema", "CREATE SCHEMA IF NOT EXISTS "+schema)
		}
	}

	// As in the direct operator, this gate uses the run-scoped materializer flag
	// rather than the per-asset effective strategy.
	if requestedFullRefresh {
		appendRuntimeOnlyOperatorStage(outcome, AssetRenderStage{
			Kind:        "target_compatibility",
			Language:    "json",
			Content:     mustRenderOperationJSON(map[string]any{"operation": "recreate_snowflake_target_on_materialization_type_mismatch", "requested_full_refresh": true}),
			Status:      AssetRenderStageStatusOK,
			Fidelity:    AssetRenderFidelityRuntimeOnly,
			Conditional: true,
			Message:     "reads live Snowflake information-schema metadata and recreates the target only when its materialization type is incompatible",
		})
	}
	if asset.Materialization.IsSCD2() && !requestedFullRefresh {
		appendRuntimeOnlyOperatorStage(outcome, AssetRenderStage{
			Kind:        "scd2_migration",
			Language:    "json",
			Content:     mustRenderOperationJSON(map[string]any{"operation": "migrate_snowflake_scd2_target", "strategy": asset.Materialization.Strategy}),
			Status:      AssetRenderStageStatusOK,
			Fidelity:    AssetRenderFidelityRuntimeOnly,
			Conditional: true,
			Message:     "inspects and migrates the live SCD2 target schema before submitting materializer SQL",
		})
	}
}

func appendSnowflakeSchemaStage(outcome *assetRenderSemanticOutcome, label, sql string) {
	outcome.status = mergeAssetRenderStatus(outcome.status, AssetRenderStatusPartial)
	outcome.stages = append(outcome.stages, AssetRenderStage{
		Kind:        "schema_preparation",
		Label:       label,
		Language:    "sql",
		Content:     sql,
		Status:      AssetRenderStageStatusOK,
		Fidelity:    AssetRenderFidelitySemantic,
		Conditional: true,
		Message:     "executed until the connection-local preparation cache records this object",
	})
}

func appendRuntimeOnlyOperatorStage(outcome *assetRenderSemanticOutcome, stage AssetRenderStage) {
	outcome.status = mergeAssetRenderStatus(outcome.status, AssetRenderStatusPartial)
	outcome.stages = append(outcome.stages, stage)
}

func selectedBigQueryRenderConnection(info *directPipelineInfo) (*config.GoogleCloudPlatformConnection, bool) {
	if info == nil || info.Config == nil || info.Config.SelectedEnvironment == nil || info.Config.SelectedEnvironment.Connections == nil {
		return nil, false
	}
	name, err := assetRenderConnectionName(info)
	if err != nil {
		return nil, false
	}
	connection, ok := info.Config.SelectedEnvironment.Connections.GetConnection(name).(*config.GoogleCloudPlatformConnection)
	return connection, ok
}

func bigQueryDatasetPreparationOperation(assetName string, connection *config.GoogleCloudPlatformConnection) (map[string]any, bool) {
	parts := strings.Split(assetName, ".")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return nil, false
		}
	}
	operation := map[string]any{"operation": "ensure_bigquery_dataset"}
	switch len(parts) {
	case 2:
		operation["dataset"] = parts[0]
		if connection != nil && strings.TrimSpace(connection.ProjectID) != "" {
			operation["project"] = connection.ProjectID
		}
	case 3:
		operation["project"] = parts[0]
		operation["dataset"] = parts[1]
	default:
		return nil, false
	}
	return operation, true
}

func warehouseExecutionRenderError(err error) assetRenderSemanticOutcome {
	return assetRenderSemanticOutcome{
		handled: true,
		status:  AssetRenderStatusPartial,
		stages:  []AssetRenderStage{failedExactRenderStage("execution_sql", err)},
	}
}
