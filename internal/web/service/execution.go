package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/sqlparser"
	"gopkg.in/yaml.v3"
	"renart/internal/web/bus"
	"renart/internal/web/identity"
	"renart/internal/web/matlog"
	"renart/internal/web/policy"
	"renart/internal/web/runcontext"
)

type InspectResult struct {
	Status                              string
	Columns                             []string
	Rows                                []map[string]any
	RawOutput                           string
	Operation                           OperationMetadata
	Error                               string
	Info                                string
	MissingUpstreamAssetIDs             []string
	MissingUpstreamAssetNames           []string
	MissingUpstreamAssetsMaterializable bool
	Attempts                            int
	Retryable                           bool
	HTTPStatus                          int
}

type MaterializeResult struct {
	Status          string
	Operation       OperationMetadata
	Output          string
	Error           string
	ExitCode        int
	ChangedAssetIDs []string
	MaterializedAt  *time.Time
	Warnings        []string
}

type MaterializeScope string

const (
	MaterializeScopeAsset                 MaterializeScope = "asset"
	MaterializeScopeAssetWithUpstreams    MaterializeScope = "asset_with_upstreams"
	MaterializeScopeAssetWithDownstreams  MaterializeScope = "asset_with_downstreams"
	MaterializeScopeAssetWithNeighborhood MaterializeScope = "asset_with_upstreams_and_downstreams"
)

type ExecutionDependencies struct {
	WorkspaceRoot        string
	ConfigPath           string
	Executor             BruinCommandExecutor
	ResolveAssetByID     func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error)
	ResolveAssetNameByID func(string) string
	FindInspectIDs       func(...string) []string
	CurrentPipelines     func() []PipelineView
	ParseQueryOutput     func([]byte) ([]string, []map[string]any)
	NewPipelineBuilder   func() *pipeline.Builder
	Events               *bus.Bus
	// PolicyFor returns the execution policy for an environment; nil means
	// unrestricted. Enforced here — the run-dispatch chokepoint every
	// execution path goes through — not in UI handlers.
	PolicyFor           func(environment string) policy.EnvironmentPolicy
	SelectedEnvironment func() string
}

type PipelineView struct {
	ID     string
	UUID   string
	Assets []AssetView
}

type AssetView struct {
	ID   string
	Name string
}

type PipelineMaterializationInfo struct {
	AssetName       string
	Connection      string
	IsMaterialized  bool
	MaterializedAs  string
	RowCount        *int64
	DeclaredMatType string
}

type PipelineMaterializationState struct {
	AssetID         string `json:"asset_id"`
	IsMaterialized  bool   `json:"is_materialized"`
	MaterializedAs  string `json:"materialized_as,omitempty"`
	RowCount        *int64 `json:"row_count,omitempty"`
	Connection      string `json:"connection,omitempty"`
	DeclaredMatType string `json:"materialization_type,omitempty"`
}

type PipelineMaterializationResponse struct {
	PipelineID string                         `json:"pipeline_id"`
	Assets     []PipelineMaterializationState `json:"assets"`
}

type ExecutionService struct {
	deps ExecutionDependencies
}

const inspectReadOnlyErrorMessage = "Inspect only supports read-only single SELECT queries. Materialize the asset to run write, delete, copy, or multi-statement SQL."

func NewExecutionService(deps ExecutionDependencies) *ExecutionService {
	return &ExecutionService{deps: deps}
}

// emitRunCompleted publishes the run-completion event on the process bus.
// This is the single seam Phase 2 (materialization facts) and Phase 3
// (staleness) attach to for run observation.
func (s *ExecutionService) emitRunCompleted(runID, pipelineUUID, environment string, window ExecutionTimeWindow, completedAt time.Time, assets []bus.AssetRun) {
	s.emitRunCompletedForSpec(PipelineRunSpec{RunID: runID, Environment: environment}, pipelineUUID, window, completedAt, assets)
}

func (s *ExecutionService) emitRunCompletedForSpec(spec PipelineRunSpec, pipelineUUID string, window ExecutionTimeWindow, completedAt time.Time, assets []bus.AssetRun) {
	if s.deps.Events == nil || pipelineUUID == "" || len(assets) == 0 {
		return
	}
	event := bus.RunCompleted{
		RunID:             spec.RunID,
		PipelineUUID:      pipelineUUID,
		Environment:       spec.Environment,
		FullRefresh:       spec.FullRefresh,
		CompletedAt:       completedAt,
		Assets:            assets,
		SnapshotVersionID: spec.SnapshotVersionID,
		SnapshotDir:       spec.SnapshotDir,
	}
	if !window.IsZero() {
		start := window.Start
		end := window.End
		event.WinStart = &start
		event.WinEnd = &end
	}
	s.deps.Events.EmitRunCompleted(event)
}

// checkRunPolicy evaluates the environment policy at the run-dispatch
// chokepoint.
func (s *ExecutionService) checkRunPolicy(request policy.RunRequest) error {
	if s.deps.PolicyFor == nil {
		return nil
	}
	return policy.Check(s.deps.PolicyFor(request.Environment), request)
}

func (s *ExecutionService) effectiveEnvironment(environment string) string {
	if environment = strings.TrimSpace(environment); environment != "" {
		return environment
	}
	if s.deps.SelectedEnvironment != nil {
		return strings.TrimSpace(s.deps.SelectedEnvironment())
	}
	return ""
}

func (s *ExecutionService) effectiveFullRefresh(ctx context.Context, environment string, requested bool) bool {
	if !requested || strings.TrimSpace(s.deps.ConfigPath) == "" {
		return requested
	}
	cfg, err := loadSelectedConfig(s.deps.ConfigPath, environment)
	if err != nil || !selectedEnvironmentRestrictsFullRefresh(cfg) {
		return requested
	}
	addExecutionWarning(ctx, fmt.Sprintf("Full refresh is restricted for environment %s; running configured materialization strategies instead.", environment))
	return false
}

// findPipelineViewForAsset locates the workspace pipeline containing the
// given encoded asset ID.
func (s *ExecutionService) findPipelineViewForAsset(assetID string) (PipelineView, bool) {
	if s.deps.CurrentPipelines == nil {
		return PipelineView{}, false
	}
	for _, view := range s.deps.CurrentPipelines() {
		for _, asset := range view.Assets {
			if asset.ID == assetID {
				return view, true
			}
		}
	}
	return PipelineView{}, false
}

