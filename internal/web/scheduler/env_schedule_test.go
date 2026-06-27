package scheduler

import (
	"context"
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
	})
	ctx := context.Background()

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
	assert.Empty(t, migrated.SnapshotVersionID, "migrated rows fall back to latest-snapshot-or-working-tree")

	// The config-only pipeline (schedule string, no explicit enabled row) is
	// migrated as active; the explicitly-disabled one stays out.
	config := byUUID["uuid-config"]
	assert.Equal(t, "@daily", config.Cron)
	assert.Equal(t, ScheduleStatusActive, config.Status)
	_, disabledMigrated := byUUID["uuid-disabled"]
	assert.False(t, disabledMigrated, "explicitly disabled schedule must not migrate")

	// Migration is one-shot: a second call must not duplicate or resurrect.
	require.NoError(t, service.migrateLegacySchedules(ctx))
	rows, err = store.ListEnvSchedules(ctx)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}
