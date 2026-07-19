package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bruin-data/bruin/pkg/ansisql"
	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cliCompatExecutor struct {
	workspaceRoot string
	binaryPath    string
}

func NewCLIBruinExecutor(workspaceRoot, binaryPath string) *cliCompatExecutor {
	return &cliCompatExecutor{workspaceRoot: workspaceRoot, binaryPath: binaryPath}
}

func (e *cliCompatExecutor) RunAsset(ctx context.Context, req RunAssetRequest, onChunk func([]byte)) ([]byte, error) {
	args := []string{"run"}
	if strings.TrimSpace(req.Environment) != "" {
		args = append(args, "--env", req.Environment)
	}
	if strings.TrimSpace(req.SensorMode) != "" {
		args = append(args, "--sensor-mode", req.SensorMode)
	}
	args = append(args, req.AssetPath)
	return e.runMaybeStreaming(ctx, args, onChunk)
}

func (e *cliCompatExecutor) RunPipeline(ctx context.Context, req RunPipelineRequest, onChunk func([]byte)) ([]byte, error) {
	args := []string{"run"}
	if strings.TrimSpace(req.Environment) != "" {
		args = append(args, "--env", req.Environment)
	}
	if strings.TrimSpace(req.SensorMode) != "" {
		args = append(args, "--sensor-mode", req.SensorMode)
	}
	args = append(args, req.Target)
	return e.runMaybeStreaming(ctx, args, onChunk)
}

func (e *cliCompatExecutor) runMaybeStreaming(ctx context.Context, args []string, onChunk func([]byte)) ([]byte, error) {
	cmd := exec.CommandContext(ctx, e.binaryPath, args...)
	cmd.Dir = e.workspaceRoot
	cmd.Env = append(os.Environ(), "TELEMETRY_OPTOUT=1")

	if onChunk == nil {
		return cmd.CombinedOutput()
	}

	buffer := bytes.NewBuffer(nil)
	writer := &streamCaptureWriter{onChunk: onChunk, buffer: buffer}
	cmd.Stdout = writer
	cmd.Stderr = writer
	err := cmd.Run()
	return buffer.Bytes(), err
}

type stubConnectionManager struct {
	conn           any
	connectionType string
}

func (s *stubConnectionManager) GetConnection(_ string) any {
	return s.conn
}

func (s *stubConnectionManager) GetConnectionDetails(_ string) any {
	return nil
}

func (s *stubConnectionManager) GetConnectionType(_ string) string {
	if s.connectionType != "" {
		return s.connectionType
	}
	return "stub"
}

func TestFillDirectColumnsFromDBRejectsMissingPipelineInfo(t *testing.T) {
	t.Parallel()

	status, err := fillDirectColumnsFromDB(context.Background(), &directPipelineInfo{}, afero.NewMemMapFs(), "", nil)

	assert.Equal(t, fillStatusFailed, status)
	require.Error(t, err)
}

func TestUpdateDirectAssetDependenciesRejectsMissingPipelineInfo(t *testing.T) {
	t.Parallel()

	err := updateDirectAssetDependencies(context.Background(), nil, nil, nil, nil, afero.NewMemMapFs())

	require.Error(t, err)
}

type stubSchemaQuerier struct {
	query string
}

func (s *stubSchemaQuerier) SelectWithSchema(_ context.Context, q *query.Query) (*query.QueryResult, error) {
	s.query = q.Query
	return &query.QueryResult{
		Columns:     []string{"id", "name"},
		ColumnTypes: []string{"INTEGER", "VARCHAR"},
		Rows: [][]interface{}{
			{1, "alice"},
		},
	}, nil
}

func (s *stubSchemaQuerier) RunQueryWithoutResult(_ context.Context, q *query.Query) error {
	s.query = q.Query
	return nil
}

func (s *stubSchemaQuerier) Select(_ context.Context, q *query.Query) ([][]interface{}, error) {
	s.query = q.Query
	return [][]interface{}{{1, "alice"}}, nil
}

func (s *stubSchemaQuerier) Ping(_ context.Context) error { return nil }

func (s *stubSchemaQuerier) GetDatabaseSummary(_ context.Context) (*ansisql.DBDatabase, error) {
	return &ansisql.DBDatabase{}, nil
}

type stubDuckDBLogicalSchemaQuerier struct {
	query string
}

func (s *stubDuckDBLogicalSchemaQuerier) SelectWithSchema(_ context.Context, q *query.Query) (*query.QueryResult, error) {
	s.query = q.Query
	return &query.QueryResult{
		Columns:     []string{"column_name", "column_type", "null", "key", "default", "extra"},
		ColumnTypes: []string{"VARCHAR", "VARCHAR", "VARCHAR", "VARCHAR", "VARCHAR", "VARCHAR"},
		Rows: [][]interface{}{
			{"id", "VARCHAR", "YES", nil, nil, nil},
			{[]byte("geometry"), []byte("JSON"), "YES", nil, nil, nil},
		},
	}, nil
}

type stubComplexSchemaQuerier struct {
	queries []string
}

