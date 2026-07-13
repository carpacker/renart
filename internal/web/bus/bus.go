// Package bus is the single in-process seam that observes run completion and
// asset saves. The fingerprint cache, the materialization log, and the
// staleness service all attach here instead of each finding their own hook
// into the executor or the file watcher.
package bus

import (
	"sync"
	"time"
)

// AssetRun describes one asset that a completed run materialized.
type AssetRun struct {
	// AssetID is the durable identifier (pipeline UUID + ":" + asset name).
	AssetID   string
	AssetName string
	Status    string // "succeeded" / "failed" / "cancelled"
}

// RunCompleted is emitted once per finished run (build-mode single asset,
// build-mode pipeline run, or scheduled run).
type RunCompleted struct {
	RunID        string // scheduler run ID when applicable, "" for build-mode runs
	PipelineUUID string
	Environment  string
	// WinStart/WinEnd carry the requested execution interval. FullRefresh is
	// independent: a window-filtered full refresh still represents that window.
	WinStart    *time.Time
	WinEnd      *time.Time
	FullRefresh bool
	CompletedAt time.Time
	Assets      []AssetRun
	// SnapshotVersionID/SnapshotDir are set when the run executed a deployed
	// snapshot; SnapshotDir is the materialized source the run actually used
	// (valid for the duration of the event dispatch).
	SnapshotVersionID string
	SnapshotDir       string
}

// AssetSaved is emitted whenever an asset's saved state changes on disk,
// whether through the API or an external editor caught by the watcher.
type AssetSaved struct {
	PipelineUUID string
	AssetID      string // durable identifier
	AssetName    string
	Path         string // workspace-relative path
	SavedAt      time.Time
}

// Events is the seam consumers subscribe to. Handlers run synchronously on
// the emitting goroutine, in subscription order — keep them fast and never
// emit from inside a handler.
type Events interface {
	OnRunCompleted(func(RunCompleted)) (unsubscribe func())
	OnAssetSaved(func(AssetSaved)) (unsubscribe func())
}

// Bus is the canonical Events implementation. The zero value is unusable;
// call New.
type Bus struct {
	mu       sync.RWMutex
	nextID   int
	runSubs  map[int]func(RunCompleted)
	saveSubs map[int]func(AssetSaved)
}

func New() *Bus {
	return &Bus{
		runSubs:  make(map[int]func(RunCompleted)),
		saveSubs: make(map[int]func(AssetSaved)),
	}
}

func (b *Bus) OnRunCompleted(handler func(RunCompleted)) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	b.runSubs[id] = handler
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.runSubs, id)
	}
}

func (b *Bus) OnAssetSaved(handler func(AssetSaved)) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	b.saveSubs[id] = handler
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.saveSubs, id)
	}
}

func (b *Bus) EmitRunCompleted(event RunCompleted) {
	if b == nil {
		return
	}
	for _, handler := range b.snapshotRunSubs() {
		handler(event)
	}
}

func (b *Bus) EmitAssetSaved(event AssetSaved) {
	if b == nil {
		return
	}
	for _, handler := range b.snapshotSaveSubs() {
		handler(event)
	}
}

func (b *Bus) snapshotRunSubs() []func(RunCompleted) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	handlers := make([]func(RunCompleted), 0, len(b.runSubs))
	for id := 0; id < b.nextID; id++ {
		if handler, ok := b.runSubs[id]; ok {
			handlers = append(handlers, handler)
		}
	}
	return handlers
}

func (b *Bus) snapshotSaveSubs() []func(AssetSaved) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	handlers := make([]func(AssetSaved), 0, len(b.saveSubs))
	for id := 0; id < b.nextID; id++ {
		if handler, ok := b.saveSubs[id]; ok {
			handlers = append(handlers, handler)
		}
	}
	return handlers
}
