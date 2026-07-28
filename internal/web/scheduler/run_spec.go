package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

const (
	runSpecVersionV1             = 1
	runSpecVersionV2             = 2
	runSpecVersionV3             = 3
	runDispatchRiver             = "river"
	runDispatchInlineStreaming   = "inline_streaming"
	runSelectionAll              = "all"
	runSelectionAsset            = "asset"
	runSelectionNeeded           = "needed"
	runSpecRetrySnoozeTime       = 30 * time.Second
	maxRunVariableOverridesBytes = 256 << 10
	maxRunSelectionUnits         = 100_000
)

var (
	ErrPipelineRunActive = errors.New("pipeline run is already active")
	ErrInvalidStoredSpec = errors.New("stored run spec is invalid")
)

type invalidRunSpecError struct {
	RunID string
	Err   error
}

func (e *invalidRunSpecError) Error() string {
	if e == nil {
		return ErrInvalidStoredSpec.Error()
	}
	return fmt.Sprintf("load run spec for %s: %v", e.RunID, e.Err)
}

func (e *invalidRunSpecError) Unwrap() error {
	return ErrInvalidStoredSpec
}

type PipelineRunActiveError struct {
	PipelineID  string
	ActiveRunID string
}

func (e *PipelineRunActiveError) Error() string {
	if e == nil {
		return ErrPipelineRunActive.Error()
	}
	return fmt.Sprintf("pipeline %s execution conflicts with active run %s", e.PipelineID, e.ActiveRunID)
}

func (e *PipelineRunActiveError) Unwrap() error {
	return ErrPipelineRunActive
}

// runSpecV1 is the private, replayable behavior contract for queued and inline
// runs. Version 1 represents an entire pipeline; version 2 adds exact
// asset/window selection provenance; version 3 records the exact units chosen
// for an all-pipeline execution. It is stored separately from the public
// run row so authorization and future secret references cannot leak through
// run-list JSON or SSE events.
// Requested context stays immutable; effective post-policy context continues
// to live in the pipeline_runs columns written immediately before execution.
type runSpecV1 struct {
	Version          int                  `json:"version"`
	Pipeline         runPipelineIdentity  `json:"pipeline"`
	Origin           RunTrigger           `json:"origin"`
	Dispatch         string               `json:"dispatch"`
	Source           runSourceSpec        `json:"source"`
	Requested        runRequestedContext  `json:"requested"`
	Expected         *runExpectedIdentity `json:"expected,omitempty"`
	Authorization    runAuthorization     `json:"authorization"`
	Selection        string               `json:"selection"`
	SelectionDetails *runSelectionDetails `json:"selection_details,omitempty"`
	Schedule         *runScheduleIdentity `json:"schedule,omitempty"`
}

type runSelectionDetails struct {
	Scope         string             `json:"scope,omitempty"`
	AnchorAssetID string             `json:"anchor_asset_id,omitempty"`
	Units         []runSelectionUnit `json:"units"`
}

type runSelectionUnit struct {
	AssetID   string     `json:"asset_id"`
	AssetName string     `json:"asset_name"`
	AssetPath string     `json:"asset_path"`
	Start     *time.Time `json:"start,omitempty"`
	End       *time.Time `json:"end,omitempty"`
	Reason    string     `json:"reason"`
}

type runPipelineIdentity struct {
	ID   string `json:"id"`
	UUID string `json:"uuid,omitempty"`
	Name string `json:"name"`
}

type runSourceSpec struct {
	Kind              RunSource `json:"kind"`
	SnapshotVersionID string    `json:"snapshot_version_id,omitempty"`
}

type runRequestedContext struct {
	Environment        string            `json:"environment"`
	Start              *time.Time        `json:"start,omitempty"`
	End                *time.Time        `json:"end,omitempty"`
	ExecutionTime      *time.Time        `json:"execution_time,omitempty"`
	Variables          map[string]any    `json:"variables,omitempty"`
	VariableReferences map[string]string `json:"variable_references,omitempty"`
	FullRefresh        bool              `json:"full_refresh,omitempty"`
	Backfill           bool              `json:"backfill,omitempty"`
	SensorMode         string            `json:"sensor_mode,omitempty"`
}

