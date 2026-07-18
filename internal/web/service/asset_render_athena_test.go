package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const athenaTestResultsPath = "s3://renart-test/query-results"

func TestAthenaExecutionRenderMatchesDirectRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		assetSQL string
		wantLen  int
	}{
		{
			name: "query with hooks",
			assetSQL: `
/* @bruin
name: analytics.report
type: athena.sql
hooks:
  pre:
    - query: "SELECT 'pre {{ start_date }}'"
  post:
    - query: "SELECT 'post {{ end_date }}'"
@bruin */
select 'main' as phase
`,
			wantLen: 3,
		},
		{
			name: "metadata-only DDL",
			assetSQL: `
/* @bruin
name: analytics.report
type: athena.sql
materialization:
  type: table
  strategy: ddl
columns:
  - name: id
    type: bigint
@bruin */
`,
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  athena: athena-default
`, map[string]string{"report.sql": tt.assetSQL})
			writeAthenaTestConfig(t, root, athenaTestResultsPath, false)

			assetPath := filepath.Join(root, "analytics", "assets", "report.sql")
			connection := &hookParityBatchConnection{}
			executor := newCompatDirectExecutor(root, "")
			executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
				return &stubConnectionManager{conn: connection}, nil
			}
			request := RunAssetRequest{
				AssetPath: assetPath,
				StartDate: "2026-07-15T00:00:00Z",
				EndDate:   "2026-07-16T00:00:00Z",
			}
			_, err := executor.RunAsset(context.Background(), request, nil)
			require.NoError(t, err)

			outcome := renderAthenaWorkspaceAsset(t, root, assetPath, request)
			require.True(t, outcome.handled)
			assert.Equal(t, AssetRenderStatusOK, outcome.status)
			require.Len(t, outcome.stages, tt.wantLen)
			assert.Equal(t, connection.queries, athenaStageContents(outcome.stages))

			result, err := NewAssetRenderService(root).RenderPath(
				context.Background(),
				"analytics/assets/report.sql",
				AssetRenderRequest{
					StartDate: request.StartDate,
					EndDate:   request.EndDate,
				},
			)
			require.NoError(t, err)
			assert.Equal(t, connection.queries, athenaExecutionStageContents(result.Stages))

			for index, stage := range outcome.stages {
				assert.Equal(t, "execution_sql", stage.Kind)
				assert.Equal(t, fmt.Sprintf("Execution SQL %d", index+1), stage.Label)
				assert.Equal(t, "sql", stage.Language)
				assert.Equal(t, AssetRenderStageStatusOK, stage.Status)
				assert.Equal(t, AssetRenderFidelityExact, stage.Fidelity)
			}
			if tt.name == "metadata-only DDL" {
				assert.True(t, athenaExecutionAllowsEmptyCompiledQuery(outcomeAssetForTest(t, root, assetPath)))
				assert.Contains(t, outcome.stages[0].Content, "LOCATION '"+athenaTestResultsPath+"/analytics.report'")
			}
		})
	}
}

func TestAthenaExecutionRenderReextractsEveryTimeIntervalStage(t *testing.T) {
	t.Parallel()

	extractor := &athenaTrackingReextractor{}
	asset := &pipeline.Asset{
		Name: "analytics.events",
		Type: pipeline.AssetTypeAthenaQuery,
		Materialization: pipeline.Materialization{
			Type:            pipeline.MaterializationTypeTable,
			Strategy:        pipeline.MaterializationStrategyTimeInterval,
			IncrementalKey:  "event_date",
			TimeGranularity: pipeline.MaterializationTimeGranularityDate,
		},
		Hooks: pipeline.Hooks{
			Pre:  []pipeline.Hook{{Query: "SELECT 'pre'"}},
			Post: []pipeline.Hook{{Query: "SELECT 'post'"}},
		},
	}

	stages, err := renderExactAthenaExecutionStages(
		asset,
		extractor,
		"SELECT event_date FROM source.events",
		athenaTestResultsPath,
		false,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, extractor.calls)
	require.Len(t, extractor.input, 4)
	require.Len(t, stages, 4)
	for index, stage := range stages {
		assert.Equal(t, "execution_sql", stage.Kind)
		assert.Equal(t, fmt.Sprintf("Execution SQL %d", index+1), stage.Label)
		assert.NotContains(t, stage.Content, "{{")
	}
	assert.Contains(t, stages[1].Content, "2026-07-15")
	assert.Contains(t, stages[1].Content, "2026-07-16")
}

func TestAthenaExecutionRenderRequiresConfiguredResultsPath(t *testing.T) {
	t.Parallel()

	connections := &config.Connections{
		AthenaConnection: []config.AthenaConnection{{
			ConnectionMetadata: config.ConnectionMetadata{Name: "athena-default"},
		}},
	}
	cfg := &config.Config{
		DefaultEnvironmentName: "default",
		Environments: map[string]config.Environment{
			"default": {Connections: connections},
		},
	}
	require.NoError(t, cfg.SelectEnvironment("default"))
	asset := &pipeline.Asset{
		Name:       "analytics.report",
		Type:       pipeline.AssetTypeAthenaQuery,
		Connection: "athena-default",
	}
	outcome := renderAthenaExecution(
		context.Background(),
		&directPipelineInfo{
			Pipeline: &pipeline.Pipeline{Name: "analytics", Assets: []*pipeline.Asset{asset}},
			Asset:    asset,
			Config:   cfg,
		},
		nil,
		nil,
		"SELECT 1",
		false,
		false,
	)

	require.True(t, outcome.handled)
	assert.Equal(t, AssetRenderStatusPartial, outcome.status)
	require.Len(t, outcome.stages, 1)
	assert.Equal(t, AssetRenderStageStatusError, outcome.stages[0].Status)
	assert.Contains(t, outcome.stages[0].Message, `selected Athena connection "athena-default" has no query results path`)
}

func TestAthenaExecutionEphemeralIdentifierFidelity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		strategy             pipeline.MaterializationStrategy
		effectiveFullRefresh bool
		wantRuntimeOnly      bool
	}{
		{name: "append", strategy: pipeline.MaterializationStrategyAppend},
		{name: "configured create replace", strategy: pipeline.MaterializationStrategyCreateReplace, wantRuntimeOnly: true},
		{name: "configured delete insert", strategy: pipeline.MaterializationStrategyDeleteInsert, wantRuntimeOnly: true},
		{name: "configured SCD2 by column", strategy: pipeline.MaterializationStrategySCD2ByColumn, wantRuntimeOnly: true},
		{name: "full refresh append", strategy: pipeline.MaterializationStrategyAppend, effectiveFullRefresh: true, wantRuntimeOnly: true},
		{name: "full refresh DDL", strategy: pipeline.MaterializationStrategyDDL, effectiveFullRefresh: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			asset := &pipeline.Asset{
				Type: pipeline.AssetTypeAthenaQuery,
				Materialization: pipeline.Materialization{
					Type:     pipeline.MaterializationTypeTable,
					Strategy: tt.strategy,
				},
			}
			assert.Equal(t, tt.wantRuntimeOnly, athenaExecutionUsesEphemeralIdentifiers(asset, tt.effectiveFullRefresh))
		})
	}
}

func TestDirectAthenaRestrictedFullRefreshUsesEffectiveHookContext(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name                   string
		assetRestriction       string
		environmentRestriction bool
	}{
		{
			name:             "asset restriction",
			assetRestriction: "refresh_restricted: true",
		},
		{
			name:                   "environment restriction",
			environmentRestriction: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  athena: athena-default
`, map[string]string{"report.sql": fmt.Sprintf(`
/* @bruin
name: analytics.report
type: athena.sql
%s
materialization:
  type: table
  strategy: append
hooks:
  pre:
    - query: "SELECT '{{ full_refresh }}' AS pre_hook"
  post:
    - query: "SELECT '{{ full_refresh }}' AS post_hook"
@bruin */
SELECT '{{ full_refresh }}' AS main_query
`, tt.assetRestriction)})
			writeAthenaTestConfig(t, root, athenaTestResultsPath, tt.environmentRestriction)

			connection := &hookParityBatchConnection{}
			executor := newCompatDirectExecutor(root, "")
			executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
				return &stubConnectionManager{conn: connection}, nil
			}
			request := RunAssetRequest{
				AssetPath:   filepath.Join(root, "analytics", "assets", "report.sql"),
				StartDate:   "2026-07-15T00:00:00Z",
				EndDate:     "2026-07-16T00:00:00Z",
				FullRefresh: true,
			}
			_, err := executor.RunAsset(context.Background(), request, nil)
			require.NoError(t, err)

			require.Len(t, connection.queries, 3)
			for _, submitted := range connection.queries {
				assert.Contains(t, submitted, "False")
				assert.NotContains(t, submitted, "True")
			}

			outcome := renderAthenaWorkspaceAsset(t, root, request.AssetPath, request)
			require.True(t, outcome.handled)
			assert.Equal(t, AssetRenderStatusOK, outcome.status)
			assert.Equal(t, connection.queries, athenaStageContents(outcome.stages))
		})
	}
}

