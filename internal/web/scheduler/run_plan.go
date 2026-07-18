package scheduler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	PipelineRunPlanVersionV1 = 1
	maxPipelineRunPlanBytes  = 8 << 20
	pipelineDataStateTokenV1 = "renart-data-state-v1"
)

var ErrInvalidStoredRunPlan = errors.New("stored pipeline run plan is invalid")

// PipelineRunPlan is the durable, redacted plan reviewed before admission.
// Typed selection and execution units are kept beside the presentation
// artifact so recovery/execution never has to infer behavior from UI JSON.
type PipelineRunPlan struct {
	Version             int                        `json:"version"`
	PlanID              string                     `json:"plan_id"`
	PipelineID          string                     `json:"pipeline_id"`
	PipelineUUID        string                     `json:"pipeline_uuid"`
	SourceMerkle        string                     `json:"source_merkle"`
	ConfigurationDigest string                     `json:"configuration_digest"`
	ExecutionTime       string                     `json:"execution_time"`
	Blocked             bool                       `json:"blocked,omitempty"`
	Blockers            []string                   `json:"blockers,omitempty"`
	Selection           PipelineRunPlanSelection   `json:"selection"`
	ExecutionUnits      []PipelineRunExecutionUnit `json:"execution_units"`
	Preview             *PipelineRunPlanPreview    `json:"preview,omitempty"`
	Artifact            json.RawMessage            `json:"artifact"`
}

// PipelineRunPlanPreview records a reviewed needed-plan that safely shrank at
// confirmation. The final plan and units remain authoritative; this evidence
// explains what disappeared between preview and admission.
type PipelineRunPlanPreview struct {
	PlanID                string                     `json:"plan_id"`
	DataStateToken        string                     `json:"data_state_token"`
	ExecutionUnits        []PipelineRunExecutionUnit `json:"execution_units"`
	OmittedExecutionUnits []PipelineRunExecutionUnit `json:"omitted_execution_units"`
}

type PipelineRunPlanSelection struct {
	Mode           string `json:"mode"`
	AssetName      string `json:"asset_name,omitempty"`
	Scope          string `json:"scope,omitempty"`
	DataStateToken string `json:"data_state_token,omitempty"`
}

