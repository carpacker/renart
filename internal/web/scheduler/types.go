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
	PipelineName string     `json:"pipeline_name"`
	PipelinePath string     `json:"pipeline_path"`
	Schedule     string     `json:"schedule"`
	Timezone     string     `json:"timezone"`
	Catchup      bool       `json:"catchup"`
	Enabled      bool       `json:"enabled"`
	NextRunAt    *time.Time `json:"next_run_at,omitempty"`
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
	Trigger     string `json:"trigger,omitempty"`
}

type PipelineRun struct {
	ID          string     `json:"id"`
	PipelineID  string     `json:"pipeline_id"`
	Pipeline    string     `json:"pipeline"`
	Environment string     `json:"environment"`
	Trigger     RunTrigger `json:"trigger"`
	Status      RunStatus  `json:"status"`
	WinStart    *time.Time `json:"win_start,omitempty"`
	WinEnd      *time.Time `json:"win_end,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	Error       string     `json:"error,omitempty"`
	LogRef      string     `json:"log_ref,omitempty"`
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
	PipelineID string
	Limit      int
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
	PipelineID  string
	Environment string
	Start       string
	End         string
	OnStep      func(RunStepEvent)
}

type RunStepEvent struct {
	Asset      string
	Status     RunStatus
	StartedAt  *time.Time
	FinishedAt *time.Time
	Error      string
}