type runExpectedIdentity struct {
	SourceMerkle        string `json:"source_merkle"`
	ConfigurationDigest string `json:"configuration_digest"`
}

type runAuthorization struct {
	ConfirmedEnvironment string `json:"confirmed_environment,omitempty"`
}

type runScheduleIdentity struct {
	PipelineUUID      string `json:"pipeline_uuid"`
	Environment       string `json:"environment"`
	Cron              string `json:"cron,omitempty"`
	Timezone          string `json:"timezone,omitempty"`
	OccurrenceKey     string `json:"occurrence_key,omitempty"`
	AdvancesWatermark bool   `json:"advances_watermark"`
}

func (spec runSpecV1) validate() error {
	if spec.Version != runSpecVersionV1 &&
		spec.Version != runSpecVersionV2 &&
		spec.Version != runSpecVersionV3 {
		return fmt.Errorf("unsupported run spec version %d", spec.Version)
	}
	if strings.TrimSpace(spec.Pipeline.ID) == "" || strings.TrimSpace(spec.Pipeline.Name) == "" {
		return errors.New("run spec pipeline id and name are required")
	}
	switch spec.Dispatch {
	case runDispatchRiver, runDispatchInlineStreaming:
	default:
		return fmt.Errorf("unsupported run dispatch %q", spec.Dispatch)
	}
	if err := validateRunSelection(spec); err != nil {
		return err
	}
	if err := validateRunSpecSource(spec.Source); err != nil {
		return err
	}
	if (spec.Requested.Start == nil) != (spec.Requested.End == nil) {
		return errors.New("run spec start and end must both be set or both be omitted")
	}
	if err := validateRunVariableOverrides(spec.Requested.Variables); err != nil {
		return err
	}
	if err := validateRunVariableReferences(spec.Requested.Variables, spec.Requested.VariableReferences); err != nil {
		return err
	}
	if spec.Requested.Start != nil && !spec.Requested.Start.Before(*spec.Requested.End) {
		return errors.New("run spec requires an increasing execution window")
	}
	if spec.Expected != nil {
		if err := validateRunIdentityDigest("source_merkle", spec.Expected.SourceMerkle); err != nil {
			return err
		}
		if err := validateRunIdentityDigest("configuration_digest", spec.Expected.ConfigurationDigest); err != nil {
			return err
		}
		if spec.Requested.ExecutionTime == nil {
			return errors.New("run spec with expected plan identities requires execution_time")
		}
	}
	if spec.Requested.FullRefresh && spec.Requested.Backfill {
		return errors.New("run spec full refresh and backfill are mutually exclusive")
	}
	if spec.Requested.Backfill && (spec.Requested.Start == nil || spec.Requested.End == nil) {
		return errors.New("run spec backfill requires an explicit interval")
	}
	switch strings.TrimSpace(spec.Requested.SensorMode) {
	case "", "once", "wait", "skip":
	default:
		return fmt.Errorf("invalid run spec sensor mode %q", spec.Requested.SensorMode)
	}
	switch spec.Origin {
	case RunTriggerSchedule:
		if spec.Dispatch != runDispatchRiver {
			return errors.New("scheduled run spec requires River dispatch")
		}
		if spec.Schedule == nil {
			return errors.New("scheduled run spec requires schedule provenance")
		}
		if strings.TrimSpace(spec.Schedule.PipelineUUID) == "" || strings.TrimSpace(spec.Pipeline.UUID) == "" {
			return errors.New("scheduled run spec requires a stable pipeline UUID")
		}
		if strings.TrimSpace(spec.Schedule.PipelineUUID) != strings.TrimSpace(spec.Pipeline.UUID) {
			return errors.New("scheduled run spec pipeline UUID does not match schedule provenance")
		}
		if strings.TrimSpace(spec.Schedule.Environment) == "" {
			return errors.New("scheduled run spec requires an environment")
		}
		if strings.TrimSpace(spec.Schedule.Environment) != strings.TrimSpace(spec.Requested.Environment) {
			return errors.New("scheduled run spec environment does not match requested context")
		}
		if spec.Requested.Start == nil || spec.Requested.End == nil {
			return errors.New("scheduled run spec requires an exact interval")
		}
		if occurrenceKey := strings.TrimSpace(spec.Schedule.OccurrenceKey); occurrenceKey != "" {
			if err := validateRunIdentityDigest("schedule occurrence_key", occurrenceKey); err != nil {
				return err
			}
			occurrence, err := newScheduleOccurrence(
				spec.Schedule.PipelineUUID,
				spec.Schedule.Environment,
				*spec.Requested.Start,
				*spec.Requested.End,
			)
			if err != nil {
				return err
			}
			if occurrence.Key != occurrenceKey {
				return errors.New("scheduled run spec occurrence key does not match its normalized interval")
			}
		}
		if spec.Source.Kind != RunSourceSnapshot {
			return errors.New("scheduled run spec requires an immutable snapshot source")
		}
		if !spec.Schedule.AdvancesWatermark {
			return errors.New("scheduled run spec must retain its server-owned watermark capability")
		}
	case RunTriggerManual, RunTriggerAPI, RunTriggerCLI:
		if spec.Schedule != nil {
			return errors.New("non-scheduled run spec cannot advance a schedule watermark")
		}
	default:
		return fmt.Errorf("invalid run spec origin %q", spec.Origin)
	}
	return nil
}

