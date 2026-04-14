// Package service provides the business logic layer for the Bruin web server.
package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type RunAssetRequest struct {
	AssetPath string
}

type RunPipelineRequest struct {
	Target string
}

type QueryAssetRequest struct {
	AssetPath   string
	Limit       string
	Environment string
	ConfigFile  string
	Output      string
}

type QueryConnectionRequest struct {
	ConnectionName string
	Query          string
	Environment    string
	Output         string
}

type FormatAssetRequest struct {
	AssetPath   string
	UseSQLFluff bool
}

type PatchRequest struct {
	Operation  string
	TargetPath string
}

type ImportDatabaseRequest struct {
	PipelinePath   string
	ConnectionName string
	Schema         string
	Schemas        []string
	Tables         []string
	DisableColumns bool
	Environment    string
	ConfigFilePath string
}

// BruinCommandExecutor executes Bruin operations through typed methods.
// Implementations can shell out to the Bruin CLI today and later call the
// corresponding Go APIs directly.
type BruinCommandExecutor interface {
	RunAsset(ctx context.Context, req RunAssetRequest, onChunk func([]byte)) ([]byte, error)
	RunPipeline(ctx context.Context, req RunPipelineRequest, onChunk func([]byte)) ([]byte, error)
	QueryAsset(ctx context.Context, req QueryAssetRequest) ([]byte, error)
	QueryConnection(ctx context.Context, req QueryConnectionRequest) ([]byte, error)
	FormatAsset(ctx context.Context, req FormatAssetRequest) ([]byte, error)
	ApplyPatch(ctx context.Context, req PatchRequest) ([]byte, error)
	ImportDatabase(ctx context.Context, req ImportDatabaseRequest) ([]byte, error)
	RunWithRetry(ctx context.Context, req QueryAssetRequest, retries int, initialDelay time.Duration) ([]byte, error, int)
}

// CLIBruinExecutor implements BruinCommandExecutor using subprocess execution.
type CLIBruinExecutor struct {
	WorkspaceRoot string
	BinaryPath    string
}

func NewCLIBruinExecutor(workspaceRoot, binaryPath string) *CLIBruinExecutor {
	trimmedBinaryPath := strings.TrimSpace(binaryPath)
	if trimmedBinaryPath == "" {
		trimmedBinaryPath = resolveDefaultBruinBinary(workspaceRoot)
	}

	return &CLIBruinExecutor{
		WorkspaceRoot: workspaceRoot,
		BinaryPath:    trimmedBinaryPath,
	}
}

func resolveDefaultBruinBinary(workspaceRoot string) string {
	candidates := []string{}
	appendCandidate := func(path string) {
		if strings.TrimSpace(path) != "" {
			candidates = append(candidates, path)
		}
	}

	appendCandidate(filepath.Join(workspaceRoot, "bruin"))
	appendCandidate(filepath.Join(filepath.Dir(workspaceRoot), "bruin"))

	if cwd, err := os.Getwd(); err == nil {
		appendCandidate(filepath.Join(cwd, "bruin"))
		appendCandidate(filepath.Join(filepath.Dir(cwd), "bruin"))
	}

	if executablePath, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executablePath)
		appendCandidate(filepath.Join(executableDir, "bruin"))
		appendCandidate(filepath.Join(filepath.Dir(executableDir), "bruin"))
	}

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}

	return "bruin"
}

func (r *CLIBruinExecutor) RunAsset(ctx context.Context, req RunAssetRequest, onChunk func([]byte)) ([]byte, error) {
	return r.runMaybeStreaming(ctx, []string{"run", req.AssetPath}, onChunk)
}

func (r *CLIBruinExecutor) RunPipeline(ctx context.Context, req RunPipelineRequest, onChunk func([]byte)) ([]byte, error) {
	return r.runMaybeStreaming(ctx, []string{"run", req.Target}, onChunk)
}

func (r *CLIBruinExecutor) QueryAsset(ctx context.Context, req QueryAssetRequest) ([]byte, error) {
	args := []string{"query", "--asset", req.AssetPath}
	if strings.TrimSpace(req.Output) != "" {
		args = append(args, "--output", req.Output)
	}
	if strings.TrimSpace(req.Limit) != "" {
		args = append(args, "--limit", req.Limit)
	}
	if strings.TrimSpace(req.Environment) != "" {
		args = append(args, "--environment", req.Environment)
	}
	if strings.TrimSpace(req.ConfigFile) != "" {
		args = append(args, "--config-file", req.ConfigFile)
	}
	return r.run(ctx, args)
}

