package service

import (
	"testing"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
