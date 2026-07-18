package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/fatih/color"
	"github.com/spf13/afero"
	"github.com/urfave/cli/v3"

	"renart/internal/clientapi"
	"renart/internal/web/service"
)

// Render previews the saved operations for one asset without connecting to a
// warehouse. A live server is used when available so the CLI and Build editor
// share the same service boundary; otherwise the read-only service runs in the
// current process.
func Render() *cli.Command {
	return &cli.Command{
		Name:      "render",
		Usage:     "preview the saved operations for one asset without executing them",
		ArgsUsage: "<asset name or path>",
		Category:  categoryPipeline,
		Description: "Shows compiled SQL, execution SQL, or a truthful semantic operation for the saved asset.\n" +
			"Examples:\n" +
			"   renart render mart.orders\n" +
			"   renart render assets/orders.sql --env production\n" +
			"   renart render seeds/customers.asset.yml --json",
		Flags: []cli.Flag{
			workspaceFlag(),
			&cli.StringFlag{
				Name:  "env",
				Usage: "environment whose render context should be used",
			},
			&cli.StringFlag{
				Name:  "start-date",
				Usage: "ISO start date for the render window (defaults to the pipeline schedule)",
			},
			&cli.StringFlag{
				Name:  "end-date",
				Usage: "ISO end date for the render window (defaults to the pipeline schedule)",
			},
			&cli.StringFlag{
				Name:  "execution-time",
				Usage: "RFC3339 execution timestamp used by templates (default: now)",
			},
			&cli.BoolFlag{
				Name:  "full-refresh",
				Usage: "preview the full-refresh form when the selected environment permits it",
			},
			&cli.BoolFlag{
				Name:    "local",
				Usage:   "render in-process even when a Renart server is running",
				Sources: cli.EnvVars("RENART_NO_SERVER"),
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "emit the structured render response as JSON",
			},
		},
		Action: renderAction,
	}
}

func renderAction(ctx context.Context, c *cli.Command) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	workspaceRoot, err := findWorkspaceRoot(c.String("workspace"), cwd)
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	assetPath, err := resolveRenderAssetPath(ctx, workspaceRoot, cwd, c.Args().Get(0))
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}

	request := service.AssetRenderRequest{
		Environment:   strings.TrimSpace(c.String("env")),
		StartDate:     strings.TrimSpace(c.String("start-date")),
		EndDate:       strings.TrimSpace(c.String("end-date")),
		ExecutionTime: strings.TrimSpace(c.String("execution-time")),
		FullRefresh:   c.Bool("full-refresh"),
	}

	client := discoverRenderClient(ctx, workspaceRoot, c.Bool("local"), c.Bool("json"))
	var result service.AssetRenderResult
	if client != nil {
		result, err = client.RenderAsset(ctx, service.EncodeID(assetPath), request)
	} else {
		result, err = service.NewAssetRenderService(workspaceRoot).RenderPath(ctx, assetPath, request)
	}
	if err != nil {
		return fmt.Errorf("failed to render %s: %w", assetPath, err)
	}

	if c.Bool("json") {
		encoded, marshalErr := json.MarshalIndent(result, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Fprintln(c.Writer, string(encoded))
	} else {
		printAssetRenderResult(c.Writer, result)
	}
	if result.Status == service.AssetRenderStatusError || result.Status == service.AssetRenderStatusUnsupported {
		return cli.Exit("", 1)
	}
	return nil
}

func discoverRenderClient(ctx context.Context, workspaceRoot string, local, jsonOutput bool) *clientapi.Client {
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
			fmt.Fprintln(os.Stderr, "notice: rendering locally instead of using the running Renart server")
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

func resolveRenderAssetPath(ctx context.Context, workspaceRoot, cwd, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("an asset name or path is required")
	}

	for _, base := range []string{cwd, workspaceRoot} {
		candidate := target
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(base, filepath.FromSlash(candidate))
		}
		info, statErr := os.Stat(candidate)
		if statErr != nil {
			if filepath.IsAbs(target) {
				break
			}
			continue
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s is a directory; pass an asset file", candidate)
		}
		relative, relErr := filepath.Rel(workspaceRoot, candidate)
		if relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(relative), nil
		}
		if filepath.IsAbs(target) {
			return "", fmt.Errorf("asset path %q is outside the workspace", target)
		}
	}

	pipelines, err := listWorkspacePipelines(workspaceRoot)
	if err != nil {
		return "", err
	}
	builder := service.NewRenartPipelineBuilder(afero.NewOsFs())
	matches := make([]string, 0, 1)
	parseFailures := 0
	for _, candidate := range pipelines {
		parsed, parseErr := builder.CreatePipelineFromPath(ctx, candidate.Dir, pipeline.WithMutate())
		if parseErr != nil {
			parseFailures++
			continue
		}
		for _, asset := range parsed.Assets {
			if asset == nil || asset.Name != target {
				continue
			}
			path := asset.ExecutableFile.Path
			if strings.TrimSpace(path) == "" {
				path = asset.DefinitionFile.Path
			}
			relative, relErr := filepath.Rel(workspaceRoot, path)
			if relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				matches = append(matches, filepath.ToSlash(relative))
			}
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		if parseFailures > 0 {
			return "", fmt.Errorf("no asset named %q was found; %d pipeline(s) could not be parsed, so pass the asset path to render an incomplete definition", target, parseFailures)
		}
		return "", fmt.Errorf("no asset named %q was found (try `renart ls`)", target)
	default:
		return "", fmt.Errorf("asset name %q is ambiguous, pass a path instead: %s", target, strings.Join(matches, ", "))
	}
}

