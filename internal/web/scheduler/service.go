package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/riverqueue/river/rivertype"
	"github.com/robfig/cron/v3"
)

const pipelineRunQueue = "renart_pipeline_runs"

type PipelineSource func(context.Context) ([]PipelineSchedule, error)

type Service struct {
	store                 *Store
	runner                func(context.Context, RunRequest, func(string)) RunResult
	pipelines             PipelineSource
	publish               func(any)
	stateDir              string
	housekeeping          func(context.Context) error
	resolvePipelineRef    func(context.Context, string) (PipelineRef, bool)
	defaultEnvironment    func() string
	pipelineIntervalAware func(context.Context, string) bool
	deployPipeline        func(context.Context, string) (string, error)
	recoverRun            func(context.Context, PipelineRun, []PipelineRunStep) error
	lock                  *flock.Flock
	riverClient           *river.Client[*sql.Tx]
	mu                    sync.Mutex
	schedulerOn           bool
	ownerMessage          string
}

type Options struct {
	Store     *Store
	Runner    func(context.Context, RunRequest, func(string)) RunResult
	Pipelines PipelineSource
	Publish   func(any)
	StateDir  string
	// Housekeeping runs daily as a River periodic job (materialization-log
	// retention pruning and similar maintenance).
	Housekeeping func(context.Context) error
	// ResolvePipelineRef maps a stable pipeline UUID to its current
	// workspace incarnation; ok=false means the pipeline file is gone
	// (deleted or on another branch).
	ResolvePipelineRef func(context.Context, string) (PipelineRef, bool)
	// DefaultEnvironment names the environment legacy single-env schedules
	// are migrated to.
	DefaultEnvironment func() string
	// PipelineIntervalAware reports whether the pipeline has interval-aware
	// assets; the backfill catch-up policy is only legal for those.
	PipelineIntervalAware func(context.Context, string) bool
	// DeployPipeline snapshots the working tree and returns the new
	// version ID (the "deploy now" path when enabling a schedule).
	DeployPipeline func(context.Context, string) (string, error)
	// RecoverRun observes a run reconciled after an unclean stop. The callback
	// receives the persisted terminal steps after open steps have been failed;
	// it may rebuild derived state, but must never execute the pipeline again.
	RecoverRun func(context.Context, PipelineRun, []PipelineRunStep) error
}

type pipelineRunJobArgs struct {
	RunID        string `json:"run_id,omitempty" river:"unique"`
	PipelineID   string `json:"pipeline_id,omitempty" river:"unique"`
	PipelineName string `json:"pipeline_name,omitempty"`
	// PipelineUUID is set for per-environment scheduled runs; together with
	// Environment and the interval it forms the unique logical-run key, so
	// restarts, leader churn, and catch-up can never double-enqueue.
	PipelineUUID      string     `json:"pipeline_uuid,omitempty" river:"unique"`
	Environment       string     `json:"environment,omitempty" river:"unique"`
	Trigger           RunTrigger `json:"trigger,omitempty"`
	Schedule          string     `json:"schedule,omitempty"`
	Timezone          string     `json:"timezone,omitempty"`
	Start             string     `json:"start,omitempty" river:"unique"`
	End               string     `json:"end,omitempty" river:"unique"`
	SnapshotVersionID string     `json:"snapshot_version_id,omitempty"`
}

func (pipelineRunJobArgs) Kind() string { return "renart-pipeline-run" }

type pipelineRunWorker struct {
	river.WorkerDefaults[pipelineRunJobArgs]
	service *Service
}

func (w *pipelineRunWorker) Work(ctx context.Context, job *river.Job[pipelineRunJobArgs]) error {
	run, ok, err := w.service.prepareRun(ctx, job.Args)
	if err != nil || !ok {
		return err
	}
	return w.service.execute(ctx, run)
}

type housekeepingJobArgs struct{}

func (housekeepingJobArgs) Kind() string { return "renart-housekeeping" }

type housekeepingWorker struct {
	river.WorkerDefaults[housekeepingJobArgs]
	service *Service
}

func (w *housekeepingWorker) Work(ctx context.Context, job *river.Job[housekeepingJobArgs]) error {
	if w.service.housekeeping == nil {
		return nil
	}
	return w.service.housekeeping(ctx)
}

type cronPeriodicSchedule struct {
	schedule cron.Schedule
}

func (s cronPeriodicSchedule) Next(current time.Time) time.Time {
	return s.schedule.Next(current)
}

