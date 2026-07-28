package executiongraph

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunnerOverlapsIndependentNodesWithinLimit(t *testing.T) {
	t.Parallel()
	nodes := []Node{{Position: 0}, {Position: 1}, {Position: 2}, {Position: 3}}
	release := make(chan struct{})
	started := make(chan int, len(nodes))
	var active atomic.Int64
	var peak atomic.Int64

	done := make(chan error, 1)
	go func() {
		done <- (Runner{MaxActive: 2, Workspace: NewBudget(8)}).Run(
			context.Background(),
			nodes,
			func(_ context.Context, node Node) error {
				current := active.Add(1)
				for {
					previous := peak.Load()
					if current <= previous || peak.CompareAndSwap(previous, current) {
						break
					}
				}
				started <- node.Position
				<-release
				active.Add(-1)
				return nil
			},
		)
	}()

	require.ElementsMatch(t, []int{0, 1}, []int{
		receiveInt(t, started),
		receiveInt(t, started),
	})
	assertNoInt(t, started)
	close(release)
	require.NoError(t, <-done)
	assert.Equal(t, int64(2), peak.Load())
}

func TestRunnerWaitsForDependencies(t *testing.T) {
	t.Parallel()
	nodes := []Node{
		{Position: 0},
		{Position: 1},
		{Position: 2, Dependencies: []int{0, 1}},
	}
	releaseRoots := make(chan struct{})
	rootsStarted := make(chan int, 2)
	dependentStarted := make(chan struct{}, 1)

	done := make(chan error, 1)
	go func() {
		done <- (Runner{MaxActive: 3, Workspace: NewBudget(3)}).Run(
			context.Background(),
			nodes,
			func(_ context.Context, node Node) error {
				if node.Position < 2 {
					rootsStarted <- node.Position
					<-releaseRoots
				} else {
					dependentStarted <- struct{}{}
				}
				return nil
			},
		)
	}()

	require.ElementsMatch(t, []int{0, 1}, []int{
		receiveInt(t, rootsStarted),
		receiveInt(t, rootsStarted),
	})
	select {
	case <-dependentStarted:
		t.Fatal("dependent started before its upstreams completed")
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseRoots)
	select {
	case <-dependentStarted:
	case <-time.After(time.Second):
		t.Fatal("dependent did not start")
	}
	require.NoError(t, <-done)
}

func TestRunnerSerializesResourcesAndConnectionLimits(t *testing.T) {
	t.Parallel()
	nodes := []Node{
		{Position: 0, Resources: []string{"duckdb:a"}, Connections: []string{"warehouse"}},
		{Position: 1, Resources: []string{"duckdb:a"}, Connections: []string{"warehouse"}},
		{Position: 2, Resources: []string{"duckdb:b"}, Connections: []string{"other"}},
	}
	release := make(chan struct{})
	started := make(chan int, len(nodes))

	done := make(chan error, 1)
	go func() {
		done <- (Runner{
			MaxActive: 3, Workspace: NewBudget(3),
			ConnectionLimits: map[string]int{"warehouse": 1},
		}).Run(context.Background(), nodes, func(_ context.Context, node Node) error {
			started <- node.Position
			<-release
			return nil
		})
	}()

	require.ElementsMatch(t, []int{0, 2}, []int{
		receiveInt(t, started),
		receiveInt(t, started),
	})
	assertNoInt(t, started)
	close(release)
	require.NoError(t, <-done)
}

func TestRunnerReservesAllResourcesAtomically(t *testing.T) {
	t.Parallel()
	nodes := []Node{
		{Position: 0, Resources: []string{"a", "b"}},
		{Position: 1, Resources: []string{"a"}},
		{Position: 2, Resources: []string{"b"}},
	}
	release := make(chan struct{})
	started := make(chan int, len(nodes))
	done := make(chan error, 1)
	go func() {
		done <- (Runner{MaxActive: 3, Workspace: NewBudget(3)}).Run(
			context.Background(),
			nodes,
			func(_ context.Context, node Node) error {
				started <- node.Position
				<-release
				return nil
			},
		)
	}()

	assert.Equal(t, 0, receiveInt(t, started))
	assertNoInt(t, started)
	close(release)
	require.NoError(t, <-done)
}

func TestRunnerRunsExclusiveNodeAlone(t *testing.T) {
	t.Parallel()
	nodes := []Node{
		{Position: 0},
		{Position: 1, Exclusive: true},
		{Position: 2},
	}
	var active atomic.Int64
	var exclusiveOverlap atomic.Bool

	require.NoError(t, (Runner{MaxActive: 3, Workspace: NewBudget(3)}).Run(
		context.Background(),
		nodes,
		func(_ context.Context, node Node) error {
			current := active.Add(1)
			if node.Exclusive && current != 1 {
				exclusiveOverlap.Store(true)
			}
			time.Sleep(5 * time.Millisecond)
			active.Add(-1)
			return nil
		},
	))
	assert.False(t, exclusiveOverlap.Load())
}

