package scheduler

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/riverqueue/river/rivermigrate"
	_ "modernc.org/sqlite"

	"renart/internal/web/scheduler/storedb"
)

//go:embed storedb/migrations/*.sql
var schedulerMigrations embed.FS

type Store struct {
	db      *sql.DB
	queries *storedb.Queries
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
	store := &Store{db: db, queries: storedb.New(db)}
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

// DB exposes the underlying SQLite handle so sibling stores (the
// materialization log, snapshots) share the same database and migration
// lifecycle.
func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	migrations, err := fs.Sub(schedulerMigrations, "storedb/migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, s.db, migrations)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
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
	err := s.queries.CreateRun(ctx, storedb.CreateRunParams{
		ID:                run.ID,
		PipelineID:        run.PipelineID,
		Pipeline:          run.Pipeline,
		Environment:       run.Environment,
		Trigger:           string(run.Trigger),
		Status:            string(run.Status),
		WinStart:          nullTime(run.WinStart),
		WinEnd:            nullTime(run.WinEnd),
		StartedAt:         nullTime(run.StartedAt),
		FinishedAt:        nullTime(run.FinishedAt),
		Error:             stringValue(run.Error),
		LogRef:            stringValue(run.LogRef),
		SnapshotVersionID: stringValue(run.SnapshotVersionID),
	})
	return run.ID, err
}

// SetRunSnapshotVersion records which deployed snapshot a run executed.
func (s *Store) SetRunSnapshotVersion(ctx context.Context, runID, versionID string) error {
	return s.queries.SetRunSnapshotVersion(ctx, storedb.SetRunSnapshotVersionParams{
		SnapshotVersionID: stringValue(versionID),
		ID:                runID,
	})
}

func (s *Store) MarkRunning(ctx context.Context, id string, at time.Time) error {
	return s.queries.MarkRunRunning(ctx, storedb.MarkRunRunningParams{
		Status:    string(RunStatusRunning),
		StartedAt: stringValue(formatTime(at)),
		ID:        id,
	})
}

func (s *Store) AppendLog(ctx context.Context, id string, line LogLine) error {
	if line.At.IsZero() {
		line.At = time.Now().UTC()
	}
	return s.queries.AppendRunLog(ctx, storedb.AppendRunLogParams{RunID: id, At: formatTime(line.At), Line: line.Line})
}

// FailOrphanedRuns reconciles runs left mid-flight by a previous process (e.g.
// the server was killed while executing tasks): every run still marked running
// is finished as failed, its open steps are closed, and durable derived-state
// replay is marked pending. Returns the newly reconciled run IDs. Queued runs
// are left untouched — the job queue may still pick them up.
func (s *Store) FailOrphanedRuns(ctx context.Context, reason string) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `SELECT id FROM pipeline_runs WHERE status = ?`, string(RunStatusRunning))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		ids = append(ids, id)
	}
	if closeErr := rows.Close(); closeErr != nil {
		return nil, closeErr
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, tx.Commit()
	}

	now := formatTime(time.Now().UTC())
	if _, err := tx.ExecContext(ctx,
		`UPDATE pipeline_run_steps
		 SET status = ?, finished_at = ?, error = CASE WHEN error IS NULL OR error = '' THEN ? ELSE error END
		 WHERE finished_at IS NULL AND run_id IN (SELECT id FROM pipeline_runs WHERE status = ?)`,
		string(RunStatusFailed), now, reason, string(RunStatusRunning),
	); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE pipeline_runs
		 SET status = ?, finished_at = ?, error = ?, recovery_pending = 1
		 WHERE status = ?`,
		string(RunStatusFailed), now, reason, string(RunStatusRunning),
	); err != nil {
		return nil, err
	}
	return ids, tx.Commit()
}

// PendingRunRecoveries returns interrupted runs whose persisted terminal steps
// have not yet been replayed into derived state.
func (s *Store) PendingRunRecoveries(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM pipeline_runs
		WHERE recovery_pending = 1
		ORDER BY COALESCE(finished_at, started_at, '') ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkRunRecoveryReplayed acknowledges derived-state replay. If the process
// dies before this write, the next startup replays the run again; downstream
// stores therefore make replay idempotent by run ID.
func (s *Store) MarkRunRecoveryReplayed(ctx context.Context, runID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pipeline_runs SET recovery_pending = 0 WHERE id = ?`, runID)
	return err
}

func (s *Store) Finish(ctx context.Context, id string, status RunStatus, runErr error) error {
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	return s.queries.FinishRun(ctx, storedb.FinishRunParams{
		Status:     string(status),
		FinishedAt: stringValue(formatTime(time.Now().UTC())),
		Error:      stringValue(message),
		ID:         id,
	})
}