func (s *stubComplexSchemaQuerier) SelectWithSchema(_ context.Context, q *query.Query) (*query.QueryResult, error) {
	s.queries = append(s.queries, q.Query)
	switch len(s.queries) {
	case 1:
		return nil, fmt.Errorf("not yet implemented populating from columns of type struct<test: int32>")
	case 2:
		return &query.QueryResult{
			Columns:     []string{"a", "b", "c", "plain value"},
			ColumnTypes: []string{"STRUCT(test INTEGER)", "INTEGER[]", "STRUCT(test INTEGER)[]", "VARCHAR"},
		}, nil
	default:
		return &query.QueryResult{
			Columns:     []string{"a", "b", "c", "plain value"},
			ColumnTypes: []string{"JSON", "JSON", "JSON", "VARCHAR"},
			Rows: [][]interface{}{{
				`{"test":1}`,
				`[1,2,4]`,
				`[{"test":1},{"test":1}]`,
				"ok",
			}},
		}, nil
	}
}

func (s *stubSchemaQuerier) CreateSchemaIfNotExist(_ context.Context, _ *pipeline.Asset) error {
	return nil
}

func (s *stubSchemaQuerier) PushColumnDescriptions(_ context.Context, _ *pipeline.Asset) error {
	return nil
}

var _ config.ConnectionAndDetailsGetter = (*stubConnectionManager)(nil)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func TestHybridBruinExecutorQueryConnectionUsesDirectPath(t *testing.T) {
	t.Parallel()

	querier := &stubSchemaQuerier{}
	executor := NewHybridBruinExecutor(
		".",
		"bruin",
		func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
			return &stubConnectionManager{conn: querier}, nil
		},
		nil,
	)

	output, err := executor.QueryConnection(context.Background(), QueryConnectionRequest{
		ConnectionName: "warehouse",
		Query:          "select 1 as id, 'alice' as name",
		Output:         "json",
	})
	require.NoError(t, err)
	assert.Equal(t, "select 1 as id, 'alice' as name", querier.query)
	assert.JSONEq(t, `{
		"columns": [
			{"name": "id", "type": "INTEGER"},
			{"name": "name", "type": "VARCHAR"}
		],
		"rows": [[1, "alice"]],
		"connectionName": "warehouse",
		"query": "select 1 as id, 'alice' as name"
	}`, string(output))
}

func TestHybridBruinExecutorQueryConnectionUsesDuckDBLogicalSchema(t *testing.T) {
	t.Parallel()

	querier := &stubDuckDBLogicalSchemaQuerier{}
	executor := NewHybridBruinExecutor(
		".",
		"bruin",
		func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
			return &stubConnectionManager{conn: querier, connectionType: "duckdb"}, nil
		},
		nil,
	)

	output, err := executor.QueryConnection(context.Background(), QueryConnectionRequest{
		ConnectionName: "warehouse",
		Query:          `select * from "example"."api" limit 1;`,
		Output:         "json",
		LogicalSchema:  true,
	})
	require.NoError(t, err)
	assert.Equal(t, `DESCRIBE select * from "example"."api" limit 1`, querier.query)
	assert.JSONEq(t, `{
		"columns": [
			{"name": "id", "type": "VARCHAR"},
			{"name": "geometry", "type": "JSON"}
		],
		"rows": [],
		"connectionName": "warehouse",
		"query": "select * from \"example\".\"api\" limit 1;"
	}`, string(output))
}

func TestDirectRunTerminalErrorStatusPreservesCancellation(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "cancelled", directRunTerminalErrorStatus(context.Background(), context.Canceled))
	assert.Equal(t, "cancelled", directRunTerminalErrorStatus(context.Background(), context.DeadlineExceeded))

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Equal(t, "cancelled", directRunTerminalErrorStatus(cancelled, errors.New("operator stopped")))
	assert.Equal(t, "failed", directRunTerminalErrorStatus(context.Background(), errors.New("warehouse failed")))
}

func TestSelectWithComplexJSONFallbackRewritesOnlyComplexColumns(t *testing.T) {
	t.Parallel()

	querier := &stubComplexSchemaQuerier{}
	queryText := "select struct_pack(test := 1) a, [1,2,4] b, [struct_pack(test := 1)] c, 'ok' as \"plain value\""
	result, err := selectWithComplexJSONFallback(context.Background(), querier, queryText)

	require.NoError(t, err)
	require.Len(t, querier.queries, 3)
	assert.Equal(t, "SELECT * FROM (\n"+queryText+"\n) AS renart_schema_query LIMIT 0", querier.queries[1])
	assert.Equal(t, "SELECT to_json(a) AS a, to_json(b) AS b, to_json(c) AS c, \"plain value\" FROM (\n"+queryText+"\n) AS renart_complex_query", querier.queries[2])
	assert.Equal(t, [][]interface{}{{`{"test":1}`, `[1,2,4]`, `[{"test":1},{"test":1}]`, "ok"}}, result.Rows)
}

func TestHybridBruinExecutorQueryAssetUsesDirectPath(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".bruin.yml"), []byte(strings.TrimSpace(`
default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: duckdb-default
          path: duckdb-files/local.db
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	assetPath := filepath.Join(assetsRoot, "customers.sql")
	require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
@bruin */

select 1 as customer_id,'Ada' as customer_name union all select 2 as customer_id,'Grace' as customer_name
`)+"\n"), 0o644))
	querier := &stubSchemaQuerier{}
	executor := NewHybridBruinExecutor(
		workspaceRoot,
		"bruin",
		nil,
		func() *pipeline.Builder {
			osFS := afero.NewOsFs()
			return pipeline.NewBuilder(
				BuilderConfig,
				pipeline.CreateTaskFromYamlDefinition(osFS),
				pipeline.CreateTaskFromFileComments(osFS),
				osFS,
				DefaultGlossaryReader,
				jinja.VariantRendererFactory,
			)
		},
	)
	executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
		return &stubConnectionManager{conn: querier}, nil
	}

	output, err := executor.QueryAsset(context.Background(), QueryAssetRequest{
		AssetPath: assetPath,
		Limit:     "200",
		Output:    "json",
	})
	require.NoError(t, err)
	assert.Contains(t, querier.query, "SELECT 1 AS customer_id")
	assert.Contains(t, querier.query, "LIMIT 200")
	assert.JSONEq(t, `{
		"columns": [
			{"name": "id", "type": "INTEGER"},
			{"name": "name", "type": "VARCHAR"}
		],
		"rows": [[1, "alice"]],
		"connectionName": "duckdb-default",
		"query": "SELECT 1 AS customer_id, 'Ada' AS customer_name UNION ALL SELECT 2 AS customer_id, 'Grace' AS customer_name LIMIT 200"
	}`, string(output))
}

