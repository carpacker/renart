CREATE TABLE IF NOT EXISTS pipeline_runs (
    id TEXT PRIMARY KEY,
    pipeline_id TEXT NOT NULL,
    pipeline TEXT NOT NULL,
    environment TEXT NOT NULL,
    trigger TEXT NOT NULL,
    status TEXT NOT NULL,
    win_start TEXT,
    win_end TEXT,
    started_at TEXT,
    finished_at TEXT,
    error TEXT,
    log_ref TEXT,
    snapshot_version_id TEXT,
    recovery_pending INTEGER NOT NULL DEFAULT 0,
    river_job_id INTEGER
);

CREATE INDEX IF NOT EXISTS idx_runs_pipeline_time ON pipeline_runs (pipeline_id, started_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pipeline_runs_river_job ON pipeline_runs (river_job_id) WHERE river_job_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS pipeline_run_logs (
    run_id TEXT NOT NULL,
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    at TEXT NOT NULL,
    line TEXT NOT NULL,
    FOREIGN KEY(run_id) REFERENCES pipeline_runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_run_logs_run_seq ON pipeline_run_logs (run_id, seq);

CREATE TABLE IF NOT EXISTS pipeline_run_steps (
    run_id TEXT NOT NULL,
    asset TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    error TEXT,
    PRIMARY KEY(run_id, asset),
    FOREIGN KEY(run_id) REFERENCES pipeline_runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_run_steps_run_started ON pipeline_run_steps (run_id, started_at);

CREATE TABLE IF NOT EXISTS schedule_watermarks (
    pipeline TEXT PRIMARY KEY,
    up_to TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pipeline_schedule_settings (
    pipeline_id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);

-- Materialization log (queried by the matlog package directly, not sqlc;
-- kept here so this file stays the full schema reference).
CREATE TABLE IF NOT EXISTS renart_materializations (
    id              INTEGER PRIMARY KEY,
    asset_id        TEXT NOT NULL,
    environment     TEXT NOT NULL,
    fingerprint     TEXT NOT NULL,
    vars_hash       TEXT NOT NULL,
    interval_start  TEXT NOT NULL DEFAULT '',
    interval_end    TEXT NOT NULL DEFAULT '',
    run_id          TEXT NOT NULL,
    materialized_at TEXT NOT NULL,
    own_content     TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_renart_mat_lookup ON renart_materializations
    (asset_id, environment, fingerprint, vars_hash);
CREATE INDEX IF NOT EXISTS idx_renart_mat_age ON renart_materializations
    (materialized_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_renart_mat_run ON renart_materializations
    (asset_id, environment, run_id) WHERE run_id <> '';

CREATE TABLE IF NOT EXISTS renart_coverage (
    asset_id        TEXT NOT NULL,
    environment     TEXT NOT NULL,
    fingerprint     TEXT NOT NULL,
    vars_hash       TEXT NOT NULL,
    interval_start  TEXT NOT NULL DEFAULT '',
    interval_end    TEXT NOT NULL DEFAULT '',
    materialized_at TEXT NOT NULL,
    own_content     TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (asset_id, environment, fingerprint, vars_hash, interval_start)
);

CREATE INDEX IF NOT EXISTS idx_renart_coverage_selection ON renart_coverage
    (environment, vars_hash, asset_id);

-- Most recent run attempt per (asset, environment), success or failure;
-- upserted so a later run overwrites the previous outcome.
CREATE TABLE IF NOT EXISTS renart_asset_runs (
    asset_id     TEXT NOT NULL,
    environment  TEXT NOT NULL,
    fingerprint  TEXT NOT NULL,
    status       TEXT NOT NULL,
    run_id       TEXT NOT NULL DEFAULT '',
    ran_at       TEXT NOT NULL,
    PRIMARY KEY (asset_id, environment)
);

-- Snapshot store (queried by the snapshot package directly, not sqlc).
CREATE TABLE IF NOT EXISTS renart_blobs (
    hash    TEXT PRIMARY KEY,
    content BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS renart_snapshots (
    version_id  TEXT PRIMARY KEY,
    pipeline_id TEXT NOT NULL,
    merkle_root TEXT NOT NULL,
    manifest    TEXT NOT NULL,
    git_sha     TEXT,
    git_dirty   INTEGER,
    created_at  TEXT NOT NULL,
    created_by  TEXT
);

CREATE INDEX IF NOT EXISTS idx_renart_snapshots_pipeline
    ON renart_snapshots (pipeline_id, created_at DESC);

CREATE TABLE IF NOT EXISTS renart_schedules (
    pipeline_id         TEXT NOT NULL,
    environment         TEXT NOT NULL,
    snapshot_version_id TEXT NOT NULL DEFAULT '',
    cron                TEXT NOT NULL,
    timezone            TEXT NOT NULL DEFAULT 'UTC',
    vars                TEXT,
    catchup_policy      TEXT NOT NULL DEFAULT 'skip',
    status              TEXT NOT NULL DEFAULT 'active',
    archived_reason     TEXT NOT NULL DEFAULT '',
    next_run_at         TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    PRIMARY KEY (pipeline_id, environment)
);
