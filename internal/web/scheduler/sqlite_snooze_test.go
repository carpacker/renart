package scheduler

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sqliteSnoozeTestArgs struct{}

func (sqliteSnoozeTestArgs) Kind() string { return "renart-test-sqlite-snooze" }

type sqliteSnoozeTestWorker struct {
	river.WorkerDefaults[sqliteSnoozeTestArgs]
	attempts atomic.Int32
	done     chan struct{}
}

func (w *sqliteSnoozeTestWorker) Work(context.Context, *river.Job[sqliteSnoozeTestArgs]) error {
	if w.attempts.Add(1) == 1 {
		return river.JobSnooze(100 * time.Millisecond)
	}
	close(w.done)
	return nil
}

func TestRiverSQLiteSnoozedJobBecomesRunnableAgain(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	worker := &sqliteSnoozeTestWorker{done: make(chan struct{})}
	workers := river.NewWorkers()
	river.AddWorker(workers, worker)
	client, err := river.NewClient(riversqlite.New(store.db), &river.Config{
		FetchPollInterval: 100 * time.Millisecond,
		MaxAttempts:       1,
		PollOnly:          true,
		Queues: map[string]river.QueueConfig{
			pipelineRunQueue: {MaxWorkers: 1},
		},
		Workers: workers,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, client.Start(ctx))
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		require.NoError(t, client.Stop(stopCtx))
	}()

	inserted, err := client.Insert(ctx, sqliteSnoozeTestArgs{}, &river.InsertOpts{
		MaxAttempts: 1,
		Queue:       pipelineRunQueue,
	})
	require.NoError(t, err)
	select {
	case <-worker.done:
	case <-time.After(5 * time.Second):
		t.Fatal("snoozed River job did not become runnable again")
	}
	assert.EqualValues(t, 2, worker.attempts.Load())

	var scheduledAt string
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var state string
		err := store.db.QueryRowContext(context.Background(), `
			SELECT state, scheduled_at FROM river_job WHERE id = ?`, inserted.Job.ID).Scan(&state, &scheduledAt)
		require.NoError(collect, err)
		assert.Equal(collect, "completed", state)
	}, 5*time.Second, 25*time.Millisecond)
	assert.NotContains(t, scheduledAt, "m=")
	var parseable bool
	require.NoError(t, store.db.QueryRow(`SELECT julianday(?) IS NOT NULL`, scheduledAt).Scan(&parseable))
	assert.True(t, parseable)
}
