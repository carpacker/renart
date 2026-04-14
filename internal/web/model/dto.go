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
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Path   string  `json:"path"`
	Assets []Asset `json:"assets"`
}

// WorkspaceState represents the current state of a workspace.
type WorkspaceState struct {
	Pipelines           []Pipeline          `json:"pipelines"`
	Connections         map[string]string   `json:"connections"`
	SelectedEnvironment string              `json:"selected_environment"`
	Errors              []string            `json:"errors"`
	UpdatedAt           time.Time           `json:"updated_at"`
	Metadata            map[string][]string `json:"metadata"`
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
	Command    string   `json:"command"`
	PipelineID string   `json:"pipeline_id"`
	AssetPath  string   `json:"asset_path"`
	Args       []string `json:"args"`
}

// CommandResult represents the result of a command execution.
type CommandResult struct {
	Status    string   `json:"status"`
	Command   []string `json:"command"`
	Output    string   `json:"output"`
	ExitCode  int      `json:"exit_code"`
	Error     string   `json:"error,omitempty"`
	Attempts  int      `json:"attempts,omitempty"`
	Retryable bool     `json:"retryable,omitempty"`
}

// InspectResult represents the result of an asset inspection.
type InspectResult struct {
	Status    string           `json:"status"`
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	RawOutput string           `json:"raw_output"`
	Command   []string         `json:"command"`
	Error     string           `json:"error,omitempty"`
	Attempts  int              `json:"attempts,omitempty"`
	Retryable bool             `json:"retryable,omitempty"`
}

// InferColumnsResult represents the result of column inference.
type InferColumnsResult struct {
	Status    string   `json:"status"`
	Columns   []Column `json:"columns"`
	RawOutput string   `json:"raw_output"`
	Command   []string `json:"command"`
	Error     string   `json:"error,omitempty"`
}
