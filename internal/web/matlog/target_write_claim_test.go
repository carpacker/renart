package matlog_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/matlog"
	"renart/internal/web/scheduler"
)

func targetClaim(materialization matlog.Materialization, claimedHour int) matlog.TargetWriteClaim {
	return matlog.TargetWriteClaim{
		TargetIdentity: materialization.TargetIdentity,
		CompletionID:   materialization.CompletionID,
		AssetID:        materialization.AssetID,
		ClaimedAt:      ts(claimedHour),
	}
}

func requireTargetAvailable(
	t *testing.T,
	store *matlog.Store,
	assetID string,
	targetIdentity string,
	environment string,
	varsHash string,
) {
	t.Helper()
	ctx := context.Background()
	writers, err := store.LatestWriters(ctx, []string{targetIdentity})
	require.NoError(t, err)
	require.Contains(t, writers, targetIdentity)
	coverage, err := store.CurrentTargetCoverage(
		ctx,
		map[string]string{assetID: targetIdentity},
		environment,
		varsHash,
	)
	require.NoError(t, err)
	require.NotEmpty(t, coverage[assetID])
	ownContent, err := store.CurrentTargetOwnContent(
		ctx,
		map[string]string{assetID: targetIdentity},
		environment,
	)
	require.NoError(t, err)
	require.Contains(t, ownContent, assetID)
}

func requireTargetUnavailable(
	t *testing.T,
	store *matlog.Store,
	assetID string,
	targetIdentity string,
	environment string,
	varsHash string,
) {
	t.Helper()
	ctx := context.Background()
	writers, err := store.LatestWriters(ctx, []string{targetIdentity})
	require.NoError(t, err)
	assert.NotContains(t, writers, targetIdentity)
	coverage, err := store.CurrentTargetCoverage(
		ctx,
		map[string]string{assetID: targetIdentity},
		environment,
		varsHash,
	)
	require.NoError(t, err)
	assert.Empty(t, coverage[assetID])
	ownContent, err := store.CurrentTargetOwnContent(
		ctx,
		map[string]string{assetID: targetIdentity},
		environment,
	)
	require.NoError(t, err)
	assert.NotContains(t, ownContent, assetID)
}

func TestTargetWriteClaimMakesExistingWriterUnavailableWhileActiveOrDirty(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	baseline := targetMaterialization(
		"p:a", "prod", "v1:baseline", "vars", "run-1", "completion-1", 0, 1,
	)
	baseline.OwnContent = "own-baseline"
	require.NoError(t, store.Record(ctx, baseline))
	requireTargetAvailable(t, store, "p:a", testTargetIdentity, "prod", "vars")

	pending := targetMaterialization(
		"p:a", "prod", "v1:pending", "vars", "run-2", "completion-2", 0, 2,
	)
	claim := targetClaim(pending, 2)
	require.NoError(t, store.ClaimTargetWrite(ctx, claim))
	requireTargetUnavailable(t, store, "p:a", testTargetIdentity, "prod", "vars")

	require.NoError(t, store.MarkTargetWriteClaimDirty(ctx, claim, ts(3)))
	requireTargetUnavailable(t, store, "p:a", testTargetIdentity, "prod", "vars")
}

func TestSuccessfulClaimedRepairClearsItsClaimAndOlderDirtyClaims(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	baseline := targetMaterialization(
		"p:a", "prod", "v1:baseline", "vars", "run-1", "completion-1", 0, 1,
	)
	baseline.OwnContent = "own-baseline"
	require.NoError(t, store.Record(ctx, baseline))

	failed := targetMaterialization(
		"p:a", "prod", "v1:failed", "vars", "run-2", "completion-2", 0, 2,
	)
	failedClaim := targetClaim(failed, 2)
	require.NoError(t, store.ClaimTargetWrite(ctx, failedClaim))
	require.NoError(t, store.MarkTargetWriteClaimDirty(ctx, failedClaim, ts(3)))
	requireTargetUnavailable(t, store, "p:a", testTargetIdentity, "prod", "vars")

	repair := targetMaterialization(
		"p:a", "prod", "v1:repair", "vars", "run-3", "completion-3", 0, 4,
	)
	repair.OwnContent = "own-repair"
	require.NoError(t, store.ClaimTargetWrite(ctx, targetClaim(repair, 4)), "dirty claims must allow a repair")
	require.NoError(t, store.Record(ctx, repair))

	requireTargetAvailable(t, store, "p:a", testTargetIdentity, "prod", "vars")
	writers, err := store.LatestWriters(ctx, []string{testTargetIdentity})
	require.NoError(t, err)
	assert.Equal(t, "v1:repair", writers[testTargetIdentity].Fingerprint)
}

