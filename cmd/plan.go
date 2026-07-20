package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/urfave/cli/v3"

	"renart/internal/clientapi"
	"renart/internal/web/model"
	"renart/internal/web/service"
)

// Plan previews a pipeline execution without running it. A live server is
// preferred so CLI and Build share the same staleness ledger and deployment
// store; embedded mode uses the identical service graph.
func Plan() *cli.Command {
	return &cli.Command{
		Name:      "plan",
		Usage:     "review pipeline readiness and planned operations without executing them",
		ArgsUsage: "[pipeline, asset name, or path]",
		Category:  categoryPipeline,
		Description: "Defaults to assets currently needing work. Pass an asset to preview an asset closure, or --all for the entire pipeline.\n" +
			"Examples:\n" +
			"   renart plan marts\n" +
			"   renart plan marts --all\n" +
			"   renart plan marts --selector 'tag:daily,+fct_orders'\n" +
			"   renart plan marts --selector 'tag:daily' --selector-needed\n" +
			"   renart plan mart.orders --upstream\n" +
			"   renart plan marts --source snapshot --snapshot <version> --json",
		Flags: []cli.Flag{
			workspaceFlag(),
			&cli.StringFlag{Name: "env", Usage: "environment whose policy and configuration should be planned"},
			&cli.StringFlag{Name: "start-date", Usage: "ISO start date for the execution window"},
			&cli.StringFlag{Name: "end-date", Usage: "ISO end date for the execution window"},
			&cli.StringFlag{Name: "execution-time", Usage: "RFC3339 execution timestamp used by templates (default: now)"},
			&cli.StringFlag{Name: "source", Usage: "saved source: working-tree or snapshot"},
			&cli.StringFlag{Name: "snapshot", Usage: "exact deployment version (implies --source snapshot)"},
			&cli.BoolFlag{Name: "all", Usage: "plan every asset rather than only assets needing work"},
			&cli.StringFlag{Name: "selector", Usage: "pipeline targets only: plan assets matched by a selector expression"},
			&cli.BoolFlag{Name: "selector-needed", Usage: "with --selector, keep only matching assets that need work"},
			&cli.BoolFlag{Name: "upstream", Usage: "asset targets only: include upstream dependencies"},
			&cli.BoolFlag{Name: "downstream", Usage: "asset targets only: include downstream dependents"},
			&cli.BoolFlag{Name: "full-refresh", Usage: "preview the effective full-refresh form"},
			&cli.BoolFlag{Name: "backfill", Usage: "mark an explicit single-asset window as a backfill"},
			&cli.StringFlag{Name: "sensor-mode", Usage: "sensor behavior: once, wait, or skip (default: once)"},
			&cli.BoolFlag{Name: "include-stage-content", Usage: "include and print rendered operation bodies"},
			&cli.BoolFlag{
				Name: "local", Usage: "plan in-process even when a Renart server is running",
				Sources: cli.EnvVars("RENART_NO_SERVER"),
			},
			&cli.BoolFlag{Name: "json", Usage: "emit the structured plan response as JSON"},
		},
		Action: planAction,
	}
}

