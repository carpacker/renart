-- +goose Up
-- +goose StatementBegin
-- Link Renart's durable run record to the River job that owns its execution.
-- This lets startup recovery distinguish a queued job that was never claimed
-- from one whose worker died just before it could mark the run as running.
ALTER TABLE pipeline_runs
    ADD COLUMN river_job_id INTEGER;

CREATE UNIQUE INDEX idx_pipeline_runs_river_job
    ON pipeline_runs (river_job_id)
    WHERE river_job_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_pipeline_runs_river_job;
ALTER TABLE pipeline_runs DROP COLUMN river_job_id;
-- +goose StatementEnd