func TestWriterUpdateAndClaimResolutionAreAtomic(t *testing.T) {
	t.Parallel()
	schedulerStore, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = schedulerStore.Close() })
	store := matlog.NewStore(schedulerStore.DB())
	ctx := context.Background()

	baseline := targetMaterialization(
		"p:a", "prod", "v1:baseline", "vars", "run-1", "completion-1", 0, 1,
	)
	baseline.OwnContent = "own-baseline"
	require.NoError(t, store.Record(ctx, baseline))

	failed := targetMaterialization(
		"p:a", "prod", "v1:failed", "vars", "run-2", "completion-2", 0, 2,
	)
	failedClaim := targetClaim(failed, 2)
	require.NoError(t, store.ClaimTargetWrite(ctx, failedClaim))
	require.NoError(t, store.MarkTargetWriteClaimDirty(ctx, failedClaim, ts(3)))

	repair := targetMaterialization(
		"p:a", "prod", "v1:repair", "vars", "run-3", "completion-3", 0, 4,
	)
	repair.OwnContent = "own-repair"
	require.NoError(t, store.ClaimTargetWrite(ctx, targetClaim(repair, 4)))
	_, err = schedulerStore.DB().ExecContext(ctx, `
		CREATE TRIGGER reject_claimed_writer_update
		BEFORE UPDATE ON renart_latest_successful_writers
		WHEN NEW.fingerprint = 'v1:repair'
		BEGIN
			SELECT RAISE(ABORT, 'writer update rejected');
		END`)
	require.NoError(t, err)

	require.ErrorContains(t, store.Record(ctx, repair), "writer update rejected")
	requireTargetUnavailable(t, store, "p:a", testTargetIdentity, "prod", "vars")
	_, err = schedulerStore.DB().ExecContext(ctx, `DROP TRIGGER reject_claimed_writer_update`)
	require.NoError(t, err)
	require.NoError(t, store.Record(ctx, repair))
	requireTargetAvailable(t, store, "p:a", testTargetIdentity, "prod", "vars")
}

func TestLateOlderSuccessDoesNotClearNewerDirtyClaim(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	baseline := targetMaterialization(
		"p:a", "prod", "v1:baseline", "vars", "run-1", "completion-1", 0, 1,
	)
	baseline.OwnContent = "own-baseline"
	require.NoError(t, store.Record(ctx, baseline))

	older := targetMaterialization(
		"p:a", "prod", "v1:older", "vars", "run-2", "completion-2", 0, 2,
	)
	older.OwnContent = "own-older"
	olderClaim := targetClaim(older, 2)
	require.NoError(t, store.ClaimTargetWrite(ctx, olderClaim))
	require.NoError(t, store.MarkTargetWriteClaimDirty(ctx, olderClaim, ts(3)))

	newerFailed := targetMaterialization(
		"p:a", "prod", "v1:newer-failed", "vars", "run-3", "completion-3", 0, 4,
	)
	newerClaim := targetClaim(newerFailed, 4)
	require.NoError(t, store.ClaimTargetWrite(ctx, newerClaim))
	require.NoError(t, store.MarkTargetWriteClaimDirty(ctx, newerClaim, ts(5)))

	// Delayed fact recording can prove the older physical write succeeded, but
	// it cannot prove what the newer failed write left behind.
	require.NoError(t, store.Record(ctx, older))
	requireTargetUnavailable(t, store, "p:a", testTargetIdentity, "prod", "vars")

	repair := targetMaterialization(
		"p:a", "prod", "v1:repair", "vars", "run-4", "completion-4", 0, 6,
	)
	repair.OwnContent = "own-repair"
	require.NoError(t, store.ClaimTargetWrite(ctx, targetClaim(repair, 6)))
	require.NoError(t, store.Record(ctx, repair))
	requireTargetAvailable(t, store, "p:a", testTargetIdentity, "prod", "vars")
}