func planAction(ctx context.Context, c *cli.Command) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	workspaceRoot, err := findWorkspaceRoot(c.String("workspace"), cwd)
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}

	client := discoverPlanClient(ctx, workspaceRoot, c.Bool("local"), c.Bool("json"))
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
	selection, err := planSelection(
		target,
		c.Bool("all"),
		c.Bool("upstream"),
		c.Bool("downstream"),
		c.String("selector"),
		c.Bool("selector-needed"),
	)
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	source, err := planSource(c.String("source"), c.String("snapshot"))
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	sensorMode := strings.ToLower(strings.TrimSpace(c.String("sensor-mode")))
	if sensorMode != "" && sensorMode != "once" && sensorMode != "wait" && sensorMode != "skip" {
		return cli.Exit("--sensor-mode must be one of once, wait, or skip", 2)
	}

	request := service.PipelinePlanRequest{
		Environment:         strings.TrimSpace(c.String("env")),
		StartDate:           strings.TrimSpace(c.String("start-date")),
		EndDate:             strings.TrimSpace(c.String("end-date")),
		ExecutionTime:       strings.TrimSpace(c.String("execution-time")),
		FullRefresh:         c.Bool("full-refresh"),
		Backfill:            c.Bool("backfill"),
		SensorMode:          sensorMode,
		Source:              source,
		Selection:           selection,
		IncludeStageContent: c.Bool("include-stage-content"),
	}
	var plan service.PipelinePlan
	if client != nil {
		plan, err = client.PlanPipeline(ctx, target.pipeline.ID, request)
	} else {
		var apiErr *service.APIError
		plan, apiErr = server.pipelinePlanSvc.Plan(ctx, target.pipeline.ID, request)
		if apiErr != nil {
			err = apiErr
		}
	}
	if err != nil {
		return fmt.Errorf("failed to plan %s: %w", target.pipeline.Name, err)
	}

	if c.Bool("json") {
		encoded, marshalErr := json.MarshalIndent(plan, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Fprintln(c.Writer, string(encoded))
	} else {
		printPipelinePlan(c.Writer, plan, c.Bool("include-stage-content"))
	}
	if plan.Status == service.PipelinePlanStatusBlocked {
		return cli.Exit("", 1)
	}
	return nil
}

func planSelection(target runTarget, all, upstream, downstream bool, rawSelector string, selectorNeeded bool) (service.PipelinePlanSelectionRequest, error) {
	selector := strings.TrimSpace(rawSelector)
	if selectorNeeded && selector == "" {
		return service.PipelinePlanSelectionRequest{}, fmt.Errorf("--selector-needed requires --selector")
	}
	if target.kind == "pipeline" {
		if upstream || downstream {
			return service.PipelinePlanSelectionRequest{}, fmt.Errorf("--upstream and --downstream apply only to asset targets")
		}
		if all && selector != "" {
			return service.PipelinePlanSelectionRequest{}, fmt.Errorf("--all and --selector cannot be combined")
		}
		if selector != "" {
			mode := service.PipelinePlanSelectionSelector
			if selectorNeeded {
				mode = service.PipelinePlanSelectionSelectorNeeded
			}
			return service.PipelinePlanSelectionRequest{Mode: mode, Selector: selector}, nil
		}
		mode := service.PipelinePlanSelectionNeeded
		if all {
			mode = service.PipelinePlanSelectionAll
		}
		return service.PipelinePlanSelectionRequest{Mode: mode}, nil
	}
	if all || selector != "" {
		return service.PipelinePlanSelectionRequest{}, fmt.Errorf("--all and --selector apply to pipeline targets; omit them for an asset closure")
	}
	scope := "asset"
	switch {
	case upstream && downstream:
		scope = "asset_with_upstreams_and_downstreams"
	case upstream:
		scope = "asset_with_upstreams"
	case downstream:
		scope = "asset_with_downstreams"
	}
	return service.PipelinePlanSelectionRequest{Mode: service.PipelinePlanSelectionAsset, AssetName: target.asset.Name, Scope: scope}, nil
}

func planSource(rawSource, rawVersion string) (service.PipelinePlanSourceRequest, error) {
	source := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(rawSource)), "-", "_")
	version := strings.TrimSpace(rawVersion)
	if source == "" && version != "" {
		source = service.PipelinePlanSourceSnapshot
	}
	if source != "" && source != service.PipelinePlanSourceWorkingTree && source != service.PipelinePlanSourceSnapshot {
		return service.PipelinePlanSourceRequest{}, fmt.Errorf("--source must be working-tree or snapshot")
	}
	if source == service.PipelinePlanSourceWorkingTree && version != "" {
		return service.PipelinePlanSourceRequest{}, fmt.Errorf("--snapshot cannot be combined with --source working-tree")
	}
	return service.PipelinePlanSourceRequest{Kind: source, VersionID: version}, nil
}

