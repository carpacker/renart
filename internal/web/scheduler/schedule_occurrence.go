package scheduler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrScheduleOccurrenceAlreadyAdmitted = errors.New("schedule occurrence is already admitted")

// ScheduleOccurrenceAlreadyAdmittedError is the idempotent result of two due
// signals racing, or of a duplicate signal arriving after the interval already
// succeeded. It is not an execution failure and must never create another run.
type ScheduleOccurrenceAlreadyAdmittedError struct {
	OccurrenceKey string
	Status        ScheduleOccurrenceStatus
	RunID         string
}

func (e *ScheduleOccurrenceAlreadyAdmittedError) Error() string {
	if e == nil {
		return ErrScheduleOccurrenceAlreadyAdmitted.Error()
	}
	if strings.TrimSpace(e.RunID) == "" {
		return fmt.Sprintf("schedule occurrence %s is already %s", e.OccurrenceKey, e.Status)
	}
	return fmt.Sprintf("schedule occurrence %s is already %s as run %s", e.OccurrenceKey, e.Status, e.RunID)
}

func (e *ScheduleOccurrenceAlreadyAdmittedError) Unwrap() error {
	return ErrScheduleOccurrenceAlreadyAdmitted
}

// newScheduleOccurrence canonicalizes the identity before hashing it. A JSON
// tuple avoids delimiter ambiguity while retaining a stable, secret-free key.
func newScheduleOccurrence(
	pipelineUUID string,
	environment string,
	start time.Time,
	end time.Time,
) (ScheduleOccurrence, error) {
	pipelineUUID = strings.TrimSpace(pipelineUUID)
	environment = strings.TrimSpace(environment)
	start = start.UTC()
	end = end.UTC()
	if pipelineUUID == "" {
		return ScheduleOccurrence{}, errors.New("schedule occurrence requires a stable pipeline UUID")
	}
	if environment == "" {
		return ScheduleOccurrence{}, errors.New("schedule occurrence requires an environment")
	}
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		return ScheduleOccurrence{}, errors.New("schedule occurrence requires an increasing interval")
	}
	identity, err := json.Marshal([]string{
		pipelineUUID,
		environment,
		formatTime(start),
		formatTime(end),
	})
	if err != nil {
		return ScheduleOccurrence{}, err
	}
	digest := sha256.Sum256(identity)
	now := time.Now().UTC()
	return ScheduleOccurrence{
		Key:           hex.EncodeToString(digest[:]),
		PipelineUUID:  pipelineUUID,
		Environment:   environment,
		IntervalStart: start,
		IntervalEnd:   end,
		Status:        ScheduleOccurrencePending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func validateScheduleOccurrence(occurrence ScheduleOccurrence) error {
	canonical, err := newScheduleOccurrence(
		occurrence.PipelineUUID,
		occurrence.Environment,
		occurrence.IntervalStart,
		occurrence.IntervalEnd,
	)
	if err != nil {
		return err
	}
	if strings.TrimSpace(occurrence.Key) != canonical.Key {
		return errors.New("schedule occurrence key does not match its normalized identity")
	}
	return nil
}

// EnsureScheduleOccurrence durably records the due interval before planning or
// run-slot admission. A failed/cancelled occurrence becomes pending when a new
// signal explicitly retries it; successful and active occurrences stay final.
func (s *Store) EnsureScheduleOccurrence(
	ctx context.Context,
	occurrence ScheduleOccurrence,
) (ScheduleOccurrence, bool, error) {
	if err := validateScheduleOccurrence(occurrence); err != nil {
		return ScheduleOccurrence{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScheduleOccurrence{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	now := formatTime(time.Now().UTC())
	result, err := tx.ExecContext(ctx, `
		INSERT INTO schedule_occurrences (
			occurrence_key, pipeline_uuid, environment, interval_start,
			interval_end, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		occurrence.Key,
		strings.TrimSpace(occurrence.PipelineUUID),
		strings.TrimSpace(occurrence.Environment),
		formatTime(occurrence.IntervalStart),
		formatTime(occurrence.IntervalEnd),
		string(ScheduleOccurrencePending),
		now,
		now,
	)
	if err != nil {
		return ScheduleOccurrence{}, false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return ScheduleOccurrence{}, false, err
	}
	retryResult, err := tx.ExecContext(ctx, `
		UPDATE schedule_occurrences
		SET status = ?, updated_at = ?
		WHERE occurrence_key = ? AND status IN (?, ?)`,
		string(ScheduleOccurrencePending), now, occurrence.Key,
		string(ScheduleOccurrenceFailed), string(ScheduleOccurrenceCancelled),
	)
	if err != nil {
		return ScheduleOccurrence{}, false, err
	}
	retried, err := retryResult.RowsAffected()
	if err != nil {
		return ScheduleOccurrence{}, false, err
	}
	persisted, err := getScheduleOccurrence(ctx, tx, occurrence.Key)
	if errors.Is(err, sql.ErrNoRows) {
		var conflictingKey string
		lookupErr := tx.QueryRowContext(ctx, `
			SELECT occurrence_key
			FROM schedule_occurrences
			WHERE pipeline_uuid = ? AND environment = ?
			  AND interval_start = ? AND interval_end = ?`,
			strings.TrimSpace(occurrence.PipelineUUID),
			strings.TrimSpace(occurrence.Environment),
			formatTime(occurrence.IntervalStart),
			formatTime(occurrence.IntervalEnd),
		).Scan(&conflictingKey)
		if lookupErr == nil {
			return ScheduleOccurrence{}, false, fmt.Errorf(
				"schedule occurrence identity collision: interval is stored as %s, not %s",
				conflictingKey, occurrence.Key,
			)
		}
		return ScheduleOccurrence{}, false, err
	}
	if err != nil {
		return ScheduleOccurrence{}, false, err
	}
	if err := validateScheduleOccurrenceBinding(occurrence, persisted); err != nil {
		return ScheduleOccurrence{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ScheduleOccurrence{}, false, err
	}
	return persisted, inserted == 1 || retried == 1, nil
}

func (s *Store) GetScheduleOccurrence(ctx context.Context, key string) (ScheduleOccurrence, bool, error) {
	occurrence, err := getScheduleOccurrence(ctx, s.db, strings.TrimSpace(key))
	if errors.Is(err, sql.ErrNoRows) {
		return ScheduleOccurrence{}, false, nil
	}
	return occurrence, err == nil, err
}

type scheduleOccurrenceQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getScheduleOccurrence(
	ctx context.Context,
	queryer scheduleOccurrenceQueryer,
	key string,
) (ScheduleOccurrence, error) {
	var occurrence ScheduleOccurrence
	var status, start, end, created, updated string
	var currentRunID sql.NullString
	err := queryer.QueryRowContext(ctx, `
		SELECT occurrence_key, pipeline_uuid, environment, interval_start,
		       interval_end, status, current_run_id, attempt_count,
		       created_at, updated_at
		FROM schedule_occurrences
		WHERE occurrence_key = ?`, strings.TrimSpace(key)).Scan(
		&occurrence.Key,
		&occurrence.PipelineUUID,
		&occurrence.Environment,
		&start,
		&end,
		&status,
		&currentRunID,
		&occurrence.AttemptCount,
		&created,
		&updated,
	)
	if err != nil {
		return ScheduleOccurrence{}, err
	}
	occurrence.IntervalStart = parseTimeValue(start)
	occurrence.IntervalEnd = parseTimeValue(end)
	occurrence.Status = ScheduleOccurrenceStatus(status)
	occurrence.CurrentRunID = stringFromNull(currentRunID)
	occurrence.CreatedAt = parseTimeValue(created)
	occurrence.UpdatedAt = parseTimeValue(updated)
	if !validScheduleOccurrenceStatus(occurrence.Status) || occurrence.IntervalStart.IsZero() ||
		occurrence.IntervalEnd.IsZero() || occurrence.CreatedAt.IsZero() || occurrence.UpdatedAt.IsZero() {
		return ScheduleOccurrence{}, fmt.Errorf("schedule occurrence %s contains invalid durable state", occurrence.Key)
	}
	if err := validateScheduleOccurrence(occurrence); err != nil {
		return ScheduleOccurrence{}, err
	}
	return occurrence, nil
}

func validScheduleOccurrenceStatus(status ScheduleOccurrenceStatus) bool {
	switch status {
	case ScheduleOccurrencePending, ScheduleOccurrenceAdmitting, ScheduleOccurrenceActive,
		ScheduleOccurrenceSuccess, ScheduleOccurrenceFailed, ScheduleOccurrenceCancelled:
		return true
	default:
		return false
	}
}

func validateScheduleOccurrenceBinding(expected, actual ScheduleOccurrence) error {
	if expected.Key != actual.Key || strings.TrimSpace(expected.PipelineUUID) != actual.PipelineUUID ||
		strings.TrimSpace(expected.Environment) != actual.Environment ||
		!expected.IntervalStart.UTC().Equal(actual.IntervalStart) ||
		!expected.IntervalEnd.UTC().Equal(actual.IntervalEnd) {
		return errors.New("stored schedule occurrence does not match the due signal identity")
	}
	return nil
}

// DeferredScheduleOccurrence returns the oldest interval currently waiting for
// planning or a pipeline run slot. It is deliberately a redacted projection.
func (s *Store) DeferredScheduleOccurrence(
	ctx context.Context,
	pipelineUUID string,
	environment string,
) (DeferredScheduleOccurrence, bool, error) {
	var result DeferredScheduleOccurrence
	var start, end string
	err := s.db.QueryRowContext(ctx, `
		SELECT interval_start, interval_end, attempt_count
		FROM schedule_occurrences
		WHERE pipeline_uuid = ? AND environment = ? AND status = ?
		ORDER BY interval_start, occurrence_key
		LIMIT 1`,
		strings.TrimSpace(pipelineUUID),
		strings.TrimSpace(environment),
		string(ScheduleOccurrencePending),
	).Scan(&start, &end, &result.AttemptCount)
	if errors.Is(err, sql.ErrNoRows) {
		return DeferredScheduleOccurrence{}, false, nil
	}
	if err != nil {
		return DeferredScheduleOccurrence{}, false, err
	}
	result.IntervalStart = parseTimeValue(start)
	result.IntervalEnd = parseTimeValue(end)
	if result.IntervalStart.IsZero() || result.IntervalEnd.IsZero() ||
		!result.IntervalStart.Before(result.IntervalEnd) || result.AttemptCount < 0 {
		return DeferredScheduleOccurrence{}, false, errors.New("deferred schedule occurrence contains invalid durable state")
	}
	return result, true, nil
}

// CreateScheduleOccurrenceAttemptWithSpecAndPlan atomically claims the
// occurrence, pipeline slot, run row, private spec, retained plan, and numbered
// attempt. It is retained for legacy scheduled jobs and focused store callers;
// new v2 signals use the private dispatch hook to add the run-ID-only River job
// in the same transaction.
func (s *Store) CreateScheduleOccurrenceAttemptWithSpecAndPlan(
	ctx context.Context,
	occurrence ScheduleOccurrence,
	run PipelineRun,
	spec runSpecV1,
	plan PipelineRunPlan,
) (string, error) {
	return s.createScheduleOccurrenceAttemptWithSpecAndPlan(ctx, occurrence, run, spec, plan, nil)
}

func (s *Store) createScheduleOccurrenceAttemptWithSpecAndPlan(
	ctx context.Context,
	occurrence ScheduleOccurrence,
	run PipelineRun,
	spec runSpecV1,
	plan PipelineRunPlan,
	dispatch func(*sql.Tx, string) error,
) (string, error) {
	if err := validateScheduleOccurrenceAdmissionBinding(occurrence, run, spec); err != nil {
		return "", err
	}
	if err := validateRunPlanAdmissionBinding(run, spec, plan); err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	now := formatTime(time.Now().UTC())
	claim, err := tx.ExecContext(ctx, `
		UPDATE schedule_occurrences
		SET status = ?, attempt_count = attempt_count + 1, updated_at = ?
		WHERE occurrence_key = ? AND status IN (?, ?, ?)`,
		string(ScheduleOccurrenceAdmitting), now, occurrence.Key,
		string(ScheduleOccurrencePending), string(ScheduleOccurrenceFailed), string(ScheduleOccurrenceCancelled),
	)
	if err != nil {
		return "", err
	}
	claimed, err := claim.RowsAffected()
	if err != nil {
		return "", err
	}
	if claimed == 0 {
		persisted, lookupErr := getScheduleOccurrence(ctx, tx, occurrence.Key)
		if lookupErr != nil {
			return "", lookupErr
		}
		if bindingErr := validateScheduleOccurrenceBinding(occurrence, persisted); bindingErr != nil {
			return "", bindingErr
		}
		return "", &ScheduleOccurrenceAlreadyAdmittedError{
			OccurrenceKey: persisted.Key,
			Status:        persisted.Status,
			RunID:         persisted.CurrentRunID,
		}
	}

	var attemptNo int
	if err := tx.QueryRowContext(ctx, `
		SELECT attempt_count
		FROM schedule_occurrences
		WHERE occurrence_key = ? AND status = ?`,
		occurrence.Key, string(ScheduleOccurrenceAdmitting),
	).Scan(&attemptNo); err != nil {
		return "", err
	}
	queries := s.queries.WithTx(tx)
	runID, err := s.createRun(ctx, queries, run)
	if err == nil {
		err = s.claimRunSlot(ctx, tx, run, runID)
	}
	if err == nil {
		err = s.insertRunSpec(ctx, tx, runID, spec)
	}
	if err == nil {
		err = s.insertRunPlan(ctx, tx, runID, plan)
	}
	if err == nil && dispatch != nil {
		err = dispatch(tx, runID)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO schedule_occurrence_attempts (
				occurrence_key, attempt_no, run_id, created_at
			) VALUES (?, ?, ?, ?)`, occurrence.Key, attemptNo, runID, now)
	}
	if err == nil {
		var activated sql.Result
		activated, err = tx.ExecContext(ctx, `
			UPDATE schedule_occurrences
			SET status = ?, current_run_id = ?, updated_at = ?
			WHERE occurrence_key = ? AND status = ? AND attempt_count = ?`,
			string(ScheduleOccurrenceActive), runID, now, occurrence.Key,
			string(ScheduleOccurrenceAdmitting), attemptNo,
		)
		if err == nil {
			var updated int64
			updated, err = activated.RowsAffected()
			if err == nil && updated != 1 {
				err = errors.New("schedule occurrence changed during atomic admission")
			}
		}
	}
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return runID, nil
}

func validateScheduleOccurrenceAdmissionBinding(
	occurrence ScheduleOccurrence,
	run PipelineRun,
	spec runSpecV1,
) error {
	if err := validateScheduleOccurrence(occurrence); err != nil {
		return err
	}
	if err := spec.validate(); err != nil {
		return err
	}
	if err := validateRunSpecAdmissionBinding(run, spec); err != nil {
		return err
	}
	if run.Trigger != RunTriggerSchedule || spec.Origin != RunTriggerSchedule || spec.Schedule == nil {
		return errors.New("schedule occurrence attempts require scheduled run provenance")
	}
	if strings.TrimSpace(spec.Schedule.OccurrenceKey) != occurrence.Key {
		return errors.New("run spec schedule occurrence key does not match admission")
	}
	if strings.TrimSpace(run.PipelineUUID) != occurrence.PipelineUUID ||
		strings.TrimSpace(run.Environment) != occurrence.Environment ||
		run.WinStart == nil || run.WinEnd == nil ||
		!run.WinStart.UTC().Equal(occurrence.IntervalStart) ||
		!run.WinEnd.UTC().Equal(occurrence.IntervalEnd) {
		return errors.New("scheduled run does not match its occurrence identity")
	}
	return nil
}

func (s *Store) validateActiveRunOccurrenceBinding(
	ctx context.Context,
	run PipelineRun,
	spec runSpecV1,
) error {
	if spec.Schedule == nil || strings.TrimSpace(spec.Schedule.OccurrenceKey) == "" {
		return nil // Strict compatibility for pre-occurrence RunSpec rows.
	}
	var matches bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM schedule_occurrences AS occurrence
			JOIN schedule_occurrence_attempts AS attempt
			  ON attempt.occurrence_key = occurrence.occurrence_key
			WHERE occurrence.occurrence_key = ?
			  AND occurrence.pipeline_uuid = ?
			  AND occurrence.environment = ?
			  AND occurrence.interval_start = ?
			  AND occurrence.interval_end = ?
			  AND occurrence.status = ?
			  AND occurrence.current_run_id = ?
			  AND attempt.run_id = ?
		)`,
		spec.Schedule.OccurrenceKey,
		spec.Schedule.PipelineUUID,
		spec.Schedule.Environment,
		formatTime(*spec.Requested.Start),
		formatTime(*spec.Requested.End),
		string(ScheduleOccurrenceActive),
		run.ID,
		run.ID,
	).Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return errors.New("run spec schedule occurrence does not match its active durable attempt")
	}
	return nil
}
