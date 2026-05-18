package service

import (
	"encoding/json"
	"testing"

	"github.com/bruin-data/bruin/pkg/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewQueryResultDTOFormatsBytesAndColumnTypes(t *testing.T) {
	t.Parallel()

	dto := NewQueryResultDTO(&query.QueryResult{
		Columns:     []string{"id", "payload"},
		ColumnTypes: []string{"integer", "blob"},
		Rows: [][]interface{}{
			{1, []byte("hello")},
		},
	}, "duckdb-default", "select 1")

	require.Len(t, dto.Columns, 2)
	assert.Equal(t, QueryResultColumnDTO{Name: "id", Type: "integer"}, dto.Columns[0])
	assert.Equal(t, QueryResultColumnDTO{Name: "payload", Type: "blob"}, dto.Columns[1])
	assert.Equal(t, [][]interface{}{{1, "hello"}}, dto.Rows)
	assert.Equal(t, "duckdb-default", dto.ConnectionName)
	assert.Equal(t, "select 1", dto.Query)
}

func TestParseQueryJSONOutputSupportsDTOEnvelope(t *testing.T) {
	t.Parallel()

	output, err := json.Marshal(QueryResultDTO{
		Columns: []QueryResultColumnDTO{
			{Name: "id", Type: "integer"},
			{Name: "name", Type: "text"},
		},
		Rows: [][]interface{}{{float64(1), "Ada"}},
	})
	require.NoError(t, err)

	columns, rows := ParseQueryJSONOutput(output)

	assert.Equal(t, []string{"id", "name"}, columns)
	assert.Equal(t, []map[string]any{{"id": float64(1), "name": "Ada"}}, rows)
}

func TestParseQueryJSONOutputSupportsLegacyRowObjects(t *testing.T) {
	t.Parallel()

	columns, rows := ParseQueryJSONOutput([]byte(`[{"name":"Ada","id":1}]`))

	assert.Equal(t, []string{"id", "name"}, columns)
	assert.Equal(t, []map[string]any{{"id": float64(1), "name": "Ada"}}, rows)
}
