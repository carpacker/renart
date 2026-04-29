package service

import (
	"testing"

	"renart/internal/web/sqlintelligence"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssetTypeToDialect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		assetType pipeline.AssetType
		expected  string
	}{
		{pipeline.AssetTypeDuckDBQuery, "duckdb"},
		{pipeline.AssetTypePostgresQuery, "postgres"},
		{pipeline.AssetTypeBigqueryQuery, "bigquery"},
		{pipeline.AssetTypeSynapseQuery, "tsql"},
	}

	for _, tt := range tests {
		t.Run(string(tt.assetType), func(t *testing.T) {
			t.Parallel()
			dialect, err := AssetTypeToDialect(tt.assetType)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, dialect)
		})
	}
}

func TestAssetTypeToDialect_Unsupported(t *testing.T) {
	t.Parallel()

	_, err := AssetTypeToDialect(pipeline.AssetTypePython)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported asset type")
}

func TestBuildParseContextSchema_MergesSuggestedAndAssetColumns(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{
		Name: "marts.orders",
		Columns: []pipeline.Column{
			{Name: "order_id", Type: "integer"},
			{Name: "customer_id", Type: "integer"},
		},
	}

	schema := BuildParseContextSchema(asset, []ParseContextSchemaTable{
		{
			Name: "raw.customers",
			Columns: []ParseContextSchemaColumn{
				{Name: "id", Type: "integer"},
				{Name: "email", Type: "varchar"},
			},
		},
		{
			Name:    " ",
			Columns: []ParseContextSchemaColumn{{Name: "ignored", Type: "text"}},
		},
	})

	assert.Equal(t, map[string]string{"id": "integer", "email": "varchar"}, schema["raw.customers"])
	assert.Equal(t, map[string]string{"order_id": "integer", "customer_id": "integer"}, schema["marts.orders"])
	_, exists := schema[" "]
	assert.False(t, exists)
}

func TestBuildParseContextSchema_SkipsBlankColumnNames(t *testing.T) {
	t.Parallel()

	schema := BuildParseContextSchema(nil, []ParseContextSchemaTable{
		{
			Name: "raw.events",
			Columns: []ParseContextSchemaColumn{
				{Name: "", Type: "text"},
				{Name: "event_id", Type: "uuid"},
			},
		},
	})

	assert.Equal(t, map[string]string{"event_id": "uuid"}, schema["raw.events"])
}

func TestParseContextWithSchema_DuckDBPathReferenceDoesNotProduceUnresolvedTableDiagnostic(t *testing.T) {
	t.Parallel()

	parseContext, err := sqlintelligence.ParseContextWithSchema(
		`select * from "./customers.csv"`,
		"duckdb",
		sqlintelligence.Schema{
			"analytics.orders": {"order_id": "integer"},
		},
	)
	require.NoError(t, err)
	require.NotNil(t, parseContext)

	assert.Empty(t, parseContext.Errors)
	assert.Len(t, parseContext.Tables, 1)
	assert.Equal(t, "./customers.csv", parseContext.Tables[0].Name)
	assert.NotContains(t, parseContext.Diagnostics, sqlintelligence.ParseContextDiagnostic{
		Message:  "Unresolved table: ./customers.csv",
		Severity: "error",
	})

	for _, diagnostic := range parseContext.Diagnostics {
		assert.NotEqual(t, "Unresolved table: ./customers.csv", diagnostic.Message)
	}
}

func TestParseContextWithSchema_SelectAliasInOrderByDoesNotProduceUnresolvedColumnDiagnostic(t *testing.T) {
	t.Parallel()

	parseContext, err := sqlintelligence.ParseContextWithSchema(
		`SELECT
  customers.customer_id,
  customers.customer_name,
  customers.city,
  count(orders.order_id) AS order_count,
  sum(orders.order_total) AS total_revenue
FROM quickstart.customers AS customers
JOIN quickstart.orders AS orders
  ON customers.customer_id = orders.customer_id
GROUP BY 1, 2, 3
ORDER BY total_revenue DESC`,
		"duckdb",
		sqlintelligence.Schema{
			"quickstart.customers": {
				"customer_id":   "integer",
				"customer_name": "varchar",
				"city":          "varchar",
			},
			"quickstart.orders": {
				"order_id":    "integer",
				"customer_id": "integer",
				"order_total": "double",
			},
		},
	)
	require.NoError(t, err)
	require.NotNil(t, parseContext)

	assert.Empty(t, parseContext.Errors)
	for _, diagnostic := range parseContext.Diagnostics {
		assert.NotEqual(t, "Unresolved column: total_revenue", diagnostic.Message)
	}
}

