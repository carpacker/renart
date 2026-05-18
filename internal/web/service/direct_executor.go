package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/ansisql"
	ath "github.com/bruin-data/bruin/pkg/athena"
	bq "github.com/bruin-data/bruin/pkg/bigquery"
	ch "github.com/bruin-data/bruin/pkg/clickhouse"
	"github.com/bruin-data/bruin/pkg/config"
	dbsql "github.com/bruin-data/bruin/pkg/databricks"
	duck "github.com/bruin-data/bruin/pkg/duckdb"
	bruinexecutor "github.com/bruin-data/bruin/pkg/executor"
	fw "github.com/bruin-data/bruin/pkg/fabric"
	"github.com/bruin-data/bruin/pkg/git"
	bruiningestr "github.com/bruin-data/bruin/pkg/ingestr"
	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/mssql"
	ms "github.com/bruin-data/bruin/pkg/mssql"
	my "github.com/bruin-data/bruin/pkg/mysql"
	"github.com/bruin-data/bruin/pkg/oracle"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/postgres"
	pg "github.com/bruin-data/bruin/pkg/postgres"
	bruinpython "github.com/bruin-data/bruin/pkg/python"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/bruin-data/bruin/pkg/s3"
	"github.com/bruin-data/bruin/pkg/scheduler"
	sf "github.com/bruin-data/bruin/pkg/snowflake"
	"github.com/bruin-data/bruin/pkg/sqlparser"
	tri "github.com/bruin-data/bruin/pkg/trino"
	vert "github.com/bruin-data/bruin/pkg/vertica"
	"github.com/spf13/afero"
	"go.uber.org/zap"
)

type HybridBruinExecutor struct {
	newConnectionManager func(context.Context, string) (config.ConnectionAndDetailsGetter, error)
	newPipelineBuilder   func() *pipeline.Builder
	workspaceRoot        string
}

func NewHybridBruinExecutor(
	workspaceRoot string,
	binaryPath string,
	newConnectionManager func(context.Context, string) (config.ConnectionAndDetailsGetter, error),
	newPipelineBuilder func() *pipeline.Builder,
) *HybridBruinExecutor {
	return &HybridBruinExecutor{
		newConnectionManager: newConnectionManager,
		newPipelineBuilder:   newPipelineBuilder,
		workspaceRoot:        workspaceRoot,
	}
}

func (e *HybridBruinExecutor) RunAsset(ctx context.Context, req RunAssetRequest, onChunk func([]byte)) ([]byte, error) {
	if e.newPipelineBuilder == nil {
		return nil, fmt.Errorf("direct run requires a pipeline builder")
	}

	pp, err := getDirectPipelineAndAsset(ctx, e.workspaceRoot, req.AssetPath, afero.NewOsFs())
	if err != nil {
		return nil, err
	}

	if shouldFallbackToCLIRunAsset(pp.Asset, pp.Pipeline) {
		return nil, fmt.Errorf("direct run is not supported for asset type %q", pp.Asset.Type)
	}
	if _, err := selectConfigEnvironment(pp.Config, req.Environment); err != nil {
		return nil, fmt.Errorf("failed to use the environment '%s': %w", req.Environment, err)
	}

	manager, err := e.directConnectionManager(ctx, pp.Config)
	if err != nil {
		return nil, err
	}

	runCtx, parser, cleanup, err := buildDirectRunAssetContext(ctx, pp)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	renderer, err := buildDirectRunAssetRenderer(pp)
	if err != nil {
		return nil, err
	}

	mainExecutors, err := buildDirectMainExecutors(manager, renderer, parser, pp.Pipeline)
	if err != nil {
		return nil, err
	}

	s := scheduler.NewScheduler(zap.NewNop().Sugar(), pp.Pipeline, "renart-run")
	s.MarkAll(scheduler.Skipped)
	if !s.MarkAsset(pp.Asset, scheduler.Pending, false) {
		return nil, fmt.Errorf("asset '%s' was not found among the pipeline's scheduled task instances", pp.Asset.Name)
	}

	pending := s.GetTaskInstancesByStatus(scheduler.Pending)
	if len(pending) == 0 {
		return []byte(""), nil
	}

	printer := &streamCaptureWriter{buffer: bytes.NewBuffer(nil), onChunk: onChunk}
	formatting := directRunFormatting{doNotLogTaskName: true}
	if startDate, ok := runCtx.Value(pipeline.RunConfigStartDate).(time.Time); ok {
		formatting.startDate = startDate
	}
	if endDate, ok := runCtx.Value(pipeline.RunConfigEndDate).(time.Time); ok {
		formatting.endDate = endDate
	}
	writeDirectRunPrelude(printer, pp.Pipeline, pp.Asset, formatting)
	runCtx = context.WithValue(runCtx, bruinexecutor.KeyPrinter, printer)
	runCtx = context.WithValue(runCtx, bruinexecutor.ContextLogger, zap.NewNop().Sugar())

	seq := bruinexecutor.Sequential{TaskTypeMap: mainExecutors}
	results := make([]*scheduler.TaskExecutionResult, 0, len(pending))
	startedAt := time.Now()

	for {
		pending = s.GetTaskInstancesByStatus(scheduler.Pending)
		if len(pending) == 0 {
			break
		}

		progressed := false
		for _, instance := range pending {
			if instance.GetType() != scheduler.TaskInstanceTypeMain &&
				instance.GetType() != scheduler.TaskInstanceTypeColumnCheck &&
				instance.GetType() != scheduler.TaskInstanceTypeCustomCheck &&
				instance.GetType() != scheduler.TaskInstanceTypeMetadataPush {
				continue
			}
			if !allDirectRunPipelineDependenciesSucceeded(instance) {
				continue
			}

			progressed = true
			instance.MarkAs(scheduler.Running)
			writeDirectRunLifecycle(printer, instance, nil, true, 0)
			taskStartedAt := time.Now()
			if err := seq.RunSingleTask(runCtx, instance); err != nil {
				results = append(results, &scheduler.TaskExecutionResult{Instance: instance, Error: err})
				writeDirectRunLifecycle(printer, instance, err, false, time.Since(taskStartedAt))
				writeDirectRunSummary(printer, buildDirectRunSummary(results, time.Since(startedAt)))
				return printer.buffer.Bytes(), err
			}
			instance.MarkAs(scheduler.Succeeded)
			results = append(results, &scheduler.TaskExecutionResult{Instance: instance, Error: nil})
			writeDirectRunLifecycle(printer, instance, nil, false, time.Since(taskStartedAt))
		}

		if !progressed {
			writeDirectRunSummary(printer, buildDirectRunSummary(results, time.Since(startedAt)))
			return printer.buffer.Bytes(), fmt.Errorf("direct run stalled: no runnable task instances remained")
		}
	}

	writeDirectRunSummary(printer, buildDirectRunSummary(results, time.Since(startedAt)))
	return printer.buffer.Bytes(), nil
}

