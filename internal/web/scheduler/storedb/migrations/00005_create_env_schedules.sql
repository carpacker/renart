-- +goose Up
-- +goose StatementBegin
-- Schedule identity is (pipeline UUID, environment): the same pipeline can
-- run hourly in prod and daily in staging, independently toggleable.
-- status: active | paused | archived | delegated (reserved for cloud).
-- snapshot_version_id pins the deployed code a schedule executes; '' falls
-- back to the latest snapshot at run time (legacy-migrated rows).
CREATE TABLE IF NOT EXISTS renart_schedules (
    pipeline_id         TEXT NOT NULL,
    environment         TEXT NOT NULL,
    snapshot_version_id TEXT NOT NULL DEFAULT '',
    cron                TEXT NOT NULL,
    timezone            TEXT NOT NULL DEFAULT 'UTC',
    vars                TEXT,                            -- JSON, env-specific overrides
    catchup_policy      TEXT NOT NULL DEFAULT 'skip',    -- skip | run_once | backfill
    status              TEXT NOT NULL DEFAULT 'active',  -- active | paused | archived | delegated
    -- archived_reason: 'missing' (reconciler tombstone, auto-restores when
    -- the pipeline file reappears, e.g. after a branch switch) or 'user'.
    archived_reason     TEXT NOT NULL DEFAULT '',
    next_run_at         TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    PRIMARY KEY (pipeline_id, environment)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS renart_schedules;
-- +goose StatementEnd