func TestHybridBruinExecutorFormatAssetUsesDirectPath(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	assetPath := filepath.Join(workspaceRoot, "customers.sql")
	require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
@bruin */
select 1 as customer_id
`)+"\n"), 0o644))

	executor := NewHybridBruinExecutor(workspaceRoot, "bruin", nil, nil)
	output, err := executor.FormatAsset(context.Background(), FormatAssetRequest{
		AssetPath: assetPath,
	})
	require.NoError(t, err)
	assert.Empty(t, output)

	formattedBytes, err := os.ReadFile(assetPath)
	require.NoError(t, err)
	assert.Contains(t, string(formattedBytes), "  type: view")
	assert.Contains(t, string(formattedBytes), "select 1 as customer_id")
}

func TestHybridBruinExecutorQueryAssetRejectsWriteQueries(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".bruin.yml"), []byte(strings.TrimSpace(`
default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: duckdb-default
          path: duckdb-files/local.db
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	assetPath := filepath.Join(assetsRoot, "customers.sql")
	require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
@bruin */

delete from analytics.customers
`)+"\n"), 0o644))

	executor := NewHybridBruinExecutor(
		workspaceRoot,
		"bruin",
		nil,
		func() *pipeline.Builder {
			osFS := afero.NewOsFs()
			return pipeline.NewBuilder(
				BuilderConfig,
				pipeline.CreateTaskFromYamlDefinition(osFS),
				pipeline.CreateTaskFromFileComments(osFS),
				osFS,
				DefaultGlossaryReader,
				jinja.VariantRendererFactory,
			)
		},
	)

	output, err := executor.QueryAsset(context.Background(), QueryAssetRequest{
		AssetPath: assetPath,
		Limit:     "200",
		Output:    "json",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), inspectReadOnlyErrorMessage)
	assert.JSONEq(t, `{"error":"`+inspectReadOnlyErrorMessage+`"}`, string(output))
}

func TestHybridBruinExecutorRunAssetSupportsOracle(t *testing.T) {
	t.Parallel()

	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeOracleQuery}, &pipeline.Pipeline{}))
}

func TestHybridBruinExecutorRunAssetAllowsMetadataPushPipelines(t *testing.T) {
	t.Parallel()

	assert.False(t, shouldFallbackToCLIRunAsset(
		&pipeline.Asset{Type: pipeline.AssetTypeBigqueryQuery},
		&pipeline.Pipeline{MetadataPush: pipeline.MetadataPush{Global: true}},
	))
}

func TestHybridBruinExecutorRunPipelineFallsBackForUnsupportedCases(t *testing.T) {
	t.Parallel()

	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypePostgresQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeRedshiftQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets:       []*pipeline.Asset{{Type: pipeline.AssetTypeDuckDBQuery}},
		MetadataPush: pipeline.MetadataPush{Global: true},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeDuckDBQuery, Columns: []pipeline.Column{{Name: "id"}}}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeDuckDBQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeMotherduckQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypePostgresQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeBigqueryQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeAthenaQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeDatabricksQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeFabricQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeFabricQueryLegacy}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeMySQLQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeSnowflakeQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeMsSQLQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeSynapseQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeClickHouse}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeTrinoQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeVerticaQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypeOracleQuery}},
	}))
	assert.False(t, shouldFallbackToCLIRunPipeline(&pipeline.Pipeline{
		Assets: []*pipeline.Asset{{Type: pipeline.AssetTypePostgresQuerySensor}},
	}))
}

func TestHybridBruinExecutorRunAssetSupportsSimpleSQLAssets(t *testing.T) {
	t.Parallel()

	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeMotherduckQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypePostgresQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeRedshiftQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeBigqueryQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeAthenaQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeDatabricksQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeFabricQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeFabricQueryLegacy}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeMySQLQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeSnowflakeQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeMsSQLQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeSynapseQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeClickHouse}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeTrinoQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeVerticaQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypePostgresQuery, Columns: []pipeline.Column{{Name: "id"}}}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypePostgresQuery, CustomChecks: []pipeline.CustomCheck{{Name: "row_count"}}}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeOracleQuery}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeDuckDBSeed}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeBigquerySeed}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypePostgresSeed}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: assetTypeTrinoSeed}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypePostgresQuerySensor}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeS3KeySensor}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypePython}, &pipeline.Pipeline{}))
	assert.False(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetTypeIngestr}, &pipeline.Pipeline{}))
	assert.True(t, shouldFallbackToCLIRunAsset(&pipeline.Asset{Type: pipeline.AssetType("unknown.custom")}, &pipeline.Pipeline{}))
}

func TestDirectRunAssetWithCustomChecksDoesNotFallbackToCLI(t *testing.T) {
	t.Parallel()

	assert.False(t, shouldFallbackToCLIRunAsset(
		&pipeline.Asset{
			Type:         pipeline.AssetTypeDuckDBQuery,
			CustomChecks: []pipeline.CustomCheck{{Name: "row_count_positive"}},
		},
		&pipeline.Pipeline{},
	))
}

func TestDirectFormatAssetMatchesCLIFormatting(t *testing.T) {
	t.Parallel()

	bruinBinary := strings.TrimSpace(os.Getenv("BRUIN_COMPAT_BINARY"))
	if _, err := os.Stat(bruinBinary); err != nil {
		t.Skip("compatibility binary not available")
	}

	workspaceRoot := t.TempDir()
	directPath := filepath.Join(workspaceRoot, "direct.sql")
	cliPath := filepath.Join(workspaceRoot, "cli.sql")
	input := strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
@bruin */
select 1 as customer_id
`) + "\n"
	require.NoError(t, os.WriteFile(directPath, []byte(input), 0o644))
	require.NoError(t, os.WriteFile(cliPath, []byte(input), 0o644))

	executor := NewHybridBruinExecutor(workspaceRoot, bruinBinary, nil, nil)
	_, err := executor.FormatAsset(context.Background(), FormatAssetRequest{AssetPath: directPath})
	require.NoError(t, err)

	cmd := exec.Command(bruinBinary, "format", cliPath)
	cmd.Dir = workspaceRoot
	cmd.Env = append(os.Environ(), "TELEMETRY_OPTOUT=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		assert.Contains(t, string(output), "Successfully formatted asset 'analytics.customers'")
	}

	directBytes, err := os.ReadFile(directPath)
	require.NoError(t, err)
	cliBytes, err := os.ReadFile(cliPath)
	require.NoError(t, err)
	assert.Equal(t, string(cliBytes), string(directBytes))
}

