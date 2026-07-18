package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"renart/internal/web/bus"
	"renart/internal/web/completion"
	"renart/internal/web/fingerprint"
	"renart/internal/web/matlog"
	webscheduler "renart/internal/web/scheduler"
)

func TestRunCompletionOutboxReplaysWithoutRepeatingPhysicalExecution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	schedulerStore, err := webscheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = schedulerStore.Close() })

	materializations := matlog.NewStore(schedulerStore.DB())
	events := bus.New()
	recorder := matlog.NewRecorder(materializations, fingerprint.NewEngine(), nil, nil, zap.NewNop())
	events.OnRunCompleted(recorder.HandleRunCompleted)
	failFollower := true
	deliveries := 0
	events.OnRunCompleted(func(bus.RunCompleted) error {
		deliveries++
		if failFollower {
			return errors.New("transient completion subscriber failure")
		}
		return nil
	})

	server := &webServer{
		eventBus:        events,
		completionStore: completion.NewStore(schedulerStore.DB()),
	}
	finished := time.Date(2026, 7, 17, 14, 0, 0, 0, time.UTC)
	event := selfContainedCompletionEvent(finished)
	entry := event.ExecutionTargets["analytics.orders"]
	require.NoError(t, materializations.ClaimTargetWrite(ctx, matlog.TargetWriteClaim{
		TargetIdentity: entry.TargetIdentity,
		CompletionID:   event.CompletionID,
		AssetID:        entry.AssetID,
		ClaimedAt:      finished.Add(-time.Second),
	}))

	require.NoError(t, server.dispatchRunCompletion(ctx, event),
		"a durable physical completion must not fail when only derived-state dispatch is pending")
	pending, err := server.completionStore.ListPending(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 1, "the physical completion remains durable after dispatch fails")

	coverage, err := materializations.CurrentTargetCoverage(ctx, map[string]string{
		entry.AssetID: entry.TargetIdentity,
	}, event.Environment, entry.VarsHash)
	require.NoError(t, err)
	require.Len(t, coverage[entry.AssetID], 1,
		"the recorder committed before the later subscriber failed and atomically cleared the claim")

	failFollower = false
	require.NoError(t, server.replayPendingCompletions(ctx))
	pending, err = server.completionStore.ListPending(ctx)
	require.NoError(t, err)
	assert.Empty(t, pending)
	assert.Equal(t, 2, deliveries, "replay redispatches evidence, never the physical executor")
}

func TestRunCompletionOutboxPropagatesEnqueueFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	schedulerStore, err := webscheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)

	server := &webServer{
		eventBus:        bus.New(),
		completionStore: completion.NewStore(schedulerStore.DB()),
	}
	require.NoError(t, schedulerStore.Close())

	err = server.dispatchRunCompletion(ctx, selfContainedCompletionEvent(time.Now().UTC()))
	require.ErrorContains(t, err, "enqueue completed run")
}

func TestServerTargetWriteStorePublishesClaimAndDirtyTransitions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	schedulerStore, err := webscheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = schedulerStore.Close() })

	events := bus.New()
	var changed []bus.TargetWriteChanged
	events.OnTargetWriteChanged(func(event bus.TargetWriteChanged) {
		changed = append(changed, event)
	})
	server := &webServer{
		eventBus:    events,
		matlogStore: matlog.NewStore(schedulerStore.DB()),
	}
	store := serverTargetWriteStore{server: server}
	claim := matlog.TargetWriteClaim{
		TargetIdentity: "target-orders",
		CompletionID:   "completion-id",
		AssetID:        "pipeline-uuid:analytics.orders",
		ClaimedAt:      time.Now().UTC(),
	}

	require.NoError(t, store.ClaimTargetWrite(ctx, claim))
	require.NoError(t, store.MarkTargetWriteClaimDirty(ctx, claim, time.Now().UTC()))
	assert.Equal(t, []bus.TargetWriteChanged{
		{PipelineUUID: "pipeline-uuid", AssetID: claim.AssetID},
		{PipelineUUID: "pipeline-uuid", AssetID: claim.AssetID},
	}, changed)
}

func selfContainedCompletionEvent(finished time.Time) bus.RunCompleted {
	const (
		pipelineUUID = "pipeline-uuid"
		assetName    = "analytics.orders"
		assetID      = pipelineUUID + ":" + assetName
	)
	entry := bus.ExecutionTargetSnapshotEntry{
		AssetID: assetID, TargetIdentity: "target-orders", TargetFidelity: "exact",
		Fingerprint: "v1:target", OwnContent: "v1:own", ConsumedVarsHash: "vars-consumed",
		VarsHash: "vars-all", CoverageMode: "marker",
	}
	return bus.RunCompleted{
		RunID: "run-id", CompletionID: "completion-id", PipelineUUID: pipelineUUID,
		Environment: "prod", CompletedAt: finished,
		ExecutionTargetSnapshotVersion: 2, ExecutionPipelineUUID: pipelineUUID,
		ExecutionTargets: map[string]bus.ExecutionTargetSnapshotEntry{assetName: entry},
		Assets: []bus.AssetRun{{
			AssetID: assetID, AssetName: assetName, Status: "succeeded", FinishedAt: &finished,
			CompletionOrdinal: 0, HasCompletionOrdinal: true,
			UpstreamWriters: map[string]bus.UpstreamWriterSnapshot{}, HasUpstreamWriterSnapshot: true,
			TargetIdentity: entry.TargetIdentity, TargetFidelity: entry.TargetFidelity,
			Fingerprint: entry.Fingerprint, OwnContent: entry.OwnContent,
			ConsumedVarsHash: entry.ConsumedVarsHash, VarsHash: entry.VarsHash,
		}},
	}
}