func validateRunSelection(spec runSpecV1) error {
	selection := strings.TrimSpace(spec.Selection)
	if selection != spec.Selection {
		return errors.New("run spec selection must be canonical")
	}
	if spec.Version == runSpecVersionV1 {
		if selection != runSelectionAll || spec.SelectionDetails != nil {
			return errors.New("run spec v1 only supports an all selection")
		}
		return nil
	}

	switch selection {
	case runSelectionAll:
		if spec.Version < runSpecVersionV3 {
			if spec.SelectionDetails != nil {
				return errors.New("run spec all selection cannot contain selection details before v3")
			}
			return nil
		}
		if spec.SelectionDetails == nil {
			return errors.New("run spec v3 all selection requires execution units")
		}
	case runSelectionAsset, runSelectionNeeded:
	default:
		return fmt.Errorf("unsupported run selection %q", spec.Selection)
	}
	if spec.SelectionDetails == nil {
		return fmt.Errorf("run spec %s selection requires details", selection)
	}
	details := spec.SelectionDetails
	if len(details.Units) == 0 || len(details.Units) > maxRunSelectionUnits {
		return fmt.Errorf("run spec %s selection requires between 1 and %d execution units", selection, maxRunSelectionUnits)
	}

	scope := strings.TrimSpace(details.Scope)
	anchor := strings.TrimSpace(details.AnchorAssetID)
	if scope != details.Scope || anchor != details.AnchorAssetID {
		return errors.New("run spec selection identity must be canonical")
	}
	if selection == runSelectionAsset {
		switch scope {
		case "asset", "asset_with_upstreams", "asset_with_downstreams", "asset_with_upstreams_and_downstreams":
		default:
			return fmt.Errorf("invalid run spec asset selection scope %q", details.Scope)
		}
		if anchor == "" {
			return errors.New("run spec asset selection requires an anchor asset id")
		}
	} else if scope != "" || anchor != "" {
		return errors.New("run spec non-asset selection cannot contain asset scope provenance")
	}

	seen := make(map[string]struct{}, len(details.Units))
	anchorFound := false
	for index, unit := range details.Units {
		if err := validateRunSelectionUnit(spec, unit); err != nil {
			return fmt.Errorf("run spec selection unit %d: %w", index, err)
		}
		if unit.AssetID == anchor {
			anchorFound = true
		}
		key := strings.Join([]string{
			unit.AssetID,
			unit.AssetPath,
			unit.Start.UTC().Format(time.RFC3339Nano),
			unit.End.UTC().Format(time.RFC3339Nano),
		}, "\x00")
		if _, exists := seen[key]; exists {
			return fmt.Errorf("run spec selection unit %d is duplicated", index)
		}
		seen[key] = struct{}{}
	}
	if selection == runSelectionAsset && !anchorFound {
		return errors.New("run spec asset selection anchor is absent from its units")
	}
	return nil
}

