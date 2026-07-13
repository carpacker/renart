// Package staleness classifies every asset of a pipeline against the
// materialization coverage table for the current selection (environment,
// time range, variables). It keeps the last-requested selection per
// pipeline in memory and recomputes on AssetSaved / RunCompleted bus
// events, pushing updates over SSE — the UI never polls per render.
package staleness

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"go.uber.org/zap"
	"renart/internal/web/bus"
	"renart/internal/web/fingerprint"
	"renart/internal/web/identity"
	"renart/internal/web/matlog"
)

type Status string

const (
	// StatusFresh: coverage exists for the current fingerprint + vars + range.
	StatusFresh Status = "fresh"
	// StatusStaleEdited: this asset's own definition changed since last build.
	StatusStaleEdited Status = "stale_edited"
	// StatusStaleUpstream: inherited staleness via the Merkle cascade (or a
	// changed variable value — own content matches, the full hash does not).
	StatusStaleUpstream Status = "stale_upstream"
	// StatusPartial: incremental asset with some but not all of the selected
	// range covered.
	StatusPartial Status = "partial"
	// StatusNeverBuilt: no coverage row in this environment at all, under
	// any fingerprint.
	StatusNeverBuilt Status = "never_built"
	// StatusMissing: the log says fresh but verification could not find the
	// table in the warehouse.
	StatusMissing Status = "missing"
)

// Interval is a [Start, End) time range.
type Interval struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// AssetStatus is the classification result for one asset.
type AssetStatus struct {
	AssetID            string     `json:"asset_id"` // durable: pipeline UUID + ":" + name
	AssetName          string     `json:"asset_name"`
	Status             Status     `json:"status"`
	Fingerprint        string     `json:"fingerprint"`
	IntervalAware      bool       `json:"interval_aware"`
	CoveredSeconds     float64    `json:"covered_seconds,omitempty"`
	TotalSeconds       float64    `json:"total_seconds,omitempty"`
	Gaps               []Interval `json:"gaps,omitempty"` // uncovered sub-ranges, the Build-stale plan input
	LastMaterializedAt *time.Time `json:"last_materialized_at,omitempty"`
	// Last run attempt (success or failure), orthogonal to the base Status.
	// Together with the base status and the current fingerprint they let the UI
	// tell an untested edit from a run that failed, and an unchanged asset whose
	// last run failed. LastRunOnCurrentContent is true when the last run's
	// fingerprint matches the asset's current fingerprint (i.e. the run was on
	// the content still on disk).
	LastRunStatus           string     `json:"last_run_status,omitempty"` // "succeeded" | "failed" | "cancelled"
	LastRunAt               *time.Time `json:"last_run_at,omitempty"`
	LastRunOnCurrentContent bool       `json:"last_run_on_current_content,omitempty"`
}

// Selection identifies what the user is looking at.
type Selection struct {
	PipelineUUID string
	// EncodedPipelineID is the path-encoded API pipeline ID, carried along
	// so published events and verification calls can address API routes.
	EncodedPipelineID string
	Environment       string
	// Start/End bound the freshness check for interval-aware assets; when
	// nil, any coverage under the current fingerprint counts as fresh.
	Start, End *time.Time
	// VarOverrides are merged over pipeline variable defaults.
	VarOverrides map[string]any
}

func (s Selection) rangeInterval() *Interval {
	if s.Start == nil || s.End == nil || !s.End.After(*s.Start) {
		return nil
	}
	return &Interval{Start: s.Start.UTC(), End: s.End.UTC()}
}

// Verifier asynchronously checks which assets actually exist in the
// warehouse; assets reported false are downgraded from fresh to missing.
type Verifier func(ctx context.Context, selection Selection, assetNames []string) (map[string]bool, error)

type Dependencies struct {
	Store   *matlog.Store
	Engine  *fingerprint.Engine
	Resolve matlog.PipelineResolver
	// Publish pushes staleness.updated events to SSE clients.
	Publish func(any)
	// Verify is optional trust-but-verify support; it runs async, throttled
	// to once per (pipeline, environment) per session, and never blocks
	// status computation.
	Verify Verifier
	Logger *zap.Logger
}

