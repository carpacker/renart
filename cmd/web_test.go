package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/policy"
	"renart/internal/web/scheduler"
	"renart/internal/web/service"
	"renart/internal/web/snapshot"
)

func TestNormalizeTriggerEnvironmentBeforePolicyLookup(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "prod", normalizeTriggerEnvironment("  prod  ", "dev"))
	assert.Equal(t, "dev", normalizeTriggerEnvironment("  ", "  dev  "))
}

func TestEnvScheduleTriggerRequestKeepsPinnedSourceAndPrivateVariables(t *testing.T) {
	t.Parallel()
	req, err := envScheduleTriggerRequest(scheduler.EnvSchedule{
		Environment: "prod", SnapshotVersionID: "deployment-id",
		Vars: map[string]any{"region": "eu"}, Status: scheduler.ScheduleStatusPaused,
	})
	require.NoError(t, err)
	assert.Equal(t, "prod", req.Environment)
	assert.Equal(t, scheduler.RunSourceSnapshot, req.Source)
	assert.Equal(t, "deployment-id", req.SnapshotVersionID)
	assert.Equal(t, map[string]any{"region": "eu"}, req.VariableOverrides)
	assert.Empty(t, req.Start)
	assert.Empty(t, req.End)

	_, err = envScheduleTriggerRequest(scheduler.EnvSchedule{Status: scheduler.ScheduleStatusArchived})
	require.ErrorContains(t, err, "not found")
}

func TestResolveTriggerRunSourceDefaultsByEnvironmentPolicy(t *testing.T) {
	t.Parallel()

	request := scheduler.TriggerRequest{Environment: "dev"}
	require.NoError(t, resolveTriggerRunSource(context.Background(), nil, "pipeline-a", "dev", policy.EnvironmentPolicy{}, &request))
	assert.Equal(t, scheduler.RunSourceWorkingTree, request.Source)
	assert.Empty(t, request.SnapshotVersionID)

	store, versionID := deployTriggerTestSnapshot(t, "pipeline-a")
	request = scheduler.TriggerRequest{Environment: "prod"}
	require.NoError(t, resolveTriggerRunSource(context.Background(), store, "pipeline-a", "prod", policy.EnvironmentPolicy{DeployedOnly: true}, &request))
	assert.Equal(t, scheduler.RunSourceSnapshot, request.Source)
	assert.Equal(t, versionID, request.SnapshotVersionID)
}

func TestResolveTriggerRunSourceValidatesExactSnapshot(t *testing.T) {
	t.Parallel()
	store, versionID := deployTriggerTestSnapshot(t, "pipeline-a")

	request := scheduler.TriggerRequest{Source: scheduler.RunSourceSnapshot, SnapshotVersionID: "  " + versionID + "  "}
	require.NoError(t, resolveTriggerRunSource(context.Background(), store, "pipeline-a", "dev", policy.EnvironmentPolicy{}, &request))
	assert.Equal(t, versionID, request.SnapshotVersionID)

	request = scheduler.TriggerRequest{Source: scheduler.RunSourceSnapshot, SnapshotVersionID: versionID}
	err := resolveTriggerRunSource(context.Background(), store, "pipeline-b", "dev", policy.EnvironmentPolicy{}, &request)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "belongs to pipeline pipeline-a")
}

