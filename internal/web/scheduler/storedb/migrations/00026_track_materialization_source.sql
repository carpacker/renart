-- +goose Up
-- +goose StatementBegin
-- Preserve whether the physical output was written from the saved working
-- tree or an immutable deployment. Materialization facts outlive retained run
-- rows, so this provenance belongs on both the immutable fact and the durable
-- latest-writer summary rather than being derived from pipeline_runs at read
-- time.
ALTER TABLE renart_materializations
    ADD COLUMN snapshot_version_id TEXT NOT NULL DEFAULT '';
ALTER TABLE renart_latest_successful_writers
    ADD COLUMN snapshot_version_id TEXT NOT NULL DEFAULT '';

-- Runs retained during the upgrade can restore provenance for existing facts.
-- Older/pruned rows safely remain the legacy empty (working-tree/unknown)
-- value, which keeps their prior classification.
UPDATE renart_materializations
SET snapshot_version_id = COALESCE((
    SELECT run.snapshot_version_id
    FROM pipeline_runs AS run
    WHERE run.id = renart_materializations.run_id
), '')
WHERE run_id <> '';

UPDATE renart_latest_successful_writers
SET snapshot_version_id = COALESCE((
    SELECT run.snapshot_version_id
    FROM pipeline_runs AS run
    WHERE run.id = renart_latest_successful_writers.run_id
), '')
WHERE run_id <> '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE renart_latest_successful_writers DROP COLUMN snapshot_version_id;
ALTER TABLE renart_materializations DROP COLUMN snapshot_version_id;
-- +goose StatementEnd
