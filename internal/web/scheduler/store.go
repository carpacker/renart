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
	"github.com/riverqueue/river/rivertype"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"renart/internal/web/scheduler/storedb"
)

//go:embed storedb/migrations/*.sql
var schedulerMigrations embed.FS

type Store struct {
	db      *sql.DB
	queries *storedb.Queries
}

// ErrStateDatabaseIntegrity marks a state database that cannot be trusted.
// Callers must preserve it for recovery rather than silently recreating it:
// state.db contains schedules, deployments, run history, and derived freshness.
var ErrStateDatabaseIntegrity = errors.New("state database integrity check failed")

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
	if err := verifyStateDatabaseIntegrity(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf(
			"%w for %q: %v; stop Renart and back up state.db, state.db-wal, and state.db-shm before recovery",
			ErrStateDatabaseIntegrity,
			path,
			err,
		)
	}
	if err := preflightActiveRunSlotMigration(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
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

func preflightActiveRunSlotMigration(ctx context.Context, db *sql.DB) error {
	var hasRunsTable, hasSlotTable bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = 'pipeline_runs')`).Scan(&hasRunsTable); err != nil {
		return fmt.Errorf("inspect state database schema: %w", err)
	}
	if !hasRunsTable {
		return nil
	}
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = 'pipeline_run_slots')`).Scan(&hasSlotTable); err != nil {
		return fmt.Errorf("inspect active-run slot migration: %w", err)
	}
	if hasSlotTable {
		return nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT pipeline_id, id
		FROM pipeline_runs
		WHERE status IN ('queued', 'running')
		  AND pipeline_id IN (
			SELECT pipeline_id
			FROM pipeline_runs
			WHERE status IN ('queued', 'running')
			GROUP BY pipeline_id
			HAVING COUNT(*) > 1
		  )
		ORDER BY pipeline_id, id`)
	if err != nil {
		return fmt.Errorf("preflight active-run slot migration: %w", err)
	}
	defer rows.Close()
	conflicts := make(map[string][]string)
	order := make([]string, 0)
	for rows.Next() {
		var pipelineID, runID string
		if err := rows.Scan(&pipelineID, &runID); err != nil {
			return err
		}
		if _, exists := conflicts[pipelineID]; !exists {
			order = append(order, pipelineID)
		}
		conflicts[pipelineID] = append(conflicts[pipelineID], runID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(conflicts) == 0 {
		return nil
	}
	details := make([]string, 0, len(order))
	for _, pipelineID := range order {
		details = append(details, fmt.Sprintf("%s=[%s]", pipelineID, strings.Join(conflicts[pipelineID], ", ")))
	}
	return fmt.Errorf(
		"cannot migrate the state database to atomic pipeline run slots while duplicate active runs exist: %s; finish or cancel the listed runs before retrying",
		strings.Join(details, "; "),
	)
}

func verifyStateDatabaseIntegrity(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA quick_check(1)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	checked := false
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return err
		}
		checked = true
		if !strings.EqualFold(strings.TrimSpace(result), "ok") {
			return errors.New(result)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !checked {
		return errors.New("SQLite returned no integrity result")
	}
	return nil
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
	for attempt := 0; attempt < 2; attempt++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return "", err
		}
		queries := s.queries.WithTx(tx)
		id, err := s.createRun(ctx, queries, run)
		if err == nil {
			err = s.claimRunSlot(ctx, tx, run, id)
		}
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			_ = tx.Rollback()
		}
		if !isActiveRunSlotConstraint(err) {
			return id, err
		}
		conflict, found, lookupErr := s.pipelineRunActiveError(ctx, run.PipelineID, runSlotKeys(run))
		if lookupErr != nil {
			return "", errors.Join(err, lookupErr)
		}
		if found || attempt == 1 {
			return "", conflict
		}
	}
	return "", errors.New("pipeline run admission retry exhausted")
}

func (s *Store) createRun(ctx context.Context, queries *storedb.Queries, run PipelineRun) (string, error) {
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	if run.Status == "" {
		run.Status = RunStatusQueued
	}
	if run.ExecutionContextResolved {
		if run.WinStart == nil || run.WinEnd == nil {
			return "", errors.New("resolved run context requires a complete increasing window")
		}
		if err := validateRunExecutionContext(RunExecutionContext{
			Environment: run.Environment,
			WinStart:    *run.WinStart,
			WinEnd:      *run.WinEnd,
			FullRefresh: run.FullRefresh,
			Backfill:    run.Backfill,
			SensorMode:  run.SensorMode,
		}); err != nil {
			return "", err
		}
	}
	err := queries.CreateRun(ctx, storedb.CreateRunParams{
		ID:                       run.ID,
		PipelineID:               run.PipelineID,
		Pipeline:                 run.Pipeline,
		Environment:              run.Environment,
		Trigger:                  string(run.Trigger),
		Status:                   string(run.Status),
		WinStart:                 nullTime(run.WinStart),
		WinEnd:                   nullTime(run.WinEnd),
		StartedAt:                nullTime(run.StartedAt),
		FinishedAt:               nullTime(run.FinishedAt),
		Error:                    stringValue(run.Error),
		LogRef:                   stringValue(run.LogRef),
		SnapshotVersionID:        stringValue(run.SnapshotVersionID),
		RiverJobID:               nullInt64(run.RiverJobID),
		FullRefresh:              boolInt64(run.FullRefresh),
		Backfill:                 boolInt64(run.Backfill),
		SensorMode:               strings.TrimSpace(run.SensorMode),
		ExecutionContextResolved: boolInt64(run.ExecutionContextResolved),
	})
	return run.ID, err
}

type runSpecExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func runSlotKeys(run PipelineRun) []string {
	pipelineID := strings.TrimSpace(run.PipelineID)
	pipelineUUID := strings.TrimSpace(run.PipelineUUID)
	keys := make([]string, 0, 2)
	if pipelineID != "" {
		keys = append(keys, "path:"+pipelineID)
	}
	if pipelineUUID != "" {
		keys = append(keys, "uuid:"+pipelineUUID)
	}
	return keys
}

func (s *Store) claimRunSlot(ctx context.Context, execer runSpecExecer, run PipelineRun, runID string) error {
	status := run.Status
	if status == "" {
		status = RunStatusQueued
	}
	if status != RunStatusQueued && status != RunStatusRunning {
		return nil
	}
	slotKeys := runSlotKeys(run)
	if len(slotKeys) == 0 {
		return errors.New("active pipeline run requires a stable slot key")
	}
	for _, slotKey := range slotKeys {
		if _, err := execer.ExecContext(ctx, `
			INSERT INTO pipeline_run_slots (slot_key, run_id)
			VALUES (?, ?)`, slotKey, runID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) insertRunSpec(ctx context.Context, execer runSpecExecer, runID string, spec runSpecV1) error {
	body, err := marshalRunSpec(spec)
	if err != nil {
		return err
	}
	_, err = execer.ExecContext(ctx, `
		INSERT INTO pipeline_run_specs (run_id, version, body, created_at)
		VALUES (?, ?, ?, ?)`, runID, spec.Version, string(body), formatTime(time.Now().UTC()))
	return err
}

func (s *Store) CreateWithSpec(ctx context.Context, run PipelineRun, spec runSpecV1) (string, error) {
	if err := spec.validate(); err != nil {
		return "", err
	}
	if err := validateRunSpecBinding(run, spec); err != nil {
		return "", err
	}
	for attempt := 0; attempt < 2; attempt++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return "", err
		}
		queries := s.queries.WithTx(tx)
		id, err := s.createRun(ctx, queries, run)
		if err == nil {
			err = s.claimRunSlot(ctx, tx, run, id)
		}
		if err == nil {
			err = s.insertRunSpec(ctx, tx, id, spec)
		}
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			_ = tx.Rollback()
		}
		if !isActiveRunSlotConstraint(err) {
			return id, err
		}
		conflict, found, lookupErr := s.pipelineRunActiveError(ctx, run.PipelineID, runSlotKeys(run))
		if lookupErr != nil {
			return "", errors.Join(err, lookupErr)
		}
		if found || attempt == 1 {
			return "", conflict
		}
	}
	return "", errors.New("pipeline run admission retry exhausted")
}

func (s *Store) GetRunSpec(ctx context.Context, runID string) (runSpecV1, bool, error) {
	var version int
	var body string
	err := s.db.QueryRowContext(ctx, `
		SELECT version, body
		FROM pipeline_run_specs
		WHERE run_id = ?`, strings.TrimSpace(runID)).Scan(&version, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return runSpecV1{}, false, nil
	}
	if err != nil {
		return runSpecV1{}, false, err
	}
	spec, err := unmarshalRunSpec(version, []byte(body))
	if err != nil {
		return runSpecV1{}, true, &invalidRunSpecError{RunID: runID, Err: err}
	}
	return spec, true, nil
}

func (s *Store) SetRunSpecIfMissing(ctx context.Context, runID string, spec runSpecV1) (runSpecV1, error) {
	body, err := marshalRunSpec(spec)
	if err != nil {
		return runSpecV1{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return runSpecV1{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO pipeline_run_specs (run_id, version, body, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (run_id) DO NOTHING`, runID, spec.Version, string(body), formatTime(time.Now().UTC()))
	if err != nil {
		return runSpecV1{}, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return runSpecV1{}, err
	}
	if inserted == 1 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE pipeline_runs
			SET full_refresh = ?, backfill = ?, sensor_mode = ?
			WHERE id = ? AND status = ? AND execution_context_resolved = 0`,
			boolInt64(spec.Requested.FullRefresh), boolInt64(spec.Requested.Backfill), strings.TrimSpace(spec.Requested.SensorMode),
			runID, string(RunStatusQueued)); err != nil {
			return runSpecV1{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return runSpecV1{}, err
	}
	persisted, found, err := s.GetRunSpec(ctx, runID)
	if err != nil {
		return runSpecV1{}, err
	}
	if !found {
		return runSpecV1{}, fmt.Errorf("pipeline run %s spec was not persisted", runID)
	}
	return persisted, nil
}

func isActiveRunSlotConstraint(err error) bool {
	if err == nil {
		return false
	}
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) &&
		(sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE || sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY) &&
		strings.Contains(sqliteErr.Error(), "pipeline_run_slots.slot_key")
}

func (s *Store) pipelineRunActiveError(ctx context.Context, pipelineID string, slotKeys []string) (*PipelineRunActiveError, bool, error) {
	for _, slotKey := range slotKeys {
		var activeRunID string
		err := s.db.QueryRowContext(ctx, `
			SELECT run_id
			FROM pipeline_run_slots
			WHERE slot_key = ?`, strings.TrimSpace(slotKey)).Scan(&activeRunID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		return &PipelineRunActiveError{
			PipelineID:  strings.TrimSpace(pipelineID),
			ActiveRunID: activeRunID,
		}, true, nil
	}
	return &PipelineRunActiveError{PipelineID: strings.TrimSpace(pipelineID)}, false, nil
}

// RunExecutionContext is the normalized context that will be used by the
// executor. It is persisted synchronously before the first asset starts so a
// process crash cannot change recovery's materialization semantics.
type RunExecutionContext struct {
	Environment string
	WinStart    time.Time
	WinEnd      time.Time
	FullRefresh bool
	Backfill    bool
	SensorMode  string
}

func (s *Store) SetRunExecutionContext(ctx context.Context, runID string, execution RunExecutionContext) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run id is required")
	}
	if err := validateRunExecutionContext(execution); err != nil {
		return err
	}
	updated, err := s.queries.SetRunExecutionContext(ctx, storedb.SetRunExecutionContextParams{
		Environment: strings.TrimSpace(execution.Environment),
		WinStart:    stringValue(formatTime(execution.WinStart)),
		WinEnd:      stringValue(formatTime(execution.WinEnd)),
		FullRefresh: boolInt64(execution.FullRefresh),
		Backfill:    boolInt64(execution.Backfill),
		SensorMode:  strings.TrimSpace(execution.SensorMode),
		ID:          runID,
	})
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("pipeline run %s was not found", runID)
	}
	return nil
}

func validateRunExecutionContext(execution RunExecutionContext) error {
	if execution.WinStart.IsZero() || execution.WinEnd.IsZero() || !execution.WinStart.Before(execution.WinEnd) {
		return errors.New("resolved run context requires a complete increasing window")
	}
	if execution.FullRefresh && execution.Backfill {
		return errors.New("full refresh and backfill are mutually exclusive")
	}
	switch strings.TrimSpace(execution.SensorMode) {
	case "once", "wait", "skip":
		return nil
	default:
		return fmt.Errorf("resolved run context has invalid sensor mode %q", execution.SensorMode)
	}
}

// SetRunRiverJob links a run created before queue insertion (manual/API runs)
// to the River job that owns its execution.
func (s *Store) SetRunRiverJob(ctx context.Context, runID string, riverJobID int64) error {
	return s.setRunRiverJob(ctx, s.queries, runID, riverJobID)
}

func (s *Store) setRunRiverJob(ctx context.Context, queries *storedb.Queries, runID string, riverJobID int64) error {
	return queries.SetRunRiverJob(ctx, storedb.SetRunRiverJobParams{
		ID:         runID,
		RiverJobID: sql.NullInt64{Int64: riverJobID, Valid: true},
	})
}

func (s *Store) RunIDForRiverJob(ctx context.Context, riverJobID int64) (string, bool, error) {
	if riverJobID == 0 {
		return "", false, nil
	}
	var runID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM pipeline_runs WHERE river_job_id = ?`, riverJobID).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return runID, true, nil
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

