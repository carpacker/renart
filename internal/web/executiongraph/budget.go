package executiongraph

import (
	"context"
	"errors"
	"sync"
)

// Budget bounds active execution units across every run owned by one workspace
// runtime. Acquisitions are FIFO and each runner queues at most one request at
// a time, preventing one large run from filling the wait queue ahead of later
// interactive work.
type Budget struct {
	mu      sync.Mutex
	limit   int
	active  int
	waiters []*budgetWaiter
}

type budgetWaiter struct {
	ready chan struct{}
}

type BudgetLease struct {
	once    sync.Once
	release func()
}

func NewBudget(limit int) *Budget {
	if limit < 1 {
		limit = 1
	}
	return &Budget{limit: limit}
}

func (b *Budget) Limit() int {
	if b == nil {
		return 1
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.limit
}

func (b *Budget) Acquire(ctx context.Context) (*BudgetLease, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b == nil {
		return &BudgetLease{}, nil
	}

	b.mu.Lock()
	if b.active < b.limit && len(b.waiters) == 0 {
		b.active++
		b.mu.Unlock()
		return b.lease(), nil
	}
	waiter := &budgetWaiter{ready: make(chan struct{})}
	b.waiters = append(b.waiters, waiter)
	b.mu.Unlock()

	select {
	case <-waiter.ready:
		lease := b.lease()
		if err := ctx.Err(); err != nil {
			lease.Release()
			return nil, err
		}
		return lease, nil
	case <-ctx.Done():
		b.mu.Lock()
		removed := b.removeWaiterLocked(waiter)
		if !removed {
			// The waiter was granted concurrently with cancellation. Return its
			// reserved slot before reporting the context error.
			b.releaseLocked()
		}
		b.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (b *Budget) lease() *BudgetLease {
	return &BudgetLease{release: func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.releaseLocked()
	}}
}

func (b *Budget) releaseLocked() {
	if b.active > 0 {
		b.active--
	}
	for b.active < b.limit && len(b.waiters) > 0 {
		waiter := b.waiters[0]
		b.waiters = b.waiters[1:]
		b.active++
		close(waiter.ready)
	}
}

func (b *Budget) removeWaiterLocked(target *budgetWaiter) bool {
	for index, waiter := range b.waiters {
		if waiter != target {
			continue
		}
		copy(b.waiters[index:], b.waiters[index+1:])
		b.waiters = b.waiters[:len(b.waiters)-1]
		return true
	}
	return false
}

func (l *BudgetLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		if l.release != nil {
			l.release()
		}
	})
}

var errNilRun = errors.New("execution graph run function is required")
