package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"renart/internal/web/snapshot"
)

func TestAssetRenderServiceRendersExactDuckDBQueryAndExecutionSQL(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
variables:
  threshold:
    type: integer
    default: 100
`, map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
materialization:
  type: table
  strategy: append
hooks:
  pre:
    - query: "SELECT 'render-started'"
  post:
    - query: "SELECT 'render-finished'"
@bruin */
select
  {{ var.threshold }} as threshold,
  '{{ start_date }}' as window_start,
  '{{ execution_timestamp }}' as execution_time,
  '{{ run_id }}' as run_id,
  '{{ this }}' as target
`,
	})

	service := NewAssetRenderService(root)
	result, err := service.RenderPath(context.Background(), "analytics/assets/report.sql", AssetRenderRequest{
		Environment:   "default",
		StartDate:     "2026-07-15T00:00:00Z",
		EndDate:       "2026-07-16T00:00:00Z",
		ExecutionTime: "2026-07-16T12:34:56Z",
		FullRefresh:   true,
	})
	require.NoError(t, err)

	assert.Equal(t, AssetRenderStatusPartial, result.Status)
	assert.Equal(t, "working_tree", result.Provenance.Source.Kind)
	assert.Equal(t, "analytics/pipeline.yml", result.Provenance.Source.PipelinePath)
	assert.Equal(t, "analytics", result.Provenance.Pipeline)
	assert.Equal(t, "default", result.Provenance.Context.Environment)
	assert.Equal(t, "2026-07-15T00:00:00Z", result.Provenance.Context.StartDate)
	assert.Equal(t, "2026-07-16T00:00:00Z", result.Provenance.Context.EndDate)
	assert.Equal(t, "2026-07-16T12:34:56Z", result.Provenance.Context.ExecutionTime)
	assert.Equal(t, assetRenderPreviewRunID, result.Provenance.Context.RunID)
	assert.NotEmpty(t, result.Provenance.Source.MerkleRoot)
	assert.NotEmpty(t, result.Provenance.Context.ConfigurationDigest)
	assert.True(t, result.Provenance.Context.RequestedFullRefresh)
	assert.True(t, result.Provenance.Context.FullRefresh)
	assert.NotEmpty(t, result.Provenance.Context.VariablesDigest)
	assert.Equal(t, "analytics.report", result.Asset.Name)
	assert.Equal(t, "duckdb.sql", result.Asset.Type)
	assert.Equal(t, "duckdb", result.Asset.Dialect)
	assert.Equal(t, "duckdb-default", result.Asset.ConnectionName)

	require.Len(t, result.Stages, 3)
	compiled := result.Stages[0]
	assert.Equal(t, "compiled_query", compiled.Kind)
	assert.Equal(t, AssetRenderStageStatusOK, compiled.Status)
	assert.Equal(t, AssetRenderFidelityExact, compiled.Fidelity)
	assert.Contains(t, compiled.Content, "100 as threshold")
	assert.Contains(t, compiled.Content, "'2026-07-15' as window_start")
	assert.Contains(t, compiled.Content, "'2026-07-16T12:34:56.000000Z' as execution_time")
	assert.Contains(t, compiled.Content, "'renart-render-preview' as run_id")
	assert.Contains(t, compiled.Content, "'analytics.report' as target")

	schema := result.Stages[1]
	assert.Equal(t, "schema_preparation", schema.Kind)
	assert.Equal(t, "CREATE SCHEMA IF NOT EXISTS analytics", schema.Content)
	assert.True(t, schema.Conditional)
	assert.Equal(t, AssetRenderFidelitySemantic, schema.Fidelity)

	execution := result.Stages[2]
	assert.Equal(t, "execution_sql", execution.Kind)
	assert.Equal(t, AssetRenderStageStatusOK, execution.Status)
	assert.Equal(t, AssetRenderFidelityExact, execution.Fidelity)
	assert.Contains(t, execution.Content, "SELECT 'render-started';")
	assert.Contains(t, execution.Content, "DROP TABLE IF EXISTS analytics.report")
	assert.Contains(t, execution.Content, "CREATE TABLE analytics.report AS")
	assert.Contains(t, execution.Content, "SELECT 'render-finished';")
	assert.NotContains(t, execution.Content, "INSERT INTO analytics.report", "full refresh must use the runtime materializer's replace path")
	assert.Empty(t, result.Issues)
}

