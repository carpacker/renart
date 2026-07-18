package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRetainedRunContextDetectsSourceAndConfigurationDrift(t *testing.T) {
	_, root := writeTypeCheckWorkspace(t, "id: pipeline-uuid\nname: analytics", map[string]string{
		"orders.sql": `
/* @bruin
name: analytics.orders
type: duckdb.sql
materialization:
  type: table
@bruin */
select 1 as id
`,
	})
	planner := newTestPipelinePlanService(root, &pipelinePlanStalenessStub{}, nil)
	plan, apiErr := planner.Plan(context.Background(), EncodeID("analytics"), PipelinePlanRequest{
		Environment: "default",
		Selection:   PipelinePlanSelectionRequest{Mode: PipelinePlanSelectionAll},
	})
	require.Nil(t, apiErr)
	require.NotEmpty(t, plan.Source.MerkleRoot)
	require.NotEmpty(t, plan.Context.ConfigurationDigest)

	request := RetainedRunContextValidationRequest{
		PipelineID:                  EncodeID("analytics"),
		PipelineUUID:                "pipeline-uuid",
		Environment:                 "default",
		Source:                      PipelinePlanSourceRequest{Kind: PipelinePlanSourceWorkingTree},
		ConfigurationAssetNames:     []string{"analytics.orders"},
		ExpectedSourceMerkle:        plan.Source.MerkleRoot,
		ExpectedConfigurationDigest: plan.Context.ConfigurationDigest,
	}
	require.NoError(t, planner.ValidateRetainedRunContext(context.Background(), request))

	assetPath := filepath.Join(root, "analytics", "assets", "orders.sql")
	originalAsset, err := os.ReadFile(assetPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(assetPath, append(originalAsset, []byte("\n-- changed\n")...), 0o644))
	err = planner.ValidateRetainedRunContext(context.Background(), request)
	assert.ErrorContains(t, err, "original source has changed")

	require.NoError(t, os.WriteFile(assetPath, originalAsset, 0o644))
	configPath := filepath.Join(root, ".bruin.yml")
	configBody, err := os.ReadFile(configPath)
	require.NoError(t, err)
	changedConfig := []byte(string(configBody) + "\n# identity-changing environment control\n")
	// Comments do not affect the selected configuration identity, so change the
	// actual DuckDB path while leaving the pipeline source untouched.
	changedConfig = []byte(strings.Replace(string(changedConfig), "path: local.db", "path: replay.db", 1))
	require.NoError(t, os.WriteFile(configPath, changedConfig, 0o644))
	err = planner.ValidateRetainedRunContext(context.Background(), request)
	assert.ErrorContains(t, err, "selected environment configuration has changed")
}
