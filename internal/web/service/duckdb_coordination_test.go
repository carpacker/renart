package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"renart/internal/web/duckcoord"
)

type duckDBCoordinationManager struct {
	details map[string]any
}

func (m duckDBCoordinationManager) GetConnection(string) any { return nil }

func (m duckDBCoordinationManager) GetConnectionDetails(name string) any {
	return m.details[name]
}

func (m duckDBCoordinationManager) GetConnectionType(string) string { return "duckdb" }

func TestAcquireDuckDBConnectionsUsesCanonicalConnectionPath(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	databasePath := filepath.Join(workspaceRoot, "warehouse.duckdb")
	manager := duckDBCoordinationManager{details: map[string]any{
		"first":  &config.DuckDBConnection{Path: databasePath},
		"second": config.DuckDBConnection{Path: "duckdb://" + filepath.ToSlash(databasePath) + "?access_mode=read_only"},
	}}
	executor := NewHybridBruinExecutor(workspaceRoot, "", nil, nil)
	executor.SetDuckDBCoordinator(duckcoord.New(duckcoord.Options{LockDir: t.TempDir(), RetryDelay: time.Millisecond}))

	first, err := executor.acquireDuckDBConnections(context.Background(), manager, []string{"first"}, duckcoord.Owner{}, nil)
	require.NoError(t, err)
	defer first.Release()

	waited := false
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = executor.acquireDuckDBConnections(ctx, manager, []string{"second"}, duckcoord.Owner{OnWait: func(string) { waited = true }}, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
	assert.True(t, waited)
}

func TestDuckDBConnectionNamesForAssetIncludesBothIngestrEnds(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{
		Name:       "analytics.replication",
		Type:       pipeline.AssetTypeIngestr,
		Connection: "destination",
		Parameters: pipeline.ParameterMap{"source_connection": "source"},
	}
	names := duckDBConnectionNamesForAsset(&pipeline.Pipeline{}, asset)
	assert.ElementsMatch(t, []string{"source", "destination"}, names)
}

func TestDuckDBConnectionNamesForAssetDefersPythonUntilMaterialization(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{Name: "analytics.python", Type: pipeline.AssetTypePython, Connection: "destination"}
	assert.Empty(t, duckDBConnectionNamesForAsset(&pipeline.Pipeline{}, asset))
}
