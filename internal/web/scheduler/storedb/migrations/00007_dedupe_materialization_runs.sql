-- +goose Up
-- +goose StatementBegin
-- Scheduler recovery may replay a completion event when the original event was
-- recorded immediately before the process died. Keep one immutable success
-- fact per asset/environment/scheduler-run; build-mode facts have an empty
-- run_id and intentionally remain unrestricted.
DELETE FROM renart_materializations
WHERE run_id <> ''
  AND id NOT IN (
      SELECT MIN(id)
      FROM renart_materializations
      WHERE run_id <> ''
      GROUP BY asset_id, environment, run_id
  );

CREATE UNIQUE INDEX IF NOT EXISTS idx_renart_mat_run
    ON renart_materializations (asset_id, environment, run_id)
    WHERE run_id <> '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_renart_mat_run;
-- +goose StatementEnd