func TestParseContextWithSchema_CTEAliasedColumnsResolveThroughOuterAlias(t *testing.T) {
	t.Parallel()

	parseContext, err := sqlintelligence.ParseContextWithSchema(
		`with cust as (
  SELECT
    customers.customer_id as id,
    customers.customer_name as name,
    customers.city as cit
  FROM quickstart.customers
)

SELECT
  city,
  cit,
  cust.cit,
  customers.id as id,
  customers.name as name,
  customers.cit as city,
  count(orders.order_id) AS order_count,
  sum(orders.order_total) AS total_revenue
FROM cust AS customers
JOIN quickstart.orders AS orders
  ON customers.id = orders.customer_id
GROUP BY 1, 2, 3
ORDER BY total_revenue DESC`,
		"duckdb",
		sqlintelligence.Schema{
			"quickstart.customers": {
				"customer_id":   "integer",
				"customer_name": "varchar",
				"city":          "varchar",
			},
			"quickstart.orders": {
				"order_id":    "integer",
				"customer_id": "integer",
				"order_total": "double",
			},
		},
	)
	require.NoError(t, err)
	require.NotNil(t, parseContext)

	assert.Empty(t, parseContext.Errors)
	for _, diagnostic := range parseContext.Diagnostics {
		assert.NotEqual(t, "Unresolved column: customers.id", diagnostic.Message)
		assert.NotEqual(t, "Unresolved column: customers.name", diagnostic.Message)
		assert.NotEqual(t, "Unresolved column: customers.cit", diagnostic.Message)
		assert.NotEqual(t, "Unresolved column: cit", diagnostic.Message)
	}
	foundCityDiagnostic := false
	foundShadowedCTEDiagnostic := false
	for _, diagnostic := range parseContext.Diagnostics {
		if diagnostic.Message == "Unresolved column: city" {
			foundCityDiagnostic = true
		}
		if diagnostic.Message == "Unresolved table or alias: cust" {
			foundShadowedCTEDiagnostic = true
		}
	}
	assert.True(t, foundCityDiagnostic)
	assert.True(t, foundShadowedCTEDiagnostic)

	var cteReference *sqlintelligence.ParseContextTable
	for index := range parseContext.Tables {
		if parseContext.Tables[index].Name == "cust" && parseContext.Tables[index].Alias == "customers" {
			cteReference = &parseContext.Tables[index]
			break
		}
	}
	require.NotNil(t, cteReference)
	assert.Equal(t, "cte", cteReference.SourceKind)
	assert.Equal(t, "cust", cteReference.ResolvedName)
	assert.ElementsMatch(t, []sqlintelligence.SchemaColumn{
		{Name: "id"},
		{Name: "name"},
		{Name: "cit"},
	}, cteReference.Columns)
}

func TestParseContextWithSchema_CTEColumnsFromJSONExpressionsResolveThroughJoinAlias(t *testing.T) {
	t.Parallel()

	parseContext, err := sqlintelligence.ParseContextWithSchema(
		`WITH game_results AS (
    SELECT
        CASE
            WHEN g.white->>'result' = 'win' THEN g.white->>'@id'
            WHEN g.black->>'result' = 'win' THEN g.black->>'@id'
            ELSE NULL
            END AS winner_aid,
        g.white->>'@id' AS white_aid,
        g.black->>'@id' AS black_aid
    FROM chess_playground.games g
)

SELECT
    p.username,
    p.aid,
    COUNT(*) AS total_games,
    COUNT(CASE WHEN g.white_aid = p.aid AND g.winner_aid = p.aid THEN 1 END) AS white_wins,
    COUNT(CASE WHEN g.black_aid = p.aid AND g.winner_aid = p.aid THEN 1 END) AS black_wins,
    COUNT(CASE WHEN g.white_aid = p.aid THEN 1 END) AS white_games,
    COUNT(CASE WHEN g.black_aid = p.aid THEN 1 END) AS black_games
FROM chess_playground.profiles p
LEFT JOIN game_results g
       ON p.aid IN (g.white_aid, g.black_aid)
GROUP BY p.username, p.aid
ORDER BY total_games DESC`,
		"duckdb",
		sqlintelligence.Schema{
			"chess_playground.games": {
				"white": "json",
				"black": "json",
			},
			"chess_playground.profiles": {
				"username": "varchar",
				"aid":      "varchar",
			},
		},
	)
	require.NoError(t, err)
	require.NotNil(t, parseContext)

	assert.Empty(t, parseContext.Errors)
	var gameResultsReference *sqlintelligence.ParseContextTable
	for index := range parseContext.Tables {
		if parseContext.Tables[index].Name == "game_results" && parseContext.Tables[index].Alias == "g" {
			gameResultsReference = &parseContext.Tables[index]
			break
		}
	}
	require.NotNil(t, gameResultsReference)
	assert.NotZero(t, gameResultsReference.ColumnRanges["white_aid"].Start)
	assert.NotZero(t, gameResultsReference.ColumnRanges["black_aid"].Start)
	assert.NotZero(t, gameResultsReference.ColumnRanges["winner_aid"].Start)

	for _, diagnostic := range parseContext.Diagnostics {
		assert.NotEqual(t, "Unresolved column: g.white_aid", diagnostic.Message)
		assert.NotEqual(t, "Unresolved column: g.black_aid", diagnostic.Message)
		assert.NotEqual(t, "Unresolved column: g.winner_aid", diagnostic.Message)
	}
}
