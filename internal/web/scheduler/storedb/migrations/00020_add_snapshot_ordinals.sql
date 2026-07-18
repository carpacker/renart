-- +goose Up
-- +goose StatementBegin
-- Deployment UUIDs remain the immutable internal identity. A per-pipeline
-- ordinal gives people one stable, compact identity for reviews, schedule
-- pins, and run provenance. Existing deployments are ordered oldest-first;
-- version_id is the deterministic tie-breaker used by the previous Latest
-- contract when timestamps match.
ALTER TABLE renart_snapshots RENAME TO renart_snapshots_legacy;

CREATE TABLE renart_snapshots (
    version_id  TEXT PRIMARY KEY,
    pipeline_id TEXT NOT NULL,
    ordinal     INTEGER NOT NULL CHECK (ordinal > 0),
    merkle_root TEXT NOT NULL,
    manifest    TEXT NOT NULL,
    git_sha     TEXT,
    git_dirty   INTEGER,
    created_at  TEXT NOT NULL,
    created_by  TEXT
);

INSERT INTO renart_snapshots (
    version_id,
    pipeline_id,
    ordinal,
    merkle_root,
    manifest,
    git_sha,
    git_dirty,
    created_at,
    created_by
)
SELECT
    version_id,
    pipeline_id,
    ROW_NUMBER() OVER (
        PARTITION BY pipeline_id
        ORDER BY created_at ASC, version_id ASC
    ),
    merkle_root,
    manifest,
    git_sha,
    git_dirty,
    created_at,
    created_by
FROM renart_snapshots_legacy;

DROP TABLE renart_snapshots_legacy;

CREATE UNIQUE INDEX idx_renart_snapshots_pipeline_ordinal
    ON renart_snapshots (pipeline_id, ordinal);
CREATE INDEX idx_renart_snapshots_pipeline
    ON renart_snapshots (pipeline_id, ordinal DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE renart_snapshots RENAME TO renart_snapshots_with_ordinals;

CREATE TABLE renart_snapshots (
    version_id  TEXT PRIMARY KEY,
    pipeline_id TEXT NOT NULL,
    merkle_root TEXT NOT NULL,
    manifest    TEXT NOT NULL,
    git_sha     TEXT,
    git_dirty   INTEGER,
    created_at  TEXT NOT NULL,
    created_by  TEXT
);

INSERT INTO renart_snapshots (
    version_id,
    pipeline_id,
    merkle_root,
    manifest,
    git_sha,
    git_dirty,
    created_at,
    created_by
)
SELECT
    version_id,
    pipeline_id,
    merkle_root,
    manifest,
    git_sha,
    git_dirty,
    created_at,
    created_by
FROM renart_snapshots_with_ordinals;

DROP TABLE renart_snapshots_with_ordinals;

CREATE INDEX idx_renart_snapshots_pipeline
    ON renart_snapshots (pipeline_id, created_at DESC);
-- +goose StatementEnd
