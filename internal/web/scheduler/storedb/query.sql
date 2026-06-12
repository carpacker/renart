-- name: CreateRun :exec
INSERT INTO pipeline_runs (id, pipeline_id, pipeline, environment, trigger, status, win_start, win_end, started_at, finished_at, error, log_ref, snapshot_version_id)
VALUES (@id, @pipeline_id, @pipeline, @environment, @trigger, @status, @win_start, @win_end, @started_at, @finished_at, @error, @log_ref, @snapshot_version_id);

-- name: MarkRunRunning :exec
UPDATE pipeline_runs
SET status = @status, started_at = @started_at
WHERE id = @id;

-- name: AppendRunLog :exec
INSERT INTO pipeline_run_logs (run_id, at, line)
VALUES (@run_id, @at, @line);

-- name: FinishRun :exec
UPDATE pipeline_runs
SET status = @status, finished_at = @finished_at, error = @error
WHERE id = @id;

-- name: CountRuns :one
SELECT COUNT(*)
FROM pipeline_runs
WHERE (CAST(@pipeline_id AS TEXT) = '' OR pipeline_id = CAST(@pipeline_id AS TEXT))
  AND (
    CAST(@environment AS TEXT) = ''
    OR (CAST(@environment AS TEXT) = 'default' AND (environment = CAST(@environment AS TEXT) OR environment = ''))
    OR (CAST(@environment AS TEXT) <> 'default' AND environment = CAST(@environment AS TEXT))
  )
  AND (CAST(@status AS TEXT) = '' OR status = CAST(@status AS TEXT))
  AND (
    CAST(@query_like AS TEXT) = ''
    OR LOWER(id) LIKE CAST(@query_like AS TEXT)
    OR LOWER(pipeline) LIKE CAST(@query_like AS TEXT)
    OR LOWER(pipeline_id) LIKE CAST(@query_like AS TEXT)
  );

-- name: ListRuns :many
SELECT id, pipeline_id, pipeline, environment, trigger, status, win_start, win_end, started_at, finished_at, error, log_ref, snapshot_version_id
FROM pipeline_runs
WHERE (CAST(@pipeline_id AS TEXT) = '' OR pipeline_id = CAST(@pipeline_id AS TEXT))
  AND (
    CAST(@environment AS TEXT) = ''
    OR (CAST(@environment AS TEXT) = 'default' AND (environment = CAST(@environment AS TEXT) OR environment = ''))
    OR (CAST(@environment AS TEXT) <> 'default' AND environment = CAST(@environment AS TEXT))
  )
  AND (CAST(@status AS TEXT) = '' OR status = CAST(@status AS TEXT))
  AND (
    CAST(@query_like AS TEXT) = ''
    OR LOWER(id) LIKE CAST(@query_like AS TEXT)
    OR LOWER(pipeline) LIKE CAST(@query_like AS TEXT)
    OR LOWER(pipeline_id) LIKE CAST(@query_like AS TEXT)
  )
ORDER BY COALESCE(started_at, '') DESC, id DESC
LIMIT @limit OFFSET @offset;

-- name: GetRun :one
SELECT id, pipeline_id, pipeline, environment, trigger, status, win_start, win_end, started_at, finished_at, error, log_ref, snapshot_version_id
FROM pipeline_runs
WHERE id = @id;

-- name: SetRunSnapshotVersion :exec
UPDATE pipeline_runs
SET snapshot_version_id = @snapshot_version_id
WHERE id = @id;

-- name: ListRunLogs :many
SELECT at, line
FROM pipeline_run_logs
WHERE run_id = @run_id
ORDER BY seq ASC;

-- name: UpsertRunStep :exec
INSERT INTO pipeline_run_steps (run_id, asset, status, started_at, finished_at, error)
VALUES (@run_id, @asset, @status, @started_at, @finished_at, @error)
ON CONFLICT(run_id, asset) DO UPDATE SET
    status = excluded.status,
    started_at = COALESCE(pipeline_run_steps.started_at, excluded.started_at),
    finished_at = excluded.finished_at,
    error = excluded.error;

-- name: ListRunSteps :many
SELECT run_id, asset, status, started_at, finished_at, error
FROM pipeline_run_steps
WHERE run_id = @run_id
ORDER BY COALESCE(started_at, finished_at, '') ASC, asset ASC;

-- name: FinishOpenRunSteps :exec
UPDATE pipeline_run_steps
SET status = @status,
    finished_at = @finished_at,
    error = CASE WHEN CAST(@error AS TEXT) = '' THEN error ELSE CAST(@error AS TEXT) END
WHERE run_id = @run_id AND finished_at IS NULL;

-- name: CountActiveRuns :one
SELECT COUNT(*)
FROM pipeline_runs
WHERE pipeline_id = @pipeline_id AND status IN (@queued_status, @running_status);

-- name: GetScheduleWatermark :one
SELECT up_to
FROM schedule_watermarks
WHERE pipeline = @pipeline;

-- name: SetScheduleWatermark :exec
INSERT INTO schedule_watermarks (pipeline, up_to)
VALUES (@pipeline, @up_to)
ON CONFLICT(pipeline) DO UPDATE SET up_to = excluded.up_to;

-- name: GetScheduleEnabled :one
SELECT enabled
FROM pipeline_schedule_settings
WHERE pipeline_id = @pipeline_id;

-- name: SetScheduleEnabled :exec
INSERT INTO pipeline_schedule_settings (pipeline_id, enabled, updated_at)
VALUES (@pipeline_id, @enabled, @updated_at)
ON CONFLICT(pipeline_id) DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at;

-- name: UpsertEnvSchedule :exec
INSERT INTO renart_schedules (pipeline_id, environment, snapshot_version_id, cron, timezone, vars, catchup_policy, status, archived_reason, created_at, updated_at)
VALUES (@pipeline_id, @environment, @snapshot_version_id, @cron, @timezone, @vars, @catchup_policy, @status, @archived_reason, @created_at, @updated_at)
ON CONFLICT(pipeline_id, environment) DO UPDATE SET
    snapshot_version_id = excluded.snapshot_version_id,
    cron = excluded.cron,
    timezone = excluded.timezone,
    vars = excluded.vars,
    catchup_policy = excluded.catchup_policy,
    status = excluded.status,
    archived_reason = excluded.archived_reason,
    updated_at = excluded.updated_at;

-- name: ListEnvSchedules :many
SELECT pipeline_id, environment, snapshot_version_id, cron, timezone, vars, catchup_policy, status, archived_reason, next_run_at, created_at, updated_at
FROM renart_schedules
ORDER BY pipeline_id, environment;

-- name: GetEnvSchedule :one
SELECT pipeline_id, environment, snapshot_version_id, cron, timezone, vars, catchup_policy, status, archived_reason, next_run_at, created_at, updated_at
FROM renart_schedules
WHERE pipeline_id = @pipeline_id AND environment = @environment;

-- name: SetEnvScheduleStatus :exec
UPDATE renart_schedules
SET status = @status, archived_reason = @archived_reason, updated_at = @updated_at
WHERE pipeline_id = @pipeline_id AND environment = @environment;

-- name: SetEnvScheduleNextRun :exec
UPDATE renart_schedules
SET next_run_at = @next_run_at
WHERE pipeline_id = @pipeline_id AND environment = @environment;

-- name: CountEnvSchedules :one
SELECT COUNT(*) FROM renart_schedules;