func (r *CLIBruinExecutor) QueryConnection(ctx context.Context, req QueryConnectionRequest) ([]byte, error) {
	args := []string{"query", "--connection", req.ConnectionName, "--query", req.Query}
	if strings.TrimSpace(req.Output) != "" {
		args = append(args, "--output", req.Output)
	}
	if strings.TrimSpace(req.Environment) != "" {
		args = append(args, "--environment", req.Environment)
	}
	return r.run(ctx, args)
}

func (r *CLIBruinExecutor) FormatAsset(ctx context.Context, req FormatAssetRequest) ([]byte, error) {
	args := []string{"format", req.AssetPath}
	if req.UseSQLFluff {
		args = append(args, "--sqlfluff")
	}
	return r.run(ctx, args)
}

func (r *CLIBruinExecutor) ApplyPatch(ctx context.Context, req PatchRequest) ([]byte, error) {
	return r.run(ctx, []string{"patch", req.Operation, req.TargetPath})
}

func (r *CLIBruinExecutor) ImportDatabase(ctx context.Context, req ImportDatabaseRequest) ([]byte, error) {
	args := []string{"import", "database", "--connection", req.ConnectionName}
	if strings.TrimSpace(req.Schema) != "" {
		args = append(args, "--schema", req.Schema)
	}
	for _, schema := range req.Schemas {
		if trimmed := strings.TrimSpace(schema); trimmed != "" {
			args = append(args, "--schemas", trimmed)
		}
	}
	for _, table := range req.Tables {
		if trimmed := strings.TrimSpace(table); trimmed != "" {
			args = append(args, "--table", trimmed)
		}
	}
	if req.DisableColumns {
		args = append(args, "--no-columns")
	}
	if strings.TrimSpace(req.Environment) != "" {
		args = append(args, "--environment", req.Environment)
	}
	if strings.TrimSpace(req.ConfigFilePath) != "" {
		args = append(args, "--config-file", req.ConfigFilePath)
	}
	args = append(args, req.PipelinePath)
	return r.run(ctx, args)
}

func (r *CLIBruinExecutor) RunWithRetry(
	ctx context.Context,
	req QueryAssetRequest,
	retries int,
	initialDelay time.Duration,
) ([]byte, error, int) {
	attempt := 0
	delay := initialDelay
	for {
		attempt++
		output, err := r.QueryAsset(ctx, req)
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

func (r *CLIBruinExecutor) run(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.BinaryPath, args...)
	cmd.Dir = r.WorkspaceRoot
	cmd.Env = append(os.Environ(), "TELEMETRY_OPTOUT=1")
	return cmd.CombinedOutput()
}

func (r *CLIBruinExecutor) runMaybeStreaming(ctx context.Context, args []string, onChunk func([]byte)) ([]byte, error) {
	if onChunk == nil {
		return r.run(ctx, args)
	}

	cmd := exec.CommandContext(ctx, r.BinaryPath, args...)
	cmd.Dir = r.WorkspaceRoot
	cmd.Env = append(os.Environ(), "TELEMETRY_OPTOUT=1")

	buffer := bytes.NewBuffer(nil)
	writer := &streamCaptureWriter{onChunk: onChunk, buffer: buffer}
	cmd.Stdout = writer
	cmd.Stderr = writer

	err := cmd.Run()
	return buffer.Bytes(), err
}

func (r *CLIBruinExecutor) String() string {
	return fmt.Sprintf("cli(%s)", r.BinaryPath)
}

func IsDuckDBLockError(err error, output []byte) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error() + "\n" + string(output))
	return strings.Contains(message, "could not set lock on file") ||
		strings.Contains(message, "conflicting lock is held")
}

func AppendDuckDBReadOnlyMode(path string) string {
	if path == "" {
		return path
	}

	lower := strings.ToLower(path)
	if strings.Contains(lower, "access_mode=read_only") || strings.HasPrefix(lower, "md:") {
		return path
	}

	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}

	return path + separator + "access_mode=read_only"
}

type streamCaptureWriter struct {
	mu      sync.Mutex
	buffer  *bytes.Buffer
	onChunk func([]byte)
}

func (w *streamCaptureWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.buffer.Write(p); err != nil {
		return 0, err
	}

	if w.onChunk != nil {
		chunk := append([]byte(nil), p...)
		w.onChunk(chunk)
	}

	return len(p), nil
}

var _ io.Writer = (*streamCaptureWriter)(nil)
