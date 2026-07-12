package runstate

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLookupInFlightAndWait(t *testing.T) {
	r := NewRegistry()
	run := r.BeginRun("run-1", "default", []string{"Chess_Games", "player_stats"})
	defer run.End()

	finish := run.BeginTask("chess_games")

	lookup := r.Lookup("CHESS_GAMES", "default", "run-1")
	if lookup.InFlight == nil {
		t.Fatal("expected an in-flight task")
	}
	if lookup.PendingInRun {
		t.Fatal("running task must not be reported as pending")
	}

	failure := errors.New("boom")
	go func() {
		time.Sleep(10 * time.Millisecond)
		finish(failure)
	}()
	if err := lookup.InFlight.Wait(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("expected task failure, got %v", err)
	}

	after := r.Lookup("chess_games", "default", "run-1")
	if after.InFlight != nil {
		t.Fatal("finished task must leave the in-flight set")
	}
}

func TestLookupPendingInRunOnlyForOwnRun(t *testing.T) {
	r := NewRegistry()
	run := r.BeginRun("run-1", "default", []string{"a", "b"})
	defer run.End()

	if !r.Lookup("b", "default", "run-1").PendingInRun {
		t.Fatal("planned unstarted asset must be pending for its own run")
	}
	if r.Lookup("b", "default", "other-run").PendingInRun {
		t.Fatal("pending must not leak into other runs")
	}
	if r.Lookup("b", "prod", "run-1").PendingInRun {
		t.Fatal("environment must scope the lookup")
	}

	finish := run.BeginTask("b")
	finish(nil)
	if r.Lookup("b", "default", "run-1").PendingInRun {
		t.Fatal("done asset must not be pending")
	}
}

func TestEndFailsOrphanedTasks(t *testing.T) {
	r := NewRegistry()
	run := r.BeginRun("run-1", "default", []string{"a"})
	_ = run.BeginTask("a")

	lookup := r.Lookup("a", "default", "run-2")
	if lookup.InFlight == nil {
		t.Fatal("expected in-flight task")
	}

	run.End()
	select {
	case <-lookup.InFlight.Done():
	case <-time.After(time.Second):
		t.Fatal("End must release waiters of unfinished tasks")
	}
	if lookup.InFlight.Err() == nil {
		t.Fatal("orphaned task must carry an error")
	}
	if r.Lookup("a", "default", "run-2").InFlight != nil {
		t.Fatal("End must clear in-flight entries")
	}
}

func TestFinishIdempotent(t *testing.T) {
	r := NewRegistry()
	run := r.BeginRun("run-1", "default", nil)
	finish := run.BeginTask("a")
	finish(nil)
	finish(errors.New("second call must be ignored"))
	run.End()
}
