package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrExactReexecutionUnavailable = errors.New("exact re-execution is unavailable")

// ExactReexecutionUnavailableError explains why an old run cannot truthfully
// be presented as replayable. It is safe for the run-details API; private
// RunSpec values are never included.
type ExactReexecutionUnavailableError struct {
	Reason string
}

func (e *ExactReexecutionUnavailableError) Error() string {
	if e == nil || strings.TrimSpace(e.Reason) == "" {
		return ErrExactReexecutionUnavailable.Error()
	}
	return fmt.Sprintf("%s: %s", ErrExactReexecutionUnavailable, strings.TrimSpace(e.Reason))
}

func (e *ExactReexecutionUnavailableError) Unwrap() error {
	return ErrExactReexecutionUnavailable
}

type exactReexecutionCandidate struct {
	spec       runSpecV1
	plan       PipelineRunPlan
	validation RunReexecutionValidationRequest
}

// GetRunReexecution derives the run-details action from private durable state
// and then verifies that its source and selected configuration still resolve.
// A failed verification becomes the honest current-settings fallback rather
// than making the historical run itself unreadable.
func (s *Service) GetRunReexecution(ctx context.Context, runID string) (PipelineRunReexecution, error) {
	if s == nil || s.store == nil {
		return PipelineRunReexecution{}, errors.New("scheduler is not configured")
	}
	candidate, err := s.exactReexecutionCandidate(ctx, runID)
	if err != nil {
		var unavailable *ExactReexecutionUnavailableError
		if errors.As(err, &unavailable) {
			return currentSettingsReexecution(unavailable.Reason), nil
		}
		return PipelineRunReexecution{}, err
	}
	if s.validateReexecution == nil {
		return currentSettingsReexecution("the original source and selected configuration cannot be verified by this server"), nil
	}
	if err := s.validateReexecution(ctx, candidate.validation); err != nil {
		return currentSettingsReexecution(err.Error()), nil
	}
	return PipelineRunReexecution{
		Mode:           PipelineRunReexecutionExact,
		Selection:      candidate.plan.Selection.Mode,
		ExecutionUnits: len(candidate.plan.ExecutionUnits),
	}, nil
}

// ReexecuteRun admits a new manual River run from the original private
// contract. Schedule identity and watermark authority are intentionally
// removed; every other replayable input and the immutable plan are retained.
func (s *Service) ReexecuteRun(ctx context.Context, runID string) (PipelineRun, error) {
	if s == nil || s.store == nil || s.runner == nil {
		return PipelineRun{}, errors.New("scheduler is not configured")
	}
	if err := s.RequireOwner(); err != nil {
		return PipelineRun{}, err
	}
	s.mu.Lock()
	client := s.riverClient
	s.mu.Unlock()
	if client == nil {
		return PipelineRun{}, errors.New("scheduler is not running")
	}

	candidate, err := s.exactReexecutionCandidate(ctx, runID)
	if err != nil {
		return PipelineRun{}, err
	}
	if s.validateReexecution == nil {
		return PipelineRun{}, &ExactReexecutionUnavailableError{
			Reason: "the original source and selected configuration cannot be verified by this server",
		}
	}
	if err := s.validateReexecution(ctx, candidate.validation); err != nil {
		return PipelineRun{}, &ExactReexecutionUnavailableError{Reason: err.Error()}
	}

	spec := candidate.spec
	spec.Origin = RunTriggerManual
	spec.Dispatch = runDispatchRiver
	spec.Schedule = nil
	if err := spec.validate(); err != nil {
		return PipelineRun{}, &ExactReexecutionUnavailableError{Reason: "the retained execution contract is no longer valid"}
	}

	run := applyRunSpec(PipelineRun{Status: RunStatusQueued}, spec)
	run.ExecutionContextResolved = false
	return s.admitQueuedRun(ctx, client, run, spec, &candidate.plan)
}

