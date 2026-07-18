package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openEnvTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestEnvScheduleStoreRoundTrip(t *testing.T) {
	store := openEnvTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.UpsertEnvSchedule(ctx, EnvSchedule{
		PipelineUUID:      "uuid-1",
		Environment:       "prod",
		SnapshotVersionID: "snap-1",
		Cron:              "0 * * * *",
		Timezone:          "UTC",
		Vars:              map[string]any{"region": "eu"},
		CatchupPolicy:     CatchupRunOnce,
		Status:            ScheduleStatusActive,
	}))
	require.NoError(t, store.UpsertEnvSchedule(ctx, EnvSchedule{
		PipelineUUID:  "uuid-1",
		Environment:   "staging",
		Cron:          "@daily",
		Timezone:      "UTC",
		CatchupPolicy: CatchupSkip,
		Status:        ScheduleStatusPaused,
	}))

	rows, err := store.ListEnvSchedules(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	prod, found, err := store.GetEnvSchedule(ctx, "uuid-1", "prod")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "snap-1", prod.SnapshotVersionID)
	assert.Equal(t, CatchupRunOnce, prod.CatchupPolicy)
	assert.Equal(t, map[string]any{"region": "eu"}, prod.Vars)

	// Same pipeline, independent environments.
	staging, found, err := store.GetEnvSchedule(ctx, "uuid-1", "staging")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ScheduleStatusPaused, staging.Status)

	require.NoError(t, store.SetEnvScheduleStatus(ctx, "uuid-1", "prod", ScheduleStatusArchived, ArchivedReasonMissing))
	archived, _, err := store.GetEnvSchedule(ctx, "uuid-1", "prod")
	require.NoError(t, err)
	assert.Equal(t, ScheduleStatusArchived, archived.Status)
	assert.Equal(t, ArchivedReasonMissing, archived.ArchivedReason)

	next := time.Now().UTC().Add(time.Hour)
	require.NoError(t, store.SetEnvScheduleNextRun(ctx, "uuid-1", "staging", &next))
	staging, _, err = store.GetEnvSchedule(ctx, "uuid-1", "staging")
	require.NoError(t, err)
	require.NotNil(t, staging.NextRunAt)
	assert.WithinDuration(t, next, *staging.NextRunAt, time.Second)
}

func TestReconcileContainsPerRowStoreFailures(t *testing.T) {
	store := openEnvTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	for _, row := range []EnvSchedule{
		{
			PipelineUUID:      "a-missing",
			Environment:       "prod",
			SnapshotVersionID: "snap-missing",
			Cron:              "@hourly",
			Timezone:          "UTC",
			CatchupPolicy:     CatchupSkip,
			Status:            ScheduleStatusActive,
		},
		{
			PipelineUUID:      "b-healthy",
			Environment:       "prod",
			SnapshotVersionID: "snap-healthy",
			Cron:              "@hourly",
			Timezone:          "UTC",
			CatchupPolicy:     CatchupSkip,
			Status:            ScheduleStatusActive,
		},
	} {
		require.NoError(t, store.UpsertEnvSchedule(ctx, row))
	}
	_, err := store.db.ExecContext(ctx, `
		CREATE TRIGGER fail_missing_schedule_archive
		BEFORE UPDATE OF status ON renart_schedules
		WHEN OLD.pipeline_id = 'a-missing'
		BEGIN
			SELECT RAISE(ABORT, 'injected per-row schedule failure');
		END`)
	require.NoError(t, err)

	service := New(Options{
		Store:    store,
		StateDir: t.TempDir(),
		Runner:   func(context.Context, RunRequest, func(string)) RunResult { return RunResult{} },
		ResolvePipelineRef: func(_ context.Context, uuid string) (PipelineRef, bool) {
			if uuid == "b-healthy" {
				return PipelineRef{EncodedID: "healthy", Name: "healthy"}, true
			}
			return PipelineRef{}, false
		},
		CheckSnapshot: func(context.Context, string, string) error { return nil },
	})
	require.NoError(t, service.Start(ctx), "one broken row must not make the scheduler unavailable")
	t.Cleanup(service.Stop)
	assert.Equal(t, SchedulerOwnershipOwner, service.Ownership().State)

	missing, found, err := store.GetEnvSchedule(ctx, "a-missing", "prod")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ScheduleStatusActive, missing.Status, "the failed mutation remains retryable")

	healthy, found, err := store.GetEnvSchedule(ctx, "b-healthy", "prod")
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, healthy.NextRunAt, "later rows must still reconcile")
}