func writeAthenaTestConfig(t *testing.T, root, resultsPath string, restrictFullRefresh bool) {
	t.Helper()

	environmentConfig := ""
	if restrictFullRefresh {
		environmentConfig = `
    config:
      full_refresh_restricted: true`
	}
	content := `
default_environment: default
environments:
  default:` + environmentConfig + `
    connections:
      athena:
        - name: athena-default
          access_key_id: test-access-key
          secret_access_key: test-secret-key
          query_results_path: ` + resultsPath + `
          region: eu-central-1
          database: analytics
`
	require.NoError(t, os.WriteFile(filepath.Join(root, ".bruin.yml"), []byte(strings.TrimSpace(content)+"\n"), 0o644))
}

func renderAthenaWorkspaceAsset(t *testing.T, root, assetPath string, request RunAssetRequest) assetRenderSemanticOutcome {
	t.Helper()

	info, err := getDirectPipelineAndAssetReadOnly(context.Background(), root, assetPath, afero.NewOsFs())
	require.NoError(t, err)
	timeWindow, err := ResolveExecutionTimeWindow(
		string(info.Pipeline.Schedule),
		request.StartDate,
		request.EndDate,
		time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	executionTime := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	renderer, err := buildAssetPlanRenderer(afero.NewOsFs(), info.Pipeline, timeWindow, executionTime, "athena-render-test")
	require.NoError(t, err)
	operatorFullRefresh := request.FullRefresh && !selectedEnvironmentRestrictsFullRefresh(info.Config)
	applySelectedEnvironmentRefreshRestriction(info.Config, info.Pipeline.Assets)
	effectiveFullRefresh := operatorFullRefresh && !assetRefreshRestricted(info.Asset)
	renderCtx := assetPlanRenderContext(
		context.Background(),
		info.Config,
		timeWindow,
		executionTime,
		"athena-render-test",
		effectiveFullRefresh,
	)
	extractor, err := newDirectSQLQueryExtractor(afero.NewOsFs(), renderer, info.Asset.Type).
		CloneForAsset(renderCtx, info.Pipeline, info.Asset)
	require.NoError(t, err)
	queries, err := extractor.ExtractQueriesFromString(info.Asset.ExecutableFile.Content)
	require.NoError(t, err)
	compiledQuery, err := compiledQueryForRenderAsset(info.Asset, queries)
	require.NoError(t, err)

	return renderAthenaExecution(
		renderCtx,
		info,
		renderer,
		extractor,
		compiledQuery,
		operatorFullRefresh,
		effectiveFullRefresh,
	)
}

func outcomeAssetForTest(t *testing.T, root, assetPath string) *pipeline.Asset {
	t.Helper()
	info, err := getDirectPipelineAndAssetReadOnly(context.Background(), root, assetPath, afero.NewOsFs())
	require.NoError(t, err)
	return info.Asset
}

type athenaTrackingReextractor struct {
	calls int
	input []string
}

func (e *athenaTrackingReextractor) ExtractQueriesFromString(string) ([]*query.Query, error) {
	return nil, errors.New("not used")
}

func (e *athenaTrackingReextractor) CloneForAsset(context.Context, *pipeline.Pipeline, *pipeline.Asset) (query.QueryExtractor, error) {
	return e, nil
}

func (e *athenaTrackingReextractor) ReextractQueriesFromSlice(content []string) ([]string, error) {
	e.calls++
	e.input = append([]string(nil), content...)
	replacer := strings.NewReplacer(
		"{{start_date}}", "2026-07-15",
		"{{end_date}}", "2026-07-16",
	)
	result := make([]string, len(content))
	for index := range content {
		result[index] = replacer.Replace(content[index])
	}
	return result, nil
}

func athenaStageContents(stages []AssetRenderStage) []string {
	result := make([]string, len(stages))
	for index := range stages {
		result[index] = stages[index].Content
	}
	return result
}

func athenaExecutionStageContents(stages []AssetRenderStage) []string {
	result := make([]string, 0, len(stages))
	for _, stage := range stages {
		if stage.Kind == "execution_sql" {
			result = append(result, stage.Content)
		}
	}
	return result
}
