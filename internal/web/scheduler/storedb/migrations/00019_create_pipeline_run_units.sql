-- +goose Up
-- +goose StatementBegin
CREATE TABLE pipeline_run_units (
    run_id TEXT NOT NULL
        REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK (position >= 0),
    asset_id TEXT NOT NULL CHECK (asset_id <> ''),
    asset_name TEXT NOT NULL CHECK (asset_name <> ''),
    start_date TEXT NOT NULL,
    end_date TEXT NOT NULL,
    render_index INTEGER NOT NULL CHECK (render_index >= 0),
    reason TEXT NOT NULL CHECK (reason <> ''),
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'success', 'failed', 'cancelled', 'skipped')),
    started_at TEXT,
    finished_at TEXT,
    error TEXT,
    PRIMARY KEY (run_id, position)
);

CREATE INDEX idx_pipeline_run_units_run_status
    ON pipeline_run_units (run_id, status, position);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_pipeline_run_units_run_status;
DROP TABLE IF EXISTS pipeline_run_units;
-- +goose StatementEnd
