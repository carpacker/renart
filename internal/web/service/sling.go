package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	bruinexecutor "github.com/bruin-data/bruin/pkg/executor"
	"github.com/bruin-data/bruin/pkg/pipeline"
	bruinuv "github.com/bruin-data/bruin/pkg/uv"
	"gopkg.in/yaml.v3"
)

const slingAssetType = "sling"

// ingestrURIConnection is the bruin connection capability that yields a standard
// connection URI (e.g. postgresql://…, s3://…, duckdb://…). The method name comes
// from bruin; the URI it returns is a plain DSN that the Sling CLI also resolves,
// so renart reuses it to bridge a named bruin connection to a Sling --src-conn /
// --tgt-conn value (and to feed `sling conns discover`).
type ingestrURIConnection interface {
	GetIngestrURI() (string, error)
}

// slingConnectionURI resolves a named bruin connection to a Sling-usable
// connection URI. It works for any direction (source, target, discovery).
func slingConnectionURI(manager config.ConnectionGetter, connectionName string) (string, error) {
	if manager == nil {
		return "", errors.New("connection manager is required")
	}
	name := strings.TrimSpace(connectionName)
	conn := manager.GetConnection(name)
	if conn == nil {
		return "", fmt.Errorf("connection %q was not found", name)
	}
	if uriGetter, ok := conn.(ingestrURIConnection); ok {
		uri, err := uriGetter.GetIngestrURI()
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(uri) == "" {
			return "", fmt.Errorf("connection %q produced an empty URI", name)
		}
		return uri, nil
	}
	if raw, ok := conn.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw), nil
	}
	return "", fmt.Errorf("connection %q cannot be converted to a Sling connection URI", name)
}

var slingUvChecker = &bruinuv.Checker{}

func isSlingAssetType(assetType string) bool {
	return strings.EqualFold(strings.TrimSpace(assetType), slingAssetType)
}

func isSlingAsset(asset *pipeline.Asset) bool {
	return asset != nil && isSlingAssetType(string(asset.Type))
}

func isSlingExecutablePath(path string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), ".sling.yml") ||
		strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), ".sling.yaml")
}

// Flat parameter keys for a Sling asset. They live under the asset's
// `parameters:` (bruin parses that as map[string]string, so they must be flat),
// which keeps a Sling asset a single bruin-loadable .asset.yml — no .sling.yml
// replication sidecar.
const (
	slingParamSourceConnection      = "source_connection"
	slingParamSourceTable           = "source_table"
	slingParamDestinationConnection = "destination_connection"
	slingParamDestinationTable      = "destination_table"
	slingParamMode                  = "mode"
)

// slingRunParams is the resolved, flat replication intent of a Sling asset.
type slingRunParams struct {
	SourceConnection      string
	SourceTable           string
	DestinationConnection string
	DestinationTable      string
	Mode                  string
}

// slingParamsFromAsset reads the flat replication parameters off an asset.
func slingParamsFromAsset(asset *pipeline.Asset) slingRunParams {
	params := slingRunParams{}
	if asset == nil {
		return params
	}
	get := func(key string) string { return strings.TrimSpace(asset.Parameters[key]) }
	params.SourceConnection = get(slingParamSourceConnection)
	params.SourceTable = get(slingParamSourceTable)
	params.DestinationConnection = get(slingParamDestinationConnection)
	params.DestinationTable = get(slingParamDestinationTable)
	params.Mode = get(slingParamMode)
	if params.DestinationTable == "" {
		params.DestinationTable = strings.TrimSpace(asset.Name)
	}
	return params
}

// slingLocalConnectionName is the synthetic "connection" that marks a Sling
// source or target as a local file on the same machine. It is NOT a bruin
// connection: the file path lives in the corresponding _table parameter and the
// run omits --src-conn/--tgt-conn, letting Sling resolve the file:// stream.
const slingLocalConnectionName = "local"

func isLocalSlingConnection(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), slingLocalConnectionName)
}

// slingFileStreamURI turns a (possibly workspace-relative) file path into a
// file:// URI Sling can read/write. Paths that already carry a scheme (file://,
// s3://, …) pass through unchanged.
func slingFileStreamURI(workspaceRoot, rawPath string) string {
	path := strings.TrimSpace(rawPath)
	if strings.Contains(path, "://") {
		return path
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspaceRoot, path)
	}
	return "file://" + filepath.ToSlash(path)
}

