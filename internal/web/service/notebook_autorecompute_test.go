package service

import (
	"reflect"
	"sort"
	"testing"
)

// base (stale, clean) -> doubled (stale, clean): only base runs this wave.
func TestComputeAutoRecomputeWaveOnlyReadyUpstreams(t *testing.T) {
	cells := []autoCellInfo{
		{cellID: "base", stale: true, isSelectOnly: true, statusLoaded: true},
		{cellID: "doubled", stale: true, isSelectOnly: true, statusLoaded: true, upstreamIDs: []string{"base"}},
	}
	got := computeAutoRecomputeWave(cells)
	if !reflect.DeepEqual(got, []string{"base"}) {
		t.Fatalf("expected only base in wave, got %v", got)
	}
}

// Once base is fresh, doubled (clean) becomes eligible.
func TestComputeAutoRecomputeWaveDownstreamAfterUpstreamFresh(t *testing.T) {
	cells := []autoCellInfo{
		{cellID: "base", stale: false, ranOk: true, isSelectOnly: true, statusLoaded: true},
		{cellID: "doubled", stale: true, isSelectOnly: true, statusLoaded: true, upstreamIDs: []string{"base"}},
	}
	got := computeAutoRecomputeWave(cells)
	if !reflect.DeepEqual(got, []string{"doubled"}) {
		t.Fatalf("expected doubled in wave, got %v", got)
	}
}

// A downstream with a SQL error is never run and is not in the closure (so it
// stays flagged stale); the clean upstream is.
func TestComputeAutoRecomputeBreakingDownstream(t *testing.T) {
	cells := []autoCellInfo{
		{cellID: "base", stale: false, ranOk: true, isSelectOnly: true, statusLoaded: true},
		{cellID: "broken", stale: true, isSelectOnly: true, statusLoaded: true, hasSqlError: true, upstreamIDs: []string{"base"}},
	}
	if got := computeAutoRecomputeWave(cells); len(got) != 0 {
		t.Fatalf("expected empty wave, got %v", got)
	}
	closure := computeAutoRecomputeClosure(cells)
	if closure["broken"] {
		t.Fatalf("broken cell must not be in the auto-pending closure")
	}
}

// The closure spans a clean chain even when middle cells are still stale.
func TestComputeAutoRecomputeClosureSpansChain(t *testing.T) {
	cells := []autoCellInfo{
		{cellID: "a", stale: true, isSelectOnly: true, statusLoaded: true},
		{cellID: "b", stale: true, isSelectOnly: true, statusLoaded: true, upstreamIDs: []string{"a"}},
		{cellID: "c", stale: true, isSelectOnly: true, statusLoaded: true, upstreamIDs: []string{"b"}},
		{cellID: "py", stale: true, isPython: true, upstreamIDs: []string{"c"}},
	}
	closure := computeAutoRecomputeClosure(cells)
	got := make([]string, 0, len(closure))
	for id := range closure {
		got = append(got, id)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("expected a,b,c in closure (py excluded), got %v", got)
	}
}

// A Python cell and an unloaded cell are never eligible.
func TestComputeAutoRecomputeWaveExcludesPythonAndUnloaded(t *testing.T) {
	cells := []autoCellInfo{
		{cellID: "py", stale: true, isPython: true},
		{cellID: "unloaded", stale: true, isSelectOnly: true, statusLoaded: false},
		{cellID: "notselect", stale: true, isSelectOnly: false, statusLoaded: true},
		{cellID: "failed", stale: true, isSelectOnly: true, statusLoaded: true, autoFailed: true},
	}
	if got := computeAutoRecomputeWave(cells); len(got) != 0 {
		t.Fatalf("expected empty wave, got %v", got)
	}
}
