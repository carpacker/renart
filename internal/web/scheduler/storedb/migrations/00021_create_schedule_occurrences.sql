-- +goose Up
-- +goose StatementBegin
-- River's job uniqueness is intentionally temporary: it prevents duplicate
-- active signals, but no longer remembers an interval after the job reaches a
-- terminal state. Keep the logical scheduled occurrence in Renart storage so
-- retries and duplicate signals share one durable identity.
CREATE TABLE schedule_occurrences (
    occurrence_key TEXT PRIMARY KEY
        CHECK (length(occurrence_key) = 64 AND occurrence_key = lower(occurrence_key)),
    pipeline_uuid TEXT NOT NULL CHECK (pipeline_uuid <> ''),
    environment TEXT NOT NULL CHECK (environment <> ''),
    interval_start TEXT NOT NULL,
    interval_end TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'admitting', 'active', 'success', 'failed', 'cancelled')),
    current_run_id TEXT
        REFERENCES pipeline_runs(id) ON DELETE SET NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (pipeline_uuid, environment, interval_start, interval_end)
);

CREATE INDEX idx_schedule_occurrences_schedule_status
    ON schedule_occurrences (pipeline_uuid, environment, status, interval_start);

CREATE TABLE schedule_occurrence_attempts (
    occurrence_key TEXT NOT NULL
        REFERENCES schedule_occurrences(occurrence_key) ON DELETE CASCADE,
    attempt_no INTEGER NOT NULL CHECK (attempt_no > 0),
    run_id TEXT NOT NULL UNIQUE
        REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    PRIMARY KEY (occurrence_key, attempt_no)
);

-- All terminalization paths already update pipeline_runs transactionally.
-- Reflect that transition in the occurrence in the same transaction, including
-- blocked-plan failures, cancellation, panic recovery, and scheduled success
-- plus watermark advancement.
CREATE TRIGGER update_schedule_occurrence_terminal
AFTER UPDATE OF status ON pipeline_runs
WHEN NEW.status IN ('success', 'failed', 'cancelled')
BEGIN
    UPDATE schedule_occurrences
    SET status = NEW.status,
        updated_at = COALESCE(NULLIF(NEW.finished_at, ''), updated_at)
    WHERE current_run_id = NEW.id
      AND status = 'active';
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS update_schedule_occurrence_terminal;
DROP INDEX IF EXISTS idx_schedule_occurrences_schedule_status;
DROP TABLE IF EXISTS schedule_occurrence_attempts;
DROP TABLE IF EXISTS schedule_occurrences;
-- +goose StatementEnd
