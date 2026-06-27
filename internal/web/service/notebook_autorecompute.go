package service

import (
	"context"
	"sort"
	"sync"
	"time"

	"renart/internal/web/notebook"
)

// The notebook recompute engine lives on the server: it owns per-notebook
// staleness and last results, and — when auto-recompute is on — recomputes the
// safe-to-run cells itself after every save, streaming results to clients over
// SSE. This replaces the per-wave save/parse/run round-trips the client used to
// orchestrate (see architecture/renart-notebook-backend-autorecompute.md).

const (
	notebookRuntimeEventType = "notebook.runtime"
	// How long to coalesce edits before a recompute pass — short, because the
	// validation gate (not this delay) is what keeps a downstream from running
	// against an out-of-date parse.
	autoRecomputeDebounce = 200 * time.Millisecond
)

// NotebookRuntimeEvent is pushed on the workspace SSE stream whenever a
// notebook's staleness, running set, or results change.
type NotebookRuntimeEvent struct {
	Type          string `json:"type"`
	NotebookID    string `json:"notebook_id"`
	AutoRecompute bool   `json:"auto_recompute"`
	// Stale is every cell that needs recomputing. AutoPending is the subset
	// auto-recompute will refresh on its own (this wave or a later one), so the
	// client shows the stale treatment only on Stale minus AutoPending.
	Stale       []string `json:"stale"`
	AutoPending []string `json:"auto_pending"`
	Running     []string `json:"running"`
	// Results carries the results that changed in this update (a delta the
	// client merges into its map).
	Results map[string]notebook.CellRunResult `json:"results,omitempty"`
}

// notebookRuntime is the in-memory recompute state for one notebook.
type notebookRuntime struct {
	mu            sync.Mutex
	stale         map[string]bool
	results       map[string]notebook.CellRunResult
	autoFailed    map[string]bool
	autoRecompute bool
	// environment selects the connection environment for upstream imports in
	// auto runs (set by the client via the settings endpoint).
	environment string

	debounce   *time.Timer
	passActive bool
	// cancelWave interrupts the currently-executing wave so the pass re-loops
	// with fresh content (a new edit superseded it).
	cancelWave context.CancelFunc
}

func newNotebookRuntime() *notebookRuntime {
	return &notebookRuntime{
		stale:         map[string]bool{},
		results:       map[string]notebook.CellRunResult{},
		autoFailed:    map[string]bool{},
		autoRecompute: true,
	}
}

// sortedKeys returns the truthy keys of a set as a sorted slice (stable event
// payloads and snapshots).
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		if set[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

type notebookRuntimes struct {
	mu   sync.Mutex
	byID map[string]*notebookRuntime
}

func newNotebookRuntimes() *notebookRuntimes {
	return &notebookRuntimes{byID: map[string]*notebookRuntime{}}
}

func (m *notebookRuntimes) get(uuid string) *notebookRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.byID[uuid]
	if !ok {
		rt = newNotebookRuntime()
		m.byID[uuid] = rt
	}
	return rt
}

// autoCellInfo is the per-cell input to the eligibility computation, mirroring
// the client's AutoRecomputeCell.
type autoCellInfo struct {
	cellID       string
	stale        bool
	ranOk        bool
	isPython     bool
	isSelectOnly bool
	hasSqlError  bool
	statusLoaded bool
	autoFailed   bool
	upstreamIDs  []string
}

func autoIsFresh(c autoCellInfo) bool { return !c.stale && c.ranOk }

// computeAutoRecomputeWave returns the stale cell ids safe to recompute in this
// wave: a clean single-SELECT cell with no errors whose upstreams are all
// already fresh. Downstreams of a stale-but-recomputable upstream wait for a
// later pass, after that upstream's new output re-validates them.
func computeAutoRecomputeWave(cells []autoCellInfo) []string {
	byID := make(map[string]autoCellInfo, len(cells))
	for _, c := range cells {
		byID[c.cellID] = c
	}
	upstreamReady := func(id string) bool {
		c, ok := byID[id]
		if !ok {
			return true // external import — always available
		}
		return autoIsFresh(c)
	}
	eligible := func(c autoCellInfo) bool {
		if !c.stale {
			return false
		}
		if c.isPython || !c.statusLoaded || !c.isSelectOnly || c.hasSqlError || c.autoFailed {
			return false
		}
		for _, up := range c.upstreamIDs {
			if !upstreamReady(up) {
				return false
			}
		}
		return true
	}
	var out []string
	for _, c := range cells {
		if eligible(c) {
			out = append(out, c.cellID)
		}
	}
	return out
}

// computeAutoRecomputeClosure returns every stale cell auto-recompute will
// eventually refresh (this wave or later), following the chain of recomputable
// upstreams. Used for presentation: a cell in this set is not flagged stale.
func computeAutoRecomputeClosure(cells []autoCellInfo) map[string]bool {
	byID := make(map[string]autoCellInfo, len(cells))
	for _, c := range cells {
		byID[c.cellID] = c
	}
	memo := map[string]bool{}
	visiting := map[string]bool{}
	var willRecompute func(id string) bool
	willRecompute = func(id string) bool {
		if v, ok := memo[id]; ok {
			return v
		}
		c, ok := byID[id]
		if !ok {
			return true // external import
		}
		if visiting[id] {
			return false // cycle
		}
		if autoIsFresh(c) {
			memo[id] = true
			return true
		}
		if c.isPython || !c.statusLoaded || !c.isSelectOnly || c.hasSqlError || c.autoFailed {
			memo[id] = false
			return false
		}
		visiting[id] = true
		ok = true
		for _, up := range c.upstreamIDs {
			if !willRecompute(up) {
				ok = false
				break
			}
		}
		delete(visiting, id)
		memo[id] = ok
		return ok
	}
	closure := map[string]bool{}
	for _, c := range cells {
		if c.stale && willRecompute(c.cellID) {
			closure[c.cellID] = true
		}
	}
	return closure
}