func New(options Options) *Service {
	return &Service{
		store:                 options.Store,
		runner:                options.Runner,
		pipelines:             options.Pipelines,
		publish:               options.Publish,
		stateDir:              options.StateDir,
		housekeeping:          options.Housekeeping,
		resolvePipelineRef:    options.ResolvePipelineRef,
		defaultEnvironment:    options.DefaultEnvironment,
		pipelineIntervalAware: options.PipelineIntervalAware,
		deployPipeline:        options.DeployPipeline,
		recoverRun:            options.RecoverRun,
	}
}

func (s *Service) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if s.store == nil || s.runner == nil {
		return errors.New("scheduler is not configured")
	}
	if err := os.MkdirAll(s.stateDir, 0o755); err != nil {
		return err
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &pipelineRunWorker{service: s})
	river.AddWorker(workers, &housekeepingWorker{service: s})
	client, err := river.NewClient(riversqlite.New(s.store.db), &river.Config{
		CompletedJobRetentionPeriod: 24 * time.Hour,
		DiscardedJobRetentionPeriod: 7 * 24 * time.Hour,
		FetchPollInterval:           time.Second,
		JobTimeout:                  -1,
		Logger:                      slog.New(slog.NewTextHandler(io.Discard, nil)),
		MaxAttempts:                 1,
		PollOnly:                    true,
		Queues:                      map[string]river.QueueConfig{pipelineRunQueue: {MaxWorkers: 4}},
		Workers:                     workers,
	})
	if err != nil {
		return err
	}
	if err := client.Start(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	s.riverClient = client
	s.mu.Unlock()
	s.lock = flock.New(filepath.Join(s.stateDir, "scheduler.lock"))
	locked, err := s.lock.TryLock()
	if err != nil {
		s.clearRiverClient(client)
		stopRiverClient(client)
		return err
	}
	if !locked {
		s.ownerMessage = "scheduler lock is held by another Renart process; this process will not register schedules"
		return nil
	}
	s.schedulerOn = true
	s.recoverOrphanedRuns(ctx)
	if err := s.Reconcile(ctx); err != nil {
		s.schedulerOn = false
		_ = s.lock.Unlock()
		s.clearRiverClient(client)
		stopRiverClient(client)
		return err
	}
	go func() {
		<-ctx.Done()
		s.Stop()
	}()
	return nil
}

// orphanedRunError explains a run that was reconciled after an unclean stop.
const orphanedRunError = "interrupted: the server stopped while this run was executing"

// recoverOrphanedRuns fails any run left "running" by a previous process that
// was killed mid-execution (otherwise it would stay running forever), and
// notifies listeners so open run views update.
func (s *Service) recoverOrphanedRuns(ctx context.Context) {
	_, err := s.store.FailOrphanedRuns(ctx, orphanedRunError)
	if err != nil {
		slog.Warn("failed to reconcile orphaned pipeline runs", "error", err)
		return
	}
	ids, err := s.store.PendingRunRecoveries(ctx)
	if err != nil {
		slog.Warn("failed to list pending pipeline run recoveries", "error", err)
		return
	}
	for _, id := range ids {
		run, _, steps, getErr := s.store.Get(ctx, id)
		if getErr != nil {
			slog.Warn("failed to load reconciled pipeline run", "run_id", id, "error", getErr)
			continue
		}
		if s.recoverRun != nil {
			if recoverErr := s.recoverRun(ctx, run, steps); recoverErr != nil {
				slog.Warn("failed to replay reconciled pipeline run", "run_id", id, "error", recoverErr)
				s.publishRunEvent("run.finished", run)
				continue
			}
			if markErr := s.store.MarkRunRecoveryReplayed(ctx, id); markErr != nil {
				slog.Warn("failed to acknowledge reconciled pipeline run replay", "run_id", id, "error", markErr)
				s.publishRunEvent("run.finished", run)
				continue
			}
		}
		s.publishRunEvent("run.finished", run)
	}
}

func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	client := s.riverClient
	s.riverClient = nil
	s.schedulerOn = false
	s.mu.Unlock()
	if client != nil {
		stopRiverClient(client)
	}
	if s.lock != nil {
		_ = s.lock.Unlock()
	}
}

func stopRiverClient(client *river.Client[*sql.Tx]) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = client.Stop(ctx)
}

func (s *Service) clearRiverClient(client *river.Client[*sql.Tx]) {
	s.mu.Lock()
	if s.riverClient == client {
		s.riverClient = nil
	}
	s.mu.Unlock()
}

