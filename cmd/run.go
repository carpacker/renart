package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/urfave/cli/v3"

	"renart/internal/clientapi"
	"renart/internal/web/model"
	"renart/internal/web/service"
)

// Run materializes a pipeline or a single asset from the terminal. When a
// renart server has the workspace open (discovered via .renart/server.json)
// the run is delegated to it, so one process owns all DuckDB/SQLite writes
// and the UI's staleness badges and run history update live; without a
// server the same service layer runs in-process (plans/cli-v1.md §2).
func Run() *cli.Command {
	return &cli.Command{
		Name:      "run",
		Usage:     "run a pipeline or a single asset",
		ArgsUsage: "[pipeline, asset name, or path]",
		Category:  categoryPipeline,
		Description: "Without an argument, runs the pipeline enclosing the current directory.\n" +
			"Examples:\n" +
			"   renart run marts              run the whole marts pipeline\n" +
			"   renart run mart.orders        run one asset by name\n" +
			"   renart run assets/orders.sql  run one asset by path\n" +
			"   renart run mart.orders --downstream   include everything below it",
		Flags: []cli.Flag{
			workspaceFlag(),
			&cli.StringFlag{
				Name:  "env",
				Usage: "environment to run against (default: the workspace's selected environment)",
			},
			&cli.StringFlag{
				Name:  "start-date",
				Usage: "ISO start date for the run window (defaults to the pipeline schedule)",
			},
			&cli.StringFlag{
				Name:  "end-date",
				Usage: "ISO end date for the run window (defaults to the pipeline schedule)",
			},
			&cli.BoolFlag{
				Name:  "downstream",
				Usage: "asset targets only: also run everything downstream of the asset",
			},
			&cli.BoolFlag{
				Name:  "full-refresh",
				Usage: "rebuild from scratch instead of running incrementally",
			},
			&cli.StringFlag{
				Name:  "confirm-environment",
				Usage: "type the target environment name when its policy requires confirmation for a full refresh or backfill",
			},
			&cli.BoolFlag{
				Name:    "local",
				Usage:   "run in-process even when a renart server is running (DuckDB conflicts possible)",
				Sources: cli.EnvVars("RENART_NO_SERVER"),
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "emit JSON-lines events instead of human-readable output",
			},
			&cli.BoolFlag{
				Name:    "quiet",
				Aliases: []string{"q"},
				Usage:   "suppress streamed run output; print only the final status",
			},
		},
		Action: runAction,
	}
}

// runPrinter centralizes the three output modes (human / --json / --quiet).
type runPrinter struct {
	jsonMode bool
	quiet    bool
}

func (p runPrinter) notice(format string, args ...any) {
	if p.quiet {
		return
	}
	fmt.Fprintln(os.Stderr, color.New(color.Faint).Sprintf(format, args...))
}

func (p runPrinter) warn(format string, args ...any) {
	fmt.Fprintln(os.Stderr, color.New(color.FgYellow).Sprint("warning: ")+fmt.Sprintf(format, args...))
}