func TestAssetRenderServiceKeepsCompiledSQLWhenExecutionRendererIsUnsupported(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  postgres: postgres-default
`, map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: pg.sql
materialization:
  type: table
@bruin */
select '{{ start_date }}' as window_start
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/report.sql", AssetRenderRequest{
		StartDate: "2026-07-15T00:00:00Z",
		EndDate:   "2026-07-16T00:00:00Z",
	})
	require.NoError(t, err)

	assert.Equal(t, AssetRenderStatusPartial, result.Status)
	require.Len(t, result.Stages, 2)
	assert.Equal(t, AssetRenderStageStatusOK, result.Stages[0].Status)
	assert.Equal(t, AssetRenderFidelityExact, result.Stages[0].Fidelity)
	assert.Contains(t, result.Stages[0].Content, "'2026-07-15'")
	assert.Equal(t, AssetRenderStageStatusUnsupported, result.Stages[1].Status)
	assert.Equal(t, AssetRenderFidelityUnsupported, result.Stages[1].Fidelity)
	assert.Empty(t, result.Stages[1].Content)
}

func TestAssetRenderServiceUsesQueryParameterForQuerySensors(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
variables:
  threshold:
    type: integer
    default: 10
`, map[string]string{
		"ready.asset.yml": `
name: analytics.ready
type: duckdb.sensor.query
parameters:
  query: |
    select
      {{ var.threshold }} as threshold,
      '{{ start_date }}' as window_start
  poke_interval: 5
  timeout: 60
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/ready.asset.yml", AssetRenderRequest{
		StartDate: "2026-07-15T00:00:00Z",
		EndDate:   "2026-07-16T00:00:00Z",
	})
	require.NoError(t, err)
	require.Equal(t, AssetRenderStatusOK, result.Status)
	assert.Equal(t, "duckdb", result.Asset.Dialect)
	require.Len(t, result.Stages, 2)

	compiled := result.Stages[0]
	assert.Equal(t, "compiled_query", compiled.Kind)
	assert.Equal(t, AssetRenderFidelityExact, compiled.Fidelity)
	assert.Contains(t, compiled.Content, "10 as threshold")
	assert.Contains(t, compiled.Content, "'2026-07-15' as window_start")
	assert.NotContains(t, compiled.Content, "parameters:")
	assert.NotContains(t, compiled.Content, "poke_interval")

	execution := result.Stages[1]
	assert.Equal(t, "execution_sql", execution.Kind)
	assert.Equal(t, AssetRenderStageStatusOK, execution.Status)
	assert.Equal(t, AssetRenderFidelityExact, execution.Fidelity)
	assert.Equal(t, compiled.Content, execution.Content)
	assert.Contains(t, execution.Message, "polling mode")
	assert.Contains(t, execution.Message, "runtime controls")
	assert.Empty(t, result.Issues)
}

func TestAssetRenderServiceReturnsStructuredMissingQuerySensorDiagnostic(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"ready.asset.yml": `
name: analytics.ready
type: duckdb.sensor.query
parameters:
  poke_interval: 5
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/ready.asset.yml", AssetRenderRequest{})
	require.NoError(t, err)
	assert.Equal(t, AssetRenderStatusError, result.Status)
	require.Len(t, result.Stages, 1)
	assert.Equal(t, "compiled_query", result.Stages[0].Kind)
	assert.Equal(t, AssetRenderStageStatusError, result.Stages[0].Status)
	assert.Contains(t, result.Stages[0].Message, `parameter "query" is required`)
	require.Len(t, result.Issues, 1)
	assert.Equal(t, "query_sensor_query_missing", result.Issues[0].Code)
	assert.Equal(t, "error", result.Issues[0].Severity)
}

func TestAssetRenderServiceRendersMotherDuckThroughDuckDBDialect(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  motherduck: motherduck-default
`, map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: motherduck.sql
@bruin */
select '{{ start_date }}' as window_start
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/report.sql", AssetRenderRequest{
		StartDate: "2026-07-15T00:00:00Z",
		EndDate:   "2026-07-16T00:00:00Z",
	})
	require.NoError(t, err)
	require.Equal(t, AssetRenderStatusOK, result.Status)
	assert.Equal(t, "motherduck.sql", result.Asset.Type)
	assert.Equal(t, "duckdb", result.Asset.Dialect)
	assert.Equal(t, "motherduck-default", result.Asset.ConnectionName)
	require.Len(t, result.Stages, 2)
	assert.Equal(t, AssetRenderFidelityExact, result.Stages[0].Fidelity)
	assert.Equal(t, AssetRenderFidelityExact, result.Stages[1].Fidelity)
	assert.Contains(t, result.Stages[1].Content, "'2026-07-15' as window_start")
}

func TestAssetRenderServiceReturnsPartialResultOnMaterializationError(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
materialization:
  type: table
  strategy: merge
@bruin */
select 1 as id
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/report.sql", AssetRenderRequest{
		StartDate: "2026-07-15T00:00:00Z",
		EndDate:   "2026-07-16T00:00:00Z",
	})
	require.NoError(t, err)

	assert.Equal(t, AssetRenderStatusPartial, result.Status)
	require.Len(t, result.Stages, 2)
	assert.Equal(t, AssetRenderStageStatusOK, result.Stages[0].Status)
	assert.Equal(t, "select 1 as id", strings.TrimSpace(result.Stages[0].Content))
	assert.Equal(t, AssetRenderStageStatusError, result.Stages[1].Status)
	assert.Equal(t, AssetRenderFidelityExact, result.Stages[1].Fidelity)
	assert.Contains(t, result.Stages[1].Message, "requires the `columns` field")
}

func TestAssetRenderServiceMarksPythonExecutionRuntimeOnly(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, "name: analytics", map[string]string{
		"task.py": `
""" @bruin
name: analytics.task
type: python
@bruin """

print("hello")
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/task.py", AssetRenderRequest{})
	require.NoError(t, err)

	assert.Equal(t, AssetRenderStatusPartial, result.Status)
	require.Len(t, result.Stages, 1)
	assert.Equal(t, "runtime", result.Stages[0].Kind)
	assert.Equal(t, "json", result.Stages[0].Language)
	assert.Equal(t, AssetRenderStageStatusOK, result.Stages[0].Status)
	assert.Equal(t, AssetRenderFidelityRuntimeOnly, result.Stages[0].Fidelity)
	assert.Contains(t, result.Stages[0].Content, `"operation": "execute_python"`)
	assert.Contains(t, result.Stages[0].Content, `"entrypoint": "analytics/assets/task.py"`)
	assert.NotContains(t, result.Stages[0].Content, `print("hello")`)
}

func TestAssetRenderServiceKeepsDotDotPrefixedWorkspacePathsRelative(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pipelineRoot := filepath.Join(root, "..analytics")
	assetPath := filepath.Join(pipelineRoot, "assets", "task.py")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(assetPath), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".bruin.yml"), []byte(`
default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: duckdb-default
          path: local.db
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(assetPath, []byte(`
""" @bruin
name: analytics.task
type: python
@bruin """
`), 0o644))

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "..analytics/assets/task.py", AssetRenderRequest{})
	require.NoError(t, err)
	assert.Equal(t, "..analytics/pipeline.yml", result.Provenance.Source.PipelinePath)
	require.Len(t, result.Stages, 1)
	assert.Contains(t, result.Stages[0].Content, `"entrypoint": "..analytics/assets/task.py"`)
	assert.NotContains(t, result.Stages[0].Content, filepath.ToSlash(root))
}

func TestAssetRenderServiceDescribesSeedAsSemanticSlingLoad(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"customers.asset.yml": `
name: analytics.customers
type: duckdb.seed
parameters:
  path: https://example.test/customers.csv?token=secret-token&X-Amz-Credential=aws-credential&X-Amz-Signature=aws-signature&X-Amz-Security-Token=aws-session&GoogleAccessId=google-access
  enforce_schema: "true"
columns:
  - name: customer_id
    type: integer
    source: id
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/customers.asset.yml", AssetRenderRequest{})
	require.NoError(t, err)

	assert.Equal(t, AssetRenderStatusOK, result.Status)
	require.Len(t, result.Stages, 1)
	stage := result.Stages[0]
	assert.Equal(t, "materialization", stage.Kind)
	assert.Equal(t, AssetRenderFidelitySemantic, stage.Fidelity)
	assert.Contains(t, stage.Content, `"operation": "sling_load"`)
	assert.Contains(t, stage.Content, `"mode": "full-refresh"`)
	assert.Contains(t, stage.Content, `token=REDACTED`)
	assert.NotContains(t, stage.Content, "secret-token")
	assert.NotContains(t, stage.Content, "aws-credential")
	assert.NotContains(t, stage.Content, "aws-signature")
	assert.NotContains(t, stage.Content, "aws-session")
	assert.NotContains(t, stage.Content, "google-access")
	assert.Contains(t, stage.Content, `"cast": "integer"`)
	require.Len(t, result.Redactions, 1)
}

func TestAssetRenderServiceRejectsSeedSourcesTheRuntimeCannotUse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "missing local file", path: "./missing.csv", want: "is unavailable"},
		{name: "unsupported URL scheme", path: "s3://private-bucket/customers.csv", want: "must use http or https"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
				"customers.asset.yml": `
name: analytics.customers
type: duckdb.seed
parameters:
  path: ` + tt.path + `
`,
			})

			result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/customers.asset.yml", AssetRenderRequest{})
			require.NoError(t, err)
			assert.Equal(t, AssetRenderStatusError, result.Status)
			require.Len(t, result.Stages, 1)
			assert.Equal(t, AssetRenderStageStatusError, result.Stages[0].Status)
			assert.Contains(t, result.Stages[0].Message, tt.want)
			require.Len(t, result.Issues, 1)
			assert.Equal(t, "seed_source_invalid", result.Issues[0].Code)
		})
	}
}

