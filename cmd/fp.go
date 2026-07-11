package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
	"github.com/urfave/cli/v3"
	"renart/internal/web/fingerprint"
	"renart/internal/web/identity"
	"renart/internal/web/service"
)

// Fp is a debug command: it prints the fingerprint DAG for a pipeline so
// canonicalization and Merkle propagation can be inspected by hand.
func Fp() *cli.Command {
	return &cli.Command{
		Name:      "fp",
		Usage:     "print the fingerprint DAG of a pipeline",
		ArgsUsage: "[pipeline name or directory]",
		Flags: []cli.Flag{
			workspaceFlag(),
			&cli.BoolFlag{Name: "json", Usage: "emit JSON instead of a table"},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			absTarget, err := resolvePipelineTarget(c)
			if err != nil {
				return err
			}

			pipelineYml := filepath.Join(absTarget, "pipeline.yml")
			if _, statErr := os.Stat(pipelineYml); statErr != nil {
				if alt := filepath.Join(absTarget, "pipeline.yaml"); fileExists(alt) {
					pipelineYml = alt
				} else {
					return fmt.Errorf("no pipeline.yml found in %s", absTarget)
				}
			}

			pipelineID, generated, err := identity.EnsurePipelineID(afero.NewOsFs(), pipelineYml)
			if err != nil {
				return fmt.Errorf("failed to ensure pipeline id: %w", err)
			}
			if generated {
				fmt.Fprintf(os.Stderr, "notice: assigned stable id %s to %s\n", pipelineID, pipelineYml)
			}

			builder := service.NewDefaultPipelineBuilder()
			parsed, err := builder.CreatePipelineFromPath(ctx, absTarget, pipeline.WithMutate())
			if err != nil {
				return fmt.Errorf("failed to parse pipeline: %w", err)
			}

			engine := fingerprint.NewEngine()
			vars := fingerprint.EffectiveVars(parsed, nil)
			results, err := engine.DAG(parsed, vars)
			if err != nil {
				return err
			}

			if c.Bool("json") {
				encoder := json.NewEncoder(os.Stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(results)
			}

			ids := make([]string, 0, len(results))
			for id := range results {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			fmt.Printf("pipeline %s (%s), %d assets, algorithm %s\n\n", parsed.Name, pipelineID, len(ids), fingerprint.Version)
			for _, id := range ids {
				result := results[id]
				_, assetName, _ := identity.SplitAssetID(id)
				fmt.Printf("%s\n  fp          %s\n  own-content %s\n", assetName, result.FP, result.OwnContent)
				if len(result.ConsumedVars) > 0 {
					fmt.Printf("  vars        %s\n", strings.Join(result.ConsumedVars, ", "))
				}
			}
			return nil
		},
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
