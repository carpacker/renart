package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/google/uuid"
	"renart/internal/web/identity"
	webmodel "renart/internal/web/model"
	"renart/internal/web/policy"
	webscheduler "renart/internal/web/scheduler"
	"renart/internal/web/staleness"
)

// StaleAssetPlan is one stale asset to rebuild. Windows carries the uncovered
// gap intervals of a partially-covered incremental; empty means one build over
// the selection (or the pipeline's default) window.
type StaleAssetPlan struct {
	AssetName string
	Windows   []ExecutionTimeWindow
	Reason    string
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
		item := StaleAssetPlan{AssetName: status.AssetName, Reason: pipelinePlanStalenessReason(status)}
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
	policyRequest := policy.RunRequest{Environment: environment, Interactive: true}
	if err := s.checkRunPolicy(policyRequest); err != nil {
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

	executionTime := time.Now().UTC()
	defaultWindow, windowErr := ResolveExecutionTimeWindow(string(parsed.Schedule), startDate, endDate, executionTime)
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
	pipelineUUID := strings.TrimSpace(pipelineView.UUID)
	if pipelineUUID == "" {
		pipelineUUID = strings.TrimSpace(parsed.LegacyID)
	}
	if strings.TrimSpace(pipelineView.Name) == "" {
		pipelineView.Name = strings.TrimSpace(parsed.Name)
	}
	units := staleBuildExecutionUnits(s.deps.WorkspaceRoot, pipelineUUID, ordered, defaultWindow)
	unitsByAsset := make(map[string][]staleBuildExecutionUnit, len(ordered))
	for _, unit := range units {
		unitsByAsset[unit.asset.Name] = append(unitsByAsset[unit.asset.Name], unit)
	}

	inlineLedger := s.inlineRunLedger()
	var inlineRunID string
	inlineFinalized := false
	finishInline := func(status webscheduler.RunStatus, runErr error) error {
		if inlineLedger == nil || inlineRunID == "" || inlineFinalized {
			return nil
		}
		inlineFinalized = true
		return inlineLedger.FinishInlineRun(ctx, inlineRunID, status, runErr)
	}
	completionBase := uuid.NewString()
	if inlineLedger != nil && len(units) > 0 {
		selectionUnits := make([]webscheduler.RunSelectionUnit, 0, len(units))
		for _, unit := range units {
			start, end := unit.window.Start, unit.window.End
			selectionUnits = append(selectionUnits, webscheduler.RunSelectionUnit{
				AssetID: unit.assetID, AssetName: unit.asset.Name, AssetPath: unit.assetPath,
				Start: &start, End: &end, Reason: unit.reason,
			})
		}
		pipelineTarget, targetErr := ResolvePipelineRunTarget(pipelineID)
		if targetErr != nil {
			return MaterializeResult{Status: "error", Operation: operation, Error: "admit durable inline run: invalid pipeline id", ExitCode: 1}
		}
		admitted, admitErr := inlineLedger.AdmitInlineRun(ctx, webscheduler.InlineRunAdmission{
			PipelineID: pipelineID, PipelineUUID: pipelineUUID,
			PipelineName: inlinePipelineName(pipelineView, pipelineTarget, s.deps.WorkspaceRoot),
			Environment:  environment, Origin: ExecutionOrigin(ctx), Source: webscheduler.RunSourceWorkingTree,
			Start: defaultWindow.Start, End: defaultWindow.End, ExecutionTime: executionTime,
			SensorMode: sensorModeOnce,
			Selection:  webscheduler.RunSelection{Mode: webscheduler.RunSelectionNeeded, Units: selectionUnits},
		})
		if admitErr != nil {
			return MaterializeResult{Status: "error", Operation: operation, Error: "admit durable inline run: " + admitErr.Error(), ExitCode: 1}
		}
		inlineRunID = admitted.ID
		completionBase = admitted.ID
		if startErr := inlineLedger.StartInlineRun(ctx, admitted.ID, time.Now().UTC()); startErr != nil {
			startErr = errors.Join(startErr, finishInline(webscheduler.RunStatusFailed, startErr))
			return MaterializeResult{Status: "error", Operation: operation, Error: "start durable inline run: " + startErr.Error(), ExitCode: 1}
		}
		defer func() {
			if !inlineFinalized {
				_ = finishInline(webscheduler.RunStatusFailed, errors.New("inline execution ended before durable finalization"))
			}
		}()
	}

	var releaseExecutionLease func() error
	if len(ordered) > 0 {
		releaseExecutionLease, err = s.acquireExecutionLease(ctx)
		if err != nil {
			err = errors.Join(err, finishInline(webscheduler.RunStatusFailed, err))
			return MaterializeResult{
				Status:    "error",
				Operation: operation,
				Error:     "acquire workspace execution lease: " + err.Error(),
				ExitCode:  1,
			}
		}
		defer func() { _ = releaseExecutionLease() }()
		if err := s.checkRunPolicy(policyRequest); err != nil {
			err = errors.Join(err, finishInline(webscheduler.RunStatusFailed, err))
			return MaterializeResult{Status: "error", Operation: operation, Error: err.Error(), ExitCode: 1}
		}
	}
	emit := func(event StaleBuildEvent) {
		if onEvent != nil {
			onEvent(event)
		}
	}
	writeChunk := func(text string) {
		if text == "" {
			return
		}
		if inlineLedger != nil && inlineRunID != "" {
			_ = inlineLedger.AppendInlineRunLog(ctx, inlineRunID, text)
		}
		if onChunk != nil {
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
	var ledgerRunErr error
	wasCancelled := false

	for index, step := range ordered {
		asset := step.asset
		assetUnits := unitsByAsset[asset.Name]
		if upstream := failedUpstreamFor(asset, parsed, failed); upstream != "" {
			skipMessage := fmt.Sprintf("upstream %s failed, a build now would still be stale", upstream)
			logLine(fmt.Sprintf("\nSkipping %s (%d/%d): %s.\n", asset.Name, index+1, total, skipMessage))
			if inlineLedger != nil {
				finished := time.Now().UTC()
				for _, unit := range assetUnits {
					if unitErr := inlineLedger.RecordInlineRunUnit(ctx, inlineRunID, webscheduler.PipelineRunUnitEvent{
						Position: unit.position, Status: webscheduler.PipelineRunUnitSkipped,
						FinishedAt: &finished, Error: skipMessage,
					}); unitErr != nil {
						ledgerRunErr = errors.Join(ledgerRunErr, unitErr)
					}
				}
			}
			emit(StaleBuildEvent{AssetName: asset.Name, Status: "skipped", Step: index + 1, Total: total})
			continue
		}

		emit(StaleBuildEvent{AssetName: asset.Name, Status: "running", Step: index + 1, Total: total})

		encodedAssetID := encodePipelineAssetID(s.deps.WorkspaceRoot, asset)
		assetFailed := false
		assetAttempted := false
		assetStepStarted := false
		assetStartedAt := time.Now().UTC()
		var lastTerminalEvent ExecutionAssetEvent
		var assetFailure error
		for unitIndex, unit := range assetUnits {
			suffix := ""
			if len(step.plan.Windows) > 0 {
				suffix = fmt.Sprintf(" [%s → %s]", unit.window.StartRFC3339(), unit.window.EndRFC3339())
			}
			logLine(fmt.Sprintf("\n━━ Building %s (%d/%d)%s ━━\n", asset.Name, index+1, total, suffix))

			started := time.Now().UTC()
			assetAttempted = true
			if inlineLedger != nil {
				if unitErr := inlineLedger.RecordInlineRunUnit(ctx, inlineRunID, webscheduler.PipelineRunUnitEvent{
					Position: unit.position, Status: webscheduler.PipelineRunUnitRunning, StartedAt: &started,
				}); unitErr != nil {
					assetFailure = unitErr
				}
			}
			completionID := pipelineExecutionUnitCompletionID(completionBase, unit.position)
			lastTerminalEvent = ExecutionAssetEvent{}
			onObservedEvent := func(event ExecutionAssetEvent) error {
				if inlineLedger == nil {
					return nil
				}
				if strings.EqualFold(strings.TrimSpace(event.Status), "running") {
					if assetStepStarted {
						return nil
					}
					if stepErr := inlineLedger.RecordInlineRunStep(ctx, inlineRunID, schedulerRunStepEvent(event)); stepErr != nil {
						return fmt.Errorf("persist inline execution step: %w", stepErr)
					}
					assetStepStarted = true
					if event.StartedAt != nil && !event.StartedAt.IsZero() {
						assetStartedAt = event.StartedAt.UTC()
					}
					return nil
				}
				if terminalPipelineExecutionUnitStatus(event.Status) {
					lastTerminalEvent = event
				}
				return nil
			}
			observed := newPipelineRunObservation(onObservedEvent)
			observed.configureTargetWrites(ctx, completionID, s.deps.TargetWrites)
			targetsResolved := observed.captureExecutionTargets
			if inlineLedger != nil {
				targetsResolved = func(snapshot ExecutionTargetSnapshot) error {
					if targetErr := inlineLedger.SetInlineRunExecutionTargetSnapshot(ctx, inlineRunID, schedulerExecutionTargetSnapshot(snapshot)); targetErr != nil {
						return fmt.Errorf("persist inline execution targets: %w", targetErr)
					}
					return observed.captureExecutionTargets(snapshot)
				}
			}
			var output []byte
			runErr := assetFailure
			if runErr == nil {
				output, runErr = s.runSingleAssetMaterializationObserved(
					ctx, unit.assetPath, environment, unit.window, false, sensorModeOnce,
					func(chunk []byte) { writeChunk(string(chunk)) },
					observed.handle, targetsResolved, observed.beginTargetWrite,
				)
			}
			combined.Write(output)
			physicalErr := runErr
			if inlineLedger != nil {
				finished := time.Now().UTC()
				unitStatus := webscheduler.PipelineRunUnitSuccess
				unitError := ""
				if physicalErr != nil {
					unitStatus = webscheduler.PipelineRunUnitFailed
					unitError = physicalErr.Error()
					if executionWasCancelled(ctx, physicalErr) {
						unitStatus = webscheduler.PipelineRunUnitCancelled
					}
				}
				if unitErr := inlineLedger.RecordInlineRunUnit(ctx, inlineRunID, webscheduler.PipelineRunUnitEvent{
					Position: unit.position, Status: unitStatus, FinishedAt: &finished, Error: unitError,
				}); unitErr != nil {
					runErr = errors.Join(runErr, unitErr)
				}
			}
			if pipelineFound || observed.pipelineUUID() != "" || pipelineUUID != "" {
				completionStatus := "succeeded"
				if physicalErr != nil {
					completionStatus = "failed"
					if executionWasCancelled(ctx, physicalErr) {
						completionStatus = "cancelled"
					}
				}
				runAssets, _ := observed.completedAssetsForNames(pipelineView, completionStatus, []string{asset.Name})
				if len(runAssets) > 0 {
					now := time.Now().UTC()
					completionPipelineUUID := observed.pipelineUUID()
					if completionPipelineUUID == "" {
						completionPipelineUUID = pipelineUUID
					}
					completionErr := s.emitRunCompletedForSpec(ctx, PipelineRunSpec{
						RunID:                   inlineRunID,
						CompletionID:            completionID,
						Environment:             environment,
						executionTargetSnapshot: observed.executionTargetSnapshot(),
					}, completionPipelineUUID, unit.window, now, runAssets)
					if completionErr != nil {
						completionErr = errors.Join(completionErr, observed.markSuccessfulTargetWritesDirty(now))
						runErr = errors.Join(runErr, fmt.Errorf("record durable completion: %w", completionErr))
					}
				}
			}

			if runErr != nil {
				assetFailure = runErr
				ledgerRunErr = errors.Join(ledgerRunErr, runErr)
				if executionWasCancelled(ctx, runErr) {
					wasCancelled = true
				}
				message := runErr.Error()
				if IsDuckDBLockError(runErr, output) {
					message = "duckdb database is busy (lock held by another process), please retry"
				}
				logLine(fmt.Sprintf("\n%s failed: %s\n", asset.Name, message))
				assetFailed = true
				if inlineLedger != nil {
					finished := time.Now().UTC()
					for _, remaining := range assetUnits[unitIndex+1:] {
						if unitErr := inlineLedger.RecordInlineRunUnit(ctx, inlineRunID, webscheduler.PipelineRunUnitEvent{
							Position: remaining.position, Status: webscheduler.PipelineRunUnitSkipped,
							FinishedAt: &finished, Error: "an earlier window for this asset failed",
						}); unitErr != nil {
							ledgerRunErr = errors.Join(ledgerRunErr, unitErr)
						}
					}
				}
				break
			}

			// Record each window as its own completed run so the coverage log
			// and achieved fingerprints reflect exactly what was executed; bus
			// handlers run synchronously, so downstreams built later in this
			// loop already see this asset's fresh fingerprint.
		}
		if inlineLedger != nil && assetAttempted {
			finished := time.Now().UTC()
			stepStatus := webscheduler.RunStatusSuccess
			stepError := ""
			if lastTerminalEvent.Asset == "" {
				lastTerminalEvent = ExecutionAssetEvent{Asset: asset.Name, StartedAt: &assetStartedAt, FinishedAt: &finished}
			}
			if assetFailure != nil {
				stepStatus = webscheduler.RunStatusFailed
				stepError = assetFailure.Error()
				if executionWasCancelled(ctx, assetFailure) {
					stepStatus = webscheduler.RunStatusCancelled
				}
			}
			stepEvent := schedulerRunStepEvent(lastTerminalEvent)
			stepEvent.Asset = asset.Name
			stepEvent.Status = stepStatus
			stepEvent.StartedAt = &assetStartedAt
			stepEvent.FinishedAt = &finished
			stepEvent.Error = stepError
			stepEvent.CompletionOrdinal = nil
			if stepErr := inlineLedger.RecordInlineRunStep(ctx, inlineRunID, stepEvent); stepErr != nil {
				ledgerRunErr = errors.Join(ledgerRunErr, stepErr)
				assetFailure = errors.Join(assetFailure, stepErr)
				assetFailed = true
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
		if wasCancelled {
			result.Status = "cancelled"
		}
	}
	if inlineRunID != "" {
		terminalStatus := webscheduler.RunStatusSuccess
		terminalErr := ledgerRunErr
		if len(failedNames) > 0 {
			terminalErr = errors.Join(terminalErr, errors.New(result.Error))
			terminalStatus = webscheduler.RunStatusFailed
			if wasCancelled {
				terminalStatus = webscheduler.RunStatusCancelled
			}
		}
		if terminalErr != nil && terminalStatus == webscheduler.RunStatusSuccess {
			terminalStatus = webscheduler.RunStatusFailed
			result.Status = "error"
			result.ExitCode = 1
			result.Error = terminalErr.Error()
		}
		if finishErr := finishInline(terminalStatus, terminalErr); finishErr != nil {
			result.Status = "error"
			result.ExitCode = 1
			var resultErr error
			if strings.TrimSpace(result.Error) != "" {
				resultErr = errors.New(result.Error)
			}
			result.Error = errors.Join(resultErr, fmt.Errorf("finalize durable inline run: %w", finishErr)).Error()
		}
	}
	return result
}

type stalePlanStep struct {
	asset *pipeline.Asset
	plan  StaleAssetPlan
}

type staleBuildExecutionUnit struct {
	position  int
	asset     *pipeline.Asset
	assetID   string
	assetPath string
	window    ExecutionTimeWindow
	reason    string
}

func staleBuildExecutionUnits(workspaceRoot, pipelineUUID string, ordered []stalePlanStep, defaultWindow ExecutionTimeWindow) []staleBuildExecutionUnit {
	units := make([]staleBuildExecutionUnit, 0, len(ordered))
	for _, step := range ordered {
		windows := step.plan.Windows
		if len(windows) == 0 {
			windows = []ExecutionTimeWindow{defaultWindow}
		}
		reason := strings.TrimSpace(step.plan.Reason)
		if reason == "" {
			reason = "needed"
			if len(step.plan.Windows) > 0 {
				reason = "uncovered_interval"
			}
		}
		assetID := encodePipelineAssetID(workspaceRoot, step.asset)
		if strings.TrimSpace(pipelineUUID) != "" {
			assetID = identity.AssetID(strings.TrimSpace(pipelineUUID), step.asset.Name)
		}
		for _, window := range windows {
			units = append(units, staleBuildExecutionUnit{
				position: len(units), asset: step.asset, assetID: assetID,
				assetPath: assetRunPathForPipelineAsset(workspaceRoot, step.asset),
				window:    window, reason: reason,
			})
		}
	}
	return units
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
