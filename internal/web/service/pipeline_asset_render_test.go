package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"renart/internal/web/scheduler"
	"renart/internal/web/snapshot"
)

func TestPipelineAssetRenderUsesExactDeploymentAndComparesAlignedStages(t *testing.T) {
	_, root := writeTypeCheckWorkspace(t, "id: pipeline-uuid\nname: analytics", map[string]string{
		"orders.sql": `
/* @bruin
name: analytics.orders
type: duckdb.sql
materialization:
  type: view
@bruin */
select 1 as order_id
`,
	})
	store, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	snapshots := snapshot.NewStore(store.DB())
	deployed, created, err := snapshots.Deploy(
		context.Background(), "pipeline-uuid", filepath.Join(root, "analytics"), "test",
	)
	require.NoError(t, err)
	require.True(t, created)

	assetPath := filepath.Join(root, "analytics", "assets", "orders.sql")
	body, err := os.ReadFile(assetPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(assetPath, []byte(strings.ReplaceAll(string(body), "select 1", "select 2")), 0o644))

	svc := newTestPipelinePlanService(root, &pipelinePlanStalenessStub{}, snapshots)
	snapshotRender, apiErr := svc.RenderPipelineAsset(context.Background(), EncodeID("analytics"), PipelineAssetRenderRequest{
		AssetName: "analytics.orders",
		Source:    PipelinePlanSourceRequest{Kind: PipelinePlanSourceSnapshot, VersionID: deployed.VersionID},
	})
	require.Nil(t, apiErr)
	assert.Equal(t, PipelinePlanSourceSnapshot, snapshotRender.Provenance.Source.Kind)
	assert.Equal(t, deployed.VersionID, snapshotRender.Provenance.Source.VersionID)
	assert.Contains(t, joinedRenderStageContent(snapshotRender.Stages), "select 1")
	assert.NotContains(t, joinedRenderStageContent(snapshotRender.Stages), "select 2")

	comparison, apiErr := svc.ComparePipelineAssetRenders(context.Background(), EncodeID("analytics"), PipelineAssetRenderComparisonRequest{
		AssetName:         "analytics.orders",
		SnapshotVersionID: deployed.VersionID,
	})
	require.Nil(t, apiErr)
	assert.Equal(t, "changed", comparison.Status)
	assert.Equal(t, deployed.VersionID, comparison.Snapshot.VersionID)
	require.NotNil(t, comparison.Deployment)
	require.NotNil(t, comparison.Current)
	assert.Equal(t, comparison.Current.Provenance.Context.StartDate, comparison.Deployment.Provenance.Context.StartDate)
	assert.Equal(t, comparison.Current.Provenance.Context.EndDate, comparison.Deployment.Provenance.Context.EndDate)
	assert.Equal(t, comparison.Current.Provenance.Context.ExecutionTime, comparison.Deployment.Provenance.Context.ExecutionTime)
	assert.Greater(t, comparison.Summary.Changed, 0)
	assert.Contains(t, joinedRenderStageContent(comparison.Current.Stages), "select 2")
}

func TestPipelineAssetRenderComparisonTreatsAssetMissingFromDeploymentAsAdded(t *testing.T) {
	_, root := writeTypeCheckWorkspace(t, "id: pipeline-uuid\nname: analytics", map[string]string{
		"existing.sql": `
/* @bruin
name: analytics.existing
type: duckdb.sql
@bruin */
select 1
`,
	})
	store, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	snapshots := snapshot.NewStore(store.DB())
	deployed, _, err := snapshots.Deploy(
		context.Background(), "pipeline-uuid", filepath.Join(root, "analytics"), "test",
	)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(root, "analytics", "assets", "new.sql"), []byte(`
/* @bruin
name: analytics.new
type: duckdb.sql
@bruin */
select 2
`), 0o644))

	svc := newTestPipelinePlanService(root, &pipelinePlanStalenessStub{}, snapshots)
	comparison, apiErr := svc.ComparePipelineAssetRenders(context.Background(), EncodeID("analytics"), PipelineAssetRenderComparisonRequest{
		AssetName:         "analytics.new",
		SnapshotVersionID: deployed.VersionID,
	})
	require.Nil(t, apiErr)
	assert.Nil(t, comparison.Deployment)
	require.NotNil(t, comparison.Current)
	assert.Equal(t, len(comparison.Current.Stages), comparison.Summary.Added)
	assert.Zero(t, comparison.Summary.Removed)
}

func joinedRenderStageContent(stages []AssetRenderStage) string {
	var builder strings.Builder
	for _, stage := range stages {
		builder.WriteString(stage.Content)
		builder.WriteByte('\n')
	}
	return builder.String()
}