func printAssetRenderResult(w interface{ Write([]byte) (int, error) }, result service.AssetRenderResult) {
	dim := color.New(color.Faint).SprintFunc()
	warn := color.New(color.FgYellow).SprintFunc()
	bad := color.New(color.FgRed).SprintFunc()

	fmt.Fprintf(w, "Preview — not executed: %s %s\n", result.Asset.Name, dim("("+result.Asset.Type+")"))
	fmt.Fprintf(w, "Source: %s %s · environment %s · %s → %s\n",
		result.Provenance.Source.Kind,
		dim(shortRenderIdentity(result.Provenance.Source.MerkleRoot)),
		valueOrDefault(result.Provenance.Context.Environment, "default"),
		result.Provenance.Context.StartDate,
		result.Provenance.Context.EndDate,
	)
	if result.Provenance.Context.FullRefresh {
		fmt.Fprintln(w, "Mode: full refresh")
	}
	if result.Provenance.Context.ConfigurationFidelity != "" || result.Provenance.Context.ConfigurationDigest != "" {
		if result.Provenance.Context.ConfigurationDigest == "" {
			fmt.Fprintln(w, "Configuration: runtime-only")
		} else {
			fmt.Fprintf(w, "Configuration: %s %s\n",
				shortRenderIdentity(result.Provenance.Context.ConfigurationDigest),
				dim("["+result.Provenance.Context.ConfigurationFidelity+"]"),
			)
		}
		if result.Provenance.Context.ConfigurationMessage != "" {
			fmt.Fprintln(w, dim(result.Provenance.Context.ConfigurationMessage))
		}
	}
	if result.Provenance.Context.VariablesDigest != "" {
		fmt.Fprintf(w, "Variables: %s", shortRenderIdentity(result.Provenance.Context.VariablesDigest))
		if provenance := renderVariableProvenance(result.Provenance.Context.VariableProvenance); provenance != "" {
			fmt.Fprintf(w, " · %s", provenance)
		}
		fmt.Fprintln(w)
	}
	if result.Asset.Fingerprint != "" {
		fmt.Fprintf(w, "Asset fingerprint: %s\n", shortRenderIdentity(result.Asset.Fingerprint))
	}
	if result.Asset.Target.Kind != "" {
		if result.Asset.Target.Kind == "none" && result.Asset.Target.Fidelity == service.AssetRenderFidelityExact {
			fmt.Fprintln(w, "Target: no physical output")
		} else if result.Asset.Target.Identity != "" {
			fmt.Fprintf(w, "Target: %s %s %s\n",
				valueOrDefault(result.Asset.Target.Object, result.Asset.Target.Kind),
				shortRenderIdentity(result.Asset.Target.Identity),
				dim("["+string(result.Asset.Target.Fidelity)+"]"),
			)
		} else {
			fmt.Fprintf(w, "Target: %s %s\n",
				valueOrDefault(result.Asset.Target.Object, result.Asset.Target.Kind),
				dim("["+string(result.Asset.Target.Fidelity)+"]"),
			)
		}
		if result.Asset.Target.Message != "" {
			fmt.Fprintln(w, dim(result.Asset.Target.Message))
		}
	}
	for _, issue := range result.Issues {
		label := warn("warning")
		if issue.Severity == "error" {
			label = bad("error")
		}
		fmt.Fprintf(w, "%s: %s\n", label, issue.Message)
	}
	for _, stage := range result.Stages {
		fmt.Fprintf(w, "\n%s %s\n", renderStageTitle(stage), dim("["+string(stage.Fidelity)+"]"))
		if stage.Content != "" {
			fmt.Fprintln(w, stage.Content)
		}
		if stage.Message != "" {
			fmt.Fprintln(w, dim(stage.Message))
		}
	}
	if len(result.Redactions) > 0 {
		fmt.Fprintln(w, dim("Known credential values were redacted from this preview."))
	}
}

func renderVariableProvenance(variables []service.AssetRenderVariableProvenance) string {
	items := make([]string, 0, len(variables))
	for _, variable := range variables {
		source := strings.ReplaceAll(variable.Source, "_", " ")
		items = append(items, variable.Name+" ("+source+")")
	}
	return strings.Join(items, ", ")
}

func renderStageTitle(stage service.AssetRenderStage) string {
	if strings.TrimSpace(stage.Label) != "" {
		return stage.Label
	}
	switch stage.Kind {
	case "compiled_query":
		return "Compiled query"
	case "execution_sql":
		return "Execution SQL"
	case "schema_preparation":
		return "Schema preparation"
	default:
		words := strings.ReplaceAll(stage.Kind, "_", " ")
		if words == "" {
			return "Operation"
		}
		return strings.ToUpper(words[:1]) + words[1:]
	}
}

func shortRenderIdentity(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
