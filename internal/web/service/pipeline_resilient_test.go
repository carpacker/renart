package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"renart/internal/web/model"
)

func TestComputeStateKeepsPipelineWhenAnAssetFailsToParse(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineRoot, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))

	// A valid SQL asset.
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "assets/good.sql"),
		[]byte("/* @bruin\nname: analytics.good\ntype: duckdb.sql\n@bruin */\nselect 1\n"), 0o644))
	// A broken asset: tab-indented YAML, which is invalid and fails to parse.
	brokenContent := "name: analytics.broken\ntype: sling\nparameters:\n\tobject: x\n"
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "assets/broken.asset.yml"), []byte(brokenContent), 0o644))

	workspace := NewWorkspaceService(workspaceRoot, filepath.Join(workspaceRoot, ".bruin.yml"))
	state, err := workspace.ComputeState(context.Background())
	require.NoError(t, err)

	// The pipeline survives the broken asset.
	require.Len(t, state.Pipelines, 1)
	assets := state.Pipelines[0].Assets
	require.Len(t, assets, 2, "both the good and broken assets should be present")

	var good, broken *model.Asset
	for i := range assets {
		if assets[i].ParseError != "" {
			broken = &assets[i]
		} else {
			good = &assets[i]
		}
	}

	// The good asset is intact.
	require.NotNil(t, good)
	assert.Equal(t, "analytics.good", good.Name)
	assert.Empty(t, good.ParseError)

	// The broken asset is surfaced (not dropped) with its error and raw content,
	// and the internal meta key is not leaked.
	require.NotNil(t, broken, "the unparseable asset should still be present, flagged")
	assert.NotEmpty(t, broken.ParseError)
	assert.Contains(t, broken.Content, "analytics.broken")
	_, leaked := broken.Meta[parseErrorMetaKey]
	assert.False(t, leaked, "the internal parse-error meta key must not leak into the DTO")

	// The pipeline-level error is still recorded for diagnostics.
	require.NotEmpty(t, state.Errors)
}
