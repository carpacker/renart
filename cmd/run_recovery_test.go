package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"renart/internal/web/bus"
	"renart/internal/web/identity"
	webscheduler "renart/internal/web/scheduler"
	"renart/internal/web/snapshot"
)

func TestReplayRecoveredRunEmitsPersistedStepsAgainstPinnedSnapshot(t *testing.T) {
	ctx := context.Background()
	schedulerStore, err := webscheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer schedulerStore.Close()

	pipelineDir := filepath.Join(t.TempDir(), "analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineDir, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "assets", "finished.sql"), []byte("select 1\n"), 0o644))

	snapshotStore := snapshot.NewStore(schedulerStore.DB())
	deployed, _, err := snapshotStore.Deploy(ctx, "pipeline-uuid", pipelineDir, "test")
	require.NoError(t, err)

	events := bus.New()
	server := &webServer{snapshotStore: snapshotStore, eventBus: events}
	var got bus.RunCompleted
	var materializedFile string
	events.OnRunCompleted(func(event bus.RunCompleted) {
		got = event
		materializedFile = filepath.Join(event.SnapshotDir, "assets", "finished.sql")
		content, readErr := os.ReadFile(materializedFile)
		require.NoError(t, readErr)
		assert.Equal(t, "select 1\n", string(content))
	})

	windowStart := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(30 * time.Minute)
	finishedAt := windowEnd.Add(time.Minute)
	err = server.replayRecoveredRun(ctx, webscheduler.PipelineRun{
		ID: "run-id", PipelineID: "encoded-path", Pipeline: "analytics",
		Environment: "prod", Status: webscheduler.RunStatusFailed,
		WinStart: &windowStart, WinEnd: &windowEnd, FinishedAt: &finishedAt,
		SnapshotVersionID: deployed.VersionID, FullRefresh: true,
		ExecutionContextResolved: true,
	}, []webscheduler.PipelineRunStep{
		{Asset: "analytics.finished", Status: webscheduler.RunStatusSuccess},
		{Asset: "analytics.interrupted", Status: webscheduler.RunStatusFailed},
		{Asset: "analytics.unreached", Status: webscheduler.RunStatusQueued},
	})
	require.NoError(t, err)

	assert.Equal(t, "run-id", got.RunID)
	assert.Equal(t, "pipeline-uuid", got.PipelineUUID)
	assert.Equal(t, "prod", got.Environment)
	assert.Equal(t, deployed.VersionID, got.SnapshotVersionID)
	assert.Equal(t, finishedAt, got.CompletedAt)
	assert.Equal(t, &windowStart, got.WinStart)
	assert.Equal(t, &windowEnd, got.WinEnd)
	assert.True(t, got.FullRefresh)
	assert.Equal(t, []bus.AssetRun{
		{AssetID: identity.AssetID("pipeline-uuid", "analytics.finished"), AssetName: "analytics.finished", Status: "succeeded"},
		{AssetID: identity.AssetID("pipeline-uuid", "analytics.interrupted"), AssetName: "analytics.interrupted", Status: "failed"},
	}, got.Assets)
	assert.NoDirExists(t, got.SnapshotDir, "snapshot temp dir is removed after synchronous dispatch")
	assert.NoFileExists(t, materializedFile)
}

func TestRecoveredAssetRunStatusIgnoresNonTerminalSteps(t *testing.T) {
	for _, status := range []webscheduler.RunStatus{"", webscheduler.RunStatusQueued, webscheduler.RunStatusRunning} {
		mapped, ok := recoveredAssetRunStatus(status)
		assert.False(t, ok)
		assert.Empty(t, mapped)
	}
}

func TestReplayRecoveredRunSkipsSourceResolutionWithoutTerminalSteps(t *testing.T) {
	t.Parallel()
	events := bus.New()
	emitted := false
	events.OnRunCompleted(func(bus.RunCompleted) { emitted = true })
	server := &webServer{eventBus: events}

	err := server.replayRecoveredRun(context.Background(), webscheduler.PipelineRun{
		ID: "pre-execution", SnapshotVersionID: "missing",
	}, []webscheduler.PipelineRunStep{{Asset: "analytics.pending", Status: webscheduler.RunStatusRunning}})
	require.NoError(t, err)
	assert.False(t, emitted)
}

func TestReplayRecoveredRunSkipsUnresolvedContextWithTerminalSteps(t *testing.T) {
	t.Parallel()
	events := bus.New()
	emitted := false
	events.OnRunCompleted(func(bus.RunCompleted) { emitted = true })
	server := &webServer{eventBus: events}

	err := server.replayRecoveredRun(context.Background(), webscheduler.PipelineRun{
		ID: "legacy-unresolved", SnapshotVersionID: "must-not-be-resolved",
	}, []webscheduler.PipelineRunStep{{Asset: "analytics.finished", Status: webscheduler.RunStatusSuccess}})
	require.NoError(t, err)
	assert.False(t, emitted)
}

func TestReplayRecoveredRunRejectsCorruptPinnedSnapshotWithTerminalSteps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	schedulerStore, err := webscheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer schedulerStore.Close()
	pipelineDir := filepath.Join(t.TempDir(), "analytics")
	require.NoError(t, os.MkdirAll(pipelineDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	snapshotStore := snapshot.NewStore(schedulerStore.DB())
	deployed, _, err := snapshotStore.Deploy(ctx, "pipeline-uuid", pipelineDir, "test")
	require.NoError(t, err)
	hash := deployed.Manifest["pipeline.yml"]
	_, err = schedulerStore.DB().ExecContext(ctx, `UPDATE renart_blobs SET content = ? WHERE hash = ?`, []byte("corrupt"), hash)
	require.NoError(t, err)

	events := bus.New()
	emitted := false
	events.OnRunCompleted(func(bus.RunCompleted) { emitted = true })
	server := &webServer{snapshotStore: snapshotStore, eventBus: events}
	err = server.replayRecoveredRun(ctx, webscheduler.PipelineRun{
		ID: "corrupt-run", SnapshotVersionID: deployed.VersionID,
		ExecutionContextResolved: true,
	}, []webscheduler.PipelineRunStep{{Asset: "analytics.finished", Status: webscheduler.RunStatusSuccess}})
	require.ErrorContains(t, err, "blob hash mismatch")
	assert.False(t, emitted)
}
