package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"renart/internal/web/fingerprint"
	"renart/internal/web/identity"
)

func TestExecutionTargetSnapshotCapturesSecretFreeTargetAndFingerprintEvidence(t *testing.T) {
	t.Parallel()

	exact := materializedTargetAsset(pipeline.AssetTypePostgresQuery, "analytics.customers", "private-warehouse-alias")
	exact.ExecutableFile = pipeline.ExecutableFile{Content: "select {{ var.threshold }} as threshold"}
	runtimeOnly := &pipeline.Asset{
		Name:           "analytics.preview",
		Type:           pipeline.AssetTypePostgresQuery,
		Connection:     "private-warehouse-alias",
		ExecutableFile: pipeline.ExecutableFile{Content: "select 1 as value"},
	}
	pl := &pipeline.Pipeline{
		LegacyID:       "pipeline-target-snapshot-id",
		Name:           "analytics",
		DefinitionFile: pipeline.DefinitionFile{Path: filepath.Join(t.TempDir(), "pipeline.yml")},
		Assets:         []*pipeline.Asset{exact, runtimeOnly},
		Variables: pipeline.Variables{
			"threshold": {"type": "integer", "default": 7},
			"unused":    {"type": "string", "default": "stable"},
		},
	}
	cfg := &config.Config{
		SelectedEnvironmentName: "private-environment",
		SelectedEnvironment: &config.Environment{Connections: &config.Connections{
			Postgres: []config.PostgresConnection{{
				ConnectionMetadata: targetMetadata("private-warehouse-alias"),
				Host:               "private.pg.internal",
				Port:               5432,
				Database:           "warehouse_database",
				Schema:             "private_schema",
				Username:           "private-user",
				Password:           "super-secret-password",
			}},
		}},
	}
	executor := NewHybridBruinExecutor(t.TempDir(), "", nil, nil)

	snapshot, err := executor.resolveExecutionTargetSnapshot(pl, cfg, pl.Assets)
	require.NoError(t, err)
	require.Equal(t, ExecutionTargetSnapshotVersion, snapshot.Version)
	assert.Equal(t, pl.LegacyID, snapshot.PipelineUUID)
	require.Len(t, snapshot.Entries, 2)

	vars := fingerprint.EffectiveVars(pl, nil)
	dag, err := fingerprint.NewEngine().DAG(pl, vars)
	require.NoError(t, err)
	varsHash := fingerprint.AllVarsHash(vars)
	for _, asset := range pl.Assets {
		assetID := identity.AssetID(pl.LegacyID, asset.Name)
		entry, ok := snapshot.Entries[asset.Name]
		require.True(t, ok)
		assert.Equal(t, assetID, entry.AssetID)
		assert.Equal(t, string(dag[assetID].FP), entry.Fingerprint)
		assert.Equal(t, string(dag[assetID].OwnContent), entry.OwnContent)
		assert.Equal(t, dag[assetID].ConsumedVarsHash, entry.ConsumedVarsHash)
		assert.Equal(t, varsHash, entry.VarsHash)
	}

	exactEntry := snapshot.Entries[exact.Name]
	assert.Equal(t, AssetRenderFidelityExact, exactEntry.TargetFidelity)
	assert.NotEmpty(t, exactEntry.TargetIdentity)
	runtimeEntry := snapshot.Entries[runtimeOnly.Name]
	assert.Equal(t, AssetRenderFidelityRuntimeOnly, runtimeEntry.TargetFidelity)
	assert.Empty(t, runtimeEntry.TargetIdentity)

	body, err := json.Marshal(snapshot)
	require.NoError(t, err)
	var wire struct {
		Version int                       `json:"version"`
		Entries map[string]map[string]any `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(body, &wire))
	require.Len(t, wire.Entries, 2)
	for _, entry := range wire.Entries {
		assert.ElementsMatch(t, []string{
			"asset_id",
			"target_identity",
			"target_fidelity",
			"fingerprint",
			"own_content",
			"consumed_vars_hash",
			"vars_hash",
			"upstreams",
			"coverage_mode",
			"refresh_restricted",
		}, mapKeys(entry))
	}
	for _, secret := range []string{
		"private-environment",
		"private-warehouse-alias",
		"private.pg.internal",
		"warehouse_database",
		"private_schema",
		"private-user",
		"super-secret-password",
	} {
		assert.NotContains(t, string(body), secret)
	}
}

func TestExecutionTargetSnapshotCallbackFailureStopsBeforeAnyTask(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(context.Context, *HybridBruinExecutor, string, func(ExecutionTargetSnapshot) error, func(ExecutionAssetEvent) error) error
	}{
		{
			name: "asset",
			run: func(ctx context.Context, executor *HybridBruinExecutor, assetPath string, onTargets func(ExecutionTargetSnapshot) error, onAsset func(ExecutionAssetEvent) error) error {
				_, err := executor.RunAsset(ctx, RunAssetRequest{
					AssetPath:         assetPath,
					OnTargetsResolved: onTargets,
					AssetEvent:        onAsset,
				}, nil)
				return err
			},
		},
		{
			name: "pipeline",
			run: func(ctx context.Context, executor *HybridBruinExecutor, _ string, onTargets func(ExecutionTargetSnapshot) error, onAsset func(ExecutionAssetEvent) error) error {
				_, err := executor.RunPipeline(ctx, RunPipelineRequest{
					Target:            "analytics",
					OnTargetsResolved: onTargets,
					AssetEvent:        onAsset,
				}, nil)
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspaceRoot, assetPath := createSuccessfulDuckDBWorkspace(t)
			addExecutionSnapshotPipelineID(t, workspaceRoot)
			executor, closeManager := executionSnapshotTestExecutor(t, workspaceRoot)
			defer closeManager()

			persistErr := errors.New("state database is unavailable")
			callbackCount := 0
			assetEvents := 0
			var captured ExecutionTargetSnapshot
			err := tc.run(context.Background(), executor, assetPath, func(snapshot ExecutionTargetSnapshot) error {
				callbackCount++
				captured = snapshot
				return persistErr
			}, func(ExecutionAssetEvent) error {
				assetEvents++
				return nil
			})

			require.ErrorIs(t, err, persistErr)
			assert.Equal(t, 1, callbackCount)
			assert.Zero(t, assetEvents)
			require.Contains(t, captured.Entries, "analytics.customers")
			assert.Equal(t, AssetRenderFidelityExact, captured.Entries["analytics.customers"].TargetFidelity)
			assert.False(t, executionSnapshotTableExists(t, executor), "the main materializer must not run")
		})
	}
}

func TestDirectRunAssetEventFailuresAreExecutionErrors(t *testing.T) {
	t.Run("running event aborts before the main task", func(t *testing.T) {
		workspaceRoot, assetPath := createSuccessfulDuckDBWorkspace(t)
		executor, closeManager := executionSnapshotTestExecutor(t, workspaceRoot)
		defer closeManager()
		persistErr := errors.New("running step was not durable")

		_, err := executor.RunAsset(context.Background(), RunAssetRequest{
			AssetPath: assetPath,
			AssetEvent: func(event ExecutionAssetEvent) error {
				if event.Status == "running" {
					return persistErr
				}
				return nil
			},
		}, nil)

		require.ErrorIs(t, err, persistErr)
		assert.False(t, executionSnapshotTableExists(t, executor))
	})

	t.Run("success event failure fails the completed execution", func(t *testing.T) {
		workspaceRoot, assetPath := createSuccessfulDuckDBWorkspace(t)
		executor, closeManager := executionSnapshotTestExecutor(t, workspaceRoot)
		defer closeManager()
		persistErr := errors.New("successful step was not durable")

		_, err := executor.RunAsset(context.Background(), RunAssetRequest{
			AssetPath: assetPath,
			AssetEvent: func(event ExecutionAssetEvent) error {
				if event.Status == "success" {
					return persistErr
				}
				return nil
			},
		}, nil)

		require.ErrorIs(t, err, persistErr)
		assert.True(t, executionSnapshotTableExists(t, executor), "the task completed before terminal persistence failed")
	})

	t.Run("failed event failure preserves both errors", func(t *testing.T) {
		workspaceRoot, assetPath := createSuccessfulDuckDBWorkspace(t)
		require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: table
@bruin */

select * from a_table_that_does_not_exist
`)+"\n"), 0o644))
		executor, closeManager := executionSnapshotTestExecutor(t, workspaceRoot)
		defer closeManager()
		persistErr := errors.New("failed step was not durable")

		_, err := executor.RunAsset(context.Background(), RunAssetRequest{
			AssetPath: assetPath,
			AssetEvent: func(event ExecutionAssetEvent) error {
				if event.Status == "failed" {
					return persistErr
				}
				return nil
			},
		}, nil)

		require.ErrorIs(t, err, persistErr)
		assert.Contains(t, err.Error(), "a_table_that_does_not_exist")
	})
}

