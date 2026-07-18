package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/google/uuid"
	webmodel "renart/internal/web/model"
	"renart/internal/web/policy"
	"renart/internal/web/staleness"
)

// StaleAssetPlan is one stale asset to rebuild. Windows carries the uncovered
// gap intervals of a partially-covered incremental; empty means one build over
// the selection (or the pipeline's default) window.
type StaleAssetPlan struct {
	AssetName string
	Windows   []ExecutionTimeWindow
}

// PipelineUpstreamNames returns the transitive in-pipeline upstream closure
// for targetAssetName. The target itself is excluded, including when a cycle
// points back to it. Unknown/URI dependencies are ignored; pipeline type-check
// reports those separately before execution.
func PipelineUpstreamNames(view webmodel.Pipeline, targetAssetName string) (map[string]struct{}, bool) {
	assetByName := make(map[string]webmodel.Asset, len(view.Assets))
	for _, asset := range view.Assets {
		assetByName[asset.Name] = asset
	}
	targetAssetName = strings.TrimSpace(targetAssetName)
	target, ok := assetByName[targetAssetName]
	if !ok {
		return nil, false
	}

	upstreams := make(map[string]struct{})
	queue := append([]string(nil), target.Upstreams...)
	for len(queue) > 0 {
		name := strings.TrimSpace(queue[0])
		queue = queue[1:]
		if name == "" || name == targetAssetName {
			continue
		}
		if _, seen := upstreams[name]; seen {
			continue
		}
		asset, exists := assetByName[name]
		if !exists {
			continue
		}
		upstreams[name] = struct{}{}
		queue = append(queue, asset.Upstreams...)
	}
	return upstreams, true
}

// BuildStalePlan translates staleness classifications into executable plan
// items. When include is non-nil, only those asset names are considered; an
// empty non-nil set therefore means there is deliberately nothing to build.
func BuildStalePlan(statuses []staleness.AssetStatus, include map[string]struct{}) []StaleAssetPlan {
	plan := make([]StaleAssetPlan, 0, len(statuses))
	for _, status := range statuses {
		if status.Status == staleness.StatusFresh {
			continue
		}
		if include != nil {
			if _, selected := include[status.AssetName]; !selected {
				continue
			}
		}
		item := StaleAssetPlan{AssetName: status.AssetName}
		for _, gap := range status.Gaps {
			item.Windows = append(item.Windows, ExecutionTimeWindow{Start: gap.Start, End: gap.End})
		}
		plan = append(plan, item)
	}
	return plan
}

// StaleBuildEvent reports per-asset progress of a stale build stream.
type StaleBuildEvent struct {
	AssetName string `json:"asset_name"`
	Status    string `json:"status"` // "running" / "succeeded" / "failed" / "skipped"
	Step      int    `json:"step"`
	Total     int    `json:"total"`
}