func (e *HybridBruinExecutor) RunPipeline(ctx context.Context, req RunPipelineRequest, onChunk func([]byte)) ([]byte, error) {
	if e.newPipelineBuilder == nil {
		return nil, fmt.Errorf("direct pipeline run requires a pipeline builder")
	}

	resolvedTarget := resolveDirectPath(e.workspaceRoot, req.Target)
	builder := e.newPipelineBuilder()
	foundPipeline, err := builder.CreatePipelineFromPath(ctx, resolvedTarget, pipeline.WithMutate())
	if err != nil {
		return nil, err
	}
	if foundPipeline == nil {
		return nil, fmt.Errorf("pipeline not found")
	}
	if shouldFallbackToCLIRunPipeline(foundPipeline) {
		return nil, fmt.Errorf("direct pipeline run is not supported for one or more asset types")
	}

	repoRoot, err := git.FindRepoFromPath(resolvedTarget)
	if err != nil {
		return nil, fmt.Errorf("failed to find the git repository root: %w", err)
	}
	configPath := filepath.Join(repoRoot.Path, ".bruin.yml")
	cfg, err := loadSelectedConfig(configPath, req.Environment)
	if err != nil {
		return nil, err
	}

	pp := &directPipelineInfo{Pipeline: foundPipeline, Config: cfg}
	manager, err := e.directConnectionManager(ctx, cfg)
	if err != nil {
		return nil, err
	}
	runCtx, parser, cleanup, err := buildDirectRunAssetContext(ctx, pp)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	renderer, err := buildDirectRunAssetRenderer(pp)
	if err != nil {
		return nil, err
	}
	mainExecutors, err := buildDirectMainExecutors(manager, renderer, parser, foundPipeline)
	if err != nil {
		return nil, err
	}

	printer := &streamCaptureWriter{buffer: bytes.NewBuffer(nil), onChunk: onChunk}
	formatting := directRunFormatting{}
	if startDate, ok := runCtx.Value(pipeline.RunConfigStartDate).(time.Time); ok {
		formatting.startDate = startDate
	}
	if endDate, ok := runCtx.Value(pipeline.RunConfigEndDate).(time.Time); ok {
		formatting.endDate = endDate
	}
	writeDirectRunPrelude(printer, foundPipeline, nil, formatting)
	runCtx = context.WithValue(runCtx, bruinexecutor.KeyPrinter, printer)
	runCtx = context.WithValue(runCtx, bruinexecutor.ContextLogger, zap.NewNop().Sugar())

	seq := bruinexecutor.Sequential{TaskTypeMap: mainExecutors}
	s := scheduler.NewScheduler(zap.NewNop().Sugar(), foundPipeline, "renart-run")
	s.MarkAll(scheduler.Pending)
	results := make([]*scheduler.TaskExecutionResult, 0)
	startedAt := time.Now()

	for {
		pending := s.GetTaskInstancesByStatus(scheduler.Pending)
		if len(pending) == 0 {
			break
		}

		progressed := false
		for _, instance := range pending {
			if instance.GetType() != scheduler.TaskInstanceTypeMain &&
				instance.GetType() != scheduler.TaskInstanceTypeColumnCheck &&
				instance.GetType() != scheduler.TaskInstanceTypeCustomCheck &&
				instance.GetType() != scheduler.TaskInstanceTypeMetadataPush {
				continue
			}
			if !allDirectRunPipelineDependenciesSucceeded(instance) {
				continue
			}

			progressed = true
			instance.MarkAs(scheduler.Running)
			writeDirectRunLifecycle(printer, instance, nil, true, 0)
			taskStartedAt := time.Now()
			if err := seq.RunSingleTask(runCtx, instance); err != nil {
				results = append(results, &scheduler.TaskExecutionResult{Instance: instance, Error: err})
				writeDirectRunLifecycle(printer, instance, err, false, time.Since(taskStartedAt))
				writeDirectRunSummary(printer, buildDirectRunSummary(results, time.Since(startedAt)))
				return printer.buffer.Bytes(), err
			}
			instance.MarkAs(scheduler.Succeeded)
			results = append(results, &scheduler.TaskExecutionResult{Instance: instance, Error: nil})
			writeDirectRunLifecycle(printer, instance, nil, false, time.Since(taskStartedAt))
		}

		if !progressed {
			writeDirectRunSummary(printer, buildDirectRunSummary(results, time.Since(startedAt)))
			return printer.buffer.Bytes(), fmt.Errorf("direct run stalled: no runnable task instances remained")
		}
	}

	writeDirectRunSummary(printer, buildDirectRunSummary(results, time.Since(startedAt)))
	return printer.buffer.Bytes(), nil
}

func (e *HybridBruinExecutor) QueryAsset(ctx context.Context, req QueryAssetRequest) ([]byte, error) {
	if e.newPipelineBuilder == nil {
		return nil, fmt.Errorf("direct asset query requires a pipeline builder")
	}

	pp, err := getDirectPipelineAndAsset(ctx, e.workspaceRoot, req.AssetPath, afero.NewOsFs())
	if err != nil {
		output, marshalErr := json.Marshal(directErrorResponse{Error: err.Error()})
		if marshalErr != nil {
			return nil, err
		}
		return output, err
	}

	if !pp.Asset.IsSQLAsset() {
		err := fmt.Errorf("asset '%s' is not a SQL asset (type: %s). Only SQL assets can be queried", req.AssetPath, pp.Asset.Type)
		output, marshalErr := json.Marshal(directErrorResponse{Error: err.Error()})
		if marshalErr != nil {
			return nil, err
		}
		return output, err
	}

	connName, conn, queryStr, err := e.buildDirectAssetQuery(ctx, pp, req.Environment)
	if err != nil {
		output, marshalErr := json.Marshal(directErrorResponse{Error: err.Error()})
		if marshalErr != nil {
			return nil, err
		}
		return output, err
	}

	dialect, err := sqlparser.AssetTypeToDialect(pp.Asset.Type)
	if err != nil {
		dialect = ""
	}

	if dialect != "" {
		isSelect, selectErr := isReadOnlySelectQuery(queryStr, pp.Asset.Type)
		if selectErr == nil && !isSelect {
			output, marshalErr := json.Marshal(directErrorResponse{Error: inspectReadOnlyErrorMessage})
			if marshalErr != nil {
				return nil, fmt.Errorf(inspectReadOnlyErrorMessage)
			}
			return output, fmt.Errorf(inspectReadOnlyErrorMessage)
		}
	}

	var parser *sqlparser.SQLParser
	needsParser := strings.TrimSpace(req.Limit) != "" || pp.Config.SelectedEnvironment.SchemaPrefix != ""
	if needsParser {
		parser, err = sqlparser.NewSQLParser(false)
		if err != nil {
			wrappedErr := fmt.Errorf("failed to initialize SQL parser: %w", err)
			output, marshalErr := json.Marshal(directErrorResponse{Error: wrappedErr.Error()})
			if marshalErr != nil {
				return nil, wrappedErr
			}
			return output, wrappedErr
		}
		defer parser.Close()
		if err := parser.Start(); err != nil {
			wrappedErr := fmt.Errorf("failed to start SQL parser: %w", err)
			output, marshalErr := json.Marshal(directErrorResponse{Error: wrappedErr.Error()})
			if marshalErr != nil {
				return nil, wrappedErr
			}
			return output, wrappedErr
		}
	}

	if parser != nil && pp.Config.SelectedEnvironment.SchemaPrefix != "" {
		queryStr, err = applyDirectSchemaPrefix(ctx, queryStr, dialect, parser, pp, conn)
		if err != nil {
			wrappedErr := fmt.Errorf("failed to apply schema prefix: %w", err)
			output, marshalErr := json.Marshal(directErrorResponse{Error: wrappedErr.Error()})
			if marshalErr != nil {
				return nil, wrappedErr
			}
			return output, wrappedErr
		}
	}

	if parser != nil && strings.TrimSpace(req.Limit) != "" {
		limitValue, convErr := strconv.ParseInt(strings.TrimSpace(req.Limit), 10, 64)
		if convErr == nil {
			queryStr = addDirectLimitToQuery(queryStr, limitValue, conn, parser, dialect)
		}
	}

	querier, ok := conn.(interface {
		SelectWithSchema(context.Context, *query.Query) (*query.QueryResult, error)
	})
	if !ok {
		err := fmt.Errorf("connection type %s does not support querying", connName)
		output, marshalErr := json.Marshal(directErrorResponse{Error: err.Error()})
		if marshalErr != nil {
			return nil, err
		}
		return output, err
	}

	result, err := querier.SelectWithSchema(ctx, &query.Query{Query: queryStr})
	if err != nil {
		wrappedErr := fmt.Errorf("query execution failed: %w", err)
		output, marshalErr := json.Marshal(directErrorResponse{Error: wrappedErr.Error()})
		if marshalErr != nil {
			return nil, wrappedErr
		}
		return output, wrappedErr
	}

	response := NewQueryResultDTO(result, connName, queryStr)
	return json.Marshal(response)
}

