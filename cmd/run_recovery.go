package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"renart/internal/web/bus"
	"renart/internal/web/identity"
	webscheduler "renart/internal/web/scheduler"

	"go.uber.org/zap"
)

// replayRecoveredRun rebuilds the canonical materialization/run-attempt state
// from scheduler steps that were durable before an unclean server stop. It
// emits the same completion event as normal execution, but never reruns an
// asset or replays textual logs.
func (s *webServer) replayRecoveredRun(ctx context.Context, run webscheduler.PipelineRun, steps []webscheduler.PipelineRunStep) error {
	if s == nil || s.eventBus == nil {
		return nil
	}
	if s.logger != nil {
		s.logger.Info("replaying persisted steps for interrupted run", zap.String("run_id", run.ID), zap.Int("steps", len(steps)))
	}

	pipelineUUID := ""
	snapshotDir := ""
	cleanup := func() {}
	defer func() { cleanup() }()

	if versionID := strings.TrimSpace(run.SnapshotVersionID); versionID != "" {
		if s.snapshotStore == nil {
			return fmt.Errorf("snapshot store is unavailable for recovered run %s", run.ID)
		}
		snapshot, err := s.snapshotStore.Get(ctx, versionID)
		if err != nil {
			return fmt.Errorf("load snapshot %s for recovered run %s: %w", versionID, run.ID, err)
		}
		pipelineUUID = snapshot.PipelineUUID
		tempDir, err := os.MkdirTemp("", "renart-recovered-snapshot-")
		if err != nil {
			return fmt.Errorf("create recovery snapshot directory: %w", err)
		}
		cleanup = func() { _ = os.RemoveAll(tempDir) }
		if err := s.snapshotStore.MaterializeForExecution(ctx, versionID, tempDir); err != nil {
			return fmt.Errorf("materialize snapshot %s for recovered run %s: %w", versionID, run.ID, err)
		}
		snapshotDir = tempDir
	} else {
		for _, candidate := range s.currentState().Pipelines {
			if candidate.ID == run.PipelineID {
				pipelineUUID = candidate.UUID
				break
			}
		}
		if pipelineUUID == "" {
			return fmt.Errorf("pipeline %s for recovered run %s is not in the current workspace", run.PipelineID, run.ID)
		}
	}

	assets := make([]bus.AssetRun, 0, len(steps))
	for _, step := range steps {
		status, terminal := recoveredAssetRunStatus(step.Status)
		assetName := strings.TrimSpace(step.Asset)
		if !terminal || assetName == "" {
			continue
		}
		assets = append(assets, bus.AssetRun{
			AssetID:   identity.AssetID(pipelineUUID, assetName),
			AssetName: assetName,
			Status:    status,
		})
	}
	if len(assets) == 0 {
		return nil
	}

	completedAt := time.Now().UTC()
	if run.FinishedAt != nil && !run.FinishedAt.IsZero() {
		completedAt = run.FinishedAt.UTC()
	}
	s.eventBus.EmitRunCompleted(bus.RunCompleted{
		RunID:             run.ID,
		PipelineUUID:      pipelineUUID,
		Environment:       run.Environment,
		WinStart:          run.WinStart,
		WinEnd:            run.WinEnd,
		CompletedAt:       completedAt,
		Assets:            assets,
		SnapshotVersionID: run.SnapshotVersionID,
		SnapshotDir:       snapshotDir,
	})
	if s.logger != nil {
		s.logger.Info("replayed persisted steps for interrupted run", zap.String("run_id", run.ID), zap.Int("assets", len(assets)))
	}
	return nil
}

func recoveredAssetRunStatus(status webscheduler.RunStatus) (string, bool) {
	switch status {
	case webscheduler.RunStatusSuccess:
		return "succeeded", true
	case webscheduler.RunStatusFailed:
		return "failed", true
	case webscheduler.RunStatusCancelled:
		return "cancelled", true
	default:
		return "", false
	}
}
