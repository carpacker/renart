package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"renart/internal/clientapi"
	"renart/internal/web/model"
	"renart/internal/web/service"
)

// Ls lists what's in the workspace. It prefers a running server's parsed
// state (free and always current) and otherwise parses the workspace
// directly — no scheduler, no execution graph, just the manifest walk.
func Ls() *cli.Command {
	return &cli.Command{
		Name:      "ls",
		Usage:     "list pipelines or assets in the workspace",
		ArgsUsage: "[pipelines|assets] [pipeline]",
		Category:  categoryPipeline,
		Flags: []cli.Flag{
			workspaceFlag(),
			&cli.BoolFlag{Name: "json", Usage: "emit JSON instead of a table"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			what := c.Args().Get(0)
			if what == "" {
				what = "pipelines"
			}
			if what != "pipelines" && what != "assets" {
				return cli.Exit(fmt.Sprintf("unknown listing %q, expected pipelines or assets", what), 2)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			workspaceRoot, err := findWorkspaceRoot(c.String("workspace"), cwd)
			if err != nil {
				return cli.Exit(err.Error(), 2)
			}

			state, err := loadWorkspaceState(ctx, workspaceRoot)
			if err != nil {
				return err
			}

			pipelines := state.Pipelines
			if filter := c.Args().Get(1); what == "assets" && filter != "" {
				pipelines = nil
				for _, p := range state.Pipelines {
					if p.Name == filter {
						pipelines = append(pipelines, p)
					}
				}
				if len(pipelines) == 0 {
					return cli.Exit(fmt.Sprintf("no pipeline named %q", filter), 2)
				}
			}

			if c.Bool("json") {
				return printLsJSON(what, pipelines)
			}

			writer := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			defer writer.Flush()
			if what == "pipelines" {
				fmt.Fprintln(writer, "NAME\tASSETS\tSCHEDULE\tPATH")
				for _, p := range pipelines {
					schedule := p.Schedule
					if schedule == "" {
						schedule = "-"
					}
					fmt.Fprintf(writer, "%s\t%d\t%s\t%s\n", p.Name, len(p.Assets), schedule, p.Path)
				}
				return nil
			}
			fmt.Fprintln(writer, "NAME\tTYPE\tPIPELINE\tPATH")
			for _, p := range pipelines {
				for _, a := range p.Assets {
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", a.Name, a.Type, p.Name, a.Path)
				}
			}
			return nil
		},
	}
}

func printLsJSON(what string, pipelines []model.Pipeline) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if what == "pipelines" {
		type row struct {
			Name     string `json:"name"`
			Assets   int    `json:"assets"`
			Schedule string `json:"schedule,omitempty"`
			Path     string `json:"path"`
		}
		rows := make([]row, 0, len(pipelines))
		for _, p := range pipelines {
			rows = append(rows, row{Name: p.Name, Assets: len(p.Assets), Schedule: p.Schedule, Path: p.Path})
		}
		return encoder.Encode(rows)
	}
	type row struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Pipeline string `json:"pipeline"`
		Path     string `json:"path"`
	}
	var rows []row
	for _, p := range pipelines {
		for _, a := range p.Assets {
			rows = append(rows, row{Name: a.Name, Type: a.Type, Pipeline: p.Name, Path: a.Path})
		}
	}
	return encoder.Encode(rows)
}

// loadWorkspaceState fetches the workspace from a running server when one
// has it open, else parses it in-process (read-only, no service graph).
func loadWorkspaceState(ctx context.Context, workspaceRoot string) (model.WorkspaceState, error) {
	if client, _ := clientapi.Discover(ctx, workspaceRoot); client != nil {
		return client.Workspace(ctx)
	}
	workspaceSvc := service.NewWorkspaceService(workspaceRoot, resolveConfigFilePath(workspaceRoot))
	return workspaceSvc.ComputeState(ctx)
}