func (p runPrinter) event(payload map[string]any) {
	if !p.jsonMode {
		return
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Println(string(encoded))
}

func (p runPrinter) chunk(chunk string) {
	if p.jsonMode {
		p.event(map[string]any{"event": "output", "chunk": chunk})
		return
	}
	if p.quiet {
		return
	}
	fmt.Print(chunk)
}

func runAction(ctx context.Context, c *cli.Command) error {
	printer := runPrinter{jsonMode: c.Bool("json"), quiet: c.Bool("quiet")}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	workspaceRoot, err := findWorkspaceRoot(c.String("workspace"), cwd)
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}

	client := discoverRunClient(ctx, c.Bool("local"), workspaceRoot, printer)

	// Resolve the target against the same workspace state the UI sees.
	var state model.WorkspaceState
	var server *webServer
	if client != nil {
		state, err = client.Workspace(ctx)
		if err != nil {
			return fmt.Errorf("failed to load workspace from the server: %w", err)
		}
	} else {
		var cleanup func()
		server, cleanup, err = newEmbeddedServer(ctx, workspaceRoot)
		if err != nil {
			return err
		}
		defer cleanup()
		state = server.currentState()
	}

	target, err := resolveRunTarget(state, workspaceRoot, c.Args().Get(0), cwd)
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	if target.kind == "pipeline" && c.Bool("downstream") {
		return cli.Exit("--downstream applies to asset targets; a pipeline run already covers the whole DAG", 2)
	}

	query := url.Values{}
	setNonEmpty := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			query.Set(key, value)
		}
	}
	runEnvironment := strings.TrimSpace(c.String("env"))
	if runEnvironment == "" {
		runEnvironment = strings.TrimSpace(state.SelectedEnvironment)
	}
	setNonEmpty("environment", runEnvironment)
	setNonEmpty("start_date", c.String("start-date"))
	setNonEmpty("end_date", c.String("end-date"))
	setNonEmpty("confirmed_environment", c.String("confirm-environment"))
	backfill := strings.TrimSpace(c.String("start-date")) != "" || strings.TrimSpace(c.String("end-date")) != ""
	if backfill {
		query.Set("backfill", "true")
	}
	if c.Bool("full-refresh") {
		query.Set("full_refresh", "true")
	}
	scope := string(service.MaterializeScopeAsset)
	if c.Bool("downstream") {
		scope = string(service.MaterializeScopeAssetWithDownstreams)
	}
	if target.kind == "asset" {
		query.Set("scope", scope)
	}

	printer.event(map[string]any{
		"event":  "start",
		"mode":   map[bool]string{true: "client", false: "embedded"}[client != nil],
		"kind":   target.kind,
		"target": target.name(),
	})

	started := time.Now()
	var status, message, output string
	if client != nil {
		var done clientapi.StreamDone
		if target.kind == "pipeline" {
			done, err = client.MaterializePipelineStream(ctx, target.pipeline.ID, query, printer.chunk)
		} else {
			done, err = client.MaterializeAssetStream(ctx, target.asset.ID, query, printer.chunk)
		}
		if err != nil {
			return err
		}
		status, message, output = done.Status, done.Error, done.Output
	} else {
		onChunk := func(chunk []byte) { printer.chunk(string(chunk)) }
		var result service.MaterializeResult
		if target.kind == "pipeline" {
			result = server.executionSvc.MaterializePipelineStream(
				ctx, target.pipeline.ID, runEnvironment, false, c.Bool("full-refresh"), backfill,
				c.String("start-date"), c.String("end-date"), c.String("confirm-environment"), onChunk,
			)
		} else {
			result = server.executionSvc.MaterializeAssetStream(
				ctx, target.asset.ID, runEnvironment, scope,
				c.String("start-date"), c.String("end-date"), c.Bool("full-refresh"), backfill, c.String("confirm-environment"), onChunk,
			)
		}
		status, message, output = result.Status, result.Error, result.Output
	}

	succeeded := status == "ok"
	printer.event(map[string]any{
		"event":       "done",
		"status":      status,
		"error":       message,
		"duration_ms": time.Since(started).Milliseconds(),
	})

	if !printer.jsonMode {
		duration := time.Since(started).Round(100 * time.Millisecond)
		if succeeded {
			fmt.Printf("%s %s (%s)\n", color.GreenString("✓"), target.name(), duration)
		} else {
			// In quiet mode the streamed output was suppressed; surface it now
			// so the failure is diagnosable.
			if printer.quiet && strings.TrimSpace(output) != "" {
				fmt.Fprintln(os.Stderr, strings.TrimSpace(output))
			}
			fmt.Printf("%s %s (%s): %s\n", color.RedString("✗"), target.name(), duration, message)
		}
	}
	if !succeeded {
		return cli.Exit("", 1)
	}
	return nil
}

