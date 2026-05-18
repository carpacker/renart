package service

import (
	"encoding/json"
	"sort"

	"github.com/bruin-data/bruin/pkg/query"
)

type QueryResultColumnDTO struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type QueryResultDTO struct {
	Columns        []QueryResultColumnDTO `json:"columns"`
	Rows           [][]interface{}        `json:"rows"`
	ConnectionName string                 `json:"connectionName"`
	Query          string                 `json:"query"`
	Status         string                 `json:"status,omitempty"`
	Error          string                 `json:"error,omitempty"`
	Output         string                 `json:"output,omitempty"`
}

type QueryRowsEnvelope struct {
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
}

func NewQueryResultDTO(result *query.QueryResult, connectionName, queryText string) QueryResultDTO {
	if result == nil {
		return QueryResultDTO{ConnectionName: connectionName, Query: queryText}
	}

	columns := make([]QueryResultColumnDTO, len(result.Columns))
	for i, colName := range result.Columns {
		colType := ""
		if i < len(result.ColumnTypes) {
			colType = result.ColumnTypes[i]
		}
		columns[i] = QueryResultColumnDTO{Name: colName, Type: colType}
	}

	return QueryResultDTO{
		Columns:        columns,
		Rows:           formatQueryRowsForJSON(result.Rows),
		ConnectionName: connectionName,
		Query:          queryText,
	}
}

func formatQueryRowsForJSON(rows [][]interface{}) [][]interface{} {
	formatted := make([][]interface{}, len(rows))
	for i, row := range rows {
		formatted[i] = make([]interface{}, len(row))
		for j, value := range row {
			formatted[i][j] = formatQueryJSONValue(value)
		}
	}
	return formatted
}

func formatQueryJSONValue(value interface{}) interface{} {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return string(v)
	default:
		return v
	}
}

func ParseQueryJSONOutput(output []byte) ([]string, []map[string]any) {
	rows := make([]map[string]any, 0)

	var asRows []map[string]any
	if err := json.Unmarshal(output, &asRows); err == nil {
		rows = asRows
		return inferColumns(rows), rows
	}

	var asEnvelope map[string]any
	if err := json.Unmarshal(output, &asEnvelope); err == nil {
		columns := extractColumnNames(asEnvelope["columns"])

		if v, ok := asEnvelope["rows"]; ok {
			if parsedRows := castRows(v); len(parsedRows) > 0 {
				rows = parsedRows
			} else if parsedRowsByColumns := castRowsByColumns(v, columns); len(parsedRowsByColumns) > 0 {
				rows = parsedRowsByColumns
			}
		}
		if len(rows) == 0 {
			if v, ok := asEnvelope["data"]; ok {
				if parsedRows := castRows(v); len(parsedRows) > 0 {
					rows = parsedRows
				} else {
					rows = castRowsByColumns(v, columns)
				}
			}
		}

		if len(columns) == 0 {
			columns = inferColumns(rows)
		}

		return columns, rows
	}

	return []string{}, rows
}

func extractColumnNames(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return []string{}
	}

	columns := make([]string, 0, len(items))
	for _, item := range items {
		if name, ok := item.(string); ok {
			columns = append(columns, name)
			continue
		}

		columnMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		nameValue, ok := columnMap["name"]
		if !ok {
			continue
		}

		if name, ok := nameValue.(string); ok {
			columns = append(columns, name)
		}
	}

	return columns
}

func castRows(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}

	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if ok {
			rows = append(rows, row)
		}
	}

	return rows
}

func castRowsByColumns(value any, columns []string) []map[string]any {
	if len(columns) == 0 {
		return []map[string]any{}
	}

	items, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}

	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		cellValues, ok := item.([]any)
		if !ok {
			continue
		}

		row := make(map[string]any, len(columns))
		for idx, column := range columns {
			if idx < len(cellValues) {
				row[column] = cellValues[idx]
				continue
			}
			row[column] = nil
		}

		rows = append(rows, row)
	}

	return rows
}

func inferColumns(rows []map[string]any) []string {
	if len(rows) == 0 {
		return []string{}
	}

	columns := make([]string, 0)
	for key := range rows[0] {
		columns = append(columns, key)
	}
	sort.Strings(columns)
	return columns
}