func (s *Store) List(ctx context.Context, filter RunFilter) (RunList, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	params := runFilterParams(filter, limit, offset)
	total, err := s.queries.CountRuns(ctx, storedb.CountRunsParams{
		PipelineID:  params.PipelineID,
		Environment: params.Environment,
		Status:      params.Status,
		QueryLike:   params.QueryLike,
	})
	if err != nil {
		return RunList{}, err
	}
	rows, err := s.queries.ListRuns(ctx, params)
	if err != nil {
		return RunList{}, err
	}
	return RunList{Runs: runsFromDB(rows), Total: int(total), Limit: limit, Offset: offset}, nil
}

func runFilterParams(filter RunFilter, limit, offset int) storedb.ListRunsParams {
	queryLike := ""
	if query := strings.TrimSpace(filter.Query); query != "" {
		queryLike = "%" + strings.ToLower(query) + "%"
	}
	return storedb.ListRunsParams{
		PipelineID:  filter.PipelineID,
		Environment: filter.Environment,
		Status:      string(filter.Status),
		QueryLike:   queryLike,
		Limit:       int64(limit),
		Offset:      int64(offset),
	}
}

func (s *Store) Get(ctx context.Context, id string) (PipelineRun, []LogLine, []PipelineRunStep, error) {
	row, err := s.queries.GetRun(ctx, id)
	if err != nil {
		return PipelineRun{}, nil, nil, err
	}
	logRows, err := s.queries.ListRunLogs(ctx, id)
	if err != nil {
		return PipelineRun{}, nil, nil, err
	}
	logs := make([]LogLine, 0, len(logRows))
	for _, item := range logRows {
		logs = append(logs, LogLine{At: parseTimeValue(item.At), Line: item.Line})
	}
	steps, err := s.ListSteps(ctx, id)
	if err != nil {
		return PipelineRun{}, nil, nil, err
	}
	return runFromDB(row), logs, steps, nil
}

func (s *Store) UpsertStep(ctx context.Context, step PipelineRunStep) error {
	if strings.TrimSpace(step.Asset) == "" {
		return nil
	}
	return s.queries.UpsertRunStep(ctx, storedb.UpsertRunStepParams{
		RunID:      step.RunID,
		Asset:      step.Asset,
		Status:     string(step.Status),
		StartedAt:  nullTime(step.StartedAt),
		FinishedAt: nullTime(step.FinishedAt),
		Error:      stringValue(step.Error),
	})
}

func (s *Store) ListSteps(ctx context.Context, runID string) ([]PipelineRunStep, error) {
	rows, err := s.queries.ListRunSteps(ctx, runID)
	if err != nil {
		return nil, err
	}
	return stepsFromDB(rows), nil
}

func (s *Store) FinishOpenSteps(ctx context.Context, runID string, status RunStatus, at time.Time, runErr error) error {
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	return s.queries.FinishOpenRunSteps(ctx, storedb.FinishOpenRunStepsParams{
		Status:     string(status),
		FinishedAt: stringValue(formatTime(at)),
		Error:      message,
		RunID:      runID,
	})
}

func (s *Store) HasActiveRun(ctx context.Context, pipelineID string) (bool, error) {
	count, err := s.queries.CountActiveRuns(ctx, storedb.CountActiveRunsParams{
		PipelineID:    pipelineID,
		QueuedStatus:  string(RunStatusQueued),
		RunningStatus: string(RunStatusRunning),
	})
	return count > 0, err
}

func (s *Store) LastInterval(ctx context.Context, pipeline string) (time.Time, bool, error) {
	raw, err := s.queries.GetScheduleWatermark(ctx, pipeline)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return parseTimeValue(raw), true, nil
}

func (s *Store) SetInterval(ctx context.Context, pipeline string, upTo time.Time) error {
	return s.queries.SetScheduleWatermark(ctx, storedb.SetScheduleWatermarkParams{Pipeline: pipeline, UpTo: formatTime(upTo)})
}

func (s *Store) ScheduleEnabled(ctx context.Context, pipelineID string) (bool, bool, error) {
	enabled, err := s.queries.GetScheduleEnabled(ctx, pipelineID)
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
	return s.queries.SetScheduleEnabled(ctx, storedb.SetScheduleEnabledParams{
		PipelineID: pipelineID,
		Enabled:    int64(value),
		UpdatedAt:  formatTime(time.Now().UTC()),
	})
}

func (s *Store) UpsertEnvSchedule(ctx context.Context, schedule EnvSchedule) error {
	now := time.Now().UTC()
	if schedule.CreatedAt.IsZero() {
		schedule.CreatedAt = now
	}
	varsJSON := ""
	if len(schedule.Vars) > 0 {
		encoded, err := json.Marshal(schedule.Vars)
		if err != nil {
			return err
		}
		varsJSON = string(encoded)
	}
	return s.queries.UpsertEnvSchedule(ctx, storedb.UpsertEnvScheduleParams{
		PipelineID:        schedule.PipelineUUID,
		Environment:       schedule.Environment,
		SnapshotVersionID: schedule.SnapshotVersionID,
		Cron:              schedule.Cron,
		Timezone:          schedule.Timezone,
		Vars:              stringValue(varsJSON),
		CatchupPolicy:     string(schedule.CatchupPolicy),
		Status:            string(schedule.Status),
		ArchivedReason:    schedule.ArchivedReason,
		CreatedAt:         formatTime(schedule.CreatedAt),
		UpdatedAt:         formatTime(now),
	})
}