type PipelineRunExecutionUnit struct {
	AssetID     string `json:"asset_id"`
	AssetName   string `json:"asset_name"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	RenderIndex int    `json:"render_index"`
	Reason      string `json:"reason"`
}

type PipelineRunUnitStatus string

const (
	PipelineRunUnitQueued    PipelineRunUnitStatus = "queued"
	PipelineRunUnitRunning   PipelineRunUnitStatus = "running"
	PipelineRunUnitSuccess   PipelineRunUnitStatus = "success"
	PipelineRunUnitFailed    PipelineRunUnitStatus = "failed"
	PipelineRunUnitCancelled PipelineRunUnitStatus = "cancelled"
	PipelineRunUnitSkipped   PipelineRunUnitStatus = "skipped"
)

type PipelineRunUnit struct {
	Position    int                   `json:"position"`
	AssetID     string                `json:"asset_id"`
	AssetName   string                `json:"asset_name"`
	StartDate   string                `json:"start_date"`
	EndDate     string                `json:"end_date"`
	RenderIndex int                   `json:"render_index"`
	Reason      string                `json:"reason"`
	Status      PipelineRunUnitStatus `json:"status"`
	StartedAt   *time.Time            `json:"started_at,omitempty"`
	FinishedAt  *time.Time            `json:"finished_at,omitempty"`
	Error       string                `json:"error,omitempty"`
}

type PipelineRunUnitEvent struct {
	Position   int
	Status     PipelineRunUnitStatus
	StartedAt  *time.Time
	FinishedAt *time.Time
	Error      string
}

type invalidRunPlanError struct {
	RunID string
	Err   error
}

func (e *invalidRunPlanError) Error() string {
	if e == nil {
		return ErrInvalidStoredRunPlan.Error()
	}
	return fmt.Sprintf("load pipeline run plan for %s: %v", e.RunID, e.Err)
}

func (e *invalidRunPlanError) Unwrap() error {
	return ErrInvalidStoredRunPlan
}

func (plan PipelineRunPlan) validate() error {
	if plan.Version != PipelineRunPlanVersionV1 {
		return fmt.Errorf("unsupported pipeline run plan version %d", plan.Version)
	}
	if err := validateRunIdentityDigest("plan_id", strings.TrimSpace(plan.PlanID)); err != nil {
		return err
	}
	if strings.TrimSpace(plan.PipelineID) == "" || strings.TrimSpace(plan.PipelineUUID) == "" {
		return errors.New("pipeline run plan pipeline_id and pipeline_uuid are required")
	}
	if err := validateRunIdentityDigest("source_merkle", strings.TrimSpace(plan.SourceMerkle)); err != nil {
		return err
	}
	if err := validateRunIdentityDigest("configuration_digest", strings.TrimSpace(plan.ConfigurationDigest)); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(plan.ExecutionTime)); err != nil {
		return errors.New("pipeline run plan execution_time must be an RFC3339 timestamp")
	}
	if err := validatePipelineRunPlanSelection(plan.Selection); err != nil {
		return err
	}
	if len(plan.ExecutionUnits) == 0 && !plan.Blocked && (plan.Preview == nil || plan.Selection.Mode != "needed" || len(plan.Preview.ExecutionUnits) == 0) {
		return errors.New("pipeline run plan requires at least one execution unit unless a reviewed needed plan became empty")
	}
	if plan.Blocked != (len(plan.Blockers) > 0) {
		return errors.New("pipeline run plan blocked status requires blocker messages")
	}
	if len(plan.Blockers) > 256 {
		return errors.New("pipeline run plan contains too many blocker messages")
	}
	for _, blocker := range plan.Blockers {
		if strings.TrimSpace(blocker) == "" || len(blocker) > 4096 {
			return errors.New("pipeline run plan blocker messages must be non-empty and at most 4096 bytes")
		}
	}
	seen := make(map[string]struct{}, len(plan.ExecutionUnits))
	for index, unit := range plan.ExecutionUnits {
		if err := validatePipelineRunExecutionUnit(unit); err != nil {
			return fmt.Errorf("pipeline run plan execution unit %d: %w", index, err)
		}
		key := strings.Join([]string{
			strings.TrimSpace(unit.AssetID),
			strings.TrimSpace(unit.StartDate),
			strings.TrimSpace(unit.EndDate),
			fmt.Sprintf("%d", unit.RenderIndex),
		}, "\x00")
		if _, exists := seen[key]; exists {
			return fmt.Errorf("pipeline run plan execution unit %d is duplicated", index)
		}
		seen[key] = struct{}{}
	}
	if len(plan.Artifact) == 0 || len(plan.Artifact) > maxPipelineRunPlanBytes {
		return fmt.Errorf("pipeline run plan artifact must be between 1 and %d bytes", maxPipelineRunPlanBytes)
	}
	if err := validatePipelineRunPlanArtifact(plan); err != nil {
		return err
	}
	if err := validatePipelineRunPlanPreview(plan.Preview); err != nil {
		return err
	}
	if plan.Preview != nil {
		if plan.Selection.Mode != "needed" {
			return errors.New("pipeline run plan preview is only valid for needed selection")
		}
		if plan.Preview.PlanID == plan.PlanID {
			return errors.New("pipeline run plan preview and final plan identities must differ")
		}
		omitted, expanded := pipelineRunUnitDelta(plan.Preview.ExecutionUnits, plan.ExecutionUnits)
		if expanded || !equalPipelineRunExecutionUnitsIgnoringRenderIndex(omitted, plan.Preview.OmittedExecutionUnits) {
			return errors.New("pipeline run plan preview delta does not match final execution units")
		}
	}
	return nil
}

func validatePipelineRunPlanPreview(preview *PipelineRunPlanPreview) error {
	if preview == nil {
		return nil
	}
	if err := validateRunIdentityDigest("preview.plan_id", strings.TrimSpace(preview.PlanID)); err != nil {
		return err
	}
	if err := validatePipelineDataStateToken("preview.data_state_token", strings.TrimSpace(preview.DataStateToken)); err != nil {
		return err
	}
	if len(preview.ExecutionUnits) == 0 {
		return errors.New("pipeline run plan preview requires reviewed execution units")
	}
	for index, unit := range preview.ExecutionUnits {
		if err := validatePipelineRunExecutionUnit(unit); err != nil {
			return fmt.Errorf("pipeline run plan preview execution unit %d: %w", index, err)
		}
	}
	for index, unit := range preview.OmittedExecutionUnits {
		if err := validatePipelineRunExecutionUnit(unit); err != nil {
			return fmt.Errorf("pipeline run plan omitted execution unit %d: %w", index, err)
		}
	}
	return nil
}

func validatePipelineRunPlanSelection(selection PipelineRunPlanSelection) error {
	selection.Mode = strings.TrimSpace(selection.Mode)
	selection.AssetName = strings.TrimSpace(selection.AssetName)
	selection.Scope = strings.TrimSpace(selection.Scope)
	switch selection.Mode {
	case "all", "needed":
		if selection.AssetName != "" || selection.Scope != "" {
			return errors.New("pipeline run plan all/needed selection cannot contain asset_name or scope")
		}
	case "asset":
		if selection.AssetName == "" || selection.Scope == "" {
			return errors.New("pipeline run plan asset selection requires asset_name and scope")
		}
	default:
		return fmt.Errorf("unsupported pipeline run plan selection %q", selection.Mode)
	}
	if token := strings.TrimSpace(selection.DataStateToken); token != "" {
		if err := validatePipelineDataStateToken("data_state_token", token); err != nil {
			return err
		}
	}
	return nil
}

func validatePipelineDataStateToken(field, value string) error {
	version, digest, found := strings.Cut(value, ":")
	if !found || version != pipelineDataStateTokenV1 {
		return fmt.Errorf("pipeline run plan %s must use the %s token format", field, pipelineDataStateTokenV1)
	}
	return validateRunIdentityDigest(field, digest)
}

func validatePipelineRunExecutionUnit(unit PipelineRunExecutionUnit) error {
	if strings.TrimSpace(unit.AssetID) == "" || strings.TrimSpace(unit.AssetName) == "" {
		return errors.New("asset_id and asset_name are required")
	}
	if strings.TrimSpace(unit.Reason) == "" {
		return errors.New("reason is required")
	}
	if unit.RenderIndex < 0 {
		return errors.New("render_index cannot be negative")
	}
	start, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(unit.StartDate))
	if err != nil {
		return errors.New("start_date must be an RFC3339 timestamp")
	}
	end, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(unit.EndDate))
	if err != nil {
		return errors.New("end_date must be an RFC3339 timestamp")
	}
	if !start.Before(end) {
		return errors.New("execution unit requires an increasing window")
	}
	return nil
}

func validatePipelineRunPlanArtifact(plan PipelineRunPlan) error {
	var artifact struct {
		ID           string `json:"id"`
		PipelineID   string `json:"pipeline_id"`
		PipelineUUID string `json:"pipeline_uuid"`
		Source       struct {
			MerkleRoot string `json:"merkle_root"`
		} `json:"source"`
		Context struct {
			ExecutionTime       string `json:"execution_time"`
			ConfigurationDigest string `json:"configuration_digest"`
		} `json:"context"`
		Status    string `json:"status"`
		Readiness struct {
			Blockers []struct {
				Message string `json:"message"`
			} `json:"blockers"`
		} `json:"readiness"`
		Selection      PipelineRunPlanSelection   `json:"selection"`
		ExecutionUnits []PipelineRunExecutionUnit `json:"execution_units"`
		Assets         []struct {
			Renders []struct {
				Stages []struct {
					Content string `json:"content"`
				} `json:"stages"`
			} `json:"renders"`
		} `json:"assets"`
	}
	decoder := json.NewDecoder(bytes.NewReader(plan.Artifact))
	if err := decoder.Decode(&artifact); err != nil {
		return fmt.Errorf("decode pipeline run plan artifact: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("pipeline run plan artifact must contain one JSON object")
	}
	if strings.TrimSpace(artifact.ID) != strings.TrimSpace(plan.PlanID) ||
		strings.TrimSpace(artifact.PipelineID) != strings.TrimSpace(plan.PipelineID) ||
		strings.TrimSpace(artifact.PipelineUUID) != strings.TrimSpace(plan.PipelineUUID) ||
		strings.TrimSpace(artifact.Source.MerkleRoot) != strings.TrimSpace(plan.SourceMerkle) ||
		strings.TrimSpace(artifact.Context.ConfigurationDigest) != strings.TrimSpace(plan.ConfigurationDigest) ||
		strings.TrimSpace(artifact.Context.ExecutionTime) != strings.TrimSpace(plan.ExecutionTime) {
		return errors.New("pipeline run plan artifact identity does not match its durable binding")
	}
	if artifact.Selection != plan.Selection {
		return errors.New("pipeline run plan artifact selection does not match its durable binding")
	}
	if plan.Blocked != (strings.TrimSpace(artifact.Status) == "blocked") {
		return errors.New("pipeline run plan artifact blocked status does not match its durable binding")
	}
	artifactBlockers := make([]string, 0, len(artifact.Readiness.Blockers))
	for _, blocker := range artifact.Readiness.Blockers {
		if message := strings.TrimSpace(blocker.Message); message != "" {
			artifactBlockers = append(artifactBlockers, message)
		}
	}
	if !equalStrings(artifactBlockers, plan.Blockers) {
		return errors.New("pipeline run plan artifact blockers do not match its durable binding")
	}
	if !equalPipelineRunExecutionUnits(artifact.ExecutionUnits, plan.ExecutionUnits) {
		return errors.New("pipeline run plan artifact execution units do not match their durable binding")
	}
	for _, asset := range artifact.Assets {
		for _, rendered := range asset.Renders {
			for _, stage := range rendered.Stages {
				if stage.Content != "" {
					return errors.New("pipeline run plan artifact contains stage content")
				}
			}
		}
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalPipelineRunExecutionUnits(left, right []PipelineRunExecutionUnit) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalPipelineRunExecutionUnitsIgnoringRenderIndex(left, right []PipelineRunExecutionUnit) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		leftUnit, rightUnit := left[index], right[index]
		leftUnit.RenderIndex, rightUnit.RenderIndex = 0, 0
		if leftUnit != rightUnit {
			return false
		}
	}
	return true
}

func pipelineRunUnitDelta(reviewed, current []PipelineRunExecutionUnit) ([]PipelineRunExecutionUnit, bool) {
	available := make(map[string][]int, len(reviewed))
	for index, unit := range reviewed {
		available[pipelineRunUnitSemanticKey(unit)] = append(available[pipelineRunUnitSemanticKey(unit)], index)
	}
	consumed := make([]bool, len(reviewed))
	for _, unit := range current {
		matched := -1
		for _, index := range available[pipelineRunUnitSemanticKey(unit)] {
			if !consumed[index] {
				matched = index
				break
			}
		}
		if matched < 0 {
			return nil, true
		}
		consumed[matched] = true
	}
	omitted := make([]PipelineRunExecutionUnit, 0)
	for index, unit := range reviewed {
		if !consumed[index] {
			omitted = append(omitted, unit)
		}
	}
	return omitted, false
}

func pipelineRunUnitSemanticKey(unit PipelineRunExecutionUnit) string {
	return strings.Join([]string{unit.AssetID, unit.AssetName, unit.StartDate, unit.EndDate, unit.Reason}, "\x00")
}

func validateRunPlanAdmissionBinding(run PipelineRun, spec runSpecV1, plan PipelineRunPlan) error {
	if err := plan.validate(); err != nil {
		return fmt.Errorf("invalid confirmed pipeline run plan: %w", err)
	}
	if spec.Expected == nil {
		return errors.New("confirmed pipeline run plan requires expected source and configuration identities")
	}
	if plan.SourceMerkle != spec.Expected.SourceMerkle {
		return errors.New("confirmed pipeline run plan source identity does not match the run spec")
	}
	if plan.ConfigurationDigest != spec.Expected.ConfigurationDigest {
		return errors.New("confirmed pipeline run plan configuration identity does not match the run spec")
	}
	if spec.Requested.ExecutionTime == nil {
		return errors.New("confirmed pipeline run plan requires an execution time")
	}
	executionTime, err := time.Parse(time.RFC3339Nano, plan.ExecutionTime)
	if err != nil || !executionTime.Equal(*spec.Requested.ExecutionTime) {
		return errors.New("confirmed pipeline run plan execution time does not match the run spec")
	}
	if strings.TrimSpace(plan.PipelineID) != strings.TrimSpace(spec.Pipeline.ID) ||
		strings.TrimSpace(plan.PipelineID) != strings.TrimSpace(run.PipelineID) {
		return errors.New("confirmed pipeline run plan does not match the admitted pipeline")
	}
	if strings.TrimSpace(spec.Pipeline.UUID) != "" && strings.TrimSpace(plan.PipelineUUID) != strings.TrimSpace(spec.Pipeline.UUID) {
		return errors.New("confirmed pipeline run plan stable identity does not match the admitted pipeline")
	}
	return nil
}

func marshalPipelineRunPlan(plan PipelineRunPlan) ([]byte, error) {
	if err := plan.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(plan)
}

func unmarshalPipelineRunPlan(version int, body []byte) (PipelineRunPlan, error) {
	if version != PipelineRunPlanVersionV1 {
		return PipelineRunPlan{}, fmt.Errorf("unsupported pipeline run plan version %d", version)
	}
	var plan PipelineRunPlan
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return PipelineRunPlan{}, fmt.Errorf("decode pipeline run plan v%d: %w", version, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return PipelineRunPlan{}, errors.New("decode pipeline run plan trailing content")
	}
	if plan.Version != version {
		return PipelineRunPlan{}, fmt.Errorf("pipeline run plan version mismatch: row=%d body=%d", version, plan.Version)
	}
	if err := plan.validate(); err != nil {
		return PipelineRunPlan{}, err
	}
	return plan, nil
}
