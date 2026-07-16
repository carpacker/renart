package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	runSpecVersionV1       = 1
	runDispatchRiver       = "river"
	runSelectionAll        = "all"
	runSpecRetrySnoozeTime = 30 * time.Second
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
	return fmt.Sprintf("pipeline %s already has active run %s", e.PipelineID, e.ActiveRunID)
}

func (e *PipelineRunActiveError) Unwrap() error {
	return ErrPipelineRunActive
}

// runSpecV1 is the private, replayable behavior contract for scheduler-backed
// runs. It is stored separately from the public run row so authorization and
// future secret references cannot leak through run-list JSON or SSE events.
// Requested context stays immutable; effective post-policy context continues
// to live in the pipeline_runs columns written immediately before execution.
type runSpecV1 struct {
	Version       int                  `json:"version"`
	Pipeline      runPipelineIdentity  `json:"pipeline"`
	Origin        RunTrigger           `json:"origin"`
	Dispatch      string               `json:"dispatch"`
	Source        runSourceSpec        `json:"source"`
	Requested     runRequestedContext  `json:"requested"`
	Authorization runAuthorization     `json:"authorization"`
	Selection     string               `json:"selection"`
	Schedule      *runScheduleIdentity `json:"schedule,omitempty"`
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
	Environment string     `json:"environment"`
	Start       *time.Time `json:"start,omitempty"`
	End         *time.Time `json:"end,omitempty"`
	FullRefresh bool       `json:"full_refresh,omitempty"`
	Backfill    bool       `json:"backfill,omitempty"`
	SensorMode  string     `json:"sensor_mode,omitempty"`
}

type runAuthorization struct {
	ConfirmedEnvironment string `json:"confirmed_environment,omitempty"`
}

type runScheduleIdentity struct {
	PipelineUUID      string `json:"pipeline_uuid"`
	Environment       string `json:"environment"`
	Cron              string `json:"cron,omitempty"`
	Timezone          string `json:"timezone,omitempty"`
	AdvancesWatermark bool   `json:"advances_watermark"`
}

func (spec runSpecV1) validate() error {
	if spec.Version != runSpecVersionV1 {
		return fmt.Errorf("unsupported run spec version %d", spec.Version)
	}
	if strings.TrimSpace(spec.Pipeline.ID) == "" || strings.TrimSpace(spec.Pipeline.Name) == "" {
		return errors.New("run spec pipeline id and name are required")
	}
	if spec.Dispatch != runDispatchRiver {
		return fmt.Errorf("unsupported run dispatch %q", spec.Dispatch)
	}
	if spec.Selection != runSelectionAll {
		return fmt.Errorf("unsupported run selection %q", spec.Selection)
	}
	if err := validateRunSpecSource(spec.Source); err != nil {
		return err
	}
	if (spec.Requested.Start == nil) != (spec.Requested.End == nil) {
		return errors.New("run spec start and end must both be set or both be omitted")
	}
	if spec.Requested.Start != nil && !spec.Requested.Start.Before(*spec.Requested.End) {
		return errors.New("run spec requires an increasing execution window")
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
	if version != runSpecVersionV1 {
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
	return runSpecV1{
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
			Environment: strings.TrimSpace(run.Environment),
			Start:       cloneRunTime(run.WinStart),
			End:         cloneRunTime(run.WinEnd),
			FullRefresh: run.FullRefresh,
			Backfill:    run.Backfill,
			SensorMode:  strings.TrimSpace(run.SensorMode),
		},
		Authorization: runAuthorization{ConfirmedEnvironment: strings.TrimSpace(confirmedEnvironment)},
		Selection:     runSelectionAll,
	}
}

func validateRunSpecBinding(run PipelineRun, spec runSpecV1) error {
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
			Environment: strings.TrimSpace(run.Environment),
			Start:       cloneRunTime(run.WinStart),
			End:         cloneRunTime(run.WinEnd),
			FullRefresh: run.FullRefresh,
			Backfill:    run.Backfill,
			SensorMode:  strings.TrimSpace(run.SensorMode),
		},
		Authorization: runAuthorization{ConfirmedEnvironment: strings.TrimSpace(args.ConfirmedEnvironment)},
		Selection:     runSelectionAll,
		Schedule: &runScheduleIdentity{
			PipelineUUID:      pipelineUUID,
			Environment:       strings.TrimSpace(run.Environment),
			Cron:              strings.TrimSpace(args.Schedule),
			Timezone:          strings.TrimSpace(args.Timezone),
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
	return run
}
