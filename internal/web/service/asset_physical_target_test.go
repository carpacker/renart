package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssetPhysicalTargetResolvesSupportedRelationFamilies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		assetType   pipeline.AssetType
		assetName   string
		connections func(string) *config.Connections
	}{
		{"duckdb", pipeline.AssetTypeDuckDBQuery, "analytics.customers", func(root string) *config.Connections {
			return &config.Connections{DuckDB: []config.DuckDBConnection{{ConnectionMetadata: targetMetadata("warehouse"), Path: filepath.Join(root, "warehouse.duckdb")}}}
		}},
		{"postgres", pipeline.AssetTypePostgresQuery, "customers", func(string) *config.Connections {
			return &config.Connections{Postgres: []config.PostgresConnection{{ConnectionMetadata: targetMetadata("warehouse"), Host: "pg.internal", Port: 5432, Database: "analytics", Schema: "public"}}}
		}},
		{"redshift", pipeline.AssetTypeRedshiftQuery, "customers", func(string) *config.Connections {
			return &config.Connections{RedShift: []config.RedshiftConnection{{ConnectionMetadata: targetMetadata("warehouse"), Host: "rs.internal", Port: 5439, Database: "analytics", Schema: "public"}}}
		}},
		{"mysql", pipeline.AssetTypeMySQLQuery, "customers", func(string) *config.Connections {
			return &config.Connections{MySQL: []config.MySQLConnection{{ConnectionMetadata: targetMetadata("warehouse"), Host: "mysql.internal", Port: 3306, Database: "analytics"}}}
		}},
		{"mssql", pipeline.AssetTypeMsSQLQuery, "dbo.customers", func(string) *config.Connections {
			return &config.Connections{MsSQL: []config.MsSQLConnection{{ConnectionMetadata: targetMetadata("warehouse"), Host: "mssql.internal", Port: 1433, Database: "analytics"}}}
		}},
		{"fabric", pipeline.AssetTypeFabricQuery, "dbo.customers", func(string) *config.Connections {
			return &config.Connections{Fabric: []config.FabricConnection{{ConnectionMetadata: targetMetadata("warehouse"), Host: "fabric.internal", Port: 1433, Database: "analytics"}}}
		}},
		{"synapse", pipeline.AssetTypeSynapseQuery, "dbo.customers", func(string) *config.Connections {
			return &config.Connections{Synapse: []config.SynapseConnection{{ConnectionMetadata: targetMetadata("warehouse"), Host: "synapse.internal", Port: 1433, Database: "analytics"}}}
		}},
		{"vertica", pipeline.AssetTypeVerticaQuery, "customers", func(string) *config.Connections {
			return &config.Connections{Vertica: []config.VerticaConnection{{ConnectionMetadata: targetMetadata("warehouse"), Host: "vertica.internal", Port: 5433, Database: "analytics", Schema: "public"}}}
		}},
		{"clickhouse", pipeline.AssetTypeClickHouse, "customers", func(string) *config.Connections {
			return &config.Connections{ClickHouse: []config.ClickHouseConnection{{ConnectionMetadata: targetMetadata("warehouse"), Host: "clickhouse.internal", Port: 9000, Database: "analytics"}}}
		}},
		{"trino", pipeline.AssetTypeTrinoQuery, "customers", func(string) *config.Connections {
			return &config.Connections{Trino: []config.TrinoConnection{{ConnectionMetadata: targetMetadata("warehouse"), Host: "trino.internal", Port: 8080, Catalog: "lakehouse", Schema: "analytics"}}}
		}},
		{"databricks", pipeline.AssetTypeDatabricksQuery, "analytics.customers", func(string) *config.Connections {
			return &config.Connections{Databricks: []config.DatabricksConnection{{ConnectionMetadata: targetMetadata("warehouse"), Host: "dbc.internal", Port: 443, Path: "sql/1.0/warehouses/abc", Catalog: "main"}}}
		}},
		{"snowflake", pipeline.AssetTypeSnowflakeQuery, "customers", func(string) *config.Connections {
			return &config.Connections{Snowflake: []config.SnowflakeConnection{{ConnectionMetadata: targetMetadata("warehouse"), Account: "account", Region: "eu-central-1", Database: "analytics", Schema: "public"}}}
		}},
		{"bigquery", pipeline.AssetTypeBigqueryQuery, "dataset.customers", func(string) *config.Connections {
			return &config.Connections{GoogleCloudPlatform: []config.GoogleCloudPlatformConnection{{ConnectionMetadata: targetMetadata("warehouse"), ProjectID: "analytics-project"}}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			asset := materializedTargetAsset(tt.assetType, tt.assetName, "warehouse")
			target := resolveAssetPhysicalTarget(root, targetInfo(asset, tt.connections(root), ""))

			require.Equal(t, AssetRenderFidelityExact, target.Fidelity, target.Message)
			assert.Equal(t, assetRenderTargetKindRelation, target.Kind)
			assert.NotEmpty(t, target.Identity)
			assert.NotEmpty(t, target.Object)
			assert.Empty(t, target.Message)
			assert.NotContains(t, target.Object, "internal")
			assert.NotContains(t, target.Object, root)
		})
	}
}

