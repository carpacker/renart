package scheduler

import "time"

const (
	ExecutionTargetSnapshotVersionV1 = 1
	ExecutionTargetSnapshotVersionV2 = 2
)

type RunStatus string

const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusSuccess   RunStatus = "success"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

type RunTrigger string

const (
	RunTriggerSchedule RunTrigger = "schedule"
	RunTriggerManual   RunTrigger = "manual"
	RunTriggerAPI      RunTrigger = "api"
	RunTriggerCLI      RunTrigger = "cli"
)

// RunSource identifies the source tree a manual run executes. Scheduled runs
// receive their source from the persisted environment schedule instead.
type RunSource string

const (
	RunSourceWorkingTree RunSource = "working_tree"
	RunSourceSnapshot    RunSource = "snapshot"
)

type PipelineSchedule struct {
	PipelineID   string     `json:"pipeline_id"`
	PipelineUUID string     `json:"pipeline_uuid,omitempty"`
	PipelineName string     `json:"pipeline_name"`
	PipelinePath string     `json:"pipeline_path"`
	Schedule     string     `json:"schedule"`
	Timezone     string     `json:"timezone"`
	Catchup      bool       `json:"catchup"`
	Enabled      bool       `json:"enabled"`
	NextRunAt    *time.Time `json:"next_run_at,omitempty"`
}

type CatchupPolicy string

const (
	CatchupSkip     CatchupPolicy = "skip"
	CatchupRunOnce  CatchupPolicy = "run_once"
	CatchupBackfill CatchupPolicy = "backfill"
)

type ScheduleStatus string

const (
	ScheduleStatusActive   ScheduleStatus = "active"
	ScheduleStatusPaused   ScheduleStatus = "paused"
	ScheduleStatusArchived ScheduleStatus = "archived"
	// ScheduleStatusDelegated is reserved for cloud-executed schedules.
	ScheduleStatusDelegated ScheduleStatus = "delegated"
)

const (
	// ArchivedReasonMissing marks reconciler tombstones (pipeline file gone,
	// e.g. branch switch); these auto-restore when the file reappears.
	ArchivedReasonMissing = "missing"
	// ArchivedReasonUser marks explicit deletions; never auto-restored.
	ArchivedReasonUser = "user"
)

// EnvSchedule is one (pipeline, environment) schedule row — the unit of
// schedule identity.
type EnvSchedule struct {
	PipelineUUID      string         `json:"pipeline_uuid"`
	Environment       string         `json:"environment"`
	SnapshotVersionID string         `json:"snapshot_version_id,omitempty"`
	Cron              string         `json:"cron"`
	Timezone          string         `json:"timezone"`
	Vars              map[string]any `json:"vars,omitempty"`
	CatchupPolicy     CatchupPolicy  `json:"catchup_policy"`
	Status            ScheduleStatus `json:"status"`
	ArchivedReason    string         `json:"archived_reason,omitempty"`
	NextRunAt         *time.Time     `json:"next_run_at,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`

	// Resolved presentation fields (not persisted).
	PipelineID   string       `json:"pipeline_id,omitempty"` // path-encoded API ID
	PipelineName string       `json:"pipeline_name,omitempty"`
	LastRun      *PipelineRun `json:"last_run,omitempty"`
}

// PipelineRef resolves a stable pipeline UUID to its current workspace
// incarnation.
type PipelineRef struct {
	EncodedID string
	Name      string
}

// UpsertEnvScheduleRequest creates or updates a per-environment schedule.
type UpsertEnvScheduleRequest struct {
	// Environment comes exclusively from the URL path at the HTTP boundary.
	// Keeping it out of JSON prevents a second, conflicting schedule identity.
	Environment       string         `json:"-"`
	Cron              string         `json:"cron"`
	Timezone          string         `json:"timezone"`
	Vars              map[string]any `json:"vars,omitempty"`
	CatchupPolicy     CatchupPolicy  `json:"catchup_policy,omitempty"`
	SnapshotVersionID string         `json:"snapshot_version_id,omitempty"`
	// DeployNow deploys the working tree and pins the schedule to the new
	// snapshot when none exists yet.
	DeployNow bool `json:"deploy_now,omitempty"`
	Paused    bool `json:"paused,omitempty"`
}

type UpdateScheduleRequest struct {
	Enabled  bool   `json:"enabled"`
	Schedule string `json:"schedule"`
	Timezone string `json:"timezone"`
	Catchup  bool   `json:"catchup"`
}