func (s *ExecutionService) InspectAsset(ctx context.Context, assetID, limit, environment, startDate, endDate string) InspectResult {
	relAssetPath, err := DecodeID(assetID)
	if err != nil {
		return InspectResult{Status: "error", Error: "invalid asset id", HTTPStatus: 400}
	}

	if guardErr := s.ensureAssetInspectable(ctx, assetID, environment, startDate, endDate); guardErr != nil {
		return InspectResult{
			Status:     "error",
			Columns:    []string{},
			Rows:       []map[string]any{},
			RawOutput:  guardErr.Error(),
			Operation:  queryAssetOperation(relAssetPath, limit, environment, ""),
			Error:      guardErr.Error(),
			Attempts:   0,
			Retryable:  false,
			HTTPStatus: 400,
		}
	}

	if result, ok := s.inspectMaterializedNonSQLAsset(ctx, assetID, relAssetPath, limit, environment); ok {
		return result
	}

	queryReq := QueryAssetRequest{
		AssetPath:   relAssetPath,
		Limit:       limit,
		Environment: environment,
		StartDate:   startDate,
		EndDate:     endDate,
		Output:      "json",
	}
	timeWindow, _ := s.resolveAssetExecutionTimeWindow(ctx, assetID, startDate, endDate)
	operation := withOperationTimeWindow(queryAssetOperation(relAssetPath, limit, environment, ""), timeWindow)

	var output []byte
	var attempts int
	run := func() error {
		var runErr error
		output, runErr, attempts = s.deps.Executor.RunWithRetry(ctx, queryReq, 4, 150*time.Millisecond)
		return runErr
	}

	err = run()

	if err != nil {
		statusCode := 400
		errorMessage := err.Error()
		rawOutput := extractInspectRawOutput(output)
		if rawOutput == "" {
			rawOutput = string(output)
		}
		if IsDuckDBLockError(err, output) {
			statusCode = 409
			errorMessage = "duckdb database is busy (lock held by another process), please retry"
		}
		detectionText := rawOutput
		if strings.TrimSpace(detectionText) == "" {
			detectionText = errorMessage
		}
		missingUpstreamIDs, missingUpstreamNames := s.findMissingUpstreamAssets(ctx, assetID, detectionText)
		return InspectResult{
			Status:                              "error",
			Columns:                             []string{},
			Rows:                                []map[string]any{},
			RawOutput:                           rawOutput,
			Operation:                           operation,
			Error:                               errorMessage,
			MissingUpstreamAssetIDs:             missingUpstreamIDs,
			MissingUpstreamAssetNames:           missingUpstreamNames,
			MissingUpstreamAssetsMaterializable: len(missingUpstreamIDs) > 0,
			Attempts:                            attempts,
			Retryable:                           statusCode == 409,
			HTTPStatus:                          statusCode,
		}
	}

	columns, rows := s.deps.ParseQueryOutput(output)
	// Surface the executed (rendered) query so the UI can show what actually ran.
	operation.Query = ExtractQueryTextFromOutput(output)
	return InspectResult{
		Status:     "ok",
		Columns:    columns,
		Rows:       rows,
		RawOutput:  string(output),
		Operation:  operation,
		Attempts:   attempts,
		HTTPStatus: 200,
	}
}

func (s *ExecutionService) inspectMaterializedNonSQLAsset(ctx context.Context, assetID, relAssetPath, limit, environment string) (InspectResult, bool) {
	if s.deps.ResolveAssetByID == nil {
		return InspectResult{}, false
	}

	_, parsedPipeline, asset, err := s.deps.ResolveAssetByID(ctx, assetID)
	if err != nil || parsedPipeline == nil || asset == nil || asset.IsSQLAsset() {
		return InspectResult{}, false
	}

	rowLimit := normalizeInspectLimit(limit)
	if isSensorAssetType(asset.Type) {
		return InspectResult{
			Status:     "info",
			Columns:    []string{},
			Rows:       []map[string]any{},
			Operation:  queryAssetOperation(relAssetPath, limit, environment, ""),
			Info:       "Sensors do not materialize previewable data. Run the sensor to check its condition now.",
			HTTPStatus: 200,
		}, true
	}

	// A non-SQL asset (python, api, load) is inspected by previewing the table it
	// materializes into. For load assets the destination is a flat parameter; for
	// python/api it's the asset's own connection + name.
	var connectionName, tableName string
	if isLoadAsset(asset) {
		params, paramsErr := resolvedLoadParams(asset, parsedPipeline)
		if paramsErr != nil {
			return InspectResult{}, false
		}
		if isLocalLoadConnection(params.DestinationConnection) || strings.TrimSpace(params.DestinationObject) != "" {
			// A load asset that writes to a file/object has no queryable table —
			// surface an informational note rather than an error.
			return InspectResult{
				Status:     "info",
				Columns:    []string{},
				Rows:       []map[string]any{},
				Operation:  queryAssetOperation(relAssetPath, limit, environment, ""),
				Info:       fmt.Sprintf("This load asset writes to %s, which can't be previewed as a database table.", strings.TrimSpace(params.DestinationObject)),
				HTTPStatus: 200,
			}, true
		}
		if strings.TrimSpace(params.DestinationConnection) == "" {
			return InspectResult{}, false
		}
		connectionName = params.DestinationConnection
		tableName = asset.Name
	} else {
		connectionName, err = targetConnectionNameForAsset(asset, parsedPipeline)
		if err != nil || strings.TrimSpace(connectionName) == "" {
			return InspectResult{}, false
		}
		tableName = asset.Name
	}

	query := fmt.Sprintf("select * from %s limit %d", tableName, rowLimit)
	operation := queryConnectionOperation(connectionName, query, environment)
	operation.AssetPath = relAssetPath
	operation.Target = relAssetPath
	operation.Limit = limit

	columns, rows, err := s.RunConnectionQueryForEnvironment(ctx, connectionName, environment, query)
	if err != nil {
		return InspectResult{
			Status:     "error",
			Columns:    []string{},
			Rows:       []map[string]any{},
			RawOutput:  err.Error(),
			Operation:  operation,
			Error:      fmt.Sprintf("No materialized table found for %s on connection %s. Materialize the asset first, then inspect again.", asset.Name, connectionName),
			Attempts:   0,
			Retryable:  false,
			HTTPStatus: 400,
		}, true
	}

	output, _ := json.Marshal(QueryRowsEnvelope{Columns: columns, Rows: rows})
	return InspectResult{
		Status:     "ok",
		Columns:    columns,
		Rows:       rows,
		RawOutput:  string(output),
		Operation:  operation,
		Attempts:   1,
		HTTPStatus: 200,
	}, true
}

func normalizeInspectLimit(limit string) int {
	trimmed := strings.TrimSpace(limit)
	if trimmed == "" {
		return 100
	}
	var value int
	if _, err := fmt.Sscanf(trimmed, "%d", &value); err != nil || value <= 0 {
		return 100
	}
	if value > 1000 {
		return 1000
	}
	return value
}