func TestAssetRenderServiceDescribesLoadWithoutResolvingConnectionCredentials(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"orders.asset.yml": `
name: analytics.orders
type: load
connection: duckdb-default
parameters:
  source_connection: duckdb-default
  source_table: raw.orders
materialization:
  type: table
  strategy: append
  incremental_key: updated_at
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/orders.asset.yml", AssetRenderRequest{FullRefresh: true})
	require.NoError(t, err)

	assert.Equal(t, AssetRenderStatusOK, result.Status)
	require.Len(t, result.Stages, 1)
	stage := result.Stages[0]
	assert.Equal(t, AssetRenderFidelitySemantic, stage.Fidelity)
	assert.Contains(t, stage.Content, `"operation": "sling_copy"`)
	assert.Contains(t, stage.Content, `"object": "raw.orders"`)
	assert.Contains(t, stage.Content, `"full_refresh": true`)
	assert.Contains(t, stage.Content, `"runtime_options"`)
	assert.NotContains(t, stage.Content, "duckdb://")
}

func TestAssetRenderServiceDescribesTableSensorCondition(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"ready.asset.yml": `
name: analytics.ready
type: duckdb.sensor.table
connection: duckdb-default
parameters:
  table: analytics.orders
  poke_interval: "10"
  timeout: 5m
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/ready.asset.yml", AssetRenderRequest{})
	require.NoError(t, err)

	assert.Equal(t, AssetRenderStatusOK, result.Status)
	require.Len(t, result.Stages, 1)
	assert.Equal(t, "condition", result.Stages[0].Kind)
	assert.Equal(t, AssetRenderFidelitySemantic, result.Stages[0].Fidelity)
	assert.Contains(t, result.Stages[0].Content, `"operation": "wait_for_table"`)
	assert.Contains(t, result.Stages[0].Content, `"table": "analytics.orders"`)
	var operation map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Stages[0].Content), &operation))
	assert.Equal(t, map[string]any{"poke_interval": "10", "timeout": "5m"}, operation["runtime_controls"])
}