func TestDirectQueryConnectionMatchesExpectedEnvelope(t *testing.T) {
	t.Parallel()

	querier := &stubSchemaQuerier{}
	executor := NewHybridBruinExecutor(
		".",
		"bruin",
		func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
			return &stubConnectionManager{conn: querier}, nil
		},
		nil,
	)

	output, err := executor.QueryConnection(context.Background(), QueryConnectionRequest{
		ConnectionName: "warehouse",
		Query:          "select 1 as id, 'alice' as name",
		Output:         "json",
	})
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(output, &payload))
	assert.Equal(t, "warehouse", payload["connectionName"])
	assert.Equal(t, "select 1 as id, 'alice' as name", payload["query"])
}

func TestDirectRunAssetFullRefreshReplacesAppendTable(t *testing.T) {
	workspaceRoot, assetPath := createSuccessfulDuckDBWorkspace(t)
	configPath := filepath.Join(workspaceRoot, ".bruin.yml")
	cfg, err := loadSelectedConfig(configPath, "")
	require.NoError(t, err)
	manager, err := newConnectionManagerFromConfig(context.Background(), cfg)
	require.NoError(t, err)
	if connection, ok := manager.GetConnection("duckdb-default").(interface{ Close() }); ok {
		t.Cleanup(connection.Close)
	}

	executor := newCompatDirectExecutor(workspaceRoot, "")
	executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
		return manager, nil
	}

	_, err = executor.RunAsset(context.Background(), RunAssetRequest{AssetPath: assetPath}, nil)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: table
  strategy: append
@bruin */

select 1 as customer_id, 'seeded' as source_label
`)+"\n"), 0o644))
	_, err = executor.RunAsset(context.Background(), RunAssetRequest{AssetPath: assetPath}, nil)
	require.NoError(t, err)
	assert.Equal(t, float64(2), directDuckDBCount(t, executor))

	_, err = executor.RunAsset(context.Background(), RunAssetRequest{AssetPath: assetPath, FullRefresh: true}, nil)
	require.NoError(t, err)
	assert.Equal(t, float64(1), directDuckDBCount(t, executor))
}

func TestDirectRunAssetTimeIntervalRendersExecutionWindow(t *testing.T) {
	workspaceRoot, assetPath := createSuccessfulDuckDBWorkspace(t)
	configPath := filepath.Join(workspaceRoot, ".bruin.yml")
	cfg, err := loadSelectedConfig(configPath, "")
	require.NoError(t, err)
	manager, err := newConnectionManagerFromConfig(context.Background(), cfg)
	require.NoError(t, err)
	if connection, ok := manager.GetConnection("duckdb-default").(interface{ Close() }); ok {
		t.Cleanup(connection.Close)
	}

	executor := newCompatDirectExecutor(workspaceRoot, "")
	executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
		return manager, nil
	}

	require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: table
  strategy: create+replace
columns:
  - name: event_id
    type: BIGINT
  - name: event_at
    type: TIMESTAMP
@bruin */

select * from (values
  (1, timestamp '2024-01-01 12:00:00'),
  (2, timestamp '2024-01-02 12:00:00')
) as events(event_id, event_at)
`)+"\n"), 0o644))
	_, err = executor.RunAsset(context.Background(), RunAssetRequest{AssetPath: assetPath}, nil)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: table
  strategy: time_interval
  incremental_key: event_at
  time_granularity: timestamp
columns:
  - name: event_id
    type: BIGINT
  - name: event_at
    type: TIMESTAMP