func TestHybridBruinExecutorSetFingerprintEngine(t *testing.T) {
	t.Parallel()
	executor := NewHybridBruinExecutor(t.TempDir(), "", nil, nil)
	shared := fingerprint.NewEngine()

	executor.SetFingerprintEngine(shared)
	assert.Same(t, shared, executor.fingerprintEngine)
	executor.SetFingerprintEngine(nil)
	assert.NotNil(t, executor.fingerprintEngine)
	assert.NotSame(t, shared, executor.fingerprintEngine)
}

func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func addExecutionSnapshotPipelineID(t *testing.T, workspaceRoot string) {
	t.Helper()
	pipelinePath := filepath.Join(workspaceRoot, "analytics", "pipeline.yml")
	body, err := os.ReadFile(pipelinePath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(pipelinePath, append([]byte("id: pipeline-target-callback-id\n"), body...), 0o644))
}

func executionSnapshotTestExecutor(t *testing.T, workspaceRoot string) (*HybridBruinExecutor, func()) {
	t.Helper()
	cfg, err := loadSelectedConfig(filepath.Join(workspaceRoot, ".bruin.yml"), "")
	require.NoError(t, err)
	manager, err := newConnectionManagerFromConfig(context.Background(), cfg)
	require.NoError(t, err)
	executor := newCompatDirectExecutor(workspaceRoot, "")
	executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
		return manager, nil
	}
	cleanup := func() {
		if connection, ok := manager.GetConnection("duckdb-default").(interface{ Close() }); ok {
			connection.Close()
		}
	}
	return executor, cleanup
}

func executionSnapshotTableExists(t *testing.T, executor *HybridBruinExecutor) bool {
	t.Helper()
	body, err := executor.QueryConnection(context.Background(), QueryConnectionRequest{
		ConnectionName: "duckdb-default",
		Query:          "select count(*) from information_schema.tables where table_schema = 'analytics' and table_name = 'customers'",
		Output:         "json",
	})
	require.NoError(t, err)
	var payload struct {
		Rows [][]any `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Equal(t, 1, len(payload.Rows))
	require.Equal(t, 1, len(payload.Rows[0]))
	count, ok := payload.Rows[0][0].(float64)
	require.True(t, ok)
	return count > 0
}