func TestAssetRenderServiceValidatesIngestrBeforeDescribingTheOperation(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"events.asset.yml": `
name: analytics.events
type: ingestr
parameters:
  source_connection: source-default
  source_table: public.events
  destination: duckdb
  cdc: "true"
materialization:
  type: table
  strategy: append
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/events.asset.yml", AssetRenderRequest{})
	require.NoError(t, err)
	assert.Equal(t, AssetRenderStatusError, result.Status)
	require.Len(t, result.Stages, 1)
	assert.Equal(t, AssetRenderStageStatusError, result.Stages[0].Status)
	assert.Equal(t, AssetRenderFidelitySemantic, result.Stages[0].Fidelity)
	assert.Contains(t, result.Stages[0].Content, `"operation": "ingestr_copy"`)
	assert.Contains(t, result.Stages[0].Message, "require incremental strategy 'merge'")
	require.NotEmpty(t, result.Issues)
	assert.Equal(t, "ingestr_configuration_invalid", result.Issues[0].Code)
}

func TestAssetRenderServiceShowsEffectiveIngestrStrategy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		parameters string
		want       string
	}{
		{
			name: "parameters append",
			parameters: `
  incremental_strategy: append`,
			want: "append",
		},
		{
			name: "CDC defaults to merge",
			parameters: `
  cdc: "true"`,
			want: "merge",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
				"events.asset.yml": `
name: analytics.events
type: ingestr
parameters:
  source_connection: source-default
  source_table: public.events
  destination: duckdb` + tt.parameters + `
`,
			})

			result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/events.asset.yml", AssetRenderRequest{})
			require.NoError(t, err)
			assert.Equal(t, AssetRenderStatusOK, result.Status)
			require.Len(t, result.Stages, 1)
			assert.Equal(t, AssetRenderStageStatusOK, result.Stages[0].Status)
			assert.Contains(t, result.Stages[0].Content, `"effective_strategy": "`+tt.want+`"`)
		})
	}
}

func TestAssetRenderServiceDescribesAPIExtractionAndRedactsCredentials(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"events.asset.yml": `
name: analytics.events
type: api
connection: duckdb-default
materialization:
  type: table
  strategy: create+replace
parameters:
  request:
    url: https://api-user:api-password@example.test/events?api_key=secret-key&X-Goog-Credential=google-credential&X-Goog-Signature=google-signature
    method: GET
    headers:
      Authorization: Bearer secret-header
      Accept: application/json
  auth:
    type: bearer
    token: another-secret
  response:
    records_path: events
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/events.asset.yml", AssetRenderRequest{})
	require.NoError(t, err)

	assert.Equal(t, AssetRenderStatusOK, result.Status)
	require.Len(t, result.Stages, 2)
	assert.Equal(t, "extraction", result.Stages[0].Kind)
	assert.Equal(t, "materialization", result.Stages[1].Kind)
	assert.Contains(t, result.Stages[0].Content, "api_key=REDACTED")
	assert.Contains(t, result.Stages[0].Content, `"Authorization"`)
	assert.NotContains(t, result.Stages[0].Content, "secret-key")
	assert.NotContains(t, result.Stages[0].Content, "api-user")
	assert.NotContains(t, result.Stages[0].Content, "api-password")
	assert.NotContains(t, result.Stages[0].Content, "secret-header")
	assert.NotContains(t, result.Stages[0].Content, "another-secret")
	assert.NotContains(t, result.Stages[0].Content, "google-credential")
	assert.NotContains(t, result.Stages[0].Content, "google-signature")
	assert.Contains(t, result.Stages[1].Content, `"operation": "sling_load_jsonlines"`)
	require.Len(t, result.Redactions, 1)
}

