-- +goose Up
-- +goose StatementBegin
-- Resource-aware admission also coordinates writes to warehouse relations.
-- Migration 00023 predated that resource kind and rejected otherwise valid
-- reviewed plans at admission time.
ALTER TABLE pipeline_run_resource_claims
    RENAME TO pipeline_run_resource_claims_legacy;

CREATE TABLE pipeline_run_resource_claims (
    claim_key TEXT PRIMARY KEY,
    run_id TEXT NOT NULL
        REFERENCES pipeline_run_claim_sets(run_id) ON DELETE CASCADE,
    kind TEXT NOT NULL
        CHECK (kind IN ('local_file', 'duckdb_database', 'warehouse_relation')),
    identity TEXT NOT NULL
        CHECK (length(identity) = 64 AND identity = lower(identity)),
    UNIQUE (run_id, kind, identity)
);

INSERT INTO pipeline_run_resource_claims (claim_key, run_id, kind, identity)
SELECT claim_key, run_id, kind, identity
FROM pipeline_run_resource_claims_legacy;

DROP TABLE pipeline_run_resource_claims_legacy;

CREATE INDEX idx_pipeline_run_resource_claims_run
    ON pipeline_run_resource_claims (run_id, kind, identity);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE pipeline_run_resource_claims
    RENAME TO pipeline_run_resource_claims_with_warehouse;

CREATE TABLE pipeline_run_resource_claims (
    claim_key TEXT PRIMARY KEY,
    run_id TEXT NOT NULL
        REFERENCES pipeline_run_claim_sets(run_id) ON DELETE CASCADE,
    kind TEXT NOT NULL
        CHECK (kind IN ('local_file', 'duckdb_database')),
    identity TEXT NOT NULL
        CHECK (length(identity) = 64 AND identity = lower(identity)),
    UNIQUE (run_id, kind, identity)
);

INSERT INTO pipeline_run_resource_claims (claim_key, run_id, kind, identity)
SELECT claim_key, run_id, kind, identity
FROM pipeline_run_resource_claims_with_warehouse;

DROP TABLE pipeline_run_resource_claims_with_warehouse;

CREATE INDEX idx_pipeline_run_resource_claims_run
    ON pipeline_run_resource_claims (run_id, kind, identity);
-- +goose StatementEnd