@bruin */

select 20 as event_id, timestamp '2024-01-02 12:00:00' as event_at
`)+"\n"), 0o644))
	_, err = executor.RunAsset(context.Background(), RunAssetRequest{
		AssetPath: assetPath,
		StartDate: "2024-01-02T00:00:00Z",
		EndDate:   "2024-01-03T00:00:00Z",
	}, nil)
	require.NoError(t, err)

	output, err := executor.QueryConnection(context.Background(), QueryConnectionRequest{
		ConnectionName: "duckdb-default",
		Query:          "select event_id from analytics.customers order by event_id",
		Output:         "json",
	})
	require.NoError(t, err)
	var payload struct {
		Rows [][]any `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(output, &payload))
	assert.Equal(t, [][]any{{float64(1)}, {float64(20)}}, payload.Rows)
}

func TestDirectRunsRenderDuckDBHookTemplates(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pipeline bool
	}{
		{name: "asset"},
		{name: "pipeline", pipeline: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspaceRoot, assetPath := createSuccessfulDuckDBWorkspace(t)
			configPath := filepath.Join(workspaceRoot, ".bruin.yml")
			cfg, err := loadSelectedConfig(configPath, "")
			require.NoError(t, err)
			manager, err := newConnectionManagerFromConfig(context.Background(), cfg)
			require.NoError(t, err)
			if connection, ok := manager.GetConnection("duckdb-default").(interface{ Close() }); ok {
				t.Cleanup(connection.Close)
			}

			executor := newCompatDirectExecutor(workspaceRoot, "")
			executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
				return manager, nil
			}

			require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: table
hooks:
  pre:
    - query: "CREATE TABLE IF NOT EXISTS hook_audit (rendered_start VARCHAR); INSERT INTO hook_audit VALUES ('{{ start_date }}')"
@bruin */

select 1 as customer_id, 'seeded' as source_label
`)+"\n"), 0o644))

			if tc.pipeline {
				_, err = executor.RunPipeline(context.Background(), RunPipelineRequest{
					Target:    "analytics",
					StartDate: "2026-07-15T00:00:00Z",
					EndDate:   "2026-07-16T00:00:00Z",
				}, nil)
			} else {
				_, err = executor.RunAsset(context.Background(), RunAssetRequest{
					AssetPath: assetPath,
					StartDate: "2026-07-15T00:00:00Z",
					EndDate:   "2026-07-16T00:00:00Z",
				}, nil)
			}
			require.NoError(t, err)

			output, err := executor.QueryConnection(context.Background(), QueryConnectionRequest{
				ConnectionName: "duckdb-default",
				Query:          "select rendered_start from hook_audit",
				Output:         "json",
			})
			require.NoError(t, err)
			var payload struct {
				Rows [][]any `json:"rows"`
			}
			require.NoError(t, json.Unmarshal(output, &payload))
			assert.Equal(t, [][]any{{"2026-07-15"}}, payload.Rows)
		})
	}
}

func directDuckDBCount(t *testing.T, executor *HybridBruinExecutor) float64 {
	t.Helper()
	output, err := executor.QueryConnection(context.Background(), QueryConnectionRequest{
		ConnectionName: "duckdb-default",
		Query:          "select count(*) as row_count from analytics.customers",
		Output:         "json",
	})
	require.NoError(t, err)
	var payload struct {
		Rows [][]any `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(output, &payload))
	require.Len(t, payload.Rows, 1)
	require.Len(t, payload.Rows[0], 1)
	count, ok := payload.Rows[0][0].(float64)
	require.True(t, ok, "unexpected count value: %#v", payload.Rows[0][0])
	return count
}

func TestDirectRunAssetFailureMatchesCLIErrorSemantics(t *testing.T) {
	t.Parallel()

	bruinBinary := compatBruinBinary(t)
	workspaceRoot, assetPath := createFailingDuckDBWorkspace(t)

	directExecutor := NewHybridBruinExecutor(
		workspaceRoot,
		bruinBinary,
		nil,
		func() *pipeline.Builder {
			osFS := afero.NewOsFs()
			return pipeline.NewBuilder(
				BuilderConfig,
				pipeline.CreateTaskFromYamlDefinition(osFS),
				pipeline.CreateTaskFromFileComments(osFS),
				osFS,
				DefaultGlossaryReader,
				jinja.VariantRendererFactory,
			)
		},
	)
	cliExecutor := NewCLIBruinExecutor(workspaceRoot, bruinBinary)

	var directChunks bytes.Buffer
	directOutput, directErr := directExecutor.RunAsset(context.Background(), RunAssetRequest{AssetPath: assetPath}, func(chunk []byte) {
		_, _ = directChunks.Write(chunk)
	})
	var cliChunks bytes.Buffer
	cliOutput, cliErr := cliExecutor.RunAsset(context.Background(), RunAssetRequest{AssetPath: assetPath}, func(chunk []byte) {
		_, _ = cliChunks.Write(chunk)
	})

	require.Error(t, directErr)
	require.Error(t, cliErr)
	assert.Equal(t, directOutput, directChunks.Bytes())
	assert.Equal(t, cliOutput, cliChunks.Bytes())

	directMessage := normalizeRunCompatibilityText(directErr.Error() + "\n" + string(directOutput))
	cliMessage := normalizeRunCompatibilityText(cliErr.Error() + "\n" + string(cliOutput))
	directCore := extractCoreRunFailure(directMessage)
	cliCore := extractCoreRunFailure(cliMessage)
	assert.Contains(t, directCore, "missing_source")
	assert.Contains(t, cliCore, "missing_source")
	assert.Contains(t, directCore, "does not exist")
	assert.Contains(t, cliCore, "does not exist")
	assert.Equal(t, cliCore, directCore)
	normalizedDirectOutput := normalizeRunCompatibilityText(string(directOutput))
	assert.Contains(t, normalizedDirectOutput, "bruin run completed with failures")
	assert.Contains(t, extractCoreRunFailure(normalizedDirectOutput), directCore)
	assert.Contains(t, normalizeRunCompatibilityText(string(cliOutput)), cliCore)
}

