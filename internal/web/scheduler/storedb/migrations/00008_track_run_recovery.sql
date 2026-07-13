-- +goose Up
-- +goose StatementBegin
-- Keep interrupted-run replay durable. The callback clears this flag only
-- after derived freshness state has been rebuilt, so another hard stop during
-- startup safely retries the replay on the next launch.
ALTER TABLE pipeline_runs
    ADD COLUMN recovery_pending INTEGER NOT NULL DEFAULT 0;

-- Runs reconciled by builds predating durable replay still need their persisted
-- terminal steps folded into freshness state once.
UPDATE pipeline_runs
SET recovery_pending = 1
WHERE status = 'failed'
  AND error = 'interrupted: the server stopped while this run was executing';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE pipeline_runs DROP COLUMN recovery_pending;
-- +goose StatementEnd
