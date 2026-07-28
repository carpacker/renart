package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigLoadPreservesSecretPlaceholdersAgainstAmbientEnvironment(t *testing.T) {
	t.Setenv("RENART_TEST_DATABASE_HOST", "db.internal")
	t.Setenv("RENART_WAREHOUSE_PASSWORD", "ambient-value-must-not-win")

	root := t.TempDir()
	configPath := filepath.Join(root, ".bruin.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
default_environment: default
environments:
  default:
    connections:
      postgres:
        - name: warehouse
          host: ${RENART_TEST_DATABASE_HOST}
          port: 5432
          database: analytics
          username: renart
          password: ${RENART_WAREHOUSE_PASSWORD}
`), 0o600))

	runtimeConfig, err := loadSelectedConfig(configPath, "default")
	require.NoError(t, err)
	require.Len(t, runtimeConfig.SelectedEnvironment.Connections.Postgres, 1)
	assert.Equal(t, "db.internal", runtimeConfig.SelectedEnvironment.Connections.Postgres[0].Host)
	assert.Equal(
		t,
		"${RENART_WAREHOUSE_PASSWORD}",
		runtimeConfig.SelectedEnvironment.Connections.Postgres[0].Password,
	)

	editingConfig, _, err := NewConfigService(root, configPath).LoadForEditing()
	require.NoError(t, err)
	require.Len(t, editingConfig.SelectedEnvironment.Connections.Postgres, 1)
	assert.Equal(t, "db.internal", editingConfig.SelectedEnvironment.Connections.Postgres[0].Host)
	assert.Equal(
		t,
		"${RENART_WAREHOUSE_PASSWORD}",
		editingConfig.SelectedEnvironment.Connections.Postgres[0].Password,
	)
}

func TestReadOnlyConfigLoadPreservesSecretPlaceholdersFromEnvironmentConfig(t *testing.T) {
	t.Setenv("RENART_WAREHOUSE_PASSWORD", "ambient-value-must-not-win")
	t.Setenv("BRUIN_CONFIG_FILE_CONTENT", `
default_environment: default
environments:
  default:
    connections:
      postgres:
        - name: warehouse
          host: localhost
          port: 5432
          database: analytics
          username: renart
          password: ${RENART_WAREHOUSE_PASSWORD}
`)

	cfg, err := loadSelectedConfigReadOnlyFS(afero.NewMemMapFs(), "/missing/.bruin.yml", "default")
	require.NoError(t, err)
	require.Len(t, cfg.SelectedEnvironment.Connections.Postgres, 1)
	assert.Equal(
		t,
		"${RENART_WAREHOUSE_PASSWORD}",
		cfg.SelectedEnvironment.Connections.Postgres[0].Password,
	)
}

func TestConfigServiceReadOnlyLoadDoesNotMutateProjectFiles(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".bruin.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: duckdb-default
          path: warehouse.duckdb
`), 0o600))

	cfg, loadedPath, err := NewConfigService(root, configPath).LoadReadOnly()
	require.NoError(t, err)
	assert.Equal(t, configPath, loadedPath)
	require.Len(t, cfg.SelectedEnvironment.Connections.DuckDB, 1)
	_, err = os.Stat(filepath.Join(root, ".gitignore"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestApplySelectedEnvironmentRefreshRestriction(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{Name: "analytics.orders"}
	cfg := &config.Config{SelectedEnvironment: &config.Environment{
		Config: &config.EnvironmentConfig{RefreshRestricted: true},
	}}

	applySelectedEnvironmentRefreshRestriction(cfg, []*pipeline.Asset{asset, nil})

	require.NotNil(t, asset.RefreshRestricted)
	assert.True(t, *asset.RefreshRestricted)
}

func TestApplySelectedEnvironmentRefreshRestrictionLeavesUnrestrictedAssetsAlone(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{Name: "analytics.orders"}
	applySelectedEnvironmentRefreshRestriction(&config.Config{}, []*pipeline.Asset{asset})
	assert.Nil(t, asset.RefreshRestricted)
}