func validateRunSelectionUnit(spec runSpecV1, unit runSelectionUnit) error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "asset_id", value: unit.AssetID},
		{name: "asset_name", value: unit.AssetName},
		{name: "asset_path", value: unit.AssetPath},
		{name: "reason", value: unit.Reason},
	} {
		if strings.TrimSpace(field.value) == "" || strings.TrimSpace(field.value) != field.value {
			return fmt.Errorf("%s must be non-empty and canonical", field.name)
		}
		if len(field.value) > 4096 {
			return fmt.Errorf("%s exceeds the 4096 byte limit", field.name)
		}
	}
	firstPathSegment := strings.SplitN(unit.AssetPath, "/", 2)[0]
	if strings.ContainsRune(unit.AssetPath, 0) || strings.Contains(unit.AssetPath, "\\") ||
		(len(firstPathSegment) >= 2 && firstPathSegment[1] == ':') || strings.HasPrefix(unit.AssetPath, "/") ||
		path.Clean(unit.AssetPath) != unit.AssetPath || unit.AssetPath == "." ||
		unit.AssetPath == ".." || strings.HasPrefix(unit.AssetPath, "../") {
		return errors.New("asset_path must be a canonical workspace-relative slash path")
	}
	if (unit.Start == nil) != (unit.End == nil) || unit.Start == nil {
		return errors.New("start and end must both contain an exact execution window")
	}
	if !unit.Start.Before(*unit.End) {
		return errors.New("execution window must be increasing")
	}
	if spec.Requested.Start == nil || spec.Requested.End == nil ||
		unit.Start.Before(*spec.Requested.Start) || unit.End.After(*spec.Requested.End) {
		return errors.New("execution window must be contained by the requested run window")
	}
	return nil
}

func applyInlineRunSelection(spec *runSpecV1, selection RunSelection) error {
	mode := selection.Mode
	if mode == "" {
		mode = RunSelectionAll
	}
	if mode == RunSelectionAll {
		if strings.TrimSpace(selection.Scope) != "" || strings.TrimSpace(selection.AnchorAssetID) != "" {
			return errors.New("inline all selection cannot contain asset scope details")
		}
		if len(selection.Units) == 0 {
			return nil
		}
	}

	details := &runSelectionDetails{
		Scope:         selection.Scope,
		AnchorAssetID: selection.AnchorAssetID,
		Units:         make([]runSelectionUnit, 0, len(selection.Units)),
	}
	for _, unit := range selection.Units {
		details.Units = append(details.Units, runSelectionUnit{
			AssetID:   unit.AssetID,
			AssetName: unit.AssetName,
			AssetPath: unit.AssetPath,
			Start:     cloneRunTime(unit.Start),
			End:       cloneRunTime(unit.End),
			Reason:    unit.Reason,
		})
	}
	spec.Version = runSpecVersionV2
	if mode == RunSelectionAll {
		spec.Version = runSpecVersionV3
	}
	spec.Selection = string(mode)
	spec.SelectionDetails = details
	return spec.validate()
}

func validateRunIdentityDigest(field, value string) error {
	if len(value) != 64 || strings.ToLower(value) != value {
		return fmt.Errorf("run spec %s must be a lowercase SHA-256 digest", field)
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("run spec %s must be a lowercase SHA-256 digest", field)
		}
	}
	return nil
}

func validateRunSpecSource(source runSourceSpec) error {
	versionID := strings.TrimSpace(source.SnapshotVersionID)
	switch source.Kind {
	case RunSourceWorkingTree:
		if versionID != "" {
			return errors.New("working-tree run spec cannot contain a snapshot version")
		}
	case RunSourceSnapshot:
		if versionID == "" {
			return errors.New("snapshot run spec requires an exact version")
		}
	default:
		return fmt.Errorf("invalid run spec source %q", source.Kind)
	}
	return nil
}