func TestDirectRunPipelineFailureMatchesCLIErrorSemantics(t *testing.T) {
	t.Parallel()

	bruinBinary := compatBruinBinary(t)
	workspaceRoot, _ := createFailingDuckDBWorkspace(t)
	target := filepath.Join(workspaceRoot, "analytics")

	directExecutor := NewHybridBruinExecutor(
		workspaceRoot,
		bruinBinary,
		nil,
		func() *pipeline.Builder {
			osFS := afero.NewOsFs()
			return pipeline.NewBuilder(
				BuilderConfig,
				pipeline.CreateTaskFromYamlDefinition(osFS),
				pipeline.CreateTaskFromFileComments(osFS),
				osFS,
				DefaultGlossaryReader,
				jinja.VariantRendererFactory,
			)
		},
	)
	cliExecutor := NewCLIBruinExecutor(workspaceRoot, bruinBinary)

	var directChunks bytes.Buffer
	directOutput, directErr := directExecutor.RunPipeline(context.Background(), RunPipelineRequest{Target: target}, func(chunk []byte) {
		_, _ = directChunks.Write(chunk)
	})
	var cliChunks bytes.Buffer
	cliOutput, cliErr := cliExecutor.RunPipeline(context.Background(), RunPipelineRequest{Target: target}, func(chunk []byte) {
		_, _ = cliChunks.Write(chunk)
	})

	require.Error(t, directErr)
	require.Error(t, cliErr)
	assert.Equal(t, directOutput, directChunks.Bytes())
	assert.Equal(t, cliOutput, cliChunks.Bytes())

	directMessage := normalizeRunCompatibilityText(directErr.Error() + "\n" + string(directOutput))
	cliMessage := normalizeRunCompatibilityText(cliErr.Error() + "\n" + string(cliOutput))
	directCore := extractCoreRunFailure(directMessage)
	cliCore := extractCoreRunFailure(cliMessage)
	assert.Contains(t, directCore, "missing_source")
	assert.Contains(t, cliCore, "missing_source")
	assert.Contains(t, directCore, "does not exist")
	assert.Contains(t, cliCore, "does not exist")
	assert.Equal(t, cliCore, directCore)
	normalizedDirectOutput := normalizeRunCompatibilityText(string(directOutput))
	assert.Contains(t, normalizedDirectOutput, "bruin run completed with failures")
	assert.Contains(t, extractCoreRunFailure(normalizedDirectOutput), directCore)
	assert.Contains(t, normalizeRunCompatibilityText(string(cliOutput)), cliCore)
}

func TestDirectRunAssetSuccessMatchesCLISideEffects(t *testing.T) {
	t.Parallel()

	bruinBinary := compatBruinBinary(t)
	directWorkspace, directAssetPath := createSuccessfulDuckDBWorkspace(t)
	cliWorkspace, cliAssetPath := createSuccessfulDuckDBWorkspace(t)

	directExecutor := NewHybridBruinExecutor(
		directWorkspace,
		bruinBinary,
		nil,
		func() *pipeline.Builder {
			osFS := afero.NewOsFs()
			return pipeline.NewBuilder(
				BuilderConfig,
				pipeline.CreateTaskFromYamlDefinition(osFS),
				pipeline.CreateTaskFromFileComments(osFS),
				osFS,
				DefaultGlossaryReader,
				jinja.VariantRendererFactory,
			)
		},
	)
	cliExecutor := NewCLIBruinExecutor(cliWorkspace, bruinBinary)

	var directChunks bytes.Buffer
	directOutput, directErr := directExecutor.RunAsset(context.Background(), RunAssetRequest{AssetPath: directAssetPath}, func(chunk []byte) {
		_, _ = directChunks.Write(chunk)
	})
	var cliChunks bytes.Buffer
	cliOutput, cliErr := cliExecutor.RunAsset(context.Background(), RunAssetRequest{AssetPath: cliAssetPath}, func(chunk []byte) {
		_, _ = cliChunks.Write(chunk)
	})

	require.NoError(t, directErr)
	require.NoError(t, cliErr)
	assert.Equal(t, directOutput, directChunks.Bytes())
	assert.Equal(t, cliOutput, cliChunks.Bytes())
	directText := normalizeRunCompatibilityText(string(directOutput))
	cliText := normalizeRunCompatibilityText(string(cliOutput))
	assert.Contains(t, directText, "Analyzed the pipeline 'analytics' with 1 assets.")
	assert.Contains(t, directText, "Running only the asset 'analytics.customers'")
	assert.Contains(t, directText, "Starting the pipeline execution...")
	assert.Contains(t, directText, "Running:")
	assert.Contains(t, directText, "analytics.customers")
	assert.Contains(t, directText, "Finished: analytics.customers")
	assert.Contains(t, directText, "bruin run completed successfully")
	assert.Contains(t, directText, "Assets executed")
	assert.Contains(t, cliText, "Analyzed the pipeline 'analytics' with 1 assets.")
	assert.Contains(t, cliText, "Running only the asset 'analytics.customers'")
	assertDirectRunCreatedExpectedRows(t, bruinBinary, directWorkspace)
	assertDirectRunCreatedExpectedRows(t, bruinBinary, cliWorkspace)
	assert.Equal(t, queryDuckDBResult(t, bruinBinary, cliWorkspace), queryDuckDBResult(t, bruinBinary, directWorkspace))
}

