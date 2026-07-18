-- +goose Up
-- +goose StatementBegin
ALTER TABLE pipeline_runs
    ADD COLUMN execution_target_snapshot TEXT NOT NULL DEFAULT ''
        CHECK (execution_target_snapshot = '' OR json_valid(execution_target_snapshot));

ALTER TABLE pipeline_run_steps
    ADD COLUMN completion_ordinal INTEGER
        CHECK (completion_ordinal IS NULL OR completion_ordinal >= 0);

-- Existing terminal steps predate explicit completion ordering. Give them a
-- deterministic per-run order so startup recovery remains stable after the
-- migration; open steps receive their ordinal when they become terminal.
WITH ranked AS (
    SELECT
        run_id,
        asset,
        ROW_NUMBER() OVER (
            PARTITION BY run_id
            ORDER BY COALESCE(finished_at, started_at, ''), asset
        ) - 1 AS completion_ordinal
    FROM pipeline_run_steps
    WHERE finished_at IS NOT NULL
       OR status IN ('success', 'failed', 'cancelled')
)
UPDATE pipeline_run_steps
SET completion_ordinal = (
    SELECT ranked.completion_ordinal
    FROM ranked
    WHERE ranked.run_id = pipeline_run_steps.run_id
      AND ranked.asset = pipeline_run_steps.asset
)
WHERE EXISTS (
    SELECT 1
    FROM ranked
    WHERE ranked.run_id = pipeline_run_steps.run_id
      AND ranked.asset = pipeline_run_steps.asset
);

CREATE UNIQUE INDEX idx_pipeline_run_steps_completion
    ON pipeline_run_steps (run_id, completion_ordinal)
    WHERE completion_ordinal IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_pipeline_run_steps_completion;
ALTER TABLE pipeline_run_steps DROP COLUMN completion_ordinal;
ALTER TABLE pipeline_runs DROP COLUMN execution_target_snapshot;
-- +goose StatementEnd