func (e *HybridBruinExecutor) QueryConnection(ctx context.Context, req QueryConnectionRequest) ([]byte, error) {
	if e.newConnectionManager == nil {
		return nil, fmt.Errorf("direct connection query requires a connection manager")
	}

	manager, err := e.newConnectionManager(ctx, req.Environment)
	if err != nil {
		return nil, err
	}

	conn := manager.GetConnection(req.ConnectionName)
	if conn == nil {
		return nil, fmt.Errorf("connection %q not found", req.ConnectionName)
	}

	querier, ok := conn.(interface {
		SelectWithSchema(context.Context, *query.Query) (*query.QueryResult, error)
	})
	if !ok {
		return nil, fmt.Errorf("connection %q does not support querying", req.ConnectionName)
	}

	result, err := querier.SelectWithSchema(ctx, &query.Query{Query: req.Query})
	if err != nil {
		return nil, err
	}

	response := NewQueryResultDTO(result, req.ConnectionName, req.Query)

	if strings.EqualFold(strings.TrimSpace(req.Output), "json") || strings.TrimSpace(req.Output) == "" {
		return json.Marshal(response)
	}

	return nil, fmt.Errorf("direct connection query only supports json output")
}

func (e *HybridBruinExecutor) buildDirectAssetQuery(ctx context.Context, pp *directPipelineInfo, environment string) (string, interface{}, string, error) {
	if strings.TrimSpace(environment) != "" {
		if _, err := selectConfigEnvironment(pp.Config, environment); err != nil {
			return "", nil, "", fmt.Errorf("failed to use the environment '%s': %w", environment, err)
		}
	}

	var manager config.ConnectionAndDetailsGetter
	if e.newConnectionManager != nil {
		var err error
		manager, err = e.newConnectionManager(ctx, pp.Config.SelectedEnvironmentName)
		if err != nil {
			return "", nil, "", fmt.Errorf("failed to create connection manager: %w", err)
		}
	} else {
		connectionManager, err := newConnectionManagerFromConfig(ctx, pp.Config)
		if err != nil {
			return "", nil, "", fmt.Errorf("failed to create connection manager: %w", err)
		}
		manager = connectionManager
	}

	connName, err := pp.Pipeline.GetConnectionNameForAsset(pp.Asset)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to get connection: %w", err)
	}
	conn := manager.GetConnection(connName)
	if conn == nil {
		return "", nil, "", fmt.Errorf("connection %q not found", connName)
	}

	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)
	startDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC)
	endDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 59, 59, 0, time.UTC)
	renderer := jinja.NewRendererWithStartEndDates(&startDate, &endDate, &now, pp.Pipeline.Name, "your-run-id", nil)
	fetchCtx := context.WithValue(ctx, pipeline.RunConfigStartDate, startDate)
	fetchCtx = context.WithValue(fetchCtx, pipeline.RunConfigEndDate, endDate)
	fetchCtx = context.WithValue(fetchCtx, pipeline.RunConfigExecutionDate, now)
	fetchCtx = context.WithValue(fetchCtx, pipeline.RunConfigRunID, "your-run-id")
	fetchCtx = context.WithValue(fetchCtx, config.EnvironmentContextKey, pp.Config.SelectedEnvironment)

	extractor := &query.WholeFileExtractor{Fs: afero.NewOsFs(), Renderer: renderer}
	clonedExtractor, err := extractor.CloneForAsset(fetchCtx, pp.Pipeline, pp.Asset)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to clone extractor for asset %s: %w", pp.Asset.Name, err)
	}

	queries, err := clonedExtractor.ExtractQueriesFromString(pp.Asset.ExecutableFile.Content)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to extract query: %w", err)
	}
	if len(queries) == 0 {
		return "", nil, "", fmt.Errorf("no query found in asset")
	}

	return connName, conn, queries[0].Query, nil
}

func (e *HybridBruinExecutor) directConnectionManager(ctx context.Context, cfg *config.Config) (config.ConnectionAndDetailsGetter, error) {
	if e.newConnectionManager != nil {
		return e.newConnectionManager(ctx, cfg.SelectedEnvironmentName)
	}

	return newConnectionManagerFromConfig(ctx, cfg)
}

func shouldFallbackToCLIRunAsset(asset *pipeline.Asset, foundPipeline *pipeline.Pipeline) bool {
	if asset == nil || foundPipeline == nil {
		return true
	}
	if isDirectRunAssetTypeSupported(asset.Type) {
		return false
	}
	_, known := bruinexecutor.DefaultExecutorsV2[asset.Type]
	return !known
}

func shouldFallbackToCLIRunPipeline(foundPipeline *pipeline.Pipeline) bool {
	if foundPipeline == nil {
		return true
	}
	for _, asset := range foundPipeline.Assets {
		if asset == nil {
			return true
		}
		if shouldFallbackToCLIRunAsset(asset, foundPipeline) {
			return true
		}
	}
	return false
}

func isDirectRunAssetTypeSupported(assetType pipeline.AssetType) bool {
	_, ok := directRunAssetTypes[assetType]
	return ok
}

