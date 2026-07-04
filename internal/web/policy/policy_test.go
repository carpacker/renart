package policy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	t.Parallel()
	cfg, err := Load(filepath.Join(t.TempDir(), "environments.yml"))
	require.NoError(t, err)
	assert.True(t, cfg.For("prod").Zero())
}

func TestLoadParsesPolicies(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "environments.yml")
	require.NoError(t, os.WriteFile(path, []byte(`
environments:
  prod:
    protected: true
    deployed_only: true
    confirm_destructive: true
  staging:
    confirm_destructive: true
`), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, EnvironmentPolicy{Protected: true, DeployedOnly: true, ConfirmDestructive: true}, cfg.For("prod"))
	assert.Equal(t, EnvironmentPolicy{ConfirmDestructive: true}, cfg.For("staging"))
	assert.True(t, cfg.For("dev").Zero())
}

func TestSaveOmitsZeroPolicies(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".renart", "environments.yml")

	require.NoError(t, Save(path, Config{Environments: map[string]EnvironmentPolicy{
		"prod": {Protected: true},
		"dev":  {},
	}}))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, EnvironmentPolicy{Protected: true}, cfg.For("prod"))
	assert.True(t, cfg.For("dev").Zero())
}

func TestLoaderSetPersistsPolicy(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".renart", "environments.yml")
	loader := NewLoader(path)

	cfg, err := loader.Set("prod", EnvironmentPolicy{Protected: true, ConfirmDestructive: true})
	require.NoError(t, err)
	assert.Equal(t, EnvironmentPolicy{Protected: true, ConfirmDestructive: true}, cfg.For("prod"))
	assert.Equal(t, EnvironmentPolicy{Protected: true, ConfirmDestructive: true}, loader.For("prod"))

	cfg, err = loader.Set("prod", EnvironmentPolicy{})
	require.NoError(t, err)
	assert.True(t, cfg.For("prod").Zero())
	assert.True(t, loader.For("prod").Zero())
}

func TestCheckProtectedBlocksInteractiveOnly(t *testing.T) {
	t.Parallel()
	p := EnvironmentPolicy{Protected: true}

	err := Check(p, RunRequest{Environment: "prod", Interactive: true})
	require.ErrorContains(t, err, "protected")

	// Scheduled snapshot runs pass trivially.
	assert.NoError(t, Check(p, RunRequest{Environment: "prod", SnapshotBased: true}))
	// Non-interactive working-tree (scheduled fallback) passes under
	// protected alone; deployed_only is the flag that forbids it.
	assert.NoError(t, Check(p, RunRequest{Environment: "prod"}))
}

func TestCheckDeployedOnly(t *testing.T) {
	t.Parallel()
	p := EnvironmentPolicy{DeployedOnly: true}
	require.ErrorContains(t, Check(p, RunRequest{Environment: "prod"}), "deployed snapshots")
	assert.NoError(t, Check(p, RunRequest{Environment: "prod", SnapshotBased: true}))
}

func TestCheckConfirmDestructive(t *testing.T) {
	t.Parallel()
	p := EnvironmentPolicy{ConfirmDestructive: true}
	require.Error(t, Check(p, RunRequest{Environment: "prod", Destructive: true}))
	require.Error(t, Check(p, RunRequest{Environment: "prod", Destructive: true, ConfirmedEnvironment: "prdo"}))
	assert.NoError(t, Check(p, RunRequest{Environment: "prod", Destructive: true, ConfirmedEnvironment: "prod"}))
	assert.NoError(t, Check(p, RunRequest{Environment: "prod"}))
}

func TestLoaderRevalidatesByStat(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "environments.yml")
	loader := NewLoader(path)
	assert.True(t, loader.For("prod").Zero())

	require.NoError(t, os.WriteFile(path, []byte("environments:\n  prod:\n    protected: true\n"), 0o644))
	assert.True(t, loader.For("prod").Protected)

	require.NoError(t, os.Remove(path))
	assert.True(t, loader.For("prod").Zero())
}
