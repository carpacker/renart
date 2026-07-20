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
	if !run.ExecutionContextResolved {
		// The scheduler normally filters these rows. Keep the replay boundary
		// fail-safe as well: legacy request fields cannot prove which defaults and
		// environment restrictions the interrupted executor actually used.
		if s.logger != nil {
			s.logger.Warn("skipping interrupted run replay without persisted effective context", zap.String("run_id", run.ID))
		}
		return nil
	}
	if s.logger != nil {
		s.logger.Info("replaying persisted steps for interrupted run", zap.String("run_id", run.ID), zap.Int("steps", len(steps)))
	}
	type recoveredAsset struct {
		step   webscheduler.PipelineRunStep
		name   string
		status string
	}
	recovered := make([]recoveredAsset, 0, len(steps))
	for _, step := range steps {
		status, terminal := recoveredAssetRunStatus(step.Status)
		assetName := strings.TrimSpace(step.Asset)
		if terminal && assetName != "" {
			recovered = append(recovered, recoveredAsset{step: step, name: assetName, status: status})
		}
	}
	if len(recovered) == 0 {
		return nil
	}

	pipelineUUID := ""
	snapshotDir := ""
	cleanup := func() {}
	defer func() { cleanup() }()

	targetSnapshot := run.ExecutionTargetSnapshot
	selfContainedSnapshot := targetSnapshot != nil && targetSnapshot.Version >= webscheduler.ExecutionTargetSnapshotVersionV2
	if versionID := strings.TrimSpace(run.SnapshotVersionID); versionID != "" {
		if s.snapshotStore == nil {
			return fmt.Errorf("snapshot store is unavailable for recovered run %s", run.ID)
		}
		snapshot, err := s.snapshotStore.Get(ctx, versionID)
		if err != nil {
			return fmt.Errorf("load snapshot %s for recovered run %s: %w", versionID, run.ID, err)
		}
		pipelineUUID = snapshot.PipelineUUID
		if !selfContainedSnapshot {
			tempDir, err := os.MkdirTemp("", "renart-recovered-snapshot-")
			if err != nil {
				return fmt.Errorf("create recovery snapshot directory: %w", err)
			}
			cleanup = func() { _ = os.RemoveAll(tempDir) }
			if err := s.snapshotStore.MaterializeForExecution(ctx, versionID, tempDir); err != nil {
				return fmt.Errorf("materialize snapshot %s for recovered run %s: %w", versionID, run.ID, err)
			}
			snapshotDir = tempDir
		}
	} else {
		pipelineUUID = strings.TrimSpace(run.PipelineUUID)
		if selfContainedSnapshot {
			capturedUUID := strings.TrimSpace(targetSnapshot.PipelineUUID)
			if capturedUUID == "" || (pipelineUUID != "" && capturedUUID != pipelineUUID) {
				return fmt.Errorf("recovered run %s target snapshot does not match its admitted pipeline identity", run.ID)
			}
			pipelineUUID = capturedUUID
		}
		if pipelineUUID == "" {
			for _, candidate := range s.currentState().Pipelines {
				if candidate.UUID == run.PipelineUUID || (run.PipelineUUID == "" && candidate.ID == run.PipelineID) {
					pipelineUUID = candidate.UUID
					break
				}
			}
		}
		if pipelineUUID == "" {
			return fmt.Errorf("pipeline %s for recovered run %s is not in the current workspace", run.PipelineID, run.ID)
		}
	}
	if targetSnapshot != nil && strings.TrimSpace(targetSnapshot.PipelineUUID) != "" && targetSnapshot.PipelineUUID != pipelineUUID {
		return fmt.Errorf("recovered run %s target snapshot pipeline identity does not match executed source", run.ID)
	}

	assets := make([]bus.AssetRun, 0, len(recovered))
	for index, asset := range recovered {
		runAsset := bus.AssetRun{
			AssetID:    identity.AssetID(pipelineUUID, asset.name),
			AssetName:  asset.name,
			Status:     asset.status,
			StartedAt:  asset.step.StartedAt,
			FinishedAt: asset.step.FinishedAt,
		}
		if asset.step.CompletionOrdinal != nil {
			runAsset.CompletionOrdinal = *asset.step.CompletionOrdinal
			runAsset.HasCompletionOrdinal = true
		} else {
			runAsset.CompletionOrdinal = int64(index)
		}
		if asset.step.HasUpstreamWriterSnapshot {
			runAsset.UpstreamWriters = make(map[string]bus.UpstreamWriterSnapshot, len(asset.step.UpstreamWriters))
			for assetID, writer := range asset.step.UpstreamWriters {
				runAsset.UpstreamWriters[assetID] = bus.UpstreamWriterSnapshot{
					AssetID:           writer.AssetID,
					TargetIdentity:    writer.TargetIdentity,
					Fingerprint:       writer.Fingerprint,
					VarsHash:          writer.VarsHash,
					TargetGeneration:  writer.TargetGeneration,
					CompletionID:      writer.CompletionID,
					CompletionOrdinal: writer.CompletionOrdinal,
					MaterializedAt:    writer.MaterializedAt,
				}
			}
			runAsset.HasUpstreamWriterSnapshot = true
		}
		if snapshot := run.ExecutionTargetSnapshot; snapshot != nil {
			entry, exists := snapshot.Entries[asset.name]
			if !exists {
				return fmt.Errorf("recovered run %s target snapshot has no entry for %s", run.ID, asset.name)
			}
			if asset.status == "succeeded" && (asset.step.FinishedAt == nil || asset.step.CompletionOrdinal == nil) {
				return fmt.Errorf("recovered run %s successful step %s has incomplete completion coordinates", run.ID, asset.name)
			}
			if snapshot.Version >= webscheduler.ExecutionTargetSnapshotVersionV2 && asset.status == "succeeded" && !asset.step.HasUpstreamWriterSnapshot {
				return fmt.Errorf("recovered run %s successful step %s has no upstream writer snapshot", run.ID, asset.name)
			}
			expectedAssetID := identity.AssetID(pipelineUUID, asset.name)
			if entry.AssetID != expectedAssetID {
				return fmt.Errorf("recovered run %s target snapshot asset identity does not match %s", run.ID, asset.name)
			}
			runAsset.AssetID = entry.AssetID
			runAsset.TargetIdentity = entry.TargetIdentity
			runAsset.TargetFidelity = entry.TargetFidelity
			runAsset.Fingerprint = entry.Fingerprint
			runAsset.OwnContent = entry.OwnContent
			runAsset.ConsumedVarsHash = entry.ConsumedVarsHash
			runAsset.VarsHash = entry.VarsHash
		}
		assets = append(assets, runAsset)
	}

	completedAt := time.Now().UTC()
	if run.FinishedAt != nil && !run.FinishedAt.IsZero() {
		completedAt = run.FinishedAt.UTC()
	}
	event := bus.RunCompleted{
		RunID:             run.ID,
		CompletionID:      run.ID,
		PipelineUUID:      pipelineUUID,
		Environment:       run.Environment,
		WinStart:          run.WinStart,
		WinEnd:            run.WinEnd,
		FullRefresh:       run.FullRefresh,
		CompletedAt:       completedAt,
		Assets:            assets,
		SnapshotVersionID: run.SnapshotVersionID,
		SnapshotDir:       snapshotDir,
	}
	if snapshot := run.ExecutionTargetSnapshot; snapshot != nil {
		event.ExecutionTargetSnapshotVersion = snapshot.Version
		event.ExecutionPipelineUUID = snapshot.PipelineUUID
		event.ExecutionTargets = make(map[string]bus.ExecutionTargetSnapshotEntry, len(snapshot.Entries))
		for assetName, entry := range snapshot.Entries {
			upstreams := make([]bus.ExecutionUpstreamSnapshot, 0, len(entry.Upstreams))
			for _, upstream := range entry.Upstreams {
				upstreams = append(upstreams, bus.ExecutionUpstreamSnapshot{Type: upstream.Type, Value: upstream.Value})
			}
			event.ExecutionTargets[assetName] = bus.ExecutionTargetSnapshotEntry{
				AssetID:                     entry.AssetID,
				TargetIdentity:              entry.TargetIdentity,
				TargetFidelity:              entry.TargetFidelity,
				TargetWriteEvidenceRequired: entry.TargetWriteEvidenceRequired,
				WriteResourceKind:           entry.WriteResourceKind,
				WriteResourceIdentity:       entry.WriteResourceIdentity,
				WriteResourceFidelity:       entry.WriteResourceFidelity,
				Fingerprint:                 entry.Fingerprint,
				OwnContent:                  entry.OwnContent,
				ConsumedVarsHash:            entry.ConsumedVarsHash,
				VarsHash:                    entry.VarsHash,
				Upstreams:                   upstreams,
				CoverageMode:                entry.CoverageMode,
				RefreshRestricted:           entry.RefreshRestricted,
			}
		}
	}
	if err := s.eventBus.EmitRunCompleted(event); err != nil {
		return fmt.Errorf("replay recovered run %s completion: %w", run.ID, err)
	}
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
