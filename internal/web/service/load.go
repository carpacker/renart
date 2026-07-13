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
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	bruinexecutor "github.com/bruin-data/bruin/pkg/executor"
	"github.com/bruin-data/bruin/pkg/pipeline"
	bruinuv "github.com/bruin-data/bruin/pkg/uv"
	"gopkg.in/yaml.v3"
)

const loadAssetType = "load"

// ingestrURIConnection is the bruin connection capability that yields a standard
// connection URI (e.g. postgresql://…, s3://…, duckdb://…). The method name comes
// from bruin; the URI it returns is a plain DSN that the Sling CLI also resolves,
// so renart reuses it to bridge a named bruin connection to a Sling --src-conn /
// --tgt-conn value (and to feed `sling conns discover`).
type ingestrURIConnection interface {
	GetIngestrURI() (string, error)
}

// loadConnectionURI resolves a named bruin connection to a Sling-usable
// connection URI. It works for any direction (source, target, discovery).
func loadConnectionURI(manager config.ConnectionGetter, connectionName string) (string, error) {
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
	return "", fmt.Errorf("connection %q cannot be converted to a Load connection URI", name)
}

var loadUvChecker = &bruinuv.Checker{}

func isLoadAssetType(assetType string) bool {
	return strings.EqualFold(strings.TrimSpace(assetType), loadAssetType)
}

func isLoadAsset(asset *pipeline.Asset) bool {
	return asset != nil && isLoadAssetType(string(asset.Type))
}

// Flat parameter keys for a Load asset. They live under the asset's
// `parameters:` (bruin parses that as map[string]string, so they must be flat),
// which keeps a Load asset a single bruin-loadable .asset.yml — no .sling.yml
// replication sidecar.
const (
	loadParamSourceConnection  = "source_connection"
	loadParamSourceTable       = "source_table"
	loadParamDestinationObject = "destination_object"
)

// loadRunParams is the resolved, flat replication intent of a Load asset.
type loadRunParams struct {
	SourceConnection      string
	SourceTable           string
	DestinationConnection string
	DestinationObject     string
	AssetName             string
}

// loadParamsFromAsset reads the flat replication parameters off an asset.
func loadParamsFromAsset(asset *pipeline.Asset) loadRunParams {
	params := loadRunParams{}
	if asset == nil {
		return params
	}
	get := func(key string) string {
		value, _ := asset.Parameters.GetString(key)
		return strings.TrimSpace(value)
	}
	params.SourceConnection = get(loadParamSourceConnection)
	params.SourceTable = get(loadParamSourceTable)
	params.DestinationConnection = strings.TrimSpace(asset.Connection)
	params.DestinationObject = get(loadParamDestinationObject)
	params.AssetName = strings.TrimSpace(asset.Name)
	return params
}

func resolvedLoadParams(asset *pipeline.Asset, pl *pipeline.Pipeline) (loadRunParams, error) {
	params := loadParamsFromAsset(asset)
	connectionName, err := loadConnectionNameForAsset(asset, pl)
	if err != nil {
		return params, err
	}
	params.DestinationConnection = connectionName
	return params, nil
}

// loadLocalConnectionName is the synthetic "connection" that marks a Load
// source or target as a local file on the same machine. It is NOT a bruin
// connection: the file path lives in the corresponding _table parameter and the
// run omits --src-conn/--tgt-conn, letting Load resolve the file:// stream.
const loadLocalConnectionName = "local"

func isLocalLoadConnection(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), loadLocalConnectionName)
}

// loadFileStreamURI turns a (possibly workspace-relative) file path into a
// file:// URI Load can read/write. Paths that already carry a scheme (file://,
// s3://, …) pass through unchanged.
func loadFileStreamURI(workspaceRoot, rawPath string) string {
	path := strings.TrimSpace(rawPath)
	if strings.Contains(path, "://") {
		return path
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspaceRoot, path)
	}
	return "file://" + filepath.ToSlash(path)
}

// loadSourceArgs builds the --src-* flags for a run: a file:// stream (no
// connection) for a local source, otherwise the bridged connection URI + stream.
func (e *HybridBruinExecutor) loadSourceArgs(manager config.ConnectionGetter, params loadRunParams) ([]string, error) {
	if isLocalLoadConnection(params.SourceConnection) {
		if params.SourceTable == "" {
			return nil, errors.New("a local load source requires a source_table file path")
		}
		return []string{"--src-stream", loadFileStreamURI(e.workspaceRoot, params.SourceTable)}, nil
	}
	if params.SourceConnection == "" {
		return nil, errors.New("load asset requires a source_connection parameter")
	}
	if params.SourceTable == "" {
		return nil, errors.New("load asset requires a source_table parameter")
	}
	uri, err := loadConnectionURI(manager, params.SourceConnection)
	if err != nil {
		return nil, err
	}
	return []string{"--src-conn", uri, "--src-stream", params.SourceTable}, nil
}