type TriggerRequest struct {
	Environment string `json:"environment"`
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
	// Source is normalized by the admission layer. Empty remains a temporary
	// internal compatibility value and is treated as working_tree; callers that
	// request a snapshot must always provide its exact immutable version ID.
	Source            RunSource `json:"source,omitempty"`
	SnapshotVersionID string    `json:"snapshot_version_id,omitempty"`
	// LegacyTrigger accepts the former client hint for rolling compatibility.
	// It never controls persisted origin; only "manual" is accepted at HTTP.
	LegacyTrigger        string `json:"trigger,omitempty"`
	FullRefresh          bool   `json:"full_refresh,omitempty"`
	Backfill             bool   `json:"backfill,omitempty"`
	ConfirmedEnvironment string `json:"confirmed_environment,omitempty"`
	SensorMode           string `json:"sensor_mode,omitempty"`
	// Expected identities and the preview execution time are server-owned plan
	// confirmation evidence. The ordinary trigger JSON endpoint cannot set them.
	ExpectedSourceMerkle        string `json:"-"`
	ExpectedConfigurationDigest string `json:"-"`
	ExecutionTime               string `json:"-"`
	// ConfirmedPlan is the server-regenerated, redacted plan admitted with this
	// run. It is never accepted from the ordinary trigger JSON endpoint.
	ConfirmedPlan *PipelineRunPlan `json:"-"`
}