func TestAssetRenderServiceRejectsInvalidExecutionContext(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, "name: analytics", map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
@bruin */
select 1
`,
	})

	_, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/report.sql", AssetRenderRequest{
		ExecutionTime: "not-a-time",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid execution_time")
}

func TestAssetRenderServiceReturnsStructuredRequestContextErrors(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, "name: analytics", map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
@bruin */
select 1
`,
	})
	service := NewAssetRenderService(root)
	assetID := EncodeID("analytics/assets/report.sql")

	tests := []struct {
		name    string
		request AssetRenderRequest
		code    string
	}{
		{
			name:    "execution time",
			request: AssetRenderRequest{ExecutionTime: "not-a-time"},
			code:    "invalid_execution_time",
		},
		{
			name:    "environment",
			request: AssetRenderRequest{Environment: "missing"},
			code:    "invalid_environment",
		},
		{
			name:    "incomplete window",
			request: AssetRenderRequest{StartDate: "2026-07-15T00:00:00Z"},
			code:    "invalid_time_window",
		},
		{
			name: "reversed window",
			request: AssetRenderRequest{
				StartDate: "2026-07-16T00:00:00Z",
				EndDate:   "2026-07-15T00:00:00Z",
			},
			code: "invalid_time_window",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, apiErr := service.RenderAsset(context.Background(), assetID, test.request)
			require.NotNil(t, apiErr)
			assert.Equal(t, 400, apiErr.Status)
			assert.Equal(t, test.code, apiErr.Code)
			assert.NotContains(t, apiErr.Message, root)
		})
	}
}

