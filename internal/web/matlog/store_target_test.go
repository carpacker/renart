package matlog_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/matlog"
)

const testTargetIdentity = "renart-physical-target-v1:test"

func targetMaterialization(
	assetID string,
	environment string,
	fingerprint string,
	varsHash string,
	runID string,
	completionID string,
	completionOrdinal int64,
	materializedHour int,
) matlog.Materialization {
	return matlog.Materialization{
		AssetID:           assetID,
		Environment:       environment,
		Fingerprint:       fingerprint,
		VarsHash:          varsHash,
		RunID:             runID,
		TargetIdentity:    testTargetIdentity,
		CompletionID:      completionID,
		CompletionOrdinal: completionOrdinal,
		MaterializedAt:    ts(materializedHour),
	}
}

func TestTargetGenerationPreventsHistoricalCoverageResurrection(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	first := targetMaterialization("p:a", "prod", "v1:a", "vars", "run-a1", "completion-a1", 0, 1)
	first.IntervalStart, first.IntervalEnd = interval(0, 1)
	require.NoError(t, store.Record(ctx, first))

	// The same source/variables pair extends the current generation.
	second := targetMaterialization("p:a", "prod", "v1:a", "vars", "run-a2", "completion-a2", 0, 2)
	second.IntervalStart, second.IntervalEnd = interval(1, 2)
	require.NoError(t, store.Record(ctx, second))
	writers, err := store.LatestWriters(ctx, []string{testTargetIdentity})
	require.NoError(t, err)
	assert.EqualValues(t, 1, writers[testTargetIdentity].TargetGeneration)
	current, err := store.CurrentTargetCoverage(
		ctx,
		map[string]string{"p:a": testTargetIdentity},
		"prod",
		"vars",
	)
	require.NoError(t, err)
	require.Len(t, current["p:a"], 1)
	assert.Equal(t, ts(0), *current["p:a"][0].IntervalStart)
	assert.Equal(t, ts(2), *current["p:a"][0].IntervalEnd)

	// A different variant advances the target generation.
	variantB := targetMaterialization("p:a", "prod", "v1:b", "vars", "run-b", "completion-b", 0, 3)
	variantB.IntervalStart, variantB.IntervalEnd = interval(3, 4)
	require.NoError(t, store.Record(ctx, variantB))
	writers, err = store.LatestWriters(ctx, []string{testTargetIdentity})
	require.NoError(t, err)
	assert.EqualValues(t, 2, writers[testTargetIdentity].TargetGeneration)

	// Returning to A starts generation three; generation one's broader
	// coverage is retained for audit but cannot become current again.
	revertedA := targetMaterialization("p:a", "prod", "v1:a", "vars", "run-a3", "completion-a3", 0, 4)
	revertedA.IntervalStart, revertedA.IntervalEnd = interval(4, 5)
	require.NoError(t, store.Record(ctx, revertedA))
	writers, err = store.LatestWriters(ctx, []string{testTargetIdentity})
	require.NoError(t, err)
	writer := writers[testTargetIdentity]
	assert.EqualValues(t, 3, writer.TargetGeneration)
	assert.Equal(t, "v1:a", writer.Fingerprint)
	assert.Equal(t, "run-a3", writer.RunID)
	assert.Equal(t, "completion-a3", writer.CompletionID)

	current, err = store.CurrentTargetCoverage(
		ctx,
		map[string]string{"p:a": testTargetIdentity},
		"prod",
		"vars",
	)
	require.NoError(t, err)
	require.Len(t, current["p:a"], 1)
	assert.EqualValues(t, 3, current["p:a"][0].TargetGeneration)
	assert.Equal(t, ts(4), *current["p:a"][0].IntervalStart)
	assert.Equal(t, ts(5), *current["p:a"][0].IntervalEnd)

	all, err := store.Coverage(ctx, []string{"p:a"}, "prod", "vars")
	require.NoError(t, err)
	assert.Len(t, all["p:a"], 3, "historical generations remain durable evidence")
}

