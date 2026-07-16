-- +goose Up
-- +goose StatementBegin
CREATE TABLE pipeline_run_specs (
    run_id TEXT PRIMARY KEY
        REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    body TEXT NOT NULL CHECK (json_valid(body)),
    created_at TEXT NOT NULL
);

CREATE TABLE pipeline_run_slots (
    slot_key TEXT PRIMARY KEY,
    run_id TEXT NOT NULL
        REFERENCES pipeline_runs(id) ON DELETE CASCADE
);

CREATE INDEX idx_pipeline_run_slots_run ON pipeline_run_slots (run_id);

INSERT INTO pipeline_run_slots (slot_key, run_id)
SELECT 'path:' || pipeline_id, id
FROM pipeline_runs
WHERE status IN ('queued', 'running');

CREATE TRIGGER release_pipeline_run_slot
AFTER UPDATE OF status ON pipeline_runs
WHEN OLD.status IN ('queued', 'running')
 AND NEW.status NOT IN ('queued', 'running')
BEGIN
    DELETE FROM pipeline_run_slots WHERE run_id = NEW.id;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS release_pipeline_run_slot;
DROP TABLE IF EXISTS pipeline_run_slots;
DROP TABLE IF EXISTS pipeline_run_specs;
-- +goose StatementEnd