func (s *ExecutionService) ensureAssetInspectable(ctx context.Context, assetID, environment, startDate, endDate string) error {
	if s.deps.ResolveAssetByID == nil {
		return nil
	}

	_, parsedPipeline, asset, err := s.deps.ResolveAssetByID(ctx, assetID)
	if err != nil {
		return err
	}
	if asset == nil || parsedPipeline == nil || !asset.IsSQLAsset() {
		return nil
	}

	_, _, queryStr, err := getDirectConnectionAndQuery(ctx, &directPipelineInfo{Pipeline: parsedPipeline, Asset: asset, Config: loadExecutionConfigOrEmpty(s.deps.ConfigPath)}, environment, startDate, endDate)
	if err != nil {
		return nil
	}

	ok, err := isReadOnlySelectQuery(queryStr, asset.Type)
	if err != nil {
		return nil
	}
	if ok {
		return nil
	}

	return fmt.Errorf(inspectReadOnlyErrorMessage)
}

func loadExecutionConfigOrEmpty(configPath string) *config.Config {
	selected, err := selectConfigEnvironment(loadConfigOrEmpty(configPath), "")
	if err != nil {
		return &config.Config{}
	}
	return selected
}

func isReadOnlySelectQuery(queryStr string, assetType pipeline.AssetType) (bool, error) {
	dialect, err := sqlparser.AssetTypeToDialect(assetType)
	if err != nil {
		return false, err
	}

	parser, err := sqlparser.NewSQLParser(false)
	if err != nil {
		return false, err
	}
	defer parser.Close()

	return parser.IsSingleSelectQuery(queryStr, dialect)
}

func extractInspectRawOutput(output []byte) string {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return ""
	}

	var envelope map[string]any
	if err := json.Unmarshal(output, &envelope); err != nil {
		return trimmed
	}

	for _, key := range []string{"raw_output", "error"} {
		if value, ok := envelope[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}

	return trimmed
}

func (s *ExecutionService) MaterializeAssetStream(ctx context.Context, assetID, environment, scope, startDate, endDate string, fullRefresh, backfill bool, confirmedEnvironment string, onChunk func([]byte)) MaterializeResult {
	return s.MaterializeAssetStreamWithSensorMode(ctx, assetID, environment, scope, startDate, endDate, fullRefresh, backfill, confirmedEnvironment, "", onChunk)
}

func (s *ExecutionService) MaterializeAssetStreamWithSensorMode(ctx context.Context, assetID, environment, scope, startDate, endDate string, fullRefresh, backfill bool, confirmedEnvironment, sensorMode string, onChunk func([]byte)) MaterializeResult {
	normalizedContext, contextErr := runcontext.Normalize(runcontext.Input{
		Start:       startDate,
		End:         endDate,
		FullRefresh: fullRefresh,
		Backfill:    backfill,
		SensorMode:  sensorMode,
	})
	if contextErr != nil {
		return MaterializeResult{Status: "error", Error: contextErr.Error(), ExitCode: 1}
	}
	startDate = normalizedContext.StartString()
	endDate = normalizedContext.EndString()
	sensorMode = normalizedContext.SensorMode

	ctx, warnings := withExecutionWarnings(ctx)
	environment = s.effectiveEnvironment(environment)
	fullRefresh = s.effectiveFullRefresh(ctx, environment, fullRefresh)
	relAssetPath, err := DecodeID(assetID)
	if err != nil {
		return MaterializeResult{Status: "error", Error: "invalid asset id", ExitCode: 1}
	}

	normalizedScope, scopeErr := NormalizeMaterializeScope(scope)
	if scopeErr != nil {
		return MaterializeResult{Status: "error", Error: scopeErr.Error(), ExitCode: 1}
	}
	if backfill {
		if normalizedScope != MaterializeScopeAsset {
			return MaterializeResult{Status: "error", Error: "backfill only supports a single asset", ExitCode: 1}
		}
		if s.deps.ResolveAssetByID == nil {
			return MaterializeResult{Status: "error", Error: "asset resolution is not available for backfill", ExitCode: 1}
		}
		_, _, asset, resolveErr := s.deps.ResolveAssetByID(ctx, assetID)
		if resolveErr != nil || asset == nil {
			return MaterializeResult{Status: "error", Error: "asset could not be resolved for backfill", ExitCode: 1}
		}
		if !matlog.BackfillSafe(asset) {
			return MaterializeResult{Status: "error", Error: "asset materialization is not safe to backfill by independent execution windows", ExitCode: 1}
		}
	}
	if err := s.checkRunPolicy(policy.RunRequest{
		Environment:          environment,
		Interactive:          true,
		Destructive:          fullRefresh || backfill,
		ConfirmedEnvironment: strings.TrimSpace(confirmedEnvironment),
	}); err != nil {
		return MaterializeResult{Status: "error", Error: err.Error(), ExitCode: 1}
	}

	timeWindow, timeWindowErr := s.resolveAssetExecutionTimeWindow(ctx, assetID, startDate, endDate)
	if timeWindowErr != nil {
		return MaterializeResult{Status: "error", Error: timeWindowErr.Error(), ExitCode: 1}
	}
	operation := withOperationTimeWindow(runOperation(relAssetPath, "", relAssetPath, environment), timeWindow)
	var output []byte
	assetIDsToRefresh := []string{assetID}
	materializedAssetIDs := []string{assetID}
	assetNamesToRecord := make([]string, 0, 1)
	sensorMode = effectiveSensorMode(sensorMode, false)
	run := func() error {
		var runErr error
		output, runErr = s.runSingleAssetMaterializationWithSensorMode(ctx, relAssetPath, environment, timeWindow, fullRefresh, sensorMode, onChunk)
		return runErr
	}
	if normalizedScope != MaterializeScopeAsset {
		scoped, scopedErr := s.resolveMaterializeAssetScope(ctx, assetID, normalizedScope)
		if scopedErr != nil {
			return MaterializeResult{Status: "error", Error: scopedErr.Error(), ExitCode: 1}
		}
		operation = withOperationTimeWindow(scopedRunOperation(relAssetPath, scoped.PipelineID, relAssetPath, environment, string(normalizedScope), scoped.AssetPaths), timeWindow)
		assetIDsToRefresh = scoped.RefreshAssetIDs
		materializedAssetIDs = scoped.AssetIDs
		assetNamesToRecord = scoped.AssetNames
		run = func() error {
			var runErr error
			output, runErr = s.runScopedAssetMaterialization(ctx, scoped.AssetPaths, environment, timeWindow, fullRefresh, sensorMode, onChunk)
			return runErr
		}
	} else if assetName := s.deps.ResolveAssetNameByID(assetID); assetName != "" {
		assetNamesToRecord = append(assetNamesToRecord, assetName)
	}

	runErr := run()

	changedAssetIDs := make([]string, 0)
	var materializedAt *time.Time
	if runErr == nil {
		now := time.Now().UTC()
		materializedAt = &now
		runAssets := make([]bus.AssetRun, 0, len(assetNamesToRecord))
		pipelineView, pipelineFound := s.findPipelineViewForAsset(assetID)
		for _, assetName := range assetNamesToRecord {
			if assetName == "" {
				continue
			}
			if pipelineFound {
				runAssets = append(runAssets, bus.AssetRun{
					AssetID:   identity.AssetID(pipelineView.UUID, assetName),
					AssetName: assetName,
					Status:    "succeeded",
				})
			}
		}
		if pipelineFound {
			s.emitRunCompletedForSpec(PipelineRunSpec{Environment: environment, FullRefresh: fullRefresh}, pipelineView.UUID, timeWindow, now, runAssets)
		}
		changedAssetIDs = s.deps.FindInspectIDs(assetIDsToRefresh...)
	} else {
		// The run failed. Emit a RunCompleted marking the attempted assets as
		// failed so the matlog recorder persists a failed run attempt. Coverage
		// (success facts) stays untouched, but staleness can now tell "edited and
		// the run failed" from "edited, not run yet", and surface an unchanged
		// asset whose last run failed.
		now := time.Now().UTC()
		pipelineView, pipelineFound := s.findPipelineViewForAsset(assetID)
		if pipelineFound {
			runAssets := make([]bus.AssetRun, 0, len(assetNamesToRecord))
			for _, assetName := range assetNamesToRecord {
				if assetName == "" {
					continue
				}
				runAssets = append(runAssets, bus.AssetRun{
					AssetID:   identity.AssetID(pipelineView.UUID, assetName),
					AssetName: assetName,
					Status:    "failed",
				})
			}
			if len(runAssets) > 0 {
				s.emitRunCompletedForSpec(PipelineRunSpec{Environment: environment, FullRefresh: fullRefresh}, pipelineView.UUID, timeWindow, now, runAssets)
			}
		}
	}

	status := "ok"
	errorMessage := ""
	exitCode := 0
	if runErr != nil {
		status = "error"
		exitCode = 1
		errorMessage = runErr.Error()
		if IsDuckDBLockError(runErr, output) {
			errorMessage = "duckdb database is busy (lock held by another process), please retry"
		}
	}

	if runErr != nil {
		materializedAssetIDs = nil
	}

	return MaterializeResult{
		Status:          status,
		Operation:       operation,
		Output:          string(output),
		Error:           errorMessage,
		ExitCode:        exitCode,
		ChangedAssetIDs: coalesceMaterializedAssetIDs(changedAssetIDs, materializedAssetIDs),
		MaterializedAt:  materializedAt,
		Warnings:        warnings.snapshot(),
	}
}