func TestLatestWriterIsGlobalAcrossAssetsAndEnvironments(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	first := targetMaterialization("p:a", "dev", "v1:same", "vars", "run-shared", "completion-shared", 0, 1)
	require.NoError(t, store.Record(ctx, first))
	second := targetMaterialization("other:b", "prod", "v1:same", "vars", "run-shared", "completion-shared", 1, 1)
	require.NoError(t, store.Record(ctx, second))

	writers, err := store.LatestWriters(ctx, []string{"", testTargetIdentity, testTargetIdentity})
	require.NoError(t, err)
	require.Len(t, writers, 1)
	writer := writers[testTargetIdentity]
	assert.EqualValues(t, 2, writer.TargetGeneration, "a different asset/environment writer starts a new generation")
	assert.Equal(t, "other:b", writer.AssetID)
	assert.Equal(t, "prod", writer.Environment)
	assert.EqualValues(t, 1, writer.CompletionOrdinal)

	dev, err := store.CurrentTargetCoverage(
		ctx,
		map[string]string{"p:a": testTargetIdentity},
		"dev",
		"vars",
	)
	require.NoError(t, err)
	assert.Empty(t, dev, "the displaced writer cannot reuse same-generation coverage")
	prod, err := store.CurrentTargetCoverage(
		ctx,
		map[string]string{"other:b": testTargetIdentity},
		"prod",
		"vars",
	)
	require.NoError(t, err)
	require.Len(t, prod["other:b"], 1)
	assert.EqualValues(t, 2, prod["other:b"][0].TargetGeneration)
}

func TestWriterScopeRoundTripCannotResurrectReplacedCoverage(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	devFirst := targetMaterialization("p:a", "dev", "v1:same", "vars", "run-dev-1", "completion-dev-1", 0, 1)
	devFirst.IntervalStart, devFirst.IntervalEnd = interval(0, 10)
	require.NoError(t, store.Record(ctx, devFirst))

	prodReplacement := targetMaterialization("p:a", "prod", "v1:same", "vars", "run-prod", "completion-prod", 0, 2)
	prodReplacement.IntervalStart, prodReplacement.IntervalEnd = interval(10, 20)
	prodReplacement.ReplaceCoverage = true
	require.NoError(t, store.Record(ctx, prodReplacement))

	devReturn := targetMaterialization("p:a", "dev", "v1:same", "vars", "run-dev-2", "completion-dev-2", 0, 3)
	devReturn.IntervalStart, devReturn.IntervalEnd = interval(20, 30)
	require.NoError(t, store.Record(ctx, devReturn))

	writers, err := store.LatestWriters(ctx, []string{testTargetIdentity})
	require.NoError(t, err)
	assert.EqualValues(t, 3, writers[testTargetIdentity].TargetGeneration)

	current, err := store.CurrentTargetCoverage(
		ctx,
		map[string]string{"p:a": testTargetIdentity},
		"dev",
		"vars",
	)
	require.NoError(t, err)
	require.Len(t, current["p:a"], 1)
	assert.EqualValues(t, 3, current["p:a"][0].TargetGeneration)
	assert.Equal(t, ts(20), *current["p:a"][0].IntervalStart)
	assert.Equal(t, ts(30), *current["p:a"][0].IntervalEnd)
}

func TestCoverageRemainsIsolatedAcrossPhysicalTargets(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	targetA := targetMaterialization("p:a", "prod", "v1:a", "vars", "run-a1", "completion-a1", 0, 1)
	targetA.TargetIdentity = "renart-physical-target-v1:a"
	targetA.IntervalStart, targetA.IntervalEnd = interval(0, 2)
	require.NoError(t, store.Record(ctx, targetA))

	targetB := targetMaterialization("p:a", "prod", "v1:a", "vars", "run-b1", "completion-b1", 0, 2)
	targetB.TargetIdentity = "renart-physical-target-v1:b"
	targetB.IntervalStart, targetB.IntervalEnd = interval(2, 4)
	require.NoError(t, store.Record(ctx, targetB))

	all, err := store.Coverage(ctx, []string{"p:a"}, "prod", "vars")
	require.NoError(t, err)
	require.Len(t, all["p:a"], 2)
	assert.Equal(t, "renart-physical-target-v1:a", all["p:a"][0].TargetIdentity)
	assert.Equal(t, "renart-physical-target-v1:b", all["p:a"][1].TargetIdentity)

	currentA, err := store.CurrentTargetCoverage(
		ctx,
		map[string]string{"p:a": "renart-physical-target-v1:a"},
		"prod",
		"vars",
	)
	require.NoError(t, err)
	require.Len(t, currentA["p:a"], 1)
	assert.Equal(t, ts(0), *currentA["p:a"][0].IntervalStart)
	assert.Equal(t, ts(2), *currentA["p:a"][0].IntervalEnd)

	// Replacement at target A must not erase evidence for target B, even when
	// the asset/environment/source context is otherwise identical.
	replacement := targetMaterialization("p:a", "prod", "v1:a", "vars", "run-a2", "completion-a2", 0, 3)
	replacement.TargetIdentity = "renart-physical-target-v1:a"
	replacement.IntervalStart, replacement.IntervalEnd = interval(1, 2)
	replacement.ReplaceCoverage = true
	require.NoError(t, store.Record(ctx, replacement))

	all, err = store.Coverage(ctx, []string{"p:a"}, "prod", "vars")
	require.NoError(t, err)
	require.Len(t, all["p:a"], 2)
	assert.Equal(t, ts(1), *all["p:a"][0].IntervalStart)
	assert.Equal(t, ts(2), *all["p:a"][0].IntervalEnd)
	assert.Equal(t, ts(2), *all["p:a"][1].IntervalStart)
	assert.Equal(t, ts(4), *all["p:a"][1].IntervalEnd)
}

