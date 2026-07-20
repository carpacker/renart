-- +goose Up
-- +goose StatementBegin
-- A v2 reviewed plan owns either a conservative logical-pipeline claim or a
-- set of exact write resources. The claim-set row also represents a proven
-- no-write run (for example a sensor-only plan), whose resource set is empty.
CREATE TABLE pipeline_run_claim_sets (
    run_id TEXT PRIMARY KEY
        REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    pipeline_id TEXT NOT NULL CHECK (pipeline_id <> ''),
    pipeline_uuid TEXT NOT NULL CHECK (pipeline_uuid <> ''),
    isolation TEXT NOT NULL
        CHECK (isolation IN ('resources', 'pipeline'))
);

CREATE INDEX idx_pipeline_run_claim_sets_pipeline
    ON pipeline_run_claim_sets (pipeline_uuid, pipeline_id, isolation, run_id);

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

CREATE INDEX idx_pipeline_run_resource_claims_run
    ON pipeline_run_resource_claims (run_id, kind, identity);

CREATE TRIGGER release_pipeline_run_resource_claims
AFTER UPDATE OF status ON pipeline_runs
WHEN OLD.status IN ('queued', 'running')
 AND NEW.status NOT IN ('queued', 'running')
BEGIN
    DELETE FROM pipeline_run_claim_sets WHERE run_id = NEW.id;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS release_pipeline_run_resource_claims;
DROP TABLE IF EXISTS pipeline_run_resource_claims;
DROP TABLE IF EXISTS pipeline_run_claim_sets;
-- +goose StatementEnd