// loadTargetArgs builds the --tgt-* flags for a run. Database destinations are
// always named after the asset; file and object-storage destinations use the
// explicit destination_object parameter.
func (e *HybridBruinExecutor) loadTargetArgs(manager config.ConnectionGetter, params loadRunParams) ([]string, error) {
	if isLocalLoadConnection(params.DestinationConnection) {
		if params.DestinationObject == "" {
			return nil, errors.New("a local load target requires a destination_object file path")
		}
		return []string{"--tgt-object", loadFileStreamURI(e.workspaceRoot, params.DestinationObject)}, nil
	}
	if params.DestinationConnection == "" {
		return nil, errors.New("load asset requires a target connection")
	}
	uri, err := loadConnectionURI(manager, params.DestinationConnection)
	if err != nil {
		return nil, err
	}
	targetObject := strings.TrimSpace(params.AssetName)
	if details, ok := manager.(config.ConnectionDetailsGetter); ok {
		connectionType := details.GetConnectionType(params.DestinationConnection)
		switch loadConnectionCategory(connectionType) {
		case LoadCategoryDatabase:
			// The database table is the asset's canonical name.
		case LoadCategoryStorage, LoadCategoryFile:
			targetObject = strings.TrimSpace(params.DestinationObject)
			if targetObject == "" {
				return nil, fmt.Errorf("load target connection %q requires a destination_object", params.DestinationConnection)
			}
		default:
			return nil, fmt.Errorf("connection %q is not a supported Load target", params.DestinationConnection)
		}
	} else if destinationObject := strings.TrimSpace(params.DestinationObject); destinationObject != "" {
		// Minimal test/custom managers cannot report a category. Preserve their
		// ability to target an explicit object while real managers enforce the
		// database-vs-storage distinction above.
		targetObject = destinationObject
	}
	if targetObject == "" {
		return nil, errors.New("load asset requires a name for its destination table")
	}
	return []string{"--tgt-conn", uri, "--tgt-object", targetObject}, nil
}

type loadAssetYAML struct {
	Type            string                       `yaml:"type"`
	Connection      string                       `yaml:"connection,omitempty"`
	Depends         []string                     `yaml:"depends,omitempty"`
	Parameters      loadAssetParametersYAML      `yaml:"parameters"`
	Materialization loadAssetMaterializationYAML `yaml:"materialization"`
}

type loadAssetParametersYAML struct {
	SourceConnection  string `yaml:"source_connection"`
	SourceTable       string `yaml:"source_table"`
	DestinationObject string `yaml:"destination_object,omitempty"`
}

type loadAssetMaterializationYAML struct {
	Type     string `yaml:"type"`
	Strategy string `yaml:"strategy"`
}

func renderLoadAssetContent(connection, sourceConnection, sourceTable, destinationObject string, depends []string) (string, error) {
	if strings.TrimSpace(sourceConnection) == "" {
		return "", errors.New("load asset requires a source connection")
	}
	if strings.TrimSpace(sourceTable) == "" {
		return "", errors.New("load asset requires a source table or object")
	}
	if isLocalLoadConnection(connection) && strings.TrimSpace(destinationObject) == "" {
		return "", errors.New("a local load target requires a destination object")
	}
	definition := loadAssetYAML{
		Type:       loadAssetType,
		Connection: strings.TrimSpace(connection),
		Depends:    depends,
		Parameters: loadAssetParametersYAML{
			SourceConnection:  strings.TrimSpace(sourceConnection),
			SourceTable:       strings.TrimSpace(sourceTable),
			DestinationObject: strings.TrimSpace(destinationObject),
		},
		Materialization: loadAssetMaterializationYAML{
			Type:     "table",
			Strategy: "create+replace",
		},
	}
	content, err := yaml.Marshal(definition)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// defaultLoadAssetContent is used by non-interactive callers that need a
// starter document. The create dialog sends concrete semantic fields instead.
func defaultLoadAssetContent(assetName string) string {
	leaf := assetNameLeafPath(assetName)
	content, _ := renderLoadAssetContent(
		"your_destination_connection",
		"your_source_connection",
		"public."+leaf,
		"",
		nil,
	)
	return content
}

func loadBinaryPath() string {
	if value := strings.TrimSpace(os.Getenv("RENART_SLING_BINARY")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("SLING_BINARY")); value != "" {
		return value
	}
	return ""

}

func loadPackageName() string {
	if value := strings.TrimSpace(os.Getenv("RENART_SLING_PACKAGE")); value != "" {
		return value
	}
	return "sling"
}

func loadUvBinaryPath(ctx context.Context, output io.Writer) (string, error) {
	if value := strings.TrimSpace(os.Getenv("RENART_UV_BINARY")); value != "" {
		return value, nil
	}
	if output != nil {
		ctx = context.WithValue(ctx, bruinexecutor.KeyPrinter, output)
	}
	return loadUvChecker.EnsureUvInstalled(ctx)
}

func loadCommand(ctx context.Context, loadArgs []string, output io.Writer) (string, []string, error) {
	if binaryPath := loadBinaryPath(); binaryPath != "" {
		return binaryPath, loadArgs, nil
	}
	uvBinaryPath, err := loadUvBinaryPath(ctx, output)
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
		loadPackageName(),
		"sling",
	}
	return uvBinaryPath, append(cmdline, loadArgs...), nil
}

