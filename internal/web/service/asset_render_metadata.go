package service

import (
	"fmt"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/scheduler"
	"go.uber.org/zap"
)

type directMetadataPushBackend string

const (
	directMetadataPushPostgres  directMetadataPushBackend = "postgres"
	directMetadataPushBigQuery  directMetadataPushBackend = "bigquery"
	directMetadataPushSnowflake directMetadataPushBackend = "snowflake"
)

// directMetadataPushBackendForAssetType mirrors the metadata operators installed
// by buildDirectMainExecutors. Keeping the variants explicit is important: the
// scheduler creates a metadata task for every asset when metadata push is
// enabled, but Bruin's default executor is a no-op unless Renart replaces it
// with one of these live warehouse operators.
func directMetadataPushBackendForAssetType(assetType pipeline.AssetType) (directMetadataPushBackend, bool) {
	switch assetType {
	case pipeline.AssetTypePostgresQuery,
		pipeline.AssetTypePostgresSeed,
		pipeline.AssetTypePostgresQuerySensor,
		pipeline.AssetTypePostgresTableSensor,
		pipeline.AssetTypeRedshiftTableSensor:
		return directMetadataPushPostgres, true
	case pipeline.AssetTypeBigqueryQuery,
		pipeline.AssetTypeBigquerySeed,
		pipeline.AssetTypeBigqueryQuerySensor,
		pipeline.AssetTypeBigqueryTableSensor:
		return directMetadataPushBigQuery, true
	case pipeline.AssetTypeSnowflakeQuery,
		pipeline.AssetTypeSnowflakeSeed,
		pipeline.AssetTypeSnowflakeTableSensor:
		return directMetadataPushSnowflake, true
	default:
		return "", false
	}
}

type assetMetadataPushRenderOutcome struct {
	stages []AssetRenderStage
	issues []AssetRenderIssue
	status AssetRenderStatus
}

func renderAssetMetadataPushStages(pl *pipeline.Pipeline, asset *pipeline.Asset) assetMetadataPushRenderOutcome {
	outcome := assetMetadataPushRenderOutcome{
		stages: []AssetRenderStage{},
		issues: []AssetRenderIssue{},
		status: AssetRenderStatusOK,
	}
	if pl == nil || asset == nil {
		return outcome
	}

	// Ask the authoritative scheduler graph whether this run contains the task.
	// This preserves Bruin's pipeline-level gate rather than reinterpreting the
	// metadata_push configuration in the renderer.
	taskGraph := scheduler.NewScheduler(zap.NewNop().Sugar(), pl, assetRenderPreviewRunID)
	hasMetadataTask := false
	for _, instance := range taskGraph.GetTaskInstances() {
		if instance != nil && instance.GetAsset() == asset && instance.GetType() == scheduler.TaskInstanceTypeMetadataPush {
			hasMetadataTask = true
			break
		}
	}
	if !hasMetadataTask {
		return outcome
	}

	backend, supported := directMetadataPushBackendForAssetType(asset.Type)
	if !supported {
		stage := AssetRenderStage{
			Kind:        "metadata_push",
			Label:       "Metadata push",
			Language:    "json",
			Content:     mustRenderOperationJSON(map[string]any{"operation": "no_op", "asset_type": asset.Type}),
			Status:      AssetRenderStageStatusUnsupported,
			Fidelity:    AssetRenderFidelityUnsupported,
			Conditional: true,
			Message:     metadataPushTaskMessage("the scheduler creates this task, but direct execution has no metadata publisher for this asset type and treats it as a no-op"),
		}
		outcome.stages = append(outcome.stages, stage)
		outcome.issues = append(outcome.issues, AssetRenderIssue{
			Code:     "metadata_push_unsupported",
			Severity: "warning",
			Message:  stage.Message,
		})
		outcome.status = AssetRenderStatusPartial
		return outcome
	}

	switch backend {
	case directMetadataPushPostgres:
		return renderPostgresMetadataPush(asset)
	case directMetadataPushBigQuery:
		return renderBigQueryMetadataPush(asset)
	case directMetadataPushSnowflake:
		return renderSnowflakeMetadataPush(asset)
	default:
		return outcome
	}
}