// slingSourceArgs builds the --src-* flags for a run: a file:// stream (no
// connection) for a local source, otherwise the bridged connection URI + stream.
func (e *HybridBruinExecutor) slingSourceArgs(manager config.ConnectionGetter, params slingRunParams) ([]string, error) {
	if isLocalSlingConnection(params.SourceConnection) {
		if params.SourceTable == "" {
			return nil, errors.New("a local sling source requires a source_table file path")
		}
		return []string{"--src-stream", slingFileStreamURI(e.workspaceRoot, params.SourceTable)}, nil
	}
	if params.SourceConnection == "" {
		return nil, errors.New("sling asset requires a source_connection parameter")
	}
	if params.SourceTable == "" {
		return nil, errors.New("sling asset requires a source_table parameter")
	}
	uri, err := slingConnectionURI(manager, params.SourceConnection)
	if err != nil {
		return nil, err
	}
	return []string{"--src-conn", uri, "--src-stream", params.SourceTable}, nil
}

// slingTargetArgs builds the --tgt-* flags for a run: a file:// object (no
// connection) for a local target, otherwise the bridged connection URI + object.
func (e *HybridBruinExecutor) slingTargetArgs(manager config.ConnectionGetter, params slingRunParams) ([]string, error) {
	if isLocalSlingConnection(params.DestinationConnection) {
		if params.DestinationTable == "" {
			return nil, errors.New("a local sling target requires a destination_table file path")
		}
		return []string{"--tgt-object", slingFileStreamURI(e.workspaceRoot, params.DestinationTable)}, nil
	}
	if params.DestinationConnection == "" {
		return nil, errors.New("sling asset requires a destination_connection parameter")
	}
	uri, err := slingConnectionURI(manager, params.DestinationConnection)
	if err != nil {
		return nil, err
	}
	return []string{"--tgt-conn", uri, "--tgt-object", params.DestinationTable}, nil
}

// hasSlingFlatParams reports whether the asset carries the flat-parameter
// replication shape (vs. a legacy .sling.yml replication sidecar).
func hasSlingFlatParams(asset *pipeline.Asset) bool {
	if asset == nil {
		return false
	}
	params := slingParamsFromAsset(asset)
	return params.SourceConnection != "" || params.SourceTable != "" || params.DestinationConnection != ""
}

// defaultSlingAssetContent is the single-file flat-parameter skeleton for a new
// Sling asset. The name is intentionally omitted (inferred from the filename).
func defaultSlingAssetContent(assetName string) string {
	leaf := assetNameLeafPath(assetName)
	return fmt.Sprintf(`type: sling

parameters:
  source_connection: your_source_connection
  source_table: public.%s
  destination_connection: your_destination_connection
  destination_table: public.%s
  mode: full-refresh
`, leaf, leaf)
}

func slingSummaryParameters(content string) map[string]string {
	type slingReplicationSummary struct {
		Source  string         `yaml:"source"`
		Target  string         `yaml:"target"`
		Streams map[string]any `yaml:"streams"`
	}
	var summary slingReplicationSummary
	if err := yaml.Unmarshal([]byte(content), &summary); err != nil {
		return nil
	}
	params := map[string]string{}
	if strings.TrimSpace(summary.Source) != "" {
		params["source"] = strings.TrimSpace(summary.Source)
	}
	if strings.TrimSpace(summary.Target) != "" {
		params["target"] = strings.TrimSpace(summary.Target)
	}
	if len(summary.Streams) > 0 {
		streamNames := make([]string, 0, len(summary.Streams))
		for name := range summary.Streams {
			streamNames = append(streamNames, name)
		}
		sort.Strings(streamNames)
		params["streams"] = strings.Join(streamNames, ", ")
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

func slingBinaryPath() string {
	if value := strings.TrimSpace(os.Getenv("RENART_SLING_BINARY")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("SLING_BINARY")); value != "" {
		return value
	}
	return ""

}

func slingPackageName() string {
	if value := strings.TrimSpace(os.Getenv("RENART_SLING_PACKAGE")); value != "" {
		return value
	}
	return "sling"
}

func slingUvBinaryPath(ctx context.Context, output io.Writer) (string, error) {
	if value := strings.TrimSpace(os.Getenv("RENART_UV_BINARY")); value != "" {
		return value, nil
	}
	if output != nil {
		ctx = context.WithValue(ctx, bruinexecutor.KeyPrinter, output)
	}
	return slingUvChecker.EnsureUvInstalled(ctx)
}

func slingCommand(ctx context.Context, slingArgs []string, output io.Writer) (string, []string, error) {
	if binaryPath := slingBinaryPath(); binaryPath != "" {
		return binaryPath, slingArgs, nil
	}
	uvBinaryPath, err := slingUvBinaryPath(ctx, output)
	if err != nil {
		return "", nil, err
	}
	cmdline := []string{
		"tool",
		"run",
		"--no-config",
		"--python",
		"3.11",
		"--from",
		slingPackageName(),
		"sling",
	}
	return uvBinaryPath, append(cmdline, slingArgs...), nil
}

func slingRunModeArgs(ctx context.Context) []string {
	fullRefresh, _ := ctx.Value(pipeline.RunConfigFullRefresh).(bool)
	if !fullRefresh {
		return nil
	}
	return []string{"--mode", "full-refresh"}
}

func newStreamingCommand(ctx context.Context, name string, args []string, dir string, writer io.Writer) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), slingRunEnv(ctx)...)
	return cmd
}