type Service struct {
	deps Dependencies

	mu sync.Mutex
	// selections holds the last-requested selection per pipeline UUID; bus
	// events recompute exactly these.
	selections map[string]Selection
	// statuses caches the last computed result per pipeline UUID.
	statuses map[string][]AssetStatus
	// verified tracks (pipelineUUID, environment) pairs already verified
	// this session; missing tables found are remembered until the next run.
	verified       map[string]bool
	missingByPanel map[string]map[string]bool // panelKey -> assetName -> missing
}

func New(deps Dependencies) *Service {
	return &Service{
		deps:           deps,
		selections:     make(map[string]Selection),
		statuses:       make(map[string][]AssetStatus),
		verified:       make(map[string]bool),
		missingByPanel: make(map[string]map[string]bool),
	}
}

// AttachBus subscribes the service to the process event bus.
func (s *Service) AttachBus(events bus.Events) {
	events.OnAssetSaved(func(event bus.AssetSaved) {
		s.recomputePipeline(event.PipelineUUID, "asset saved")
	})
	events.OnRunCompleted(func(event bus.RunCompleted) {
		s.recomputePipeline(event.PipelineUUID, "run completed")
	})
}

// Statuses computes (and caches) the staleness map for a selection. This is
// the selection-change entry point: one batched coverage query, the rest in
// memory.
func (s *Service) Statuses(ctx context.Context, selection Selection) ([]AssetStatus, error) {
	statuses, err := s.compute(ctx, selection)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.selections[selection.PipelineUUID] = selection
	s.statuses[selection.PipelineUUID] = statuses
	shouldVerify := s.deps.Verify != nil && !s.verified[verifyKey(selection)]
	if shouldVerify {
		s.verified[verifyKey(selection)] = true
	}
	s.mu.Unlock()

	if shouldVerify {
		go s.runVerification(selection)
	}
	return statuses, nil
}

func verifyKey(selection Selection) string {
	return selection.PipelineUUID + "\x00" + selection.Environment
}

// recomputePipeline refreshes the cached selection for a pipeline after a
// bus event and publishes the updated statuses.
func (s *Service) recomputePipeline(pipelineUUID, reason string) {
	s.mu.Lock()
	selection, ok := s.selections[pipelineUUID]
	s.mu.Unlock()
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	statuses, err := s.compute(ctx, selection)
	if err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Warn("staleness recompute failed",
				zap.String("pipeline", pipelineUUID), zap.String("trigger", reason), zap.Error(err))
		}
		return
	}

	s.mu.Lock()
	s.statuses[pipelineUUID] = statuses
	s.mu.Unlock()
	s.publish(selection, statuses)
}

func (s *Service) publish(selection Selection, statuses []AssetStatus) {
	if s.deps.Publish == nil {
		return
	}
	event := map[string]any{
		"type":          "staleness.updated",
		"pipeline_id":   selection.EncodedPipelineID,
		"pipeline_uuid": selection.PipelineUUID,
		"environment":   selection.Environment,
		"assets":        statuses,
	}
	// The selection's range rides along so clients can discard pushes
	// computed for a selection they have already moved away from.
	if selection.Start != nil {
		event["start"] = selection.Start.UTC().Format(time.RFC3339)
	}
	if selection.End != nil {
		event["end"] = selection.End.UTC().Format(time.RFC3339)
	}
	s.deps.Publish(event)
}

