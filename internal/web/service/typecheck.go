package service

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/spf13/afero"

	"renart/internal/web/sqlintelligence"
)

const typeCheckRunID = "renart-type-check"

// TypeCheck loads a pipeline by its workspace ID and type-checks every asset.
// startDate/endDate are optional ISO dates used to build the Jinja date context;
// when empty they are derived from the pipeline's schedule.
func (s *PipelineService) TypeCheck(ctx context.Context, pipelineID, startDate, endDate string) (TypeCheckReport, *APIError) {
	_, absPath, err := s.resolver().DecodePipelineID(pipelineID)
	if err != nil {
		return TypeCheckReport{}, &APIError{Status: 400, Code: "invalid_pipeline_id", Message: err.Error()}
	}
	// Load the full pipeline (WithMutate, not WithOnlyPipeline) so asset content
	// and columns are populated — type checking needs the rendered SQL.
	parsed, err := s.newPipelineBuilder().CreatePipelineFromPath(ctx, absPath, pipeline.WithMutate())
	if err != nil {
		return TypeCheckReport{}, &APIError{Status: 400, Code: "pipeline_not_found", Message: err.Error()}
	}

	tw, err := ResolveExecutionTimeWindow(string(parsed.Schedule), startDate, endDate, time.Now().UTC())
	if err != nil {
		return TypeCheckReport{}, &APIError{Status: 400, Code: "invalid_time_window", Message: err.Error()}
	}

	report := CheckPipeline(ctx, afero.NewOsFs(), parsed, s.workspaceRoot, tw)
	report.PipelineID = pipelineID
	return report, nil
}

const (
	typeCheckStatusOK      = "ok"
	typeCheckStatusWarning = "warning"
	typeCheckStatusError   = "error"

	typeCheckSeverityError   = "error"
	typeCheckSeverityWarning = "warning"
)

