package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func newSemanticAssetTestService(t *testing.T, connectionTypeFor func(string) string) (*AssetService, string) {
	t.Helper()
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineRoot, "assets", "analytics"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\ndefault_connections:\n  duckdb: duckdb-default\n"), 0o644))
	resolver := newAssetTestResolver(workspaceRoot)
	return NewAssetService(AssetDependencies{
		WorkspaceRoot:                              workspaceRoot,
		ResolveAssetByID:                           resolver.ResolveAssetByID,
		DefaultAssetContent:                        DefaultAssetContent,
		DerivedAssetContent:                        DefaultDerivedSQLAssetContent,
		EnsurePythonProject:                        func(string, string, string) error { return nil },
		SuppressWatcher:                            func(string) {},
		PushWorkspaceUpdateImmediate:               func(context.Context, string, string) {},
		PushWorkspaceUpdateImmediateWithChangedIDs: func(context.Context, string, string, []string) {},
		ConnectionTypeFor:                          connectionTypeFor,
	}), pipelineRoot
}

func TestAssetServiceCreateBinarySeedAndDeleteOwnedUpload(t *testing.T) {
	t.Parallel()
	service, pipelineRoot := newSemanticAssetTestService(t, nil)
	upload := []byte{0x50, 0x41, 0x52, 0x31, 0x00, 0xff}

	response, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name:          "analytics.regional_customers",
		Type:          "duckdb.seed",
		Connection:    "duckdb-default",
		Parameters:    map[string]string{"enforce_schema": "false"},
		SeedFileName:  "regional_customers.parquet",
		SeedFileBytes: upload,
	})
	require.Nil(t, apiErr)
	assert.Equal(t, "analytics/assets/analytics/regional_customers.asset.yml", response.AssetPath)

	definitionPath := filepath.Join(pipelineRoot, "assets", "analytics", "regional_customers.asset.yml")
	sidecarPath := filepath.Join(pipelineRoot, "assets", "analytics", "regional_customers.parquet")
	actualUpload, err := os.ReadFile(sidecarPath)
	require.NoError(t, err)
	assert.Equal(t, upload, actualUpload)

	definitionBytes, err := os.ReadFile(definitionPath)
	require.NoError(t, err)
	var definition semanticAssetDefinition
	require.NoError(t, yaml.Unmarshal(definitionBytes, &definition))
	assert.Equal(t, "./regional_customers.parquet", definition.Parameters.Path)
	assert.Equal(t, "parquet", definition.Parameters.FileType)
	require.NotNil(t, definition.Parameters.EnforceSchema)
	assert.False(t, *definition.Parameters.EnforceSchema)
	assert.Equal(t, "regional_customers.parquet", definition.Meta[renartOwnedSeedFileMetaKey])

	deleteResponse, deleteErr := service.Delete(context.Background(), response.AssetID)
	require.Nil(t, deleteErr)
	assert.Equal(t, "ok", deleteResponse.Status)
	assert.NoFileExists(t, definitionPath)
	assert.NoFileExists(t, sidecarPath)
}

func TestAssetServiceDeleteSeedPreservesReferencedWorkspaceFile(t *testing.T) {
	t.Parallel()
	service, pipelineRoot := newSemanticAssetTestService(t, nil)
	externalPath := filepath.Join(pipelineRoot, "assets", "analytics", "shared.csv")
	require.NoError(t, os.WriteFile(externalPath, []byte("id\n1\n"), 0o644))

	response, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name:       "analytics.shared_seed",
		Type:       "duckdb.seed",
		Connection: "duckdb-default",
		Parameters: map[string]string{"path": "./shared.csv"},
	})
	require.Nil(t, apiErr)

	_, deleteErr := service.Delete(context.Background(), response.AssetID)
	require.Nil(t, deleteErr)
	assert.FileExists(t, externalPath)
}

func TestAssetServiceCreateSeedConvertsWorkspacePickerPath(t *testing.T) {
	t.Parallel()
	service, pipelineRoot := newSemanticAssetTestService(t, nil)
	workspaceFile := filepath.Join(filepath.Dir(pipelineRoot), "data", "customers.csv")
	require.NoError(t, os.MkdirAll(filepath.Dir(workspaceFile), 0o755))
	require.NoError(t, os.WriteFile(workspaceFile, []byte("id\n1\n"), 0o644))

	response, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name:       "analytics.customers",
		Type:       "duckdb.seed",
		Connection: "duckdb-default",
		Parameters: map[string]string{seedWorkspacePathParameter: "./data/customers.csv"},
	})
	require.Nil(t, apiErr)

	definitionBytes, err := os.ReadFile(filepath.Join(pipelineRoot, "assets", "analytics", "customers.asset.yml"))
	require.NoError(t, err)
	var definition semanticAssetDefinition
	require.NoError(t, yaml.Unmarshal(definitionBytes, &definition))
	assert.Equal(t, "../../../data/customers.csv", definition.Parameters.Path)
	assert.NotContains(t, string(definitionBytes), seedWorkspacePathParameter)
	assert.Equal(t, "analytics/assets/analytics/customers.asset.yml", response.AssetPath)
}

func TestAssetServiceCreateSeedRejectsWorkspacePickerTraversal(t *testing.T) {
	t.Parallel()
	service, _ := newSemanticAssetTestService(t, nil)

	_, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name: "analytics.customers",
		Type: "duckdb.seed",
		Parameters: map[string]string{
			seedWorkspacePathParameter: "../customers.csv",
		},
	})
	require.NotNil(t, apiErr)
	assert.Equal(t, "invalid_seed_workspace_path", apiErr.Code)
}

