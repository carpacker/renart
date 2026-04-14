package service

import (
	"context"
	"fmt"
)

type RunRequest struct {
	Command    string   `json:"command"`
	PipelineID string   `json:"pipeline_id"`
	AssetPath  string   `json:"asset_path"`
	Args       []string `json:"args"`
}

type RunResult struct {
	Status   string
	Command  []string
	Output   string
	Error    string
	ExitCode int
	HTTPCode int
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
	command := req.Command
	if command == "" {
		command = "run"
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

	var (
		output  []byte
		err     error
		cmdArgs []string
	)

	switch command {
	case "run":
		if req.AssetPath != "" {
			cmdArgs = []string{"run", target}
			output, err = s.deps.Executor.RunAsset(ctx, RunAssetRequest{AssetPath: target}, nil)
		} else {
			cmdArgs = []string{"run", target}
			output, err = s.deps.Executor.RunPipeline(ctx, RunPipelineRequest{Target: target}, nil)
		}
	default:
		cmdArgs = append([]string{command, target}, req.Args...)
		output, err = runLegacyCLICommand(ctx, s.deps.Executor, command, target, req.Args)
	}
	if err != nil {
		return RunResult{
			Status:   "error",
			Command:  cmdArgs,
			Output:   string(output),
			Error:    err.Error(),
			ExitCode: 1,
			HTTPCode: 400,
		}
	}

	return RunResult{
		Status:   "ok",
		Command:  cmdArgs,
		Output:   string(output),
		ExitCode: 0,
		HTTPCode: 200,
	}
}

var allowedRunCommands = map[string]bool{
	"run":    true,
	"query":  true,
	"patch":  true,
	"lint":   true,
	"format": true,
	"import": true,
}

func IsRunCommandAllowed(command string) bool {
	return allowedRunCommands[command]
}

func runLegacyCLICommand(ctx context.Context, executor BruinCommandExecutor, command, target string, extraArgs []string) ([]byte, error) {
	switch command {
	case "format":
		useSQLFluff := false
		for _, arg := range extraArgs {
			if arg == "--sqlfluff" {
				useSQLFluff = true
				break
			}
		}
		return executor.FormatAsset(ctx, FormatAssetRequest{AssetPath: target, UseSQLFluff: useSQLFluff})
	case "patch":
		if len(extraArgs) > 0 {
			return executor.ApplyPatch(ctx, PatchRequest{Operation: target, TargetPath: extraArgs[len(extraArgs)-1]})
		}
	}

	cliExecutor, ok := executor.(*CLIBruinExecutor)
	if !ok {
		return nil, fmt.Errorf("command %q is not supported by this executor", command)
	}

	return cliExecutor.run(ctx, append([]string{command, target}, extraArgs...))
}
