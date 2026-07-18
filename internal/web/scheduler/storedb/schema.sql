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
    river_job_id INTEGER,
    full_refresh INTEGER NOT NULL DEFAULT 0,
    backfill INTEGER NOT NULL DEFAULT 0,
    sensor_mode TEXT NOT NULL DEFAULT '',
    execution_context_resolved INTEGER NOT NULL DEFAULT 0,
    execution_target_snapshot TEXT NOT NULL DEFAULT ''
        CHECK (execution_target_snapshot = '' OR json_valid(execution_target_snapshot))
);

CREATE INDEX IF NOT EXISTS idx_runs_pipeline_time ON pipeline_runs (pipeline_id, started_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pipeline_runs_river_job ON pipeline_runs (river_job_id) WHERE river_job_id IS NOT NULL;
-- Private execution contracts are deliberately kept out of pipeline_runs so
-- run-list DTOs and SSE payloads cannot expose authorization or future secret
-- references by accident.
CREATE TABLE IF NOT EXISTS pipeline_run_specs (
    run_id TEXT PRIMARY KEY,
    version INTEGER NOT NULL CHECK (version > 0),
    body TEXT NOT NULL CHECK (json_valid(body)),
    created_at TEXT NOT NULL,
    FOREIGN KEY(run_id) REFERENCES pipeline_runs(id) ON DELETE CASCADE
);

-- Pipeline-scope execution claims namespaced path and stable-UUID aliases. The
-- path alias bridges pre-upgrade active rows; UUID keeps the slot stable across
-- a rename or move.
CREATE TABLE IF NOT EXISTS pipeline_run_slots (
    slot_key TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    FOREIGN KEY(run_id) REFERENCES pipeline_runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_pipeline_run_slots_run ON pipeline_run_slots (run_id);

CREATE TRIGGER IF NOT EXISTS release_pipeline_run_slot
AFTER UPDATE OF status ON pipeline_runs
WHEN OLD.status IN ('queued', 'running')
 AND NEW.status NOT IN ('queued', 'running')
BEGIN
    DELETE FROM pipeline_run_slots WHERE run_id = NEW.id;
END;

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
    completion_ordinal INTEGER
        CHECK (completion_ordinal IS NULL OR completion_ordinal >= 0),
    upstream_writer_snapshot TEXT NOT NULL DEFAULT ''
        CHECK (upstream_writer_snapshot = '' OR json_valid(upstream_writer_snapshot)),
    PRIMARY KEY(run_id, asset),
    FOREIGN KEY(run_id) REFERENCES pipeline_runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_run_steps_run_started ON pipeline_run_steps (run_id, started_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pipeline_run_steps_completion
    ON pipeline_run_steps (run_id, completion_ordinal)
    WHERE completion_ordinal IS NOT NULL;

CREATE TABLE IF NOT EXISTS schedule_watermarks (
    pipeline TEXT PRIMARY KEY,
    up_to TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pipeline_schedule_settings (
    pipeline_id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);

-- Durable hand-off between physical run completion and derived-state
-- consumers. The body is a strict versioned completion envelope; sequence is
-- the deterministic replay order.
CREATE TABLE IF NOT EXISTS renart_completion_outbox (
    sequence      INTEGER PRIMARY KEY AUTOINCREMENT,
    completion_id TEXT NOT NULL UNIQUE
        CHECK (completion_id <> '' AND completion_id = trim(completion_id)),
    version       INTEGER NOT NULL CHECK (version = 1),
    body          TEXT NOT NULL
        CHECK (json_valid(body))
        CHECK (json_type(body) = 'object')
        CHECK (COALESCE(json_extract(body, '$.version') = version, 0))
        CHECK (COALESCE(json_extract(body, '$.event.completion_id') = completion_id, 0)),
    enqueued_at   TEXT NOT NULL CHECK (enqueued_at <> '')
);

-- Materialization log (queried by the matlog package directly, not sqlc;
-- kept here so this file stays the full schema reference).
CREATE TABLE IF NOT EXISTS renart_materializations (
    id                INTEGER PRIMARY KEY,
    asset_id          TEXT NOT NULL,
    environment       TEXT NOT NULL,
    fingerprint       TEXT NOT NULL,
    vars_hash         TEXT NOT NULL,
    interval_start    TEXT NOT NULL DEFAULT '',
    interval_end      TEXT NOT NULL DEFAULT '',
    run_id            TEXT NOT NULL,
    materialized_at   TEXT NOT NULL,
    own_content       TEXT NOT NULL DEFAULT '',
    target_identity   TEXT NOT NULL DEFAULT '',
    target_generation INTEGER NOT NULL DEFAULT 0 CHECK (target_generation >= 0),
    completion_id     TEXT NOT NULL DEFAULT '',
    completion_ordinal INTEGER NOT NULL DEFAULT 0 CHECK (completion_ordinal >= 0)
);

CREATE INDEX IF NOT EXISTS idx_renart_mat_lookup ON renart_materializations
    (asset_id, environment, fingerprint, vars_hash);
CREATE INDEX IF NOT EXISTS idx_renart_mat_age ON renart_materializations
    (materialized_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_renart_mat_run ON renart_materializations
    (asset_id, environment, run_id) WHERE run_id <> '';

CREATE TABLE IF NOT EXISTS renart_coverage (
    asset_id          TEXT NOT NULL,
    environment       TEXT NOT NULL,
    fingerprint       TEXT NOT NULL,
    vars_hash         TEXT NOT NULL,
    target_identity   TEXT NOT NULL DEFAULT '',
    target_generation INTEGER NOT NULL DEFAULT 0 CHECK (target_generation >= 0),
    interval_start    TEXT NOT NULL DEFAULT '',
    interval_end      TEXT NOT NULL DEFAULT '',
    materialized_at   TEXT NOT NULL,
    own_content       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (
        asset_id, environment, fingerprint, vars_hash,
        target_identity, target_generation, interval_start
    )
);

CREATE INDEX IF NOT EXISTS idx_renart_coverage_selection ON renart_coverage
    (environment, vars_hash, asset_id);
CREATE INDEX IF NOT EXISTS idx_renart_coverage_target ON renart_coverage
    (target_identity, target_generation, asset_id, environment, vars_hash);

-- Durable identity of the successful writer currently present at each
-- physical target. Raw materialization-fact retention never prunes this row.
CREATE TABLE IF NOT EXISTS renart_latest_successful_writers (
    target_identity    TEXT PRIMARY KEY CHECK (target_identity <> ''),
    target_generation  INTEGER NOT NULL CHECK (target_generation > 0),
    asset_id           TEXT NOT NULL,
    environment        TEXT NOT NULL,
    fingerprint        TEXT NOT NULL,
    vars_hash          TEXT NOT NULL,
    run_id             TEXT NOT NULL DEFAULT '',
    materialized_at    TEXT NOT NULL,
    completion_id      TEXT NOT NULL CHECK (completion_id <> ''),
    completion_ordinal INTEGER NOT NULL CHECK (completion_ordinal >= 0),
    ambiguous          INTEGER NOT NULL DEFAULT 0 CHECK (ambiguous IN (0, 1))
);

-- Durable fail-closed marker spanning physical execution and materialization
-- fact recording. A target with any active or dirty claim is never considered
-- fresh. Successful target-aware recording clears the matching claim and all
-- older dirty claims in the same transaction as the writer update.
CREATE TABLE IF NOT EXISTS renart_target_write_claims (
    claim_sequence  INTEGER PRIMARY KEY AUTOINCREMENT,
    target_identity TEXT NOT NULL CHECK (target_identity <> ''),
    completion_id   TEXT NOT NULL CHECK (completion_id <> ''),
    asset_id        TEXT NOT NULL CHECK (asset_id <> ''),
    state           TEXT NOT NULL CHECK (state IN ('active', 'dirty')),
    claimed_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE (target_identity, completion_id, asset_id)
);

CREATE INDEX IF NOT EXISTS idx_renart_target_write_claims_target_state
    ON renart_target_write_claims (target_identity, state, claim_sequence);

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
