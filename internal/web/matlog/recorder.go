package matlog

import (
	"context"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"go.uber.org/zap"
	"renart/internal/web/bus"
	"renart/internal/web/fingerprint"
)

var executionWindowReference = regexp.MustCompile(`(?i)\{\{[-\s]*(?:start|end)_(?:date|date_nodash|datetime|timestamp)\b`)

// PipelineResolver loads the parsed pipeline for a stable pipeline UUID.
type PipelineResolver func(ctx context.Context, pipelineUUID string) (*pipeline.Pipeline, error)

// PathResolver parses a pipeline from an explicit directory (a materialized
// snapshot); used so snapshot runs record the deployed code's fingerprints
// rather than the working tree's.
type PathResolver func(ctx context.Context, pipelineDir string) (*pipeline.Pipeline, error)

// Recorder subscribes to RunCompleted bus events and writes materialization
// facts with the fingerprints current at completion time.
type Recorder struct {
	store       *Store
	engine      *fingerprint.Engine
	resolve     PipelineResolver
	resolvePath PathResolver
	logger      *zap.Logger
}

func NewRecorder(store *Store, engine *fingerprint.Engine, resolve PipelineResolver, resolvePath PathResolver, logger *zap.Logger) *Recorder {
	return &Recorder{store: store, engine: engine, resolve: resolve, resolvePath: resolvePath, logger: logger}
}

// HandleRunCompleted is the bus subscriber. Failures are logged, never
// propagated — a missed fact only means the asset reads as stale, which a
// rebuild repairs.
func (r *Recorder) HandleRunCompleted(event bus.RunCompleted) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var parsed *pipeline.Pipeline
	var err error
	if event.SnapshotDir != "" && r.resolvePath != nil {
		parsed, err = r.resolvePath(ctx, event.SnapshotDir)
	} else {
		parsed, err = r.resolve(ctx, event.PipelineUUID)
	}
	if err != nil {
		r.warn("failed to resolve pipeline for materialization log", event.PipelineUUID, err)
		return
	}

	vars := fingerprint.EffectiveVars(parsed, nil)
	results, err := r.engine.DAG(parsed, vars)
	if err != nil {
		r.warn("failed to fingerprint pipeline for materialization log", event.PipelineUUID, err)
		return
	}
	varsHash := fingerprint.AllVarsHash(vars)

	// Record each asset's *achieved* fingerprint — the data it actually
	// produced, folding in the fingerprint of each upstream as physically read
	// — not the current target. Materializing a downstream while an upstream is
	// stale must record as stale, not silently fresh. latest holds each asset's
	// pre-run fingerprint (this run's facts are not yet written), so an upstream
	// not rebuilt here contributes its old fingerprint; one rebuilt here
	// contributes its fresh achieved fingerprint via topo order.
	succeeded := make(map[string]bool, len(event.Assets))
	assetIDs := make([]string, 0, len(results))
	for id := range results {
		assetIDs = append(assetIDs, id)
	}
	for _, assetRun := range event.Assets {
		if assetRun.Status == "succeeded" {
			succeeded[assetRun.AssetID] = true
		}
	}
	latest, err := r.store.LatestFingerprint(ctx, assetIDs, event.Environment)
	if err != nil {
		r.warn("failed to load latest fingerprints for materialization log", event.PipelineUUID, err)
		return
	}
	achieved, err := r.engine.AchievedFingerprints(parsed, results, succeeded, func(id string) (fingerprint.Fingerprint, bool) {
		fp, ok := latest[id]
		return fingerprint.Fingerprint(fp), ok
	})
	if err != nil {
		r.warn("failed to compute achieved fingerprints for materialization log", event.PipelineUUID, err)
		return
	}

	for _, assetRun := range event.Assets {
		if assetRun.Status != "succeeded" {
			continue
		}
		result, ok := results[assetRun.AssetID]
		if !ok {
			continue
		}
		achievedFP, ok := achieved[assetRun.AssetID]
		if !ok {
			continue
		}
		materialization := Materialization{
			AssetID:        assetRun.AssetID,
			Environment:    event.Environment,
			Fingerprint:    string(achievedFP),
			OwnContent:     string(result.OwnContent),
			VarsHash:       varsHash,
			RunID:          event.RunID,
			MaterializedAt: event.CompletedAt,
		}
		asset := parsed.GetAssetByName(assetRun.AssetName)
		behavior := coverageBehaviorFor(asset)
		effectiveFullRefresh := event.FullRefresh && !refreshRestricted(asset)
		if behavior != coverageMarker {
			if event.WinStart == nil || event.WinEnd == nil {
				r.warn("skipping interval materialization fact without a complete run window", assetRun.AssetID, nil)
				continue
			}
			materialization.IntervalStart = event.WinStart
			materialization.IntervalEnd = event.WinEnd
			materialization.ReplaceCoverage = behavior == coverageReplaceInterval || effectiveFullRefresh
		} else if effectiveFullRefresh {
			materialization.ReplaceCoverage = true
		}
		if err := r.store.Record(ctx, materialization); err != nil {
			r.warn("failed to record materialization", assetRun.AssetID, err)
		}
	}

	// Record the latest run attempt (success or failure) per asset. The facts
	// above only capture successes; this lets the staleness service tell an
	// untested edit from a run that was attempted and failed, and surface an
	// unchanged asset whose last run failed. Fingerprint is the target of what
	// ran, compared later against the asset's current fingerprint.
	for _, assetRun := range event.Assets {
		result, ok := results[assetRun.AssetID]
		if !ok {
			continue
		}
		if err := r.store.RecordRun(ctx, AssetRunRecord{
			AssetID:     assetRun.AssetID,
			Environment: event.Environment,
			Fingerprint: string(result.FP),
			Status:      assetRun.Status,
			RunID:       event.RunID,
			RanAt:       event.CompletedAt,
		}); err != nil {
			r.warn("failed to record run attempt", assetRun.AssetID, err)
		}
	}
}