var directRunAssetTypes = map[pipeline.AssetType]struct{}{
	pipeline.AssetTypeDuckDBQuery:             {},
	pipeline.AssetTypeDuckDBSeed:              {},
	pipeline.AssetTypeMotherduckQuery:         {},
	pipeline.AssetTypePostgresQuery:           {},
	pipeline.AssetTypePostgresSeed:            {},
	pipeline.AssetTypeRedshiftQuery:           {},
	pipeline.AssetTypeRedshiftSeed:            {},
	pipeline.AssetTypeBigqueryQuery:           {},
	pipeline.AssetTypeBigquerySeed:            {},
	pipeline.AssetTypeAthenaQuery:             {},
	pipeline.AssetTypeAthenaSeed:              {},
	pipeline.AssetTypeDatabricksQuery:         {},
	pipeline.AssetTypeDatabricksSeed:          {},
	pipeline.AssetTypeFabricQuery:             {},
	pipeline.AssetTypeFabricSeed:              {},
	pipeline.AssetTypeFabricQueryLegacy:       {},
	pipeline.AssetTypeFabricSeedLegacy:        {},
	pipeline.AssetTypeMySQLQuery:              {},
	pipeline.AssetTypeMySQLSeed:               {},
	pipeline.AssetTypeSnowflakeQuery:          {},
	pipeline.AssetTypeSnowflakeSeed:           {},
	pipeline.AssetTypeMsSQLQuery:              {},
	pipeline.AssetTypeMsSQLSeed:               {},
	pipeline.AssetTypeSynapseQuery:            {},
	pipeline.AssetTypeSynapseSeed:             {},
	pipeline.AssetTypeClickHouse:              {},
	pipeline.AssetTypeClickHouseSeed:          {},
	pipeline.AssetTypeTrinoQuery:              {},
	pipeline.AssetTypeVerticaQuery:            {},
	pipeline.AssetTypeVerticaSeed:             {},
	pipeline.AssetTypeOracleQuery:             {},
	pipeline.AssetTypeBigqueryQuerySensor:     {},
	pipeline.AssetTypeBigqueryTableSensor:     {},
	pipeline.AssetTypePostgresQuerySensor:     {},
	pipeline.AssetTypePostgresTableSensor:     {},
	pipeline.AssetTypeRedshiftQuerySensor:     {},
	pipeline.AssetTypeRedshiftTableSensor:     {},
	pipeline.AssetTypeMySQLQuerySensor:        {},
	pipeline.AssetTypeMySQLTableSensor:        {},
	pipeline.AssetTypeClickHouseQuerySensor:   {},
	pipeline.AssetTypeClickHouseTableSensor:   {},
	pipeline.AssetTypeMsSQLQuerySensor:        {},
	pipeline.AssetTypeMsSQLTableSensor:        {},
	pipeline.AssetTypeFabricQuerySensor:       {},
	pipeline.AssetTypeFabricQuerySensorLegacy: {},
	pipeline.AssetTypeFabricTableSensor:       {},
	pipeline.AssetTypeFabricTableSensorLegacy: {},
	pipeline.AssetTypeDatabricksQuerySensor:   {},
	pipeline.AssetTypeDatabricksTableSensor:   {},
	pipeline.AssetTypeAthenaSQLSensor:         {},
	pipeline.AssetTypeAthenaTableSensor:       {},
	pipeline.AssetTypeDuckDBQuerySensor:       {},
	pipeline.AssetTypeSynapseQuerySensor:      {},
	pipeline.AssetTypeSynapseTableSensor:      {},
	pipeline.AssetTypeSnowflakeQuerySensor:    {},
	pipeline.AssetTypeSnowflakeTableSensor:    {},
	pipeline.AssetTypeTrinoQuerySensor:        {},
	pipeline.AssetTypeVerticaQuerySensor:      {},
	pipeline.AssetTypeVerticaTableSensor:      {},
	pipeline.AssetTypeS3KeySensor:             {},
	pipeline.AssetTypePython:                  {},
	pipeline.AssetTypeIngestr:                 {},
}

func allDirectRunPipelineDependenciesSucceeded(instance scheduler.TaskInstance) bool {
	for _, upstream := range instance.GetUpstream() {
		if upstream.GetStatus() != scheduler.Succeeded && upstream.GetStatus() != scheduler.Skipped {
			return false
		}
	}
	return true
}

func buildDirectRunAssetContext(ctx context.Context, pp *directPipelineInfo) (context.Context, *sqlparser.SQLParser, func(), error) {
	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)
	startDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC)
	endDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 59, 59, 0, time.UTC)

	runCtx := context.WithValue(ctx, pipeline.RunConfigFullRefresh, false)
	runCtx = context.WithValue(runCtx, pipeline.RunConfigStartDate, startDate)
	runCtx = context.WithValue(runCtx, pipeline.RunConfigEndDate, endDate)
	runCtx = context.WithValue(runCtx, pipeline.RunConfigExecutionDate, now)
	runCtx = context.WithValue(runCtx, pipeline.RunConfigRunID, "renart-run")
	runCtx = context.WithValue(runCtx, config.EnvironmentContextKey, pp.Config.SelectedEnvironment)
	runCtx = context.WithValue(runCtx, config.EnvironmentNameContextKey, pp.Config.SelectedEnvironmentName)
	runCtx = context.WithValue(runCtx, bruinexecutor.KeyIsDebug, false)
	runCtx = context.WithValue(runCtx, bruinexecutor.KeyVerbose, false)
	runCtx = context.WithValue(runCtx, config.SecretsBackendContextKey, "")

	if pp.Config.SelectedEnvironment == nil || pp.Config.SelectedEnvironment.SchemaPrefix == "" {
		return runCtx, nil, func() {}, nil
	}

	parser, err := sqlparser.NewSQLParser(false)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := parser.Start(); err != nil {
		parser.Close()
		return nil, nil, nil, err
	}

	cleanup := func() {
		parser.Close()
	}

	return runCtx, parser, cleanup, nil
}

func buildDirectRunAssetRenderer(pp *directPipelineInfo) (*jinja.Renderer, error) {
	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)
	startDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC)
	endDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 59, 59, 0, time.UTC)
	macroContent, err := jinja.LoadMacros(afero.NewOsFs(), pp.Pipeline.MacrosPath)
	if err != nil {
		return nil, err
	}

	return jinja.NewRendererWithStartEndDatesAndMacros(&startDate, &endDate, &now, pp.Pipeline.Name, "renart-run", nil, macroContent), nil
}