func TestAssetPhysicalTargetIdentityIgnoresAliasesPrincipalsAndCredentials(t *testing.T) {
	t.Parallel()

	leftAsset := materializedTargetAsset(pipeline.AssetTypePostgresQuery, "customers", "primary")
	rightAsset := materializedTargetAsset(pipeline.AssetTypePostgresQuery, "customers", "renamed")
	left := resolveAssetPhysicalTarget(t.TempDir(), targetInfo(leftAsset, &config.Connections{Postgres: []config.PostgresConnection{{
		ConnectionMetadata: targetMetadata("primary"), Host: "PG.INTERNAL", Port: 5432, Database: "analytics", Schema: "public", Username: "first", Password: "first-secret",
	}}}, ""))
	right := resolveAssetPhysicalTarget(t.TempDir(), targetInfo(rightAsset, &config.Connections{Postgres: []config.PostgresConnection{{
		ConnectionMetadata: targetMetadata("renamed"), Host: "pg.internal.", Port: 5432, Database: "analytics", Schema: "public", Username: "second", Password: "second-secret",
	}}}, ""))

	require.Equal(t, AssetRenderFidelityExact, left.Fidelity, left.Message)
	require.Equal(t, AssetRenderFidelityExact, right.Fidelity, right.Message)
	assert.Equal(t, left.Identity, right.Identity)
	assert.NotContains(t, left.Message, "secret")
}

