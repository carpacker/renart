package cmd

import (
	"context"
	"encoding/json"
	"errors"
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
	webscheduler "renart/internal/web/scheduler"
	"renart/internal/web/service"
	"renart/internal/web/staleness"
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
			"   renart run mart.orders --downstream   include everything below it\n" +
			"   renart run mart.orders --refresh-upstreams   build stale upstreams first",
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
				Name:  "refresh-upstreams",
				Usage: "asset targets only: build stale transitive upstreams before running the asset",
			},
			&cli.BoolFlag{
				Name:  "full-refresh",
				Usage: "rebuild from scratch instead of running incrementally",
			},
			&cli.StringFlag{
				Name:  "sensor-mode",
				Usage: "sensor behavior: once, wait, or skip (default: once)",
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
	if target.kind == "pipeline" && c.Bool("refresh-upstreams") {
		return cli.Exit("--refresh-upstreams applies to asset targets; a pipeline run already covers the whole DAG", 2)
	}

	query := url.Values{}
	setNonEmpty := func(values url.Values, key, value string) {
		if strings.TrimSpace(value) != "" {
			values.Set(key, value)
		}
	}
	runEnvironment := strings.TrimSpace(c.String("env"))
	if runEnvironment == "" {
		runEnvironment = strings.TrimSpace(state.SelectedEnvironment)
	}
	setNonEmpty(query, "environment", runEnvironment)
	setNonEmpty(query, "start_date", c.String("start-date"))
	setNonEmpty(query, "end_date", c.String("end-date"))
	setNonEmpty(query, "confirmed_environment", c.String("confirm-environment"))
	sensorMode := strings.ToLower(strings.TrimSpace(c.String("sensor-mode")))
	if sensorMode != "" && sensorMode != "once" && sensorMode != "wait" && sensorMode != "skip" {
		return cli.Exit("--sensor-mode must be one of once, wait, or skip", 2)
	}
	setNonEmpty(query, "sensor_mode", sensorMode)
	fullRefresh := c.Bool("full-refresh")
	hasExplicitWindow := strings.TrimSpace(c.String("start-date")) != "" || strings.TrimSpace(c.String("end-date")) != ""
	// A full refresh can still be scoped to an explicit execution window. Do
	// not also label that request as a backfill: the two modes are mutually
	// exclusive, and the window must reach the executor unchanged.
	backfill := hasExplicitWindow && !fullRefresh
	if backfill {
		query.Set("backfill", "true")
	}
	if fullRefresh {
		query.Set("full_refresh", "true")
	}
	scope := string(service.MaterializeScopeAsset)
	if c.Bool("downstream") {
		scope = string(service.MaterializeScopeAssetWithDownstreams)
	}
	if target.kind == "asset" {
		query.Set("scope", scope)
	}
	if c.Bool("refresh-upstreams") {
		if _, err := service.ResolveExecutionTimeWindow(
			target.pipeline.Schedule,
			c.String("start-date"),
			c.String("end-date"),
			time.Now().UTC(),
		); err != nil {
			return cli.Exit(err.Error(), 2)
		}
	}

	printer.event(map[string]any{
		"event":             "start",
		"mode":              map[bool]string{true: "client", false: "embedded"}[client != nil],
		"kind":              target.kind,
		"target":            target.name(),
		"refresh_upstreams": c.Bool("refresh-upstreams"),
	})

	started := time.Now()
	var status, message, output string
	if c.Bool("refresh-upstreams") {
		printer.event(map[string]any{"event": "phase", "phase": "refresh_upstreams", "status": "running"})
		status, message, output, err = refreshRunUpstreams(
			ctx,
			client,
			server,
			target,
			runEnvironment,
			c.String("start-date"),
			c.String("end-date"),
			printer,
		)
		if err != nil {
			return err
		}
		printer.event(map[string]any{
			"event":  "phase",
			"phase":  "refresh_upstreams",
			"status": status,
			"error":  message,
		})
		if status == "ok" {
			status, message, output = "", "", ""
		}
	}
	if status == "" && client != nil {
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
	} else if status == "" {
		ctx = service.WithExecutionOrigin(ctx, webscheduler.RunTriggerCLI)
		onChunk := func(chunk []byte) { printer.chunk(string(chunk)) }
		var result service.MaterializeResult
		if target.kind == "pipeline" {
			result = server.executionSvc.MaterializePipelineStreamWithSensorMode(
				ctx, target.pipeline.ID, runEnvironment, false, fullRefresh, backfill,
				c.String("start-date"), c.String("end-date"), c.String("confirm-environment"), sensorMode, onChunk,
			)
		} else {
			result = server.executionSvc.MaterializeAssetStreamWithSensorMode(
				ctx, target.asset.ID, runEnvironment, scope,
				c.String("start-date"), c.String("end-date"), fullRefresh, backfill, c.String("confirm-environment"), sensorMode, onChunk,
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

func refreshRunUpstreams(
	ctx context.Context,
	client *clientapi.Client,
	server *webServer,
	target runTarget,
	environment, startDate, endDate string,
	printer runPrinter,
) (status, message, output string, err error) {
	printer.notice("refreshing stale upstreams of %s", target.asset.Name)
	if client != nil {
		query := url.Values{"upstream_of": []string{target.asset.Name}}
		if environment != "" {
			query.Set("environment", environment)
		}
		if strings.TrimSpace(startDate) != "" {
			query.Set("start", startDate)
		}
		if strings.TrimSpace(endDate) != "" {
			query.Set("end", endDate)
		}
		done, streamErr := client.BuildStalePipelineStream(ctx, target.pipeline.ID, query, printer.chunk)
		if streamErr != nil {
			return "", "", "", fmt.Errorf("failed to refresh upstreams: %w", streamErr)
		}
		return done.Status, done.Error, done.Output, nil
	}
	if server == nil {
		return "", "", "", errors.New("embedded runner is unavailable")
	}
	if strings.TrimSpace(target.pipeline.UUID) == "" {
		return "", "", "", fmt.Errorf("pipeline %q has no stable id; add an id to pipeline.yml before refreshing upstreams", target.pipeline.Name)
	}

	upstreams, ok := service.PipelineUpstreamNames(target.pipeline, target.asset.Name)
	if !ok {
		return "", "", "", fmt.Errorf("asset %q is not part of pipeline %q", target.asset.Name, target.pipeline.Name)
	}
	selection := staleness.Selection{
		PipelineUUID:      target.pipeline.UUID,
		EncodedPipelineID: target.pipeline.ID,
		Environment:       environment,
	}
	if strings.TrimSpace(startDate) != "" || strings.TrimSpace(endDate) != "" {
		window, windowErr := service.ResolveExecutionTimeWindow(target.pipeline.Schedule, startDate, endDate, time.Now().UTC())
		if windowErr != nil {
			return "", "", "", windowErr
		}
		selection.Start = &window.Start
		selection.End = &window.End
	}
	statuses, statusErr := server.stalenessSvc.Statuses(ctx, selection)
	if statusErr != nil {
		return "", "", "", fmt.Errorf("failed to compute upstream freshness: %w", statusErr)
	}
	result := server.executionSvc.MaterializeStaleAssetsStream(
		ctx,
		target.pipeline.ID,
		environment,
		service.BuildStalePlan(statuses, upstreams),
		startDate,
		endDate,
		func(chunk []byte) { printer.chunk(string(chunk)) },
		nil,
	)
	return result.Status, result.Error, result.Output, nil
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
