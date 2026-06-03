package sqlintelligence

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseContextWithSchemaPolyglotExtractsTablesColumnsAndDiagnostics(t *testing.T) {
	parseContext, err := ParseContextWithSchemaPolyglot(
		"select c.customer_id, o.total from analytics.customers c join analytics.orders o on c.customer_id = o.customer_id where missing_col = 1",
		"duckdb",
		Schema{
			"analytics.customers": {"customer_id": "int"},
			"analytics.orders":    {"customer_id": "int", "total": "int"},
		},
	)
	require.NoError(t, err)

	assert.Equal(t, "select", parseContext.QueryKind)
	assert.True(t, parseContext.IsSingleSelect)
	require.Len(t, parseContext.Tables, 2)
	tablesByName := map[string]ParseContextTable{}
	for _, table := range parseContext.Tables {
		tablesByName[table.Name] = table
	}
	assert.Equal(t, "c", tablesByName["analytics.customers"].Alias)
	assert.Equal(t, "o", tablesByName["analytics.orders"].Alias)

	columnNames := make([]string, 0, len(parseContext.Columns))
	for _, column := range parseContext.Columns {
		columnNames = append(columnNames, column.Name)
	}
	assert.Contains(t, columnNames, "customer_id")
	assert.Contains(t, columnNames, "total")
	assert.Contains(t, columnNames, "missing_col")

	require.NotEmpty(t, parseContext.Diagnostics)
	assert.Contains(t, parseContext.Diagnostics[0].Message, "missing_col")
	assert.Equal(t, "error", parseContext.Diagnostics[0].Severity)
	require.NotNil(t, parseContext.Diagnostics[0].Range)
	assert.Equal(t, "missing_col", parseContext.Diagnostics[0].Range.RangeText("select c.customer_id, o.total from analytics.customers c join analytics.orders o on c.customer_id = o.customer_id where missing_col = 1"))
}

func TestParseContextWithSchemaPolyglotResolvesQuickstartPlayerStats(t *testing.T) {
	query := `WITH players_white AS (
    SELECT white->>'@id' AS player_id
    FROM quickstart.games
),

players_black AS (
    SELECT black->>'@id' AS player_id
    FROM quickstart.games
)

SELECT
    name,
    (
        SELECT count(*) FROM players_white
        WHERE quickstart.players.aid = players_white.player_id
    ) AS games_white,
    (
        SELECT count(*) FROM players_black
        WHERE quickstart.players.aid = players_black.player_id
    ) as games_black
FROM quickstart.players`

	parseContext, err := ParseContextWithSchemaPolyglot(query, "duckdb", Schema{
		"quickstart.games":   {"white": "json", "black": "json"},
		"quickstart.players": {"aid": "varchar", "name": "varchar"},
	})
	require.NoError(t, err)

	assert.Empty(t, parseContext.Diagnostics)
}

func (r ParseContextRange) RangeText(query string) string {
	if r.Start < 0 || r.End > len(query) || r.Start > r.End {
		return ""
	}
	return query[r.Start:r.End]
}