type PipelineRun struct {
	ID         string `json:"id"`
	PipelineID string `json:"pipeline_id"`
	// RiverJobID links this application-level run to its internal queue job.
	// It is deliberately not exposed through the API.
	RiverJobID *int64 `json:"-"`
	// PipelineUUID is the stable identity for per-environment scheduled
	// runs. Not persisted; carried in memory so run completion can advance
	// the (pipeline, environment) watermark.
	PipelineUUID string     `json:"pipeline_uuid,omitempty"`
	Pipeline     string     `json:"pipeline"`
	Environment  string     `json:"environment"`
	Trigger      RunTrigger `json:"trigger"`
	Status       RunStatus  `json:"status"`
	WinStart     *time.Time `json:"win_start,omitempty"`
	WinEnd       *time.Time `json:"win_end,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	Error        string     `json:"error,omitempty"`
	LogRef       string     `json:"log_ref,omitempty"`
	// SnapshotVersionID records the deployed snapshot the run executed;
	// empty for working-tree builds.
	SnapshotVersionID string `json:"snapshot_version_id,omitempty"`
	// FullRefresh, Backfill, and SensorMode hold the normalized admission request
	// until ExecutionContextResolved becomes true. Once resolved, they are the
	// effective modes persisted before the first asset starts. They remain an
	// internal recovery contract until the API has a nested requested/effective
	// execution-context DTO.
	FullRefresh bool   `json:"-"`
	Backfill    bool   `json:"-"`
	SensorMode  string `json:"-"`
	// Plan confirmation evidence is persisted only in the private RunSpec.
	ExecutionTime               *time.Time `json:"-"`
	ExpectedSourceMerkle        string     `json:"-"`
	ExpectedConfigurationDigest string     `json:"-"`
	// ExecutionContextResolved distinguishes effective execution provenance from
	// pending, pre-execution-failed, and legacy request-only rows. Callers must
	// not treat environment, window, or mode fields as executed context while it
	// is false.
	ExecutionContextResolved bool `json:"execution_context_resolved"`
	// ExecutionTargetSnapshot is the immutable, secret-free execution identity
	// captured after runtime context resolution and before the first asset is
	// executed. It remains private recovery provenance rather than API state.
	ExecutionTargetSnapshot *ExecutionTargetSnapshot `json:"-"`
	// ConfirmedPlan is loaded from the immutable reviewed-plan row for worker
	// execution. It is never exposed through run-list DTOs or SSE events.
	ConfirmedPlan *PipelineRunPlan `json:"-"`
}

// ExecutionTargetSnapshot captures the exact source and physical-target
// identity used by one run. Entries are keyed by canonical asset name because
// persisted run steps use that name; AssetID retains the durable identity used
// by fingerprint, coverage, and materialization stores.
type ExecutionTargetSnapshot struct {
	Version               int                                     `json:"version"`
	PipelineUUID          string                                  `json:"pipeline_uuid,omitempty"`
	ConfigurationDigest   string                                  `json:"configuration_digest,omitempty"`
	ConfigurationFidelity string                                  `json:"configuration_fidelity,omitempty"`
	Entries               map[string]ExecutionTargetSnapshotEntry `json:"entries"`
}

type ExecutionUpstreamSnapshot struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type ExecutionTargetSnapshotEntry struct {
	AssetID           string                      `json:"asset_id"`
	TargetIdentity    string                      `json:"target_identity,omitempty"`
	TargetFidelity    string                      `json:"target_fidelity"`
	Fingerprint       string                      `json:"fingerprint"`
	OwnContent        string                      `json:"own_content"`
	ConsumedVarsHash  string                      `json:"consumed_vars_hash"`
	VarsHash          string                      `json:"vars_hash"`
	Upstreams         []ExecutionUpstreamSnapshot `json:"upstreams,omitempty"`
	CoverageMode      string                      `json:"coverage_mode,omitempty"`
	RefreshRestricted bool                        `json:"refresh_restricted,omitempty"`
}

type PipelineRunStep struct {
	RunID                     string                            `json:"run_id"`
	Asset                     string                            `json:"asset"`
	Status                    RunStatus                         `json:"status"`
	StartedAt                 *time.Time                        `json:"started_at,omitempty"`
	FinishedAt                *time.Time                        `json:"finished_at,omitempty"`
	Error                     string                            `json:"error,omitempty"`
	CompletionOrdinal         *int64                            `json:"completion_ordinal,omitempty"`
	UpstreamWriters           map[string]UpstreamWriterSnapshot `json:"-"`
	HasUpstreamWriterSnapshot bool                              `json:"-"`
}

// UpstreamWriterSnapshot is the exact physical output an asset read from one
// upstream immediately before its main task began. The containing map is keyed
// by AssetID. It is persisted with the run step so recovery never has to infer
// read provenance from a later latest-writer state.
type UpstreamWriterSnapshot struct {
	AssetID           string
	TargetIdentity    string
	Fingerprint       string
	VarsHash          string
	TargetGeneration  int64
	CompletionID      string
	CompletionOrdinal int64
	MaterializedAt    time.Time
}

type LogLine struct {
	At   time.Time `json:"at"`
	Line string    `json:"line"`
}

type RunFilter struct {
	PipelineID  string
	Environment string
	Status      RunStatus
	Query       string
	Limit       int
	Offset      int
}

type RunList struct {
	Runs   []PipelineRun `json:"runs"`
	Total  int           `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

type RunResult struct {
	Status string
	Error  string
}

type Runner func(ctx Context, req RunRequest, onLog func(string)) RunResult

type Context interface {
	Done() <-chan struct{}
	Err() error
}

type RunRequest struct {
	RunID         string
	PipelineID    string
	PipelineUUID  string
	Environment   string
	Start         string
	End           string
	ExecutionTime string
	// Scheduled is derived from the persisted server-owned run origin. It must
	// not be inferred from RunID because queued manual runs also have one.
	Scheduled bool
	// SnapshotVersionID pins the exact deployed snapshot the run must execute.
	// Empty identifies a manual working-tree run and is invalid when Scheduled.
	SnapshotVersionID           string
	FullRefresh                 bool
	Backfill                    bool
	ConfirmedEnvironment        string
	SensorMode                  string
	ExpectedSourceMerkle        string
	ExpectedConfigurationDigest string
	ConfirmedPlan               *PipelineRunPlan
	// OnContextResolved must be called synchronously after execution policy,
	// source, defaults, and modes are resolved but before the first asset starts.
	// The scheduler uses it to durably replace admission intent and publish a
	// canonical running event.
	OnContextResolved func(RunExecutionContext) error
	// OnTargetsResolved persists the value-only execution target snapshot after
	// effective configuration is selected and before the first task starts.
	OnTargetsResolved func(ExecutionTargetSnapshot) error
	// OnStep is synchronous: a running-step persistence failure must stop before
	// the physical task, and a terminal persistence failure must fail closed.
	OnStep func(RunStepEvent) error
	// OnUnit persists one exact asset/window unit. Unlike OnStep, positions can
	// represent multiple windows for the same asset.
	OnUnit func(PipelineRunUnitEvent) error
}

type RunStepEvent struct {
	Asset                     string
	Status                    RunStatus
	StartedAt                 *time.Time
	FinishedAt                *time.Time
	Error                     string
	CompletionOrdinal         *int64
	UpstreamWriters           map[string]UpstreamWriterSnapshot
	HasUpstreamWriterSnapshot bool
}