// Reconcile diffs the persisted (pipeline, environment) schedule rows
// against the workspace and the running River periodic jobs. Pipelines
// whose files are gone get archived tombstones (run history is untouched);
// tombstones whose pipeline reappears — same UUID, e.g. after a branch
// switch — are reactivated. Catch-up policies are applied for active rows.
func (s *Service) Reconcile(ctx context.Context) error {
	if s == nil || !s.schedulerOn {
		return nil
	}
	s.mu.Lock()
	client := s.riverClient
	s.mu.Unlock()
	if client == nil {
		return nil
	}

	if err := s.migrateLegacySchedules(ctx); err != nil {
		return err
	}

	rows, err := s.store.ListEnvSchedules(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	jobs := make([]*river.PeriodicJob, 0, len(rows)+1)
	for _, row := range rows {
		row := row
		ref, exists := s.resolveRef(ctx, row.PipelineUUID)

		switch row.Status {
		case ScheduleStatusActive, ScheduleStatusPaused:
			if !exists {
				_ = s.store.SetEnvScheduleStatus(ctx, row.PipelineUUID, row.Environment, ScheduleStatusArchived, ArchivedReasonMissing)
				continue
			}
		case ScheduleStatusArchived:
			if exists && row.ArchivedReason == ArchivedReasonMissing {
				_ = s.store.SetEnvScheduleStatus(ctx, row.PipelineUUID, row.Environment, ScheduleStatusActive, "")
				row.Status = ScheduleStatusActive
			} else {
				continue
			}
		default:
			continue
		}
		if row.Status != ScheduleStatusActive {
			continue
		}

		schedule, parseErr := parseSchedule(row.Cron, row.Timezone)
		if parseErr != nil {
			continue
		}
		next := schedule.Next(now)
		_ = s.store.SetEnvScheduleNextRun(ctx, row.PipelineUUID, row.Environment, &next)
		s.catchUp(ctx, client, row, ref, schedule)

		jobs = append(jobs, river.NewPeriodicJob(cronPeriodicSchedule{schedule: schedule}, func() (river.JobArgs, *river.InsertOpts) {
			args := pipelineRunJobArgs{
				PipelineUUID:      row.PipelineUUID,
				PipelineName:      ref.Name,
				Environment:       row.Environment,
				Trigger:           RunTriggerSchedule,
				Schedule:          row.Cron,
				Timezone:          row.Timezone,
				SnapshotVersionID: row.SnapshotVersionID,
			}
			if start, end, ok := previousScheduleInterval(schedule, time.Now().UTC()); ok {
				args.Start = start.UTC().Format(time.RFC3339Nano)
				args.End = end.UTC().Format(time.RFC3339Nano)
			}
			return args, pipelineRunInsertOpts()
		}, &river.PeriodicJobOpts{ID: "schedule:" + row.PipelineUUID + ":" + row.Environment}))
	}

	if s.housekeeping != nil {
		jobs = append(jobs, river.NewPeriodicJob(river.PeriodicInterval(24*time.Hour), func() (river.JobArgs, *river.InsertOpts) {
			return housekeepingJobArgs{}, &river.InsertOpts{MaxAttempts: 1, Queue: pipelineRunQueue}
		}, &river.PeriodicJobOpts{ID: "renart-housekeeping", RunOnStart: true}))
	}
	client.PeriodicJobs().Clear()
	_, err = client.PeriodicJobs().AddManySafely(jobs)
	return err
}

func (s *Service) resolveRef(ctx context.Context, pipelineUUID string) (PipelineRef, bool) {
	if s.resolvePipelineRef == nil {
		return PipelineRef{}, false
	}
	return s.resolvePipelineRef(ctx, pipelineUUID)
}

// migrateLegacySchedules converts the pre-Phase-5 single-environment
// schedule settings (pipeline.yml schedule + enabled flag) into explicit
// (pipeline, environment) rows, once. The implicit "default" path is gone
// afterwards: every schedule names its environment.
func (s *Service) migrateLegacySchedules(ctx context.Context) error {
	count, err := s.store.CountEnvSchedules(ctx)
	if err != nil || count > 0 {
		return err
	}
	if s.pipelines == nil {
		return nil
	}
	items, err := s.pipelines(ctx)
	if err != nil {
		return err
	}
	environment := "default"
	if s.defaultEnvironment != nil {
		if name := strings.TrimSpace(s.defaultEnvironment()); name != "" {
			environment = name
		}
	}
	for _, item := range items {
		enabled, ok, err := s.store.ScheduleEnabled(ctx, item.PipelineID)
		if err != nil {
			return err
		}
		// The schedule_enabled flag is an explicit *override*: when set it wins,
		// but its absence means the legacy default — a pipeline with a schedule
		// string is enabled (mirroring PipelineService.GetSchedule, where
		// Enabled = schedule != ""). Requiring an explicit row would skip every
		// config-defined schedule, since nothing writes that flag outside tests,
		// leaving the redesign Schedules page permanently empty.
		if ok && !enabled {
			continue
		}
		if strings.TrimSpace(item.Schedule) == "" || item.PipelineUUID == "" {
			continue
		}
		policy := CatchupSkip
		if item.Catchup {
			policy = CatchupRunOnce
		}
		// Migrated rows keep snapshot_version_id empty: they execute the
		// latest snapshot if one exists, else the working tree, preserving
		// pre-migration behavior until the user deploys.
		if err := s.store.UpsertEnvSchedule(ctx, EnvSchedule{
			PipelineUUID:  item.PipelineUUID,
			Environment:   environment,
			Cron:          strings.TrimSpace(item.Schedule),
			Timezone:      item.Timezone,
			CatchupPolicy: policy,
			Status:        ScheduleStatusActive,
		}); err != nil {
			return err
		}
	}
	return nil
}

func watermarkKey(row EnvSchedule) string {
	return row.PipelineUUID + "|" + row.Environment
}

// catchUp applies the schedule's catch-up policy for intervals missed while
// the process was down (laptop closed overnight).
func (s *Service) catchUp(ctx context.Context, client *river.Client[*sql.Tx], row EnvSchedule, ref PipelineRef, schedule cron.Schedule) {
	key := watermarkKey(row)
	now := time.Now().UTC()
	_, prevEnd, found := previousScheduleInterval(schedule, now)
	if !found {
		return
	}
	last, ok, err := s.store.LastInterval(ctx, key)
	if err != nil {
		return
	}
	if !ok {
		// First reconcile for this schedule: start tracking from here, no
		// retroactive catch-up.
		_ = s.store.SetInterval(ctx, key, prevEnd)
		return
	}
	if !last.Before(prevEnd) {
		return
	}

	insertCatchupJob := func(start, end time.Time) {
		args := pipelineRunJobArgs{
			PipelineUUID:      row.PipelineUUID,
			PipelineName:      ref.Name,
			Environment:       row.Environment,
			Trigger:           RunTriggerSchedule,
			Schedule:          row.Cron,
			Timezone:          row.Timezone,
			Start:             start.UTC().Format(time.RFC3339Nano),
			End:               end.UTC().Format(time.RFC3339Nano),
			SnapshotVersionID: row.SnapshotVersionID,
		}
		_, _ = client.Insert(ctx, args, pipelineRunInsertOpts())
	}

	switch row.CatchupPolicy {
	case CatchupRunOnce:
		insertCatchupJob(last, prevEnd)
	case CatchupBackfill:
		cursor := last
		const maxBackfillRuns = 100
		for count := 0; count < maxBackfillRuns; count++ {
			next := schedule.Next(cursor)
			if !next.After(cursor) || next.After(prevEnd) {
				break
			}
			insertCatchupJob(cursor, next)
			cursor = next
		}
	default: // CatchupSkip
		_ = s.store.SetInterval(ctx, key, prevEnd)
	}
}

func (s *Service) ListSchedules(ctx context.Context) ([]PipelineSchedule, error) {
	if s.pipelines == nil {
		return nil, nil
	}
	items, err := s.pipelines(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if err := s.applyScheduleSettings(ctx, &items[i]); err != nil {
			return nil, err
		}
		if !items[i].Enabled || strings.TrimSpace(items[i].Schedule) == "" {
			continue
		}
		schedule, err := parseSchedule(items[i].Schedule, items[i].Timezone)
		if err == nil {
			next := schedule.Next(time.Now())
			items[i].NextRunAt = &next
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].PipelineName < items[j].PipelineName })
	return items, nil
}