func TestTargetWriteClaimRejectsConcurrentActiveWriterButAllowsDirtyPredecessor(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	first := matlog.TargetWriteClaim{
		TargetIdentity: testTargetIdentity,
		CompletionID:   "completion-1",
		AssetID:        "p:a",
		ClaimedAt:      ts(1),
	}
	second := matlog.TargetWriteClaim{
		TargetIdentity: testTargetIdentity,
		CompletionID:   "completion-2",
		AssetID:        "p:b",
		ClaimedAt:      ts(2),
	}
	require.NoError(t, store.ClaimTargetWrite(ctx, first))
	require.ErrorIs(t, store.ClaimTargetWrite(ctx, second), matlog.ErrTargetWriteClaimActive)

	otherTarget := second
	otherTarget.TargetIdentity = "renart-physical-target-v1:other"
	require.NoError(t, store.ClaimTargetWrite(ctx, otherTarget), "claims are isolated by physical target")

	require.NoError(t, store.MarkTargetWriteClaimDirty(ctx, first, ts(3)))
	require.NoError(t, store.ClaimTargetWrite(ctx, second), "a dirty predecessor must not block repair")
}

func TestStartupConvertsActiveTargetWriteClaimsToRepairableDirtyState(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	baseline := targetMaterialization(
		"p:a", "prod", "v1:baseline", "vars", "run-1", "completion-1", 0, 1,
	)
	baseline.OwnContent = "own-baseline"
	require.NoError(t, store.Record(ctx, baseline))

	interrupted := targetMaterialization(
		"p:a", "prod", "v1:interrupted", "vars", "run-2", "completion-2", 0, 2,
	)
	require.NoError(t, store.ClaimTargetWrite(ctx, targetClaim(interrupted, 2)))
	converted, err := store.MarkActiveTargetWriteClaimsDirty(ctx, ts(3))
	require.NoError(t, err)
	assert.EqualValues(t, 1, converted)
	converted, err = store.MarkActiveTargetWriteClaimsDirty(ctx, ts(4))
	require.NoError(t, err)
	assert.Zero(t, converted, "startup conversion is idempotent")
	requireTargetUnavailable(t, store, "p:a", testTargetIdentity, "prod", "vars")

	repair := targetMaterialization(
		"p:a", "prod", "v1:repair", "vars", "run-3", "completion-3", 0, 5,
	)
	repair.OwnContent = "own-repair"
	require.NoError(t, store.ClaimTargetWrite(ctx, targetClaim(repair, 5)))
	require.NoError(t, store.Record(ctx, repair))
	requireTargetAvailable(t, store, "p:a", testTargetIdentity, "prod", "vars")
}

func TestAmbiguousTargetWriteCommitsDirtyClaimAndLaterRepairResolvesIt(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	first := targetMaterialization(
		"p:a", "prod", "v1:first", "vars", "run-1", "completion-1", 0, 1,
	)
	require.NoError(t, store.Record(ctx, first))

	ambiguous := targetMaterialization(
		"p:a", "prod", "v1:ambiguous", "vars", "run-2", "completion-2", 0, 1,
	)
	require.NoError(t, store.ClaimTargetWrite(ctx, targetClaim(ambiguous, 2)))
	require.ErrorIs(t, store.Record(ctx, ambiguous), matlog.ErrTargetWriterAmbiguous)
	requireTargetUnavailable(t, store, "p:a", testTargetIdentity, "prod", "vars")

	repair := targetMaterialization(
		"p:a", "prod", "v1:repair", "vars", "run-3", "completion-3", 0, 3,
	)
	require.NoError(t, store.ClaimTargetWrite(ctx, targetClaim(repair, 3)), "the ambiguous completion claim must be dirty, not active")
	require.NoError(t, store.Record(ctx, repair))
	requireTargetAvailable(t, store, "p:a", testTargetIdentity, "prod", "vars")
}

func TestTargetWriteClaimValidationAndTargetlessNoOp(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.ClaimTargetWrite(ctx, matlog.TargetWriteClaim{}))
	require.NoError(t, store.MarkTargetWriteClaimDirty(ctx, matlog.TargetWriteClaim{}, time.Time{}))
	converted, err := store.MarkActiveTargetWriteClaimsDirty(ctx, ts(1))
	require.NoError(t, err)
	assert.Zero(t, converted, "an empty/runtime-only target must not create a claim")

	require.ErrorContains(t, store.ClaimTargetWrite(ctx, matlog.TargetWriteClaim{
		TargetIdentity: testTargetIdentity,
	}), "completion_id and asset_id")
	require.ErrorIs(t, store.MarkTargetWriteClaimDirty(ctx, matlog.TargetWriteClaim{
		TargetIdentity: testTargetIdentity,
		CompletionID:   "missing",
		AssetID:        "p:a",
	}, ts(2)), matlog.ErrTargetWriteClaimNotFound)
}