func (r *Recorder) warn(message, subject string, err error) {
	if r.logger != nil {
		r.logger.Warn(message, zap.String("subject", subject), zap.Error(err))
	}
}

type coverageBehavior uint8

const (
	coverageMarker coverageBehavior = iota
	coverageUnionIntervals
	coverageReplaceInterval
)

// IntervalAware reports whether the physical result represents a bounded run
// window. This drives staleness coverage display; it does not by itself mean
// independent windows can safely be accumulated by scheduler catch-up.
func IntervalAware(asset *pipeline.Asset) bool {
	return coverageBehaviorFor(asset) != coverageMarker
}

// BackfillSafe reports whether independent windows are replay-safe and can be
// unioned into cumulative coverage. The scheduler uses this narrower contract
// before enabling catch-up backfills.
func BackfillSafe(asset *pipeline.Asset) bool {
	return coverageBehaviorFor(asset) == coverageUnionIntervals
}

func coverageBehaviorFor(asset *pipeline.Asset) coverageBehavior {
	if asset == nil {
		return coverageMarker
	}
	assetType := strings.ToLower(strings.TrimSpace(string(asset.Type)))
	if assetType == "load" || asset.Type == pipeline.AssetTypePython {
		return coverageMarker
	}
	if assetType == "api" {
		if !executionWindowReference.MatchString(apiAssetContent(asset)) {
			return coverageMarker
		}
		if asset.Materialization.Strategy == pipeline.MaterializationStrategyMerge && len(asset.ColumnNamesWithPrimaryKey()) > 0 {
			return coverageUnionIntervals
		}
		return coverageReplaceInterval
	}
	if asset.Materialization.Strategy == pipeline.MaterializationStrategyTimeInterval {
		return coverageUnionIntervals
	}
	return coverageMarker
}

func refreshRestricted(asset *pipeline.Asset) bool {
	return asset != nil && asset.RefreshRestricted != nil && *asset.RefreshRestricted
}

func apiAssetContent(asset *pipeline.Asset) string {
	if asset == nil {
		return ""
	}
	if asset.ExecutableFile.Content != "" {
		return asset.ExecutableFile.Content
	}
	path := asset.ExecutableFile.Path
	if path == "" {
		path = asset.DefinitionFile.Path
	}
	if data, err := os.ReadFile(path); err == nil {
		return string(data)
	}
	return ""
}