// applyScheduleSettings derives the legacy single-flag Enabled view from
// the per-environment schedule rows: enabled means any active row exists
// for the pipeline. Falls back to the pre-migration settings table while
// no rows exist yet.
func (s *Service) applyScheduleSettings(ctx context.Context, item *PipelineSchedule) error {
	if s.store == nil || item == nil {
		return nil
	}
	if item.PipelineUUID != "" {
		rows, err := s.store.ListEnvSchedules(ctx)
		if err != nil {
			return err
		}
		seen := false
		enabled := false
		for _, row := range rows {
			if row.PipelineUUID != item.PipelineUUID {
				continue
			}
			seen = true
			if row.Status == ScheduleStatusActive {
				enabled = true
				break
			}
		}
		if seen {
			item.Enabled = enabled && strings.TrimSpace(item.Schedule) != ""
			return nil
		}
	}
	enabled, ok, err := s.store.ScheduleEnabled(ctx, item.PipelineID)
	if err != nil {
		return err
	}
	if ok {
		item.Enabled = enabled && strings.TrimSpace(item.Schedule) != ""
	}
	return nil
}

// ListAllEnvSchedules returns the live (active/paused) rows and the
// archived tombstones, with presentation fields resolved.
func (s *Service) ListAllEnvSchedules(ctx context.Context) (live []EnvSchedule, archived []EnvSchedule, err error) {
	rows, err := s.store.ListEnvSchedules(ctx)
	if err != nil {
		return nil, nil, err
	}
	live = make([]EnvSchedule, 0, len(rows))
	archived = make([]EnvSchedule, 0)
	for _, row := range rows {
		if ref, ok := s.resolveRef(ctx, row.PipelineUUID); ok {
			row.PipelineID = ref.EncodedID
			row.PipelineName = ref.Name
		}
		if row.Status == ScheduleStatusActive {
			if schedule, parseErr := parseSchedule(row.Cron, row.Timezone); parseErr == nil {
				next := schedule.Next(time.Now())
				row.NextRunAt = &next
			}
		}
		if row.PipelineID != "" {
			if list, listErr := s.store.List(ctx, RunFilter{PipelineID: row.PipelineID, Environment: row.Environment, Limit: 1}); listErr == nil && len(list.Runs) > 0 {
				lastRun := list.Runs[0]
				row.LastRun = &lastRun
			}
		}
		if row.Status == ScheduleStatusArchived {
			archived = append(archived, row)
		} else {
			live = append(live, row)
		}
	}
	sort.Slice(live, func(i, j int) bool {
		if live[i].PipelineName != live[j].PipelineName {
			return live[i].PipelineName < live[j].PipelineName
		}
		return live[i].Environment < live[j].Environment
	})
	return live, archived, nil
}

