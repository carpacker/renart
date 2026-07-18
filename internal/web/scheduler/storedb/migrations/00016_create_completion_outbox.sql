-- +goose Up
-- +goose StatementBegin
CREATE TABLE renart_completion_outbox (
    sequence      INTEGER PRIMARY KEY AUTOINCREMENT,
    completion_id TEXT NOT NULL UNIQUE
        CHECK (completion_id <> '' AND completion_id = trim(completion_id)),
    version       INTEGER NOT NULL CHECK (version = 1),
    body          TEXT NOT NULL
        CHECK (json_valid(body))
        CHECK (json_type(body) = 'object')
        CHECK (COALESCE(json_extract(body, '$.version') = version, 0))
        CHECK (COALESCE(json_extract(body, '$.event.completion_id') = completion_id, 0)),
    enqueued_at   TEXT NOT NULL CHECK (enqueued_at <> '')
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS renart_completion_outbox;
-- +goose StatementEnd