func (s *Store) ListEnvSchedules(ctx context.Context) ([]EnvSchedule, error) {
	rows, err := s.queries.ListEnvSchedules(ctx)
	if err != nil {
		return nil, err
	}
	schedules := make([]EnvSchedule, 0, len(rows))
	for _, row := range rows {
		schedules = append(schedules, envScheduleFromDB(row))
	}
	return schedules, nil
}

func (s *Store) GetEnvSchedule(ctx context.Context, pipelineUUID, environment string) (EnvSchedule, bool, error) {
	row, err := s.queries.GetEnvSchedule(ctx, storedb.GetEnvScheduleParams{PipelineID: pipelineUUID, Environment: environment})
	if errors.Is(err, sql.ErrNoRows) {
		return EnvSchedule{}, false, nil
	}
	if err != nil {
		return EnvSchedule{}, false, err
	}
	return envScheduleFromDB(row), true, nil
}

func (s *Store) SetEnvScheduleStatus(ctx context.Context, pipelineUUID, environment string, status ScheduleStatus, archivedReason string) error {
	return s.queries.SetEnvScheduleStatus(ctx, storedb.SetEnvScheduleStatusParams{
		Status:         string(status),
		ArchivedReason: archivedReason,
		UpdatedAt:      formatTime(time.Now().UTC()),
		PipelineID:     pipelineUUID,
		Environment:    environment,
	})
}

func (s *Store) SetEnvScheduleNextRun(ctx context.Context, pipelineUUID, environment string, nextRunAt *time.Time) error {
	return s.queries.SetEnvScheduleNextRun(ctx, storedb.SetEnvScheduleNextRunParams{
		NextRunAt:   nullTime(nextRunAt),
		PipelineID:  pipelineUUID,
		Environment: environment,
	})
}

func (s *Store) CountEnvSchedules(ctx context.Context) (int64, error) {
	return s.queries.CountEnvSchedules(ctx)
}

func envScheduleFromDB(row storedb.RenartSchedule) EnvSchedule {
	schedule := EnvSchedule{
		PipelineUUID:      row.PipelineID,
		Environment:       row.Environment,
		SnapshotVersionID: row.SnapshotVersionID,
		Cron:              row.Cron,
		Timezone:          row.Timezone,
		CatchupPolicy:     CatchupPolicy(row.CatchupPolicy),
		Status:            ScheduleStatus(row.Status),
		ArchivedReason:    row.ArchivedReason,
		NextRunAt:         parseNullTime(row.NextRunAt),
		CreatedAt:         parseTimeValue(row.CreatedAt),
		UpdatedAt:         parseTimeValue(row.UpdatedAt),
	}
	if raw := stringFromNull(row.Vars); raw != "" {
		_ = json.Unmarshal([]byte(raw), &schedule.Vars)
	}
	return schedule
}

func nullTime(value *time.Time) sql.NullString {
	if value == nil || value.IsZero() {
		return sql.NullString{}
	}
	return stringValue(formatTime(*value))
}

func stringValue(value string) sql.NullString {
	return sql.NullString{String: value, Valid: true}
}

func stringFromNull(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
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

func runsFromDB(rows []storedb.PipelineRun) []PipelineRun {
	runs := make([]PipelineRun, 0, len(rows))
	for _, row := range rows {
		runs = append(runs, runFromDB(row))
	}
	return runs
}

func runFromDB(row storedb.PipelineRun) PipelineRun {
	return PipelineRun{
		ID:                row.ID,
		PipelineID:        row.PipelineID,
		Pipeline:          row.Pipeline,
		Environment:       row.Environment,
		Trigger:           RunTrigger(row.Trigger),
		Status:            RunStatus(row.Status),
		WinStart:          parseNullTime(row.WinStart),
		WinEnd:            parseNullTime(row.WinEnd),
		StartedAt:         parseNullTime(row.StartedAt),
		FinishedAt:        parseNullTime(row.FinishedAt),
		Error:             stringFromNull(row.Error),
		LogRef:            stringFromNull(row.LogRef),
		SnapshotVersionID: stringFromNull(row.SnapshotVersionID),
	}
}

func stepsFromDB(rows []storedb.PipelineRunStep) []PipelineRunStep {
	steps := make([]PipelineRunStep, 0, len(rows))
	for _, row := range rows {
		steps = append(steps, PipelineRunStep{
			RunID:      row.RunID,
			Asset:      row.Asset,
			Status:     RunStatus(row.Status),
			StartedAt:  parseNullTime(row.StartedAt),
			FinishedAt: parseNullTime(row.FinishedAt),
			Error:      stringFromNull(row.Error),
		})
	}
	return steps
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