func (s *ExecutionService) runSingleAssetMaterialization(ctx context.Context, assetPath, environment string, timeWindow ExecutionTimeWindow, fullRefresh bool, onChunk func([]byte)) ([]byte, error) {
	return s.runSingleAssetMaterializationWithSensorMode(ctx, assetPath, environment, timeWindow, fullRefresh, sensorModeOnce, onChunk)
}

func (s *ExecutionService) runSingleAssetMaterializationWithSensorMode(ctx context.Context, assetPath, environment string, timeWindow ExecutionTimeWindow, fullRefresh bool, sensorMode string, onChunk func([]byte)) ([]byte, error) {
	return s.deps.Executor.RunAsset(ctx, RunAssetRequest{AssetPath: assetPath, Environment: environment, SensorMode: effectiveSensorMode(sensorMode, false), StartDate: timeWindow.StartRFC3339(), EndDate: timeWindow.EndRFC3339(), FullRefresh: fullRefresh}, onChunk)
}

func (s *ExecutionService) runScopedAssetMaterialization(ctx context.Context, assetPaths []string, environment string, timeWindow ExecutionTimeWindow, fullRefresh bool, sensorMode string, onChunk func([]byte)) ([]byte, error) {
	var combined bytes.Buffer
	for _, assetPath := range assetPaths {
		chunkOutput, err := s.runSingleAssetMaterializationWithSensorMode(ctx, assetPath, environment, timeWindow, fullRefresh, sensorMode, onChunk)
		if len(chunkOutput) > 0 {
			_, _ = combined.Write(chunkOutput)
		}
		if err != nil {
			return combined.Bytes(), err
		}
	}
	return combined.Bytes(), nil
}

type materializeAssetScopeResult struct {
	PipelineID      string
	AssetIDs        []string
	AssetPaths      []string
	AssetNames      []string
	RefreshAssetIDs []string
}

// NormalizeMaterializeScope validates an asset materialization selection and
// supplies the one-asset default used by both HTTP and service callers.
func NormalizeMaterializeScope(scope string) (MaterializeScope, error) {
	trimmed := strings.TrimSpace(scope)
	if trimmed == "" {
		return MaterializeScopeAsset, nil
	}
	value := MaterializeScope(trimmed)
	switch value {
	case MaterializeScopeAsset, MaterializeScopeAssetWithUpstreams, MaterializeScopeAssetWithDownstreams, MaterializeScopeAssetWithNeighborhood:
		return value, nil
	default:
		return "", fmt.Errorf("invalid materialize scope %q", scope)
	}
}

func coalesceMaterializedAssetIDs(changedAssetIDs, materializedAssetIDs []string) []string {
	if len(changedAssetIDs) > 0 {
		return changedAssetIDs
	}
	return materializedAssetIDs
}

func (s *ExecutionService) resolveMaterializeAssetScope(ctx context.Context, assetID string, scope MaterializeScope) (materializeAssetScopeResult, error) {
	if s.deps.ResolveAssetByID == nil {
		return materializeAssetScopeResult{}, fmt.Errorf("asset resolution is not available")
	}

	_, parsedPipeline, selectedAsset, err := s.deps.ResolveAssetByID(ctx, assetID)
	if err != nil {
		return materializeAssetScopeResult{}, err
	}
	if parsedPipeline == nil || selectedAsset == nil {
		return materializeAssetScopeResult{}, fmt.Errorf("asset not found")
	}

	assetIDByName := make(map[string]string, len(parsedPipeline.Assets))
	assetPathByName := make(map[string]string, len(parsedPipeline.Assets))
	assetByName := make(map[string]*pipeline.Asset, len(parsedPipeline.Assets))
	downstreamByName := make(map[string][]string)
	for _, asset := range parsedPipeline.Assets {
		assetByName[asset.Name] = asset
		assetIDByName[asset.Name] = encodePipelineAssetID(s.deps.WorkspaceRoot, asset)
		assetPathByName[asset.Name] = assetRunPathForPipelineAsset(s.deps.WorkspaceRoot, asset)
		for _, upstream := range asset.Upstreams {
			upstreamName := strings.TrimSpace(upstream.Value)
			if upstreamName == "" {
				continue
			}
			downstreamByName[upstreamName] = append(downstreamByName[upstreamName], asset.Name)
		}
	}

	selected := make(map[string]struct{})
	queue := []string{selectedAsset.Name}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if _, seen := selected[name]; seen {
			continue
		}
		selected[name] = struct{}{}
		current := assetByName[name]
		if current == nil {
			continue
		}
		if scope == MaterializeScopeAssetWithUpstreams || scope == MaterializeScopeAssetWithNeighborhood {
			for _, upstream := range current.Upstreams {
				upstreamName := strings.TrimSpace(upstream.Value)
				if upstreamName == "" || assetByName[upstreamName] == nil {
					continue
				}
				queue = append(queue, upstreamName)
			}
		}
		if scope == MaterializeScopeAssetWithDownstreams || scope == MaterializeScopeAssetWithNeighborhood {
			for _, downstream := range downstreamByName[name] {
				if assetByName[downstream] == nil {
					continue
				}
				queue = append(queue, downstream)
			}
		}
	}

	orderedNames := make([]string, 0, len(parsedPipeline.Assets))
	for _, asset := range parsedPipeline.Assets {
		if _, ok := selected[asset.Name]; ok {
			orderedNames = append(orderedNames, asset.Name)
		}
	}
	if len(orderedNames) == 0 {
		return materializeAssetScopeResult{}, fmt.Errorf("asset scope is empty")
	}

	assetIDs := make([]string, 0, len(orderedNames))
	assetPaths := make([]string, 0, len(orderedNames))
	assetNames := make([]string, 0, len(orderedNames))
	for _, name := range orderedNames {
		assetIDs = append(assetIDs, assetIDByName[name])
		assetPaths = append(assetPaths, assetPathByName[name])
		assetNames = append(assetNames, name)
	}

	refreshIDs := append([]string(nil), assetIDs...)
	return materializeAssetScopeResult{
		PipelineID:      encodePipelineIDForParsedPipeline(s.deps.WorkspaceRoot, parsedPipeline),
		AssetIDs:        assetIDs,
		AssetPaths:      assetPaths,
		AssetNames:      assetNames,
		RefreshAssetIDs: refreshIDs,
	}, nil
}

