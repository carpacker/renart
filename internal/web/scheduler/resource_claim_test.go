package scheduler

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreSerializesMatchingWriteResourcesAndAllowsDistinctOnes(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()

	localA := PipelineRunResourceClaim{Kind: PipelineRunResourceKindLocalFile, Identity: strings.Repeat("a", 64)}
	localB := PipelineRunResourceClaim{Kind: PipelineRunResourceKindLocalFile, Identity: strings.Repeat("b", 64)}

	runA, specA, planA := resourceClaimAdmission(t, "run-a", "pipeline-a", "uuid-a", PipelineRunPlanResources{
		Isolation: PipelineRunResourceIsolationResources, Claims: []PipelineRunResourceClaim{localA},
	})
	_, err = store.CreateWithSpecAndPlan(ctx, runA, specA, planA)
	require.NoError(t, err)
	require.NoError(t, store.validateActiveRunSpecSlotBinding(ctx, runA, specA))
	assert.Zero(t, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_slots WHERE run_id = ?`, runA.ID))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_resource_claims WHERE run_id = ?`, runA.ID))

	runB, specB, planB := resourceClaimAdmission(t, "run-b", "pipeline-a", "uuid-a", PipelineRunPlanResources{
		Isolation: PipelineRunResourceIsolationResources, Claims: []PipelineRunResourceClaim{localB},
	})
	_, err = store.CreateWithSpecAndPlan(ctx, runB, specB, planB)
	require.NoError(t, err, "distinct proven resources in one pipeline may execute concurrently")

	sensor, sensorSpec, sensorPlan := resourceClaimAdmission(t, "run-sensor", "pipeline-a", "uuid-a", PipelineRunPlanResources{
		Isolation: PipelineRunResourceIsolationResources, Claims: []PipelineRunResourceClaim{},
	})
	_, err = store.CreateWithSpecAndPlan(ctx, sensor, sensorSpec, sensorPlan)
	require.NoError(t, err, "a proven no-write run owns no mutation resource")
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_claim_sets WHERE run_id = ?`, sensor.ID))
	assert.Zero(t, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_resource_claims WHERE run_id = ?`, sensor.ID))

	sameTarget, sameSpec, samePlan := resourceClaimAdmission(t, "run-same", "pipeline-other", "uuid-other", PipelineRunPlanResources{
		Isolation: PipelineRunResourceIsolationResources, Claims: []PipelineRunResourceClaim{localA},
	})
	_, err = store.CreateWithSpecAndPlan(ctx, sameTarget, sameSpec, samePlan)
	var conflict *PipelineRunActiveError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, runA.ID, conflict.ActiveRunID)

	conservative, conservativeSpec, conservativePlan := resourceClaimAdmission(t, "run-conservative", "pipeline-a", "uuid-a", PipelineRunPlanResources{
		Isolation: PipelineRunResourceIsolationPipeline, Claims: []PipelineRunResourceClaim{},
	})
	_, err = store.CreateWithSpecAndPlan(ctx, conservative, conservativeSpec, conservativePlan)
	require.ErrorIs(t, err, ErrPipelineRunActive)

	mixed, mixedSpec, mixedPlan := resourceClaimAdmission(t, "run-mixed", "pipeline-other", "uuid-mixed", PipelineRunPlanResources{
		Isolation: PipelineRunResourceIsolationPipeline, Claims: []PipelineRunResourceClaim{localB},
	})
	_, err = store.CreateWithSpecAndPlan(ctx, mixed, mixedSpec, mixedPlan)
	require.ErrorIs(t, err, ErrPipelineRunActive, "known exact claims remain global even on a conservative plan")
	assert.Zero(t, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_slots WHERE run_id = ?`, mixed.ID),
		"a resource conflict rolls the provisional pipeline slots back")

	legacy, legacySpec, _ := resourceClaimAdmission(t, "run-legacy", "pipeline-a", "uuid-a", PipelineRunPlanResources{})
	_, err = store.CreateWithSpec(ctx, legacy, legacySpec)
	require.ErrorIs(t, err, ErrPipelineRunActive)

	previewConflict, err := store.ConflictingRunID(ctx, "pipeline-other", "uuid-other", samePlan.Resources)
	require.NoError(t, err)
	assert.Equal(t, runA.ID, previewConflict)

	require.NoError(t, store.FinalizeExecution(ctx, runA.ID, RunStatusFailed, time.Now().UTC(), assert.AnError, "", nil))
	assert.Zero(t, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_claim_sets WHERE run_id = ?`, runA.ID))
	assert.Zero(t, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_resource_claims WHERE run_id = ?`, runA.ID))
	_, err = store.CreateWithSpecAndPlan(ctx, sameTarget, sameSpec, samePlan)
	require.NoError(t, err, "terminalization releases the exact resource atomically")
}

func TestPipelineIsolatedClaimSetStillRequiresLegacyCompatibleSlots(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()
	run, spec, plan := resourceClaimAdmission(t, "run", "pipeline", "pipeline-uuid", PipelineRunPlanResources{
		Isolation: PipelineRunResourceIsolationPipeline, Claims: []PipelineRunResourceClaim{},
	})
	_, err = store.CreateWithSpecAndPlan(ctx, run, spec, plan)
	require.NoError(t, err)
	require.NoError(t, store.validateActiveRunSpecSlotBinding(ctx, run, spec))

	_, err = store.DB().ExecContext(ctx, `DELETE FROM pipeline_run_slots WHERE slot_key = ?`, "path:"+run.PipelineID)
	require.NoError(t, err)
	require.ErrorContains(t, store.validateActiveRunSpecSlotBinding(ctx, run, spec), "pipeline path")
}

func TestStoreBindsRuntimeWriteResourcesToReviewedClaims(t *testing.T) {
	t.Parallel()
	localA := PipelineRunResourceClaim{Kind: PipelineRunResourceKindLocalFile, Identity: strings.Repeat("a", 64)}
	localB := PipelineRunResourceClaim{Kind: PipelineRunResourceKindLocalFile, Identity: strings.Repeat("b", 64)}

	tests := []struct {
		name      string
		resources PipelineRunPlanResources
		snapshot  ExecutionTargetSnapshot
		wantError string
	}{
		{
			name: "matching exact resource",
			resources: PipelineRunPlanResources{
				Isolation: PipelineRunResourceIsolationResources, Claims: []PipelineRunResourceClaim{localA},
			},
			snapshot: resourceClaimSnapshot(ExecutionTargetSnapshotVersionV3, localA),
		},
		{
			name: "different exact resource",
			resources: PipelineRunPlanResources{
				Isolation: PipelineRunResourceIsolationResources, Claims: []PipelineRunResourceClaim{localA},
			},
			snapshot:  resourceClaimSnapshot(ExecutionTargetSnapshotVersionV3, localB),
			wantError: "do not match the reviewed plan",
		},
		{
			name: "legacy target snapshot",
			resources: PipelineRunPlanResources{
				Isolation: PipelineRunResourceIsolationResources, Claims: []PipelineRunResourceClaim{localA},
			},
			snapshot:  resourceClaimSnapshot(ExecutionTargetSnapshotVersionV2, PipelineRunResourceClaim{}),
			wantError: "has no write-resource evidence",
		},
		{
			name:      "matching conservative resource",
			resources: PipelineRunPlanResources{Isolation: PipelineRunResourceIsolationPipeline, Claims: []PipelineRunResourceClaim{}},
			snapshot:  resourceClaimSnapshot(ExecutionTargetSnapshotVersionV3, PipelineRunResourceClaim{}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
			require.NoError(t, err)
			defer store.Close()
			run, spec, plan := resourceClaimAdmission(t, "run", "pipeline", "pipeline-uuid", tt.resources)
			_, err = store.CreateWithSpecAndPlan(context.Background(), run, spec, plan)
			require.NoError(t, err)

			err = store.SetRunExecutionTargetSnapshot(context.Background(), run.ID, tt.snapshot)
			if tt.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantError)
		})
	}
}

func resourceClaimAdmission(
	t testing.TB,
	runID string,
	pipelineID string,
	pipelineUUID string,
	resources PipelineRunPlanResources,
) (PipelineRun, runSpecV1, PipelineRunPlan) {
	t.Helper()
	plan := validPipelineRunPlan(t)
	plan.Version = PipelineRunPlanVersionV2
	plan.PipelineID = pipelineID
	plan.PipelineUUID = pipelineUUID
	plan.Resources = resources
	plan.Artifact = pipelineRunPlanArtifact(t, plan)
	executionTime, err := time.Parse(time.RFC3339Nano, plan.ExecutionTime)
	require.NoError(t, err)
	run := PipelineRun{
		ID: runID, PipelineID: pipelineID, PipelineUUID: pipelineUUID, Pipeline: "analytics",
		Trigger: RunTriggerManual, Status: RunStatusQueued, ExecutionTime: &executionTime,
		ExpectedSourceMerkle: plan.SourceMerkle, ExpectedConfigurationDigest: plan.ConfigurationDigest,
	}
	spec := manualRunSpec(run, RunSourceWorkingTree, "")
	return run, spec, plan
}

func resourceClaimSnapshot(version int, claim PipelineRunResourceClaim) ExecutionTargetSnapshot {
	snapshot := testExecutionTargetSnapshot()
	delete(snapshot.Entries, "analytics.sensor")
	snapshot.Version = version
	if version < ExecutionTargetSnapshotVersionV3 {
		return snapshot
	}
	entry := snapshot.Entries["analytics.orders"]
	if claim.Kind == "" {
		entry.WriteResourceKind = "pipeline"
		entry.WriteResourceFidelity = ExecutionTargetFidelityRuntimeOnly
	} else {
		entry.WriteResourceKind = claim.Kind
		entry.WriteResourceIdentity = claim.Identity
		entry.WriteResourceFidelity = ExecutionTargetFidelityExact
	}
	snapshot.Entries["analytics.orders"] = entry
	return snapshot
}

func TestPipelineRunPlanV2ValidatesCanonicalResourceClaims(t *testing.T) {
	t.Parallel()
	plan := validPipelineRunPlan(t)
	plan.Version = PipelineRunPlanVersionV2
	plan.Resources = PipelineRunPlanResources{
		Isolation: PipelineRunResourceIsolationResources,
		Claims: []PipelineRunResourceClaim{{
			Kind: PipelineRunResourceKindDuckDBDatabase, Identity: strings.Repeat("a", 64),
		}},
	}
	plan.Artifact = pipelineRunPlanArtifact(t, plan)
	require.NoError(t, plan.validate())

	unsorted := plan
	unsorted.Resources.Claims = []PipelineRunResourceClaim{
		{Kind: PipelineRunResourceKindLocalFile, Identity: strings.Repeat("b", 64)},
		{Kind: PipelineRunResourceKindDuckDBDatabase, Identity: strings.Repeat("a", 64)},
	}
	unsorted.Artifact = pipelineRunPlanArtifact(t, unsorted)
	require.ErrorContains(t, unsorted.validate(), "sorted and unique")

	legacy := validPipelineRunPlan(t)
	legacy.Resources = plan.Resources
	legacy.Artifact = pipelineRunPlanArtifact(t, legacy)
	require.ErrorContains(t, legacy.validate(), "v1 cannot contain resource claims")
}