func marshalRunSpec(spec runSpecV1) ([]byte, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(spec)
}

func unmarshalRunSpec(version int, body []byte) (runSpecV1, error) {
	if version != runSpecVersionV1 &&
		version != runSpecVersionV2 &&
		version != runSpecVersionV3 {
		return runSpecV1{}, fmt.Errorf("unsupported run spec version %d", version)
	}
	var spec runSpecV1
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return runSpecV1{}, fmt.Errorf("decode run spec v%d: %w", version, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return runSpecV1{}, fmt.Errorf("decode run spec v%d trailing content: %w", version, err)
	}
	if spec.Version != version {
		return runSpecV1{}, fmt.Errorf("run spec version mismatch: row=%d body=%d", version, spec.Version)
	}
	if err := spec.validate(); err != nil {
		return runSpecV1{}, err
	}
	return spec, nil
}

func manualRunSpec(run PipelineRun, source RunSource, confirmedEnvironment string) runSpecV1 {
	spec := runSpecV1{
		Version: runSpecVersionV1,
		Pipeline: runPipelineIdentity{
			ID:   strings.TrimSpace(run.PipelineID),
			UUID: strings.TrimSpace(run.PipelineUUID),
			Name: strings.TrimSpace(run.Pipeline),
		},
		Origin:   run.Trigger,
		Dispatch: runDispatchRiver,
		Source: runSourceSpec{
			Kind:              source,
			SnapshotVersionID: strings.TrimSpace(run.SnapshotVersionID),
		},
		Requested: runRequestedContext{
			Environment:   strings.TrimSpace(run.Environment),
			Start:         cloneRunTime(run.WinStart),
			End:           cloneRunTime(run.WinEnd),
			ExecutionTime: cloneRunTime(run.ExecutionTime),
			Variables:     run.VariableOverrides,
			FullRefresh:   run.FullRefresh,
			Backfill:      run.Backfill,
			SensorMode:    strings.TrimSpace(run.SensorMode),
		},
		Authorization: runAuthorization{ConfirmedEnvironment: strings.TrimSpace(confirmedEnvironment)},
		Selection:     runSelectionAll,
	}
	if strings.TrimSpace(run.ExpectedSourceMerkle) != "" || strings.TrimSpace(run.ExpectedConfigurationDigest) != "" {
		spec.Expected = &runExpectedIdentity{
			SourceMerkle:        strings.TrimSpace(run.ExpectedSourceMerkle),
			ConfigurationDigest: strings.TrimSpace(run.ExpectedConfigurationDigest),
		}
	}
	return spec
}

func inlineRunSpec(run PipelineRun, source RunSource, confirmedEnvironment string) runSpecV1 {
	spec := manualRunSpec(run, source, confirmedEnvironment)
	spec.Dispatch = runDispatchInlineStreaming
	return spec
}

// validateRunSpecImmutableBinding compares the parts of a persisted RunSpec
// that must remain identical for the lifetime of a run. The requested
// execution context is deliberately excluded: once execution starts, the run
// row contains the effective environment, window, and modes resolved by the
// execution service rather than these immutable request values.
func validateRunSpecImmutableBinding(run PipelineRun, spec runSpecV1) error {
	if strings.TrimSpace(spec.Pipeline.ID) != strings.TrimSpace(run.PipelineID) ||
		strings.TrimSpace(spec.Pipeline.Name) != strings.TrimSpace(run.Pipeline) {
		return errors.New("run spec pipeline identity does not match queued run")
	}
	if spec.Origin != run.Trigger {
		return errors.New("run spec origin does not match queued run")
	}
	if strings.TrimSpace(spec.Source.SnapshotVersionID) != strings.TrimSpace(run.SnapshotVersionID) {
		return errors.New("run spec source does not match queued run")
	}
	expectedSource := RunSourceWorkingTree
	if strings.TrimSpace(run.SnapshotVersionID) != "" {
		expectedSource = RunSourceSnapshot
	}
	if spec.Source.Kind != expectedSource {
		return errors.New("run spec source kind does not match queued run")
	}
	if runUUID := strings.TrimSpace(run.PipelineUUID); runUUID != "" &&
		runUUID != strings.TrimSpace(spec.Pipeline.UUID) {
		return errors.New("run spec stable pipeline UUID does not match queued run")
	}
	return nil
}