// UpsertEnvSchedule creates or updates the schedule for one (pipeline,
// environment). Enabling requires a deployed snapshot: pass an explicit
// version, set DeployNow, or rely on an already-pinned version.
func (s *Service) UpsertEnvSchedule(ctx context.Context, pipelineUUID string, req UpsertEnvScheduleRequest) (EnvSchedule, error) {
	environment := strings.TrimSpace(req.Environment)
	if environment == "" {
		return EnvSchedule{}, errors.New("environment is required: per-environment schedules have no implicit default")
	}
	cronExpr := strings.TrimSpace(req.Cron)
	if cronExpr == "" {
		return EnvSchedule{}, errors.New("cron expression is required")
	}
	timezone := strings.TrimSpace(req.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	if _, err := parseSchedule(cronExpr, timezone); err != nil {
		return EnvSchedule{}, fmt.Errorf("invalid cron expression: %w", err)
	}
	policy := req.CatchupPolicy
	if policy == "" {
		policy = CatchupSkip
	}
	switch policy {
	case CatchupSkip, CatchupRunOnce, CatchupBackfill:
	default:
		return EnvSchedule{}, fmt.Errorf("invalid catchup policy %q", policy)
	}
	if policy == CatchupBackfill && s.pipelineIntervalAware != nil && !s.pipelineIntervalAware(ctx, pipelineUUID) {
		return EnvSchedule{}, errors.New("backfill catch-up requires interval-aware assets (incremental materialization)")
	}

	existing, found, err := s.store.GetEnvSchedule(ctx, pipelineUUID, environment)
	if err != nil {
		return EnvSchedule{}, err
	}

	snapshotVersionID := strings.TrimSpace(req.SnapshotVersionID)
	if snapshotVersionID == "" && found {
		snapshotVersionID = existing.SnapshotVersionID
	}
	if req.DeployNow {
		if s.deployPipeline == nil {
			return EnvSchedule{}, errors.New("deploy is not available")
		}
		deployed, deployErr := s.deployPipeline(ctx, pipelineUUID)
		if deployErr != nil {
			return EnvSchedule{}, fmt.Errorf("deploy failed: %w", deployErr)
		}
		snapshotVersionID = deployed
	}
	if snapshotVersionID == "" && !found {
		return EnvSchedule{}, errors.New("a deployed snapshot is required to schedule this pipeline; deploy it first or pass deploy_now")
	}

	status := ScheduleStatusActive
	if req.Paused {
		status = ScheduleStatusPaused
	}
	schedule := EnvSchedule{
		PipelineUUID:      pipelineUUID,
		Environment:       environment,
		SnapshotVersionID: snapshotVersionID,
		Cron:              cronExpr,
		Timezone:          timezone,
		Vars:              req.Vars,
		CatchupPolicy:     policy,
		Status:            status,
	}
	if found {
		schedule.CreatedAt = existing.CreatedAt
	}
	if err := s.store.UpsertEnvSchedule(ctx, schedule); err != nil {
		return EnvSchedule{}, err
	}
	if err := s.Reconcile(ctx); err != nil {
		return EnvSchedule{}, err
	}
	updated, _, err := s.store.GetEnvSchedule(ctx, pipelineUUID, environment)
	if err != nil {
		return EnvSchedule{}, err
	}
	if ref, ok := s.resolveRef(ctx, pipelineUUID); ok {
		updated.PipelineID = ref.EncodedID
		updated.PipelineName = ref.Name
	}
	return updated, nil
}

// SetEnvScheduleLifecycle pauses or resumes one (pipeline, environment).
func (s *Service) SetEnvScheduleLifecycle(ctx context.Context, pipelineUUID, environment string, status ScheduleStatus) error {
	if status != ScheduleStatusActive && status != ScheduleStatusPaused {
		return fmt.Errorf("invalid schedule status %q", status)
	}
	_, found, err := s.store.GetEnvSchedule(ctx, pipelineUUID, environment)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("schedule not found")
	}
	if err := s.store.SetEnvScheduleStatus(ctx, pipelineUUID, environment, status, ""); err != nil {
		return err
	}
	return s.Reconcile(ctx)
}

