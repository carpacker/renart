package service

import (
	"path/filepath"
	"testing"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/identity"
)

func TestPrepareDraftConnectionReplacesExistingConnection(t *testing.T) {
	svc := NewConfigService("/tmp/workspace", "/tmp/workspace/.bruin.yml")
	cfg := &config.Config{
		DefaultEnvironmentName:  "default",
		SelectedEnvironmentName: "default",
		Environments: map[string]config.Environment{
			"default": {
				Connections: &config.Connections{},
			},
		},
	}

	require.NoError(t, svc.prepareDraftConnection(cfg, TestWorkspaceConnectionParams{
		EnvironmentName: "default",
		Name:            "postgres-default",
		Type:            "postgres",
		Values: map[string]any{
			"host":     "127.0.0.1",
			"port":     5432,
			"database": "bruin",
			"username": "postgres",
			"password": "secret",
		},
	}))

	require.NoError(t, svc.prepareDraftConnection(cfg, TestWorkspaceConnectionParams{
		EnvironmentName: "default",
		CurrentName:     "postgres-default",
		Name:            "postgres-default",
		Type:            "postgres",
		Values: map[string]any{
			"host":     "localhost",
			"port":     5433,
			"database": "bruin",
			"username": "postgres",
			"password": "updated",
		},
	}))

	env := cfg.Environments["default"]
	require.Len(t, env.Connections.Postgres, 1)
	assert.Equal(t, "postgres-default", env.Connections.Postgres[0].Name)
	assert.Equal(t, "localhost", env.Connections.Postgres[0].Host)
	assert.Equal(t, 5433, env.Connections.Postgres[0].Port)
	assert.Equal(t, "updated", env.Connections.Postgres[0].Password)
}

func TestSetProjectRetentionPersistsValidatedTrackedSettings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	svc := NewConfigService(root, filepath.Join(root, ".bruin.yml"))

	project, err := svc.SetProjectRetention(WorkspaceRetentionSettings{
		RunMetadata:               WorkspaceRetentionWindow{Days: 60, MinimumPerPipeline: 12},
		FullLogs:                  WorkspaceRetentionWindow{Days: 14, MinimumPerPipeline: 5},
		MaterializationFactsDays:  45,
		ScheduleHistoryDays:       120,
		Deployments:               WorkspaceRetentionWindow{Days: 30, MinimumPerPipeline: 7},
		TemporaryDirectoriesHours: 48,
	})
	require.NoError(t, err)
	require.NotNil(t, project.Retention)
	assert.Equal(t, 60, project.Retention.RunMetadata.Days)

	loaded, err := identity.LoadProject(
		afero.NewOsFs(),
		filepath.Join(root, ".renart", "project.yml"),
	)
	require.NoError(t, err)
	require.NotNil(t, loaded.Retention)
	assert.Equal(t, identity.RetentionWindow{Days: 14, MinimumPerPipeline: 5}, loaded.Retention.FullLogs)
	assert.Equal(t, 48, loaded.Retention.TemporaryDirectoriesHours)

	_, err = svc.SetProjectRetention(WorkspaceRetentionSettings{
		RunMetadata: WorkspaceRetentionWindow{Days: -1},
	})
	require.ErrorContains(t, err, "run metadata retention")
}