func assetRunPathForPipelineAsset(workspaceRoot string, asset *pipeline.Asset) string {
	assetPath := asset.ExecutableFile.Path
	if assetPath == "" {
		assetPath = asset.DefinitionFile.Path
	}
	relPath, err := filepath.Rel(workspaceRoot, assetPath)
	if err != nil {
		return filepath.ToSlash(assetPath)
	}
	return filepath.ToSlash(relPath)
}

func encodePipelineAssetID(workspaceRoot string, asset *pipeline.Asset) string {
	return EncodeID(assetRunPathForPipelineAsset(workspaceRoot, asset))
}

func encodePipelineIDForParsedPipeline(workspaceRoot string, parsed *pipeline.Pipeline) string {
	if parsed == nil {
		return ""
	}
	pipelinePath := parsed.DefinitionFile.Path
	if pipelinePath == "" {
		return ""
	}
	relPath, err := filepath.Rel(workspaceRoot, pipelinePath)
	if err != nil {
		return ""
	}
	return EncodeID(filepath.ToSlash(relPath))
}

func (s *ExecutionService) findMissingUpstreamAssets(ctx context.Context, assetID, rawOutput string) ([]string, []string) {
	if s.deps.ResolveAssetByID == nil || strings.TrimSpace(rawOutput) == "" {
		return nil, nil
	}

	_, parsedPipeline, asset, err := s.deps.ResolveAssetByID(ctx, assetID)
	if err != nil || parsedPipeline == nil || asset == nil {
		return nil, nil
	}

	missingObjectNames := extractMissingObjectNames(rawOutput)
	if len(missingObjectNames) == 0 {
		return nil, nil
	}

	assetIDs := make([]string, 0)
	assetNames := make([]string, 0)
	for _, upstream := range asset.Upstreams {
		upstreamName := strings.TrimSpace(upstream.Value)
		if upstreamName == "" {
			continue
		}
		if _, ok := missingObjectNames[normalizeMissingObjectIdentifier(upstreamName)]; !ok {
			continue
		}
		upstreamAsset := parsedPipeline.GetAssetByNameCaseInsensitive(upstreamName)
		if upstreamAsset == nil {
			continue
		}
		assetIDs = append(assetIDs, encodePipelineAssetID(s.deps.WorkspaceRoot, upstreamAsset))
		assetNames = append(assetNames, upstreamAsset.Name)
	}
	if len(assetIDs) == 0 {
		return nil, nil
	}
	return assetIDs, assetNames
}

var missingObjectPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)table with name ([a-zA-Z0-9_\.\"]+) does not exist`),
	regexp.MustCompile(`(?i)relation ([a-zA-Z0-9_\.\"]+) does not exist`),
	regexp.MustCompile(`(?i)no such table:?\s*([a-zA-Z0-9_\.\"]+)`),
	regexp.MustCompile(`(?i)object ([a-zA-Z0-9_\.\"]+) does not exist`),
}

func extractMissingObjectNames(rawOutput string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, pattern := range missingObjectPatterns {
		matches := pattern.FindAllStringSubmatch(rawOutput, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			name := normalizeMissingObjectIdentifier(match[1])
			if name == "" {
				continue
			}
			result[name] = struct{}{}
		}
	}
	return result
}

func normalizeMissingObjectIdentifier(name string) string {
	return strings.ToLower(strings.Trim(strings.ReplaceAll(name, `"`, ""), " "))
}

func (s *ExecutionService) GetPipelineMaterialization(ctx context.Context, pipelineID, environment string) (PipelineMaterializationResponse, *APIError) {
	relPipelinePath, err := DecodeID(pipelineID)
	if err != nil {
		return PipelineMaterializationResponse{}, badRequestError("invalid_pipeline_id", "invalid pipeline id")
	}
	absPipelinePath, err := NewWorkspaceResolver(s.deps.WorkspaceRoot, nil).JoinPath(relPipelinePath)
	if err != nil {
		return PipelineMaterializationResponse{}, badRequestError("invalid_pipeline_path", err.Error())
	}
	parsed, err := s.deps.NewPipelineBuilder().CreatePipelineFromPath(ctx, absPipelinePath, pipeline.WithMutate())
	if err != nil {
		return PipelineMaterializationResponse{}, badRequestError("pipeline_parse_failed", err.Error())
	}

	matInfo := s.inspectPipelineMaterializations(ctx, parsed, environment)
	assets := make([]PipelineMaterializationState, 0, len(parsed.Assets))

	for _, asset := range parsed.Assets {
		assetPath := asset.ExecutableFile.Path
		if assetPath == "" {
			assetPath = asset.DefinitionFile.Path
		}

		relAssetPath, relErr := filepath.Rel(s.deps.WorkspaceRoot, assetPath)
		if relErr != nil {
			relAssetPath = assetPath
		}

		connectionName := ""
		if conn, connErr := targetConnectionNameForAsset(asset, parsed); connErr == nil {
			connectionName = conn
		}

		key := MaterializationAssetKey(asset.Name, connectionName)
		item := PipelineMaterializationState{
			AssetID:         EncodeID(filepath.ToSlash(relAssetPath)),
			Connection:      connectionName,
			DeclaredMatType: string(asset.Materialization.Type),
		}

		if info, ok := matInfo[key]; ok {
			item.IsMaterialized = info.IsMaterialized
			item.MaterializedAs = info.MaterializedAs
			item.RowCount = info.RowCount
			if info.DeclaredMatType != "" {
				item.DeclaredMatType = info.DeclaredMatType
			}
		}

		assets = append(assets, item)
	}

	return PipelineMaterializationResponse{PipelineID: pipelineID, Assets: assets}, nil
}

func (s *ExecutionService) MaterializePipelineStream(ctx context.Context, pipelineID, environment string, dryRun, fullRefresh, backfill bool, startDate, endDate, confirmedEnvironment string, onChunk func([]byte)) MaterializeResult {
	return s.MaterializePipelineStreamWithSensorMode(ctx, pipelineID, environment, dryRun, fullRefresh, backfill, startDate, endDate, confirmedEnvironment, "", onChunk)
}

func (s *ExecutionService) MaterializePipelineStreamWithSensorMode(ctx context.Context, pipelineID, environment string, dryRun, fullRefresh, backfill bool, startDate, endDate, confirmedEnvironment, sensorMode string, onChunk func([]byte)) MaterializeResult {
	return s.MaterializePipelineRun(ctx, PipelineRunSpec{
		PipelineID:           pipelineID,
		Environment:          environment,
		SensorMode:           sensorMode,
		DryRun:               dryRun,
		FullRefresh:          fullRefresh,
		Backfill:             backfill,
		StartDate:            startDate,
		EndDate:              endDate,
		ConfirmedEnvironment: confirmedEnvironment,
	}, onChunk, nil)
}

func (s *ExecutionService) MaterializePipelineStreamWithAssetEvents(ctx context.Context, pipelineID, environment string, dryRun, fullRefresh, backfill bool, startDate, endDate, confirmedEnvironment string, onChunk func([]byte), onAssetEvent func(ExecutionAssetEvent)) MaterializeResult {
	return s.MaterializePipelineRun(ctx, PipelineRunSpec{
		PipelineID:           pipelineID,
		Environment:          environment,
		DryRun:               dryRun,
		FullRefresh:          fullRefresh,
		Backfill:             backfill,
		StartDate:            startDate,
		EndDate:              endDate,
		ConfirmedEnvironment: confirmedEnvironment,
	}, onChunk, onAssetEvent)
}

// MaterializePipelineStreamForRun is the variant used by the scheduler: the
// run ID is threaded through so the RunCompleted bus event attributes
// materializations to the scheduler run record.
func (s *ExecutionService) MaterializePipelineStreamForRun(ctx context.Context, runID, pipelineID, environment string, dryRun, fullRefresh bool, startDate, endDate string, onChunk func([]byte), onAssetEvent func(ExecutionAssetEvent)) MaterializeResult {
	return s.MaterializePipelineRun(ctx, PipelineRunSpec{
		RunID:       runID,
		PipelineID:  pipelineID,
		Environment: environment,
		Scheduled:   true,
		DryRun:      dryRun,
		FullRefresh: fullRefresh,
		StartDate:   startDate,
		EndDate:     endDate,
	}, onChunk, onAssetEvent)
}

// PipelineRunSpec describes one pipeline execution. When SnapshotDir is set
// the executor runs the materialized snapshot instead of the working tree;
// PipelineID still identifies the pipeline for events and asset listing.
type PipelineRunSpec struct {
	RunID       string
	PipelineID  string
	Environment string
	// Scheduled is derived from the server-owned run origin. A queued manual
	// run also has a RunID, so RunID must not be used for this distinction.
	Scheduled            bool
	SensorMode           string
	DryRun               bool
	FullRefresh          bool
	Backfill             bool
	StartDate            string
	EndDate              string
	ConfirmedEnvironment string
	SnapshotDir          string
	SnapshotVersionID    string
	// ConfigPath points the executor at .bruin.yml when the target directory
	// is outside the workspace git repository (snapshot runs).
	ConfigPath string
	// OnContextResolved persists the effective context after policy and source
	// normalization but before the first asset starts. A scheduler-backed run
	// uses it to make crash recovery preserve materialization semantics.
	OnContextResolved func(ResolvedPipelineRunContext) error
}

type ResolvedPipelineRunContext struct {
	Environment string
	WinStart    time.Time
	WinEnd      time.Time
	FullRefresh bool
	Backfill    bool
	SensorMode  string
}

func (s *ExecutionService) MaterializePipelineRun(ctx context.Context, spec PipelineRunSpec, onChunk func([]byte), onAssetEvent func(ExecutionAssetEvent)) MaterializeResult {
	contextInput := runcontext.Input{
		Start:       spec.StartDate,
		End:         spec.EndDate,
		FullRefresh: spec.FullRefresh,
		Backfill:    spec.Backfill,
		SensorMode:  spec.SensorMode,
	}
	normalizedContext, contextErr := runcontext.Normalize(contextInput)
	if contextErr != nil {
		return MaterializeResult{Status: "error", Error: contextErr.Error(), ExitCode: 1}
	}
	if contextErr := runcontext.ValidateDryRun(spec.DryRun, contextInput); contextErr != nil {
		return MaterializeResult{Status: "error", Error: contextErr.Error(), ExitCode: 1}
	}
	spec.StartDate = normalizedContext.StartString()
	spec.EndDate = normalizedContext.EndString()
	spec.SensorMode = normalizedContext.SensorMode

	ctx, warnings := withExecutionWarnings(ctx)
	spec.Environment = s.effectiveEnvironment(spec.Environment)
	spec.FullRefresh = s.effectiveFullRefresh(ctx, spec.Environment, spec.FullRefresh)
	if err := s.checkRunPolicy(policy.RunRequest{
		Environment:          spec.Environment,
		Interactive:          !spec.Scheduled,
		SnapshotBased:        spec.SnapshotDir != "",
		Destructive:          !spec.DryRun && (spec.FullRefresh || spec.Backfill),
		ConfirmedEnvironment: strings.TrimSpace(spec.ConfirmedEnvironment),
	}); err != nil {
		return MaterializeResult{Status: "error", Error: err.Error(), ExitCode: 1}
	}
	target, err := ResolvePipelineRunTarget(spec.PipelineID)
	if err != nil {
		return MaterializeResult{Status: "error", Error: "invalid pipeline id", ExitCode: 1}
	}
	if spec.SnapshotDir != "" {
		target = spec.SnapshotDir
	}

	timeWindow := ExecutionTimeWindow{}
	if !spec.DryRun {
		var timeWindowErr error
		timeWindow, timeWindowErr = s.resolvePipelineExecutionTimeWindow(ctx, spec.PipelineID, spec.SnapshotDir, spec.StartDate, spec.EndDate)
		if timeWindowErr != nil {
			return MaterializeResult{Status: "error", Error: timeWindowErr.Error(), ExitCode: 1}
		}
	}
	operation := withOperationTimeWindow(runOperation(target, spec.PipelineID, "", spec.Environment), timeWindow)
	observed := newPipelineRunObservation(onAssetEvent)
	sensorMode := effectiveSensorMode(spec.SensorMode, spec.Scheduled)
	if !spec.DryRun && spec.OnContextResolved != nil {
		if err := spec.OnContextResolved(ResolvedPipelineRunContext{
			Environment: spec.Environment,
			WinStart:    timeWindow.Start,
			WinEnd:      timeWindow.End,
			FullRefresh: spec.FullRefresh,
			Backfill:    spec.Backfill,
			SensorMode:  sensorMode,
		}); err != nil {
			return MaterializeResult{Status: "error", Error: "persist resolved run context: " + err.Error(), ExitCode: 1}
		}
	}
	output, runErr := s.deps.Executor.RunPipeline(ctx, RunPipelineRequest{
		Target:      target,
		Environment: spec.Environment,
		SensorMode:  sensorMode,
		DryRun:      spec.DryRun,
		StartDate:   timeWindow.StartRFC3339(),
		EndDate:     timeWindow.EndRFC3339(),
		AssetEvent:  observed.handle,
		ConfigPath:  spec.ConfigPath,
		FullRefresh: spec.FullRefresh,
	}, onChunk)

	changedAssetIDs := make([]string, 0)
	var materializedAt *time.Time
	if !spec.DryRun {
		now := time.Now().UTC()
		if currentPipeline, ok := s.findPipelineView(spec.PipelineID); ok {
			completionStatus := "succeeded"
			if runErr != nil {
				completionStatus = "failed"
				if ctx.Err() != nil {
					completionStatus = "cancelled"
				}
			}
			runAssets, succeededIDs := observed.completedAssets(currentPipeline, completionStatus)
			changedAssetIDs = append(changedAssetIDs, succeededIDs...)
			s.emitRunCompletedForSpec(spec, currentPipeline.UUID, timeWindow, now, runAssets)
			if runErr == nil {
				materializedAt = &now
			}
		}
	}

	status := "ok"
	errorMessage := ""
	exitCode := 0
	if runErr != nil {
		status = "error"
		errorMessage = runErr.Error()
		exitCode = 1
	}

	return MaterializeResult{
		Status:          status,
		Operation:       operation,
		Output:          string(output),
		Error:           errorMessage,
		ExitCode:        exitCode,
		ChangedAssetIDs: changedAssetIDs,
		MaterializedAt:  materializedAt,
		Warnings:        warnings.snapshot(),
	}
}

type pipelineRunObservation struct {
	mu       sync.Mutex
	onEvent  func(ExecutionAssetEvent)
	order    []string
	statuses map[string]string
}

func newPipelineRunObservation(onEvent func(ExecutionAssetEvent)) *pipelineRunObservation {
	return &pipelineRunObservation{onEvent: onEvent, statuses: make(map[string]string)}
}

func (o *pipelineRunObservation) handle(event ExecutionAssetEvent) {
	if o.onEvent != nil {
		o.onEvent(event)
	}
	assetName := strings.TrimSpace(event.Asset)
	if assetName == "" {
		return
	}
	status := completedExecutionStatus(event.Status)
	running := strings.EqualFold(strings.TrimSpace(event.Status), "running")
	if status == "" && !running {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, exists := o.statuses[assetName]; !exists {
		o.order = append(o.order, assetName)
	}
	if status != "" {
		o.statuses[assetName] = status
	} else if _, exists := o.statuses[assetName]; !exists {
		o.statuses[assetName] = ""
	}
}

func (o *pipelineRunObservation) completedAssets(view PipelineView, completionStatus string) ([]bus.AssetRun, []string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	assetsByName := make(map[string]AssetView, len(view.Assets))
	for _, asset := range view.Assets {
		assetsByName[asset.Name] = asset
		if completionStatus == "succeeded" {
			if _, observed := o.statuses[asset.Name]; !observed {
				o.order = append(o.order, asset.Name)
				o.statuses[asset.Name] = "succeeded"
			}
		}
	}

	runs := make([]bus.AssetRun, 0, len(o.order))
	succeededIDs := make([]string, 0, len(o.order))
	for _, name := range o.order {
		asset, exists := assetsByName[name]
		if !exists {
			continue
		}
		status := o.statuses[name]
		if status == "" {
			status = completionStatus
		}
		runs = append(runs, bus.AssetRun{
			AssetID:   identity.AssetID(view.UUID, name),
			AssetName: name,
			Status:    status,
		})
		if status == "succeeded" {
			succeededIDs = append(succeededIDs, asset.ID)
		}
	}
	return runs, succeededIDs
}

func completedExecutionStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "succeeded", "ok", "finished":
		return "succeeded"
	case "failed", "failure", "error", "errored":
		return "failed"
	case "cancelled", "canceled":
		return "cancelled"
	default:
		return ""
	}
}