func buildDirectMainExecutors(manager config.ConnectionAndDetailsGetter, renderer *jinja.Renderer, parser *sqlparser.SQLParser, pl *pipeline.Pipeline) (map[pipeline.AssetType]bruinexecutor.Config, error) {
	executors := make(map[pipeline.AssetType]bruinexecutor.Config, len(bruinexecutor.DefaultExecutorsV2))
	for assetType, cfg := range bruinexecutor.DefaultExecutorsV2 {
		if cfg == nil {
			executors[assetType] = nil
			continue
		}
		cloned := make(bruinexecutor.Config, len(cfg))
		for instanceType, operator := range cfg {
			cloned[instanceType] = operator
		}
		executors[assetType] = cloned
	}
	for assetType := range executors {
		if isDirectRunAssetTypeSupported(assetType) {
			continue
		}
		if executors[assetType] == nil {
			executors[assetType] = bruinexecutor.Config{}
		}
		executors[assetType][scheduler.TaskInstanceTypeMain] = directUnsupportedOperator{assetType: assetType}
	}

	wholeFileExtractor := &query.WholeFileExtractor{Fs: afero.NewOsFs(), Renderer: renderer}
	customCheckRunner := ansisql.NewCustomCheckOperator(manager, renderer)
	seedOperator, err := bruiningestr.NewSeedOperator(manager, renderer)
	if err != nil {
		return nil, err
	}
	ensureExecutorConfig := func(assetType pipeline.AssetType) {
		if executors[assetType] == nil {
			executors[assetType] = bruinexecutor.Config{}
		}
	}
	assignSeedExecutor := func(assetType pipeline.AssetType, columnCheck bruinexecutor.Operator, customCheck bruinexecutor.Operator, metadataPush bruinexecutor.Operator) {
		ensureExecutorConfig(assetType)
		executors[assetType][scheduler.TaskInstanceTypeMain] = seedOperator
		if columnCheck != nil {
			executors[assetType][scheduler.TaskInstanceTypeColumnCheck] = columnCheck
		}
		if customCheck != nil {
			executors[assetType][scheduler.TaskInstanceTypeCustomCheck] = customCheck
		}
		if metadataPush != nil {
			executors[assetType][scheduler.TaskInstanceTypeMetadataPush] = metadataPush
		}
	}

	ensureExecutorConfig(pipeline.AssetTypeDuckDBQuery)
	executors[pipeline.AssetTypeDuckDBQuery][scheduler.TaskInstanceTypeMain] = duck.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: duck.NewMaterializer(false),
	}, parser)
	duckColumnCheckOperator := duck.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeDuckDBQuery][scheduler.TaskInstanceTypeColumnCheck] = duckColumnCheckOperator
	executors[pipeline.AssetTypeDuckDBQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeDuckDBSeed, duckColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeMotherduckQuery)
	executors[pipeline.AssetTypeMotherduckQuery][scheduler.TaskInstanceTypeMain] = duck.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: duck.NewMaterializer(false),
	}, parser)
	executors[pipeline.AssetTypeMotherduckQuery][scheduler.TaskInstanceTypeColumnCheck] = duck.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeMotherduckQuery][scheduler.TaskInstanceTypeCustomCheck] = ansisql.NewCustomCheckOperator(manager, renderer)
	ensureExecutorConfig(pipeline.AssetTypePostgresQuery)
	executors[pipeline.AssetTypePostgresQuery][scheduler.TaskInstanceTypeMain] = pg.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: pg.NewMaterializer(false),
	}, parser)
	pgColumnCheckOperator := pg.NewColumnCheckOperator(manager)
	pgMetadataPushOperator := pg.NewMetadataPushOperator(manager)
	executors[pipeline.AssetTypePostgresQuery][scheduler.TaskInstanceTypeColumnCheck] = pgColumnCheckOperator
	executors[pipeline.AssetTypePostgresQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	executors[pipeline.AssetTypePostgresQuery][scheduler.TaskInstanceTypeMetadataPush] = pgMetadataPushOperator
	assignSeedExecutor(pipeline.AssetTypePostgresSeed, pgColumnCheckOperator, customCheckRunner, pgMetadataPushOperator)
	ensureExecutorConfig(pipeline.AssetTypeRedshiftQuery)
	executors[pipeline.AssetTypeRedshiftQuery][scheduler.TaskInstanceTypeMain] = pg.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: pg.NewMaterializer(false),
	}, parser)
	executors[pipeline.AssetTypeRedshiftQuery][scheduler.TaskInstanceTypeColumnCheck] = pgColumnCheckOperator
	executors[pipeline.AssetTypeRedshiftQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeRedshiftSeed, pgColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeBigqueryQuery)
	executors[pipeline.AssetTypeBigqueryQuery][scheduler.TaskInstanceTypeMain] = bq.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: bq.NewMaterializer(false),
	}, parser)
	bqColumnCheckOperator, err := bq.NewColumnCheckOperator(manager)
	if err != nil {
		return nil, err
	}
	executors[pipeline.AssetTypeBigqueryQuery][scheduler.TaskInstanceTypeColumnCheck] = bqColumnCheckOperator
	bqMetadataPushOperator := bq.NewMetadataPushOperator(manager)
	executors[pipeline.AssetTypeBigqueryQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	executors[pipeline.AssetTypeBigqueryQuery][scheduler.TaskInstanceTypeMetadataPush] = bqMetadataPushOperator
	assignSeedExecutor(pipeline.AssetTypeBigquerySeed, bqColumnCheckOperator, customCheckRunner, bqMetadataPushOperator)
	ensureExecutorConfig(pipeline.AssetTypeBigqueryQuerySensor)
	executors[pipeline.AssetTypeBigqueryQuerySensor][scheduler.TaskInstanceTypeMain] = bq.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeBigqueryTableSensor)
	executors[pipeline.AssetTypeBigqueryTableSensor][scheduler.TaskInstanceTypeMain] = bq.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeAthenaQuery)
	executors[pipeline.AssetTypeAthenaQuery][scheduler.TaskInstanceTypeMain] = ath.NewBasicOperator(manager, wholeFileExtractor, ath.NewMaterializer(false), parser)
	athenaColumnCheckOperator := ath.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeAthenaQuery][scheduler.TaskInstanceTypeColumnCheck] = athenaColumnCheckOperator
	executors[pipeline.AssetTypeAthenaQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeAthenaSeed, athenaColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeAthenaSQLSensor)
	executors[pipeline.AssetTypeAthenaSQLSensor][scheduler.TaskInstanceTypeMain] = ath.NewQuerySensor(manager, renderer, 30)
	ensureExecutorConfig(pipeline.AssetTypeAthenaTableSensor)
	executors[pipeline.AssetTypeAthenaTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeDatabricksQuery)
	executors[pipeline.AssetTypeDatabricksQuery][scheduler.TaskInstanceTypeMain] = dbsql.NewBasicOperator(manager, wholeFileExtractor, dbsql.NewMaterializer(false), parser)
	databricksColumnCheckOperator := dbsql.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeDatabricksQuery][scheduler.TaskInstanceTypeColumnCheck] = databricksColumnCheckOperator
	executors[pipeline.AssetTypeDatabricksQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeDatabricksSeed, databricksColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeDatabricksQuerySensor)
	executors[pipeline.AssetTypeDatabricksQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeDatabricksTableSensor)
	executors[pipeline.AssetTypeDatabricksTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeFabricQuery)
	executors[pipeline.AssetTypeFabricQuery][scheduler.TaskInstanceTypeMain] = fw.NewBasicOperator(manager, wholeFileExtractor, fw.NewMaterializer(false), parser)
	fabricColumnCheckOperator := fw.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeFabricQuery][scheduler.TaskInstanceTypeColumnCheck] = fabricColumnCheckOperator
	executors[pipeline.AssetTypeFabricQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeFabricSeed, fabricColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeFabricQuerySensor)
	executors[pipeline.AssetTypeFabricQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeFabricTableSensor)
	executors[pipeline.AssetTypeFabricTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeFabricQueryLegacy)
	executors[pipeline.AssetTypeFabricQueryLegacy][scheduler.TaskInstanceTypeMain] = fw.NewBasicOperator(manager, wholeFileExtractor, fw.NewMaterializer(false), parser)
	executors[pipeline.AssetTypeFabricQueryLegacy][scheduler.TaskInstanceTypeColumnCheck] = fabricColumnCheckOperator
	executors[pipeline.AssetTypeFabricQueryLegacy][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeFabricSeedLegacy, fabricColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeFabricQuerySensorLegacy)
	executors[pipeline.AssetTypeFabricQuerySensorLegacy][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeFabricTableSensorLegacy)
	executors[pipeline.AssetTypeFabricTableSensorLegacy][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeMySQLQuery)
	executors[pipeline.AssetTypeMySQLQuery][scheduler.TaskInstanceTypeMain] = my.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: my.NewMaterializer(false),
	}, parser)
	mySQLColumnCheckOperator := my.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeMySQLQuery][scheduler.TaskInstanceTypeColumnCheck] = mySQLColumnCheckOperator
	executors[pipeline.AssetTypeMySQLQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeMySQLSeed, mySQLColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeMySQLQuerySensor)
	executors[pipeline.AssetTypeMySQLQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeMySQLTableSensor)
	executors[pipeline.AssetTypeMySQLTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeSnowflakeQuery)
	executors[pipeline.AssetTypeSnowflakeQuery][scheduler.TaskInstanceTypeMain] = sf.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: sf.NewMaterializer(false),
	}, parser)
	snowflakeColumnCheckOperator := sf.NewColumnCheckOperator(manager)
	snowflakeMetadataPushOperator := sf.NewMetadataPushOperator(manager)
	executors[pipeline.AssetTypeSnowflakeQuery][scheduler.TaskInstanceTypeColumnCheck] = snowflakeColumnCheckOperator
	executors[pipeline.AssetTypeSnowflakeQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	executors[pipeline.AssetTypeSnowflakeQuery][scheduler.TaskInstanceTypeMetadataPush] = snowflakeMetadataPushOperator
	assignSeedExecutor(pipeline.AssetTypeSnowflakeSeed, snowflakeColumnCheckOperator, customCheckRunner, snowflakeMetadataPushOperator)
	ensureExecutorConfig(pipeline.AssetTypeSnowflakeQuerySensor)
	executors[pipeline.AssetTypeSnowflakeQuerySensor][scheduler.TaskInstanceTypeMain] = sf.NewQuerySensor(manager, wholeFileExtractor, 30)
	ensureExecutorConfig(pipeline.AssetTypeSnowflakeTableSensor)
	executors[pipeline.AssetTypeSnowflakeTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeMsSQLQuery)
	executors[pipeline.AssetTypeMsSQLQuery][scheduler.TaskInstanceTypeMain] = ms.NewBasicOperator(manager, wholeFileExtractor, ms.NewMaterializer(false), parser)
	msSQLColumnCheckOperator := ms.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeMsSQLQuery][scheduler.TaskInstanceTypeColumnCheck] = msSQLColumnCheckOperator
	executors[pipeline.AssetTypeMsSQLQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeMsSQLSeed, msSQLColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeMsSQLQuerySensor)
	executors[pipeline.AssetTypeMsSQLQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeMsSQLTableSensor)
	executors[pipeline.AssetTypeMsSQLTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeSynapseQuery)
	executors[pipeline.AssetTypeSynapseQuery][scheduler.TaskInstanceTypeMain] = ms.NewBasicOperator(manager, wholeFileExtractor, ms.NewMaterializer(false), parser)
	executors[pipeline.AssetTypeSynapseQuery][scheduler.TaskInstanceTypeColumnCheck] = msSQLColumnCheckOperator
	executors[pipeline.AssetTypeSynapseQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeSynapseSeed, msSQLColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeSynapseQuerySensor)
	executors[pipeline.AssetTypeSynapseQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeSynapseTableSensor)
	executors[pipeline.AssetTypeSynapseTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeClickHouse)
	executors[pipeline.AssetTypeClickHouse][scheduler.TaskInstanceTypeMain] = ch.NewBasicOperator(manager, wholeFileExtractor, ch.NewMaterializer(false), parser)
	clickHouseColumnCheckOperator := ch.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeClickHouse][scheduler.TaskInstanceTypeColumnCheck] = clickHouseColumnCheckOperator
	executors[pipeline.AssetTypeClickHouse][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeClickHouseSeed, clickHouseColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeClickHouseQuerySensor)
	executors[pipeline.AssetTypeClickHouseQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeClickHouseTableSensor)
	executors[pipeline.AssetTypeClickHouseTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeTrinoQuery)
	executors[pipeline.AssetTypeTrinoQuery][scheduler.TaskInstanceTypeMain] = tri.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: tri.NewMaterializer(false),
	}, parser)
	executors[pipeline.AssetTypeTrinoQuery][scheduler.TaskInstanceTypeCustomCheck] = ansisql.NewCustomCheckOperator(manager, renderer)
	ensureExecutorConfig(pipeline.AssetTypeTrinoQuerySensor)
	executors[pipeline.AssetTypeTrinoQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeVerticaQuery)
	executors[pipeline.AssetTypeVerticaQuery][scheduler.TaskInstanceTypeMain] = vert.NewBasicOperator(manager, wholeFileExtractor, vert.NewMaterializer(false), parser)
	verticaColumnCheckOperator := vert.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeVerticaQuery][scheduler.TaskInstanceTypeColumnCheck] = verticaColumnCheckOperator
	executors[pipeline.AssetTypeVerticaQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeVerticaSeed, verticaColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeVerticaQuerySensor)
	executors[pipeline.AssetTypeVerticaQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeVerticaTableSensor)
	executors[pipeline.AssetTypeVerticaTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypePostgresQuerySensor)
	executors[pipeline.AssetTypePostgresQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypePostgresTableSensor)
	executors[pipeline.AssetTypePostgresTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeRedshiftQuerySensor)
	executors[pipeline.AssetTypeRedshiftQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeRedshiftTableSensor)
	executors[pipeline.AssetTypeRedshiftTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeDuckDBQuerySensor)
	executors[pipeline.AssetTypeDuckDBQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeS3KeySensor)
	executors[pipeline.AssetTypeS3KeySensor][scheduler.TaskInstanceTypeMain] = s3.NewKeySensor(manager, "once")
	ensureExecutorConfig(pipeline.AssetTypeOracleQuery)
	executors[pipeline.AssetTypeOracleQuery][scheduler.TaskInstanceTypeMain] = directOracleBasicOperator{connection: manager, extractor: wholeFileExtractor}
	executors[pipeline.AssetTypeOracleQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	ensureExecutorConfig(pipeline.AssetTypePython)
	executors[pipeline.AssetTypePython][scheduler.TaskInstanceTypeMain] = bruinpython.NewLocalOperator(manager, directPythonEnvVariables(pl))
	ingestrOperator, err := bruiningestr.NewBasicOperator(manager, renderer)
	if err != nil {
		return nil, err
	}
	ensureExecutorConfig(pipeline.AssetTypeIngestr)
	executors[pipeline.AssetTypeIngestr][scheduler.TaskInstanceTypeMain] = ingestrOperator
	return executors, nil
}