func TestCompletionOrdinalOrdersWritesWithinOneCompletion(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	first := targetMaterialization("p:a", "prod", "v1:a", "vars", "run", "completion", 0, 1)
	require.NoError(t, store.Record(ctx, first))
	second := targetMaterialization("p:b", "prod", "v1:b", "vars", "run", "completion", 1, 1)
	require.NoError(t, store.Record(ctx, second))

	writers, err := store.LatestWriters(ctx, []string{testTargetIdentity})
	require.NoError(t, err)
	writer := writers[testTargetIdentity]
	assert.False(t, writer.Ambiguous)
	assert.EqualValues(t, 2, writer.TargetGeneration)
	assert.Equal(t, "p:b", writer.AssetID)
	assert.EqualValues(t, 1, writer.CompletionOrdinal)
}

func TestOlderTargetCompletionAndReplayCannotReplaceWriter(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	latest := targetMaterialization("p:a", "prod", "v1:new", "vars", "run-new", "completion-new", 0, 2)
	require.NoError(t, store.Record(ctx, latest))

	older := targetMaterialization("p:a", "prod", "v1:old", "vars", "run-old", "completion-old", 0, 1)
	require.NoError(t, store.Record(ctx, older))
	require.NoError(t, store.Record(ctx, latest), "an exact recovery replay is a no-op")

	conflicting := latest
	conflicting.AssetID = "other:b"
	conflicting.Fingerprint = "v1:conflict"
	err := store.Record(ctx, conflicting)
	require.ErrorIs(t, err, matlog.ErrTargetWriterAmbiguous)

	writers, err := store.LatestWriters(ctx, []string{testTargetIdentity})
	require.NoError(t, err)
	writer := writers[testTargetIdentity]
	assert.Equal(t, "p:a", writer.AssetID)
	assert.Equal(t, "v1:new", writer.Fingerprint)
	assert.EqualValues(t, 1, writer.TargetGeneration)
	assert.True(t, writer.Ambiguous)
	current, err := store.CurrentTargetCoverage(
		ctx,
		map[string]string{"p:a": testTargetIdentity},
		"prod",
		"vars",
	)
	require.NoError(t, err)
	assert.Empty(t, current, "an ambiguous physical writer must never make coverage fresh")
	facts, err := store.CountFacts(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, facts, "ignored and conflicting completions write no partial facts")

	// A strictly newer completion resolves ambiguity in a fresh generation,
	// even when it writes the same fingerprint/variables pair as the retained
	// pre-ambiguity writer.
	repair := targetMaterialization("p:a", "prod", "v1:new", "vars", "run-repair", "completion-repair", 0, 3)
	require.NoError(t, store.Record(ctx, repair))
	writers, err = store.LatestWriters(ctx, []string{testTargetIdentity})
	require.NoError(t, err)
	writer = writers[testTargetIdentity]
	assert.False(t, writer.Ambiguous)
	assert.EqualValues(t, 2, writer.TargetGeneration)
	current, err = store.CurrentTargetCoverage(
		ctx,
		map[string]string{"p:a": testTargetIdentity},
		"prod",
		"vars",
	)
	require.NoError(t, err)
	require.Len(t, current["p:a"], 1)
	assert.EqualValues(t, 2, current["p:a"][0].TargetGeneration)
}

func TestEqualTimestampDifferentCompletionIDsFailClosed(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	first := targetMaterialization("p:a", "prod", "v1:a", "vars", "run-a", "completion-a", 0, 1)
	require.NoError(t, store.Record(ctx, first))
	second := targetMaterialization("p:a", "prod", "v1:b", "vars", "run-b", "completion-b", 0, 1)
	require.ErrorIs(t, store.Record(ctx, second), matlog.ErrTargetWriterAmbiguous)

	writers, err := store.LatestWriters(ctx, []string{testTargetIdentity})
	require.NoError(t, err)
	assert.True(t, writers[testTargetIdentity].Ambiguous)
	current, err := store.CurrentTargetCoverage(
		ctx,
		map[string]string{"p:a": testTargetIdentity},
		"prod",
		"vars",
	)
	require.NoError(t, err)
	assert.Empty(t, current)
}