func validateRunSpecBinding(run PipelineRun, spec runSpecV1) error {
	if err := validateRunSpecImmutableBinding(run, spec); err != nil {
		return err
	}
	if strings.TrimSpace(spec.Requested.Environment) != strings.TrimSpace(run.Environment) ||
		!equalRunTime(spec.Requested.Start, run.WinStart) ||
		!equalRunTime(spec.Requested.End, run.WinEnd) {
		return errors.New("run spec requested context does not match queued run")
	}
	if spec.Requested.FullRefresh != run.FullRefresh || spec.Requested.Backfill != run.Backfill ||
		strings.TrimSpace(spec.Requested.SensorMode) != strings.TrimSpace(run.SensorMode) {
		return errors.New("run spec requested modes do not match queued run")
	}
	return nil
}

// validateRunSpecAdmissionBinding compares the stable identity while it is
// still present on the in-memory admission request. PipelineUUID is private
// RunSpec provenance and is not persisted in the public run row, so queued-run
// reloads validate it independently against the durable UUID slot instead.
func validateRunSpecAdmissionBinding(run PipelineRun, spec runSpecV1) error {
	if err := validateRunSpecBinding(run, spec); err != nil {
		return err
	}
	if strings.TrimSpace(spec.Pipeline.UUID) != strings.TrimSpace(run.PipelineUUID) {
		return errors.New("run spec stable pipeline UUID does not match queued run")
	}
	return nil
}

func equalRunTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func scheduledRunSpec(run PipelineRun, args pipelineRunJobArgs) runSpecV1 {
	pipelineUUID := strings.TrimSpace(args.PipelineUUID)
	return runSpecV1{
		Version: runSpecVersionV1,
		Pipeline: runPipelineIdentity{
			ID:   strings.TrimSpace(run.PipelineID),
			UUID: pipelineUUID,
			Name: strings.TrimSpace(run.Pipeline),
		},
		Origin:   RunTriggerSchedule,
		Dispatch: runDispatchRiver,
		Source: runSourceSpec{
			Kind:              RunSourceSnapshot,
			SnapshotVersionID: strings.TrimSpace(run.SnapshotVersionID),
		},
		Requested: runRequestedContext{
			Environment:        strings.TrimSpace(run.Environment),
			Start:              cloneRunTime(run.WinStart),
			End:                cloneRunTime(run.WinEnd),
			ExecutionTime:      cloneRunTime(run.ExecutionTime),
			Variables:          cloneScheduleVariables(args.Variables),
			VariableReferences: cloneScheduleSecretRefs(args.VariableReferences),
			FullRefresh:        run.FullRefresh,
			Backfill:           run.Backfill,
			SensorMode:         strings.TrimSpace(run.SensorMode),
		},
		Authorization: runAuthorization{ConfirmedEnvironment: strings.TrimSpace(args.ConfirmedEnvironment)},
		Selection:     runSelectionAll,
		Schedule: &runScheduleIdentity{
			PipelineUUID:      pipelineUUID,
			Environment:       strings.TrimSpace(run.Environment),
			Cron:              strings.TrimSpace(args.Schedule),
			Timezone:          strings.TrimSpace(args.Timezone),
			OccurrenceKey:     strings.TrimSpace(args.OccurrenceKey),
			AdvancesWatermark: true,
		},
	}
}