func TestDirectRunPipelineSuccessMatchesCLISideEffects(t *testing.T) {
	t.Parallel()

	bruinBinary := compatBruinBinary(t)
	directWorkspace, _ := createSuccessfulDuckDBWorkspace(t)
	cliWorkspace, _ := createSuccessfulDuckDBWorkspace(t)
	target := "analytics"

	directExecutor := NewHybridBruinExecutor(
		directWorkspace,
		bruinBinary,
		nil,
		func() *pipeline.Builder {
			osFS := afero.NewOsFs()
			return pipeline.NewBuilder(
				BuilderConfig,
				pipeline.CreateTaskFromYamlDefinition(osFS),
				pipeline.CreateTaskFromFileComments(osFS),
				osFS,
				DefaultGlossaryReader,
				jinja.VariantRendererFactory,
			)
		},
	)
	cliExecutor := NewCLIBruinExecutor(cliWorkspace, bruinBinary)

	var directChunks bytes.Buffer
	directOutput, directErr := directExecutor.RunPipeline(context.Background(), RunPipelineRequest{Target: target}, func(chunk []byte) {
		_, _ = directChunks.Write(chunk)
	})
	var cliChunks bytes.Buffer
	cliOutput, cliErr := cliExecutor.RunPipeline(context.Background(), RunPipelineRequest{Target: target}, func(chunk []byte) {
		_, _ = cliChunks.Write(chunk)
	})

	require.NoError(t, directErr)
	require.NoError(t, cliErr)
	assert.Equal(t, directOutput, directChunks.Bytes())
	assert.Equal(t, cliOutput, cliChunks.Bytes())
	directText := normalizeRunCompatibilityText(string(directOutput))
	cliText := normalizeRunCompatibilityText(string(cliOutput))
	assert.Contains(t, directText, "Analyzed the pipeline 'analytics' with 1 assets.")
	assert.Contains(t, directText, "Starting the pipeline execution...")
	assert.Contains(t, directText, "Running:")
	assert.Contains(t, directText, "analytics.customers")
	assert.Contains(t, directText, "Finished: analytics.customers")
	assert.Contains(t, directText, "bruin run completed successfully")
	assert.Contains(t, directText, "Assets executed")
	assert.Contains(t, cliText, "Analyzed the pipeline 'analytics' with 1 assets.")
	assertDirectRunCreatedExpectedRows(t, bruinBinary, directWorkspace)
	assertDirectRunCreatedExpectedRows(t, bruinBinary, cliWorkspace)
	assert.Equal(t, queryDuckDBResult(t, bruinBinary, cliWorkspace), queryDuckDBResult(t, bruinBinary, directWorkspace))
}

func TestDirectRunPythonAssetMatchesCLIOutput(t *testing.T) {
	t.Parallel()

	bruinBinary := compatBruinBinary(t)
	directWorkspace, directAssetPath := createPythonWorkspace(t)
	cliWorkspace, cliAssetPath := createPythonWorkspace(t)

	directExecutor := newCompatDirectExecutor(directWorkspace, bruinBinary)
	cliExecutor := NewCLIBruinExecutor(cliWorkspace, bruinBinary)

	directOutput, directErr := directExecutor.RunAsset(context.Background(), RunAssetRequest{AssetPath: directAssetPath}, nil)
	cliOutput, cliErr := cliExecutor.RunAsset(context.Background(), RunAssetRequest{AssetPath: cliAssetPath}, nil)

	require.NoError(t, directErr)
	require.NoError(t, cliErr)
	directText := normalizeRunCompatibilityText(string(directOutput))
	cliText := normalizeRunCompatibilityText(string(cliOutput))
	assert.Contains(t, directText, "python direct compatibility")
	assert.Contains(t, cliText, "python direct compatibility")
	assert.Contains(t, directText, "bruin run completed successfully")
	assert.Contains(t, cliText, "bruin run completed successfully")
	assert.Contains(t, directText, "analytics.python_task")
	assert.Contains(t, cliText, "analytics.python_task")
}

func TestDirectRunIngestrAssetFailureMatchesCLIErrorSemantics(t *testing.T) {
	t.Parallel()

	bruinBinary := compatBruinBinary(t)
	directWorkspace, directAssetPath := createFailingIngestrWorkspace(t)
	cliWorkspace, cliAssetPath := createFailingIngestrWorkspace(t)

	directExecutor := newCompatDirectExecutor(directWorkspace, bruinBinary)
	cliExecutor := NewCLIBruinExecutor(cliWorkspace, bruinBinary)

	directOutput, directErr := directExecutor.RunAsset(context.Background(), RunAssetRequest{AssetPath: directAssetPath}, nil)
	cliOutput, cliErr := cliExecutor.RunAsset(context.Background(), RunAssetRequest{AssetPath: cliAssetPath}, nil)

	require.Error(t, directErr)
	require.Error(t, cliErr)
	directText := normalizeRunCompatibilityText(directErr.Error() + "\n" + string(directOutput))
	cliText := normalizeRunCompatibilityText(cliErr.Error() + "\n" + string(cliOutput))
	directCore := extractCoreRunFailure(directText)
	cliCore := extractCoreRunFailure(cliText)
	for _, text := range []string{directCore, cliCore} {
		assert.Contains(t, text, "source connection 'missing-source' not found")
		assert.Contains(t, text, "analytics.ingest")
		assert.Contains(t, text, "bruin run completed with failures")
	}
}

