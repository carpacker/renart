package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/riverqueue/river/rivermigrate"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("state path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrateRiver(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS pipeline_runs (
			id TEXT PRIMARY KEY,
			pipeline_id TEXT NOT NULL,
			pipeline TEXT NOT NULL,
			environment TEXT NOT NULL,
			trigger TEXT NOT NULL,
			status TEXT NOT NULL,
			win_start TEXT,
			win_end TEXT,
			started_at TEXT,
			finished_at TEXT,
			error TEXT,
			log_ref TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_pipeline_time ON pipeline_runs (pipeline_id, started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS pipeline_run_logs (
			run_id TEXT NOT NULL,
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			at TEXT NOT NULL,
			line TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES pipeline_runs(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_run_logs_run_seq ON pipeline_run_logs (run_id, seq)`,
		`CREATE TABLE IF NOT EXISTS schedule_watermarks (
			pipeline TEXT PRIMARY KEY,
			up_to TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS pipeline_schedule_settings (
			pipeline_id TEXT PRIMARY KEY,
			enabled INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrateRiver(ctx context.Context) error {
	migrator, err := rivermigrate.New(riversqlite.New(s.db), nil)
	if err != nil {
		return err
	}
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}

func (s *Store) Create(ctx context.Context, run PipelineRun) (string, error) {
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	if run.Status == "" {
		run.Status = RunStatusQueued
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO pipeline_runs (id, pipeline_id, pipeline, environment, trigger, status, win_start, win_end, started_at, finished_at, error, log_ref) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.PipelineID, run.Pipeline, run.Environment, string(run.Trigger), string(run.Status), timePtrString(run.WinStart), timePtrString(run.WinEnd), timePtrString(run.StartedAt), timePtrString(run.FinishedAt), run.Error, run.LogRef)
	return run.ID, err
}

func (s *Store) MarkRunning(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pipeline_runs SET status = ?, started_at = ? WHERE id = ?`, string(RunStatusRunning), formatTime(at), id)
	return err
}

func (s *Store) AppendLog(ctx context.Context, id string, line LogLine) error {
	if line.At.IsZero() {
		line.At = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO pipeline_run_logs (run_id, at, line) VALUES (?, ?, ?)`, id, formatTime(line.At), line.Line)
	return err
}

func (s *Store) Finish(ctx context.Context, id string, status RunStatus, runErr error) error {
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE pipeline_runs SET status = ?, finished_at = ?, error = ? WHERE id = ?`, string(status), formatTime(time.Now().UTC()), message, id)
	return err
}

func (s *Store) List(ctx context.Context, filter RunFilter) ([]PipelineRun, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, pipeline_id, pipeline, environment, trigger, status, win_start, win_end, started_at, finished_at, error, log_ref FROM pipeline_runs`
	args := []any{}
	if filter.PipelineID != "" {
		query += ` WHERE pipeline_id = ?`
		args = append(args, filter.PipelineID)
	}
	query += ` ORDER BY COALESCE(started_at, '') DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRuns(rows)
}

func (s *Store) Get(ctx context.Context, id string) (PipelineRun, []LogLine, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, pipeline_id, pipeline, environment, trigger, status, win_start, win_end, started_at, finished_at, error, log_ref FROM pipeline_runs WHERE id = ?`, id)
	run, err := scanRun(row)
	if err != nil {
		return PipelineRun{}, nil, err
	}
	logRows, err := s.db.QueryContext(ctx, `SELECT at, line FROM pipeline_run_logs WHERE run_id = ? ORDER BY seq ASC`, id)
	if err != nil {
		return PipelineRun{}, nil, err
	}
	defer logRows.Close()
	logs := []LogLine{}
	for logRows.Next() {
		var atRaw, line string
		if err := logRows.Scan(&atRaw, &line); err != nil {
			return PipelineRun{}, nil, err
		}
		logs = append(logs, LogLine{At: parseTimeValue(atRaw), Line: line})
	}
	return run, logs, logRows.Err()
}

func (s *Store) HasActiveRun(ctx context.Context, pipelineID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pipeline_runs WHERE pipeline_id = ? AND status IN (?, ?)`, pipelineID, string(RunStatusQueued), string(RunStatusRunning)).Scan(&count)
	return count > 0, err
}

func (s *Store) LastInterval(ctx context.Context, pipeline string) (time.Time, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT up_to FROM schedule_watermarks WHERE pipeline = ?`, pipeline).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return parseTimeValue(raw), true, nil
}

func (s *Store) SetInterval(ctx context.Context, pipeline string, upTo time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO schedule_watermarks (pipeline, up_to) VALUES (?, ?) ON CONFLICT(pipeline) DO UPDATE SET up_to = excluded.up_to`, pipeline, formatTime(upTo))
	return err
}

func (s *Store) ScheduleEnabled(ctx context.Context, pipelineID string) (bool, bool, error) {
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT enabled FROM pipeline_schedule_settings WHERE pipeline_id = ?`, pipelineID).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return enabled != 0, true, nil
}

func (s *Store) SetScheduleEnabled(ctx context.Context, pipelineID string, enabled bool) error {
	value := 0
	if enabled {
		value = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO pipeline_schedule_settings (pipeline_id, enabled, updated_at) VALUES (?, ?, ?) ON CONFLICT(pipeline_id) DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at`, pipelineID, value, formatTime(time.Now().UTC()))
	return err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanRuns(rows *sql.Rows) ([]PipelineRun, error) {
	runs := []PipelineRun{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func scanRun(row rowScanner) (PipelineRun, error) {
	var run PipelineRun
	var trigger, status string
	var winStart, winEnd, startedAt, finishedAt sql.NullString
	err := row.Scan(&run.ID, &run.PipelineID, &run.Pipeline, &run.Environment, &trigger, &status, &winStart, &winEnd, &startedAt, &finishedAt, &run.Error, &run.LogRef)
	if err != nil {
		return PipelineRun{}, err
	}
	run.Trigger = RunTrigger(trigger)
	run.Status = RunStatus(status)
	run.WinStart = parseNullTime(winStart)
	run.WinEnd = parseNullTime(winEnd)
	run.StartedAt = parseNullTime(startedAt)
	run.FinishedAt = parseNullTime(finishedAt)
	return run, nil
}

func timePtrString(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return formatTime(*value)
}

func parseNullTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	parsed := parseTimeValue(value.String)
	return &parsed
}

func parseTimeValue(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func statusFromResult(result RunResult) (RunStatus, error) {
	if result.Status == "ok" || result.Status == "success" || result.Status == "" {
		return RunStatusSuccess, nil
	}
	if result.Error == "" {
		result.Error = fmt.Sprintf("pipeline run finished with status %s", result.Status)
	}
	return RunStatusFailed, errors.New(result.Error)
}
