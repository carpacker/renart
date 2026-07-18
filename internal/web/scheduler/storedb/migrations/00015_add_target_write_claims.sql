-- +goose Up
-- +goose StatementBegin
-- A physical write can succeed before its materialization fact is durably
-- recorded. Claims make that uncertainty durable: any active or dirty claim
-- suppresses freshness for the target until the matching successful recorder
-- transaction resolves it.
CREATE TABLE renart_target_write_claims (
    claim_sequence  INTEGER PRIMARY KEY AUTOINCREMENT,
    target_identity TEXT NOT NULL CHECK (target_identity <> ''),
    completion_id   TEXT NOT NULL CHECK (completion_id <> ''),
    asset_id        TEXT NOT NULL CHECK (asset_id <> ''),
    state           TEXT NOT NULL CHECK (state IN ('active', 'dirty')),
    claimed_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE (target_identity, completion_id, asset_id)
);

CREATE INDEX idx_renart_target_write_claims_target_state
    ON renart_target_write_claims (target_identity, state, claim_sequence);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_renart_target_write_claims_target_state;
DROP TABLE IF EXISTS renart_target_write_claims;
-- +goose StatementEnd