func TestLegacyTargetlessFactsRemainGenerationZero(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.Record(ctx, matlog.Materialization{
		AssetID:        "p:a",
		Environment:    "prod",
		Fingerprint:    "v1:legacy",
		VarsHash:       "vars",
		MaterializedAt: ts(1),
	}))

	rows, err := store.Coverage(ctx, []string{"p:a"}, "prod", "vars")
	require.NoError(t, err)
	require.Len(t, rows["p:a"], 1)
	assert.Empty(t, rows["p:a"][0].TargetIdentity)
	assert.Zero(t, rows["p:a"][0].TargetGeneration)
	writers, err := store.LatestWriters(ctx, []string{"", testTargetIdentity})
	require.NoError(t, err)
	assert.Empty(t, writers)
	current, err := store.CurrentTargetCoverage(
		ctx,
		map[string]string{"p:a": ""},
		"prod",
		"vars",
	)
	require.NoError(t, err)
	assert.Empty(t, current, "legacy targetless evidence is not trusted as current target coverage")
}

func TestTargetAwareMaterializationRequiresStableCompletionIdentity(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	withoutID := targetMaterialization("p:a", "prod", "v1:a", "vars", "", "", 0, 1)
	require.ErrorContains(t, store.Record(ctx, withoutID), "completion_id or run_id is required")
	negativeOrdinal := targetMaterialization("p:a", "prod", "v1:a", "vars", "run", "completion", -1, 1)
	require.ErrorContains(t, store.Record(ctx, negativeOrdinal), "completion ordinal must not be negative")

	withRunFallback := targetMaterialization("p:a", "prod", "v1:a", "vars", "run-fallback", "", 0, 1)
	require.NoError(t, store.Record(ctx, withRunFallback))
	writers, err := store.LatestWriters(ctx, []string{testTargetIdentity})
	require.NoError(t, err)
	assert.Equal(t, "run-fallback", writers[testTargetIdentity].CompletionID)
}

func TestTargetAwareReplayRejectsConflictingFactEvidenceBeforeWriterMutation(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	original := targetMaterialization("p:a", "prod", "v1:a", "vars", "stable-run", "stable-completion", 0, 1)
	original.OwnContent = "own-a"
	original.IntervalStart, original.IntervalEnd = interval(0, 1)
	require.NoError(t, store.Record(ctx, original))
	require.NoError(t, store.Record(ctx, original), "an exact completion replay is a no-op")

	conflicts := []struct {
		name   string
		mutate func(*matlog.Materialization)
	}{
		{name: "fingerprint", mutate: func(m *matlog.Materialization) { m.Fingerprint = "v1:b" }},
		{name: "own content", mutate: func(m *matlog.Materialization) { m.OwnContent = "own-b" }},
		{name: "variables", mutate: func(m *matlog.Materialization) { m.VarsHash = "other-vars" }},
		{name: "target", mutate: func(m *matlog.Materialization) { m.TargetIdentity = "renart-physical-target-v1:other" }},
		{name: "window", mutate: func(m *matlog.Materialization) { m.IntervalStart, m.IntervalEnd = interval(1, 2) }},
		{name: "timestamp", mutate: func(m *matlog.Materialization) { m.MaterializedAt = ts(2) }},
		{name: "completion id", mutate: func(m *matlog.Materialization) { m.CompletionID = "other-completion" }},
		{name: "completion ordinal", mutate: func(m *matlog.Materialization) { m.CompletionOrdinal = 1 }},
	}
	for _, tt := range conflicts {
		t.Run(tt.name, func(t *testing.T) {
			conflict := original
			tt.mutate(&conflict)
			require.ErrorIs(t, store.Record(ctx, conflict), matlog.ErrMaterializationReplayConflict)
		})
	}

	writers, err := store.LatestWriters(ctx, []string{testTargetIdentity, "renart-physical-target-v1:other"})
	require.NoError(t, err)
	require.Len(t, writers, 1)
	writer := writers[testTargetIdentity]
	assert.False(t, writer.Ambiguous)
	assert.EqualValues(t, 1, writer.TargetGeneration)
	assert.Equal(t, "v1:a", writer.Fingerprint)
	facts, err := store.CountFacts(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, facts)
}

func TestPruneKeepsDurableLatestWriter(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.Record(ctx, targetMaterialization(
		"p:a", "prod", "v1:a", "vars", "run", "completion", 0, 1,
	)))
	pruned, err := store.Prune(ctx, ts(2))
	require.NoError(t, err)
	assert.EqualValues(t, 1, pruned)
	writers, err := store.LatestWriters(ctx, []string{testTargetIdentity})
	require.NoError(t, err)
	assert.Equal(t, "v1:a", writers[testTargetIdentity].Fingerprint)
}
