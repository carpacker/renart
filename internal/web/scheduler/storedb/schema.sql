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
    log_ref TEXT
);

CREATE INDEX IF NOT EXISTS idx_runs_pipeline_time ON pipeline_runs (pipeline_id, started_at DESC);

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
