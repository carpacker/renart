package service

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	"renart/internal/web/matlog"
	"renart/internal/web/policy"
	"renart/internal/web/runcontext"
)

type RunRequest struct {
	PipelineID           string `json:"pipeline_id"`
	AssetPath            string `json:"asset_path"`
	Environment          string `json:"environment"`
	DryRun               bool   `json:"dry_run"`
	StartDate            string `json:"start_date"`
	EndDate              string `json:"end_date"`
	FullRefresh          bool   `json:"full_refresh"`
	Backfill             bool   `json:"backfill"`
	ConfirmedEnvironment string `json:"confirmed_environment"`
	SensorMode           string `json:"sensor_mode"`
}

type RunResult struct {
	Status    string
	Operation OperationMetadata
	Output    string
	Error     string
	ExitCode  int
	HTTPCode  int
}

type RunDependencies struct {
	Executor            BruinCommandExecutor
	Execution           RunMaterializer
	CurrentPipelineIDs  func() []string
	ConfigPath          string
	WorkspaceRoot       string
	PolicyFor           func(environment string) policy.EnvironmentPolicy
	SelectedEnvironment func() string
}

// RunMaterializer is the canonical, completion-aware execution path used by
// the Build UI and delegated CLI runs. Keeping this dependency narrow lets the
// legacy JSON endpoint preserve its response contract while sharing target
// capture, write claims, and completion persistence with every other run path.
type RunMaterializer interface {
	MaterializeAssetStreamWithSensorMode(ctx context.Context, assetID, environment, scope, startDate, endDate string, fullRefresh, backfill bool, confirmedEnvironment, sensorMode string, onChunk func([]byte)) MaterializeResult
	MaterializePipelineStreamWithSensorMode(ctx context.Context, pipelineID, environment string, dryRun, fullRefresh, backfill bool, startDate, endDate, confirmedEnvironment, sensorMode string, onChunk func([]byte)) MaterializeResult
}

type RunService struct {
	deps RunDependencies
}

func NewRunService(deps RunDependencies) *RunService {
	return &RunService{deps: deps}
}

func (s *RunService) Execute(ctx context.Context, req RunRequest) RunResult {
	if req.AssetPath != "" && req.DryRun {
		return RunResult{Status: "error", Error: "asset dry-run is not supported; use pipeline dry-run", ExitCode: 1, HTTPCode: 400}
	}
	contextInput := runcontext.Input{
		Start:       req.StartDate,
		End:         req.EndDate,
		FullRefresh: req.FullRefresh,
		Backfill:    req.Backfill,
		SensorMode:  req.SensorMode,
	}
	normalizedContext, err := runcontext.Normalize(contextInput)
	if err != nil {
		return RunResult{Status: "error", Error: err.Error(), ExitCode: 1, HTTPCode: 400}
	}
	if err := runcontext.ValidateDryRun(req.DryRun, contextInput); err != nil {
		return RunResult{Status: "error", Error: err.Error(), ExitCode: 1, HTTPCode: 400}
	}
	req.StartDate = normalizedContext.StartString()
	req.EndDate = normalizedContext.EndString()
	req.SensorMode = normalizedContext.SensorMode
	if req.AssetPath != "" && req.Backfill {
		workspaceRoot := strings.TrimSpace(s.deps.WorkspaceRoot)
		if workspaceRoot == "" {
			return RunResult{Status: "error", Error: "asset resolution is not available for backfill", ExitCode: 1, HTTPCode: 400}
		}
		resolved, resolveErr := getDirectPipelineAndAssetReadOnly(ctx, workspaceRoot, req.AssetPath, afero.NewOsFs())
		if resolveErr != nil || resolved == nil || resolved.Asset == nil {
			return RunResult{Status: "error", Error: "asset could not be resolved for backfill", ExitCode: 1, HTTPCode: 400}
		}
		if !matlog.BackfillSafe(resolved.Asset) {
			return RunResult{Status: "error", Error: "asset materialization is not safe to backfill by independent execution windows", ExitCode: 1, HTTPCode: 400}
		}
	}

	req.Environment = strings.TrimSpace(req.Environment)
	if req.Environment == "" && s.deps.SelectedEnvironment != nil {
		req.Environment = strings.TrimSpace(s.deps.SelectedEnvironment())
	}
	if req.FullRefresh && strings.TrimSpace(s.deps.ConfigPath) != "" {
		if cfg, err := loadSelectedConfig(s.deps.ConfigPath, req.Environment); err == nil && selectedEnvironmentRestrictsFullRefresh(cfg) {
			req.FullRefresh = false
		}
	}
	if s.deps.PolicyFor != nil {
		if err := policy.Check(s.deps.PolicyFor(req.Environment), policy.RunRequest{
			Environment:          req.Environment,
			Interactive:          true,
			Destructive:          !req.DryRun && (req.FullRefresh || req.Backfill),
			ConfirmedEnvironment: strings.TrimSpace(req.ConfirmedEnvironment),
		}); err != nil {
			return RunResult{Status: "error", Error: err.Error(), ExitCode: 1, HTTPCode: 403}
		}
	}
	target := "."
	if req.PipelineID != "" {
		relPath, err := ResolvePipelineRunTarget(req.PipelineID)
		if err != nil {
			return RunResult{
				Status:   "error",
				Error:    "invalid pipeline id",
				ExitCode: 1,
				HTTPCode: 400,
			}
		}
		target = relPath
	}

	if req.AssetPath != "" {
		target = req.AssetPath
	}

	operation := runOperation(target, req.PipelineID, req.AssetPath, req.Environment)
	if !req.DryRun && s.deps.Execution != nil {
		if req.AssetPath == "" && req.PipelineID == "" {
			return s.materializeCurrentPipelines(ctx, req, operation)
		}

		var result MaterializeResult
		if req.AssetPath != "" {
			result = s.deps.Execution.MaterializeAssetStreamWithSensorMode(
				ctx,
				EncodeID(filepath.ToSlash(target)),
				req.Environment,
				string(MaterializeScopeAsset),
				req.StartDate,
				req.EndDate,
				req.FullRefresh,
				req.Backfill,
				req.ConfirmedEnvironment,
				req.SensorMode,
				nil,
			)
		} else {
			result = s.deps.Execution.MaterializePipelineStreamWithSensorMode(
				ctx,
				req.PipelineID,
				req.Environment,
				false,
				req.FullRefresh,
				req.Backfill,
				req.StartDate,
				req.EndDate,
				req.ConfirmedEnvironment,
				req.SensorMode,
				nil,
			)
		}
		return legacyRunResult(operation, result)
	}

	var output []byte
	var runErr error
	if req.AssetPath != "" {
		output, runErr = s.deps.Executor.RunAsset(ctx, RunAssetRequest{AssetPath: target, Environment: req.Environment, SensorMode: req.SensorMode, StartDate: req.StartDate, EndDate: req.EndDate, FullRefresh: req.FullRefresh}, nil)
	} else {
		output, runErr = s.deps.Executor.RunPipeline(ctx, RunPipelineRequest{Target: target, Environment: req.Environment, SensorMode: req.SensorMode, DryRun: req.DryRun, StartDate: req.StartDate, EndDate: req.EndDate, FullRefresh: req.FullRefresh}, nil)
	}

	if runErr != nil {
		return RunResult{
			Status:    "error",
			Operation: operation,
			Output:    string(output),
			Error:     runErr.Error(),
			ExitCode:  1,
			HTTPCode:  400,
		}
	}

	return RunResult{
		Status:    "ok",
		Operation: operation,
		Output:    string(output),
		ExitCode:  0,
		HTTPCode:  200,
	}
}