func TestAssetRenderServiceRerendersDuckDBTimeIntervalMaterialization(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"events.sql": `
/* @bruin
name: analytics.events
type: duckdb.sql
materialization:
  type: table
  strategy: time_interval
  incremental_key: event_date
  time_granularity: date
@bruin */
select DATE '{{ start_date }}' as event_date
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/events.sql", AssetRenderRequest{
		StartDate: "2026-07-15T00:00:00Z",
		EndDate:   "2026-07-16T00:00:00Z",
	})
	require.NoError(t, err)
	require.Equal(t, AssetRenderStatusPartial, result.Status)
	require.Len(t, result.Stages, 3)

	execution := result.Stages[2].Content
	assert.Contains(t, execution, "DELETE FROM analytics.events WHERE event_date BETWEEN '2026-07-15' AND '2026-07-16'")
	assert.Contains(t, execution, "INSERT INTO analytics.events select DATE '2026-07-15' as event_date")
	assert.NotContains(t, execution, "{{start_date}}")
	assert.NotContains(t, execution, "{{end_date}}")
}

func TestAssetRenderServiceHonorsEnvironmentRefreshRestriction(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"events.sql": `
/* @bruin
name: analytics.events
type: duckdb.sql
materialization:
  type: table
  strategy: append
@bruin */
select 1 as id, '{{ full_refresh }}' as is_full_refresh
`,
	})
	require.NoError(t, os.WriteFile(filepath.Join(root, ".bruin.yml"), []byte(strings.TrimSpace(`
default_environment: prod
environments:
  prod:
    config:
      full_refresh_restricted: true
    connections:
      duckdb:
        - name: duckdb-default
          path: local.db
`)+"\n"), 0o644))

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/events.sql", AssetRenderRequest{
		Environment: "prod",
		FullRefresh: true,
	})
	require.NoError(t, err)
	require.Equal(t, AssetRenderStatusPartial, result.Status)
	require.Len(t, result.Stages, 3)
	assert.True(t, result.Provenance.Context.RequestedFullRefresh)
	assert.False(t, result.Provenance.Context.FullRefresh)
	require.Len(t, result.Issues, 1)
	assert.Equal(t, "full_refresh_restricted", result.Issues[0].Code)
	assert.Contains(t, result.Stages[0].Content, "'False' as is_full_refresh")
	assert.NotContains(t, result.Stages[0].Content, "'True' as is_full_refresh")

	execution := result.Stages[2].Content
	assert.Contains(t, execution, "INSERT INTO analytics.events select 1 as id")
	assert.NotContains(t, execution, "DROP TABLE IF EXISTS analytics.events")
}

func TestAssetRenderServiceUsesRuntimeHookAndDeclareOrder(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
variables:
  hook_label:
    type: string
    default: rendered-post
`, map[string]string{
		"script.sql": `
/* @bruin
name: analytics.script
type: duckdb.sql
hooks:
  pre:
    - query: "SELECT '{{ start_date }}' AS pre"
  post:
    - query: "SELECT '{{ var.hook_label }}' AS post"
@bruin */
SELECT 'first';
DECLARE marker INTEGER;
SELECT 'second';
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/script.sql", AssetRenderRequest{
		StartDate: "2026-07-15T00:00:00Z",
		EndDate:   "2026-07-16T00:00:00Z",
	})
	require.NoError(t, err)
	require.Equal(t, AssetRenderStatusOK, result.Status)
	require.Len(t, result.Stages, 2)

	compiled := result.Stages[0].Content
	assert.Less(t, strings.Index(compiled, "SELECT 'first'"), strings.Index(compiled, "DECLARE marker"))
	assert.Less(t, strings.Index(compiled, "DECLARE marker"), strings.Index(compiled, "SELECT 'second'"))

	// The preview and direct executor share Bruin's Rust DECLARE hoister, so the
	// rendered script has the same ordering as Bruin render/run.
	execution := result.Stages[1].Content
	pre := strings.Index(execution, "SELECT '2026-07-15' AS pre")
	first := strings.Index(execution, "SELECT 'first'")
	declare := strings.Index(execution, "DECLARE marker")
	second := strings.Index(execution, "SELECT 'second'")
	post := strings.Index(execution, "SELECT 'rendered-post' AS post")
	require.NotEqual(t, -1, pre)
	require.NotEqual(t, -1, first)
	require.NotEqual(t, -1, declare)
	require.NotEqual(t, -1, second)
	require.NotEqual(t, -1, post)
	assert.NotContains(t, execution, "{{")
	assert.Less(t, declare, pre)
	assert.Less(t, pre, first)
	assert.Less(t, first, second)
	assert.Less(t, second, post)
}

func TestAssetRenderServiceDoesNotCreateConfigOrGitignore(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, "name: analytics", map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
@bruin */
select 1
`,
	})
	require.NoError(t, os.Remove(filepath.Join(root, ".bruin.yml")))
	require.NoError(t, os.RemoveAll(filepath.Join(root, ".gitignore")))
	before := renderWorkspaceFiles(t, root)

	_, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/report.sql", AssetRenderRequest{})
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(root, ".bruin.yml"))
	assert.NoFileExists(t, filepath.Join(root, ".gitignore"))
	assert.Equal(t, before, renderWorkspaceFiles(t, root))
}