// MaterializeStaleAssetsStream rebuilds the given stale assets as one
// operation: dependency (topological) order so downstreams read freshly built
// upstreams, one combined output stream, and one RunCompleted bus event per
// built window so coverage and achieved fingerprints are recorded exactly as
// executed. Assets downstream of a failed plan member are skipped — building
// them would read the failed upstream's outdated table and stay stale anyway.
func (s *ExecutionService) MaterializeStaleAssetsStream(
	ctx context.Context,
	pipelineID, environment string,
	plan []StaleAssetPlan,
	startDate, endDate string,
	onChunk func([]byte),
	onEvent func(StaleBuildEvent),
) MaterializeResult {
	if err := s.checkRunPolicy(policy.RunRequest{Environment: environment, Interactive: true}); err != nil {
		return MaterializeResult{Status: "error", Error: err.Error(), ExitCode: 1}
	}

	relPipelinePath, err := DecodeID(pipelineID)
	if err != nil {
		return MaterializeResult{Status: "error", Error: "invalid pipeline id", ExitCode: 1}
	}
	operation := runOperation(relPipelinePath, pipelineID, "", environment)
	if len(plan) == 0 {
		return MaterializeResult{Status: "ok", Operation: operation, Output: "Everything is fresh; nothing to build.\n"}
	}

	absPipelinePath, err := NewWorkspaceResolver(s.deps.WorkspaceRoot, nil).JoinPath(relPipelinePath)
	if err != nil {
		return MaterializeResult{Status: "error", Operation: operation, Error: err.Error(), ExitCode: 1}
	}
	parsed, err := s.deps.NewPipelineBuilder().CreatePipelineFromPath(ctx, absPipelinePath, pipeline.WithMutate())
	if err != nil {
		return MaterializeResult{Status: "error", Operation: operation, Error: err.Error(), ExitCode: 1}
	}

	defaultWindow, windowErr := ResolveExecutionTimeWindow(string(parsed.Schedule), startDate, endDate, time.Now().UTC())
	if windowErr != nil {
		return MaterializeResult{Status: "error", Operation: operation, Error: windowErr.Error(), ExitCode: 1}
	}
	operation = withOperationTimeWindow(operation, defaultWindow)

	pipelineView := PipelineView{}
	pipelineFound := false
	if s.deps.CurrentPipelines != nil {
		for _, view := range s.deps.CurrentPipelines() {
			if view.ID == pipelineID {
				pipelineView = view
				pipelineFound = true
				break
			}
		}
	}

	ordered, unknown := orderStalePlan(parsed, plan)
	var releaseExecutionLease func() error
	if len(ordered) > 0 {
		releaseExecutionLease, err = s.acquireExecutionLease(ctx)
		if err != nil {
			return MaterializeResult{
				Status:    "error",
				Operation: operation,
				Error:     "acquire workspace execution lease: " + err.Error(),
				ExitCode:  1,
			}
		}
		defer func() { _ = releaseExecutionLease() }()
	}
	emit := func(event StaleBuildEvent) {
		if onEvent != nil {
			onEvent(event)
		}
	}
	writeChunk := func(text string) {
		if onChunk != nil && text != "" {
			onChunk([]byte(text))
		}
	}
	var combined strings.Builder
	logLine := func(text string) {
		combined.WriteString(text)
		writeChunk(text)
	}
	for _, name := range unknown {
		logLine(fmt.Sprintf("Skipping %s: not found in the pipeline.\n", name))
	}

	total := len(ordered)
	failed := make(map[string]bool)
	failedNames := make([]string, 0)
	builtAssetIDs := make([]string, 0, total)

	for index, step := range ordered {
		asset := step.asset
		if upstream := failedUpstreamFor(asset, parsed, failed); upstream != "" {
			logLine(fmt.Sprintf("\nSkipping %s (%d/%d): upstream %s failed, a build now would still be stale.\n", asset.Name, index+1, total, upstream))
			emit(StaleBuildEvent{AssetName: asset.Name, Status: "skipped", Step: index + 1, Total: total})
			continue
		}

		windows := step.plan.Windows
		if len(windows) == 0 {
			windows = []ExecutionTimeWindow{defaultWindow}
		}
		emit(StaleBuildEvent{AssetName: asset.Name, Status: "running", Step: index + 1, Total: total})

		assetPath := assetRunPathForPipelineAsset(s.deps.WorkspaceRoot, asset)
		encodedAssetID := encodePipelineAssetID(s.deps.WorkspaceRoot, asset)
		assetFailed := false
		for _, window := range windows {
			suffix := ""
			if len(step.plan.Windows) > 0 {
				suffix = fmt.Sprintf(" [%s → %s]", window.StartRFC3339(), window.EndRFC3339())
			}
			logLine(fmt.Sprintf("\n━━ Building %s (%d/%d)%s ━━\n", asset.Name, index+1, total, suffix))

			completionID := uuid.NewString()
			observed := newPipelineRunObservation(nil)
			observed.configureTargetWrites(ctx, completionID, s.deps.TargetWrites)
			output, runErr := s.runSingleAssetMaterializationObserved(
				ctx, assetPath, environment, window, false, sensorModeOnce,
				onChunk, observed.handle, observed.captureExecutionTargets, observed.beginTargetWrite,
			)
			combined.Write(output)
			if pipelineFound || observed.pipelineUUID() != "" {
				completionStatus := "succeeded"
				if runErr != nil {
					completionStatus = "failed"
					if executionWasCancelled(ctx, runErr) {
						completionStatus = "cancelled"
					}
				}
				runAssets, _ := observed.completedAssetsForNames(pipelineView, completionStatus, []string{asset.Name})
				if len(runAssets) > 0 {
					now := time.Now().UTC()
					pipelineUUID := observed.pipelineUUID()
					if pipelineUUID == "" {
						pipelineUUID = pipelineView.UUID
					}
					completionErr := s.emitRunCompletedForSpec(ctx, PipelineRunSpec{
						CompletionID:            completionID,
						Environment:             environment,
						executionTargetSnapshot: observed.executionTargetSnapshot(),
					}, pipelineUUID, window, now, runAssets)
					if completionErr != nil {
						completionErr = errors.Join(completionErr, observed.markSuccessfulTargetWritesDirty(now))
						runErr = errors.Join(runErr, fmt.Errorf("record durable completion: %w", completionErr))
					}
				}
			}

			if runErr != nil {
				message := runErr.Error()
				if IsDuckDBLockError(runErr, output) {
					message = "duckdb database is busy (lock held by another process), please retry"
				}
				logLine(fmt.Sprintf("\n%s failed: %s\n", asset.Name, message))
				assetFailed = true
				break
			}

			// Record each window as its own completed run so the coverage log
			// and achieved fingerprints reflect exactly what was executed; bus
			// handlers run synchronously, so downstreams built later in this
			// loop already see this asset's fresh fingerprint.
		}

		if assetFailed {
			failed[asset.Name] = true
			failedNames = append(failedNames, asset.Name)
			emit(StaleBuildEvent{AssetName: asset.Name, Status: "failed", Step: index + 1, Total: total})
			continue
		}
		builtAssetIDs = append(builtAssetIDs, encodedAssetID)
		emit(StaleBuildEvent{AssetName: asset.Name, Status: "succeeded", Step: index + 1, Total: total})
	}

	changedAssetIDs := make([]string, 0)
	var materializedAt *time.Time
	if len(builtAssetIDs) > 0 {
		now := time.Now().UTC()
		materializedAt = &now
		changedAssetIDs = s.deps.FindInspectIDs(builtAssetIDs...)
	}

	result := MaterializeResult{
		Status:          "ok",
		Operation:       operation,
		Output:          combined.String(),
		ChangedAssetIDs: coalesceMaterializedAssetIDs(changedAssetIDs, builtAssetIDs),
		MaterializedAt:  materializedAt,
	}
	if len(failedNames) > 0 {
		result.Status = "error"
		result.ExitCode = 1
		result.Error = fmt.Sprintf("%d of %d stale assets failed: %s", len(failedNames), total, strings.Join(failedNames, ", "))
	}
	return result
}