// TypeCheckFinding is a single diagnostic about an asset (a type/column error
// from the SQL parser, a template-rendering failure, or a missing-columns
// warning). Line/column are 1-based and point into the rendered SQL.
type TypeCheckFinding struct {
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Line      int    `json:"line,omitempty"`
	Column    int    `json:"column,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	EndColumn int    `json:"end_column,omitempty"`
}

// TypeCheckAsset is the per-asset result of a pipeline type check.
type TypeCheckAsset struct {
	ID       string             `json:"id,omitempty"`
	Name     string             `json:"name"`
	Type     string             `json:"type"`
	Dialect  string             `json:"dialect,omitempty"`
	Status   string             `json:"status"`
	Findings []TypeCheckFinding `json:"findings"`
}

// TypeCheckSummary aggregates finding counts across the pipeline.
type TypeCheckSummary struct {
	Assets   int `json:"assets"`
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
}

// TypeCheckReport is the full result of type-checking a pipeline.
type TypeCheckReport struct {
	Status       string           `json:"status"`
	PipelineID   string           `json:"pipeline_id,omitempty"`
	PipelineName string           `json:"pipeline_name"`
	StartDate    string           `json:"start_date,omitempty"`
	EndDate      string           `json:"end_date,omitempty"`
	Assets       []TypeCheckAsset `json:"assets"`
	Summary      TypeCheckSummary `json:"summary"`
}

// CheckPipeline type-checks every asset in a parsed pipeline.
//
// For SQL assets it renders the asset (Jinja templates, pipeline variables, and
// start/end/execution dates) and validates the rendered query against a schema
// built from the other assets' declared and inferable columns — surfacing
// unresolved tables/columns and type errors. For non-SQL assets that produce a
// table but whose schema cannot be inferred from their definition (Python,
// ingestr, Load, or API assets without response fields/OpenAPI metadata) it
// warns when no columns are declared, since that breaks downstream type
// checking.
//
// workspaceRoot may be empty (e.g. from the CLI), in which case asset IDs are
// left blank.
func CheckPipeline(ctx context.Context, fs afero.Fs, pp *pipeline.Pipeline, workspaceRoot string, tw ExecutionTimeWindow) TypeCheckReport {
	now := time.Now().UTC()
	macroContent, _ := jinja.LoadMacros(fs, pp.MacrosPath)
	renderer := jinja.NewRendererWithStartEndDatesAndMacros(
		&tw.Start, &tw.End, &now, pp.Name, typeCheckRunID, jinja.Context(pp.Variables.Value()), macroContent,
	)

	report := TypeCheckReport{
		PipelineName: pp.Name,
		StartDate:    tw.StartRFC3339(),
		EndDate:      tw.EndRFC3339(),
		Assets:       []TypeCheckAsset{},
	}

	assets := make([]*pipeline.Asset, 0, len(pp.Assets))
	for _, asset := range pp.Assets {
		if asset != nil {
			assets = append(assets, asset)
		}
	}
	sort.SliceStable(assets, func(i, j int) bool { return assets[i].Name < assets[j].Name })

	knownSchema := assetsWithKnownSchema(ctx, pp)

	for _, asset := range assets {
		ac := checkAsset(ctx, fs, pp, workspaceRoot, renderer, now, tw, asset, knownSchema)
		report.Assets = append(report.Assets, ac)
		report.Summary.Assets++
		for _, finding := range ac.Findings {
			switch finding.Severity {
			case typeCheckSeverityError:
				report.Summary.Errors++
			case typeCheckSeverityWarning:
				report.Summary.Warnings++
			}
		}
	}

	report.Status = typeCheckStatusOK
	if report.Summary.Warnings > 0 {
		report.Status = typeCheckStatusWarning
	}
	if report.Summary.Errors > 0 {
		report.Status = typeCheckStatusError
	}
	return report
}

func checkAsset(ctx context.Context, fs afero.Fs, pp *pipeline.Pipeline, workspaceRoot string, renderer *jinja.Renderer, now time.Time, tw ExecutionTimeWindow, asset *pipeline.Asset, knownSchema map[string]bool) TypeCheckAsset {
	ac := TypeCheckAsset{
		ID:       assetReportID(workspaceRoot, asset),
		Name:     asset.Name,
		Type:     string(asset.Type),
		Findings: []TypeCheckFinding{},
	}

	dialect, dialectErr := AssetTypeToDialect(asset.Type)
	if dialectErr != nil {
		// Non-SQL asset: we cannot infer its output schema. Warn when a
		// table-producing asset declares no columns so the gap is visible.
		if nonSQLColumnsExpected(asset) && !assetHasDeclaredColumns(ctx, asset) {
			ac.Findings = append(ac.Findings, TypeCheckFinding{
				Severity: typeCheckSeverityWarning,
				Message:  "Declares no columns; types cannot be inferred for " + string(asset.Type) + " assets. Declare columns to enable downstream type checking.",
			})
		}
		ac.Status = statusFromFindings(ac.Findings)
		return ac
	}

	ac.Dialect = dialect
	if strings.TrimSpace(asset.ExecutableFile.Content) == "" {
		ac.Status = statusFromFindings(ac.Findings)
		return ac
	}

	queries, renderErr := renderAssetQueries(ctx, fs, renderer, now, tw, pp, asset)
	if renderErr != nil {
		ac.Findings = append(ac.Findings, TypeCheckFinding{
			Severity: typeCheckSeverityError,
			Message:  "Failed to render template: " + renderErr.Error(),
		})
		ac.Status = typeCheckStatusError
		return ac
	}

	schema := sqlintelligence.Schema{}
	sources := sqlintelligence.SchemaColumnSourceMethods{}
	ApplyAssetSQLDefinitionColumns(ctx, pp, asset, schema, sources)
	// Register every other pipeline asset as a resolvable table even when its
	// columns are unknown, so references to it are not flagged "Unresolved
	// table". Column checks against unknown-schema tables are suppressed below.
	for _, other := range pp.Assets {
		if other == nil || other == asset {
			continue
		}
		name := strings.TrimSpace(other.Name)
		if name == "" {
			continue
		}
		if _, exists := schema[name]; !exists {
			schema[name] = map[string]string{}
		}
	}

	for _, renderedQuery := range queries {
		parseContext, err := sqlintelligence.ParseContextWithSchema(renderedQuery, dialect, schema, sources)
		if err != nil {
			ac.Findings = append(ac.Findings, TypeCheckFinding{
				Severity: typeCheckSeverityError,
				Message:  "Failed to parse SQL: " + err.Error(),
			})
			continue
		}
		// When the query reads from an upstream whose schema we cannot infer
		// (an undeclared Python/API/Load asset, already flagged on the producer),
		// we cannot verify column references against it — so suppress the
		// cascading "unresolved column/table" errors rather than blaming the
		// consumer for the producer's missing column declarations.
		suppressUnresolved := referencesUnknownSchema(parseContext, knownSchema)
		for _, diagnostic := range parseContext.Diagnostics {
			if suppressUnresolved && isUnresolvedMessage(diagnostic.Message) {
				continue
			}
			ac.Findings = append(ac.Findings, findingFromDiagnostic(diagnostic))
		}
		for _, parseErr := range parseContext.Errors {
			ac.Findings = append(ac.Findings, TypeCheckFinding{Severity: typeCheckSeverityError, Message: parseErr})
		}
	}

	ac.Status = statusFromFindings(ac.Findings)
	return ac
}

// assetsWithKnownSchema maps each asset name (lower-cased) to whether its output
// columns can be determined statically — via declared columns, API response
// fields, or (for SQL assets) the column names in its SELECT list.
func assetsWithKnownSchema(ctx context.Context, pp *pipeline.Pipeline) map[string]bool {
	known := make(map[string]bool, len(pp.Assets))
	for _, asset := range pp.Assets {
		if asset == nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(asset.Name))
		if name == "" {
			continue
		}
		isKnown := assetHasDeclaredColumns(ctx, asset)
		if !isKnown {
			if _, err := AssetTypeToDialect(asset.Type); err == nil {
				isKnown = len(ExtractSQLDefinitionColumns(asset.ExecutableFile.Content)) > 0
			}
		}
		known[name] = isKnown
	}
	return known
}

func referencesUnknownSchema(parseContext *sqlintelligence.ParseContext, knownSchema map[string]bool) bool {
	// With no resolved tables the FROM clause is a table-valued function,
	// subquery, or literal the parser cannot introspect (e.g. DuckDB's
	// `range(...)`), so any "unresolved column" is a parser limitation rather
	// than a real type error — don't trust column checks here.
	if len(parseContext.Tables) == 0 {
		return true
	}
	for _, table := range parseContext.Tables {
		name := strings.ToLower(strings.TrimSpace(table.ResolvedName))
		if name == "" {
			name = strings.ToLower(strings.TrimSpace(table.Name))
		}
		if isKnown, exists := knownSchema[name]; exists && !isKnown {
			return true
		}
	}
	return false
}

func isUnresolvedMessage(message string) bool {
	return strings.HasPrefix(message, "Unresolved column") ||
		strings.HasPrefix(message, "Unresolved table")
}

func renderAssetQueries(ctx context.Context, fs afero.Fs, renderer *jinja.Renderer, now time.Time, tw ExecutionTimeWindow, pp *pipeline.Pipeline, asset *pipeline.Asset) ([]string, error) {
	fetchCtx := context.WithValue(ctx, pipeline.RunConfigStartDate, tw.Start)
	fetchCtx = context.WithValue(fetchCtx, pipeline.RunConfigEndDate, tw.End)
	fetchCtx = context.WithValue(fetchCtx, pipeline.RunConfigExecutionDate, now)
	fetchCtx = context.WithValue(fetchCtx, pipeline.RunConfigRunID, typeCheckRunID)

	extractor := &query.WholeFileExtractor{Fs: fs, Renderer: renderer}
	cloned, err := extractor.CloneForAsset(fetchCtx, pp, asset)
	if err != nil {
		return nil, err
	}
	extracted, err := cloned.ExtractQueriesFromString(asset.ExecutableFile.Content)
	if err != nil {
		return nil, err
	}

	queries := make([]string, 0, len(extracted))
	for _, q := range extracted {
		if strings.TrimSpace(q.Query) != "" {
			queries = append(queries, q.Query)
		}
	}
	return queries, nil
}

func findingFromDiagnostic(diagnostic sqlintelligence.ParseContextDiagnostic) TypeCheckFinding {
	finding := TypeCheckFinding{
		Severity: normalizeSeverity(diagnostic.Severity),
		Message:  diagnostic.Message,
	}
	if diagnostic.Range != nil {
		finding.Line = diagnostic.Range.Line
		finding.Column = diagnostic.Range.Col
		finding.EndLine = diagnostic.Range.EndLine
		finding.EndColumn = diagnostic.Range.EndCol
	}
	return finding
}

func normalizeSeverity(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "error":
		return typeCheckSeverityError
	case "warning", "warn":
		return typeCheckSeverityWarning
	default:
		// Treat anything else (info/hint) as a warning so it is still surfaced
		// without failing the check.
		return typeCheckSeverityWarning
	}
}

func statusFromFindings(findings []TypeCheckFinding) string {
	status := typeCheckStatusOK
	for _, finding := range findings {
		if finding.Severity == typeCheckSeverityError {
			return typeCheckStatusError
		}
		if finding.Severity == typeCheckSeverityWarning {
			status = typeCheckStatusWarning
		}
	}
	return status
}

func assetReportID(workspaceRoot string, asset *pipeline.Asset) string {
	if strings.TrimSpace(workspaceRoot) == "" {
		return ""
	}
	path := asset.ExecutableFile.Path
	if path == "" {
		path = asset.DefinitionFile.Path
	}
	if path == "" {
		return ""
	}
	rel, err := filepath.Rel(workspaceRoot, path)
	if err != nil {
		rel = path
	}
	return EncodeID(filepath.ToSlash(rel))
}

func nonSQLColumnsExpected(asset *pipeline.Asset) bool {
	if isAPIAsset(asset) || isLoadAsset(asset) {
		return true
	}
	assetType := strings.ToLower(string(asset.Type))
	return strings.Contains(assetType, "python") || strings.Contains(assetType, "ingestr")
}

func assetHasDeclaredColumns(ctx context.Context, asset *pipeline.Asset) bool {
	if len(asset.Columns) > 0 {
		return true
	}
	if isAPIAsset(asset) && len(apiResponseFieldColumns(ctx, asset)) > 0 {
		return true
	}
	return false
}