func TestResolveTriggerRunSourceRejectsAmbiguousOrUnavailableSource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		policy  policy.EnvironmentPolicy
		request scheduler.TriggerRequest
		want    string
	}{
		{name: "deployed only without deployment", policy: policy.EnvironmentPolicy{DeployedOnly: true}, want: "deploy the pipeline first"},
		{name: "pin without source", request: scheduler.TriggerRequest{SnapshotVersionID: "snapshot-7"}, want: "source is required"},
		{name: "snapshot without pin", request: scheduler.TriggerRequest{Source: scheduler.RunSourceSnapshot}, want: "snapshot_version_id is required"},
		{name: "working tree with pin", request: scheduler.TriggerRequest{Source: scheduler.RunSourceWorkingTree, SnapshotVersionID: "snapshot-7"}, want: "snapshot_version_id must be empty"},
		{name: "unknown source", request: scheduler.TriggerRequest{Source: scheduler.RunSource("latest")}, want: "invalid run source"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			request := tt.request
			err := resolveTriggerRunSource(context.Background(), nil, "pipeline-a", "prod", tt.policy, &request)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestMaterializeExactRunSnapshotUsesPinAndCleansSandbox(t *testing.T) {
	t.Parallel()
	store, versionID := deployTriggerTestSnapshot(t, "pipeline-a")
	deployed, err := store.ValidateMetadata(context.Background(), versionID, "pipeline-a")
	require.NoError(t, err)
	spec := service.PipelineRunSpec{
		SnapshotVersionID:    "  " + versionID + "  ",
		ExpectedSourceMerkle: deployed.MerkleRoot,
	}
	var logs string

	cleanup, err := materializeExactRunSnapshot(
		context.Background(), store, "pipeline-a", "/tmp/test-bruin.yml", false, &spec,
		func(line string) { logs += line },
	)
	require.NoError(t, err)
	require.NotEmpty(t, spec.SnapshotDir)
	assert.Equal(t, versionID, spec.SnapshotVersionID)
	assert.Equal(t, "/tmp/test-bruin.yml", spec.ConfigPath)
	assert.Contains(t, logs, versionID)
	_, err = os.Stat(filepath.Join(spec.SnapshotDir, "pipeline.yml"))
	require.NoError(t, err)
	sandbox := spec.SnapshotDir

	cleanup()
	_, err = os.Stat(sandbox)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestResolveRunSnapshotFreezesConfirmedWorkingTreeSource(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	pipelineDir := filepath.Join(workspace, "analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineDir, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	assetPath := filepath.Join(pipelineDir, "assets", "orders.sql")
	require.NoError(t, os.WriteFile(assetPath, []byte("select 1"), 0o644))
	manifest, err := snapshot.CollectManifestHashes(pipelineDir)
	require.NoError(t, err)

	server := &webServer{workspaceRoot: workspace}
	spec := service.PipelineRunSpec{
		PipelineID:           service.EncodeID("analytics"),
		ExpectedSourceMerkle: snapshot.ManifestRoot(manifest),
	}
	cleanup, err := server.resolveRunSnapshot(context.Background(), &spec, false, nil)
	require.NoError(t, err)
	defer cleanup()
	require.NotEmpty(t, spec.SnapshotDir)
	require.NoError(t, os.WriteFile(assetPath, []byte("select 2"), 0o644))
	copied, err := os.ReadFile(filepath.Join(spec.SnapshotDir, "assets", "orders.sql"))
	require.NoError(t, err)
	assert.Equal(t, "select 1", string(copied))
}

func TestResolveRunSnapshotUsesStableUUIDAfterWorkingTreePathChanges(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	pipelineDir := filepath.Join(workspace, "renamed-analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineDir, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "pipeline.yml"), []byte("id: stable-pipeline-uuid\nname: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "assets", "orders.sql"), []byte("select 1"), 0o644))
	manifest, err := snapshot.CollectManifestHashes(pipelineDir)
	require.NoError(t, err)

	coordinator := service.NewWorkspaceCoordinator(service.WorkspaceCoordinatorDependencies{})
	coordinator.SetState(service.WorkspaceState{Pipelines: []service.WorkspacePipeline{{
		ID: service.EncodeID("renamed-analytics"), UUID: "stable-pipeline-uuid",
	}}})
	server := &webServer{workspaceRoot: workspace, workspaceCoord: coordinator}
	spec := service.PipelineRunSpec{
		PipelineID:           service.EncodeID("old-analytics-path"),
		PipelineUUID:         "stable-pipeline-uuid",
		ExpectedSourceMerkle: snapshot.ManifestRoot(manifest),
	}

	cleanup, err := server.resolveRunSnapshot(context.Background(), &spec, false, nil)
	require.NoError(t, err)
	defer cleanup()
	require.FileExists(t, filepath.Join(spec.SnapshotDir, "assets", "orders.sql"))
}

func TestResolveRunSnapshotRejectsChangedConfirmedSource(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	pipelineDir := filepath.Join(workspace, "analytics")
	require.NoError(t, os.MkdirAll(pipelineDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	server := &webServer{workspaceRoot: workspace}
	spec := service.PipelineRunSpec{
		PipelineID:           service.EncodeID("analytics"),
		ExpectedSourceMerkle: strings.Repeat("0", 64),
	}

	cleanup, err := server.resolveRunSnapshot(context.Background(), &spec, false, nil)
	cleanup()
	require.ErrorContains(t, err, "pipeline source changed after plan confirmation")
	assert.Empty(t, spec.SnapshotDir)
}

func TestMaterializeExactRunSnapshotRejectsChangedConfirmedDeployment(t *testing.T) {
	t.Parallel()
	store, versionID := deployTriggerTestSnapshot(t, "pipeline-a")
	spec := service.PipelineRunSpec{
		SnapshotVersionID:    versionID,
		ExpectedSourceMerkle: strings.Repeat("0", 64),
	}

	cleanup, err := materializeExactRunSnapshot(context.Background(), store, "pipeline-a", "", false, &spec, nil)
	cleanup()
	require.ErrorContains(t, err, "pipeline source changed after plan confirmation")
	assert.Empty(t, spec.SnapshotDir)
}

func TestResolveRunSnapshotUsesAdmittedUUIDAfterPipelinePathChanges(t *testing.T) {
	t.Parallel()
	store, versionID := deployTriggerTestSnapshot(t, "stable-pipeline-uuid")
	server := &webServer{
		workspaceRoot: t.TempDir(),
		snapshotStore: store,
	}
	spec := service.PipelineRunSpec{
		PipelineID:        "path-that-is-no-longer-in-the-workspace",
		PipelineUUID:      "stable-pipeline-uuid",
		SnapshotVersionID: versionID,
	}

	cleanup, err := server.resolveRunSnapshot(context.Background(), &spec, true, nil)
	require.NoError(t, err)
	defer cleanup()
	require.FileExists(t, filepath.Join(spec.SnapshotDir, "pipeline.yml"))
}

func TestPipelineRunSpecFromSchedulerRequestPreservesStableUUID(t *testing.T) {
	t.Parallel()
	spec := pipelineRunSpecFromSchedulerRequest(scheduler.RunRequest{
		RunID: "run-id", PipelineID: "old-path", PipelineUUID: "stable-pipeline-uuid",
	})
	assert.Equal(t, "stable-pipeline-uuid", spec.PipelineUUID)
}

func TestMaterializeExactRunSnapshotFailsClosed(t *testing.T) {
	t.Parallel()
	store, versionID := deployTriggerTestSnapshot(t, "pipeline-a")
	tests := []struct {
		name      string
		store     *snapshot.Store
		pipeline  string
		scheduled bool
		version   string
		want      string
	}{
		{name: "scheduled run without pin", store: store, pipeline: "pipeline-a", scheduled: true, want: "no pinned deployment"},
		{name: "missing snapshot store", pipeline: "pipeline-a", version: versionID, want: "snapshot store is unavailable"},
		{name: "wrong pipeline", store: store, pipeline: "pipeline-b", version: versionID, want: "belongs to pipeline pipeline-a"},
		{name: "unknown version", store: store, pipeline: "pipeline-a", version: "missing", want: "load metadata"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := service.PipelineRunSpec{SnapshotVersionID: tt.version}
			cleanup, err := materializeExactRunSnapshot(context.Background(), tt.store, tt.pipeline, "", tt.scheduled, &spec, nil)
			cleanup()
			require.ErrorContains(t, err, tt.want)
			assert.Empty(t, spec.SnapshotDir)
			assert.Equal(t, tt.version, spec.SnapshotVersionID)
		})
	}

	t.Run("manual working tree", func(t *testing.T) {
		spec := service.PipelineRunSpec{}
		cleanup, err := materializeExactRunSnapshot(context.Background(), nil, "", "", false, &spec, nil)
		cleanup()
		require.NoError(t, err)
		assert.Empty(t, spec.SnapshotDir)
	})
}

func TestQueuedPinnedRunFailsBeforeExecutionIfSnapshotBecomesCorrupt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	schedulerStore, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer schedulerStore.Close()
	snapshotStore := snapshot.NewStore(schedulerStore.DB())
	pipelineDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "pipeline.yml"), []byte("name: source-test\n"), 0o644))
	deployed, _, err := snapshotStore.Deploy(ctx, "pipeline-a", pipelineDir, "test")
	require.NoError(t, err)

	runnerReady := make(chan struct{})
	continueRun := make(chan struct{})
	var executionCalls atomic.Int32
	schedulerService := scheduler.New(scheduler.Options{
		Store:    schedulerStore,
		StateDir: t.TempDir(),
		Runner: func(ctx context.Context, req scheduler.RunRequest, onLog func(string)) scheduler.RunResult {
			close(runnerReady)
			<-continueRun
			spec := service.PipelineRunSpec{
				RunID:             req.RunID,
				PipelineID:        req.PipelineID,
				Scheduled:         req.Scheduled,
				SnapshotVersionID: req.SnapshotVersionID,
			}
			cleanup, resolveErr := materializeExactRunSnapshot(
				ctx, snapshotStore, "pipeline-a", "", req.Scheduled, &spec, onLog,
			)
			if resolveErr != nil {
				return scheduler.RunResult{Status: "error", Error: resolveErr.Error()}
			}
			defer cleanup()
			executionCalls.Add(1)
			return scheduler.RunResult{Status: "ok"}
		},
	})
	require.NoError(t, schedulerService.Start(ctx))
	defer schedulerService.Stop()
	runnerReleased := false
	defer func() {
		if !runnerReleased {
			close(continueRun)
		}
	}()

	run, err := schedulerService.Trigger(ctx, scheduler.PipelineSchedule{
		PipelineID: "pipeline-id", PipelineName: "analytics",
	}, scheduler.TriggerRequest{
		Source: scheduler.RunSourceSnapshot, SnapshotVersionID: deployed.VersionID,
	})
	require.NoError(t, err)

	select {
	case <-runnerReady:
	case <-time.After(2 * time.Second):
		t.Fatal("queued runner did not start")
	}
	hash := deployed.Manifest["pipeline.yml"]
	_, err = schedulerStore.DB().ExecContext(ctx, `UPDATE renart_blobs SET content = ? WHERE hash = ?`, []byte("corrupt"), hash)
	require.NoError(t, err)
	close(continueRun)
	runnerReleased = true

	require.Eventually(t, func() bool {
		stored, _, _, getErr := schedulerService.GetRun(ctx, run.ID)
		return getErr == nil && stored.Status == scheduler.RunStatusFailed
	}, 2*time.Second, 20*time.Millisecond)
	stored, _, _, err := schedulerService.GetRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Contains(t, stored.Error, "blob hash mismatch")
	assert.Zero(t, executionCalls.Load())
	_, hasWatermark, err := schedulerStore.LastInterval(ctx, "pipeline-id")
	require.NoError(t, err)
	assert.False(t, hasWatermark)
}

func deployTriggerTestSnapshot(t *testing.T, pipelineUUID string) (*snapshot.Store, string) {
	t.Helper()
	schedulerStore, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = schedulerStore.Close() })
	store := snapshot.NewStore(schedulerStore.DB())
	pipelineDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(pipelineDir, "pipeline.yml"), []byte("name: source-test\n"), 0o644))
	deployed, _, err := store.Deploy(context.Background(), pipelineUUID, pipelineDir, "test")
	require.NoError(t, err)
	return store, deployed.VersionID
}
