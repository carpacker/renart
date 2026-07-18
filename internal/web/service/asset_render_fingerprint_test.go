package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"renart/internal/web/fingerprint"
	"renart/internal/web/identity"
)

func TestAssetRenderServiceExposesCanonicalAssetAndCoverageFingerprints(t *testing.T) {
	t.Parallel()

	parsed, root := writeTypeCheckWorkspace(t, `
id: pipeline-fingerprint-id
name: analytics
default_connections:
  duckdb: duckdb-default
variables:
  threshold:
    type: integer
    default: 7
  unused:
    type: string
    default: stable
`, map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
@bruin */
select {{ var.threshold }} as threshold
`,
	})

	vars := fingerprint.EffectiveVars(parsed, nil)
	canonicalDAG, err := fingerprint.NewEngine().DAG(parsed, vars)
	require.NoError(t, err)
	expected := canonicalDAG[identity.AssetID(parsed.LegacyID, "analytics.report")]
	require.NotEmpty(t, expected.FP)

	service := NewAssetRenderService(root)
	ownedEngine := service.fingerprintEngine
	require.NotNil(t, ownedEngine)
	result, err := service.RenderPath(context.Background(), "analytics/assets/report.sql", AssetRenderRequest{})
	require.NoError(t, err)

	assert.Equal(t, string(expected.FP), result.Asset.Fingerprint)
	expectedCoverageHash := fingerprint.AllVarsHash(vars)
	assert.Equal(t, expectedCoverageHash, result.Provenance.Context.CoverageVariablesHash)
	assert.Equal(t, expectedCoverageHash, result.Provenance.Context.VariablesDigest)
	assert.Same(t, ownedEngine, service.fingerprintEngine)
}

func TestAssetRenderServiceAssetFingerprintCascadesUpstreamChanges(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
id: pipeline-upstream-cascade-id
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"source.sql": `
/* @bruin
name: analytics.source
type: duckdb.sql
@bruin */
select 1 as id
`,
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
depends:
  - analytics.source
@bruin */
select * from analytics.source
`,
	})

	service := NewAssetRenderService(root)
	reportPath := filepath.Join(root, "analytics", "assets", "report.sql")
	reportRenderPath := "analytics/assets/report.sql"
	reportBefore, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	before, err := service.RenderPath(context.Background(), reportRenderPath, AssetRenderRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, before.Asset.Fingerprint)

	upstreamPath := filepath.Join(root, "analytics", "assets", "source.sql")
	require.NoError(t, os.WriteFile(upstreamPath, []byte(`
/* @bruin
name: analytics.source
type: duckdb.sql
@bruin */
select 1000 as id
`), 0o644))

	after, err := service.RenderPath(context.Background(), reportRenderPath, AssetRenderRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, after.Asset.Fingerprint)
	assert.NotEqual(t, before.Asset.Fingerprint, after.Asset.Fingerprint)
	reportAfter, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	assert.Equal(t, reportBefore, reportAfter, "the downstream source itself must remain unchanged")
}

func TestAssetRenderServiceMissingPipelineIDUsesReadOnlyFingerprintSentinel(t *testing.T) {
	t.Parallel()

	parsed, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
@bruin */
select 1
`,
	})
	require.Empty(t, parsed.LegacyID)
	before := renderWorkspaceFiles(t, root)

	result, err := NewAssetRenderService(root).RenderPath(
		context.Background(),
		"analytics/assets/report.sql",
		AssetRenderRequest{},
	)
	require.NoError(t, err)

	fingerprintPipeline := *parsed
	fingerprintPipeline.LegacyID = assetRenderFingerprintPipelineID
	vars := fingerprint.EffectiveVars(&fingerprintPipeline, nil)
	canonicalDAG, err := fingerprint.NewEngine().DAG(&fingerprintPipeline, vars)
	require.NoError(t, err)
	expected := canonicalDAG[identity.AssetID(assetRenderFingerprintPipelineID, "analytics.report")]
	assert.Equal(t, string(expected.FP), result.Asset.Fingerprint)
	assert.Equal(t, before, renderWorkspaceFiles(t, root))
	assert.NotContains(t, before["analytics/pipeline.yml"], "id:")
}

func TestAssetRenderServiceFingerprintFailureReturnsSanitizedPartialResult(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
id: pipeline-cycle-id
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"first.sql": `
/* @bruin
name: analytics.first
type: duckdb.sql
depends:
  - analytics.second
@bruin */
select * from analytics.second
`,
		"second.sql": `
/* @bruin
name: analytics.second
type: duckdb.sql
depends:
  - analytics.first
@bruin */
select * from analytics.first
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(
		context.Background(),
		"analytics/assets/first.sql",
		AssetRenderRequest{},
	)
	require.NoError(t, err)

	assert.Equal(t, AssetRenderStatusPartial, result.Status)
	assert.Empty(t, result.Asset.Fingerprint)
	require.NotEmpty(t, result.Stages, "fingerprint failure must not erase usable rendering")
	require.Len(t, result.Issues, 1)
	assert.Equal(t, "asset_fingerprint_failed", result.Issues[0].Code)
	assert.Equal(t, "warning", result.Issues[0].Severity)
	assert.Equal(t, "asset/DAG fingerprint could not be computed", result.Issues[0].Message)
	assert.NotContains(t, result.Issues[0].Message, root)
	assert.NotContains(t, result.Issues[0].Message, "dependency cycle")
}