func TestAssetServiceCreateSensorRendersTypedCanonicalDefinition(t *testing.T) {
	t.Parallel()
	service, pipelineRoot := newSemanticAssetTestService(t, nil)

	response, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name:       "analytics.orders_ready",
		Type:       "duckdb.sensor.query",
		Connection: "duckdb-default",
		Parameters: map[string]string{
			"query":         "select count(*) > 0 from analytics.orders",
			"poke_interval": "15",
			"timeout":       "2h",
		},
	})
	require.Nil(t, apiErr)

	definitionBytes, err := os.ReadFile(filepath.Join(pipelineRoot, "assets", "analytics", "orders_ready.asset.yml"))
	require.NoError(t, err)
	var definition semanticAssetDefinition
	require.NoError(t, yaml.Unmarshal(definitionBytes, &definition))
	assert.Equal(t, "analytics.orders_ready", definition.Name)
	assert.Equal(t, "duckdb.sensor.query", definition.Type)
	assert.Equal(t, "select count(*) > 0 from analytics.orders", definition.Parameters.Query)
	require.NotNil(t, definition.Parameters.PokeInterval)
	assert.Equal(t, 15, *definition.Parameters.PokeInterval)
	assert.Equal(t, "2h", definition.Parameters.Timeout)
	assert.Empty(t, definition.Parameters.Path)
	assert.Empty(t, definition.Meta)
	assert.Equal(t, "analytics/assets/analytics/orders_ready.asset.yml", response.AssetPath)
}

func TestAssetServiceCreateSensorRejectsMissingRequiredParameter(t *testing.T) {
	t.Parallel()
	service, _ := newSemanticAssetTestService(t, nil)

	_, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name: "analytics.orders_ready",
		Type: "duckdb.sensor.query",
	})
	require.NotNil(t, apiErr)
	assert.Equal(t, "missing_sensor_parameter", apiErr.Code)
}

func TestAssetServiceUpdateSensorParametersValidatesAndPersists(t *testing.T) {
	t.Parallel()
	service, pipelineRoot := newSemanticAssetTestService(t, nil)
	response, createErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name: "analytics.orders_ready",
		Type: "duckdb.sensor.query",
		Parameters: map[string]string{
			"query": "select true",
		},
	})
	require.Nil(t, createErr)

	_, updateErr := service.Update(context.Background(), response.AssetID, AssetUpdateRequest{
		Parameters: map[string]string{
			"query":         "select count(*) > 0 from analytics.orders",
			"poke_interval": "10",
			"timeout":       "30m",
		},
	})
	require.Nil(t, updateErr)
	definitionPath := filepath.Join(pipelineRoot, "assets", "analytics", "orders_ready.asset.yml")
	updated, err := os.ReadFile(definitionPath)
	require.NoError(t, err)
	assert.Contains(t, string(updated), "select count(*) > 0 from analytics.orders")
	_, _, updatedAsset, err := service.deps.ResolveAssetByID(context.Background(), response.AssetID)
	require.NoError(t, err)
	assert.Equal(t, "10", updatedAsset.Parameters["poke_interval"])
	assert.Equal(t, "30m", updatedAsset.Parameters["timeout"])

	beforeInvalid := append([]byte(nil), updated...)
	_, invalidErr := service.Update(context.Background(), response.AssetID, AssetUpdateRequest{
		Parameters: map[string]string{
			"query":         "select true",
			"poke_interval": "0",
			"timeout":       "30m",
		},
	})
	require.NotNil(t, invalidErr)
	assert.Equal(t, "invalid_poke_interval", invalidErr.Code)
	afterInvalid, err := os.ReadFile(definitionPath)
	require.NoError(t, err)
	assert.Equal(t, beforeInvalid, afterInvalid)
}

func TestAssetServiceCreateSeedDoesNotOverwriteSidecar(t *testing.T) {
	t.Parallel()
	service, pipelineRoot := newSemanticAssetTestService(t, nil)
	sidecarPath := filepath.Join(pipelineRoot, "assets", "analytics", "customers.csv")
	require.NoError(t, os.WriteFile(sidecarPath, []byte("keep me"), 0o644))

	_, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name:          "analytics.customers",
		Type:          "duckdb.seed",
		SeedFileName:  "customers.csv",
		SeedFileBytes: []byte("replace me"),
	})
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Status)
	assert.Equal(t, "asset_path_exists", apiErr.Code)
	assert.NoFileExists(t, filepath.Join(pipelineRoot, "assets", "analytics", "customers.asset.yml"))
	contents, err := os.ReadFile(sidecarPath)
	require.NoError(t, err)
	assert.Equal(t, "keep me", string(contents))
}

func TestAssetServiceCreateSemanticAssetRejectsIncompatibleConnection(t *testing.T) {
	t.Parallel()
	service, _ := newSemanticAssetTestService(t, func(string) string { return "postgres" })

	_, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name:          "analytics.customers",
		Type:          "duckdb.seed",
		Connection:    "warehouse",
		SeedFileName:  "customers.csv",
		SeedFileBytes: []byte("id\n1\n"),
	})
	require.NotNil(t, apiErr)
	assert.Equal(t, "incompatible_connection", apiErr.Code)
}