func TestResolvePipelinePhysicalTargetsUsesSelectedConfigurationWithoutWriting(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	configPath := filepath.Join(root, ".bruin.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
default_environment: dev
environments:
  dev:
    connections:
      duckdb:
        - name: warehouse
          path: dev.duckdb
  prod:
    connections:
      duckdb:
        - name: warehouse
          path: prod.duckdb
`), 0o644))
	asset := materializedTargetAsset(pipeline.AssetTypeDuckDBQuery, "analytics.customers", "warehouse")
	pl := &pipeline.Pipeline{LegacyID: "pipeline-uuid", Assets: []*pipeline.Asset{asset}}

	dev, err := ResolvePipelinePhysicalTargets(root, configPath, "dev", pl)
	require.NoError(t, err)
	prod, err := ResolvePipelinePhysicalTargets(root, configPath, "prod", pl)
	require.NoError(t, err)
	assetID := "pipeline-uuid:analytics.customers"
	require.Contains(t, dev, assetID)
	require.Contains(t, prod, assetID)
	assert.Equal(t, AssetRenderFidelityExact, dev[assetID].Fidelity)
	assert.Equal(t, AssetRenderFidelityExact, prod[assetID].Fidelity)
	assert.NotEmpty(t, dev[assetID].Identity)
	assert.NotEqual(t, dev[assetID].Identity, prod[assetID].Identity)
	assert.FileExists(t, configPath)
	assert.NoFileExists(t, filepath.Join(root, ".gitignore"), "read-only target resolution must not create project files")
}

func TestAssetPhysicalTargetCanonicalizesDuckDBAndLocalFilePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	require.NoError(t, os.MkdirAll(realDir, 0o755))
	require.NoError(t, os.Symlink(realDir, filepath.Join(root, "linked")))

	duckAsset := materializedTargetAsset(pipeline.AssetTypeDuckDBQuery, "analytics.customers", "warehouse")
	physical := resolveAssetPhysicalTarget(root, targetInfo(duckAsset, &config.Connections{DuckDB: []config.DuckDBConnection{{
		ConnectionMetadata: targetMetadata("warehouse"), Path: filepath.Join("real", "warehouse.duckdb"),
	}}}, ""))
	linked := resolveAssetPhysicalTarget(root, targetInfo(duckAsset, &config.Connections{DuckDB: []config.DuckDBConnection{{
		ConnectionMetadata: targetMetadata("warehouse"), Path: filepath.Join("linked", "warehouse.duckdb") + "?access_mode=read_write",
	}}}, ""))
	require.Equal(t, AssetRenderFidelityExact, physical.Fidelity, physical.Message)
	require.Equal(t, AssetRenderFidelityExact, linked.Fidelity, linked.Message)
	assert.Equal(t, physical.Identity, linked.Identity)
	assert.NotContains(t, physical.Object, root)

	load := materializedTargetAsset(pipeline.AssetType(loadAssetType), "analytics.export", loadLocalConnectionName)
	load.Parameters = pipeline.ParameterMap{loadParamDestinationObject: filepath.Join("linked", "customers.parquet")}
	local := resolveAssetPhysicalTarget(root, targetInfo(load, &config.Connections{}, ""))
	require.Equal(t, AssetRenderFidelityExact, local.Fidelity, local.Message)
	assert.Equal(t, assetRenderTargetKindFile, local.Kind)
	assert.Equal(t, "real/customers.parquet", local.Object)
	assert.NotContains(t, local.Object, root)
}

func TestAssetPhysicalTargetTreatsKnownZeroMaterializationAssetsAsWriters(t *testing.T) {
	t.Parallel()

	connections := func() *config.Connections {
		return &config.Connections{Postgres: []config.PostgresConnection{{
			ConnectionMetadata: targetMetadata("warehouse"), Host: "pg.internal", Port: 5432, Database: "analytics", Schema: "public",
		}}}
	}
	assets := []*pipeline.Asset{
		{Name: "public.seeded", Type: pipeline.AssetTypePostgresSeed, Connection: "warehouse"},
		{Name: "public.loaded", Type: pipeline.AssetType(loadAssetType), Connection: "warehouse"},
		{Name: "public.fetched", Type: pipeline.AssetType(apiAssetType), Connection: "warehouse"},
		{Name: "public.ingested", Type: pipeline.AssetTypeIngestr, Connection: "warehouse"},
	}
	for _, asset := range assets {
		target := resolveAssetPhysicalTarget(t.TempDir(), targetInfo(asset, connections(), ""))
		require.Equal(t, AssetRenderFidelityExact, target.Fidelity, "%s: %s", asset.Type, target.Message)
		assert.Equal(t, assetRenderTargetKindRelation, target.Kind)
		assert.NotEmpty(t, target.Identity)
	}
}

func TestAssetPhysicalTargetUsesRuntimePortDefaultsAndPreservesRelationSpelling(t *testing.T) {
	t.Parallel()

	resolveMySQL := func(port int, name string) AssetRenderTarget {
		asset := materializedTargetAsset(pipeline.AssetTypeMySQLQuery, name, "warehouse")
		return resolveAssetPhysicalTarget(t.TempDir(), targetInfo(asset, &config.Connections{MySQL: []config.MySQLConnection{{
			ConnectionMetadata: targetMetadata("warehouse"), Host: "mysql.internal", Port: port, Database: "analytics",
		}}}, ""))
	}
	implicit := resolveMySQL(0, "Sales.Customers")
	explicit := resolveMySQL(3306, "Sales.Customers")
	lowercase := resolveMySQL(3306, "sales.customers")
	require.Equal(t, AssetRenderFidelityExact, implicit.Fidelity, implicit.Message)
	require.Equal(t, AssetRenderFidelityExact, explicit.Fidelity, explicit.Message)
	assert.Equal(t, implicit.Identity, explicit.Identity)
	assert.NotEqual(t, explicit.Identity, lowercase.Identity)
	assert.Equal(t, "Sales.Customers", explicit.Object)

	resolveFabric := func(port int) AssetRenderTarget {
		asset := materializedTargetAsset(pipeline.AssetTypeFabricQuery, "analytics.dbo.customers", "warehouse")
		return resolveAssetPhysicalTarget(t.TempDir(), targetInfo(asset, &config.Connections{Fabric: []config.FabricConnection{{
			ConnectionMetadata: targetMetadata("warehouse"), Host: "fabric.internal", Port: port, Database: "analytics",
		}}}, ""))
	}
	assert.Equal(t, resolveFabric(0).Identity, resolveFabric(1433).Identity)
}

func TestAssetPhysicalTargetPreHooksRequireAnExplicitRelation(t *testing.T) {
	t.Parallel()

	connections := func() *config.Connections {
		return &config.Connections{Postgres: []config.PostgresConnection{{
			ConnectionMetadata: targetMetadata("warehouse"), Host: "pg.internal", Port: 5432, Database: "analytics", Schema: "public",
		}}}
	}
	unqualified := materializedTargetAsset(pipeline.AssetTypePostgresQuery, "customers", "warehouse")
	unqualified.Hooks.Pre = []pipeline.Hook{{Query: "set search_path to other"}}
	qualified := materializedTargetAsset(pipeline.AssetTypePostgresQuery, "public.customers", "warehouse")
	qualified.Hooks.Pre = []pipeline.Hook{{Query: "set search_path to other"}}

	unsafeTarget := resolveAssetPhysicalTarget(t.TempDir(), targetInfo(unqualified, connections(), ""))
	safeTarget := resolveAssetPhysicalTarget(t.TempDir(), targetInfo(qualified, connections(), ""))
	assert.Equal(t, AssetRenderFidelityRuntimeOnly, unsafeTarget.Fidelity)
	assert.Empty(t, unsafeTarget.Identity)
	require.Equal(t, AssetRenderFidelityExact, safeTarget.Fidelity, safeTarget.Message)
	assert.NotEmpty(t, safeTarget.Identity)
}

func TestAssetPhysicalTargetDDLStillTargetsTheDeclaredAssetRelation(t *testing.T) {
	t.Parallel()

	asset := materializedTargetAsset(pipeline.AssetTypePostgresQuery, "public.customers", "warehouse")
	asset.Materialization.Strategy = pipeline.MaterializationStrategyDDL
	target := resolveAssetPhysicalTarget(t.TempDir(), targetInfo(asset, &config.Connections{Postgres: []config.PostgresConnection{{
		ConnectionMetadata: targetMetadata("warehouse"), Host: "pg.internal", Port: 5432, Database: "analytics", Schema: "public",
	}}}, ""))

	require.Equal(t, AssetRenderFidelityExact, target.Fidelity, target.Message)
	assert.Equal(t, "analytics.public.customers", target.Object)
	assert.NotEmpty(t, target.Identity)
}

func TestSafeFileObjectDoesNotExposeAbsoluteOrRemoteLocations(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "customers.parquet", safeFileObject("/home/alice/private/customers.parquet"))
	assert.Equal(t, "customers.parquet", safeFileObject("s3://private-bucket/team/customers.parquet?token=secret"))
	assert.Equal(t, "exports/customers.parquet", safeFileObject("exports/customers.parquet"))
}

func TestAssetPhysicalTargetFailsClosedWithoutAProvenTarget(t *testing.T) {
	t.Parallel()

	t.Run("sensor has exact no-output identity", func(t *testing.T) {
		asset := &pipeline.Asset{Type: pipeline.AssetTypePostgresQuerySensor}
		target := resolveAssetPhysicalTarget(t.TempDir(), &directPipelineInfo{Asset: asset, Pipeline: &pipeline.Pipeline{}})
		require.Equal(t, AssetRenderFidelityExact, target.Fidelity, target.Message)
		assert.Equal(t, assetRenderTargetKindNone, target.Kind)
		assert.Empty(t, target.Identity)
	})

	t.Run("seed is an implicit writer", func(t *testing.T) {
		asset := &pipeline.Asset{Name: "analytics.customers", Type: pipeline.AssetTypeDuckDBSeed, Connection: "warehouse"}
		target := resolveAssetPhysicalTarget(t.TempDir(), targetInfo(asset, &config.Connections{DuckDB: []config.DuckDBConnection{{
			ConnectionMetadata: targetMetadata("warehouse"), Path: "warehouse.duckdb",
		}}}, ""))
		require.Equal(t, AssetRenderFidelityExact, target.Fidelity, target.Message)
		assert.Equal(t, assetRenderTargetKindRelation, target.Kind)
		assert.NotEmpty(t, target.Identity)
	})

	tests := []struct {
		name   string
		asset  *pipeline.Asset
		config *config.Connections
		prefix string
		secret string
	}{
		{"materialization none", &pipeline.Asset{Name: "analytics.query", Type: pipeline.AssetTypePostgresQuery, Connection: "warehouse"}, &config.Connections{}, "", ""},
		{"python", materializedTargetAsset(pipeline.AssetTypePython, "analytics.dynamic", "warehouse"), &config.Connections{}, "", ""},
		{"schema prefix", materializedTargetAsset(pipeline.AssetTypePostgresQuery, "public.customers", "warehouse"), &config.Connections{Postgres: []config.PostgresConnection{{ConnectionMetadata: targetMetadata("warehouse"), Host: "pg.internal", Port: 5432, Database: "analytics", Schema: "public"}}}, "dev_", ""},
		{"duckdb memory", materializedTargetAsset(pipeline.AssetTypeDuckDBQuery, "analytics.customers", "warehouse"), &config.Connections{DuckDB: []config.DuckDBConnection{{ConnectionMetadata: targetMetadata("warehouse"), Path: ":memory:"}}}, "", ""},
		{"raw tds options", materializedTargetAsset(pipeline.AssetTypeMsSQLQuery, "dbo.customers", "warehouse"), &config.Connections{MsSQL: []config.MsSQLConnection{{ConnectionMetadata: targetMetadata("warehouse"), Host: "sql.internal", Port: 1433, Database: "analytics", Options: "password=do-not-leak"}}}, "", "do-not-leak"},
		{"credential-derived bigquery project", materializedTargetAsset(pipeline.AssetTypeBigqueryQuery, "dataset.customers", "warehouse"), &config.Connections{GoogleCloudPlatform: []config.GoogleCloudPlatformConnection{{ConnectionMetadata: targetMetadata("warehouse"), ServiceAccountJSON: "secret-json"}}}, "", "secret-json"},
		{"unsupported family", materializedTargetAsset(pipeline.AssetTypeOracleQuery, "analytics.customers", "warehouse"), &config.Connections{Oracle: []config.OracleConnection{{ConnectionMetadata: targetMetadata("warehouse")}}}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := resolveAssetPhysicalTarget(t.TempDir(), targetInfo(tt.asset, tt.config, tt.prefix))
			assert.Equal(t, AssetRenderFidelityRuntimeOnly, target.Fidelity)
			assert.Empty(t, target.Identity)
			assert.NotEmpty(t, target.Message)
			if tt.name == "materialization none" {
				assert.Equal(t, assetRenderTargetKindUnknown, target.Kind)
			}
			if tt.secret != "" {
				assert.NotContains(t, target.Message, tt.secret)
				assert.NotContains(t, target.Object, tt.secret)
			}
		})
	}
}

func targetMetadata(name string) config.ConnectionMetadata {
	return config.ConnectionMetadata{Name: name}
}

func materializedTargetAsset(assetType pipeline.AssetType, name, connection string) *pipeline.Asset {
	return &pipeline.Asset{
		Name:       name,
		Type:       assetType,
		Connection: connection,
		Materialization: pipeline.Materialization{
			Type:     pipeline.MaterializationTypeTable,
			Strategy: pipeline.MaterializationStrategyCreateReplace,
		},
	}
}

func targetInfo(asset *pipeline.Asset, connections *config.Connections, schemaPrefix string) *directPipelineInfo {
	return &directPipelineInfo{
		Asset:    asset,
		Pipeline: &pipeline.Pipeline{Assets: []*pipeline.Asset{asset}},
		Config: &config.Config{SelectedEnvironment: &config.Environment{
			Connections:  connections,
			SchemaPrefix: schemaPrefix,
		}},
	}
}
