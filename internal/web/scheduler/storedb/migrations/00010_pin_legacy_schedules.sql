-- +goose Up
-- +goose StatementBegin
-- Existing environment schedules used to allow an empty source pin and resolve
-- "latest deployment, else working tree" at run time. Freeze the then-current
-- deployment once. Rows without one become paused for explicit user review.
UPDATE renart_schedules
SET snapshot_version_id = COALESCE((
        SELECT version_id
        FROM renart_snapshots
        WHERE pipeline_id = renart_schedules.pipeline_id
        ORDER BY created_at DESC, version_id DESC
        LIMIT 1
    ), '')
WHERE TRIM(snapshot_version_id) = ''
  AND status != 'archived';

UPDATE renart_schedules
SET status = 'paused'
WHERE TRIM(snapshot_version_id) = ''
  AND status != 'archived';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Source pins cannot be reconstructed safely in reverse. Leave the rows pinned
-- or paused when rolling back the application schema.
SELECT 1;
-- +goose StatementEnd
