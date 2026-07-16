package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/mask"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/bruin-data/bruin/pkg/tablename"
	"github.com/spf13/afero"

	"renart/internal/web/fingerprint"
	"renart/internal/web/snapshot"
)

const assetRenderPreviewRunID = "renart-render-preview"

var (
	ErrAssetRenderSourceChanged  = errors.New("asset source changed while rendering")
	errQuerySensorQueryIsMissing = errors.New("query sensor parameter \"query\" is required")
)

type assetRenderManifestCollector func(string) (map[string]string, map[string][]byte, error)
type assetRenderSourceStateCollector func(string) (snapshot.SourceState, error)

type assetRenderBoundaryError struct {
	status  int
	code    string
	message string
	cause   error
}

func (e *assetRenderBoundaryError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *assetRenderBoundaryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func classifyAssetRenderError(status int, code, message string, cause error) error {
	return &assetRenderBoundaryError{status: status, code: code, message: message, cause: cause}
}

type AssetRenderStatus string

const (
	AssetRenderStatusOK          AssetRenderStatus = "ok"
	AssetRenderStatusPartial     AssetRenderStatus = "partial"
	AssetRenderStatusUnsupported AssetRenderStatus = "unsupported"
	AssetRenderStatusError       AssetRenderStatus = "error"
)

type AssetRenderStageStatus string

const (
	AssetRenderStageStatusOK          AssetRenderStageStatus = "ok"
	AssetRenderStageStatusUnsupported AssetRenderStageStatus = "unsupported"
	AssetRenderStageStatusError       AssetRenderStageStatus = "error"
)

type AssetRenderFidelity string

const (
	AssetRenderFidelityExact       AssetRenderFidelity = "exact"
	AssetRenderFidelitySemantic    AssetRenderFidelity = "semantic"
	AssetRenderFidelityRuntimeOnly AssetRenderFidelity = "runtime_only"
	AssetRenderFidelityUnsupported AssetRenderFidelity = "unsupported"
)

type AssetRenderRequest struct {
	Environment   string `json:"environment,omitempty"`
	StartDate     string `json:"start_date,omitempty"`
	EndDate       string `json:"end_date,omitempty"`
	ExecutionTime string `json:"execution_time,omitempty"`
	FullRefresh   bool   `json:"full_refresh"`
}

type AssetRenderSource struct {
	Kind         string `json:"kind"`
	PipelinePath string `json:"pipeline_path"`
	MerkleRoot   string `json:"merkle_root"`
}

type AssetRenderContext struct {
	Environment          string `json:"environment,omitempty"`
	SchemaPrefix         string `json:"schema_prefix,omitempty"`
	StartDate            string `json:"start_date"`
	EndDate              string `json:"end_date"`
	ExecutionTime        string `json:"execution_time"`
	RunID                string `json:"run_id"`
	RequestedFullRefresh bool   `json:"requested_full_refresh"`
	FullRefresh          bool   `json:"full_refresh"`
	VariablesDigest      string `json:"variables_digest"`
	// ConfigurationDigest covers only the selected environment fields used by
	// this connection-free DuckDB render slice. It is not the canonical
	// configuration or physical-target identity required by pipeline planning.
	ConfigurationDigest string `json:"configuration_digest"`
}

type AssetRenderProvenance struct {
	Source   AssetRenderSource  `json:"source"`
	Pipeline string             `json:"pipeline"`
	Context  AssetRenderContext `json:"context"`
}

type AssetRenderAsset struct {
	ID             string `json:"id,omitempty"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Dialect        string `json:"dialect,omitempty"`
	ConnectionName string `json:"connection_name,omitempty"`
}

type AssetRenderStage struct {
	Kind        string                 `json:"kind"`
	Language    string                 `json:"language"`
	Content     string                 `json:"content,omitempty"`
	Status      AssetRenderStageStatus `json:"status"`
	Fidelity    AssetRenderFidelity    `json:"fidelity"`
	Conditional bool                   `json:"conditional,omitempty"`
	Redacted    bool                   `json:"redacted,omitempty"`
	Message     string                 `json:"message,omitempty"`
}

type AssetRenderRedaction struct {
	Kind        string `json:"kind"`
	Replacement string `json:"replacement"`
}

type AssetRenderIssue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type AssetRenderResult struct {
	Status     AssetRenderStatus      `json:"status"`
	Provenance AssetRenderProvenance  `json:"provenance"`
	Asset      AssetRenderAsset       `json:"asset"`
	Stages     []AssetRenderStage     `json:"stages"`
	Issues     []AssetRenderIssue     `json:"issues"`
	Redactions []AssetRenderRedaction `json:"redactions"`
}

// AssetRenderService renders a saved working-tree asset without connecting to
// a warehouse. The first vertical slice provides exact compiled queries for
// SQL assets and exact materialization SQL for DuckDB/MotherDuck query assets.
// Other runtimes remain explicit partial/unsupported results until their
// executor construction exposes the same read-only planning seam.
type AssetRenderService struct {
	workspaceRoot      string
	fs                 afero.Fs
	now                func() time.Time
	collectManifest    assetRenderManifestCollector
	collectSourceState assetRenderSourceStateCollector
}

func NewAssetRenderService(workspaceRoot string) *AssetRenderService {
	return &AssetRenderService{
		workspaceRoot:      workspaceRoot,
		fs:                 afero.NewOsFs(),
		now:                func() time.Time { return time.Now().UTC() },
		collectManifest:    snapshot.CollectManifest,
		collectSourceState: snapshot.CollectSourceState,
	}
}

// RenderAsset renders the saved asset identified by the same opaque workspace
// ID used by the rest of the HTTP API. Callers cannot provide a filesystem
// path or run ID: both are resolved/owned by the server.
func (s *AssetRenderService) RenderAsset(ctx context.Context, assetID string, req AssetRenderRequest) (AssetRenderResult, *APIError) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return AssetRenderResult{}, &APIError{Status: 400, Code: "asset_id_required", Message: "asset id is required"}
	}
	assetPath, err := DecodeID(assetID)
	if err != nil {
		return AssetRenderResult{}, &APIError{Status: 400, Code: "invalid_asset_id", Message: "asset id is invalid"}
	}
	result, err := s.renderPath(ctx, assetPath, req)
	if err == nil {
		return result, nil
	}
	if errors.Is(err, ErrAssetNotFound) {
		return AssetRenderResult{}, &APIError{Status: 404, Code: "asset_not_found", Message: "asset was not found"}
	}
	if errors.Is(err, ErrAssetRenderSourceChanged) {
		return AssetRenderResult{}, &APIError{
			Status:  409,
			Code:    "source_changed",
			Message: "asset source changed while rendering; retry the preview",
		}
	}
	var boundaryErr *assetRenderBoundaryError
	if errors.As(err, &boundaryErr) {
		return AssetRenderResult{}, &APIError{
			Status:  boundaryErr.status,
			Code:    boundaryErr.code,
			Message: boundaryErr.message,
		}
	}
	// Keep unclassified parse/render failures as authoring errors. Infrastructure
	// failures are classified at the point where they occur so they cannot be
	// mistaken for a bad saved asset without turning incomplete authoring into a
	// server fault.
	return AssetRenderResult{}, &APIError{Status: 400, Code: "asset_render_failed", Message: "asset could not be rendered"}
}

// RenderPath is the in-process/CLI entry point. HTTP handlers must use
// RenderAsset so a request body can never choose a filesystem path.
func (s *AssetRenderService) RenderPath(ctx context.Context, assetPath string, req AssetRenderRequest) (AssetRenderResult, error) {
	return s.renderPath(ctx, assetPath, req)
}

func (s *AssetRenderService) renderPath(ctx context.Context, assetPath string, req AssetRenderRequest) (result AssetRenderResult, resultErr error) {
	if s == nil {
		return AssetRenderResult{}, fmt.Errorf("asset render service is not configured")
	}
	assetPath, err := resolveWorkspaceRenderAssetPath(s.workspaceRoot, assetPath)
	if err != nil {
		return AssetRenderResult{}, classifyAssetRenderError(
			http.StatusBadRequest,
			"invalid_asset_path",
			"asset path is invalid",
			err,
		)
	}

	fs := s.fs
	if fs == nil {
		fs = afero.NewOsFs()
	}
	pipelineDir, err := findPipelineRootForAsset(assetPath)
	if err != nil {
		return AssetRenderResult{}, fmt.Errorf("resolve pipeline for rendering: %w", err)
	}
	collectManifest := s.collectManifest
	if collectManifest == nil {
		collectManifest = snapshot.CollectManifest
	}
	collectSourceState := s.collectSourceState
	if collectSourceState == nil {
		collectSourceState = snapshot.CollectSourceState
	}
	sourceStateBeforeHash, err := collectSourceState(pipelineDir)
	if err != nil {
		return AssetRenderResult{}, classifyAssetRenderError(
			http.StatusInternalServerError,
			"source_identity_failed",
			"asset source identity could not be computed",
			fmt.Errorf("capture source state: %w", err),
		)
	}
	manifest, _, err := collectManifest(pipelineDir)
	if err != nil {
		return AssetRenderResult{}, classifyAssetRenderError(
			http.StatusInternalServerError,
			"source_identity_failed",
			"asset source identity could not be computed",
			fmt.Errorf("compute source identity: %w", err),
		)
	}
	if len(manifest) == 0 {
		err = fmt.Errorf("compute source identity: pipeline contains no source files")
		return AssetRenderResult{}, classifyAssetRenderError(
			http.StatusInternalServerError,
			"source_identity_failed",
			"asset source identity could not be computed",
			err,
		)
	}
	sourceMerkleRoot := snapshot.ManifestRoot(manifest)
	sourceState, err := collectSourceState(pipelineDir)
	if err != nil {
		return AssetRenderResult{}, classifyAssetRenderError(
			http.StatusInternalServerError,
			"source_identity_failed",
			"asset source identity could not be verified",
			fmt.Errorf("verify source identity after hashing: %w", err),
		)
	}
	if !sourceStateBeforeHash.Equal(sourceState) {
		return AssetRenderResult{}, ErrAssetRenderSourceChanged
	}
	defer func() {
		latestSourceState, verifyErr := collectSourceState(pipelineDir)
		if verifyErr != nil {
			if resultErr == nil {
				result = AssetRenderResult{}
				resultErr = classifyAssetRenderError(
					http.StatusInternalServerError,
					"source_identity_failed",
					"asset source identity could not be verified",
					fmt.Errorf("verify source identity after rendering: %w", verifyErr),
				)
			}
			return
		}
		if !sourceState.Equal(latestSourceState) {
			result = AssetRenderResult{}
			resultErr = ErrAssetRenderSourceChanged
		}
	}()

	pp, err := getDirectPipelineAndAssetReadOnly(ctx, s.workspaceRoot, assetPath, fs)
	if err != nil {
		return AssetRenderResult{}, fmt.Errorf("resolve asset for rendering: %w", err)
	}
	if pp == nil || pp.Pipeline == nil || pp.Asset == nil {
		return AssetRenderResult{}, fmt.Errorf("resolved asset is incomplete")
	}
	if _, err := selectConfigEnvironment(pp.Config, req.Environment); err != nil {
		return AssetRenderResult{}, classifyAssetRenderError(
			http.StatusBadRequest,
			"invalid_environment",
			"selected environment is not configured",
			fmt.Errorf("select environment: %w", err),
		)
	}
	applySelectedEnvironmentRefreshRestriction(pp.Config, pp.Pipeline.Assets)
	effectiveFullRefresh := req.FullRefresh && (pp.Asset.RefreshRestricted == nil || !*pp.Asset.RefreshRestricted)

	executionTime, err := s.resolveExecutionTime(req.ExecutionTime)
	if err != nil {
		return AssetRenderResult{}, classifyAssetRenderError(
			http.StatusBadRequest,
			"invalid_execution_time",
			"execution_time must be an RFC3339 timestamp",
			err,
		)
	}
	timeWindow, err := ResolveExecutionTimeWindow(
		string(pp.Pipeline.Schedule),
		req.StartDate,
		req.EndDate,
		executionTime,
	)
	if err != nil {
		return AssetRenderResult{}, classifyAssetRenderError(
			http.StatusBadRequest,
			"invalid_time_window",
			err.Error(),
			fmt.Errorf("resolve execution window: %w", err),
		)
	}

	runID := assetRenderPreviewRunID

	result = AssetRenderResult{
		Status: AssetRenderStatusOK,
		Provenance: AssetRenderProvenance{
			Source: AssetRenderSource{
				Kind:         "working_tree",
				PipelinePath: workspaceRelativeRenderPath(s.workspaceRoot, pp.Pipeline.DefinitionFile.Path),
				MerkleRoot:   sourceMerkleRoot,
			},
			Pipeline: pp.Pipeline.Name,
			Context: AssetRenderContext{
				Environment:          pp.Config.SelectedEnvironmentName,
				StartDate:            timeWindow.StartRFC3339(),
				EndDate:              timeWindow.EndRFC3339(),
				ExecutionTime:        executionTime.Format(time.RFC3339Nano),
				RunID:                runID,
				RequestedFullRefresh: req.FullRefresh,
				FullRefresh:          effectiveFullRefresh,
				VariablesDigest:      fingerprint.AllVarsHash(fingerprint.EffectiveVars(pp.Pipeline, nil)),
			},
		},
		Asset: AssetRenderAsset{
			ID:   assetReportID(s.workspaceRoot, pp.Asset),
			Name: pp.Asset.Name,
			Type: string(pp.Asset.Type),
		},
		Stages:     []AssetRenderStage{},
		Issues:     []AssetRenderIssue{},
		Redactions: []AssetRenderRedaction{},
	}
	if req.FullRefresh && !effectiveFullRefresh {
		result.Issues = append(result.Issues, AssetRenderIssue{
			Code:     "full_refresh_restricted",
			Severity: "warning",
			Message:  "the selected environment restricts full refresh; execution uses the configured materialization strategy",
		})
	}
	if pp.Config.SelectedEnvironment != nil {
		result.Provenance.Context.SchemaPrefix = pp.Config.SelectedEnvironment.SchemaPrefix
	}
	if connectionName, connectionErr := pp.Pipeline.GetConnectionNameForAsset(pp.Asset); connectionErr == nil {
		result.Asset.ConnectionName = connectionName
	} else {
		result.Status = AssetRenderStatusPartial
		result.Issues = append(result.Issues, AssetRenderIssue{
			Code:     "connection_unresolved",
			Severity: "error",
			Message:  connectionErr.Error(),
		})
	}
	result.Provenance.Context.ConfigurationDigest = assetRenderConfigurationDigest(pp.Config, result.Asset.ConnectionName)

	dialect, dialectErr := AssetTypeToDialect(pp.Asset.Type)
	if dialectErr != nil {
		result.Status = AssetRenderStatusUnsupported
		result.Stages = append(result.Stages, AssetRenderStage{
			Kind:     "query",
			Language: languageForRenderAsset(pp.Asset),
			Status:   AssetRenderStageStatusUnsupported,
			Fidelity: AssetRenderFidelityUnsupported,
			Message:  "static rendering is not supported for this asset type",
		})
		return finalizeAssetRenderResult(result, pp.Config), nil
	}
	result.Asset.Dialect = dialect

	renderer, err := buildAssetPlanRenderer(fs, pp.Pipeline, timeWindow, executionTime, runID)
	if err != nil {
		return AssetRenderResult{}, fmt.Errorf("build renderer: %w", err)
	}
	renderCtx := assetPlanRenderContext(ctx, pp.Config, timeWindow, executionTime, runID, effectiveFullRefresh)
	extractor := &query.WholeFileExtractor{Fs: fs, Renderer: renderer}
	assetExtractor, err := extractor.CloneForAsset(renderCtx, pp.Pipeline, pp.Asset)
	if err != nil {
		result.Status = AssetRenderStatusError
		result.Stages = append(result.Stages, failedExactRenderStage("compiled_query", err))
		return finalizeAssetRenderResult(result, pp.Config), nil
	}

	querySource, err := querySourceForRenderAsset(pp.Asset)
	if err != nil {
		result.Status = AssetRenderStatusError
		result.Stages = append(result.Stages, failedExactRenderStage("compiled_query", err))
		if errors.Is(err, errQuerySensorQueryIsMissing) {
			result.Issues = append(result.Issues, AssetRenderIssue{
				Code:     "query_sensor_query_missing",
				Severity: "error",
				Message:  err.Error(),
			})
		}
		return finalizeAssetRenderResult(result, pp.Config), nil
	}

	queries, err := assetExtractor.ExtractQueriesFromString(querySource)
	if err != nil {
		result.Status = AssetRenderStatusError
		result.Stages = append(result.Stages, failedExactRenderStage("compiled_query", err))
		return finalizeAssetRenderResult(result, pp.Config), nil
	}
	compiledQuery, err := compiledQueryForRenderAsset(pp.Asset, queries)
	if err != nil {
		result.Status = AssetRenderStatusError
		result.Stages = append(result.Stages, failedExactRenderStage("compiled_query", err))
		return finalizeAssetRenderResult(result, pp.Config), nil
	}

	result.Stages = append(result.Stages, AssetRenderStage{
		Kind:     "compiled_query",
		Language: "sql",
		Content:  compiledQuery,
		Status:   AssetRenderStageStatusOK,
		Fidelity: AssetRenderFidelityExact,
	})

	if isQuerySensorAssetType(pp.Asset.Type) {
		result.Stages = append(result.Stages, AssetRenderStage{
			Kind:     "execution_sql",
			Language: "sql",
			Content:  compiledQuery,
			Status:   AssetRenderStageStatusOK,
			Fidelity: AssetRenderFidelityExact,
			Message:  "exact rendered query submitted by the sensor; polling mode, interval, and timeout are runtime controls and are not included in this SQL stage",
		})
		return finalizeAssetRenderResult(result, pp.Config), nil
	}

	executionAsset := pp.Asset
	if supportsExactDuckDBExecutionRender(pp.Asset) {
		resolvedHooks, resolveErr := resolveAssetHookTemplates(renderCtx, pp.Pipeline, pp.Asset, renderer)
		if resolveErr != nil {
			result.Status = AssetRenderStatusPartial
			result.Stages = append(result.Stages, failedExactRenderStage("execution_sql", resolveErr))
			return finalizeAssetRenderResult(result, pp.Config), nil
		}
		assetCopy := *pp.Asset
		assetCopy.Hooks = resolvedHooks
		executionAsset = &assetCopy
	}

	executionSQL, supported, err := renderExactDuckDBExecutionSQL(executionAsset, assetExtractor, compiledQuery, effectiveFullRefresh)
	if !supported {
		result.Status = AssetRenderStatusPartial
		result.Stages = append(result.Stages, AssetRenderStage{
			Kind:     "execution_sql",
			Language: "sql",
			Status:   AssetRenderStageStatusUnsupported,
			Fidelity: AssetRenderFidelityUnsupported,
			Message:  "exact execution rendering is not yet exposed for this SQL asset type",
		})
		return finalizeAssetRenderResult(result, pp.Config), nil
	}
	if err != nil {
		result.Status = AssetRenderStatusPartial
		result.Stages = append(result.Stages, failedExactRenderStage("execution_sql", err))
		return finalizeAssetRenderResult(result, pp.Config), nil
	}

	if pp.Asset.Materialization.Type != pipeline.MaterializationTypeNone {
		if schemaName, hasSchema := tablename.SchemaToCreate(pp.Asset.Name, strings.ToLower); hasSchema {
			result.Status = AssetRenderStatusPartial
			result.Stages = append(result.Stages, AssetRenderStage{
				Kind:        "schema_preparation",
				Language:    "sql",
				Content:     "CREATE SCHEMA IF NOT EXISTS " + schemaName,
				Status:      AssetRenderStageStatusOK,
				Fidelity:    AssetRenderFidelitySemantic,
				Conditional: true,
				Message:     "executed until Bruin's connection-local schema cache records this schema",
			})
		}
	}

	executionFidelity := AssetRenderFidelityExact
	executionMessage := ""
	if result.Provenance.Context.SchemaPrefix != "" {
		// The DuckDB operator's developer-environment modifier consults the live
		// database schema and may rename referenced tables after materialization.
		// Planning must remain connection-free, so expose the canonical
		// pre-rewrite SQL while being explicit that the final query is runtime-only.
		result.Status = AssetRenderStatusPartial
		executionFidelity = AssetRenderFidelityRuntimeOnly
		executionMessage = "pre-rewrite materializer SQL; the developer-environment table rewrite depends on live warehouse state"
	}
	if duckDBMaterializationUsesEphemeralIdentifiers(pp.Asset, effectiveFullRefresh) {
		result.Status = AssetRenderStatusPartial
		executionFidelity = AssetRenderFidelityRuntimeOnly
		if executionMessage != "" {
			executionMessage += "; "
		}
		executionMessage += "temporary table identifiers are generated independently when execution starts"
	}

	result.Stages = append(result.Stages, AssetRenderStage{
		Kind:     "execution_sql",
		Language: "sql",
		Content:  executionSQL,
		Status:   AssetRenderStageStatusOK,
		Fidelity: executionFidelity,
		Message:  executionMessage,
	})
	return finalizeAssetRenderResult(result, pp.Config), nil
}

func assetRenderConfigurationDigest(cfg *config.Config, connectionName string) string {
	environmentName := ""
	schemaPrefix := ""
	refreshRestricted := "false"
	connectionType := ""
	if cfg != nil {
		environmentName = cfg.SelectedEnvironmentName
		if cfg.SelectedEnvironment != nil {
			schemaPrefix = cfg.SelectedEnvironment.SchemaPrefix
			if cfg.SelectedEnvironment.Config != nil && cfg.SelectedEnvironment.Config.RefreshRestricted {
				refreshRestricted = "true"
			}
			if cfg.SelectedEnvironment.Connections != nil {
				connectionType = cfg.SelectedEnvironment.Connections.ConnectionsSummaryList()[connectionName]
			}
		}
	}

	hasher := sha256.New()
	for _, part := range []string{environmentName, schemaPrefix, refreshRestricted, connectionName, connectionType} {
		var length [8]byte
		n := len(part)
		for i := 7; i >= 0; i-- {
			length[i] = byte(n)
			n >>= 8
		}
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write([]byte(part))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func finalizeAssetRenderResult(result AssetRenderResult, cfg *config.Config) AssetRenderResult {
	if cfg == nil || cfg.SelectedEnvironment == nil || cfg.SelectedEnvironment.Connections == nil {
		return result
	}
	redactor := mask.New(mask.InlineSensitiveValues(cfg.SelectedEnvironment.Connections))
	if redactor.Empty() {
		return result
	}

	redacted := false
	for i := range result.Stages {
		content := redactor.Mask(result.Stages[i].Content)
		message := redactor.Mask(result.Stages[i].Message)
		if content != result.Stages[i].Content || message != result.Stages[i].Message {
			result.Stages[i].Redacted = true
			redacted = true
		}
		result.Stages[i].Content = content
		result.Stages[i].Message = message
	}
	for i := range result.Issues {
		message := redactor.Mask(result.Issues[i].Message)
		redacted = redacted || message != result.Issues[i].Message
		result.Issues[i].Message = message
	}
	if redacted {
		result.Redactions = append(result.Redactions, AssetRenderRedaction{
			Kind:        "connection_credentials",
			Replacement: mask.Mask,
		})
	}
	return result
}

func (s *AssetRenderService) resolveExecutionTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if s.now == nil {
			return time.Now().UTC(), nil
		}
		return s.now().UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid execution_time: %w", err)
	}
	return parsed.UTC(), nil
}

func buildAssetPlanRenderer(fs afero.Fs, pl *pipeline.Pipeline, timeWindow ExecutionTimeWindow, executionTime time.Time, runID string) (*jinja.Renderer, error) {
	macroContent, err := jinja.LoadMacros(fs, pl.MacrosPath)
	if err != nil {
		return nil, err
	}
	return jinja.NewRendererWithStartEndDatesAndMacros(
		&timeWindow.Start,
		&timeWindow.End,
		&executionTime,
		pl.Name,
		runID,
		nil,
		macroContent,
	), nil
}

func assetPlanRenderContext(ctx context.Context, cfg *config.Config, timeWindow ExecutionTimeWindow, executionTime time.Time, runID string, fullRefresh bool) context.Context {
	ctx = context.WithValue(ctx, pipeline.RunConfigStartDate, timeWindow.Start)
	ctx = context.WithValue(ctx, pipeline.RunConfigEndDate, timeWindow.End)
	ctx = context.WithValue(ctx, pipeline.RunConfigExecutionDate, executionTime)
	ctx = context.WithValue(ctx, pipeline.RunConfigRunID, runID)
	ctx = context.WithValue(ctx, pipeline.RunConfigFullRefresh, fullRefresh)
	ctx = context.WithValue(ctx, pipeline.RunConfigApplyIntervalModifiers, false)
	if cfg != nil {
		ctx = context.WithValue(ctx, config.EnvironmentContextKey, cfg.SelectedEnvironment)
		ctx = context.WithValue(ctx, config.EnvironmentNameContextKey, cfg.SelectedEnvironmentName)
	}
	return ctx
}

func renderExactDuckDBExecutionSQL(asset *pipeline.Asset, extractor query.QueryExtractor, compiledQuery string, fullRefresh bool) (string, bool, error) {
	if !supportsExactDuckDBExecutionRender(asset) {
		return "", false, nil
	}

	materializer := newDirectDuckDBHookMaterializer(fullRefresh)
	executionSQL, err := materializer.Render(asset, compiledQuery)
	if err != nil {
		return "", true, err
	}

	// The direct DuckDB operator re-renders the materialized script for the
	// time_interval strategy because that materializer introduces date Jinja
	// expressions. Preserve that exact behavior and keep the result as one
	// canonical SQL blob rather than attempting to split or classify it.
	if asset.Materialization.Strategy == pipeline.MaterializationStrategyTimeInterval {
		rendered, renderErr := extractor.ExtractQueriesFromString(executionSQL)
		if renderErr != nil {
			return "", true, fmt.Errorf("re-render time_interval execution SQL: %w", renderErr)
		}
		if len(rendered) == 0 {
			return "", true, fmt.Errorf("time_interval execution SQL rendered empty")
		}
		executionSQL = rendered[0].Query
	}

	return executionSQL, true, nil
}

func supportsExactDuckDBExecutionRender(asset *pipeline.Asset) bool {
	if asset == nil {
		return false
	}
	return asset.Type == pipeline.AssetTypeDuckDBQuery || asset.Type == pipeline.AssetTypeMotherduckQuery
}

func querySourceForRenderAsset(asset *pipeline.Asset) (string, error) {
	if asset == nil {
		return "", fmt.Errorf("asset is required")
	}
	if !isQuerySensorAssetType(asset.Type) {
		return asset.ExecutableFile.Content, nil
	}

	querySource, ok := asset.Parameters.GetString("query")
	if !ok || strings.TrimSpace(querySource) == "" {
		return "", errQuerySensorQueryIsMissing
	}
	return querySource, nil
}

func compiledQueryForRenderAsset(asset *pipeline.Asset, queries []*query.Query) (string, error) {
	if len(queries) == 0 {
		return "", fmt.Errorf("no query was extracted")
	}
	if len(queries) > 1 && asset != nil && asset.Materialization.Type != pipeline.MaterializationTypeNone {
		return "", fmt.Errorf("cannot enable materialization for tasks with multiple queries")
	}
	return queries[0].Query, nil
}

func duckDBMaterializationUsesEphemeralIdentifiers(asset *pipeline.Asset, fullRefresh bool) bool {
	if asset == nil || asset.Materialization.Type != pipeline.MaterializationTypeTable {
		return false
	}
	if fullRefresh && asset.Materialization.Strategy != pipeline.MaterializationStrategyDDL &&
		(asset.RefreshRestricted == nil || !*asset.RefreshRestricted) {
		return false
	}
	return asset.Materialization.Strategy == pipeline.MaterializationStrategyDeleteInsert ||
		asset.Materialization.Strategy == pipeline.MaterializationStrategyMerge
}

func failedExactRenderStage(kind string, err error) AssetRenderStage {
	return AssetRenderStage{
		Kind:     kind,
		Language: "sql",
		Status:   AssetRenderStageStatusError,
		Fidelity: AssetRenderFidelityExact,
		Message:  err.Error(),
	}
}

func languageForRenderAsset(asset *pipeline.Asset) string {
	if asset == nil {
		return ""
	}
	path := asset.ExecutableFile.Path
	if path == "" {
		path = asset.DefinitionFile.Path
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py":
		return "python"
	case ".sql":
		return "sql"
	case ".yml", ".yaml":
		return "yaml"
	default:
		return ""
	}
}

func workspaceRelativeRenderPath(workspaceRoot, path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	rel, err := filepath.Rel(workspaceRoot, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func resolveWorkspaceRenderAssetPath(workspaceRoot, input string) (string, error) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return "", fmt.Errorf("workspace root is required")
	}
	input = strings.TrimSpace(input)
	if filepath.IsAbs(input) {
		return "", fmt.Errorf("asset_path must be workspace-relative")
	}

	clean := filepath.Clean(filepath.FromSlash(input))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("asset_path must stay within the workspace")
	}

	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	joined, err := filepath.Abs(filepath.Join(root, clean))
	if err != nil {
		return "", fmt.Errorf("resolve asset_path: %w", err)
	}
	if !renderPathIsWithin(root, joined) {
		return "", fmt.Errorf("asset_path must stay within the workspace")
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root symlinks: %w", err)
	}
	resolvedAsset, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", fmt.Errorf("resolve asset_path symlinks: %w", err)
	}
	if !renderPathIsWithin(resolvedRoot, resolvedAsset) {
		return "", fmt.Errorf("asset_path must stay within the workspace")
	}
	return joined, nil
}

func renderPathIsWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
