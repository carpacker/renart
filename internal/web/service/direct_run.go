package service

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	bruinexecutor "github.com/bruin-data/bruin/pkg/executor"
	"github.com/bruin-data/bruin/pkg/git"
	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/scheduler"
	"github.com/bruin-data/bruin/pkg/sqlparser"
	"github.com/spf13/afero"
	"go.uber.org/zap"

	"renart/internal/web/runstate"
)

func (e *HybridBruinExecutor) RunAsset(ctx context.Context, req RunAssetRequest, onChunk func([]byte)) ([]byte, error) {
	if e.newPipelineBuilder == nil {
		return nil, fmt.Errorf("direct run requires a pipeline builder")
	}

	pp, err := getDirectPipelineAndAsset(ctx, e.workspaceRoot, req.AssetPath, afero.NewOsFs())
	if err != nil {
		return nil, err
	}
	printer := &streamCaptureWriter{buffer: bytes.NewBuffer(nil), onChunk: onChunk}
	writeDirectRunAnalysis(printer, pp.Pipeline, pp.Asset)
	if err := validateDirectRunDependencies(ctx, printer, pp.Pipeline, e.workspaceRoot); err != nil {
		return printer.buffer.Bytes(), err
	}

	if shouldFallbackToCLIRunAsset(pp.Asset, pp.Pipeline) {
		return printer.buffer.Bytes(), fmt.Errorf("direct run is not supported for asset type %q", pp.Asset.Type)
	}
	if _, err := selectConfigEnvironment(pp.Config, req.Environment); err != nil {
		return printer.buffer.Bytes(), fmt.Errorf("failed to use the environment '%s': %w", req.Environment, err)
	}
	applySelectedEnvironmentRefreshRestriction(pp.Config, pp.Pipeline.Assets)
	manager, err := e.directConnectionManager(ctx, pp.Config)
	if err != nil {
		return printer.buffer.Bytes(), err
	}

	runID := newRenartRunID()
	timeWindow, err := ResolveExecutionTimeWindow(string(pp.Pipeline.Schedule), req.StartDate, req.EndDate, time.Now().UTC())
	if err != nil {
		return printer.buffer.Bytes(), err
	}
	runCtx, parser, cleanup, err := buildDirectRunAssetContext(ctx, pp, timeWindow, runID)
	if err != nil {
		return printer.buffer.Bytes(), err
	}
	defer cleanup()
	runCtx = context.WithValue(runCtx, pipeline.RunConfigFullRefresh, req.FullRefresh)

	renderer, err := buildDirectRunAssetRenderer(pp, timeWindow, runID)
	if err != nil {
		return printer.buffer.Bytes(), err
	}

	mainExecutors := map[pipeline.AssetType]bruinexecutor.Config{}
	if !isAPIAsset(pp.Asset) && !isLoadAsset(pp.Asset) {
		mainExecutors, err = buildDirectMainExecutors(manager, renderer, parser, pp.Pipeline, e.runRegistry, e.duckDBCoordinator, e.workspaceRoot, req.FullRefresh, effectiveSensorMode(req.SensorMode, false))
		if err != nil {
			return printer.buffer.Bytes(), err
		}
	}

	s := scheduler.NewScheduler(zap.NewNop().Sugar(), pp.Pipeline, runID)
	s.MarkAll(scheduler.Skipped)
	if !s.MarkAsset(pp.Asset, scheduler.Pending, false) {
		return printer.buffer.Bytes(), fmt.Errorf("asset '%s' was not found among the pipeline's scheduled task instances", pp.Asset.Name)
	}

	pending := s.GetTaskInstancesByStatus(scheduler.Pending)
	if len(pending) == 0 {
		return printer.buffer.Bytes(), nil
	}

	var regRun *runstate.Run
	if e.runRegistry != nil {
		regRun = e.runRegistry.BeginRun(runID, pp.Config.SelectedEnvironmentName, []string{pp.Asset.Name})
		defer regRun.End()
	}

	formatting := directRunFormatting{}
	if startDate, ok := runCtx.Value(pipeline.RunConfigStartDate).(time.Time); ok {
		formatting.startDate = startDate
	}
	if endDate, ok := runCtx.Value(pipeline.RunConfigEndDate).(time.Time); ok {
		formatting.endDate = endDate
	}
	writeDirectRunWindow(printer, formatting)
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
			taskStartedAt := time.Now()
			emitDirectRunAssetEvent(req.AssetEvent, instance, "running", taskStartedAt, time.Time{}, nil)
			writeDirectRunLifecycle(printer, instance, nil, true, 0)
			finishTask := beginRegistryTask(regRun, instance)
			runErr := e.runDirectTask(runCtx, pp.Pipeline, instance, renderer, manager, &seq, printer)
			if runErr != nil {
				finishTask(runErr)
				instance.MarkAs(scheduler.Failed)
				results = append(results, &scheduler.TaskExecutionResult{Instance: instance, Error: runErr})
				emitDirectRunAssetEvent(req.AssetEvent, instance, "failed", taskStartedAt, time.Now(), runErr)
				writeDirectRunLifecycle(printer, instance, runErr, false, time.Since(taskStartedAt))
				writeDirectRunSummary(printer, buildDirectRunSummary(results, time.Since(startedAt)))
				_ = e.saveDirectRunLog(ctx, pp.Pipeline, s, runID, req.Environment, timeWindow, []string{"renart", "run", req.AssetPath})
				return printer.buffer.Bytes(), runErr
			}
			finishTask(nil)
			instance.MarkAs(scheduler.Succeeded)
			results = append(results, &scheduler.TaskExecutionResult{Instance: instance, Error: nil})
			emitDirectRunAssetEvent(req.AssetEvent, instance, "success", taskStartedAt, time.Now(), nil)
			writeDirectRunLifecycle(printer, instance, nil, false, time.Since(taskStartedAt))
		}

		if !progressed {
			writeDirectRunSummary(printer, buildDirectRunSummary(results, time.Since(startedAt)))
			return printer.buffer.Bytes(), fmt.Errorf("direct run stalled: no runnable task instances remained")
		}
	}

	writeDirectRunSummary(printer, buildDirectRunSummary(results, time.Since(startedAt)))
	_ = e.saveDirectRunLog(ctx, pp.Pipeline, s, runID, req.Environment, timeWindow, []string{"renart", "run", req.AssetPath})
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
	configPath := strings.TrimSpace(req.ConfigPath)
	if configPath == "" {
		repoRoot, repoErr := git.FindRepoFromPath(resolvedTarget)
		if repoErr != nil {
			return nil, fmt.Errorf("failed to find the git repository root: %w", repoErr)
		}
		configPath = filepath.Join(repoRoot.Path, ".bruin.yml")
	}
	cfg, err := loadSelectedConfig(configPath, req.Environment)
	if err != nil {
		return nil, err
	}
	if req.DryRun {
		manager, err := e.directConnectionManager(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return e.dryRunPipeline(ctx, foundPipeline, cfg, manager, onChunk)
	}

	printer := &streamCaptureWriter{buffer: bytes.NewBuffer(nil), onChunk: onChunk}
	writeDirectRunAnalysis(printer, foundPipeline, nil)
	if err := validateDirectRunDependencies(ctx, printer, foundPipeline, e.workspaceRoot); err != nil {
		return printer.buffer.Bytes(), err
	}
	if shouldFallbackToCLIRunPipeline(foundPipeline) {
		return printer.buffer.Bytes(), fmt.Errorf("direct pipeline run is not supported for one or more asset types")
	}
	applySelectedEnvironmentRefreshRestriction(cfg, foundPipeline.Assets)
	manager, err := e.directConnectionManager(ctx, cfg)
	if err != nil {
		return printer.buffer.Bytes(), err
	}

	pp := &directPipelineInfo{Pipeline: foundPipeline, Config: cfg}
	runID := newRenartRunID()
	timeWindow, err := ResolveExecutionTimeWindow(string(foundPipeline.Schedule), req.StartDate, req.EndDate, time.Now().UTC())
	if err != nil {
		return printer.buffer.Bytes(), err
	}
	runCtx, parser, cleanup, err := buildDirectRunAssetContext(ctx, pp, timeWindow, runID)
	if err != nil {
		return printer.buffer.Bytes(), err
	}
	defer cleanup()
	runCtx = context.WithValue(runCtx, pipeline.RunConfigFullRefresh, req.FullRefresh)
	renderer, err := buildDirectRunAssetRenderer(pp, timeWindow, runID)
	if err != nil {
		return printer.buffer.Bytes(), err
	}
	mainExecutors, err := buildDirectMainExecutors(manager, renderer, parser, foundPipeline, e.runRegistry, e.duckDBCoordinator, e.workspaceRoot, req.FullRefresh, effectiveSensorMode(req.SensorMode, false))
	if err != nil {
		return printer.buffer.Bytes(), err
	}

	formatting := directRunFormatting{}
	if startDate, ok := runCtx.Value(pipeline.RunConfigStartDate).(time.Time); ok {
		formatting.startDate = startDate
	}
	if endDate, ok := runCtx.Value(pipeline.RunConfigEndDate).(time.Time); ok {
		formatting.endDate = endDate
	}
	writeDirectRunWindow(printer, formatting)
	runCtx = context.WithValue(runCtx, bruinexecutor.ContextLogger, zap.NewNop().Sugar())

	seq := bruinexecutor.Sequential{TaskTypeMap: mainExecutors}
	s := scheduler.NewScheduler(zap.NewNop().Sugar(), foundPipeline, runID)
	s.MarkAll(scheduler.Pending)
	results := make([]*scheduler.TaskExecutionResult, 0)
	startedAt := time.Now()

	var regRun *runstate.Run
	if e.runRegistry != nil {
		planned := make([]string, 0, len(foundPipeline.Assets))
		for _, asset := range foundPipeline.Assets {
			if asset != nil {
				planned = append(planned, asset.Name)
			}
		}
		regRun = e.runRegistry.BeginRun(runID, cfg.SelectedEnvironmentName, planned)
		defer regRun.End()
	}

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
			taskStartedAt := time.Now()
			emitDirectRunAssetEvent(req.AssetEvent, instance, "running", taskStartedAt, time.Time{}, nil)
			writeDirectRunLifecycle(printer, instance, nil, true, 0)
			finishTask := beginRegistryTask(regRun, instance)
			runErr := e.runDirectTask(runCtx, foundPipeline, instance, renderer, manager, &seq, printer)
			finishTask(runErr)
			if runErr != nil {
				instance.MarkAs(scheduler.Failed)
				results = append(results, &scheduler.TaskExecutionResult{Instance: instance, Error: runErr})
				emitDirectRunAssetEvent(req.AssetEvent, instance, "failed", taskStartedAt, time.Now(), runErr)
				writeDirectRunLifecycle(printer, instance, runErr, false, time.Since(taskStartedAt))
				writeDirectRunSummary(printer, buildDirectRunSummary(results, time.Since(startedAt)))
				_ = e.saveDirectRunLog(ctx, foundPipeline, s, runID, req.Environment, timeWindow, []string{"renart", "run", req.Target})
				return printer.buffer.Bytes(), runErr
			}
			instance.MarkAs(scheduler.Succeeded)
			results = append(results, &scheduler.TaskExecutionResult{Instance: instance, Error: nil})
			emitDirectRunAssetEvent(req.AssetEvent, instance, "success", taskStartedAt, time.Now(), nil)
			writeDirectRunLifecycle(printer, instance, nil, false, time.Since(taskStartedAt))
		}

		if !progressed {
			writeDirectRunSummary(printer, buildDirectRunSummary(results, time.Since(startedAt)))
			_ = e.saveDirectRunLog(ctx, foundPipeline, s, runID, req.Environment, timeWindow, []string{"renart", "run", req.Target})
			return printer.buffer.Bytes(), fmt.Errorf("direct run stalled: no runnable task instances remained")
		}
	}

	writeDirectRunSummary(printer, buildDirectRunSummary(results, time.Since(startedAt)))
	_ = e.saveDirectRunLog(ctx, foundPipeline, s, runID, req.Environment, timeWindow, []string{"renart", "run", req.Target})
	return printer.buffer.Bytes(), nil
}