type InterruptedStateRecovery struct {
	RunIDs             []string
	RiverJobsCancelled int64
	RiverJobsRequeued  int64
}

type interruptedRiverJob struct {
	id      int64
	attempt int
	kind    string
	args    string
}

// ReconcileInterruptedState repairs scheduler state left mid-flight by a
// previous process. The caller must hold the workspace scheduler lock and must
// invoke this before starting River workers, making every River job currently
// marked running unambiguously abandoned.
//
// Renart runs already marked running are failed, as are queued rows that no
// longer have a runnable River job. Available/pending/retryable/scheduled jobs
// are preserved. A claimed schedule signal that has not admitted a run yet is
// returned to River unchanged instead of losing its exact interval. Open steps
// are closed, derived-state replay is marked pending, and admitted abandoned
// jobs are terminalized as cancelled in the same SQLite transaction.
func (s *Store) ReconcileInterruptedState(ctx context.Context, reason string) (InterruptedStateRecovery, error) {
	var recovery InterruptedStateRecovery
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return recovery, err
	}
	defer func() { _ = tx.Rollback() }()

	jobRows, err := tx.QueryContext(ctx, `
		SELECT id, attempt, kind, json(args)
		FROM river_job
		WHERE queue = ?
		  AND kind IN (?, ?)
		  AND state = ?
		ORDER BY id`,
		pipelineRunQueue, pipelineRunJobKind, housekeepingJobKind, string(rivertype.JobStateRunning),
	)
	if err != nil {
		return recovery, err
	}
	var jobs []interruptedRiverJob
	for jobRows.Next() {
		var job interruptedRiverJob
		if scanErr := jobRows.Scan(&job.id, &job.attempt, &job.kind, &job.args); scanErr != nil {
			_ = jobRows.Close()
			return recovery, scanErr
		}
		jobs = append(jobs, job)
	}
	if closeErr := jobRows.Close(); closeErr != nil {
		return recovery, closeErr
	}
	if err := jobRows.Err(); err != nil {
		return recovery, err
	}

	// Link every pre-upgrade queued run to an extant runnable River job before
	// deciding whether the run is orphaned. New manual admissions persist this
	// link atomically, but old available jobs can survive an upgrade.
	if _, err := tx.ExecContext(ctx, `
		UPDATE pipeline_runs AS run
		SET river_job_id = (
			SELECT job.id
			FROM river_job AS job
			WHERE job.queue = ?
			  AND job.kind = ?
			  AND job.state IN (?, ?, ?, ?, ?)
			  AND json_extract(job.args, '$.run_id') = run.id
			ORDER BY CASE job.state WHEN ? THEN 0 ELSE 1 END, job.id
			LIMIT 1
		)
		WHERE run.status = ?
		  AND run.river_job_id IS NULL
		  AND EXISTS (
			SELECT 1
			FROM river_job AS job
			WHERE job.queue = ?
			  AND job.kind = ?
			  AND job.state IN (?, ?, ?, ?, ?)
			  AND json_extract(job.args, '$.run_id') = run.id
		  )`,
		pipelineRunQueue, pipelineRunJobKind,
		string(rivertype.JobStateAvailable), string(rivertype.JobStatePending), string(rivertype.JobStateRetryable), string(rivertype.JobStateScheduled), string(rivertype.JobStateRunning),
		string(rivertype.JobStateRunning), string(RunStatusQueued),
		pipelineRunQueue, pipelineRunJobKind,
		string(rivertype.JobStateAvailable), string(rivertype.JobStatePending), string(rivertype.JobStateRetryable), string(rivertype.JobStateScheduled), string(rivertype.JobStateRunning),
	); err != nil {
		return recovery, err
	}

	requeuedJobs := make(map[int64]struct{})
	// Legacy manual/API runs were created before their River job was inserted.
	// If an older process died during that handoff, recover the link from its
	// durable arguments. New admissions persist run, spec, job, and link in one
	// transaction, but keep this decoder until pre-upgrade jobs have drained.
	for _, job := range jobs {
		if job.kind != pipelineRunJobKind {
			continue
		}
		var args pipelineRunJobArgs
		if err := json.Unmarshal([]byte(job.args), &args); err != nil {
			return recovery, fmt.Errorf("decode interrupted River job %d arguments: %w", job.id, err)
		}
		if strings.TrimSpace(args.RunID) == "" {
			var linked bool
			if err := tx.QueryRowContext(ctx, `
				SELECT EXISTS(SELECT 1 FROM pipeline_runs WHERE river_job_id = ?)`, job.id).Scan(&linked); err != nil {
				return recovery, err
			}
			if linked {
				continue
			}
			result, err := tx.ExecContext(ctx, `
				UPDATE river_job
				SET state = ?,
				    attempt = CASE WHEN attempt > 0 THEN attempt - 1 ELSE 0 END,
				    scheduled_at = ?,
				    finalized_at = NULL
				WHERE id = ? AND state = ?`,
				string(rivertype.JobStateAvailable), formatTime(time.Now().UTC()), job.id, string(rivertype.JobStateRunning))
			if err != nil {
				return recovery, err
			}
			rowsAffected, err := result.RowsAffected()
			if err != nil {
				return recovery, err
			}
			if rowsAffected == 1 {
				requeuedJobs[job.id] = struct{}{}
				recovery.RiverJobsRequeued++
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE pipeline_runs
			SET river_job_id = ?
			WHERE id = ? AND river_job_id IS NULL`, job.id, args.RunID); err != nil {
			return recovery, err
		}
		// Rows admitted by builds predating durable execution context have the
		// request in River but only migration defaults in pipeline_runs. Preserve
		// that best-known request metadata for diagnostics. The resolved flag stays
		// false, so startup never treats it as effective context for fact replay.
		if _, err := tx.ExecContext(ctx, `
			UPDATE pipeline_runs
			SET full_refresh = ?, backfill = ?, sensor_mode = ?
			WHERE id = ?
			  AND execution_context_resolved = 0
			  AND status IN (?, ?)
			  AND NOT EXISTS (SELECT 1 FROM pipeline_run_specs WHERE run_id = pipeline_runs.id)`,
			boolInt64(args.FullRefresh), boolInt64(args.Backfill), strings.TrimSpace(args.SensorMode), args.RunID,
			string(RunStatusQueued), string(RunStatusRunning),
		); err != nil {
			return recovery, err
		}
	}

	runRows, err := tx.QueryContext(ctx, `
		SELECT run.id
		FROM pipeline_runs AS run
		WHERE run.status = ?
		   OR (run.status = ? AND NOT EXISTS (
			SELECT 1
			FROM river_job AS job
			WHERE job.id = run.river_job_id
			  AND job.queue = ?
			  AND job.kind = ?
			  AND job.state IN (?, ?, ?, ?)
		   ))
		ORDER BY COALESCE(started_at, ''), id`,
		string(RunStatusRunning), string(RunStatusQueued), pipelineRunQueue,
		pipelineRunJobKind, string(rivertype.JobStateAvailable), string(rivertype.JobStatePending),
		string(rivertype.JobStateRetryable), string(rivertype.JobStateScheduled),
	)
	if err != nil {
		return recovery, err
	}
	for runRows.Next() {
		var id string
		if scanErr := runRows.Scan(&id); scanErr != nil {
			_ = runRows.Close()
			return recovery, scanErr
		}
		recovery.RunIDs = append(recovery.RunIDs, id)
	}
	if closeErr := runRows.Close(); closeErr != nil {
		return recovery, closeErr
	}
	if err := runRows.Err(); err != nil {
		return recovery, err
	}

	nowTime := time.Now().UTC()
	now := formatTime(nowTime)
	for _, runID := range recovery.RunIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE pipeline_run_steps
			SET status = ?, finished_at = ?, error = CASE WHEN error IS NULL OR error = '' THEN ? ELSE error END
			WHERE run_id = ? AND finished_at IS NULL`,
			string(RunStatusFailed), now, reason, runID); err != nil {
			return recovery, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE pipeline_runs
			SET status = ?, finished_at = ?, error = ?, recovery_pending = 1
			WHERE id = ? AND status IN (?, ?)`,
			string(RunStatusFailed), now, reason, runID, string(RunStatusQueued), string(RunStatusRunning)); err != nil {
			return recovery, err
		}
	}

	for _, job := range jobs {
		if _, requeued := requeuedJobs[job.id]; requeued {
			continue
		}
		errorData, marshalErr := json.Marshal(rivertype.AttemptError{
			At:      nowTime,
			Attempt: job.attempt,
			Error:   reason,
		})
		if marshalErr != nil {
			return recovery, marshalErr
		}
		result, updateErr := tx.ExecContext(ctx, `
			UPDATE river_job
			SET state = ?,
			    finalized_at = ?,
			    errors = jsonb_insert(COALESCE(errors, jsonb('[]')), '$[#]', jsonb(?))
			WHERE id = ? AND state = ?`,
			string(rivertype.JobStateCancelled), now, string(errorData), job.id,
			string(rivertype.JobStateRunning),
		)
		if updateErr != nil {
			return recovery, updateErr
		}
		rowsAffected, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return recovery, rowsErr
		}
		recovery.RiverJobsCancelled += rowsAffected
	}

	if err := tx.Commit(); err != nil {
		return InterruptedStateRecovery{}, err
	}
	return recovery, nil
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

// FinishScheduledSuccess commits a successful scheduled run and its progress
// marker together. A crash or SQLite error must leave both unfinished so the
// interval can be retried, never a successful run with a stale watermark.
func (s *Store) FinishScheduledSuccess(ctx context.Context, id, watermark string, upTo time.Time) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("run id is required")
	}
	watermark = strings.TrimSpace(watermark)
	if watermark == "" {
		return errors.New("schedule watermark key is required")
	}
	if upTo.IsZero() {
		return errors.New("schedule watermark time is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	queries := s.queries.WithTx(tx)
	result, err := tx.ExecContext(ctx, `
		UPDATE pipeline_runs
		SET status = ?, finished_at = ?, error = NULL
		WHERE id = ? AND status IN (?, ?)`,
		string(RunStatusSuccess), formatTime(time.Now().UTC()), id,
		string(RunStatusQueued), string(RunStatusRunning),
	)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("active pipeline run %s was not found", id)
	}
	if err := queries.SetScheduleWatermark(ctx, storedb.SetScheduleWatermarkParams{
		Pipeline: watermark,
		UpTo:     formatTime(upTo),
	}); err != nil {
		return err
	}
	return tx.Commit()
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
		ID:                       row.ID,
		PipelineID:               row.PipelineID,
		RiverJobID:               int64FromNull(row.RiverJobID),
		Pipeline:                 row.Pipeline,
		Environment:              row.Environment,
		Trigger:                  RunTrigger(row.Trigger),
		Status:                   RunStatus(row.Status),
		WinStart:                 parseNullTime(row.WinStart),
		WinEnd:                   parseNullTime(row.WinEnd),
		StartedAt:                parseNullTime(row.StartedAt),
		FinishedAt:               parseNullTime(row.FinishedAt),
		Error:                    stringFromNull(row.Error),
		LogRef:                   stringFromNull(row.LogRef),
		SnapshotVersionID:        stringFromNull(row.SnapshotVersionID),
		FullRefresh:              row.FullRefresh != 0,
		Backfill:                 row.Backfill != 0,
		SensorMode:               row.SensorMode,
		ExecutionContextResolved: row.ExecutionContextResolved != 0,
	}
}

func boolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func nullInt64(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func int64FromNull(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
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
