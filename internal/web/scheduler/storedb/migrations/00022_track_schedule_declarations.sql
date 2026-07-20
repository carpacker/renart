-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS renart_schedule_declarations (
    pipeline_id TEXT NOT NULL,
    environment TEXT NOT NULL,
    PRIMARY KEY (pipeline_id, environment),
    FOREIGN KEY (pipeline_id, environment)
        REFERENCES renart_schedules (pipeline_id, environment) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS renart_schedule_declarations;
-- +goose StatementEnd