// materializeCurrentPipelines preserves the legacy POST /api/run {} meaning
// (run the workspace) without bypassing the completion-aware execution path.
// The dependency returns IDs in workspace order, so output is concatenated in
// the same deterministic order and execution stops at the first failure.
func (s *RunService) materializeCurrentPipelines(ctx context.Context, req RunRequest, operation OperationMetadata) RunResult {
	var pipelineIDs []string
	if s.deps.CurrentPipelineIDs != nil {
		pipelineIDs = s.deps.CurrentPipelineIDs()
	}

	orderedIDs := make([]string, 0, len(pipelineIDs))
	seen := make(map[string]struct{}, len(pipelineIDs))
	for _, pipelineID := range pipelineIDs {
		pipelineID = strings.TrimSpace(pipelineID)
		if pipelineID == "" {
			continue
		}
		if _, exists := seen[pipelineID]; exists {
			continue
		}
		seen[pipelineID] = struct{}{}
		orderedIDs = append(orderedIDs, pipelineID)
	}
	if len(orderedIDs) == 0 {
		return RunResult{
			Status:    "error",
			Operation: operation,
			Error:     "workspace has no pipelines to run",
			ExitCode:  1,
			HTTPCode:  400,
		}
	}

	var output strings.Builder
	for _, pipelineID := range orderedIDs {
		result := s.deps.Execution.MaterializePipelineStreamWithSensorMode(
			ctx,
			pipelineID,
			req.Environment,
			false,
			req.FullRefresh,
			req.Backfill,
			req.StartDate,
			req.EndDate,
			req.ConfirmedEnvironment,
			req.SensorMode,
			nil,
		)
		appendLegacyRunOutput(&output, result.Output)
		if result.Status != "ok" {
			legacy := legacyRunResult(operation, result)
			legacy.Output = output.String()
			return legacy
		}
	}

	return RunResult{
		Status:    "ok",
		Operation: operation,
		Output:    output.String(),
		ExitCode:  0,
		HTTPCode:  200,
	}
}

func appendLegacyRunOutput(output *strings.Builder, next string) {
	if next == "" {
		return
	}
	if output.Len() > 0 {
		current := output.String()
		if !strings.HasSuffix(current, "\n") && !strings.HasPrefix(next, "\n") {
			output.WriteByte('\n')
		}
	}
	output.WriteString(next)
}

func legacyRunResult(fallbackOperation OperationMetadata, result MaterializeResult) RunResult {
	httpCode := 200
	if result.Status != "ok" {
		httpCode = 400
	}
	return RunResult{
		Status:    result.Status,
		Operation: fallbackOperation,
		Output:    result.Output,
		Error:     result.Error,
		ExitCode:  result.ExitCode,
		HTTPCode:  httpCode,
	}
}
