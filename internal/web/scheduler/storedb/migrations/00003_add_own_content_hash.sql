-- +goose Up
-- +goose StatementBegin
-- own_content is the asset's own-definition sub-hash (no upstream
-- contribution). The staleness service compares it against the current
-- engine result to distinguish stale_edited from stale_upstream.
ALTER TABLE renart_materializations ADD COLUMN own_content TEXT NOT NULL DEFAULT '';
ALTER TABLE renart_coverage ADD COLUMN own_content TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE renart_coverage DROP COLUMN own_content;
ALTER TABLE renart_materializations DROP COLUMN own_content;
-- +goose StatementEnd
