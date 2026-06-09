package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/sqlparser"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

type InspectResult struct {
	Status                              string
	Columns                             []string
	Rows                                []map[string]any
	RawOutput                           string
	Operation                           OperationMetadata
	Error                               string
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
}

type MaterializeScope string

const (
	MaterializeScopeAsset                 MaterializeScope = "asset"
	MaterializeScopeAssetWithUpstreams    MaterializeScope = "asset_with_upstreams"
	MaterializeScopeAssetWithDownstreams  MaterializeScope = "asset_with_downstreams"
	MaterializeScopeAssetWithNeighborhood MaterializeScope = "asset_with_upstreams_and_downstreams"
)

type DuckDBExecutionInfo struct {
	ConnectionName string
	DatabasePath   string
	LockKey        string
}

type ExecutionDependencies struct {
	WorkspaceRoot         string
	ConfigPath            string
	Executor              BruinCommandExecutor
	ResolveAssetByID      func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error)
	ResolveAssetNameByID  func(string) string
	FindInspectIDs        func(...string) []string
	RecordMaterialization func(string, time.Time, string)
	CurrentPipelines      func() []PipelineView
	DuckDBLock            func(string) *sync.Mutex
	ParseQueryOutput      func([]byte) ([]string, []map[string]any)
	NewPipelineBuilder    func() *pipeline.Builder
	FreshnessSnapshot     func() map[string]AssetTimestamps
}

type PipelineView struct {
	ID     string
	Assets []AssetView
}

type AssetView struct {
	ID   string
	Name string
}

type AssetTimestamps struct {
	MaterializedAt   *time.Time
	ContentChangedAt *time.Time
	LastStatus       string
}

type PipelineMaterializationInfo struct {
	AssetName       string
	Connection      string
	IsMaterialized  bool
	MaterializedAs  string
	FreshnessStatus string
	RowCount        *int64
	DeclaredMatType string
}

type PipelineMaterializationState struct {
	AssetID         string
	IsMaterialized  bool
	MaterializedAs  string
	FreshnessStatus string
	RowCount        *int64
	Connection      string
	DeclaredMatType string
}

type PipelineMaterializationResponse struct {
	PipelineID string
	Assets     []PipelineMaterializationState
}

type ExecutionService struct {
	deps ExecutionDependencies
}

const inspectReadOnlyErrorMessage = "Inspect only supports read-only single SELECT queries. Materialize the asset to run write, delete, copy, or multi-statement SQL."