// discoverRunClient decides client vs embedded mode: RENART_SERVER pins the
// server; otherwise .renart/server.json is probed. --local skips delegation
// but still warns when a live server would have taken the run.
func discoverRunClient(ctx context.Context, local bool, workspaceRoot string, printer runPrinter) *clientapi.Client {
	client := clientapi.FromEnv()
	if client == nil {
		discovered, err := clientapi.Discover(ctx, workspaceRoot)
		if err != nil {
			printer.notice("server discovery: %v", err)
		}
		client = discovered
	}
	if client == nil {
		printer.notice("no renart server detected — running in-process")
		return nil
	}
	if local {
		printer.warn("a renart server is running for this workspace; --local runs beside it and may hit DuckDB file locks, and the UI won't reflect this run until it re-verifies")
		return nil
	}
	printer.notice("delegating to the renart server at %s", client.APIBase)
	if client.ServerVersion != "" && client.ServerVersion != buildVersion {
		printer.warn("CLI version %s differs from server version %s", buildVersion, client.ServerVersion)
	}
	return client
}

// runTarget is a resolved run request: a whole pipeline or one asset.
type runTarget struct {
	kind     string // "pipeline" | "asset"
	pipeline model.Pipeline
	asset    model.Asset
}

func (t runTarget) name() string {
	if t.kind == "asset" {
		return t.asset.Name
	}
	return t.pipeline.Name
}

// resolveRunTarget maps the positional argument onto the workspace state:
// empty → the pipeline enclosing cwd; else pipeline name, asset name, or a
// path to either.
func resolveRunTarget(state model.WorkspaceState, workspaceRoot, target, cwd string) (runTarget, error) {
	trimmed := strings.TrimSpace(target)

	if trimmed == "" {
		for _, p := range state.Pipelines {
			pipelineDir := filepath.Join(workspaceRoot, filepath.FromSlash(p.Path))
			if cwd == pipelineDir || strings.HasPrefix(cwd, pipelineDir+string(filepath.Separator)) {
				return runTarget{kind: "pipeline", pipeline: p}, nil
			}
		}
		return runTarget{}, fmt.Errorf("not inside a pipeline directory; pass a pipeline or asset (try `renart ls`)")
	}

	for _, p := range state.Pipelines {
		if p.Name == trimmed {
			return runTarget{kind: "pipeline", pipeline: p}, nil
		}
	}

	var assetMatches []runTarget
	for _, p := range state.Pipelines {
		for _, a := range p.Assets {
			if a.Name == trimmed {
				assetMatches = append(assetMatches, runTarget{kind: "asset", pipeline: p, asset: a})
			}
		}
	}
	if len(assetMatches) == 1 {
		return assetMatches[0], nil
	}
	if len(assetMatches) > 1 {
		paths := make([]string, 0, len(assetMatches))
		for _, m := range assetMatches {
			paths = append(paths, m.asset.Path)
		}
		return runTarget{}, fmt.Errorf("asset name %q is ambiguous, pass a path instead: %s", trimmed, strings.Join(paths, ", "))
	}

	// Path form: relative to cwd or to the workspace root.
	for _, base := range []string{cwd, workspaceRoot} {
		candidate := trimmed
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(base, candidate)
		}
		rel, err := filepath.Rel(workspaceRoot, candidate)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)
		for _, p := range state.Pipelines {
			if p.Path == rel {
				return runTarget{kind: "pipeline", pipeline: p}, nil
			}
			for _, a := range p.Assets {
				if a.Path == rel {
					return runTarget{kind: "asset", pipeline: p, asset: a}, nil
				}
			}
		}
		if filepath.IsAbs(trimmed) {
			break
		}
	}

	return runTarget{}, fmt.Errorf("no pipeline or asset matches %q (try `renart ls` to see what's in the workspace)", trimmed)
}
