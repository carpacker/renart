package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"renart/internal/web/bus"
	"renart/internal/web/identity"
	"renart/internal/web/policy"
)

// StaleAssetPlan is one stale asset to rebuild. Windows carries the uncovered
// gap intervals of a partially-covered incremental; empty means one build over
// the selection (or the pipeline's default) window.
type StaleAssetPlan struct {
	AssetName string
	Windows   []ExecutionTimeWindow
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

	pipelineUUID := ""
	if s.deps.CurrentPipelines != nil {
		for _, view := range s.deps.CurrentPipelines() {
			if view.ID == pipelineID {
				pipelineUUID = view.UUID
				break
			}
		}
	}

	ordered, unknown := orderStalePlan(parsed, plan)
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

			output, runErr := s.runSingleAssetMaterialization(ctx, assetPath, environment, window, false, onChunk)
			combined.Write(output)

			if runErr != nil {
				message := runErr.Error()
				if IsDuckDBLockError(runErr, output) {
					message = "duckdb database is busy (lock held by another process), please retry"
				}
				logLine(fmt.Sprintf("\n%s failed: %s\n", asset.Name, message))
				if pipelineUUID != "" {
					now := time.Now().UTC()
					s.emitRunCompleted("", pipelineUUID, environment, window, now, []bus.AssetRun{{
						AssetID:   identity.AssetID(pipelineUUID, asset.Name),
						AssetName: asset.Name,
						Status:    "failed",
					}})
				}
				assetFailed = true
				break
			}

			// Record each window as its own completed run so the coverage log
			// and achieved fingerprints reflect exactly what was executed; bus
			// handlers run synchronously, so downstreams built later in this
			// loop already see this asset's fresh fingerprint.
			now := time.Now().UTC()
			if pipelineUUID != "" {
				s.emitRunCompleted("", pipelineUUID, environment, window, now, []bus.AssetRun{{
					AssetID:   identity.AssetID(pipelineUUID, asset.Name),
					AssetName: asset.Name,
					Status:    "succeeded",
				}})
			}
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
