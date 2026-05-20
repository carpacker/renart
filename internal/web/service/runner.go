// Package service provides the business logic layer for the Bruin web server.
package service

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"time"
)

type RunAssetRequest struct {
	AssetPath   string
	Environment string
}

type RunPipelineRequest struct {
	Target      string
	Environment string
	DryRun      bool
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