func TestRunnerUsesStablePlanOrderAtOneActiveUnit(t *testing.T) {
	t.Parallel()
	nodes := []Node{{Position: 0}, {Position: 1}, {Position: 2}}
	var order []int
	require.NoError(t, (Runner{MaxActive: 1, Workspace: NewBudget(4)}).Run(
		context.Background(),
		nodes,
		func(_ context.Context, node Node) error {
			order = append(order, node.Position)
			return nil
		},
	))
	assert.Equal(t, []int{0, 1, 2}, order)
}

func TestRunnerSkipsOnlyFailedDependents(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	nodes := []Node{
		{Position: 0},
		{Position: 1},
		{Position: 2, Dependencies: []int{0}},
		{Position: 3, Dependencies: []int{1}},
	}
	var ranMu sync.Mutex
	var ran []int
	var bypassed []Bypass
	runner := Runner{
		MaxActive: 2, Workspace: NewBudget(2),
		OnBypass: func(event Bypass) error {
			bypassed = append(bypassed, event)
			return nil
		},
	}
	err := runner.Run(context.Background(), nodes, func(_ context.Context, node Node) error {
		ranMu.Lock()
		ran = append(ran, node.Position)
		ranMu.Unlock()
		if node.Position == 0 {
			return boom
		}
		return nil
	})

	require.ErrorIs(t, err, boom)
	assert.ElementsMatch(t, []int{0, 1, 3}, ran)
	require.Len(t, bypassed, 1)
	assert.Equal(t, 2, bypassed[0].Node.Position)
	assert.Equal(t, []int{0}, bypassed[0].BlockedBy)
	assert.Equal(t, BypassSkipped, bypassed[0].Status)
}

func TestRunnerContainsWorkerPanicAndContinuesIndependentWork(t *testing.T) {
	t.Parallel()
	var ran atomic.Int64
	err := (Runner{MaxActive: 2, Workspace: NewBudget(2)}).Run(
		context.Background(),
		[]Node{{Position: 0}, {Position: 1}},
		func(_ context.Context, node Node) error {
			ran.Add(1)
			if node.Position == 0 {
				panic("broken operator")
			}
			return nil
		},
	)
	require.ErrorContains(t, err, "execution unit panicked: broken operator")
	assert.Equal(t, int64(2), ran.Load())
}

func TestRunnerCancellationDrainsActiveAndCancelsQueued(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	finished := make(chan struct{})
	var bypassed []Bypass
	runner := Runner{
		MaxActive: 1, Workspace: NewBudget(1),
		OnBypass: func(event Bypass) error {
			bypassed = append(bypassed, event)
			return nil
		},
	}
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx, []Node{{Position: 0}, {Position: 1}}, func(ctx context.Context, _ Node) error {
			close(started)
			<-ctx.Done()
			close(finished)
			return ctx.Err()
		})
	}()
	<-started
	cancel()
	<-finished
	err := <-done
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, bypassed, 1)
	assert.Equal(t, 1, bypassed[0].Node.Position)
	assert.Equal(t, BypassCancelled, bypassed[0].Status)
}

func TestBudgetQueuesAcquisitionsFIFO(t *testing.T) {
	t.Parallel()
	budget := NewBudget(1)
	first, err := budget.Acquire(context.Background())
	require.NoError(t, err)
	order := make(chan int, 2)
	releases := make(chan struct{})
	acquireErrors := make(chan error, 2)
	for index := 1; index <= 2; index++ {
		index := index
		go func() {
			lease, acquireErr := budget.Acquire(context.Background())
			if acquireErr != nil {
				acquireErrors <- acquireErr
				return
			}
			order <- index
			<-releases
			lease.Release()
			acquireErrors <- nil
		}()
		time.Sleep(5 * time.Millisecond)
	}
	first.Release()
	assert.Equal(t, 1, receiveInt(t, order))
	releases <- struct{}{}
	assert.Equal(t, 2, receiveInt(t, order))
	releases <- struct{}{}
	require.NoError(t, <-acquireErrors)
	require.NoError(t, <-acquireErrors)
}

func receiveInt(t *testing.T, values <-chan int) int {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for value")
		return 0
	}
}

func assertNoInt(t *testing.T, values <-chan int) {
	t.Helper()
	select {
	case value := <-values:
		t.Fatalf("unexpected value %d", value)
	case <-time.After(30 * time.Millisecond):
	}
}