func TestAssetRenderServiceRedactsConfiguredCredentialsBeforeReturningSQL(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
@bruin */
select 'render-secret-token' as example
`,
	})
	require.NoError(t, os.WriteFile(filepath.Join(root, ".bruin.yml"), []byte(strings.TrimSpace(`
default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: duckdb-default
          path: local.db
      postgres:
        - name: unused-postgres
          host: localhost
          port: 5432
          database: analytics
          username: renart
          password: render-secret-token
`)+"\n"), 0o644))

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/report.sql", AssetRenderRequest{
		Environment: "default",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Stages)
	for _, stage := range result.Stages {
		assert.NotContains(t, stage.Content, "render-secret-token")
		assert.NotContains(t, stage.Message, "render-secret-token")
	}
	assert.Contains(t, result.Stages[0].Content, "****")
	assert.True(t, result.Stages[0].Redacted)
	require.Len(t, result.Redactions, 1)
	assert.Equal(t, "connection_credentials", result.Redactions[0].Kind)
	assert.Equal(t, "****", result.Redactions[0].Replacement)
}

func TestAssetRenderServiceHTTPEntryOwnsAssetPathAndRunID(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, "name: analytics", map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
@bruin */
select '{{ run_id }}' as run_id
`,
	})
	service := NewAssetRenderService(root)

	result, apiErr := service.RenderAsset(
		context.Background(),
		EncodeID("analytics/assets/report.sql"),
		AssetRenderRequest{},
	)
	require.Nil(t, apiErr)
	assert.Equal(t, assetRenderPreviewRunID, result.Provenance.Context.RunID)
	require.NotEmpty(t, result.Stages)
	assert.Contains(t, result.Stages[0].Content, assetRenderPreviewRunID)

	_, apiErr = service.RenderAsset(context.Background(), "not-base64!", AssetRenderRequest{})
	require.NotNil(t, apiErr)
	assert.Equal(t, 400, apiErr.Status)
	assert.Equal(t, "invalid_asset_id", apiErr.Code)

	_, apiErr = service.RenderAsset(
		context.Background(),
		EncodeID("analytics/assets/deleted.sql"),
		AssetRenderRequest{},
	)
	require.NotNil(t, apiErr)
	assert.Equal(t, 404, apiErr.Status)
	assert.Equal(t, "asset_not_found", apiErr.Code)
}

func TestAssetRenderServiceRejectsSourceChangesDuringRender(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, "name: analytics", map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
@bruin */
select 1
`,
	})
	newDriftingService := func() (*AssetRenderService, *int) {
		service := NewAssetRenderService(root)
		manifestCalls := new(int)
		collectManifest := service.collectManifest
		service.collectManifest = func(pipelineDir string) (map[string]string, error) {
			*manifestCalls = *manifestCalls + 1
			return collectManifest(pipelineDir)
		}
		collectSourceState := service.collectSourceState
		stateCalls := 0
		service.collectSourceState = func(pipelineDir string) (snapshot.SourceState, error) {
			stateCalls++
			if stateCalls == 3 {
				path := filepath.Join(pipelineDir, "assets", "report.sql")
				file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					return snapshot.SourceState{}, err
				}
				if _, err := file.WriteString("-- concurrent edit\n"); err != nil {
					_ = file.Close()
					return snapshot.SourceState{}, err
				}
				if err := file.Close(); err != nil {
					return snapshot.SourceState{}, err
				}
			}
			return collectSourceState(pipelineDir)
		}
		return service, manifestCalls
	}

	driftingService, manifestCalls := newDriftingService()
	_, err := driftingService.RenderPath(
		context.Background(),
		"analytics/assets/report.sql",
		AssetRenderRequest{},
	)
	require.ErrorIs(t, err, ErrAssetRenderSourceChanged)
	assert.Equal(t, 1, *manifestCalls, "rendering must hash source contents only once")

	driftingService, manifestCalls = newDriftingService()
	_, apiErr := driftingService.RenderAsset(
		context.Background(),
		EncodeID("analytics/assets/report.sql"),
		AssetRenderRequest{},
	)
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Status)
	assert.Equal(t, "source_changed", apiErr.Code)
	assert.NotContains(t, apiErr.Message, root)
	assert.Equal(t, 1, *manifestCalls, "HTTP rendering must hash source contents only once")
}

func TestAssetRenderServiceSanitizesHTTPFailuresButKeepsInProcessDetail(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, "name: analytics", map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
@bruin */
select 1
`,
	})
	service := NewAssetRenderService(root)
	internalDetail := filepath.Join(root, ".bruin.yml") + ": credential render-secret-token"
	service.collectManifest = func(string) (map[string]string, error) {
		return nil, errors.New(internalDetail)
	}

	_, apiErr := service.RenderAsset(
		context.Background(),
		EncodeID("analytics/assets/report.sql"),
		AssetRenderRequest{},
	)
	require.NotNil(t, apiErr)
	assert.Equal(t, 500, apiErr.Status)
	assert.Equal(t, "source_identity_failed", apiErr.Code)
	assert.Equal(t, "asset source identity could not be computed", apiErr.Message)
	assert.NotContains(t, apiErr.Message, root)
	assert.NotContains(t, apiErr.Message, "render-secret-token")

	_, err := service.RenderPath(
		context.Background(),
		"analytics/assets/report.sql",
		AssetRenderRequest{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), internalDetail)
}

func renderWorkspaceFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = string(content)
		return nil
	}))
	return files
}

func TestAssetRenderServiceMarksDeveloperEnvironmentRewriteRuntimeOnly(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"events.sql": `
/* @bruin
name: analytics.events
type: duckdb.sql
materialization:
  type: table
@bruin */
select * from analytics.source_events
`,
	})
	require.NoError(t, os.WriteFile(filepath.Join(root, ".bruin.yml"), []byte(strings.TrimSpace(`
default_environment: dev
environments:
  dev:
    schema_prefix: dev_
    connections:
      duckdb:
        - name: duckdb-default
          path: local.db
`)+"\n"), 0o644))

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/events.sql", AssetRenderRequest{
		Environment: "dev",
	})
	require.NoError(t, err)
	require.Equal(t, AssetRenderStatusPartial, result.Status)
	require.Len(t, result.Stages, 3)

	execution := result.Stages[2]
	assert.Equal(t, AssetRenderStageStatusOK, execution.Status)
	assert.Equal(t, AssetRenderFidelityRuntimeOnly, execution.Fidelity)
	assert.Contains(t, execution.Content, "analytics.source_events")
	assert.Contains(t, execution.Message, "depends on live warehouse state")
}

func TestAssetRenderServiceMarksGeneratedTemporaryIdentifiersRuntimeOnly(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"events.sql": `
/* @bruin
name: analytics.events
type: duckdb.sql
materialization:
  type: table
  strategy: merge
columns:
  - name: id
    type: BIGINT
    primary_key: true
  - name: value
    type: VARCHAR
    update_on_merge: true
@bruin */
select 1 as id, 'value' as value
`,
	})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/events.sql", AssetRenderRequest{})
	require.NoError(t, err)
	require.Equal(t, AssetRenderStatusPartial, result.Status)
	require.Len(t, result.Stages, 3)

	execution := result.Stages[2]
	assert.Equal(t, AssetRenderStageStatusOK, execution.Status)
	assert.Equal(t, AssetRenderFidelityRuntimeOnly, execution.Fidelity)
	assert.Contains(t, execution.Content, "__bruin_merge_tmp_")
	assert.Contains(t, execution.Message, "temporary table identifiers")
}

func TestAssetRenderServiceMatchesMultipleQueryMaterializationGuard(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{
		Materialization: pipeline.Materialization{Type: pipeline.MaterializationTypeTable},
	}
	_, err := compiledQueryForRenderAsset(asset, []*query.Query{
		{Query: "select 1"},
		{Query: "select 2"},
	})

	require.EqualError(t, err, "cannot enable materialization for tasks with multiple queries")
}

func TestAssetRenderServiceRejectsPathsOutsideWorkspace(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, "name: analytics", map[string]string{
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
@bruin */
select 1
`,
	})
	service := NewAssetRenderService(root)

	for _, assetPath := range []string{
		"../outside.sql",
		filepath.Join(root, "analytics", "assets", "report.sql"),
	} {
		_, err := service.RenderPath(context.Background(), assetPath, AssetRenderRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "workspace")
	}

	outsideDir := t.TempDir()
	outsideAsset := filepath.Join(outsideDir, "outside.sql")
	require.NoError(t, os.WriteFile(outsideAsset, []byte("select 2\n"), 0o644))
	linkedAsset := filepath.Join(root, "analytics", "assets", "linked.sql")
	if err := os.Symlink(outsideAsset, linkedAsset); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := service.RenderPath(context.Background(), "analytics/assets/linked.sql", AssetRenderRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stay within the workspace")
}
