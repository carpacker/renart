// Package model provides data transfer objects for the Bruin web API.
package model

import "time"

// Asset represents a web API asset with its metadata.
type Asset struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Type                string            `json:"type"`
	Path                string            `json:"path"`
	Content             string            `json:"content"`
	Upstreams           []string          `json:"upstreams"`
	Parameters          map[string]string `json:"parameters,omitempty"`
	Meta                map[string]string `json:"meta,omitempty"`
	Columns             []Column          `json:"columns,omitempty"`
	Connection          string            `json:"connection,omitempty"`
	MaterializationType string            `json:"materialization_type,omitempty"`
	IsMaterialized      bool              `json:"is_materialized"`
	MaterializedAs      string            `json:"materialized_as,omitempty"`
	RowCount            *int64            `json:"row_count,omitempty"`
}

// Column represents a column in an asset.
type Column struct {
	Name          string            `json:"name"`
	Type          string            `json:"type,omitempty"`
	Description   string            `json:"description,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	PrimaryKey    bool              `json:"primary_key,omitempty"`
	UpdateOnMerge bool              `json:"update_on_merge,omitempty"`
	MergeSQL      string            `json:"merge_sql,omitempty"`
	Nullable      *bool             `json:"nullable,omitempty"`
	Owner         string            `json:"owner,omitempty"`
	Domains       []string          `json:"domains,omitempty"`
	Meta          map[string]string `json:"meta,omitempty"`
	Checks        []ColumnCheck     `json:"checks,omitempty"`
}

// ColumnCheck represents a check on a column.
type ColumnCheck struct {
	Name        string `json:"name"`
	Value       any    `json:"value,omitempty"`
	Blocking    *bool  `json:"blocking,omitempty"`
	Description string `json:"description,omitempty"`
}

// Pipeline represents a web API pipeline.
type Pipeline struct {
	ID string `json:"id"`
	// UUID is the stable identity stored in pipeline.yml (`id:`); all durable
	// records (schedules, run history, snapshots) key off it instead of ID,
	// which encodes the filesystem path.
	UUID     string  `json:"uuid,omitempty"`
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	Schedule string  `json:"schedule,omitempty"`
	Assets   []Asset `json:"assets"`
}

// EnvironmentPolicy mirrors the per-environment execution rules from
// .renart/environments.yml so the UI can disable controls; enforcement
// lives in the run-dispatch chokepoint, not here.
type EnvironmentPolicy struct {
	Protected          bool `json:"protected"`
	DeployedOnly       bool `json:"deployed_only"`
	ConfirmDestructive bool `json:"confirm_destructive"`
}

// WorkspaceState represents the current state of a workspace.
type WorkspaceState struct {
	Pipelines           []Pipeline                   `json:"pipelines"`
	Connections         map[string]string            `json:"connections"`
	SelectedEnvironment string                       `json:"selected_environment"`
	EnvironmentPolicies map[string]EnvironmentPolicy `json:"environment_policies,omitempty"`
	Errors              []string                     `json:"errors"`
	UpdatedAt           time.Time                    `json:"updated_at"`
	Metadata            map[string][]string          `json:"metadata"`
	Revision            int64                        `json:"revision,omitempty"`
}

// WorkspaceEvent represents an SSE event for workspace changes.
type WorkspaceEvent struct {
	Type      string         `json:"type"`
	Path      string         `json:"path,omitempty"`
	Workspace WorkspaceState `json:"workspace"`
}

// AssetMaterializationState represents the materialization state of an asset.
type AssetMaterializationState struct {
	AssetID         string `json:"asset_id"`
	IsMaterialized  bool   `json:"is_materialized"`
	MaterializedAs  string `json:"materialized_as,omitempty"`
	RowCount        *int64 `json:"row_count,omitempty"`
	Connection      string `json:"connection,omitempty"`
	DeclaredMatType string `json:"materialization_type,omitempty"`
}

// PipelineMaterializationResponse represents a pipeline materialization state response.
type PipelineMaterializationResponse struct {
	PipelineID string                      `json:"pipeline_id"`
	Assets     []AssetMaterializationState `json:"assets"`
}

// MaterializationInfo is internal state for pipeline materialization info.
type MaterializationInfo struct {
	AssetName       string
	Connection      string
	IsMaterialized  bool
	MaterializedAs  string
	RowCount        *int64
	DeclaredMatType string
}

// DBObjectInfo represents database object metadata.
type DBObjectInfo struct {
	Schema        string
	Name          string
	QualifiedName string
	Kind          string
}

// DuckDBExecutionInfo contains info needed for DuckDB query execution.
type DuckDBExecutionInfo struct {
	ConnectionName string
	DatabasePath   string
	LockKey        string
}

// CreatePipelineRequest is the request body for creating a pipeline.
type CreatePipelineRequest struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// UpdatePipelineRequest is the request body for updating a pipeline.
type UpdatePipelineRequest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type PipelineConfigConnection struct {
	Platform string `json:"platform"`
	Name     string `json:"name"`
}

type PipelineConfigNotification struct {
	Enabled    bool   `json:"enabled"`
	Channel    string `json:"channel,omitempty"`
	Connection string `json:"connection,omitempty"`
	Success    bool   `json:"success"`
	Failure    bool   `json:"failure"`
}

type PipelineConfigDefaults struct {
	RerunCooldown  *int   `json:"rerun_cooldown,omitempty"`
	StartOffsetRaw string `json:"start_offset_raw,omitempty"`
	EndOffsetRaw   string `json:"end_offset_raw,omitempty"`
}

type PipelineConfigVariable struct {
	Name         string         `json:"name"`
	Type         string         `json:"type"`
	DefaultValue any            `json:"default_value"`
	Description  string         `json:"description,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"`
}

type PipelineConfigResponse struct {
	Status               string                     `json:"status"`
	ID                   string                     `json:"id"`
	Path                 string                     `json:"path"`
	Name                 string                     `json:"name"`
	Schedule             string                     `json:"schedule,omitempty"`
	StartDate            string                     `json:"start_date,omitempty"`
	Owner                string                     `json:"owner,omitempty"`
	Tags                 []string                   `json:"tags"`
	Domains              []string                   `json:"domains"`
	DefaultConnections   []PipelineConfigConnection `json:"default_connections"`
	Catchup              bool                       `json:"catchup"`
	MetadataPushBigQuery bool                       `json:"metadata_push_bigquery"`
	Retries              int                        `json:"retries"`
	Concurrency          int                        `json:"concurrency"`
	MaxActiveSteps       *int                       `json:"max_active_steps,omitempty"`
	NotificationsSlack   PipelineConfigNotification `json:"notifications_slack"`
	NotificationsTeams   PipelineConfigNotification `json:"notifications_teams"`
	Defaults             PipelineConfigDefaults     `json:"defaults"`
	Variables            []PipelineConfigVariable   `json:"variables"`
	YAML                 string                     `json:"yaml"`
}

type UpdatePipelineConfigRequest struct {
	Name                 string                     `json:"name"`
	Schedule             string                     `json:"schedule"`
	StartDate            string                     `json:"start_date"`
	Owner                string                     `json:"owner"`
	Tags                 []string                   `json:"tags"`
	Domains              []string                   `json:"domains"`
	DefaultConnections   []PipelineConfigConnection `json:"default_connections"`
	Catchup              bool                       `json:"catchup"`
	MetadataPushBigQuery bool                       `json:"metadata_push_bigquery"`
	Retries              int                        `json:"retries"`
	Concurrency          int                        `json:"concurrency"`
	MaxActiveSteps       *int                       `json:"max_active_steps,omitempty"`
	NotificationsSlack   PipelineConfigNotification `json:"notifications_slack"`
	NotificationsTeams   PipelineConfigNotification `json:"notifications_teams"`
	Defaults             PipelineConfigDefaults     `json:"defaults"`
	Variables            []PipelineConfigVariable   `json:"variables"`
}

// CreateAssetRequest is the request body for creating an asset.
type CreateAssetRequest struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

// UpdateAssetRequest is the request body for updating an asset.
type UpdateAssetRequest struct {
	Type                *string           `json:"type,omitempty"`
	Content             *string           `json:"content,omitempty"`
	MaterializationType *string           `json:"materialization_type,omitempty"`
	Meta                map[string]string `json:"meta,omitempty"`
}

// UpdateAssetColumnsRequest is the request body for updating asset columns.
type UpdateAssetColumnsRequest struct {
	Columns []Column `json:"columns"`
}

// RunRequest is the request body for running commands.
type RunRequest struct {
	PipelineID  string `json:"pipeline_id"`
	AssetPath   string `json:"asset_path"`
	Environment string `json:"environment"`
}

// OperationMetadata describes the typed backend operation behind a response.
type OperationMetadata struct {
	Type           string   `json:"type"`
	Target         string   `json:"target,omitempty"`
	PipelineID     string   `json:"pipeline_id,omitempty"`
	AssetPath      string   `json:"asset_path,omitempty"`
	RunScope       string   `json:"run_scope,omitempty"`
	AssetPaths     []string `json:"asset_paths,omitempty"`
	ConnectionName string   `json:"connection_name,omitempty"`
	Query          string   `json:"query,omitempty"`
	Limit          string   `json:"limit,omitempty"`
	Environment    string   `json:"environment,omitempty"`
	StartDate      string   `json:"start_date,omitempty"`
	EndDate        string   `json:"end_date,omitempty"`
	Operation      string   `json:"operation,omitempty"`
	TargetPath     string   `json:"target_path,omitempty"`
	ConfigFile     string   `json:"config_file,omitempty"`
}

// CommandResult represents the result of a command execution.
type CommandResult struct {
	Status    string            `json:"status"`
	Operation OperationMetadata `json:"operation"`
	Output    string            `json:"output"`
	ExitCode  int               `json:"exit_code"`
	Error     string            `json:"error,omitempty"`
	Attempts  int               `json:"attempts,omitempty"`
	Retryable bool              `json:"retryable,omitempty"`
}

// InspectResult represents the result of an asset inspection.
type InspectResult struct {
	Status                              string            `json:"status"`
	Columns                             []string          `json:"columns"`
	Rows                                []map[string]any  `json:"rows"`
	RawOutput                           string            `json:"raw_output"`
	Operation                           OperationMetadata `json:"operation"`
	Error                               string            `json:"error,omitempty"`
	MissingUpstreamAssetIDs             []string          `json:"missing_upstream_asset_ids,omitempty"`
	MissingUpstreamAssetNames           []string          `json:"missing_upstream_asset_names,omitempty"`
	MissingUpstreamAssetsMaterializable bool              `json:"missing_upstream_assets_materializable,omitempty"`
	Attempts                            int               `json:"attempts,omitempty"`
	Retryable                           bool              `json:"retryable,omitempty"`
}

// InferColumnsResult represents the result of column inference.
type InferColumnsResult struct {
	Status    string            `json:"status"`
	Columns   []Column          `json:"columns"`
	RawOutput string            `json:"raw_output"`
	Operation OperationMetadata `json:"operation"`
	Error     string            `json:"error,omitempty"`
}