// ArchiveEnvSchedule is the user-facing delete: the row becomes a tombstone
// (run history stays addressable) and is never auto-restored.
func (s *Service) ArchiveEnvSchedule(ctx context.Context, pipelineUUID, environment string) error {
	if err := s.store.SetEnvScheduleStatus(ctx, pipelineUUID, environment, ScheduleStatusArchived, ArchivedReasonUser); err != nil {
		return err
	}
	return s.Reconcile(ctx)
}

func (s *Service) Trigger(ctx context.Context, pipeline PipelineSchedule, req TriggerRequest) (PipelineRun, error) {
	if s.store == nil || s.runner == nil {
		return PipelineRun{}, errors.New("scheduler is not configured")
	}
	s.mu.Lock()
	client := s.riverClient
	s.mu.Unlock()
	if client == nil {
		return PipelineRun{}, errors.New("scheduler is not running")
	}
	active, err := s.store.HasActiveRun(ctx, pipeline.PipelineID)
	if err != nil {
		return PipelineRun{}, err
	}
	if active {
		return PipelineRun{}, fmt.Errorf("pipeline %s already has a queued or running run", pipeline.PipelineName)
	}

	trigger := RunTrigger(strings.TrimSpace(req.Trigger))
	if trigger == "" {
		trigger = RunTriggerManual
	}
	run := PipelineRun{
		PipelineID:  pipeline.PipelineID,
		Pipeline:    pipeline.PipelineName,
		Environment: strings.TrimSpace(req.Environment),
		Trigger:     trigger,
		Status:      RunStatusQueued,
		WinStart:    parseOptionalRequestTime(req.Start),
		WinEnd:      parseOptionalRequestTime(req.End),
	}
	id, err := s.store.Create(ctx, run)
	if err != nil {
		return PipelineRun{}, err
	}
	run.ID = id
	s.publishRunEvent("run.queued", run)

	if _, err := client.Insert(ctx, pipelineRunJobArgs{RunID: run.ID}, pipelineRunInsertOpts()); err != nil {
		_ = s.store.Finish(ctx, run.ID, RunStatusFailed, err)
		return PipelineRun{}, err
	}
	return run, nil
}

func (s *Service) ListRuns(ctx context.Context, filter RunFilter) (RunList, error) {
	return s.store.List(ctx, filter)
}

func (s *Service) GetRun(ctx context.Context, id string) (PipelineRun, []LogLine, []PipelineRunStep, error) {
	return s.store.Get(ctx, id)
}

