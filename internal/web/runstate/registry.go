// Package runstate tracks materialization tasks that are currently in flight
// in this process, so a consumer (the python asset broker) can wait for an
// asset's materialization to finish instead of reading a half-built table.
//
// Every run path registers here: single-asset materialize, pipeline runs
// (interactive, CLI-delegated, scheduled snapshot), and build-stale plans all
// funnel through HybridBruinExecutor.RunAsset/RunPipeline. The registry is
// in-process only — it mirrors the runtime model where one process owns all
// execution for a workspace.
package runstate

import (
	"context"
	"strings"
	"sync"
)

type taskKey struct {
	asset       string
	environment string
}

// Registry is the process-wide view of planned and in-flight tasks.
type Registry struct {
	mu       sync.Mutex
	inflight map[taskKey]*Task
	runs     map[string]*Run
}

func NewRegistry() *Registry {
	return &Registry{
		inflight: make(map[taskKey]*Task),
		runs:     make(map[string]*Run),
	}
}

// Run is one materialization run (single asset, pipeline, or stale plan)
// with its planned asset set.
type Run struct {
	registry    *Registry
	id          string
	environment string
	state       map[string]runTaskState
}

type runTaskState int

const (
	statePending runTaskState = iota
	stateRunning
	stateDone
)

// Task is one in-flight materialization of an asset. Wait on Done(); Err()
// is valid once Done() is closed.
type Task struct {
	Asset       string
	Environment string
	RunID       string

	registry *Registry
	done     chan struct{}
	err      error
	finished bool
	mu       sync.Mutex
}

func (t *Task) Done() <-chan struct{} { return t.done }

func (t *Task) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

// Wait blocks until the task finishes or ctx is cancelled, returning the
// task's outcome (or the context error).
func (t *Task) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.done:
		return t.Err()
	}
}

func normalizeAsset(asset string) string {
	return strings.ToLower(strings.TrimSpace(asset))
}

// BeginRun registers a run and its planned assets. Call End() when the run
// finishes so pending entries don't linger.
func (r *Registry) BeginRun(runID, environment string, planned []string) *Run {
	run := &Run{
		registry:    r,
		id:          runID,
		environment: environment,
		state:       make(map[string]runTaskState, len(planned)),
	}
	for _, asset := range planned {
		run.state[normalizeAsset(asset)] = statePending
	}
	r.mu.Lock()
	r.runs[runID] = run
	r.mu.Unlock()
	return run
}

// BeginTask marks an asset's materialization as in flight. The returned
// finish function must be called exactly once with the task's outcome.
func (run *Run) BeginTask(asset string) func(error) {
	name := normalizeAsset(asset)
	key := taskKey{asset: name, environment: run.environment}
	task := &Task{
		Asset:       name,
		Environment: run.environment,
		RunID:       run.id,
		registry:    run.registry,
		done:        make(chan struct{}),
	}

	r := run.registry
	r.mu.Lock()
	run.state[name] = stateRunning
	r.inflight[key] = task
	r.mu.Unlock()

	return func(err error) {
		task.mu.Lock()
		if task.finished {
			task.mu.Unlock()
			return
		}
		task.finished = true
		task.err = err
		task.mu.Unlock()

		r.mu.Lock()
		if r.inflight[key] == task {
			delete(r.inflight, key)
		}
		run.state[name] = stateDone
		r.mu.Unlock()
		close(task.done)
	}
}

// End removes the run from the registry. In-flight tasks that were never
// finished are failed defensively so waiters don't hang.
func (run *Run) End() {
	r := run.registry
	r.mu.Lock()
	delete(r.runs, run.id)
	orphans := make([]*Task, 0)
	for name, state := range run.state {
		if state != stateRunning {
			continue
		}
		key := taskKey{asset: name, environment: run.environment}
		if task, ok := r.inflight[key]; ok && task.RunID == run.id {
			delete(r.inflight, key)
			orphans = append(orphans, task)
		}
	}
	r.mu.Unlock()
	for _, task := range orphans {
		task.mu.Lock()
		if !task.finished {
			task.finished = true
			task.err = context.Canceled
			task.mu.Unlock()
			close(task.done)
			continue
		}
		task.mu.Unlock()
	}
}

// Lookup describes what the registry knows about an asset in an environment,
// from the perspective of a task inside run runID.
type Lookup struct {
	// InFlight is the task currently materializing the asset (any run), nil
	// when none is running.
	InFlight *Task
	// PendingInRun is true when the asset is planned in run runID but has not
	// started yet — waiting on it from inside the same run would deadlock.
	PendingInRun bool
}

func (r *Registry) Lookup(asset, environment, runID string) Lookup {
	name := normalizeAsset(asset)
	r.mu.Lock()
	defer r.mu.Unlock()
	result := Lookup{}
	if task, ok := r.inflight[taskKey{asset: name, environment: environment}]; ok {
		result.InFlight = task
	}
	if run, ok := r.runs[runID]; ok && run.environment == environment {
		if state, planned := run.state[name]; planned && state == statePending {
			result.PendingInRun = true
		}
	}
	return result
}