func NewExecutionService(deps ExecutionDependencies) *ExecutionService {
	return &ExecutionService{deps: deps}
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

	duckDBInfo, infoErr := s.findDuckDBExecutionInfoByAsset(ctx, assetID)
	if infoErr != nil {
		return InspectResult{Status: "error", Error: infoErr.Error(), HTTPStatus: 400}
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

	if duckDBInfo != nil {
		mu := s.deps.DuckDBLock(duckDBInfo.LockKey)
		if mu != nil {
			mu.Lock()
			err = run()
			if err != nil && IsDuckDBLockError(err, output) {
				if readOnlyConfigPath, cleanup, cfgErr := s.buildReadOnlyConfigFile(duckDBInfo); cfgErr == nil {
					defer cleanup()
					queryReq.ConfigFile = readOnlyConfigPath
					operation.ConfigFile = readOnlyConfigPath
					err = run()
				}
			}
			mu.Unlock()
		} else {
			err = run()
		}
	} else {
		err = run()
	}

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

	connectionName, err := parsedPipeline.GetConnectionNameForAsset(asset)
	if err != nil || strings.TrimSpace(connectionName) == "" {
		return InspectResult{}, false
	}

	rowLimit := normalizeInspectLimit(limit)
	query := fmt.Sprintf("select * from %s limit %d", asset.Name, rowLimit)
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

func (s *ExecutionService) MaterializeAssetStream(ctx context.Context, assetID, environment, scope, startDate, endDate string, onChunk func([]byte)) MaterializeResult {
	relAssetPath, err := DecodeID(assetID)
	if err != nil {
		return MaterializeResult{Status: "error", Error: "invalid asset id", ExitCode: 1}
	}

	normalizedScope, scopeErr := normalizeMaterializeScope(scope)
	if scopeErr != nil {
		return MaterializeResult{Status: "error", Error: scopeErr.Error(), ExitCode: 1}
	}

	duckDBInfo, infoErr := s.findDuckDBExecutionInfoByAsset(ctx, assetID)
	if infoErr != nil {
		return MaterializeResult{Status: "error", Error: infoErr.Error(), ExitCode: 1}
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
	run := func() error {
		var runErr error
		output, runErr = s.runSingleAssetMaterialization(ctx, relAssetPath, environment, timeWindow, onChunk)
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
			output, runErr = s.runScopedAssetMaterialization(ctx, scoped.AssetPaths, environment, timeWindow, onChunk)
			return runErr
		}
	} else if assetName := s.deps.ResolveAssetNameByID(assetID); assetName != "" {
		assetNamesToRecord = append(assetNamesToRecord, assetName)
	}

	var runErr error
	if duckDBInfo != nil {
		mu := s.deps.DuckDBLock(duckDBInfo.LockKey)
		mu.Lock()
		runErr = run()
		mu.Unlock()
	} else {
		runErr = run()
	}

	changedAssetIDs := make([]string, 0)
	var materializedAt *time.Time
	if runErr == nil {
		now := time.Now().UTC()
		materializedAt = &now
		for _, assetName := range assetNamesToRecord {
			if assetName == "" {
				continue
			}
			s.deps.RecordMaterialization(assetName, now, "succeeded")
		}
		changedAssetIDs = s.deps.FindInspectIDs(assetIDsToRefresh...)
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
	}
}

func (s *ExecutionService) runSingleAssetMaterialization(ctx context.Context, assetPath, environment string, timeWindow ExecutionTimeWindow, onChunk func([]byte)) ([]byte, error) {
	return s.deps.Executor.RunAsset(ctx, RunAssetRequest{AssetPath: assetPath, Environment: environment, StartDate: timeWindow.StartRFC3339(), EndDate: timeWindow.EndRFC3339()}, onChunk)
}

func (s *ExecutionService) runScopedAssetMaterialization(ctx context.Context, assetPaths []string, environment string, timeWindow ExecutionTimeWindow, onChunk func([]byte)) ([]byte, error) {
	var combined bytes.Buffer
	for _, assetPath := range assetPaths {
		chunkOutput, err := s.runSingleAssetMaterialization(ctx, assetPath, environment, timeWindow, onChunk)
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

func normalizeMaterializeScope(scope string) (MaterializeScope, error) {
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

func (s *ExecutionService) GetPipelineMaterialization(ctx context.Context, pipelineID, environment string) (PipelineMaterializationResponse, error) {
	relPipelinePath, err := DecodeID(pipelineID)
	if err != nil {
		return PipelineMaterializationResponse{}, fmt.Errorf("invalid pipeline id")
	}
	absPipelinePath, err := NewWorkspaceResolver(s.deps.WorkspaceRoot, nil).JoinPath(relPipelinePath)
	if err != nil {
		return PipelineMaterializationResponse{}, err
	}
	parsed, err := s.deps.NewPipelineBuilder().CreatePipelineFromPath(ctx, absPipelinePath, pipeline.WithMutate())
	if err != nil {
		return PipelineMaterializationResponse{}, err
	}

	matInfo := s.inspectPipelineMaterializations(ctx, parsed, environment)
	freshnessByAssetName := ComputePipelineFreshness(parsed, matInfo, s.deps.FreshnessSnapshot())
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
		if conn, connErr := parsed.GetConnectionNameForAsset(asset); connErr == nil {
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
			item.FreshnessStatus = info.FreshnessStatus
			item.RowCount = info.RowCount
			if info.DeclaredMatType != "" {
				item.DeclaredMatType = info.DeclaredMatType
			}
		}

		if status, ok := freshnessByAssetName[asset.Name]; ok {
			item.FreshnessStatus = status
		}

		assets = append(assets, item)
	}

	return PipelineMaterializationResponse{PipelineID: pipelineID, Assets: assets}, nil
}

func (s *ExecutionService) MaterializePipelineStream(ctx context.Context, pipelineID, environment string, dryRun bool, startDate, endDate string, onChunk func([]byte)) MaterializeResult {
	return s.MaterializePipelineStreamWithAssetEvents(ctx, pipelineID, environment, dryRun, startDate, endDate, onChunk, nil)
}

func (s *ExecutionService) MaterializePipelineStreamWithAssetEvents(ctx context.Context, pipelineID, environment string, dryRun bool, startDate, endDate string, onChunk func([]byte), onAssetEvent func(ExecutionAssetEvent)) MaterializeResult {
	target, err := ResolvePipelineRunTarget(pipelineID)
	if err != nil {
		return MaterializeResult{Status: "error", Error: "invalid pipeline id", ExitCode: 1}
	}

	timeWindow, timeWindowErr := s.resolvePipelineExecutionTimeWindow(ctx, pipelineID, startDate, endDate)
	if timeWindowErr != nil {
		return MaterializeResult{Status: "error", Error: timeWindowErr.Error(), ExitCode: 1}
	}
	operation := withOperationTimeWindow(runOperation(target, pipelineID, "", environment), timeWindow)
	output, runErr := s.deps.Executor.RunPipeline(ctx, RunPipelineRequest{Target: target, Environment: environment, DryRun: dryRun, StartDate: timeWindow.StartRFC3339(), EndDate: timeWindow.EndRFC3339(), AssetEvent: onAssetEvent}, onChunk)

	changedAssetIDs := make([]string, 0)
	var materializedAt *time.Time
	if runErr == nil && !dryRun {
		now := time.Now().UTC()
		materializedAt = &now
		for _, currentPipeline := range s.deps.CurrentPipelines() {
			if currentPipeline.ID != pipelineID {
				continue
			}
			for _, asset := range currentPipeline.Assets {
				changedAssetIDs = append(changedAssetIDs, asset.ID)
				if strings.TrimSpace(asset.Name) != "" {
					s.deps.RecordMaterialization(asset.Name, now, "succeeded")
				}
			}
			break
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
	}
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

func (s *ExecutionService) resolvePipelineExecutionTimeWindow(ctx context.Context, pipelineID, startDate, endDate string) (ExecutionTimeWindow, error) {
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

func (s *ExecutionService) inspectPipelineMaterializations(ctx context.Context, parsed *pipeline.Pipeline, environment string) map[string]PipelineMaterializationInfo {
	result := make(map[string]PipelineMaterializationInfo)

	assetsByConnection := make(map[string][]*pipeline.Asset)
	for _, asset := range parsed.Assets {
		conn, err := parsed.GetConnectionNameForAsset(asset)
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

func ComputePipelineFreshness(parsed *pipeline.Pipeline, matInfo map[string]PipelineMaterializationInfo, tracker map[string]AssetTimestamps) map[string]string {
	result := make(map[string]string, len(parsed.Assets))
	assetsByName := make(map[string]*pipeline.Asset, len(parsed.Assets))
	for _, asset := range parsed.Assets {
		assetsByName[asset.Name] = asset
	}

	type visitState int
	const (
		visitUnknown visitState = iota
		visitActive
		visitDone
	)

	type freshnessEval struct {
		Fresh           bool
		EffectiveUpdate *time.Time
	}

	state := make(map[string]visitState, len(parsed.Assets))
	evals := make(map[string]freshnessEval, len(parsed.Assets))

	var evalAsset func(assetName string) freshnessEval
	evalAsset = func(assetName string) freshnessEval {
		if state[assetName] == visitDone {
			return evals[assetName]
		}
		if state[assetName] == visitActive {
			return freshnessEval{Fresh: false}
		}

		asset, ok := assetsByName[assetName]
		if !ok {
			return freshnessEval{Fresh: false}
		}

		state[assetName] = visitActive
		defer func() {
			state[assetName] = visitDone
		}()

		kind := "table"
		connectionName := ""
		if conn, err := parsed.GetConnectionNameForAsset(asset); err == nil {
			connectionName = conn
		}
		if info, ok := matInfo[MaterializationAssetKey(asset.Name, connectionName)]; ok {
			if strings.EqualFold(strings.TrimSpace(info.MaterializedAs), "view") {
				kind = "view"
			}
		}
		if strings.EqualFold(strings.TrimSpace(string(asset.Materialization.Type)), "view") {
			kind = "view"
		}

		trackerEntry, hasTracker := tracker[assetName]
		var materializedAt *time.Time
		if hasTracker && trackerEntry.MaterializedAt != nil {
			ts := trackerEntry.MaterializedAt.UTC()
			materializedAt = &ts
		}

		upstreamEvals := make([]freshnessEval, 0, len(asset.Upstreams))
		for _, up := range asset.Upstreams {
			upstreamEvals = append(upstreamEvals, evalAsset(up.Value))
		}

		if kind == "view" {
			if len(upstreamEvals) == 0 {
				fresh := materializedAt != nil
				e := freshnessEval{Fresh: fresh, EffectiveUpdate: materializedAt}
				evals[assetName] = e
				return e
			}

			fresh := true
			var latest *time.Time
			for _, up := range upstreamEvals {
				if !up.Fresh {
					fresh = false
				}
				latest = maxTimePtr(latest, up.EffectiveUpdate)
			}

			e := freshnessEval{Fresh: fresh, EffectiveUpdate: latest}
			evals[assetName] = e
			return e
		}

		if materializedAt == nil {
			e := freshnessEval{Fresh: false, EffectiveUpdate: nil}
			evals[assetName] = e
			return e
		}

		fresh := true
		for _, up := range upstreamEvals {
			if !up.Fresh {
				fresh = false
				continue
			}
			if up.EffectiveUpdate != nil && up.EffectiveUpdate.After(*materializedAt) {
				fresh = false
			}
		}

		e := freshnessEval{Fresh: fresh, EffectiveUpdate: materializedAt}
		evals[assetName] = e
		return e
	}

	for _, asset := range parsed.Assets {
		e := evalAsset(asset.Name)
		if e.Fresh {
			result[asset.Name] = "fresh"
		} else {
			result[asset.Name] = "stale"
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

func (s *ExecutionService) findDuckDBExecutionInfoByAsset(ctx context.Context, assetID string) (*DuckDBExecutionInfo, error) {
	_, parsed, asset, err := s.deps.ResolveAssetByID(ctx, assetID)
	if err != nil {
		return nil, err
	}

	connectionName, err := parsed.GetConnectionNameForAsset(asset)
	if err != nil || connectionName == "" {
		return nil, nil
	}

	fs := afero.NewOsFs()
	if exists, _ := afero.Exists(fs, s.deps.ConfigPath); !exists {
		return nil, nil
	}

	cfg, cfgErr := loadSelectedConfig(s.deps.ConfigPath, "")
	if cfgErr != nil || cfg.SelectedEnvironment == nil || cfg.SelectedEnvironment.Connections == nil {
		return nil, nil
	}

	for _, conn := range cfg.SelectedEnvironment.Connections.DuckDB {
		if conn.Name != connectionName {
			continue
		}
		databasePath := strings.TrimSpace(conn.Path)
		if databasePath == "" {
			databasePath = connectionName
		} else {
			databasePath = filepath.Clean(databasePath)
		}
		return &DuckDBExecutionInfo{ConnectionName: connectionName, DatabasePath: databasePath, LockKey: "duckdb:" + databasePath}, nil
	}

	return nil, nil
}

func (s *ExecutionService) buildReadOnlyConfigFile(info *DuckDBExecutionInfo) (string, func(), error) {
	if info == nil || info.ConnectionName == "" {
		return "", nil, fmt.Errorf("duckdb read-only config requires connection info")
	}

	cfg, err := loadSelectedConfig(s.deps.ConfigPath, "")
	if err != nil {
		return "", nil, err
	}
	if cfg.SelectedEnvironment == nil || cfg.SelectedEnvironment.Connections == nil {
		return "", nil, fmt.Errorf("selected environment has no connections")
	}

	envName := cfg.SelectedEnvironmentName
	env, ok := cfg.Environments[envName]
	if !ok || env.Connections == nil {
		return "", nil, fmt.Errorf("environment '%s' not found", envName)
	}

	found := false
	for i := range env.Connections.DuckDB {
		if env.Connections.DuckDB[i].Name != info.ConnectionName {
			continue
		}
		env.Connections.DuckDB[i].Path = AppendDuckDBReadOnlyMode(env.Connections.DuckDB[i].Path)
		found = true
		break
	}
	if !found {
		return "", nil, fmt.Errorf("duckdb connection '%s' not found", info.ConnectionName)
	}
	cfg.Environments[envName] = env

	fs := afero.NewOsFs()
	tempFile, err := afero.TempFile(fs, "", "renart-readonly-*.yml")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = fs.Remove(tempFile.Name()) }
	if err := tempFile.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	content, err := yaml.Marshal(cfg)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if err := afero.WriteFile(fs, tempFile.Name(), content, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return tempFile.Name(), cleanup, nil
}