func (s *Service) prepareRun(ctx context.Context, args pipelineRunJobArgs) (PipelineRun, bool, error) {
	if strings.TrimSpace(args.RunID) != "" {
		run, _, _, err := s.store.Get(ctx, args.RunID)
		if err != nil {
			return PipelineRun{}, false, err
		}
		if run.Status != RunStatusQueued {
			return PipelineRun{}, false, nil
		}
		return run, true, nil
	}

	// Per-environment scheduled runs: resolve the stable UUID to the
	// current workspace incarnation; a missing pipeline skips silently (the
	// reconciler will archive the schedule).
	pipelineUUID := strings.TrimSpace(args.PipelineUUID)
	encodedPipelineID := strings.TrimSpace(args.PipelineID)
	pipelineName := args.PipelineName
	if pipelineUUID != "" {
		ref, ok := s.resolveRef(ctx, pipelineUUID)
		if !ok {
			return PipelineRun{}, false, nil
		}
		encodedPipelineID = ref.EncodedID
		if ref.Name != "" {
			pipelineName = ref.Name
		}
	}

	if encodedPipelineID == "" {
		return PipelineRun{}, false, errors.New("pipeline id is required")
	}
	active, err := s.store.HasActiveRun(ctx, encodedPipelineID)
	if err != nil || active {
		return PipelineRun{}, false, err
	}
	trigger := args.Trigger
	if trigger == "" {
		trigger = RunTriggerSchedule
	}
	args.PipelineID = encodedPipelineID
	start, end := s.scheduledWindow(ctx, args, time.Now().UTC())
	if parsed := parseOptionalRequestTime(args.Start); parsed != nil {
		start = *parsed
	}
	if parsed := parseOptionalRequestTime(args.End); parsed != nil {
		end = *parsed
	}
	if !start.Before(end) {
		start = end.Add(-time.Minute)
	}
	run := PipelineRun{
		PipelineID:        encodedPipelineID,
		PipelineUUID:      pipelineUUID,
		Pipeline:          pipelineName,
		Environment:       strings.TrimSpace(args.Environment),
		Trigger:           trigger,
		Status:            RunStatusQueued,
		WinStart:          &start,
		WinEnd:            &end,
		SnapshotVersionID: args.SnapshotVersionID,
	}
	id, err := s.store.Create(ctx, run)
	if err != nil {
		return PipelineRun{}, false, err
	}
	run.ID = id
	// Create persists SnapshotVersionID but PipelineUUID is in-memory only;
	// re-attach it after the round trip for the watermark update.
	run.PipelineUUID = pipelineUUID
	s.publishRunEvent("run.queued", run)
	return run, true, nil
}

func (s *Service) execute(ctx context.Context, run PipelineRun) error {
	defer func() {
		if recovered := recover(); recovered != nil {
			err := fmt.Errorf("pipeline run panicked: %v", recovered)
			_ = s.store.Finish(context.Background(), run.ID, RunStatusFailed, err)
			run.Status = RunStatusFailed
			run.Error = err.Error()
			finished := time.Now().UTC()
			run.FinishedAt = &finished
			s.publishRunEvent("run.finished", run)
		}
	}()
	run = ensureScheduledRunWindow(run)
	started := time.Now().UTC()
	if err := s.store.MarkRunning(ctx, run.ID, started); err != nil {
		return err
	}
	run.Status = RunStatusRunning
	run.StartedAt = &started
	s.publishRunEvent("run.started", run)

	req := RunRequest{RunID: run.ID, PipelineID: run.PipelineID, Environment: run.Environment, SnapshotVersionID: run.SnapshotVersionID}
	req.OnStep = func(event RunStepEvent) {
		s.persistRunStep(ctx, run.ID, event)
	}
	if run.WinStart != nil {
		req.Start = run.WinStart.Format(time.RFC3339Nano)
	}
	if run.WinEnd != nil {
		req.End = run.WinEnd.Format(time.RFC3339Nano)
	}
	result := s.runner(ctx, req, func(line string) {
		logLine := LogLine{At: time.Now().UTC(), Line: line}
		_ = s.store.AppendLog(ctx, run.ID, logLine)
		s.publishRunEvent("run.log", map[string]any{"run_id": run.ID, "log": logLine})
	})
	status, runErr := statusFromResult(result)
	if err := s.store.Finish(ctx, run.ID, status, runErr); err != nil {
		_ = s.store.AppendLog(ctx, run.ID, LogLine{At: time.Now().UTC(), Line: "failed to persist run status: " + err.Error()})
		return err
	}
	if status == RunStatusSuccess && run.Trigger == RunTriggerSchedule && run.WinEnd != nil {
		watermark := run.PipelineID
		if run.PipelineUUID != "" {
			watermark = run.PipelineUUID + "|" + run.Environment
		}
		_ = s.store.SetInterval(ctx, watermark, *run.WinEnd)
	}
	finished := time.Now().UTC()
	_ = s.store.FinishOpenSteps(ctx, run.ID, status, finished, runErr)
	run.Status = status
	run.FinishedAt = &finished
	if runErr != nil {
		run.Error = runErr.Error()
	}
	s.publishRunEvent("run.finished", run)
	return nil
}

