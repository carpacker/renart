package matlog

import (
	"context"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"go.uber.org/zap"
	"renart/internal/web/bus"
	"renart/internal/web/fingerprint"
)

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

	for _, assetRun := range event.Assets {
		if assetRun.Status != "succeeded" {
			continue
		}
		result, ok := results[assetRun.AssetID]
		if !ok {
			continue
		}
		materialization := Materialization{
			AssetID:        assetRun.AssetID,
			Environment:    event.Environment,
			Fingerprint:    string(result.FP),
			OwnContent:     string(result.OwnContent),
			VarsHash:       varsHash,
			RunID:          event.RunID,
			MaterializedAt: event.CompletedAt,
		}
		if asset := parsed.GetAssetByName(assetRun.AssetName); asset != nil && IntervalAware(asset) {
			materialization.IntervalStart = event.WinStart
			materialization.IntervalEnd = event.WinEnd
		}
		if err := r.store.Record(ctx, materialization); err != nil {
			r.warn("failed to record materialization", assetRun.AssetID, err)
		}
	}
}

func (r *Recorder) warn(message, subject string, err error) {
	if r.logger != nil {
		r.logger.Warn(message, zap.String("subject", subject), zap.Error(err))
	}
}

// IntervalAware reports whether an asset materializes per time interval
// (coverage tracks its intervals) as opposed to full refresh (coverage
// tracks a single "built" marker).
func IntervalAware(asset *pipeline.Asset) bool {
	if asset.Materialization.IncrementalKey != "" {
		return true
	}
	switch asset.Materialization.Strategy {
	case pipeline.MaterializationStrategyTimeInterval,
		pipeline.MaterializationStrategyDeleteInsert,
		pipeline.MaterializationStrategyAppend:
		return true
	default:
		return false
	}
}
