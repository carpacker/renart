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
	store        *Store
	runner       func(context.Context, RunRequest, func(string)) RunResult
	pipelines    PipelineSource
	publish      func(any)
	stateDir     string
	lock         *flock.Flock
	riverClient  *river.Client[*sql.Tx]
	mu           sync.Mutex
	schedulerOn  bool
	ownerMessage string
}

type Options struct {
	Store     *Store
	Runner    func(context.Context, RunRequest, func(string)) RunResult
	Pipelines PipelineSource
	Publish   func(any)
	StateDir  string
}

type pipelineRunJobArgs struct {
	RunID        string     `json:"run_id,omitempty" river:"unique"`
	PipelineID   string     `json:"pipeline_id,omitempty" river:"unique"`
	PipelineName string     `json:"pipeline_name,omitempty"`
	Environment  string     `json:"environment,omitempty"`
	Trigger      RunTrigger `json:"trigger,omitempty"`
	Schedule     string     `json:"schedule,omitempty"`
	Timezone     string     `json:"timezone,omitempty"`
	Start        string     `json:"start,omitempty"`
	End          string     `json:"end,omitempty"`
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

type cronPeriodicSchedule struct {
	schedule cron.Schedule
}

func (s cronPeriodicSchedule) Next(current time.Time) time.Time {
	return s.schedule.Next(current)
}

func New(options Options) *Service {
	return &Service{store: options.Store, runner: options.Runner, pipelines: options.Pipelines, publish: options.Publish, stateDir: options.StateDir}
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

func (s *Service) Reconcile(ctx context.Context) error {
	if s == nil || !s.schedulerOn || s.pipelines == nil {
		return nil
	}
	pipelines, err := s.pipelines(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	client := s.riverClient
	s.mu.Unlock()
	if client == nil {
		return nil
	}
	jobs := make([]*river.PeriodicJob, 0, len(pipelines))
	for _, item := range pipelines {
		item := item
		if !item.Enabled || strings.TrimSpace(item.Schedule) == "" {
			continue
		}
		schedule, err := parseSchedule(item.Schedule, item.Timezone)
		if err != nil {
			continue
		}
		jobs = append(jobs, river.NewPeriodicJob(cronPeriodicSchedule{schedule: schedule}, func() (river.JobArgs, *river.InsertOpts) {
			return pipelineRunJobArgs{PipelineID: item.PipelineID, PipelineName: item.PipelineName, Trigger: RunTriggerSchedule, Schedule: item.Schedule, Timezone: item.Timezone}, pipelineRunInsertOpts()
		}, &river.PeriodicJobOpts{ID: "pipeline:" + item.PipelineID}))
	}
	client.PeriodicJobs().Clear()
	_, err = client.PeriodicJobs().AddManySafely(jobs)
	return err
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

func (s *Service) ListRuns(ctx context.Context, filter RunFilter) ([]PipelineRun, error) {
	return s.store.List(ctx, filter)
}

func (s *Service) GetRun(ctx context.Context, id string) (PipelineRun, []LogLine, error) {
	return s.store.Get(ctx, id)
}

func (s *Service) prepareRun(ctx context.Context, args pipelineRunJobArgs) (PipelineRun, bool, error) {
	if strings.TrimSpace(args.RunID) != "" {
		run, _, err := s.store.Get(ctx, args.RunID)
		if err != nil {
			return PipelineRun{}, false, err
		}
		if run.Status != RunStatusQueued {
			return PipelineRun{}, false, nil
		}
		return run, true, nil
	}

	if strings.TrimSpace(args.PipelineID) == "" {
		return PipelineRun{}, false, errors.New("pipeline id is required")
	}
	active, err := s.store.HasActiveRun(ctx, args.PipelineID)
	if err != nil || active {
		return PipelineRun{}, false, err
	}
	trigger := args.Trigger
	if trigger == "" {
		trigger = RunTriggerSchedule
	}
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
		PipelineID:  args.PipelineID,
		Pipeline:    args.PipelineName,
		Environment: strings.TrimSpace(args.Environment),
		Trigger:     trigger,
		Status:      RunStatusQueued,
		WinStart:    &start,
		WinEnd:      &end,
	}
	id, err := s.store.Create(ctx, run)
	if err != nil {
		return PipelineRun{}, false, err
	}
	run.ID = id
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

	req := RunRequest{PipelineID: run.PipelineID, Environment: run.Environment}
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
		_ = s.store.SetInterval(ctx, run.PipelineID, *run.WinEnd)
	}
	finished := time.Now().UTC()
	run.Status = status
	run.FinishedAt = &finished
	if runErr != nil {
		run.Error = runErr.Error()
	}
	s.publishRunEvent("run.finished", run)
	return nil
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
