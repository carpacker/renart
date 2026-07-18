-- +goose Up
-- +goose StatementBegin
ALTER TABLE pipeline_run_steps
    ADD COLUMN upstream_writer_snapshot TEXT NOT NULL DEFAULT ''
        CHECK (upstream_writer_snapshot = '' OR json_valid(upstream_writer_snapshot));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE pipeline_run_steps DROP COLUMN upstream_writer_snapshot;
-- +goose StatementEnd
