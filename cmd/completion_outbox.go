package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"renart/internal/web/bus"
	"renart/internal/web/identity"
	"renart/internal/web/matlog"
)

// serverTargetWriteStore defers access until server initialization has opened
// the shared state database. ExecutionService is constructed earlier because
// several other services reference it during wiring.
type serverTargetWriteStore struct {
	server *webServer
}

func (a serverTargetWriteStore) ClaimTargetWrite(ctx context.Context, claim matlog.TargetWriteClaim) error {
	if a.server == nil || a.server.matlogStore == nil {
		return fmt.Errorf("physical target write store is not initialized")
	}
	if err := a.server.matlogStore.ClaimTargetWrite(ctx, claim); err != nil {
		return err
	}
	a.publishTargetWriteChanged(claim.AssetID)
	return nil
}

func (a serverTargetWriteStore) MarkTargetWriteClaimDirty(ctx context.Context, claim matlog.TargetWriteClaim, at time.Time) error {
	if a.server == nil || a.server.matlogStore == nil {
		return fmt.Errorf("physical target write store is not initialized")
	}
	if err := a.server.matlogStore.MarkTargetWriteClaimDirty(ctx, claim, at); err != nil {
		return err
	}
	a.publishTargetWriteChanged(claim.AssetID)
	return nil
}

func (a serverTargetWriteStore) LatestWriters(ctx context.Context, targets []string) (map[string]matlog.LatestSuccessfulWriter, error) {
	if a.server == nil || a.server.matlogStore == nil {
		return nil, fmt.Errorf("physical target write store is not initialized")
	}
	return a.server.matlogStore.LatestWriters(ctx, targets)
}

func (a serverTargetWriteStore) publishTargetWriteChanged(assetID string) {
	if a.server == nil || a.server.eventBus == nil {
		return
	}
	pipelineUUID, _, ok := identity.SplitAssetID(assetID)
	if !ok || pipelineUUID == "" {
		return
	}
	a.server.eventBus.EmitTargetWriteChanged(bus.TargetWriteChanged{
		PipelineUUID: pipelineUUID,
		AssetID:      assetID,
	})
}

// dispatchRunCompletion serializes normal completion delivery with startup and
// housekeeping replay. Enqueue happens before any derived-state subscriber;
// DispatchPending removes each envelope only after every subscriber accepts it.
// Once enqueue succeeds, subscriber failures remain retryable outbox work and
// are not reported as physical execution failures.
func (s *webServer) dispatchRunCompletion(ctx context.Context, event bus.RunCompleted) error {
	if s == nil {
		return fmt.Errorf("completion dispatcher is not initialized")
	}
	s.completionMu.Lock()
	defer s.completionMu.Unlock()

	if s.completionStore == nil {
		if s.eventBus == nil {
			return nil
		}
		return s.eventBus.EmitRunCompleted(event)
	}
	if err := s.completionStore.Enqueue(ctx, event); err != nil {
		return fmt.Errorf("enqueue completed run %s: %w", event.CompletionID, err)
	}
	if err := s.dispatchPendingCompletionsLocked(ctx); err != nil {
		// Physical execution has already succeeded and the canonical envelope is
		// durable. Derived-state delivery is retryable from the outbox, so never
		// turn the completed physical run into a failure (or invite a duplicate
		// materialization) merely because a subscriber is temporarily down.
		slog.Warn("completed run is durable but derived-state dispatch failed; leaving it pending for replay",
			"completion_id", event.CompletionID,
			"run_id", event.RunID,
			"error", err,
		)
	}
	return nil
}

func (s *webServer) replayPendingCompletions(ctx context.Context) error {
	if s == nil || s.completionStore == nil {
		return nil
	}
	s.completionMu.Lock()
	defer s.completionMu.Unlock()
	return s.dispatchPendingCompletionsLocked(ctx)
}

func (s *webServer) dispatchPendingCompletionsLocked(ctx context.Context) error {
	if s.eventBus == nil {
		return fmt.Errorf("completion event bus is not initialized")
	}
	return s.completionStore.DispatchPending(ctx, s.eventBus.EmitRunCompleted)
}
