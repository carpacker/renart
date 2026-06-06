package sqlintelligence

import (
	"os"
	"strings"
)

type Schema map[string]map[string]string

type SchemaColumnSourceMethods map[string]map[string][]string

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

type ParseContextTable struct {
	Name         string                       `json:"name"`
	SourceKind   string                       `json:"source_kind,omitempty"`
	ResolvedName string                       `json:"resolved_name,omitempty"`
	Alias        string                       `json:"alias"`
	Columns      []SchemaColumn               `json:"columns,omitempty"`
	ColumnRanges map[string]ParseContextRange `json:"column_ranges,omitempty"`
	Parts        []ParseContextPart           `json:"parts"`
	AliasRange   *ParseContextRange           `json:"alias_range,omitempty"`
	ScopeRange   *ParseContextRange           `json:"scope_range,omitempty"`
}

type SchemaColumn struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type ParseContextColumn struct {
	Name          string             `json:"name"`
	Qualifier     string             `json:"qualifier"`
	ResolvedTable string             `json:"resolved_table,omitempty"`
	Parts         []ParseContextPart `json:"parts"`
}

type ParseContextDiagnostic struct {
	Message  string             `json:"message"`
	Severity string             `json:"severity"`
	Range    *ParseContextRange `json:"range,omitempty"`
}

type ParseContext struct {
	QueryKind      string                   `json:"query_kind"`
	IsSingleSelect bool                     `json:"is_single_select"`
	Tables         []ParseContextTable      `json:"tables"`
	Columns        []ParseContextColumn     `json:"columns"`
	Diagnostics    []ParseContextDiagnostic `json:"diagnostics"`
	Errors         []string                 `json:"errors"`
}

type parseContextRequest struct {
	Query               string                    `json:"query"`
	Dialect             string                    `json:"dialect"`
	Schema              Schema                    `json:"schema,omitempty"`
	ColumnSourceMethods SchemaColumnSourceMethods `json:"column_source_methods,omitempty"`
}

type parseContextResponse struct {
	ParseContext
	Error string `json:"error,omitempty"`
}

func ParseContextWithSchema(query, dialect string, schema Schema, columnSourceMethods ...SchemaColumnSourceMethods) (*ParseContext, error) {
	if !strings.EqualFold(os.Getenv("RENART_SQL_PARSER"), "python") {
		return ParseContextWithSchemaPolyglot(query, dialect, schema, columnSourceMethods...)
	}
	return ParseContextWithSchemaPython(query, dialect, schema, columnSourceMethods...)
}