func (s *ExecutionService) findPipelineView(pipelineID string) (PipelineView, bool) {
	if s.deps.CurrentPipelines == nil {
		return PipelineView{}, false
	}
	for _, view := range s.deps.CurrentPipelines() {
		if view.ID == pipelineID {
			return view, true
		}
	}
	return PipelineView{}, false
}

// ResolvePipelineRunTarget validates that the pipeline ID decodes to a
// runnable target path.
func (s *ExecutionService) ResolvePipelineRunTarget(pipelineID string) error {
	_, err := ResolvePipelineRunTarget(pipelineID)
	return err
}

func ResolvePipelineRunTarget(pipelineID string) (string, error) {
	relPath, err := DecodeID(pipelineID)
	if err != nil {
		return "", err
	}

	cleaned := filepath.Clean(relPath)
	base := strings.ToLower(filepath.Base(cleaned))
	if base == "pipeline.yml" || base == "pipeline.yaml" || base == ".pipeline.yml" || base == ".pipeline.yaml" {
		dir := filepath.Dir(cleaned)
		if dir == "." {
			return ".", nil
		}
		return filepath.ToSlash(dir), nil
	}

	return filepath.ToSlash(cleaned), nil
}

func (s *ExecutionService) resolveAssetExecutionTimeWindow(ctx context.Context, assetID, startDate, endDate string) (ExecutionTimeWindow, error) {
	schedule := ""
	if s.deps.ResolveAssetByID != nil {
		_, parsedPipeline, _, err := s.deps.ResolveAssetByID(ctx, assetID)
		if err == nil && parsedPipeline != nil {
			schedule = string(parsedPipeline.Schedule)
		}
	}
	return ResolveExecutionTimeWindow(schedule, startDate, endDate, time.Now().UTC())
}

