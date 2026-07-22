package sqlintelligence

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsedTablesMatchesBruinCompatibilityPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dialect string
		query   string
		want    []string
	}{
		{name: "simple", dialect: "duckdb", query: "select * from table1", want: []string{"table1"}},
		{name: "join", dialect: "postgres", query: "select * from sales.orders o join sales.customers c on o.id = c.id", want: []string{"sales.customers", "sales.orders"}},
		{name: "CTE aliases excluded", dialect: "duckdb", query: "with orders as (select * from raw.orders), customers as (select * from dim.customers) select * from orders join customers on true", want: []string{"dim.customers", "raw.orders"}},
		{name: "nested CTE aliases excluded", dialect: "bigquery", query: "with first as (select * from RAW.EVENTS), second as (select * from first) select * from second", want: []string{"RAW.EVENTS"}},
		{name: "union", dialect: "duckdb", query: "select * from a union all select * from b", want: []string{"a", "b"}},
		{name: "BigQuery quoting", dialect: "bigquery", query: "select * from `project.dataset.table`", want: []string{"project.dataset.table"}},
		{name: "Postgres quoting", dialect: "postgres", query: `select * from "public"."users"`, want: []string{"public.users"}},
		{name: "BigQuery unnest is not a table", dialect: "bigquery", query: "select * from `p.d.events`, unnest(items)", want: []string{"p.d.events"}},
		{name: "DuckDB table function excluded", dialect: "duckdb", query: "select * from read_parquet('events.parquet')", want: []string{}},
		{name: "Postgres table function excluded", dialect: "postgres", query: "select * from generate_series(1, 10)", want: []string{}},
		{name: "TSQL USE qualifies tables", dialect: "tsql", query: "USE MY_DWH; SELECT * FROM table1; SELECT * FROM schema2.table1", want: []string{"MY_DWH.dbo.table1", "MY_DWH.schema2.table1"}},
		{name: "TSQL table hint", dialect: "tsql", query: "USE MY_DWH; SELECT * FROM schema.FactTable WITH (NOLOCK)", want: []string{"MY_DWH.schema.FactTable"}},
		{name: "TSQL legacy table hint", dialect: "tsql", query: "SELECT * FROM dbo.FactTable (NOLOCK)", want: []string{"dbo.FactTable"}},
		{name: "create table includes target", dialect: "postgres", query: "create table public.example as select * from raw.orders", want: []string{"public.example", "raw.orders"}},
		{name: "insert excludes target", dialect: "duckdb", query: "insert into target select * from raw.source", want: []string{"raw.source"}},
		{name: "update excludes target", dialect: "duckdb", query: "update target set x = source.x from source", want: []string{"source"}},
		{name: "delete excludes target and USING", dialect: "duckdb", query: "delete from target using source where target.x = source.x", want: []string{}},
		{name: "multiple statements", dialect: "duckdb", query: "select * from a; select * from b", want: []string{"a", "b"}},
		{name: "Dremio uses Trino dialect", dialect: "trino", query: `select * from "lakehouse"."space"."folder"."dataset"`, want: []string{"lakehouse.space.folder.dataset"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := UsedTables(tt.query, tt.dialect)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestUsedTablesReturnsParseErrors(t *testing.T) {
	t.Parallel()

	_, err := UsedTables("select * from", "duckdb")
	require.Error(t, err)
}
