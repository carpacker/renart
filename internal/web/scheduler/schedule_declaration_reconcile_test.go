package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduleDeclarationReconcileKeepsDeploymentPinsLocal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	declarations := NewScheduleDeclarationStore(filepath.Join(t.TempDir(), ".renart", "schedules.yml"))
	require.NoError(t, declarations.Set("pipeline-uuid", "prod", ScheduleDeclaration{
		Cron: "@daily", Timezone: "UTC", CatchupPolicy: CatchupSkip,
		Variables:  map[string]any{"region": "eu"},
		SecretRefs: map[string]string{"token": "env:RENART_TOKEN"},
	}))
	require.NoError(t, store.UpsertEnvSchedule(ctx, EnvSchedule{
		PipelineUUID: "pipeline-uuid", Environment: "prod", SnapshotVersionID: "snapshot-local",
		Cron: "@hourly", Timezone: "UTC", CatchupPolicy: CatchupSkip, Status: ScheduleStatusActive,
	}))

	var validated map[string]any
	service := New(Options{
		Store: store, ScheduleDeclarations: declarations,
		ResolvePipelineRef: func(context.Context, string) (PipelineRef, bool) {
			return PipelineRef{EncodedID: "pipeline-id", Name: "analytics"}, true
		},
		ResolveScheduleSecrets: func(_ context.Context, environment string, refs map[string]string) (map[string]any, error) {
			assert.Equal(t, "prod", environment)
			assert.Equal(t, map[string]string{"token": "env:RENART_TOKEN"}, refs)
			return map[string]any{"token": "resolved-secret"}, nil
		},
		ValidateScheduleVariables: func(_ context.Context, _, _ string, values map[string]any) error {
			validated = cloneScheduleVariables(values)
			return nil
		},
	})
	require.NoError(t, service.reconcileScheduleDeclarations(ctx))

	row, found, err := store.GetEnvSchedule(ctx, "pipeline-uuid", "prod")
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, row.DeclarationManaged)
	assert.Equal(t, "snapshot-local", row.SnapshotVersionID)
	assert.Equal(t, ScheduleStatusActive, row.Status)
	assert.Equal(t, map[string]any{"region": "eu"}, row.Vars)
	assert.Equal(t, map[string]string{"token": "env:RENART_TOKEN"}, row.SecretRefs)
	assert.Equal(t, map[string]any{"region": "eu", "token": "resolved-secret"}, validated)
	var rawVariables string
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT vars FROM renart_schedules WHERE pipeline_id = ? AND environment = ?`,
		"pipeline-uuid", "prod",
	).Scan(&rawVariables))
	assert.Contains(t, rawVariables, "env:RENART_TOKEN")
	assert.NotContains(t, rawVariables, "resolved-secret")

	require.NoError(t, declarations.Remove("pipeline-uuid", "prod"))
	require.NoError(t, service.reconcileScheduleDeclarations(ctx))
	row, found, err = store.GetEnvSchedule(ctx, "pipeline-uuid", "prod")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ScheduleStatusArchived, row.Status)
	assert.Equal(t, ArchivedReasonDeclarationMissing, row.ArchivedReason)
	assert.Equal(t, "snapshot-local", row.SnapshotVersionID)

	require.NoError(t, declarations.Set("pipeline-uuid", "prod", ScheduleDeclaration{
		Cron: "@daily", Timezone: "UTC", CatchupPolicy: CatchupSkip,
	}))
	require.NoError(t, service.reconcileScheduleDeclarations(ctx))
	row, found, err = store.GetEnvSchedule(ctx, "pipeline-uuid", "prod")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ScheduleStatusActive, row.Status)
	assert.Empty(t, row.ArchivedReason)
	assert.Equal(t, "snapshot-local", row.SnapshotVersionID)
}

func TestScheduledSecretReferencesResolveEphemerallyAndFailOnPlanDrift(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	secret := "initial-secret"
	var plannedVariables map[string]any
	service := New(Options{
		Store: store,
		ResolvePipelineRef: func(context.Context, string) (PipelineRef, bool) {
			return PipelineRef{EncodedID: "pipeline-id", Name: "analytics"}, true
		},
		ResolveScheduleSecrets: func(context.Context, string, map[string]string) (map[string]any, error) {
			return map[string]any{"token": secret}, nil
		},
		PlanScheduledRun: func(ctx context.Context, req ScheduledRunPlanRequest) (ScheduledRunPlanResult, error) {
			plannedVariables = cloneScheduleVariables(req.VariableOverrides)
			return variableAwareScheduledPlan(ctx, req)
		},
	})
	start := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	prepared, shouldAdmit, err := service.prepareScheduledRunAdmission(ctx, pipelineRunJobArgs{
		PipelineUUID: "pipeline-uuid", PipelineName: "analytics", Environment: "prod",
		Schedule: "@hourly", Timezone: "UTC", Start: start.Format(time.RFC3339Nano),
		End: end.Format(time.RFC3339Nano), SnapshotVersionID: "snapshot-id",
		Variables:          map[string]any{"region": "eu"},
		VariableReferences: map[string]string{"token": "env:RENART_TOKEN"},
	})
	require.NoError(t, err)
	require.True(t, shouldAdmit)
	assert.Equal(t, map[string]any{"region": "eu", "token": "initial-secret"}, plannedVariables)
	assert.Equal(t, map[string]any{"region": "eu"}, prepared.Spec.Requested.Variables)
	assert.Equal(t, map[string]string{"token": "env:RENART_TOKEN"}, prepared.Spec.Requested.VariableReferences)
	body, err := json.Marshal(prepared.Spec)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "initial-secret")
	assert.Contains(t, string(body), "env:RENART_TOKEN")

	runID, err := store.CreateScheduleOccurrenceAttemptWithSpecAndPlan(
		ctx, prepared.Occurrence, prepared.Run, prepared.Spec, prepared.Plan,
	)
	require.NoError(t, err)
	executableRun, executionSpec, ok, err := service.prepareRun(ctx, 91, pipelineRunJobArgs{RunID: runID})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"region": "eu", "token": "initial-secret"}, executionSpec.Requested.Variables)
	assert.Nil(t, executionSpec.Requested.VariableReferences)
	assert.Equal(t, "initial-secret", executableRun.VariableOverrides["token"])

	persistedSpec, found, err := store.GetRunSpec(ctx, runID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, map[string]any{"region": "eu"}, persistedSpec.Requested.Variables)
	assert.Equal(t, map[string]string{"token": "env:RENART_TOKEN"}, persistedSpec.Requested.VariableReferences)

	secret = "rotated-secret"
	_, _, ok, err = service.prepareRun(ctx, 91, pipelineRunJobArgs{RunID: runID})
	var invalidPlan *invalidRunPlanError
	require.ErrorAs(t, err, &invalidPlan)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "no longer match the reviewed plan")
	assert.NotContains(t, err.Error(), secret)
}

func variableAwareScheduledPlan(
	ctx context.Context,
	req ScheduledRunPlanRequest,
) (ScheduledRunPlanResult, error) {
	base, err := testScheduledRunPlan(ctx, req)
	if err != nil {
		return ScheduledRunPlanResult{}, err
	}
	digest := sha256.Sum256([]byte(fmt.Sprint(req.VariableOverrides["token"])))
	identity := hex.EncodeToString(digest[:])
	base.Plan.PlanID = identity
	base.Plan.ConfigurationDigest = identity
	var artifact map[string]any
	if err := json.Unmarshal(base.Plan.Artifact, &artifact); err != nil {
		return ScheduledRunPlanResult{}, err
	}
	artifact["id"] = identity
	contextValue, ok := artifact["context"].(map[string]any)
	if !ok {
		return ScheduledRunPlanResult{}, fmt.Errorf("test plan context is unavailable")
	}
	contextValue["configuration_digest"] = identity
	base.Plan.Artifact, err = json.Marshal(artifact)
	if err != nil {
		return ScheduledRunPlanResult{}, err
	}
	if strings.TrimSpace(base.Plan.PlanID) == "" {
		return ScheduledRunPlanResult{}, fmt.Errorf("test plan identity is empty")
	}
	return base, nil
}
