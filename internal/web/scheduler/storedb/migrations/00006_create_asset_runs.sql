-- +goose Up
-- +goose StatementBegin
-- The most recent run attempt per (asset, environment), success or failure.
-- Materialization facts only record successes, so a failed run left no trace and
-- read as plain "edited"/"never built". This one-row-per-key table (upserted, so
-- a later run overwrites the previous outcome and a success clears a prior
-- failure) lets the staleness service tell apart: an untested edit, an edit that
-- was run and failed, and unchanged code whose last run failed.
CREATE TABLE IF NOT EXISTS renart_asset_runs (
    asset_id     TEXT NOT NULL,
    environment  TEXT NOT NULL,
    fingerprint  TEXT NOT NULL,
    status       TEXT NOT NULL,            -- 'succeeded' | 'failed'
    run_id       TEXT NOT NULL DEFAULT '',
    ran_at       TEXT NOT NULL,
    PRIMARY KEY (asset_id, environment)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS renart_asset_runs;
-- +goose StatementEnd
