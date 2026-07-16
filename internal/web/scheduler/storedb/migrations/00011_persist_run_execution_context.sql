-- +goose Up
-- +goose StatementBegin
-- Persist the effective context before asset execution starts. Recovery can
-- then replay terminal steps with the same coverage semantics after a crash.
-- Existing rows remain explicitly unresolved; startup acknowledges their
-- pending recovery without inferring materialization facts from request data.
ALTER TABLE pipeline_runs
    ADD COLUMN full_refresh INTEGER NOT NULL DEFAULT 0;
ALTER TABLE pipeline_runs
    ADD COLUMN backfill INTEGER NOT NULL DEFAULT 0;
ALTER TABLE pipeline_runs
    ADD COLUMN sensor_mode TEXT NOT NULL DEFAULT '';
ALTER TABLE pipeline_runs
    ADD COLUMN execution_context_resolved INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE pipeline_runs DROP COLUMN execution_context_resolved;
ALTER TABLE pipeline_runs DROP COLUMN sensor_mode;
ALTER TABLE pipeline_runs DROP COLUMN backfill;
ALTER TABLE pipeline_runs DROP COLUMN full_refresh;
-- +goose StatementEnd