type stalePlanStep struct {
	asset *pipeline.Asset
	plan  StaleAssetPlan
}

// orderStalePlan resolves plan entries against the parsed pipeline and orders
// them topologically (Kahn over the full dependency graph, ties broken by
// pipeline declaration order) so upstreams always build before downstreams.
func orderStalePlan(parsed *pipeline.Pipeline, plan []StaleAssetPlan) (steps []stalePlanStep, unknown []string) {
	planByName := make(map[string]StaleAssetPlan, len(plan))
	for _, item := range plan {
		planByName[item.AssetName] = item
	}

	assetByName := make(map[string]*pipeline.Asset, len(parsed.Assets))
	for _, asset := range parsed.Assets {
		assetByName[asset.Name] = asset
	}
	for _, item := range plan {
		if assetByName[item.AssetName] == nil {
			unknown = append(unknown, item.AssetName)
		}
	}

	indegree := make(map[string]int, len(parsed.Assets))
	downstream := make(map[string][]string)
	for _, asset := range parsed.Assets {
		indegree[asset.Name] += 0
		for _, up := range asset.Upstreams {
			upName := strings.TrimSpace(up.Value)
			if upName == "" || assetByName[upName] == nil {
				continue
			}
			indegree[asset.Name]++
			downstream[upName] = append(downstream[upName], asset.Name)
		}
	}

	queue := make([]string, 0, len(parsed.Assets))
	for _, asset := range parsed.Assets {
		if indegree[asset.Name] == 0 {
			queue = append(queue, asset.Name)
		}
	}
	visited := make(map[string]bool, len(parsed.Assets))
	appendStep := func(name string) {
		if item, ok := planByName[name]; ok {
			steps = append(steps, stalePlanStep{asset: assetByName[name], plan: item})
		}
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if visited[name] {
			continue
		}
		visited[name] = true
		appendStep(name)
		for _, next := range downstream[name] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	// Cycles never reach indegree zero; append them in declaration order so
	// they still build rather than silently dropping out of the plan.
	for _, asset := range parsed.Assets {
		if !visited[asset.Name] {
			appendStep(asset.Name)
		}
	}
	return steps, unknown
}

// failedUpstreamFor walks the asset's transitive upstreams and returns the
// first one that failed earlier in this stale build, or "".
func failedUpstreamFor(asset *pipeline.Asset, parsed *pipeline.Pipeline, failed map[string]bool) string {
	if len(failed) == 0 {
		return ""
	}
	assetByName := make(map[string]*pipeline.Asset, len(parsed.Assets))
	for _, candidate := range parsed.Assets {
		assetByName[candidate.Name] = candidate
	}
	seen := make(map[string]bool)
	queue := []string{asset.Name}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		current := assetByName[name]
		if current == nil {
			continue
		}
		for _, up := range current.Upstreams {
			upName := strings.TrimSpace(up.Value)
			if upName == "" || seen[upName] {
				continue
			}
			seen[upName] = true
			if failed[upName] {
				return upName
			}
			queue = append(queue, upName)
		}
	}
	return ""
}