func TestUpsertEnvScheduleValidation(t *testing.T) {
	store := openEnvTestStore(t)
	service := New(Options{
		Store:    store,
		StateDir: t.TempDir(),
		Runner:   func(ctx context.Context, req RunRequest, onLog func(string)) RunResult { return RunResult{} },
		ResolvePipelineRef: func(ctx context.Context, uuid string) (PipelineRef, bool) {
			return PipelineRef{EncodedID: "enc", Name: "analytics"}, true
		},
		PipelineIntervalAware: func(ctx context.Context, uuid string) bool { return false },
		DeployPipeline:        func(ctx context.Context, uuid string) (string, error) { return "snap-new", nil },
		ValidateSnapshot: func(_ context.Context, pipelineUUID, versionID string) error {
			if pipelineUUID != "uuid-1" || (versionID != "snap-new" && versionID != "snap-existing") {
				return errors.New("snapshot does not belong to pipeline")
			}
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, service.Start(ctx))
	t.Cleanup(func() {
		cancel()
		service.Stop()
	})

	_, err := service.UpsertEnvSchedule(ctx, "uuid-1", UpsertEnvScheduleRequest{Cron: "@hourly"})
	require.ErrorContains(t, err, "environment is required")

	_, err = service.UpsertEnvSchedule(ctx, "uuid-1", UpsertEnvScheduleRequest{Environment: "prod", Cron: "not a cron"})
	require.ErrorContains(t, err, "invalid cron")

	_, err = service.UpsertEnvSchedule(ctx, "uuid-1", UpsertEnvScheduleRequest{Environment: "prod", Cron: "@hourly", CatchupPolicy: CatchupBackfill})
	require.ErrorContains(t, err, "interval-aware")

	_, err = service.UpsertEnvSchedule(ctx, "uuid-1", UpsertEnvScheduleRequest{Environment: "prod", Cron: "@hourly"})
	require.ErrorContains(t, err, "deployed snapshot is required")

	created, err := service.UpsertEnvSchedule(ctx, "uuid-1", UpsertEnvScheduleRequest{Environment: "prod", Cron: "@hourly", DeployNow: true})
	require.NoError(t, err)
	assert.Equal(t, "snap-new", created.SnapshotVersionID)
	assert.Equal(t, ScheduleStatusActive, created.Status)
	assert.Equal(t, "enc", created.PipelineID)

	// Updating an existing schedule keeps the pinned snapshot without
	// requiring a redeploy.
	updated, err := service.UpsertEnvSchedule(ctx, "uuid-1", UpsertEnvScheduleRequest{Environment: "prod", Cron: "@daily"})
	require.NoError(t, err)
	assert.Equal(t, "snap-new", updated.SnapshotVersionID)
	assert.Equal(t, "@daily", updated.Cron)

	_, err = service.UpsertEnvSchedule(ctx, "uuid-1", UpsertEnvScheduleRequest{
		Environment: "staging", Cron: "@daily", SnapshotVersionID: "wrong",
	})
	require.ErrorContains(t, err, "not executable for this pipeline")

	paused, err := service.UpsertEnvSchedule(ctx, "uuid-1", UpsertEnvScheduleRequest{
		Environment: "variables", Cron: "@daily", SnapshotVersionID: "snap-existing",
		Vars: map[string]any{"region": "eu"}, Paused: true,
	})
	require.NoError(t, err)
	assert.Equal(t, ScheduleStatusPaused, paused.Status)
	err = service.SetEnvScheduleLifecycle(ctx, "uuid-1", "variables", ScheduleStatusActive)
	require.ErrorContains(t, err, "remove the overrides")

	promoted, err := service.PromoteEnvSchedules(ctx, "uuid-1", PromoteEnvSchedulesRequest{
		SnapshotVersionID: "snap-new",
		Schedules: []EnvSchedulePinSelection{
			{Environment: "prod", ExpectedSnapshotVersionID: "snap-new"},
			{Environment: "variables", ExpectedSnapshotVersionID: "snap-existing"},
		},
	})
	require.NoError(t, err)
	require.Len(t, promoted, 2)
	assert.Equal(t, "snap-new", promoted[1].SnapshotVersionID)

	_, err = service.PromoteEnvSchedules(ctx, "uuid-1", PromoteEnvSchedulesRequest{
		SnapshotVersionID: "snap-existing",
		Schedules: []EnvSchedulePinSelection{
			{Environment: "prod", ExpectedSnapshotVersionID: "stale-client-pin"},
			{Environment: "variables", ExpectedSnapshotVersionID: "snap-new"},
		},
	})
	require.ErrorContains(t, err, "changed after deployment review")
	prod, found, loadErr := store.GetEnvSchedule(ctx, "uuid-1", "prod")
	require.NoError(t, loadErr)
	require.True(t, found)
	assert.Equal(t, "snap-new", prod.SnapshotVersionID, "a stale batch must not partially promote")
}

func TestEnvScheduledWorkerRunsWithEnvironmentAndWatermark(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	var capturedRequest RunRequest
	service := New(Options{
		Store:    store,
		StateDir: stateDir,
		Runner: func(ctx context.Context, req RunRequest, onLog func(string)) RunResult {
			capturedRequest = req
			return RunResult{Status: "ok"}
		},
		ResolvePipelineRef: func(ctx context.Context, uuid string) (PipelineRef, bool) {
			if uuid == "uuid-1" {
				return PipelineRef{EncodedID: "encoded-id", Name: "analytics"}, true
			}
			return PipelineRef{}, false
		},
	})

	worker := &pipelineRunWorker{service: service}
	start := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	require.NoError(t, worker.Work(context.Background(), &river.Job[pipelineRunJobArgs]{Args: pipelineRunJobArgs{
		PipelineUUID:      "uuid-1",
		PipelineName:      "analytics",
		Environment:       "prod",
		Trigger:           RunTriggerSchedule,
		Schedule:          "@hourly",
		Timezone:          "UTC",
		Start:             start.Format(time.RFC3339Nano),
		End:               end.Format(time.RFC3339Nano),
		SnapshotVersionID: "snap-7",
	}}))

	assert.Equal(t, "encoded-id", capturedRequest.PipelineID)
	assert.Equal(t, "prod", capturedRequest.Environment)
	assert.Equal(t, "snap-7", capturedRequest.SnapshotVersionID)

	result, err := service.ListRuns(context.Background(), RunFilter{PipelineID: "encoded-id"})
	require.NoError(t, err)
	require.Len(t, result.Runs, 1)
	run := result.Runs[0]
	assert.Equal(t, "prod", run.Environment)
	assert.Equal(t, "snap-7", run.SnapshotVersionID)

	// Watermark is keyed by (pipeline UUID, environment), so run history
	// and progress survive directory moves and stay per-environment.
	watermark, ok, err := store.LastInterval(context.Background(), "uuid-1|prod")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, end, watermark)

	// A vanished pipeline skips silently instead of failing the job.
	require.NoError(t, worker.Work(context.Background(), &river.Job[pipelineRunJobArgs]{Args: pipelineRunJobArgs{
		PipelineUUID: "gone",
		Environment:  "prod",
		Trigger:      RunTriggerSchedule,
	}}))
}

func TestEnvScheduledWorkerFailureDoesNotAdvanceWatermark(t *testing.T) {
	t.Parallel()
	store := openEnvTestStore(t)
	ctx := context.Background()
	service := New(Options{
		Store:    store,
		StateDir: t.TempDir(),
		Runner: func(_ context.Context, req RunRequest, _ func(string)) RunResult {
			require.True(t, req.Scheduled)
			require.Equal(t, "corrupt-snapshot", req.SnapshotVersionID)
			return RunResult{Status: "error", Error: "deployment blob hash mismatch"}
		},
		ResolvePipelineRef: func(context.Context, string) (PipelineRef, bool) {
			return PipelineRef{EncodedID: "encoded-id", Name: "analytics"}, true
		},
	})

	start := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	worker := &pipelineRunWorker{service: service}
	require.NoError(t, worker.Work(ctx, &river.Job[pipelineRunJobArgs]{Args: pipelineRunJobArgs{
		PipelineUUID:      "uuid-1",
		PipelineName:      "analytics",
		Environment:       "prod",
		Trigger:           RunTriggerSchedule,
		Start:             start.Format(time.RFC3339Nano),
		End:               end.Format(time.RFC3339Nano),
		SnapshotVersionID: "corrupt-snapshot",
	}}))

	runs, err := service.ListRuns(ctx, RunFilter{PipelineID: "encoded-id"})
	require.NoError(t, err)
	require.Len(t, runs.Runs, 1)
	assert.Equal(t, RunStatusFailed, runs.Runs[0].Status)
	assert.Contains(t, runs.Runs[0].Error, "blob hash mismatch")
	_, hasWatermark, err := store.LastInterval(ctx, "uuid-1|prod")
	require.NoError(t, err)
	assert.False(t, hasWatermark)
}

func TestMigrateLegacySchedules(t *testing.T) {
	store := openEnvTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.SetScheduleEnabled(ctx, "encoded-enabled", true))
	require.NoError(t, store.SetScheduleEnabled(ctx, "encoded-disabled", false))

	service := New(Options{
		Store:    store,
		StateDir: t.TempDir(),
		Runner:   func(ctx context.Context, req RunRequest, onLog func(string)) RunResult { return RunResult{} },
		Pipelines: func(ctx context.Context) ([]PipelineSchedule, error) {
			return []PipelineSchedule{
				{PipelineID: "encoded-enabled", PipelineUUID: "uuid-enabled", PipelineName: "a", Schedule: "@hourly", Timezone: "UTC", Catchup: true},
				{PipelineID: "encoded-disabled", PipelineUUID: "uuid-disabled", PipelineName: "b", Schedule: "@daily", Timezone: "UTC"},
				{PipelineID: "encoded-unscheduled", PipelineUUID: "uuid-unscheduled", PipelineName: "c"},
				// No explicit schedule_enabled row: a config-defined schedule is
				// enabled by default (the legacy Enabled = schedule != "" rule).
				{PipelineID: "encoded-config", PipelineUUID: "uuid-config", PipelineName: "d", Schedule: "@daily", Timezone: "UTC"},
			}, nil
		},
		DefaultEnvironment: func() string { return "dev" },
		LatestSnapshot: func(_ context.Context, pipelineUUID string) (string, bool, error) {
			if pipelineUUID == "uuid-enabled" {
				return "snap-enabled", true, nil
			}
			return "", false, nil
		},
		ValidateSnapshot: func(_ context.Context, pipelineUUID, versionID string) error {
			if pipelineUUID == "uuid-enabled" && versionID == "snap-enabled" {
				return nil
			}
			return errors.New("unexpected snapshot")
		},
	})

	require.NoError(t, service.migrateLegacySchedules(ctx))

	rows, err := store.ListEnvSchedules(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	byUUID := make(map[string]EnvSchedule, len(rows))
	for _, row := range rows {
		byUUID[row.PipelineUUID] = row
	}

	migrated := byUUID["uuid-enabled"]
	assert.Equal(t, "dev", migrated.Environment)
	assert.Equal(t, "@hourly", migrated.Cron)
	assert.Equal(t, CatchupRunOnce, migrated.CatchupPolicy)
	assert.Equal(t, ScheduleStatusActive, migrated.Status)
	assert.Equal(t, "snap-enabled", migrated.SnapshotVersionID)

	// The config-only pipeline has no deployment, so it is retained but paused
	// for review; the explicitly-disabled one stays out.
	config := byUUID["uuid-config"]
	assert.Equal(t, "@daily", config.Cron)
	assert.Equal(t, ScheduleStatusPaused, config.Status)
	assert.Empty(t, config.SnapshotVersionID)
	_, disabledMigrated := byUUID["uuid-disabled"]
	assert.False(t, disabledMigrated, "explicitly disabled schedule must not migrate")

	// Migration is one-shot: a second call must not duplicate or resurrect.
	require.NoError(t, service.migrateLegacySchedules(ctx))
	rows, err = store.ListEnvSchedules(ctx)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}
