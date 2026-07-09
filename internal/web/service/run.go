package service

import "context"

type RunRequest struct {
	PipelineID  string `json:"pipeline_id"`
	AssetPath   string `json:"asset_path"`
	Environment string `json:"environment"`
	DryRun      bool   `json:"dry_run"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	FullRefresh bool   `json:"full_refresh"`
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
	Executor BruinCommandExecutor
}

type RunService struct {
	deps RunDependencies
}

func NewRunService(deps RunDependencies) *RunService {
	return &RunService{deps: deps}
}

func (s *RunService) Execute(ctx context.Context, req RunRequest) RunResult {
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

	var output []byte
	var err error
	if req.AssetPath != "" {
		output, err = s.deps.Executor.RunAsset(ctx, RunAssetRequest{AssetPath: target, Environment: req.Environment, StartDate: req.StartDate, EndDate: req.EndDate, FullRefresh: req.FullRefresh}, nil)
	} else {
		output, err = s.deps.Executor.RunPipeline(ctx, RunPipelineRequest{Target: target, Environment: req.Environment, DryRun: req.DryRun, StartDate: req.StartDate, EndDate: req.EndDate, FullRefresh: req.FullRefresh}, nil)
	}

	if err != nil {
		return RunResult{
			Status:    "error",
			Operation: operation,
			Output:    string(output),
			Error:     err.Error(),
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