func (s *Service) exactReexecutionCandidate(ctx context.Context, runID string) (exactReexecutionCandidate, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return exactReexecutionCandidate{}, errors.New("run id is required")
	}
	run, _, _, err := s.store.Get(ctx, runID)
	if err != nil {
		return exactReexecutionCandidate{}, err
	}
	if !terminalRunStatus(run.Status) {
		return exactReexecutionCandidate{}, &ExactReexecutionUnavailableError{Reason: "the original run has not finished"}
	}
	spec, found, err := s.store.GetRunSpec(ctx, run.ID)
	if err != nil {
		return exactReexecutionCandidate{}, err
	}
	if !found {
		return exactReexecutionCandidate{}, &ExactReexecutionUnavailableError{Reason: "the original private execution contract was not retained"}
	}
	if err := validateRunSpecImmutableBinding(run, spec); err != nil {
		return exactReexecutionCandidate{}, &ExactReexecutionUnavailableError{Reason: "the retained execution contract no longer matches the original run"}
	}
	plan, found, err := s.store.GetRunPlan(ctx, run.ID)
	if err != nil {
		return exactReexecutionCandidate{}, err
	}
	if !found {
		return exactReexecutionCandidate{}, &ExactReexecutionUnavailableError{Reason: "the original execution plan was not retained"}
	}
	if plan.Blocked {
		return exactReexecutionCandidate{}, &ExactReexecutionUnavailableError{Reason: "the original plan was blocked and never represented executable work"}
	}
	if spec.Expected == nil || spec.Requested.ExecutionTime == nil {
		return exactReexecutionCandidate{}, &ExactReexecutionUnavailableError{Reason: "the original source or selected-configuration identity was not retained"}
	}
	if spec.Requested.Start == nil || spec.Requested.End == nil {
		return exactReexecutionCandidate{}, &ExactReexecutionUnavailableError{Reason: "the original execution window was not retained"}
	}
	if err := validateRunPlanAdmissionBinding(run, spec, plan); err != nil {
		return exactReexecutionCandidate{}, &ExactReexecutionUnavailableError{Reason: "the retained plan no longer matches its execution contract"}
	}
	resolvedSpec, err := s.resolveRunSpecForExecution(ctx, spec, plan, true)
	if err != nil {
		return exactReexecutionCandidate{}, &ExactReexecutionUnavailableError{
			Reason: "the original variable references no longer resolve to its reviewed plan",
		}
	}

	return exactReexecutionCandidate{
		spec: spec,
		plan: plan,
		validation: RunReexecutionValidationRequest{
			OriginalRunID:               run.ID,
			PipelineID:                  spec.Pipeline.ID,
			PipelineUUID:                spec.Pipeline.UUID,
			Environment:                 spec.Requested.Environment,
			Source:                      spec.Source.Kind,
			SnapshotVersionID:           spec.Source.SnapshotVersionID,
			ExpectedSourceMerkle:        spec.Expected.SourceMerkle,
			ExpectedConfigurationDigest: spec.Expected.ConfigurationDigest,
			VariableOverrides:           resolvedSpec.Requested.Variables,
			ConfigurationAssetNames:     reexecutionConfigurationAssetNames(plan),
			FullRefresh:                 spec.Requested.FullRefresh,
			Backfill:                    spec.Requested.Backfill,
			ConfirmedEnvironment:        spec.Authorization.ConfirmedEnvironment,
		},
	}, nil
}

func reexecutionConfigurationAssetNames(plan PipelineRunPlan) []string {
	// A safely-shrunk Needed plan binds its final selected-configuration digest
	// to the executable units. Preview units are historical review evidence and
	// can include assets deliberately omitted before admission.
	seen := make(map[string]struct{})
	names := make([]string, 0, len(plan.ExecutionUnits))
	for _, unit := range plan.ExecutionUnits {
		name := strings.TrimSpace(unit.AssetName)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func currentSettingsReexecution(reason string) PipelineRunReexecution {
	return PipelineRunReexecution{
		Mode:   PipelineRunReexecutionCurrentSettings,
		Reason: strings.TrimSpace(reason),
	}
}

func terminalRunStatus(status RunStatus) bool {
	switch status {
	case RunStatusSuccess, RunStatusFailed, RunStatusCancelled:
		return true
	default:
		return false
	}
}