func directPythonEnvVariables(pl *pipeline.Pipeline) map[string]string {
	if pl == nil {
		return map[string]string{}
	}
	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)
	startDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC)
	endDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 59, 59, 0, time.UTC)
	return jinja.PythonEnvVariables(&startDate, &endDate, &now, pl.Name, "renart-run", false, "")
}

type directOracleBasicOperator struct {
	connection config.ConnectionGetter
	extractor  query.QueryExtractor
}

type directUnsupportedOperator struct {
	assetType pipeline.AssetType
}

func (o directUnsupportedOperator) Run(_ context.Context, _ scheduler.TaskInstance) error {
	return fmt.Errorf("direct execution is not implemented for asset type %q", o.assetType)
}

func (o directOracleBasicOperator) Run(ctx context.Context, ti scheduler.TaskInstance) error {
	return o.RunTask(ctx, ti.GetPipeline(), ti.GetAsset())
}

func (o directOracleBasicOperator) RunTask(ctx context.Context, p *pipeline.Pipeline, asset *pipeline.Asset) error {
	if asset.Materialization.Type != pipeline.MaterializationTypeNone {
		return fmt.Errorf("direct oracle execution only supports assets without materialization")
	}

	extractor, err := o.extractor.CloneForAsset(ctx, p, asset)
	if err != nil {
		return fmt.Errorf("failed to clone extractor for asset %s: %w", asset.Name, err)
	}
	queries, err := extractor.ExtractQueriesFromString(asset.ExecutableFile.Content)
	if err != nil {
		return fmt.Errorf("cannot extract queries from the task file: %w", err)
	}
	if len(queries) == 0 {
		return nil
	}

	connName, err := p.GetConnectionNameForAsset(asset)
	if err != nil {
		return err
	}
	rawConn := o.connection.GetConnection(connName)
	if rawConn == nil {
		return config.NewConnectionNotFoundError(ctx, "", connName)
	}
	conn, ok := rawConn.(interface {
		RunQueryWithoutResult(context.Context, *query.Query) error
	})
	if !ok {
		return fmt.Errorf("connection %q cannot run oracle queries", connName)
	}

	for _, queryToRun := range queries {
		ansisql.LogQueryIfVerbose(ctx, ctx.Value(bruinexecutor.KeyPrinter), queryToRun.Query)
		if err := conn.RunQueryWithoutResult(ctx, queryToRun); err != nil {
			return err
		}
	}
	return nil
}

