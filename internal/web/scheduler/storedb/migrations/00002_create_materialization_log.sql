-- +goose Up
-- +goose StatementBegin
-- Immutable per-run facts: one row per (asset, run). Pruned after a
-- retention window; renart_coverage is the durable summary.
CREATE TABLE IF NOT EXISTS renart_materializations (
    id              INTEGER PRIMARY KEY,
    asset_id        TEXT NOT NULL,
    environment     TEXT NOT NULL,
    fingerprint     TEXT NOT NULL,
    vars_hash       TEXT NOT NULL,
    interval_start  TEXT NOT NULL DEFAULT '',  -- '' for full-refresh assets
    interval_end    TEXT NOT NULL DEFAULT '',
    run_id          TEXT NOT NULL,
    materialized_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_renart_mat_lookup ON renart_materializations
    (asset_id, environment, fingerprint, vars_hash);
CREATE INDEX IF NOT EXISTS idx_renart_mat_age ON renart_materializations
    (materialized_at);

-- Compacted coverage: merged intervals per (asset, env, fingerprint, vars).
-- interval_start = '' is the "built" marker for full-refresh assets.
-- ('' instead of NULL because SQLite treats NULLs in a primary key as
-- distinct, which would break the upsert.)
CREATE TABLE IF NOT EXISTS renart_coverage (
    asset_id        TEXT NOT NULL,
    environment     TEXT NOT NULL,
    fingerprint     TEXT NOT NULL,
    vars_hash       TEXT NOT NULL,
    interval_start  TEXT NOT NULL DEFAULT '',
    interval_end    TEXT NOT NULL DEFAULT '',
    materialized_at TEXT NOT NULL,
    PRIMARY KEY (asset_id, environment, fingerprint, vars_hash, interval_start)
);

CREATE INDEX IF NOT EXISTS idx_renart_coverage_selection ON renart_coverage
    (environment, vars_hash, asset_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS renart_coverage;
DROP TABLE IF EXISTS renart_materializations;
-- +goose StatementEnd