func (e *HybridBruinExecutor) runDirectTask(
	runCtx context.Context,
	pl *pipeline.Pipeline,
	instance scheduler.TaskInstance,
	renderer *jinja.Renderer,
	manager config.ConnectionAndDetailsGetter,
	seq *bruinexecutor.Sequential,
	printer *streamCaptureWriter,
) error {
	asset := instance.GetAsset()
	assetWriter := newDirectAssetLogWriter(printer, pl, asset)
	taskCtx := context.WithValue(runCtx, bruinexecutor.KeyPrinter, assetWriter)

	var callbackErr error
	forward := func(chunk []byte) {
		if callbackErr != nil {
			return
		}
		_, callbackErr = assetWriter.Write(chunk)
	}

	var runErr error
	switch {
	case isAPIAsset(asset):
		_, runErr = e.runAPIAsset(taskCtx, pl, asset, renderer, manager, forward)
	case isLoadAsset(asset):
		_, runErr = e.runLoadAsset(taskCtx, pl, asset, manager, forward)
	default:
		lease, leaseErr := e.acquireDuckDBConnections(
			taskCtx,
			manager,
			duckDBConnectionNamesForAsset(pl, asset),
			directTaskLeaseOwner(taskCtx, pl, asset),
			assetWriter,
		)
		if leaseErr != nil {
			runErr = leaseErr
		} else {
			runErr = seq.RunSingleTask(taskCtx, instance)
			lease.Release()
		}
	}

	if runErr == nil && callbackErr != nil {
		runErr = callbackErr
	}
	if flushErr := assetWriter.Flush(); runErr == nil && flushErr != nil {
		runErr = flushErr
	}
	return runErr
}

