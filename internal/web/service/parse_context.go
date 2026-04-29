package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"renart/internal/web/sqlintelligence"
)

var assetTypeDialectMap = map[pipeline.AssetType]string{
	pipeline.AssetTypeBigqueryQuery:   "bigquery",
	pipeline.AssetTypeSnowflakeQuery:  "snowflake",
	pipeline.AssetTypePostgresQuery:   "postgres",
	pipeline.AssetTypeRedshiftQuery:   "postgres",
	pipeline.AssetTypeTrinoQuery:      "trino",
	pipeline.AssetTypeAthenaQuery:     "athena",
	pipeline.AssetTypeClickHouse:      "clickhouse",
	pipeline.AssetTypeDatabricksQuery: "databricks",
	pipeline.AssetTypeMsSQLQuery:      "tsql",
	pipeline.AssetTypeSynapseQuery:    "tsql",
	pipeline.AssetTypeDuckDBQuery:     "duckdb",
}

type ParseContextSchemaColumn struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type ParseContextSchemaTable struct {
	Name    string                     `json:"name"`
	Columns []ParseContextSchemaColumn `json:"columns"`
}

type ParseContextRange struct {
	Start   int `json:"start"`
	End     int `json:"end"`
	Line    int `json:"line"`
	Col     int `json:"col"`
	EndLine int `json:"end_line"`
	EndCol  int `json:"end_col"`
}

type ParseContextPart struct {
	Name  string            `json:"name"`
	Kind  string            `json:"kind"`
	Range ParseContextRange `json:"range"`
}

type ParseContextDiagnostic struct {
	Message  string             `json:"message"`
	Severity string             `json:"severity"`
	Range    *ParseContextRange `json:"range,omitempty"`
}

type ParseContextTable struct {
	Name         string                       `json:"name"`
	SourceKind   string                       `json:"source_kind,omitempty"`
	ResolvedName string                       `json:"resolved_name,omitempty"`
	Alias        string                       `json:"alias,omitempty"`
	Columns      map[string]string            `json:"columns,omitempty"`
	ColumnRanges map[string]ParseContextRange `json:"column_ranges,omitempty"`
	Parts        []ParseContextPart           `json:"parts"`
	AliasRange   *ParseContextRange           `json:"alias_range,omitempty"`
}

type ParseContextColumn struct {
	Name          string             `json:"name"`
	Qualifier     string             `json:"qualifier,omitempty"`
	ResolvedTable string             `json:"resolved_table,omitempty"`
	Parts         []ParseContextPart `json:"parts"`
}

type ParseContextResult struct {
	Status         string                   `json:"status"`
	AssetID        string                   `json:"asset_id"`
	Dialect        string                   `json:"dialect,omitempty"`
	QueryKind      string                   `json:"query_kind,omitempty"`
	IsSingleSelect bool                     `json:"is_single_select"`
	Tables         []ParseContextTable      `json:"tables"`
	Columns        []ParseContextColumn     `json:"columns"`
	Diagnostics    []ParseContextDiagnostic `json:"diagnostics,omitempty"`
	Errors         []string                 `json:"errors,omitempty"`
	Error          string                   `json:"error,omitempty"`
}

type ParseContextAPIError struct {
	Status  int
	Code    string
	Message string
}

type ParseContextDependencies struct {
	ResolveAssetByID func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error)
}

type ParseContextService struct {
	deps ParseContextDependencies
}

func NewParseContextService(deps ParseContextDependencies) *ParseContextService {
	return &ParseContextService{deps: deps}
}

func (s *ParseContextService) Parse(ctx context.Context, assetID, content string, schemaTables []ParseContextSchemaTable) (ParseContextResult, *ParseContextAPIError) {
	_, _, asset, err := s.deps.ResolveAssetByID(ctx, assetID)
	if err != nil {
		return ParseContextResult{}, &ParseContextAPIError{Status: 400, Code: "asset_not_found", Message: err.Error()}
	}

	dialect, err := AssetTypeToDialect(asset.Type)
	if err != nil {
		return ParseContextResult{
			Status:  "ok",
			AssetID: assetID,
			Errors:  []string{"unsupported SQL dialect for parse context"},
			Tables:  []ParseContextTable{},
			Columns: []ParseContextColumn{},
		}, nil
	}

	if strings.TrimSpace(content) == "" {
		content = asset.ExecutableFile.Content
	}
	schema := BuildParseContextSchema(asset, schemaTables)

	parseContext, err := sqlintelligence.ParseContextWithSchema(content, dialect, schema)
	if err != nil {
		return ParseContextResult{
			Status:  "error",
			AssetID: assetID,
			Dialect: dialect,
			Error:   err.Error(),
			Tables:  []ParseContextTable{},
			Columns: []ParseContextColumn{},
		}, nil
	}

	return ParseContextResult{
		Status:         "ok",
		AssetID:        assetID,
		Dialect:        dialect,
		QueryKind:      parseContext.QueryKind,
		IsSingleSelect: parseContext.IsSingleSelect,
		Tables:         ParseContextTablesFromParser(parseContext.Tables),
		Columns:        ParseContextColumnsFromParser(parseContext.Columns),
		Diagnostics:    ParseContextDiagnosticsFromParser(parseContext.Diagnostics),
		Errors:         parseContext.Errors,
	}, nil
}

