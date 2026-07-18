-- +goose Up
-- +goose StatementBegin
CREATE TABLE pipeline_run_plans (
    run_id TEXT PRIMARY KEY
        REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    body TEXT NOT NULL CHECK (json_valid(body)),
    created_at TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS pipeline_run_plans;
-- +goose StatementEnd
