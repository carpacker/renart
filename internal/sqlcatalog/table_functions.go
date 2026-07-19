package sqlcatalog

import "strings"

// Column describes one fixed output column exposed by a built-in table
// function. Keeping these signatures here lets SQL analysis and asset schema
// inference share one catalog.
type Column struct {
	Name string
	Type string
}

var duckDBTableFunctionColumns = map[string][]Column{
	"range": {
		{Name: "range", Type: "BIGINT"},
	},
	"generate_series": {
		{Name: "generate_series", Type: "BIGINT"},
	},
}

// DuckDBTableFunctionColumns returns the fixed output signature of a known
// DuckDB table function. The returned slice is a copy and may be modified by
// the caller.
func DuckDBTableFunctionColumns(name string) ([]Column, bool) {
	columns, ok := duckDBTableFunctionColumns[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil, false
	}
	return append([]Column(nil), columns...), true
}
