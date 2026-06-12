-- +goose Up
-- +goose StatementBegin
-- Content-addressed file storage shared by all snapshots.
CREATE TABLE IF NOT EXISTS renart_blobs (
    hash    TEXT PRIMARY KEY,
    content BLOB NOT NULL
);

-- Immutable deployed versions of a pipeline's source files. Snapshots store
-- sources, not rendered SQL: rendering depends on per-run env/vars/interval,
-- so the executor renders from snapshot content exactly as it does from the
-- working tree.
CREATE TABLE IF NOT EXISTS renart_snapshots (
    version_id  TEXT PRIMARY KEY,
    pipeline_id TEXT NOT NULL,   -- stable pipeline UUID
    merkle_root TEXT NOT NULL,
    manifest    TEXT NOT NULL,   -- JSON: relpath -> blob hash
    git_sha     TEXT,
    git_dirty   INTEGER,
    created_at  TEXT NOT NULL,
    created_by  TEXT
);

CREATE INDEX IF NOT EXISTS idx_renart_snapshots_pipeline
    ON renart_snapshots (pipeline_id, created_at DESC);

ALTER TABLE pipeline_runs ADD COLUMN snapshot_version_id TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE pipeline_runs DROP COLUMN snapshot_version_id;
DROP TABLE IF EXISTS renart_snapshots;
DROP TABLE IF EXISTS renart_blobs;
-- +goose StatementEnd
