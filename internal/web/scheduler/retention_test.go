package scheduler

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPruneHistoryAppliesAgeAndMinimumFloors(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	createRetainedTestRun(t, store, "old", "pipeline", now.AddDate(-1, 0, 0), true)
	createRetainedTestRun(t, store, "newest", "pipeline", now.AddDate(-1, 0, 1), true)
	createRetainedTestRun(t, store, "other-pipeline", "other", now.AddDate(-1, 0, 0), true)

	result, err := store.PruneHistory(ctx, HistoryRetentionPolicy{
		Runs: RetentionWindow{
			OlderThan:          now.AddDate(0, 0, -180),
			MinimumPerPipeline: 1,
		},
		Logs: RetentionWindow{
			OlderThan:          now.AddDate(0, 0, -30),
			MinimumPerPipeline: 1,
		},
		ScheduleHistoryBefore: now.AddDate(0, 0, -180),
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, result.Runs)
	assert.EqualValues(t, 1, result.RunLogs)
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM pipeline_runs WHERE id = 'old'`))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_runs WHERE id = 'newest'`))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_runs WHERE id = 'other-pipeline'`))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_logs WHERE run_id = 'newest'`))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_logs WHERE run_id = 'other-pipeline'`))
}

func TestPruneHistoryProtectsActiveRecoveryAndPendingCompletionState(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(-1, 0, 0)

	createRetainedTestRun(t, store, "deletable", "pipeline-a", old, true)
	createRetainedTestRun(t, store, "pending-completion", "pipeline-b", old, true)
	createRetainedTestRun(t, store, "recovery", "pipeline-c", old, true)
	_, err = store.db.ExecContext(ctx, `
		UPDATE pipeline_runs SET recovery_pending = 1 WHERE id = 'recovery'`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO renart_completion_outbox (completion_id, version, body, enqueued_at)
		VALUES ('pending-completion-id', 1, ?, ?)`,
		`{"version":1,"event":{"completion_id":"pending-completion-id","run_id":"pending-completion"}}`,
		formatTime(old),
	)
	require.NoError(t, err)

	result, err := store.PruneHistory(ctx, HistoryRetentionPolicy{
		Runs:                  RetentionWindow{OlderThan: now.AddDate(0, 0, -1)},
		Logs:                  RetentionWindow{OlderThan: now.AddDate(0, 0, -1)},
		ScheduleHistoryBefore: now.AddDate(0, 0, -1),
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, result.Runs)
	assert.EqualValues(t, 1, result.RunLogs)
	for _, id := range []string{"pending-completion", "recovery"} {
		assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_runs WHERE id = ?`, id))
		assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_logs WHERE run_id = ?`, id))
	}
}

func TestPruneHistoryRemovesTerminalScheduleHistoryAndArchivedTombstones(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	old := formatTime(now.AddDate(-1, 0, 0))
	intervalEnd := now.AddDate(-1, 0, 0)
	intervalStart := intervalEnd.Add(-time.Hour)
	for index, status := range []ScheduleOccurrenceStatus{ScheduleOccurrenceSuccess, ScheduleOccurrencePending} {
		key := fmt.Sprintf("%064x", index+1)
		start := intervalStart.Add(time.Duration(index) * time.Hour)
		end := intervalEnd.Add(time.Duration(index) * time.Hour)
		_, err = store.db.ExecContext(ctx, `
			INSERT INTO schedule_occurrences (
				occurrence_key, pipeline_uuid, environment, interval_start, interval_end,
				status, attempt_count, created_at, updated_at
			) VALUES (?, 'pipeline', 'prod', ?, ?, ?, 0, ?, ?)`,
			key, formatTime(start), formatTime(end), string(status), old, old,
		)
		require.NoError(t, err)
	}
	require.NoError(t, store.UpsertEnvSchedule(ctx, EnvSchedule{
		PipelineUUID: "pipeline", Environment: "prod", SnapshotVersionID: "snapshot",
		Cron: "@daily", Timezone: "UTC", CatchupPolicy: CatchupSkip,
		Status: ScheduleStatusArchived, ArchivedReason: ArchivedReasonDeclarationMissing,
	}))
	_, err = store.db.ExecContext(ctx, `
		UPDATE renart_schedules SET updated_at = ? WHERE pipeline_id = 'pipeline'`, old)
	require.NoError(t, err)

	result, err := store.PruneHistory(ctx, HistoryRetentionPolicy{
		Runs:                  RetentionWindow{OlderThan: now.AddDate(0, 0, -180)},
		Logs:                  RetentionWindow{OlderThan: now.AddDate(0, 0, -30)},
		ScheduleHistoryBefore: now.AddDate(0, 0, -180),
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, result.ScheduleOccurrences)
	assert.EqualValues(t, 1, result.ArchivedSchedules)
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM schedule_occurrences`))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM schedule_occurrences WHERE status = 'pending'`))
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM renart_schedules`))
}

func TestPruneHistoryDropsLatestAttemptProjectionWithDeletedRun(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	createRetainedTestRun(t, store, "old", "pipeline", now.AddDate(-1, 0, 0), false)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO renart_asset_runs (asset_id, environment, fingerprint, status, run_id, ran_at)
		VALUES ('pipeline:asset', 'prod', 'fingerprint', 'success', 'old', ?)`,
		formatTime(now.AddDate(-1, 0, 0)),
	)
	require.NoError(t, err)

	_, err = store.PruneHistory(ctx, HistoryRetentionPolicy{
		Runs:                  RetentionWindow{OlderThan: now.AddDate(0, 0, -1)},
		Logs:                  RetentionWindow{OlderThan: now.AddDate(0, 0, -1)},
		ScheduleHistoryBefore: now.AddDate(0, 0, -1),
	})
	require.NoError(t, err)
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM renart_asset_runs`))
}

func createRetainedTestRun(
	t *testing.T,
	store *Store,
	id string,
	pipelineID string,
	finishedAt time.Time,
	withLog bool,
) {
	t.Helper()
	startedAt := finishedAt.Add(-time.Minute)
	_, err := store.Create(context.Background(), PipelineRun{
		ID: id, PipelineID: pipelineID, Pipeline: pipelineID, Environment: "prod",
		Trigger: RunTriggerManual, Status: RunStatusSuccess,
		StartedAt: &startedAt, FinishedAt: &finishedAt,
	})
	require.NoError(t, err)
	if withLog {
		require.NoError(t, store.AppendLog(context.Background(), id, LogLine{
			At: finishedAt, Line: "retained output",
		}))
	}
}
