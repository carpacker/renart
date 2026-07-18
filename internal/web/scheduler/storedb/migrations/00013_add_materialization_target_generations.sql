-- +goose Up
-- +goose StatementBegin
-- Legacy facts cannot be assigned a physical target safely after the fact.
-- Keep them explicitly untrusted at target='' / generation=0 until the asset is
-- materialized again with a target captured before execution.
ALTER TABLE renart_materializations
    ADD COLUMN target_identity TEXT NOT NULL DEFAULT '';
ALTER TABLE renart_materializations
    ADD COLUMN target_generation INTEGER NOT NULL DEFAULT 0
        CHECK (target_generation >= 0);
ALTER TABLE renart_materializations
    ADD COLUMN completion_id TEXT NOT NULL DEFAULT '';
ALTER TABLE renart_materializations
    ADD COLUMN completion_ordinal INTEGER NOT NULL DEFAULT 0
        CHECK (completion_ordinal >= 0);

-- SQLite cannot add columns to an existing primary key. Rebuild coverage so
-- generations for the same source/variable variant remain separate and an
-- A -> B -> A sequence cannot resurrect A's old physical coverage.
DROP INDEX IF EXISTS idx_renart_coverage_selection;
ALTER TABLE renart_coverage RENAME TO renart_coverage_legacy;

CREATE TABLE renart_coverage (
    asset_id          TEXT NOT NULL,
    environment       TEXT NOT NULL,
    fingerprint       TEXT NOT NULL,
    vars_hash         TEXT NOT NULL,
    target_identity   TEXT NOT NULL DEFAULT '',
    target_generation INTEGER NOT NULL DEFAULT 0
        CHECK (target_generation >= 0),
    interval_start    TEXT NOT NULL DEFAULT '',
    interval_end      TEXT NOT NULL DEFAULT '',
    materialized_at   TEXT NOT NULL,
    own_content       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (
        asset_id,
        environment,
        fingerprint,
        vars_hash,
        target_identity,
        target_generation,
        interval_start
    )
);

INSERT INTO renart_coverage (
    asset_id,
    environment,
    fingerprint,
    vars_hash,
    target_identity,
    target_generation,
    interval_start,
    interval_end,
    materialized_at,
    own_content
)
SELECT
    asset_id,
    environment,
    fingerprint,
    vars_hash,
    '',
    0,
    interval_start,
    interval_end,
    materialized_at,
    own_content
FROM renart_coverage_legacy;

DROP TABLE renart_coverage_legacy;

CREATE INDEX idx_renart_coverage_selection ON renart_coverage
    (environment, vars_hash, asset_id);
CREATE INDEX idx_renart_coverage_target ON renart_coverage
    (target_identity, target_generation, asset_id, environment, vars_hash);

-- One durable row names the successful writer whose contents are currently
-- present at a physical target. It is global by target, not scoped to an asset
-- or environment, because all writers of one mutable object compete.
CREATE TABLE renart_latest_successful_writers (
    target_identity   TEXT PRIMARY KEY
        CHECK (target_identity <> ''),
    target_generation INTEGER NOT NULL
        CHECK (target_generation > 0),
    asset_id          TEXT NOT NULL,
    environment       TEXT NOT NULL,
    fingerprint       TEXT NOT NULL,
    vars_hash         TEXT NOT NULL,
    run_id            TEXT NOT NULL DEFAULT '',
    materialized_at   TEXT NOT NULL,
    completion_id     TEXT NOT NULL
        CHECK (completion_id <> ''),
    completion_ordinal INTEGER NOT NULL
        CHECK (completion_ordinal >= 0),
    ambiguous         INTEGER NOT NULL DEFAULT 0
        CHECK (ambiguous IN (0, 1))
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS renart_latest_successful_writers;
DROP INDEX IF EXISTS idx_renart_coverage_target;
DROP INDEX IF EXISTS idx_renart_coverage_selection;

ALTER TABLE renart_coverage RENAME TO renart_coverage_targeted;
CREATE TABLE renart_coverage (
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

-- Only generation-zero rows predate target-aware recording. Discarding newer
-- target-aware coverage on downgrade is safer than merging competing physical
-- targets into the legacy key.
INSERT INTO renart_coverage (
    asset_id,
    environment,
    fingerprint,
    vars_hash,
    interval_start,
    interval_end,
    materialized_at,
    own_content
)
SELECT
    asset_id,
    environment,
    fingerprint,
    vars_hash,
    interval_start,
    interval_end,
    materialized_at,
    own_content
FROM renart_coverage_targeted
WHERE target_identity = '' AND target_generation = 0;

DROP TABLE renart_coverage_targeted;
CREATE INDEX idx_renart_coverage_selection ON renart_coverage
    (environment, vars_hash, asset_id);

-- Target-aware facts are equally unsafe once their target/generation columns
-- disappear: the legacy latest-fingerprint query would otherwise treat a
-- displaced physical writer as current after downgrade.
DELETE FROM renart_materializations
WHERE target_identity <> '' OR target_generation <> 0;

ALTER TABLE renart_materializations DROP COLUMN target_generation;
ALTER TABLE renart_materializations DROP COLUMN target_identity;
ALTER TABLE renart_materializations DROP COLUMN completion_ordinal;
ALTER TABLE renart_materializations DROP COLUMN completion_id;
-- +goose StatementEnd