func (e *HybridBruinExecutor) saveDirectRunLog(ctx context.Context, foundPipeline *pipeline.Pipeline, s *scheduler.Scheduler, runID, environment string, timeWindow ExecutionTimeWindow, cmdline []string) error {
	runConfig := &scheduler.RunConfig{
		Environment: environment,
		Output:      "plain",
		StartDate:   timeWindow.StartRFC3339(),
		EndDate:     timeWindow.EndRFC3339(),
	}
	return e.executionLogSink().SaveRunLog(ctx, RunLogRecord{
		Pipeline:  foundPipeline,
		Scheduler: s,
		RunConfig: runConfig,
		RunID:     runID,
		Cmdline:   cmdline,
	})
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

func emitDirectRunAssetEvent(onEvent func(ExecutionAssetEvent), instance scheduler.TaskInstance, status string, startedAt, finishedAt time.Time, err error) {
	if onEvent == nil || instance == nil || instance.GetAsset() == nil || instance.GetType() != scheduler.TaskInstanceTypeMain {
		return
	}
	event := ExecutionAssetEvent{Asset: instance.GetAsset().Name, Status: status}
	if !startedAt.IsZero() {
		start := startedAt.UTC()
		event.StartedAt = &start
	}
	if !finishedAt.IsZero() {
		finish := finishedAt.UTC()
		event.FinishedAt = &finish
	}
	if err != nil {
		event.Error = err.Error()
	}
	onEvent(event)
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
	assetTypeTrinoSeed:                        {},
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
	pipeline.AssetTypeDorisSeed:               {},
	pipeline.AssetTypeDorisQuerySensor:        {},
	pipeline.AssetTypeDorisTableSensor:        {},
	pipeline.AssetTypeDremioQuerySensor:       {},
	pipeline.AssetTypeAthenaSQLSensor:         {},
	pipeline.AssetTypeAthenaTableSensor:       {},
	pipeline.AssetTypeDuckDBQuerySensor:       {},
	pipeline.AssetTypeSynapseQuerySensor:      {},
	pipeline.AssetTypeSynapseTableSensor:      {},
	pipeline.AssetTypeSnowflakeQuerySensor:    {},
	pipeline.AssetTypeSnowflakeTableSensor:    {},
	pipeline.AssetTypeTrinoQuerySensor:        {},
	pipeline.AssetTypeSailQuerySensor:         {},
	pipeline.AssetTypeVerticaQuerySensor:      {},
	pipeline.AssetTypeVerticaTableSensor:      {},
	pipeline.AssetTypeS3KeySensor:             {},
	pipeline.AssetTypePython:                  {},
	pipeline.AssetTypeIngestr:                 {},
	pipeline.AssetType(loadAssetType):         {},
	pipeline.AssetType(apiAssetType):          {},
}

// beginRegistryTask registers a main task instance as in flight in the run
// registry, returning its finish callback (a no-op for check instances and
// when the registry is absent, so call sites stay unconditional).
func beginRegistryTask(regRun *runstate.Run, instance scheduler.TaskInstance) func(error) {
	if regRun == nil || instance.GetType() != scheduler.TaskInstanceTypeMain || instance.GetAsset() == nil {
		return func(error) {}
	}
	return regRun.BeginTask(instance.GetAsset().Name)
}

func allDirectRunPipelineDependenciesSucceeded(instance scheduler.TaskInstance) bool {
	for _, upstream := range instance.GetUpstream() {
		if upstream.GetStatus() != scheduler.Succeeded && upstream.GetStatus() != scheduler.Skipped {
			return false
		}
	}
	return true
}

func buildDirectRunAssetContext(ctx context.Context, pp *directPipelineInfo, timeWindow ExecutionTimeWindow, runID string) (context.Context, *sqlparser.SQLParser, func(), error) {
	now := time.Now().UTC()
	runCtx := context.WithValue(ctx, pipeline.RunConfigFullRefresh, false)
	runCtx = context.WithValue(runCtx, pipeline.RunConfigStartDate, timeWindow.Start)
	runCtx = context.WithValue(runCtx, pipeline.RunConfigEndDate, timeWindow.End)
	runCtx = context.WithValue(runCtx, pipeline.RunConfigExecutionDate, now)
	runCtx = context.WithValue(runCtx, pipeline.RunConfigRunID, runID)
	runCtx = context.WithValue(runCtx, config.EnvironmentContextKey, pp.Config.SelectedEnvironment)
	runCtx = context.WithValue(runCtx, config.EnvironmentNameContextKey, pp.Config.SelectedEnvironmentName)
	isDebug := false
	runCtx = context.WithValue(runCtx, bruinexecutor.KeyIsDebug, &isDebug)
	runCtx = context.WithValue(runCtx, bruinexecutor.KeyVerbose, false)
	runCtx = context.WithValue(runCtx, config.SecretsBackendContextKey, "")

	if pp.Config.SelectedEnvironment == nil || pp.Config.SelectedEnvironment.SchemaPrefix == "" {
		return runCtx, nil, func() {}, nil
	}

	parser, err := sqlparser.NewSQLParser(false)
	if err != nil {
		return nil, nil, nil, err
	}

	cleanup := func() {
		parser.Close()
	}

	return runCtx, parser, cleanup, nil
}

func buildDirectRunAssetRenderer(pp *directPipelineInfo, timeWindow ExecutionTimeWindow, runID string) (*jinja.Renderer, error) {
	now := time.Now().UTC()
	macroContent, err := jinja.LoadMacros(afero.NewOsFs(), pp.Pipeline.MacrosPath)
	if err != nil {
		return nil, err
	}

	return jinja.NewRendererWithStartEndDatesAndMacros(&timeWindow.Start, &timeWindow.End, &now, pp.Pipeline.Name, runID, nil, macroContent), nil
}