func legacyRunSpec(run PipelineRun, args pipelineRunJobArgs) (runSpecV1, error) {
	if run.Trigger == RunTriggerSchedule || strings.TrimSpace(args.PipelineUUID) != "" {
		spec := scheduledRunSpec(run, args)
		if err := spec.validate(); err != nil {
			return runSpecV1{}, fmt.Errorf("decode legacy scheduled run %s: %w", run.ID, err)
		}
		return spec, nil
	}
	source := RunSourceWorkingTree
	if strings.TrimSpace(run.SnapshotVersionID) != "" {
		source = RunSourceSnapshot
	}
	// Before RunSpec, the River payload was authoritative for these modes.
	// Preserve it only while upgrading a job that has no stored spec.
	run.FullRefresh = args.FullRefresh
	run.Backfill = args.Backfill
	if sensorMode := strings.TrimSpace(args.SensorMode); sensorMode != "" {
		run.SensorMode = sensorMode
	}
	spec := manualRunSpec(run, source, args.ConfirmedEnvironment)
	if err := spec.validate(); err != nil {
		return runSpecV1{}, fmt.Errorf("decode legacy run %s: %w", run.ID, err)
	}
	return spec, nil
}

func cloneRunTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func validateRunVariableOverrides(overrides map[string]any) error {
	if len(overrides) == 0 {
		return nil
	}
	for name := range overrides {
		if strings.TrimSpace(name) == "" {
			return errors.New("run spec variable override names cannot be empty")
		}
	}
	body, err := json.Marshal(overrides)
	if err != nil {
		return errors.New("run spec variable overrides must contain JSON-compatible values")
	}
	if len(body) > maxRunVariableOverridesBytes {
		return fmt.Errorf("run spec variable overrides exceed the %d byte limit", maxRunVariableOverridesBytes)
	}
	return nil
}

func validateRunVariableReferences(values map[string]any, references map[string]string) error {
	for name, reference := range references {
		if err := validateScheduleVariableName(name); err != nil {
			return fmt.Errorf("run spec variable reference: %w", err)
		}
		if _, duplicate := values[name]; duplicate {
			return fmt.Errorf("run spec variable %q cannot have both a value and a secret reference", name)
		}
		if err := validateScheduleSecretReference(reference); err != nil {
			return fmt.Errorf("run spec variable %q: %w", name, err)
		}
	}
	body, err := json.Marshal(struct {
		Values     map[string]any    `json:"values,omitempty"`
		References map[string]string `json:"references,omitempty"`
	}{Values: values, References: references})
	if err != nil {
		return errors.New("run spec variable context must be JSON-compatible")
	}
	if len(body) > maxRunVariableOverridesBytes {
		return fmt.Errorf("run spec variable context exceeds the %d byte limit", maxRunVariableOverridesBytes)
	}
	return nil
}

func applyRunSpec(run PipelineRun, spec runSpecV1) PipelineRun {
	run.PipelineID = strings.TrimSpace(spec.Pipeline.ID)
	run.PipelineUUID = strings.TrimSpace(spec.Pipeline.UUID)
	run.Pipeline = strings.TrimSpace(spec.Pipeline.Name)
	run.Trigger = spec.Origin
	run.Environment = strings.TrimSpace(spec.Requested.Environment)
	run.WinStart = cloneRunTime(spec.Requested.Start)
	run.WinEnd = cloneRunTime(spec.Requested.End)
	run.SnapshotVersionID = strings.TrimSpace(spec.Source.SnapshotVersionID)
	run.FullRefresh = spec.Requested.FullRefresh
	run.Backfill = spec.Requested.Backfill
	run.SensorMode = strings.TrimSpace(spec.Requested.SensorMode)
	run.ExecutionTime = cloneRunTime(spec.Requested.ExecutionTime)
	run.VariableOverrides = spec.Requested.Variables
	if spec.Expected != nil {
		run.ExpectedSourceMerkle = strings.TrimSpace(spec.Expected.SourceMerkle)
		run.ExpectedConfigurationDigest = strings.TrimSpace(spec.Expected.ConfigurationDigest)
	}
	return run
}

// applyRecoveredRunSpecIdentity restores the private stable identity that is
// intentionally absent from the public run row. Effective execution context
// already persisted on the row must remain untouched during recovery.
func applyRecoveredRunSpecIdentity(run PipelineRun, spec runSpecV1) PipelineRun {
	if strings.TrimSpace(run.PipelineUUID) == "" {
		run.PipelineUUID = strings.TrimSpace(spec.Pipeline.UUID)
	}
	return run
}
