package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"github.com/urfave/cli/v3"
	"renart/internal/web/identity"
	webscheduler "renart/internal/web/scheduler"
	"renart/internal/web/snapshot"
)

// Deploy snapshots a pipeline's source files into the local state store so
// scheduled runs execute the deployed version regardless of working-tree
// edits.
func Deploy() *cli.Command {
	return &cli.Command{
		Name:      "deploy",
		Usage:     "snapshot a pipeline so scheduled runs execute the deployed version",
		ArgsUsage: "<pipeline directory>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "workspace",
				Value: ".",
				Usage: "workspace root holding the .renart state directory",
			},
			&cli.StringFlag{
				Name:  "scheduler-state",
				Value: ".renart/state.db",
				Usage: "local scheduler SQLite state path, relative to the workspace root",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			target := c.Args().Get(0)
			if target == "" {
				target = "."
			}
			absTarget, err := filepath.Abs(target)
			if err != nil {
				return err
			}

			pipelineYml := filepath.Join(absTarget, "pipeline.yml")
			if !fileExists(pipelineYml) {
				if alt := filepath.Join(absTarget, "pipeline.yaml"); fileExists(alt) {
					pipelineYml = alt
				} else {
					return fmt.Errorf("no pipeline.yml found in %s", absTarget)
				}
			}
			pipelineUUID, generated, err := identity.EnsurePipelineID(afero.NewOsFs(), pipelineYml)
			if err != nil {
				return fmt.Errorf("failed to ensure pipeline id: %w", err)
			}
			if generated {
				fmt.Fprintf(os.Stderr, "notice: assigned stable id %s to %s\n", pipelineUUID, pipelineYml)
			}

			workspaceRoot, err := filepath.Abs(c.String("workspace"))
			if err != nil {
				return err
			}
			statePath := c.String("scheduler-state")
			if !filepath.IsAbs(statePath) {
				statePath = filepath.Join(workspaceRoot, statePath)
			}
			store, err := webscheduler.OpenStore(statePath)
			if err != nil {
				return fmt.Errorf("failed to open state store at %s: %w", statePath, err)
			}
			defer store.Close()

			deployed, created, err := snapshot.NewStore(store.DB()).Deploy(ctx, pipelineUUID, absTarget, "cli")
			if err != nil {
				return err
			}
			if !created {
				fmt.Printf("already up to date: latest snapshot %s matches the working tree\n", deployed.VersionID)
				return nil
			}
			fmt.Printf("deployed snapshot %s (%d files", deployed.VersionID, len(deployed.Manifest))
			if deployed.GitSHA != "" {
				dirty := ""
				if deployed.GitDirty {
					dirty = ", dirty"
				}
				fmt.Printf(", git %.10s%s", deployed.GitSHA, dirty)
			}
			fmt.Println(")")
			return nil
		},
	}
}
