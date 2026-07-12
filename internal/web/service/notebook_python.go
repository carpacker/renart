package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/connection"
	bruinexecutor "github.com/bruin-data/bruin/pkg/executor"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"go.uber.org/zap"

	"renart/internal/web/notebook"
)

// notebookLogCap bounds captured Python stdout/stderr so a runaway print loop
// cannot balloon a run response.
const notebookLogCap = 256 * 1024

// boundedLogBuffer is a size-capped, concurrency-safe io.Writer. bruin streams
// a Python cell's stdout and stderr from two goroutines into the same writer,
// so writes must be serialized.
type boundedLogBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool
}

func (b *boundedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if remaining := notebookLogCap - b.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			b.buf.Write(p[:remaining])
			b.truncated = true
		} else {
			b.buf.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

// notebookANSIPattern matches ANSI/VT100 control sequences (uv and rich-style
// tracebacks colorize their output) so they don't render as garbage in the UI.
var notebookANSIPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func (b *boundedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	cleaned := notebookANSIPattern.ReplaceAllString(b.buf.String(), "")
	// bruin prefixes every captured line with ">> "; strip it so the notebook
	// shows the program's actual output.
	lines := strings.Split(cleaned, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, ">> ")
	}
	out := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if b.truncated {
		out += "\n… output truncated …"
	}
	return out
}

// notebookSessionConnName is the synthetic connection a Python cell materializes
// into; it is wired to the throwaway DuckDB file the runner then copies into the
// notebook session.
const notebookSessionConnName = "renart-notebook-session"

// notebookInputsConnName is the read-only side of a Python notebook run. The
// runner exports the cell's materialized upstreams into this throwaway DuckDB
// file under their logical names, so renart.query() can use the same names the
// notebook editor shows without opening a second connection to the locked
// session database.
const notebookInputsConnName = "renart-notebook-inputs"

// materializePythonCell runs a Python cell's materialize() through bruin's local
// Python operator (uv), writing the resulting dataframe into destPath as table
// destTable. The runner then copies that table into the notebook session.
func (s *NotebookService) materializePythonCell(ctx context.Context, cell *notebook.Cell, destPath, destTable, inputsPath string) (string, error) {
	notebookDir := filepath.Dir(cell.Path)
	// Python cells declare dependencies in pyproject.toml; uv installs them in
	// project mode. Seed a default when none exists so a fresh cell runs.
	ensureNotebookPyproject(notebookDir)

	duckDBConnections := []config.DuckDBConnection{{
		ConnectionMetadata: config.ConnectionMetadata{Name: notebookSessionConnName},
		Path:               destPath,
	}}
	brokerConnection := notebookSessionConnName
	if inputsPath != "" {
		duckDBConnections = append(duckDBConnections, config.DuckDBConnection{
			ConnectionMetadata: config.ConnectionMetadata{Name: notebookInputsConnName},
			Path:               inputsPath,
		})
		brokerConnection = notebookInputsConnName
	}
	connections := &config.Connections{DuckDB: duckDBConnections}
	cfg := &config.Config{
		SelectedEnvironmentName: "default",
		SelectedEnvironment:     &config.Environment{Connections: connections},
		Environments:            map[string]config.Environment{"default": {Connections: connections}},
	}

	manager, errs := connection.NewManagerFromConfigWithContext(ctx, cfg)
	if len(errs) > 0 {
		return "", fmt.Errorf("failed to set up the python session connection: %w", errs[0])
	}

	// Clone the cell asset for the run: the destination table is the asset name
	// (bruin passes it to ingestr as --dest-table), materialized into the
	// session connection.
	runAsset := *cell.Asset
	runAsset.Name = destTable
	runAsset.Type = pipeline.AssetTypePython
	runAsset.Connection = notebookSessionConnName
	runAsset.Upstreams = nil
	runAsset.Materialization = pipeline.Materialization{Type: pipeline.MaterializationTypeTable}
	runAsset.ExecutableFile = pipeline.ExecutableFile{
		Name:    filepath.Base(cell.Path),
		Path:    cell.Path,
		Content: cell.Asset.ExecutableFile.Content,
	}

	runPipeline := &pipeline.Pipeline{
		Name:   "renart-notebook",
		Assets: []*pipeline.Asset{&runAsset},
	}

	// The Python operator reads the run window (start/end/execution date),
	// run id, and environment from the context, like the asset run path.
	now := time.Now().UTC()
	timeWindow, err := ResolveExecutionTimeWindow("", "", "", now)
	if err != nil {
		return "", fmt.Errorf("failed to resolve the run window: %w", err)
	}
	logs := &boundedLogBuffer{}
	runCtx := context.WithValue(ctx, pipeline.RunConfigFullRefresh, false)
	// Without interval modifiers, bruin preserves the env we pass (otherwise it
	// rebuilds it from scratch and drops RENART_NOTEBOOK_INPUTS).
	runCtx = context.WithValue(runCtx, pipeline.RunConfigApplyIntervalModifiers, false)
	runCtx = context.WithValue(runCtx, pipeline.RunConfigStartDate, timeWindow.Start)
	runCtx = context.WithValue(runCtx, pipeline.RunConfigEndDate, timeWindow.End)
	runCtx = context.WithValue(runCtx, pipeline.RunConfigExecutionDate, now)
	runCtx = context.WithValue(runCtx, pipeline.RunConfigRunID, newRenartRunID())
	runCtx = context.WithValue(runCtx, config.EnvironmentContextKey, cfg.SelectedEnvironment)
	runCtx = context.WithValue(runCtx, config.EnvironmentNameContextKey, cfg.SelectedEnvironmentName)
	runCtx = context.WithValue(runCtx, bruinexecutor.ContextLogger, zap.NewNop().Sugar())
	// Capture the cell's stdout/stderr instead of letting bruin write it to the
	// server's os.Stdout, so it can be returned and shown next to the cell.
	runCtx = context.WithValue(runCtx, bruinexecutor.KeyPrinter, io.Writer(logs))

	envVariables := map[string]string{
		// Keep uv's project virtualenv out of the notebook folder (which is a
		// git-tracked folder of cells) by placing it under .renart.
		"UV_PROJECT_ENVIRONMENT": notebookVenvDir(s.deps.WorkspaceRoot, notebookDir),
	}
	if inputsPath != "" {
		// The Python reads upstream cells from this DuckDB file by their logical
		// names, e.g. duckdb.connect(os.environ["RENART_NOTEBOOK_INPUTS"]).
		envVariables["RENART_NOTEBOOK_INPUTS"] = inputsPath
	}

	// The renart operator loads materialize() into the throwaway output file and
	// serves SDK queries from the separately exported input snapshot. Both stay
	// in-process and credential-free, and neither opens the locked session file.
	operator := newRenartPythonOperator(manager, envVariables, renartPythonOperatorOptions{
		enableBroker:            true,
		brokerDefaultConnection: brokerConnection,
	})
	runErr := operator.RunTask(runCtx, runPipeline, &runAsset)
	return logs.String(), runErr
}