func addDirectLimitToQuery(queryStr string, limit int64, conn interface{}, parser *sqlparser.SQLParser, dialect string) string {
	if parser != nil {
		isSingleSelect, err := parser.IsSingleSelectQuery(queryStr, dialect)
		if err == nil && !isSingleSelect {
			return queryStr
		}
	}

	if parser != nil {
		limitedQuery, err := parser.AddLimit(queryStr, int(limit), dialect)
		if err == nil {
			return limitedQuery
		}
	}

	if limiter, ok := conn.(interface{ Limit(string, int64) string }); ok {
		return limiter.Limit(queryStr, limit)
	}

	queryStr = strings.TrimRight(queryStr, "; \n\t")
	return fmt.Sprintf("SELECT * FROM (\n%s\n) as t LIMIT %d", queryStr, limit)
}

func applyDirectSchemaPrefix(_ context.Context, queryStr, dialect string, parser *sqlparser.SQLParser, pp *directPipelineInfo, conn interface{}) (string, error) {
	if dialect == "" || pp.Config.SelectedEnvironment == nil || pp.Config.SelectedEnvironment.SchemaPrefix == "" {
		return queryStr, nil
	}

	usedTables, err := parser.UsedTables(queryStr, dialect)
	if err != nil {
		return queryStr, nil
	}
	if len(usedTables) == 0 {
		return queryStr, nil
	}

	_ = conn
	renameMapping := map[string]string{}
	for _, tableReference := range usedTables {
		parts := strings.Split(tableReference, ".")
		if len(parts) != 2 {
			continue
		}
		schemaName := parts[0]
		tableName := parts[1]
		renameMapping[tableReference] = fmt.Sprintf("%s%s.%s", pp.Config.SelectedEnvironment.SchemaPrefix, schemaName, tableName)
	}
	if len(renameMapping) == 0 {
		return queryStr, nil
	}

	rewrittenQuery, err := parser.RenameTables(queryStr, dialect, renameMapping)
	if err != nil {
		return "", fmt.Errorf("failed to rewrite query with schema prefix: %w", err)
	}
	return rewrittenQuery, nil
}

func updateDirectAssetDependencies(ctx context.Context, asset *pipeline.Asset, p *pipeline.Pipeline, sp *sqlparser.SQLParser, renderer *jinja.Renderer, fs afero.Fs) error {
	if asset == nil || p == nil {
		return fmt.Errorf("pipeline and asset are required to update direct asset dependencies")
	}

	assetRenderer, err := renderer.CloneForAsset(ctx, p, asset)
	if err != nil {
		return fmt.Errorf("failed to create renderer for asset '%s': %w", asset.Name, err)
	}
	missingDeps, err := sp.GetMissingDependenciesForAsset(asset, p, assetRenderer)
	if err != nil {
		return fmt.Errorf("failed to get missing dependencies for asset '%s': %w", asset.Name, err)
	}
	if len(missingDeps) == 0 {
		return nil
	}
	for _, dep := range missingDeps {
		foundMissingUpstream := p.GetAssetByNameCaseInsensitive(dep)
		if foundMissingUpstream == nil || foundMissingUpstream.Name == asset.Name {
			continue
		}
		asset.AddUpstream(foundMissingUpstream)
	}
	return asset.Persist(fs)
}

func fillDirectColumnsFromDB(ctx context.Context, pp *directPipelineInfo, fs afero.Fs, environment string, manager config.ConnectionGetter) (string, error) {
	if pp == nil || pp.Pipeline == nil || pp.Asset == nil {
		return fillStatusFailed, fmt.Errorf("pipeline and asset are required to fill columns from DB")
	}

	var conn interface{}
	var err error
	if manager != nil {
		connName, err := pp.Pipeline.GetConnectionNameForAsset(pp.Asset)
		if err != nil {
			return fillStatusFailed, err
		}
		conn = manager.GetConnection(connName)
		if conn == nil {
			return fillStatusFailed, fmt.Errorf("failed to get connection for asset '%s'", pp.Asset.Name)
		}
	} else {
		selectedConfig, err := selectConfigEnvironment(pp.Config, environment)
		if err != nil {
			return fillStatusFailed, err
		}
		connectionManager, err := newConnectionManagerFromConfig(ctx, selectedConfig)
		if err != nil {
			return fillStatusFailed, err
		}
		connName, err := pp.Pipeline.GetConnectionNameForAsset(pp.Asset)
		if err != nil {
			return fillStatusFailed, err
		}
		conn = connectionManager.GetConnection(connName)
		if conn == nil {
			return fillStatusFailed, fmt.Errorf("failed to get connection for asset '%s'", pp.Asset.Name)
		}
	}

	querier, ok := conn.(interface {
		SelectWithSchema(context.Context, *query.Query) (*query.QueryResult, error)
	})
	if !ok {
		return fillStatusFailed, fmt.Errorf("connection for asset '%s' does not support schema introspection", pp.Asset.Name)
	}

	tableName := pp.Asset.Name
	if _, ok := conn.(*postgres.Client); ok {
		tableName = postgres.QuoteIdentifier(tableName)
	}
	queryStr := fmt.Sprintf("SELECT * FROM %s WHERE 1=0 LIMIT 0", tableName)
	if _, ok := conn.(*mssql.DB); ok {
		queryStr = "SELECT TOP 0 * FROM " + tableName
	}
	if _, ok := conn.(*oracle.Client); ok {
		queryStr = "SELECT * FROM " + tableName + " WHERE 1=0"
	}
	result, err := querier.SelectWithSchema(ctx, &query.Query{Query: queryStr})
	if err != nil {
		return fillStatusFailed, err
	}
	if len(result.Columns) == 0 {
		return fillStatusFailed, fmt.Errorf("no columns found for asset '%s'", pp.Asset.Name)
	}

	skipColumns := map[string]bool{"_is_current": true, "_valid_until": true, "_valid_from": true}
	existingColumns := make(map[string]pipeline.Column)
	for _, col := range pp.Asset.Columns {
		existingColumns[strings.ToLower(col.Name)] = col
	}
	if len(existingColumns) == 0 {
		columns := make([]pipeline.Column, 0, len(result.Columns))
		for i, colName := range result.Columns {
			if skipColumns[colName] {
				continue
			}
			columns = append(columns, pipeline.Column{Name: colName, Type: result.ColumnTypes[i], Checks: []pipeline.ColumnCheck{}, Upstreams: []*pipeline.UpstreamColumn{}})
		}
		pp.Asset.Columns = columns
		if err := pp.Asset.Persist(fs, pp.Pipeline); err != nil {
			return fillStatusFailed, err
		}
		return fillStatusUpdated, nil
	}

	hasChanges := false
	for i, colName := range result.Columns {
		if skipColumns[colName] {
			continue
		}
		lowerColName := strings.ToLower(colName)
		if existingCol, exists := existingColumns[lowerColName]; exists {
			if existingCol.Type != result.ColumnTypes[i] {
				for j := range pp.Asset.Columns {
					if strings.ToLower(pp.Asset.Columns[j].Name) == lowerColName {
						pp.Asset.Columns[j].Type = result.ColumnTypes[i]
						hasChanges = true
						break
					}
				}
			}
		} else {
			pp.Asset.Columns = append(pp.Asset.Columns, pipeline.Column{Name: colName, Type: result.ColumnTypes[i], Checks: []pipeline.ColumnCheck{}, Upstreams: []*pipeline.UpstreamColumn{}})
			hasChanges = true
		}
	}
	if !hasChanges {
		return fillStatusSkipped, nil
	}
	if err := pp.Asset.Persist(fs, pp.Pipeline); err != nil {
		return fillStatusFailed, err
	}
	return fillStatusUpdated, nil
}