// publishRuntime emits a runtime event for a notebook. autoPending and running
// are id lists; results is a (possibly nil) delta of changed results. The stale
// set and toggle are read from the runtime.
func (s *NotebookService) publishRuntime(notebookID, uuid string, autoPending, running []string, results map[string]notebook.CellRunResult) {
	if s.deps.PublishEvent == nil {
		return
	}
	rt := s.runtimes.get(uuid)
	rt.mu.Lock()
	// When auto-recompute is off, nothing is "pending" — stale cells stay
	// flagged for the user, since the server won't refresh them.
	if !rt.autoRecompute {
		autoPending = nil
	}
	event := NotebookRuntimeEvent{
		Type:          notebookRuntimeEventType,
		NotebookID:    notebookID,
		AutoRecompute: rt.autoRecompute,
		Stale:         sortedKeys(rt.stale),
		AutoPending:   autoPending,
		Running:       running,
		Results:       results,
	}
	rt.mu.Unlock()
	if event.AutoPending == nil {
		event.AutoPending = []string{}
	}
	if event.Running == nil {
		event.Running = []string{}
	}
	s.deps.PublishEvent(event)
}

// NotebookRuntimeSnapshot is the recompute state embedded in the notebook GET
// payload, so a freshly opened tab renders correct staleness and results.
type NotebookRuntimeSnapshot struct {
	AutoRecompute bool                              `json:"auto_recompute"`
	Stale         []string                          `json:"stale"`
	AutoPending   []string                          `json:"auto_pending"`
	Results       map[string]notebook.CellRunResult `json:"results"`
}

// runtimeSnapshot returns the current recompute state for a notebook UUID,
// validating staleness to derive the auto-pending set.
func (s *NotebookService) runtimeSnapshot(nb *notebook.Notebook) NotebookRuntimeSnapshot {
	rt := s.runtimes.get(nb.UUID)
	cells := s.buildAutoCells(nb, rt)
	closure := computeAutoRecomputeClosure(cells)

	rt.mu.Lock()
	defer rt.mu.Unlock()
	results := make(map[string]notebook.CellRunResult, len(rt.results))
	for id, result := range rt.results {
		results[id] = result
	}
	autoPending := sortedKeys(closure)
	if !rt.autoRecompute {
		autoPending = []string{}
	}
	return NotebookRuntimeSnapshot{
		AutoRecompute: rt.autoRecompute,
		Stale:         sortedKeys(rt.stale),
		AutoPending:   autoPending,
		Results:       results,
	}
}

// forgetCell drops a deleted cell from the runtime so it leaves no ghost stale
// entry, then republishes the (now smaller) stale set.
func (s *NotebookService) forgetCell(notebookID, uuid, cellID string) {
	rt := s.runtimes.get(uuid)
	rt.mu.Lock()
	delete(rt.stale, cellID)
	delete(rt.results, cellID)
	delete(rt.autoFailed, cellID)
	rt.mu.Unlock()
	s.publishRuntime(notebookID, uuid, nil, nil, nil)
}

// Runtime returns the recompute snapshot for a notebook (by encoded id).
func (s *NotebookService) Runtime(notebookID string) (NotebookRuntimeSnapshot, *APIError) {
	nb, apiErr := s.load(notebookID)
	if apiErr != nil {
		return NotebookRuntimeSnapshot{}, apiErr
	}
	return s.runtimeSnapshot(nb), nil
}

// SetAutoRecompute updates the per-notebook toggle (and import environment).
// Turning it on triggers a pass for any already-stale cells.
func (s *NotebookService) SetAutoRecompute(notebookID string, enabled bool, environment string) *APIError {
	nb, apiErr := s.load(notebookID)
	if apiErr != nil {
		return apiErr
	}
	rt := s.runtimes.get(nb.UUID)
	rt.mu.Lock()
	rt.autoRecompute = enabled
	if environment != "" {
		rt.environment = environment
	}
	rt.mu.Unlock()
	if enabled {
		s.scheduleRecompute(notebookID, nb.UUID)
	}
	return nil
}

// CancelAutoRecompute stops an in-flight pass and parks the still-stale cells so
// they are not auto-retried until edited (the Stop button during auto runs).
func (s *NotebookService) CancelAutoRecompute(notebookID string) *APIError {
	nb, apiErr := s.load(notebookID)
	if apiErr != nil {
		return apiErr
	}
	rt := s.runtimes.get(nb.UUID)
	rt.mu.Lock()
	cancel := rt.cancelWave
	for id := range rt.stale {
		rt.autoFailed[id] = true
	}
	rt.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.publishRuntime(notebookID, nb.UUID, nil, nil, nil)
	return nil
}