func compatBruinBinary(t *testing.T) string {
	t.Helper()

	bruinBinary := strings.TrimSpace(os.Getenv("BRUIN_COMPAT_BINARY"))
	if _, err := os.Stat(bruinBinary); err != nil {
		t.Skip("compatibility binary not available")
	}

	return bruinBinary
}

func newCompatDirectExecutor(workspaceRoot, bruinBinary string) *HybridBruinExecutor {
	return NewHybridBruinExecutor(
		workspaceRoot,
		bruinBinary,
		nil,
		func() *pipeline.Builder {
			osFS := afero.NewOsFs()
			return pipeline.NewBuilder(
				BuilderConfig,
				pipeline.CreateTaskFromYamlDefinition(osFS),
				pipeline.CreateTaskFromFileComments(osFS),
				osFS,
				DefaultGlossaryReader,
				jinja.VariantRendererFactory,
			)
		},
	)
}

func createFailingDuckDBWorkspace(t *testing.T) (string, string) {
	t.Helper()

	workspaceRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, "duckdb-files"), 0o755))
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".bruin.yml"), []byte(strings.TrimSpace(`
default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: duckdb-default
          path: duckdb-files/local.db
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte(strings.TrimSpace(`
name: analytics
default_connections:
  duckdb: duckdb-default
`)+"\n"), 0o644))
	assetPath := filepath.Join(assetsRoot, "customers.sql")
	require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: table
@bruin */

select * from missing_source
`)+"\n"), 0o644))

	return workspaceRoot, assetPath
}

func createPythonWorkspace(t *testing.T) (string, string) {
	t.Helper()

	workspaceRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".bruin.yml"), []byte(strings.TrimSpace(`
default_environment: default
environments:
  default:
    connections: {}
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	assetPath := filepath.Join(assetsRoot, "python_task.py")
	require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
""" @bruin
name: analytics.python_task
type: python
@bruin """

print("python direct compatibility")
`)+"\n"), 0o644))
	return workspaceRoot, assetPath
}

func createFailingIngestrWorkspace(t *testing.T) (string, string) {
	t.Helper()

	workspaceRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".bruin.yml"), []byte(strings.TrimSpace(`
default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: duckdb-default
          path: local.db
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte(strings.TrimSpace(`
name: analytics
default_connections:
  duckdb: duckdb-default
`)+"\n"), 0o644))
	assetPath := filepath.Join(assetsRoot, "ingest.asset.yml")
	require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
name: analytics.ingest
type: ingestr
parameters:
  source_connection: missing-source
  source: duckdb
  destination: duckdb
  source_table: source_table
`)+"\n"), 0o644))
	return workspaceRoot, assetPath
}

func createSuccessfulDuckDBWorkspace(t *testing.T) (string, string) {
	t.Helper()

	workspaceRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, "duckdb-files"), 0o755))
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".bruin.yml"), []byte(strings.TrimSpace(`
default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: duckdb-default
          path: duckdb-files/local.db
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte(strings.TrimSpace(`
name: analytics
default_connections:
  duckdb: duckdb-default
`)+"\n"), 0o644))
	assetPath := filepath.Join(assetsRoot, "customers.sql")
	require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: table
@bruin */

select 1 as customer_id, 'seeded' as source_label
`)+"\n"), 0o644))

	return workspaceRoot, assetPath
}

func assertDirectRunCreatedExpectedRows(t *testing.T, bruinBinary, workspaceRoot string) {
	t.Helper()

	result := queryDuckDBResult(t, bruinBinary, workspaceRoot)
	assert.Len(t, result, 1)
	assert.Len(t, result[0], 2)
	assert.Equal(t, float64(1), result[0][0])
}

func queryDuckDBResult(t *testing.T, bruinBinary, workspaceRoot string) [][]any {
	t.Helper()

	cmd := exec.Command(bruinBinary, "query", "--connection", "duckdb-default", "--query", "select customer_id, source_label from analytics.customers", "--output", "json")
	cmd.Dir = workspaceRoot
	cmd.Env = append(os.Environ(), "TELEMETRY_OPTOUT=1")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	var payload struct {
		Rows [][]any `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(output, &payload), string(output))
	return payload.Rows
}

func normalizeRunCompatibilityText(text string) string {
	text = ansiEscapePattern.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return strings.Join(normalized, "\n")
}

func extractCoreRunFailure(text string) string {
	normalizeCore := func(core string) string {
		lines := strings.Split(strings.TrimSpace(core), "\n")
		cleaned := make([]string, 0, len(lines))
		for _, line := range lines {
			trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "└── "))
			if trimmed != "" {
				cleaned = append(cleaned, trimmed)
			}
		}
		return strings.Join(cleaned, "\n")
	}

	if idx := strings.LastIndex(text, "Internal:"); idx >= 0 {
		return normalizeCore(text[idx:])
	}
	if idx := strings.LastIndex(text, "Error:"); idx >= 0 {
		return normalizeCore(text[idx:])
	}
	return normalizeCore(text)
}
