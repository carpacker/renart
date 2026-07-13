package scheduler

import "time"

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
	Environment       string         `json:"environment"`
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
	Environment          string `json:"environment"`
	Start                string `json:"start,omitempty"`
	End                  string `json:"end,omitempty"`
	Trigger              string `json:"trigger,omitempty"`
	Backfill             bool   `json:"backfill,omitempty"`
	ConfirmedEnvironment string `json:"confirmed_environment,omitempty"`
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
}

type PipelineRunStep struct {
	RunID      string     `json:"run_id"`
	Asset      string     `json:"asset"`
	Status     RunStatus  `json:"status"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
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
	RunID       string
	PipelineID  string
	Environment string
	Start       string
	End         string
	// SnapshotVersionID pins the deployed snapshot the run must execute;
	// empty means "latest snapshot, else working tree".
	SnapshotVersionID string
	OnStep            func(RunStepEvent)
}

type RunStepEvent struct {
	Asset      string
	Status     RunStatus
	StartedAt  *time.Time
	FinishedAt *time.Time
	Error      string
}