func (e *HybridBruinExecutor) FormatAsset(ctx context.Context, req FormatAssetRequest) ([]byte, error) {
	_ = ctx
	assetPath := resolveDirectPath(e.workspaceRoot, req.AssetPath)
	osFS := afero.NewOsFs()
	builder := pipeline.NewBuilder(
		BuilderConfig,
		pipeline.CreateTaskFromYamlDefinition(osFS),
		pipeline.CreateTaskFromFileComments(osFS),
		osFS,
		DefaultGlossaryReader,
	)
	asset, err := builder.CreateAssetFromFile(assetPath, nil)
	if err != nil {
		return nil, err
	}
	if asset == nil {
		return nil, fmt.Errorf("no valid asset found in the file")
	}
	if err := asset.Persist(afero.NewOsFs()); err != nil {
		return nil, err
	}
	return []byte(""), nil
}

func (e *HybridBruinExecutor) ApplyPatch(ctx context.Context, req PatchRequest) ([]byte, error) {
	switch req.Operation {
	case "fill-asset-dependencies":
		return e.applyFillAssetDependencies(ctx, req.TargetPath)
	case "fill-columns-from-db":
		return e.applyFillColumnsFromDB(ctx, req.TargetPath)
	default:
		return nil, fmt.Errorf("direct patch operation %q is not supported", req.Operation)
	}
}

func (e *HybridBruinExecutor) ImportDatabase(ctx context.Context, req ImportDatabaseRequest) ([]byte, error) {
	if e.newConnectionManager == nil || e.newPipelineBuilder == nil {
		return nil, fmt.Errorf("direct database import requires a connection manager and pipeline builder")
	}

	manager, err := e.newConnectionManager(ctx, req.Environment)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection manager: %w", err)
	}

	conn := manager.GetConnection(req.ConnectionName)
	if conn == nil {
		return nil, fmt.Errorf("connection %q not found", req.ConnectionName)
	}

	pipelinePath := resolveDirectPath(e.workspaceRoot, resolveDirectPipelinePath(req.PipelinePath))
	foundPipeline, err := e.newPipelineBuilder().CreatePipelineFromPath(ctx, pipelinePath, pipeline.WithMutate())
	if err != nil {
		return nil, fmt.Errorf("failed to get pipeline from path: %w", err)
	}

	var summary *ansisql.DBDatabase
	schemaList := append([]string{}, req.Schemas...)
	if strings.TrimSpace(req.Schema) != "" {
		schemaList = []string{req.Schema}
	}

	if len(schemaList) > 0 {
		if schemaSummarizer, ok := conn.(interface {
			GetDatabaseSummaryForSchemas(context.Context, []string) (*ansisql.DBDatabase, error)
		}); ok {
			summary, err = schemaSummarizer.GetDatabaseSummaryForSchemas(ctx, schemaList)
			if err != nil {
				return nil, fmt.Errorf("failed to retrieve database summary for specified schemas: %w", err)
			}
		}
	}

	if summary == nil {
		summarizer, ok := conn.(interface {
			GetDatabaseSummary(context.Context) (*ansisql.DBDatabase, error)
		})
		if !ok {
			return nil, fmt.Errorf("connection %q does not support database summary", req.ConnectionName)
		}
		summary, err = summarizer.GetDatabaseSummary(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve database summary: %w", err)
		}
	}

	existingAssets := make(map[string]*pipeline.Asset, len(foundPipeline.Assets))
	for _, asset := range foundPipeline.Assets {
		if asset == nil {
			continue
		}
		existingAssets[strings.ToLower(asset.Name)] = asset
	}

	assetsPath := filepath.Join(pipelinePath, "assets")
	assetType, ok := sourceAssetTypeForConnectionType(manager.GetConnectionType(req.ConnectionName))
	if !ok {
		assetType = pipeline.AssetTypeEmpty
	}
	selectedTables := make(map[string]bool, len(req.Tables))
	for _, tableName := range req.Tables {
		trimmed := strings.ToLower(strings.TrimSpace(tableName))
		if trimmed != "" {
			selectedTables[trimmed] = true
		}
	}

	fs := afero.NewOsFs()
	totalTables := 0
	mergedTableCount := 0
	warnings := make([]directImportWarning, 0)

	for _, schemaObj := range summary.Schemas {
		if req.Schema != "" && !strings.EqualFold(schemaObj.Name, req.Schema) {
			continue
		}
		for _, table := range schemaObj.Tables {
			fullName := fmt.Sprintf("%s.%s", schemaObj.Name, table.Name)
			if len(selectedTables) > 0 && !matchesDirectImportedTable(selectedTables, summary.Name, schemaObj.Name, table.Name) {
				continue
			}

			createdAsset, warning := createDirectImportedAsset(ctx, assetsPath, schemaObj.Name, table.Name, assetType, conn, !req.DisableColumns, table)
			if warning != "" {
				warnings = append(warnings, directImportWarning{Table: fullName, Warning: warning})
			}
			if createdAsset == nil {
				continue
			}

			assetName := fmt.Sprintf("%s.%s", strings.ToLower(schemaObj.Name), strings.ToLower(table.Name))
			if existingAssets[assetName] == nil {
				schemaFolder := filepath.Join(assetsPath, strings.ToLower(schemaObj.Name))
				if err := fs.MkdirAll(schemaFolder, 0o755); err != nil {
					return nil, fmt.Errorf("failed to create schema directory %s: %w", schemaFolder, err)
				}
				if err := createdAsset.Persist(fs); err != nil {
					return nil, err
				}
				existingAssets[assetName] = createdAsset
				totalTables++
			} else {
				existingAsset := existingAssets[assetName]
				existingColumns := make(map[string]pipeline.Column, len(existingAsset.Columns))
				for _, column := range existingAsset.Columns {
					existingColumns[column.Name] = column
				}
				for _, column := range createdAsset.Columns {
					if _, ok := existingColumns[column.Name]; !ok {
						existingAsset.Columns = append(existingAsset.Columns, column)
					}
				}
				if err := existingAsset.Persist(fs); err != nil {
					return nil, err
				}
				mergedTableCount++
			}
		}
	}

	response := directImportDatabaseResponse{
		Status:         "ok",
		ImportedTables: totalTables,
		MergedTables:   mergedTableCount,
		Database:       summary.Name,
		PipelinePath:   pipelinePath,
		Warnings:       warnings,
	}
	return json.Marshal(response)
}

func (e *HybridBruinExecutor) RunWithRetry(
	ctx context.Context,
	req QueryAssetRequest,
	retries int,
	initialDelay time.Duration,
) ([]byte, error, int) {
	attempt := 0
	delay := initialDelay
	for {
		attempt++
		output, err := e.QueryAsset(ctx, req)
		if err == nil {
			return output, nil, attempt
		}
		if !IsDuckDBLockError(err, output) || attempt > retries {
			return output, err, attempt
		}
		select {
		case <-ctx.Done():
			return output, ctx.Err(), attempt
		case <-time.After(delay):
		}
		delay *= 2
	}
}