func loadRunModeArgs(ctx context.Context) []string {
	fullRefresh, _ := ctx.Value(pipeline.RunConfigFullRefresh).(bool)
	if !fullRefresh {
		return nil
	}
	return []string{"--mode", "full-refresh"}
}

// slingMaterializationArgs maps Renart/Bruin materialization intent to Sling's
// loader flags. This is shared by native Load and HTTP API assets so the
// workbench never offers a strategy that silently executes as full refresh.
func slingMaterializationArgs(ctx context.Context, asset *pipeline.Asset) ([]string, error) {
	if asset == nil {
		return nil, errors.New("asset is required to resolve materialization")
	}
	if args := loadRunModeArgs(ctx); len(args) > 0 {
		if asset.RefreshRestricted == nil || !*asset.RefreshRestricted {
			return args, nil
		}
		addExecutionWarning(ctx, fmt.Sprintf("Full refresh is restricted for %s; running its configured materialization strategy instead.", asset.Name))
	}
	strategy := strings.ToLower(strings.TrimSpace(string(asset.Materialization.Strategy)))
	switch strategy {
	case "", "create+replace", "create_replace", "full-refresh", "full_refresh":
		return nil, nil
	case "truncate+insert", "truncate_insert", "truncate":
		return []string{"--mode", "truncate"}, nil
	case "append":
		if key := strings.TrimSpace(asset.Materialization.IncrementalKey); key != "" {
			return []string{"--mode", "incremental", "--update-key", key}, nil
		}
		return []string{"--mode", "snapshot"}, nil
	case "merge":
		primaryKeys := asset.ColumnNamesWithPrimaryKey()
		if len(primaryKeys) == 0 {
			return nil, errors.New("merge materialization needs at least one primary-key column")
		}
		args := []string{"--mode", "incremental", "--primary-key", strings.Join(primaryKeys, ",")}
		if key := strings.TrimSpace(asset.Materialization.IncrementalKey); key != "" {
			args = append(args, "--update-key", key)
		}
		return args, nil
	default:
		return nil, fmt.Errorf("materialization strategy %q is not supported for %s assets", strategy, asset.Type)
	}
}

func newStreamingCommand(ctx context.Context, name string, args []string, dir string, writer io.Writer) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), loadRunEnv(ctx)...)
	return cmd
}

func loadRunEnv(ctx context.Context) []string {
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

func (e *HybridBruinExecutor) runLoadAsset(ctx context.Context, pl *pipeline.Pipeline, asset *pipeline.Asset, manager config.ConnectionGetter, onChunk func([]byte)) ([]byte, error) {
	if asset == nil {
		return nil, errors.New("load asset is required")
	}
	writer := &streamCaptureWriter{buffer: bytes.NewBuffer(nil), onChunk: onChunk}

	params, err := resolvedLoadParams(asset, pl)
	if err != nil {
		return writer.buffer.Bytes(), err
	}
	srcArgs, err := e.loadSourceArgs(manager, params)
	if err != nil {
		return writer.buffer.Bytes(), err
	}
	tgtArgs, err := e.loadTargetArgs(manager, params)
	if err != nil {
		return writer.buffer.Bytes(), err
	}

	args := append([]string{"run"}, srcArgs...)
	args = append(args, tgtArgs...)
	modeArgs, err := slingMaterializationArgs(ctx, asset)
	if err != nil {
		return writer.buffer.Bytes(), err
	}
	args = append(args, modeArgs...)

	cmdName, cmdArgs, err := loadCommand(ctx, args, writer)
	if err != nil {
		return writer.buffer.Bytes(), err
	}
	cmd := newStreamingCommand(ctx, cmdName, cmdArgs, e.workspaceRoot, writer)
	lease, err := e.acquireDuckDBConnections(ctx, manager, []string{params.SourceConnection, params.DestinationConnection}, directTaskLeaseOwner(ctx, pl, asset), writer)
	if err != nil {
		return writer.buffer.Bytes(), err
	}
	defer lease.Release()
	if err := runStreamingCommand(cmd, writer); err != nil {
		return writer.buffer.Bytes(), err
	}
	return writer.buffer.Bytes(), nil
}