func renderPostgresMetadataPush(asset *pipeline.Asset) assetMetadataPushRenderOutcome {
	if asset.Materialization.Type == pipeline.MaterializationTypeView {
		return metadataPushNoOpOutcome(
			directMetadataPushPostgres,
			"the PostgreSQL metadata operator skips views because it does not push column comments for them",
		)
	}

	parts := strings.Split(asset.Name, ".")
	if len(parts) != 2 && len(parts) != 3 {
		return metadataPushErrorOutcome(
			directMetadataPushPostgres,
			fmt.Sprintf("PostgreSQL metadata push requires a schema.table or database.schema.table asset name; %q has a different shape", asset.Name),
		)
	}
	if asset.Description == "" && len(asset.Columns) == 0 {
		return metadataPushErrorOutcome(
			directMetadataPushPostgres,
			"the PostgreSQL metadata operator rejects assets with neither a table description nor declared columns",
		)
	}

	operation := metadataPushOperation(directMetadataPushPostgres, asset, true)
	operation["behavior"] = map[string]any{
		"column_comments": "all_declared_columns",
		"table_comment":   asset.Description != "",
	}
	return metadataPushRuntimeOutcome(
		directMetadataPushPostgres,
		operation,
		"runs after the main task succeeds and submits table and declared-column comments through the live PostgreSQL-compatible client",
	)
}

func renderBigQueryMetadataPush(asset *pipeline.Asset) assetMetadataPushRenderOutcome {
	describedColumns := metadataPushColumns(asset, false)
	if asset.Description == "" && len(describedColumns) == 0 {
		return metadataPushNoOpOutcome(
			directMetadataPushBigQuery,
			"the BigQuery metadata operator treats an asset with no table or column descriptions as a successful no-op",
		)
	}

	parts := strings.Split(asset.Name, ".")
	if len(parts) != 2 && len(parts) != 3 {
		return metadataPushErrorOutcome(
			directMetadataPushBigQuery,
			fmt.Sprintf("BigQuery metadata push requires a dataset.table or project.dataset.table asset name; %q has a different shape", asset.Name),
		)
	}

	operation := metadataPushOperation(directMetadataPushBigQuery, asset, true)
	operation["primary_key_columns"] = asset.ColumnNamesWithPrimaryKey()
	operation["behavior"] = map[string]any{
		"missing_table":               "no_op",
		"matching_columns_only":       true,
		"preserve_undeclared_columns": true,
	}
	return metadataPushRuntimeOutcome(
		directMetadataPushBigQuery,
		operation,
		"reads live BigQuery table metadata and patches matching column descriptions, the table description, and declared primary keys; a missing table is a no-op",
	)
}

func renderSnowflakeMetadataPush(asset *pipeline.Asset) assetMetadataPushRenderOutcome {
	if asset.Materialization.Type == pipeline.MaterializationTypeView {
		return metadataPushNoOpOutcome(
			directMetadataPushSnowflake,
			"the Snowflake metadata operator skips views because it does not push column comments for them",
		)
	}

	parts := strings.Split(asset.Name, ".")
	if len(parts) != 2 && len(parts) != 3 {
		// This is the upstream client's established behavior, even though the
		// PostgreSQL and BigQuery clients reject the same malformed shape.
		return metadataPushNoOpOutcome(
			directMetadataPushSnowflake,
			"the Snowflake metadata client treats asset names outside schema.table or database.schema.table as a successful no-op",
		)
	}
	if asset.Description == "" && len(asset.Columns) == 0 {
		return metadataPushErrorOutcome(
			directMetadataPushSnowflake,
			"the Snowflake metadata operator rejects assets with neither a table description nor declared columns",
		)
	}

	operation := metadataPushOperation(directMetadataPushSnowflake, asset, false)
	operation["behavior"] = map[string]any{
		"compare_live_column_comments": true,
		"changed_descriptions_only":    true,
		"table_comment":                asset.Description != "",
	}
	if len(parts) == 2 {
		operation["database"] = "runtime_connection_default"
	}
	return metadataPushRuntimeOutcome(
		directMetadataPushSnowflake,
		operation,
		"reads live Snowflake column comments, changes only differing non-empty descriptions, and then pushes the table comment",
	)
}