func discoverPlanClient(ctx context.Context, workspaceRoot string, local, jsonOutput bool) *clientapi.Client {
	client := clientapi.FromEnv()
	if client == nil {
		discovered, err := clientapi.Discover(ctx, workspaceRoot)
		if err != nil && !jsonOutput {
			fmt.Fprintf(os.Stderr, "notice: server discovery: %v\n", err)
		}
		client = discovered
	}
	if client == nil || local {
		if local && client != nil && !jsonOutput {
			fmt.Fprintln(os.Stderr, "notice: planning locally instead of using the running Renart server")
		}
		return nil
	}
	if !jsonOutput {
		fmt.Fprintf(os.Stderr, "notice: delegating to the Renart server at %s\n", client.APIBase)
		if client.ServerVersion != "" && client.ServerVersion != buildVersion {
			fmt.Fprintf(os.Stderr, "warning: CLI version %s differs from server version %s\n", buildVersion, client.ServerVersion)
		}
	}
	return client
}

func printPipelinePlan(w interface{ Write([]byte) (int, error) }, plan service.PipelinePlan, includeContent bool) {
	ok := color.New(color.FgGreen).SprintFunc()
	warn := color.New(color.FgYellow).SprintFunc()
	bad := color.New(color.FgRed).SprintFunc()
	dim := color.New(color.Faint).SprintFunc()
	status := ok(strings.ToUpper(plan.Status))
	if plan.Status == service.PipelinePlanStatusWarning {
		status = warn(strings.ToUpper(plan.Status))
	} else if plan.Status == service.PipelinePlanStatusBlocked {
		status = bad(strings.ToUpper(plan.Status))
	}
	fmt.Fprintf(w, "Plan — %s: %s\n", plan.PipelineName, status)
	source := strings.ReplaceAll(plan.Source.Kind, "_", " ")
	if plan.Source.VersionID != "" {
		source += " " + shortRenderIdentity(plan.Source.VersionID)
	}
	fmt.Fprintf(w, "Source: %s %s\n", source, dim(shortRenderIdentity(plan.Source.MerkleRoot)))
	fmt.Fprintf(w, "Context: %s · %s → %s · sensor %s\n",
		valueOrDefault(plan.Context.Environment, "default"), plan.Context.StartDate, plan.Context.EndDate,
		valueOrDefault(plan.Context.SensorMode, "once"),
	)
	selection := plan.Selection.Mode
	if plan.Selection.Selector != "" {
		selection += " " + plan.Selection.Selector
	}
	fmt.Fprintf(w, "Selection: %s · %d asset(s) · %d execution unit(s) · %d stage(s)\n",
		selection, plan.Summary.Assets, plan.Summary.ExecutionUnits, plan.Summary.Stages,
	)
	if plan.Context.Destructive {
		fmt.Fprintf(w, "%s\n", warn(fmt.Sprintf("Destructive operations: %d", plan.Summary.DestructiveOperations)))
	}
	printPipelinePlanIssues(w, "Blockers", plan.Readiness.Blockers, bad)
	printPipelinePlanIssues(w, "Warnings", plan.Readiness.Warnings, warn)
	if len(plan.Assets) == 0 {
		fmt.Fprintln(w, dim("No assets selected."))
		return
	}
	fmt.Fprintln(w, "Assets:")
	for _, asset := range plan.Assets {
		reasons := strings.Join(asset.InclusionReasons, ", ")
		fmt.Fprintf(w, "  • %s %s · %d render(s)\n", asset.Name, dim("["+reasons+"]"), len(asset.Renders))
		if !includeContent {
			continue
		}
		for _, rendered := range asset.Renders {
			for _, stage := range rendered.Stages {
				fmt.Fprintf(w, "      %s %s\n", valueOrDefault(stage.Label, stage.Kind), dim("["+string(stage.Fidelity)+"]"))
				if strings.TrimSpace(stage.Content) != "" {
					for _, line := range strings.Split(strings.TrimRight(stage.Content, "\n"), "\n") {
						fmt.Fprintf(w, "        %s\n", line)
					}
				}
			}
		}
	}
}

func printPipelinePlanIssues(
	w interface{ Write([]byte) (int, error) },
	label string,
	issues []service.PipelinePlanIssue,
	style func(a ...any) string,
) {
	if len(issues) == 0 {
		return
	}
	fmt.Fprintf(w, "%s:\n", label)
	for _, issue := range issues {
		asset := ""
		if issue.AssetName != "" {
			asset = issue.AssetName + ": "
		}
		fmt.Fprintf(w, "  %s %s%s\n", style("•"), asset, issue.Message)
	}
}