func (s *Service) persistRunStep(ctx context.Context, runID string, event RunStepEvent) {
	asset := strings.TrimSpace(event.Asset)
	if asset == "" {
		return
	}
	step := PipelineRunStep{RunID: runID, Asset: asset, Status: event.Status, StartedAt: event.StartedAt, FinishedAt: event.FinishedAt, Error: event.Error}
	if step.Status == "" {
		step.Status = RunStatusRunning
	}
	if err := s.store.UpsertStep(ctx, step); err == nil {
		s.publishRunEvent("run.step", step)
	}
}

func (s *Service) windowStart(ctx context.Context, pipelineID string, end time.Time) time.Time {
	if last, ok, err := s.store.LastInterval(ctx, pipelineID); err == nil && ok && !last.IsZero() {
		return last
	}
	return end
}

func (s *Service) scheduledWindow(ctx context.Context, args pipelineRunJobArgs, now time.Time) (time.Time, time.Time) {
	end := now.UTC()
	start := s.windowStart(ctx, args.PipelineID, end)
	if strings.TrimSpace(args.Schedule) != "" {
		if schedule, err := parseSchedule(args.Schedule, args.Timezone); err == nil {
			if scheduledStart, scheduledEnd, ok := previousScheduleInterval(schedule, end); ok {
				end = scheduledEnd
				if start.Before(end) {
					return start, end
				}
				return scheduledStart, end
			}
		}
	}
	if start.Before(end) {
		return start, end
	}
	return end.Add(-time.Minute), end
}

func previousScheduleInterval(schedule cron.Schedule, now time.Time) (time.Time, time.Time, bool) {
	for _, lookback := range []time.Duration{2 * time.Hour, 2 * 24 * time.Hour, 35 * 24 * time.Hour, 370 * 24 * time.Hour} {
		cursor := now.Add(-lookback)
		var beforePrevious time.Time
		var previous time.Time
		for i := 0; i < 10000; i++ {
			next := schedule.Next(cursor)
			if next.After(now) {
				if !beforePrevious.IsZero() && !previous.IsZero() {
					return beforePrevious, previous, true
				}
				break
			}
			beforePrevious = previous
			previous = next
			cursor = next
		}
	}
	return time.Time{}, time.Time{}, false
}

func ensureScheduledRunWindow(run PipelineRun) PipelineRun {
	if run.Trigger != RunTriggerSchedule || run.WinStart == nil || run.WinEnd == nil || run.WinStart.Before(*run.WinEnd) {
		return run
	}
	fixedStart := run.WinEnd.Add(-time.Minute)
	run.WinStart = &fixedStart
	return run
}

func (s *Service) publishRunEvent(eventType string, payload any) {
	if s.publish == nil {
		return
	}
	s.publish(map[string]any{"type": eventType, "run": payload})
}

func pipelineRunInsertOpts() *river.InsertOpts {
	return &river.InsertOpts{
		MaxAttempts: 1,
		Queue:       pipelineRunQueue,
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
			ByState: []rivertype.JobState{
				rivertype.JobStateAvailable,
				rivertype.JobStatePending,
				rivertype.JobStateRunning,
				rivertype.JobStateScheduled,
			},
		},
	}
}

func parseSchedule(expr, timezone string) (cron.Schedule, error) {
	location := time.UTC
	if strings.TrimSpace(timezone) != "" {
		loaded, err := time.LoadLocation(strings.TrimSpace(timezone))
		if err != nil {
			return nil, err
		}
		location = loaded
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if strings.HasPrefix(strings.TrimSpace(expr), "@") {
		return parser.Parse(expr)
	}
	return parser.Parse("CRON_TZ=" + location.String() + " " + expr)
}

func parseOptionalRequestTime(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	return &parsed
}