func (s *ExecutionService) resolvePipelineExecutionTimeWindow(ctx context.Context, pipelineID, snapshotDir, startDate, endDate string) (ExecutionTimeWindow, error) {
	// Explicit bounds are already the authoritative execution context. They do
	// not depend on either source's pipeline schedule and must survive a pinned
	// run unchanged.
	if strings.TrimSpace(startDate) != "" || strings.TrimSpace(endDate) != "" {
		return ResolveExecutionTimeWindow("", startDate, endDate, time.Now().UTC())
	}

	// A pinned run executes the pipeline materialized in SnapshotDir. Resolve
	// its default interval from that same source rather than from a potentially
	// newer working-tree pipeline.
	if strings.TrimSpace(snapshotDir) != "" {
		schedule, err := readPipelineSchedule(snapshotDir)
		if err != nil {
			return ExecutionTimeWindow{}, fmt.Errorf("resolve deployed pipeline execution window: %w", err)
		}
		return ResolveExecutionTimeWindow(string(schedule), "", "", time.Now().UTC())
	}

	if target, err := ResolvePipelineRunTarget(pipelineID); err == nil && s.deps.NewPipelineBuilder != nil {
		absPipelinePath, joinErr := NewWorkspaceResolver(s.deps.WorkspaceRoot, nil).JoinPath(target)
		if joinErr == nil {
			if parsed, parseErr := s.deps.NewPipelineBuilder().CreatePipelineFromPath(ctx, absPipelinePath, pipeline.WithMutate()); parseErr == nil && parsed != nil {
				return ResolveExecutionTimeWindow(string(parsed.Schedule), startDate, endDate, time.Now().UTC())
			}
		}
	}
	return ResolveExecutionTimeWindow("", startDate, endDate, time.Now().UTC())
}

func readPipelineSchedule(pipelineDir string) (pipeline.Schedule, error) {
	for _, definitionFile := range PipelineDefinitionFiles {
		content, err := os.ReadFile(filepath.Join(pipelineDir, definitionFile))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}

		var definition struct {
			Schedule pipeline.Schedule `yaml:"schedule"`
		}
		if err := yaml.Unmarshal(content, &definition); err != nil {
			return "", err
		}
		if schedule := string(definition.Schedule); strings.Contains(schedule, "{{") || strings.Contains(schedule, "{%") {
			return "", fmt.Errorf("templated deployed pipeline schedule requires an explicit execution window")
		}
		return definition.Schedule, nil
	}
	return "", fmt.Errorf("pipeline definition was not found")
}

func (s *ExecutionService) inspectPipelineMaterializations(ctx context.Context, parsed *pipeline.Pipeline, environment string) map[string]PipelineMaterializationInfo {
	result := make(map[string]PipelineMaterializationInfo)

	assetsByConnection := make(map[string][]*pipeline.Asset)
	for _, asset := range parsed.Assets {
		if isSensorAssetType(asset.Type) {
			continue
		}
		conn, err := targetConnectionNameForAsset(asset, parsed)
		if err != nil || conn == "" {
			continue
		}
		assetsByConnection[conn] = append(assetsByConnection[conn], asset)
	}

	for connName, assets := range assetsByConnection {
		objects, err := s.fetchObjectsForConnection(ctx, connName, environment)
		if err != nil || len(objects) == 0 {
			for _, asset := range assets {
				key := MaterializationAssetKey(asset.Name, connName)
				result[key] = PipelineMaterializationInfo{
					AssetName:       asset.Name,
					Connection:      connName,
					DeclaredMatType: string(asset.Materialization.Type),
				}
			}
			continue
		}

		wanted := make(map[string]struct{})
		for _, asset := range assets {
			wanted[NormalizeIdentifier(asset.Name)] = struct{}{}
			parts := strings.Split(NormalizeIdentifier(asset.Name), ".")
			if len(parts) > 1 {
				wanted[parts[len(parts)-1]] = struct{}{}
			}
		}

		candidateObjects := make([]DBObjectInfo, 0)
		for _, object := range objects {
			if _, ok := wanted[NormalizeIdentifier(object.QualifiedName)]; ok {
				candidateObjects = append(candidateObjects, object)
				continue
			}
			if _, ok := wanted[NormalizeIdentifier(object.Name)]; ok {
				candidateObjects = append(candidateObjects, object)
			}
		}

		tableObjects := make([]DBObjectInfo, 0, len(candidateObjects))
		for _, object := range candidateObjects {
			if object.Kind == "table" {
				tableObjects = append(tableObjects, object)
			}
		}

		rowCounts := s.fetchRowCountsForObjects(ctx, connName, environment, tableObjects)

		objectsByName := make(map[string]DBObjectInfo)
		for _, object := range objects {
			objectsByName[NormalizeIdentifier(object.QualifiedName)] = object
			objectsByName[NormalizeIdentifier(object.Name)] = object
		}

		for _, asset := range assets {
			normalized := NormalizeIdentifier(asset.Name)
			object, ok := objectsByName[normalized]
			if !ok {
				parts := strings.Split(normalized, ".")
				if len(parts) > 1 {
					object, ok = objectsByName[parts[len(parts)-1]]
				}
			}

			key := MaterializationAssetKey(asset.Name, connName)
			item := PipelineMaterializationInfo{
				AssetName:       asset.Name,
				Connection:      connName,
				DeclaredMatType: string(asset.Materialization.Type),
			}

			if ok {
				item.IsMaterialized = true
				item.MaterializedAs = object.Kind

				if count, hasCount := rowCounts[NormalizeIdentifier(object.QualifiedName)]; hasCount {
					c := count
					item.RowCount = &c
				} else if count, hasCount := rowCounts[NormalizeIdentifier(object.Name)]; hasCount {
					c := count
					item.RowCount = &c
				}
			}

			result[key] = item
		}
	}

	return result
}

func (s *ExecutionService) runConnectionQuery(ctx context.Context, connectionName, query string) ([]string, []map[string]any, error) {
	return s.RunConnectionQueryForEnvironment(ctx, connectionName, "", query)
}

func (s *ExecutionService) RunConnectionQueryForEnvironment(ctx context.Context, connectionName, environment, query string) ([]string, []map[string]any, error) {
	output, err := s.deps.Executor.QueryConnection(ctx, QueryConnectionRequest{
		ConnectionName: connectionName,
		Query:          query,
		Environment:    environment,
		Output:         "json",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("query failed for connection '%s': %w", connectionName, err)
	}

	columns, rows := ParseQueryJSONOutput(output)
	return columns, rows, nil
}

func ReadStringField(row map[string]any, keys ...string) string {
	for _, key := range keys {
		for rowKey, value := range row {
			if strings.EqualFold(rowKey, key) {
				s, ok := value.(string)
				if ok {
					return s
				}
			}
		}
	}
	return ""
}

func ReadInt64Field(row map[string]any, key string) (int64, bool) {
	for rowKey, value := range row {
		if !strings.EqualFold(rowKey, key) {
			continue
		}

		switch v := value.(type) {
		case int:
			return int64(v), true
		case int64:
			return v, true
		case float64:
			return int64(v), true
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				return 0, false
			}
			var parsed int64
			_, err := fmt.Sscan(trimmed, &parsed)
			if err == nil {
				return parsed, true
			}
		}
	}

	return 0, false
}

func maxTimePtr(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if b.After(*a) {
		return b
	}
	return a
}
