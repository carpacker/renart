-- +goose Up
-- The latest main-task outcome and the latest quality-check outcome are
-- orthogonal: a successful write remains fresh even when a post-write
-- assertion fails.
ALTER TABLE renart_asset_runs
    ADD COLUMN quality_status TEXT NOT NULL DEFAULT ''
        CHECK (quality_status IN ('', 'passed', 'failed'));

ALTER TABLE renart_asset_runs
    ADD COLUMN failed_checks TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE renart_asset_runs DROP COLUMN failed_checks;
ALTER TABLE renart_asset_runs DROP COLUMN quality_status;