func (s *Service) compute(ctx context.Context, selection Selection) ([]AssetStatus, error) {
	parsed, err := s.deps.Resolve(ctx, selection.PipelineUUID)
	if err != nil {
		return nil, err
	}

	vars := fingerprint.EffectiveVars(parsed, selection.VarOverrides)
	results, err := s.deps.Engine.DAG(parsed, vars)
	if err != nil {
		return nil, err
	}
	varsHash := fingerprint.AllVarsHash(vars)

	assetIDs := make([]string, 0, len(results))
	for id := range results {
		assetIDs = append(assetIDs, id)
	}
	sort.Strings(assetIDs)

	coverage, err := s.deps.Store.Coverage(ctx, assetIDs, selection.Environment, varsHash)
	if err != nil {
		return nil, err
	}
	anyBuilt, err := s.deps.Store.HasAnyCoverage(ctx, assetIDs, selection.Environment)
	if err != nil {
		return nil, err
	}
	lastOwnContent, err := s.deps.Store.LatestOwnContent(ctx, assetIDs, selection.Environment)
	if err != nil {
		return nil, err
	}
	lastRuns, err := s.deps.Store.LastRuns(ctx, assetIDs, selection.Environment)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	missing := s.missingByPanel[verifyKey(selection)]
	s.mu.Unlock()

	selectedRange := selection.rangeInterval()
	statuses := make([]AssetStatus, 0, len(parsed.Assets))
	for _, asset := range parsed.Assets {
		assetID := identity.AssetID(selection.PipelineUUID, asset.Name)
		result, ok := results[assetID]
		if !ok {
			continue
		}
		status := classify(asset, assetID, result, coverage[assetID], anyBuilt[assetID], lastOwnContent[assetID], selectedRange)
		applyLastRun(&status, lastRuns[assetID], result)
		if status.Status == StatusFresh && missing != nil && missing[asset.Name] && verifiableByName(asset) {
			status.Status = StatusMissing
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// verifiableByName reports whether an asset's freshness can be confirmed by
// looking up a warehouse object named after the asset — the assumption the
// trust-but-verify pass makes. It holds for SQL and seed assets. It does NOT
// hold for database Load assets as well because their table is derived from the
// asset name. File/object Load targets carry destination_object and Python
// assets may still write no queryable table, so those rest on the run fact.
func verifiableByName(asset *pipeline.Asset) bool {
	switch strings.ToLower(strings.TrimSpace(string(asset.Type))) {
	case "load":
		if strings.EqualFold(strings.TrimSpace(asset.Connection), "local") {
			return false
		}
		destinationObject, _ := asset.Parameters.GetString("destination_object")
		return strings.TrimSpace(destinationObject) == ""
	case "python":
		return false
	}
	return true
}

// applyLastRun layers the most recent run attempt onto the base classification.
// It is orthogonal to the base status: a fresh asset can still carry a failed
// last run (an unchanged re-run that failed), and for an edited asset it records
// whether the failing run was on the content still on disk.
func applyLastRun(status *AssetStatus, lastRun matlog.AssetRunRecord, result fingerprint.Result) {
	if lastRun.Status == "" {
		return
	}
	status.LastRunStatus = lastRun.Status
	if !lastRun.RanAt.IsZero() {
		at := lastRun.RanAt
		status.LastRunAt = &at
	}
	status.LastRunOnCurrentContent = lastRun.Fingerprint == string(result.FP)
}

func classify(asset *pipeline.Asset, assetID string, result fingerprint.Result, rows []matlog.CoverageRow, anyBuilt bool, lastOwnContent string, selectedRange *Interval) AssetStatus {
	status := AssetStatus{
		AssetID:       assetID,
		AssetName:     asset.Name,
		Fingerprint:   string(result.FP),
		IntervalAware: matlog.IntervalAware(asset),
	}

	currentRows := make([]matlog.CoverageRow, 0, len(rows))
	for _, row := range rows {
		if row.Fingerprint == string(result.FP) {
			currentRows = append(currentRows, row)
			if status.LastMaterializedAt == nil || row.MaterializedAt.After(*status.LastMaterializedAt) {
				at := row.MaterializedAt
				status.LastMaterializedAt = &at
			}
		}
	}

	if !status.IntervalAware || selectedRange == nil {
		if len(currentRows) > 0 {
			status.Status = StatusFresh
			return status
		}
		return classifyStale(status, anyBuilt, lastOwnContent, result, selectedRange)
	}

	covered, gaps := coverageOfRange(currentRows, *selectedRange)
	status.TotalSeconds = selectedRange.End.Sub(selectedRange.Start).Seconds()
	status.CoveredSeconds = covered.Seconds()
	switch {
	case len(gaps) == 0:
		status.Status = StatusFresh
	case len(currentRows) > 0:
		// The definition is unchanged (rows exist under the current
		// fingerprint) — the selected range just isn't (fully) built. This
		// includes zero coverage: report 0/N built rather than a misleading
		// stale_* state, so switching the time selector to an unbuilt
		// interval reads as "not built for this range".
		status.Status = StatusPartial
		status.Gaps = gaps
	default:
		return classifyStale(status, anyBuilt, lastOwnContent, result, selectedRange)
	}
	return status
}

func classifyStale(status AssetStatus, anyBuilt bool, lastOwnContent string, result fingerprint.Result, selectedRange *Interval) AssetStatus {
	if !anyBuilt {
		status.Status = StatusNeverBuilt
	} else if lastOwnContent != "" && lastOwnContent == string(result.OwnContent) {
		status.Status = StatusStaleUpstream
	} else {
		status.Status = StatusStaleEdited
	}
	if selectedRange != nil && status.IntervalAware {
		status.Gaps = []Interval{*selectedRange}
		status.TotalSeconds = selectedRange.End.Sub(selectedRange.Start).Seconds()
	}
	return status
}

// coverageOfRange intersects the (already merged, disjoint) coverage rows
// with the selected range, returning the covered duration and the uncovered
// gaps in order.
func coverageOfRange(rows []matlog.CoverageRow, selected Interval) (time.Duration, []Interval) {
	intervals := make([]Interval, 0, len(rows))
	for _, row := range rows {
		if row.IntervalStart == nil || row.IntervalEnd == nil {
			// A full-refresh marker under an interval-aware classification
			// covers everything (e.g. the asset became interval-aware later).
			return selected.End.Sub(selected.Start), nil
		}
		start := row.IntervalStart.UTC()
		end := row.IntervalEnd.UTC()
		if !end.After(selected.Start) || !selected.End.After(start) {
			continue
		}
		if start.Before(selected.Start) {
			start = selected.Start
		}
		if end.After(selected.End) {
			end = selected.End
		}
		intervals = append(intervals, Interval{Start: start, End: end})
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].Start.Before(intervals[j].Start) })

	var covered time.Duration
	gaps := make([]Interval, 0)
	cursor := selected.Start
	for _, interval := range intervals {
		if interval.Start.After(cursor) {
			gaps = append(gaps, Interval{Start: cursor, End: interval.Start})
		}
		covered += interval.End.Sub(interval.Start)
		if interval.End.After(cursor) {
			cursor = interval.End
		}
	}
	if cursor.Before(selected.End) {
		gaps = append(gaps, Interval{Start: cursor, End: selected.End})
	}
	return covered, gaps
}

// runVerification fires the async trust-but-verify pass and republishes if
// any fresh asset turns out to be missing from the warehouse.
func (s *Service) runVerification(selection Selection) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s.mu.Lock()
	statuses := s.statuses[selection.PipelineUUID]
	s.mu.Unlock()

	freshNames := make([]string, 0, len(statuses))
	for _, status := range statuses {
		if status.Status == StatusFresh || status.Status == StatusPartial {
			freshNames = append(freshNames, status.AssetName)
		}
	}
	if len(freshNames) == 0 {
		return
	}

	exists, err := s.deps.Verify(ctx, selection, freshNames)
	if err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Warn("staleness verification failed", zap.String("pipeline", selection.PipelineUUID), zap.Error(err))
		}
		return
	}

	missing := make(map[string]bool)
	for name, present := range exists {
		if !present {
			missing[name] = true
		}
	}
	if len(missing) == 0 {
		return
	}

	s.mu.Lock()
	s.missingByPanel[verifyKey(selection)] = missing
	s.mu.Unlock()
	s.recomputePipeline(selection.PipelineUUID, "verification")
}
