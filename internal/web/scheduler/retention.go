package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RetentionWindow makes a terminal run eligible only when it is older than
// OlderThan and outside the newest MinimumPerPipeline rows for its stable
// pipeline identity.
type RetentionWindow struct {
	OlderThan          time.Time
	MinimumPerPipeline int
}

type HistoryRetentionPolicy struct {
	Runs                  RetentionWindow
	Logs                  RetentionWindow
	ScheduleHistoryBefore time.Time
}

type HistoryPruneResult struct {
	RunLogs             int64
	Runs                int64
	ScheduleOccurrences int64
	ArchivedSchedules   int64
}

const rankedRunsCTE = `
	WITH ranked_runs AS (
		SELECT runs.id,
		       runs.status,
		       runs.recovery_pending,
		       runs.river_job_id,
		       COALESCE(NULLIF(runs.finished_at, ''), NULLIF(runs.started_at, ''), '') AS retained_at,
		       ROW_NUMBER() OVER (
			   PARTITION BY COALESCE(
			       NULLIF(json_extract(spec.body, '$.pipeline.uuid'), ''),
			       runs.pipeline_id
			   )
			   ORDER BY COALESCE(NULLIF(runs.finished_at, ''), NULLIF(runs.started_at, ''), '') DESC,
			            runs.id DESC
		       ) AS retention_rank
		FROM pipeline_runs AS runs
		LEFT JOIN pipeline_run_specs AS spec ON spec.run_id = runs.id
	),
	retention_candidates AS (
		SELECT ranked.id
		FROM ranked_runs AS ranked
		WHERE ranked.status IN ('success', 'failed', 'cancelled')
		  AND ranked.recovery_pending = 0
		  AND ranked.retained_at <> ''
		  AND ranked.retained_at < ?
		  AND ranked.retention_rank > ?
		  AND NOT EXISTS (
			  SELECT 1
			  FROM renart_completion_outbox AS outbox
			  WHERE json_extract(outbox.body, '$.event.run_id') = ranked.id
		  )
		  AND NOT EXISTS (
			  SELECT 1
			  FROM river_job AS job
			  WHERE job.id = ranked.river_job_id
			    AND job.state NOT IN ('completed', 'cancelled', 'discarded')
		  )
	)`

// PruneHistory removes bounded local operational history in one transaction.
// Active/recovering work, pending completion evidence, and live River jobs are
// structural exclusions rather than assumptions made by the caller.
func (s *Store) PruneHistory(ctx context.Context, policy HistoryRetentionPolicy) (HistoryPruneResult, error) {
	if s == nil || s.db == nil {
		return HistoryPruneResult{}, errors.New("scheduler store is not initialized")
	}
	if err := validateRetentionWindow("run", policy.Runs); err != nil {
		return HistoryPruneResult{}, err
	}
	if err := validateRetentionWindow("log", policy.Logs); err != nil {
		return HistoryPruneResult{}, err
	}
	if policy.ScheduleHistoryBefore.IsZero() {
		return HistoryPruneResult{}, errors.New("schedule history cutoff is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return HistoryPruneResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var result HistoryPruneResult
	logDelete, err := tx.ExecContext(
		ctx,
		rankedRunsCTE+`
		DELETE FROM pipeline_run_logs
		WHERE run_id IN (SELECT id FROM retention_candidates)`,
		formatTime(policy.Logs.OlderThan),
		policy.Logs.MinimumPerPipeline,
	)
	if err != nil {
		return HistoryPruneResult{}, fmt.Errorf("prune pipeline run logs: %w", err)
	}
	if result.RunLogs, err = logDelete.RowsAffected(); err != nil {
		return HistoryPruneResult{}, fmt.Errorf("count pruned pipeline run logs: %w", err)
	}

	// The latest-attempt projection is intentionally not correctness evidence.
	// Drop a row that points at metadata being removed so the UI never links a
	// retained "last run" badge to a missing run.
	if _, err := tx.ExecContext(
		ctx,
		rankedRunsCTE+`
		DELETE FROM renart_asset_runs
		WHERE run_id IN (SELECT id FROM retention_candidates)`,
		formatTime(policy.Runs.OlderThan),
		policy.Runs.MinimumPerPipeline,
	); err != nil {
		return HistoryPruneResult{}, fmt.Errorf("prune latest asset-run projections: %w", err)
	}

	runDelete, err := tx.ExecContext(
		ctx,
		rankedRunsCTE+`
		DELETE FROM pipeline_runs
		WHERE id IN (SELECT id FROM retention_candidates)`,
		formatTime(policy.Runs.OlderThan),
		policy.Runs.MinimumPerPipeline,
	)
	if err != nil {
		return HistoryPruneResult{}, fmt.Errorf("prune pipeline runs: %w", err)
	}
	if result.Runs, err = runDelete.RowsAffected(); err != nil {
		return HistoryPruneResult{}, fmt.Errorf("count pruned pipeline runs: %w", err)
	}

	occurrenceDelete, err := tx.ExecContext(ctx, `
		DELETE FROM schedule_occurrences
		WHERE status IN ('success', 'failed', 'cancelled')
		  AND updated_at < ?`, formatTime(policy.ScheduleHistoryBefore))
	if err != nil {
		return HistoryPruneResult{}, fmt.Errorf("prune schedule occurrences: %w", err)
	}
	if result.ScheduleOccurrences, err = occurrenceDelete.RowsAffected(); err != nil {
		return HistoryPruneResult{}, fmt.Errorf("count pruned schedule occurrences: %w", err)
	}

	// Archived rows are tombstones, not runnable pins. Keeping them for the
	// schedule-history window supports restore and audit; removing them after
	// that window also releases their deployment reference for snapshot GC.
	archivedDelete, err := tx.ExecContext(ctx, `
		DELETE FROM renart_schedules
		WHERE status = 'archived'
		  AND updated_at < ?`, formatTime(policy.ScheduleHistoryBefore))
	if err != nil {
		return HistoryPruneResult{}, fmt.Errorf("prune archived schedules: %w", err)
	}
	if result.ArchivedSchedules, err = archivedDelete.RowsAffected(); err != nil {
		return HistoryPruneResult{}, fmt.Errorf("count pruned archived schedules: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return HistoryPruneResult{}, fmt.Errorf("commit history retention: %w", err)
	}
	return result, nil
}

func validateRetentionWindow(name string, window RetentionWindow) error {
	if window.OlderThan.IsZero() {
		return fmt.Errorf("%s retention cutoff is required", name)
	}
	if window.MinimumPerPipeline < 0 {
		return fmt.Errorf("%s retention minimum per pipeline cannot be negative", name)
	}
	return nil
}