func AssetTypeToDialect(assetType pipeline.AssetType) (string, error) {
	dialect, ok := assetTypeDialectMap[assetType]
	if !ok {
		return "", fmt.Errorf("unsupported asset type %s", assetType)
	}

	return dialect, nil
}

func BuildParseContextSchema(asset *pipeline.Asset, suggestionTables []ParseContextSchemaTable) sqlintelligence.Schema {
	schema := sqlintelligence.Schema{}

	for _, table := range suggestionTables {
		if strings.TrimSpace(table.Name) == "" {
			continue
		}

		columns := map[string]string{}
		for _, column := range table.Columns {
			if strings.TrimSpace(column.Name) == "" {
				continue
			}
			columns[column.Name] = strings.TrimSpace(column.Type)
		}

		if len(columns) > 0 {
			schema[table.Name] = columns
		}
	}

	if asset != nil && strings.TrimSpace(asset.Name) != "" && len(asset.Columns) > 0 {
		columns := map[string]string{}
		for _, column := range asset.Columns {
			if strings.TrimSpace(column.Name) == "" {
				continue
			}
			columns[column.Name] = strings.TrimSpace(column.Type)
		}
		if len(columns) > 0 {
			schema[asset.Name] = columns
		}
	}

	return schema
}

func ParseContextRangeFromParser(input sqlintelligence.ParseContextRange) ParseContextRange {
	return ParseContextRange{Start: input.Start, End: input.End, Line: input.Line, Col: input.Col, EndLine: input.EndLine, EndCol: input.EndCol}
}

func ParseContextPartsFromParser(input []sqlintelligence.ParseContextPart) []ParseContextPart {
	result := make([]ParseContextPart, 0, len(input))
	for _, part := range input {
		result = append(result, ParseContextPart{Name: part.Name, Kind: part.Kind, Range: ParseContextRangeFromParser(part.Range)})
	}
	return result
}

func ParseContextTablesFromParser(input []sqlintelligence.ParseContextTable) []ParseContextTable {
	result := make([]ParseContextTable, 0, len(input))
	for _, table := range input {
		item := ParseContextTable{
			Name:         table.Name,
			SourceKind:   table.SourceKind,
			ResolvedName: table.ResolvedName,
			Alias:        table.Alias,
			Columns:      ParseContextSchemaColumnsFromParser(table.Columns),
			ColumnRanges: ParseContextColumnRangesFromParser(table.ColumnRanges),
			Parts:        ParseContextPartsFromParser(table.Parts),
		}
		if table.AliasRange != nil {
			aliasRange := ParseContextRangeFromParser(*table.AliasRange)
			item.AliasRange = &aliasRange
		}
		result = append(result, item)
	}
	return result
}

func ParseContextSchemaColumnsFromParser(input []sqlintelligence.SchemaColumn) map[string]string {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]string, len(input))
	for _, column := range input {
		result[column.Name] = column.Type
	}
	return result
}

func ParseContextColumnRangesFromParser(input map[string]sqlintelligence.ParseContextRange) map[string]ParseContextRange {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]ParseContextRange, len(input))
	for name, rangeValue := range input {
		result[name] = ParseContextRangeFromParser(rangeValue)
	}
	return result
}

func ParseContextColumnsFromParser(input []sqlintelligence.ParseContextColumn) []ParseContextColumn {
	result := make([]ParseContextColumn, 0, len(input))
	for _, column := range input {
		result = append(result, ParseContextColumn{
			Name:          column.Name,
			Qualifier:     column.Qualifier,
			ResolvedTable: column.ResolvedTable,
			Parts:         ParseContextPartsFromParser(column.Parts),
		})
	}
	return result
}

func ParseContextDiagnosticsFromParser(input []sqlintelligence.ParseContextDiagnostic) []ParseContextDiagnostic {
	result := make([]ParseContextDiagnostic, 0, len(input))
	for _, diagnostic := range input {
		item := ParseContextDiagnostic{Message: diagnostic.Message, Severity: diagnostic.Severity}
		if diagnostic.Range != nil {
			rangeValue := ParseContextRangeFromParser(*diagnostic.Range)
			item.Range = &rangeValue
		}
		result = append(result, item)
	}
	return result
}