func metadataPushOperation(backend directMetadataPushBackend, asset *pipeline.Asset, includeEmptyColumns bool) map[string]any {
	return map[string]any{
		"operation":         "push_metadata",
		"backend":           backend,
		"target":            asset.Name,
		"table_description": asset.Description,
		"columns":           metadataPushColumns(asset, includeEmptyColumns),
	}
}

func metadataPushColumns(asset *pipeline.Asset, includeEmpty bool) []map[string]string {
	columns := make([]map[string]string, 0, len(asset.Columns))
	for _, column := range asset.Columns {
		if !includeEmpty && column.Description == "" {
			continue
		}
		columns = append(columns, map[string]string{
			"name":        column.Name,
			"description": column.Description,
		})
	}
	return columns
}

func metadataPushStage(backend directMetadataPushBackend) AssetRenderStage {
	return AssetRenderStage{
		Kind:        "metadata_push",
		Label:       "Metadata push · " + metadataPushBackendLabel(backend),
		Language:    "json",
		Conditional: true,
	}
}

func metadataPushRuntimeOutcome(backend directMetadataPushBackend, operation map[string]any, message string) assetMetadataPushRenderOutcome {
	stage := metadataPushStage(backend)
	stage.Content = mustRenderOperationJSON(operation)
	stage.Status = AssetRenderStageStatusOK
	stage.Fidelity = AssetRenderFidelityRuntimeOnly
	stage.Message = metadataPushTaskMessage(message + "; connection resolution and the final warehouse mutation are runtime-only")
	return assetMetadataPushRenderOutcome{
		stages: []AssetRenderStage{stage},
		issues: []AssetRenderIssue{},
		status: AssetRenderStatusPartial,
	}
}

func metadataPushNoOpOutcome(backend directMetadataPushBackend, message string) assetMetadataPushRenderOutcome {
	stage := metadataPushStage(backend)
	stage.Content = mustRenderOperationJSON(map[string]any{
		"operation": "no_op",
		"backend":   backend,
	})
	stage.Status = AssetRenderStageStatusOK
	stage.Fidelity = AssetRenderFidelitySemantic
	stage.Message = metadataPushTaskMessage(message)
	return assetMetadataPushRenderOutcome{
		stages: []AssetRenderStage{stage},
		issues: []AssetRenderIssue{},
		status: AssetRenderStatusOK,
	}
}

func metadataPushErrorOutcome(backend directMetadataPushBackend, message string) assetMetadataPushRenderOutcome {
	stage := metadataPushStage(backend)
	stage.Content = mustRenderOperationJSON(map[string]any{
		"operation": "push_metadata",
		"backend":   backend,
	})
	stage.Status = AssetRenderStageStatusError
	stage.Fidelity = AssetRenderFidelitySemantic
	stage.Message = metadataPushTaskMessage(message)
	return assetMetadataPushRenderOutcome{
		stages: []AssetRenderStage{stage},
		issues: []AssetRenderIssue{{
			Code:     "metadata_push_invalid",
			Severity: "error",
			Message:  message,
		}},
		status: AssetRenderStatusPartial,
	}
}

func metadataPushTaskMessage(message string) string {
	return "Bruin marks this post-task non-blocking for downstream dependency scheduling; it runs after the main task independently of quality checks; " + message
}

func metadataPushBackendLabel(backend directMetadataPushBackend) string {
	switch backend {
	case directMetadataPushPostgres:
		return "PostgreSQL"
	case directMetadataPushBigQuery:
		return "BigQuery"
	case directMetadataPushSnowflake:
		return "Snowflake"
	default:
		return "Warehouse"
	}
}