func slingRunEnv(ctx context.Context) []string {
	env := []string{"SLING_DISABLE_TELEMETRY=true", "PYTHONUNBUFFERED=1"}
	if start, ok := ctx.Value(pipeline.RunConfigStartDate).(time.Time); ok && !start.IsZero() {
		env = append(env, "START_DATE="+start.UTC().Format(time.RFC3339))
	}
	if end, ok := ctx.Value(pipeline.RunConfigEndDate).(time.Time); ok && !end.IsZero() {
		env = append(env, "END_DATE="+end.UTC().Format(time.RFC3339))
	}
	return env
}

func runStreamingCommand(cmd *exec.Cmd, writer *streamCaptureWriter) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 2)
	go func() {
		_, copyErr := io.Copy(writer, stdout)
		done <- copyErr
	}()
	go func() {
		_, copyErr := io.Copy(writer, stderr)
		done <- copyErr
	}()
	copyErr1 := <-done
	copyErr2 := <-done
	waitErr := cmd.Wait()
	if copyErr1 != nil {
		return copyErr1
	}
	if copyErr2 != nil {
		return copyErr2
	}
	return waitErr
}

func (e *HybridBruinExecutor) runSlingAsset(ctx context.Context, asset *pipeline.Asset, manager config.ConnectionGetter, onChunk func([]byte)) ([]byte, error) {
	if asset == nil {
		return nil, errors.New("sling asset is required")
	}
	writer := &streamCaptureWriter{buffer: bytes.NewBuffer(nil), onChunk: onChunk}

	// Legacy: a .sling.yml replication sidecar the asset points at via `run`.
	if isSlingExecutablePath(asset.ExecutableFile.Path) {
		return e.runSlingReplicationFile(ctx, asset.ExecutableFile.Path, writer)
	}

	// Flat-parameter Sling asset: run directly from source/target connection
	// URIs, bridging the named bruin connections — no replication file needed. A
	// "local" connection is a local file rather than a bruin connection.
	params := slingParamsFromAsset(asset)
	srcArgs, err := e.slingSourceArgs(manager, params)
	if err != nil {
		return writer.buffer.Bytes(), err
	}
	tgtArgs, err := e.slingTargetArgs(manager, params)
	if err != nil {
		return writer.buffer.Bytes(), err
	}

	args := append([]string{"run"}, srcArgs...)
	args = append(args, tgtArgs...)
	args = append(args, slingModeArgsFromParams(ctx, params)...)

	cmdName, cmdArgs, err := slingCommand(ctx, args, writer)
	if err != nil {
		return writer.buffer.Bytes(), err
	}
	cmd := newStreamingCommand(ctx, cmdName, cmdArgs, e.workspaceRoot, writer)
	if err := runStreamingCommand(cmd, writer); err != nil {
		return writer.buffer.Bytes(), err
	}
	return writer.buffer.Bytes(), nil
}

// runSlingReplicationFile runs a legacy Sling asset defined by a .sling.yml
// replication sidecar via `sling run -r`.
func (e *HybridBruinExecutor) runSlingReplicationFile(ctx context.Context, replicationPath string, writer *streamCaptureWriter) ([]byte, error) {
	cmdDir := e.workspaceRoot
	if pipelineRoot, err := findPipelineRootForAsset(replicationPath); err == nil {
		cmdDir = pipelineRoot
	}
	cmdPath := replicationPath
	if rel, err := filepath.Rel(cmdDir, replicationPath); err == nil && !strings.HasPrefix(rel, "..") {
		cmdPath = filepath.ToSlash(rel)
	}
	args := append([]string{"run", "-r", cmdPath}, slingRunModeArgs(ctx)...)
	cmdName, cmdArgs, err := slingCommand(ctx, args, writer)
	if err != nil {
		return writer.buffer.Bytes(), err
	}
	cmd := newStreamingCommand(ctx, cmdName, cmdArgs, cmdDir, writer)
	if err := runStreamingCommand(cmd, writer); err != nil {
		return writer.buffer.Bytes(), err
	}
	return writer.buffer.Bytes(), nil
}

// slingModeArgsFromParams resolves the Sling --mode flag. An explicit
// full-refresh from the run config wins; otherwise the asset's mode parameter is
// passed through (full-refresh is Sling's default, so it needs no flag).
func slingModeArgsFromParams(ctx context.Context, params slingRunParams) []string {
	if args := slingRunModeArgs(ctx); len(args) > 0 {
		return args
	}
	mode := strings.TrimSpace(params.Mode)
	if mode == "" || strings.EqualFold(mode, "full-refresh") {
		return nil
	}
	return []string{"--mode", mode}
}
