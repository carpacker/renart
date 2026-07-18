package scheduler

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorePersistsStepUpstreamWriterSnapshotAcrossTerminalUpdates(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()
	runID, err := store.Create(ctx, PipelineRun{
		ID: "read-set-run", PipelineID: "pipeline-id", Pipeline: "analytics",
		Trigger: RunTriggerManual, Status: RunStatusQueued,
	})
	require.NoError(t, err)
	started := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	writers := testUpstreamWriterSnapshot()
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{
		RunID: runID, Asset: "analytics.orders", Status: RunStatusRunning, StartedAt: &started,
		UpstreamWriters: writers, HasUpstreamWriterSnapshot: true,
	}))

	steps, err := store.ListSteps(ctx, runID)
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.True(t, steps[0].HasUpstreamWriterSnapshot)
	assert.Equal(t, writers, steps[0].UpstreamWriters)

	// Terminal callbacks are allowed to omit the already captured read set. The
	// persisted running evidence remains authoritative.
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{
		RunID: runID, Asset: "analytics.orders", Status: RunStatusSuccess,
		StartedAt: &started, FinishedAt: &finished,
	}))
	steps, err = store.ListSteps(ctx, runID)
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, RunStatusSuccess, steps[0].Status)
	assert.True(t, steps[0].HasUpstreamWriterSnapshot)
	assert.Equal(t, writers, steps[0].UpstreamWriters)
}

func TestStoreDistinguishesEmptyCapturedReadSetFromLegacyAbsence(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()
	runID, err := store.Create(ctx, PipelineRun{
		ID: "empty-read-set-run", PipelineID: "pipeline-id", Pipeline: "analytics",
		Trigger: RunTriggerManual, Status: RunStatusQueued,
	})
	require.NoError(t, err)
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{
		RunID: runID, Asset: "analytics.source", Status: RunStatusRunning,
		HasUpstreamWriterSnapshot: true,
	}))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{
		RunID: runID, Asset: "analytics.legacy", Status: RunStatusRunning,
	}))

	steps, err := store.ListSteps(ctx, runID)
	require.NoError(t, err)
	require.Len(t, steps, 2)
	byAsset := map[string]PipelineRunStep{steps[0].Asset: steps[0], steps[1].Asset: steps[1]}
	assert.True(t, byAsset["analytics.source"].HasUpstreamWriterSnapshot)
	assert.NotNil(t, byAsset["analytics.source"].UpstreamWriters)
	assert.Empty(t, byAsset["analytics.source"].UpstreamWriters)
	assert.False(t, byAsset["analytics.legacy"].HasUpstreamWriterSnapshot)
	assert.Nil(t, byAsset["analytics.legacy"].UpstreamWriters)
}

func TestStoreRejectsConflictingStepUpstreamWriterSnapshotWithoutUpdatingStep(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()
	runID, err := store.Create(ctx, PipelineRun{
		ID: "conflicting-read-set-run", PipelineID: "pipeline-id", Pipeline: "analytics",
		Trigger: RunTriggerManual, Status: RunStatusQueued,
	})
	require.NoError(t, err)
	writers := testUpstreamWriterSnapshot()
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{
		RunID: runID, Asset: "analytics.orders", Status: RunStatusRunning,
		UpstreamWriters: writers, HasUpstreamWriterSnapshot: true,
	}))
	conflicting := testUpstreamWriterSnapshot()
	entry := conflicting["pipeline-uuid:analytics.raw_orders"]
	entry.Fingerprint = "v2:changed"
	conflicting[entry.AssetID] = entry
	finished := time.Date(2026, 7, 17, 10, 1, 0, 0, time.UTC)
	err = store.UpsertStep(ctx, PipelineRunStep{
		RunID: runID, Asset: "analytics.orders", Status: RunStatusSuccess, FinishedAt: &finished,
		UpstreamWriters: conflicting, HasUpstreamWriterSnapshot: true,
	})
	require.ErrorIs(t, err, ErrUpstreamWriterSnapshotConflict)

	steps, err := store.ListSteps(ctx, runID)
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, RunStatusRunning, steps[0].Status)
	assert.Nil(t, steps[0].FinishedAt)
	assert.Equal(t, writers, steps[0].UpstreamWriters)
}

func TestStepUpstreamWriterSnapshotMigrationPreservesLegacyAbsence(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenStore(path)
	require.NoError(t, err)
	ctx := context.Background()
	migrations, err := fs.Sub(schedulerMigrations, "storedb/migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, store.db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 16)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO pipeline_runs (id, pipeline_id, pipeline, environment, trigger, status)
		VALUES ('legacy-read-set-run', 'pipeline-id', 'analytics', 'prod', 'manual', 'running')`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO pipeline_run_steps (run_id, asset, status, started_at)
		VALUES ('legacy-read-set-run', 'analytics.orders', 'running', '2026-07-17T10:00:00Z')`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = OpenStore(path)
	require.NoError(t, err)
	steps, err := store.ListSteps(ctx, "legacy-read-set-run")
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.False(t, steps[0].HasUpstreamWriterSnapshot)
	assert.Nil(t, steps[0].UpstreamWriters)
	assert.Equal(t, 1, countRows(t, store, `
		SELECT COUNT(*) FROM pragma_table_info('pipeline_run_steps')
		WHERE name = 'upstream_writer_snapshot'`))

	migrations, err = fs.Sub(schedulerMigrations, "storedb/migrations")
	require.NoError(t, err)
	provider, err = goose.NewProvider(goose.DialectSQLite3, store.db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 16)
	require.NoError(t, err)
	assert.Equal(t, 0, countRows(t, store, `
		SELECT COUNT(*) FROM pragma_table_info('pipeline_run_steps')
		WHERE name = 'upstream_writer_snapshot'`))
	require.NoError(t, store.Close())
}

func testUpstreamWriterSnapshot() map[string]UpstreamWriterSnapshot {
	assetID := "pipeline-uuid:analytics.raw_orders"
	return map[string]UpstreamWriterSnapshot{
		assetID: {
			AssetID:           assetID,
			TargetIdentity:    "renart-physical-target-v1:raw-orders",
			Fingerprint:       "v2:raw-orders",
			VarsHash:          "vars-hash",
			TargetGeneration:  4,
			CompletionID:      "upstream-completion",
			CompletionOrdinal: 2,
			MaterializedAt:    time.Date(2026, 7, 17, 9, 55, 0, 123, time.UTC),
		},
	}
}
