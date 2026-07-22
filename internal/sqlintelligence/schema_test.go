package sqlintelligence

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPolyglotSchemaIncludesDeclaredConstraints(t *testing.T) {
	nullable := false
	schema := Schema{
		"users":  {"name": "VARCHAR", "id": "INTEGER"},
		"orders": {"user_id": "INTEGER", "id": "INTEGER"},
	}
	constraints := SchemaConstraints{
		"USERS": {Columns: map[string]SchemaColumnConstraints{
			"ID": {Nullable: &nullable, PrimaryKey: true},
		}},
		"orders": {Columns: map[string]SchemaColumnConstraints{
			"user_id": {ForeignKey: &SchemaColumnReference{Table: "users", Column: "id"}},
		}},
	}

	result := buildPolyglotSchema(schema, constraints)
	require.Len(t, result.Tables, 2)
	assert.Equal(t, "orders", result.Tables[0].Name, "tables should stay deterministic")
	assert.Equal(t, "users", result.Tables[1].Name)

	orders := result.Tables[0]
	require.Len(t, orders.ForeignKeys, 1)
	assert.Equal(t, []string{"user_id"}, orders.ForeignKeys[0].Columns)
	assert.Equal(t, "users", orders.ForeignKeys[0].References.Table)
	assert.Equal(t, []string{"id"}, orders.ForeignKeys[0].References.Columns)
	require.NotNil(t, orders.Columns[1].References)
	assert.Equal(t, polyglotColumnReference{Table: "users", Column: "id"}, *orders.Columns[1].References)

	users := result.Tables[1]
	assert.Equal(t, []string{"id"}, users.PrimaryKey)
	require.NotNil(t, users.Columns[0].Nullable)
	assert.False(t, *users.Columns[0].Nullable)
	assert.True(t, users.Columns[0].PrimaryKey)
}

func TestMarshalPolyglotSchemaIsDeterministicWithConstraints(t *testing.T) {
	left := SchemaConstraints{"t": {Columns: map[string]SchemaColumnConstraints{
		"b": {PrimaryKey: true},
		"a": {PrimaryKey: true},
	}}}
	right := SchemaConstraints{"t": {Columns: map[string]SchemaColumnConstraints{
		"a": {PrimaryKey: true},
		"b": {PrimaryKey: true},
	}}}
	schema := Schema{"t": {"b": "BIGINT", "a": "INTEGER"}}

	leftJSON, err := marshalPolyglotSchema(schema, left)
	require.NoError(t, err)
	rightJSON, err := marshalPolyglotSchema(schema, right)
	require.NoError(t, err)
	assert.Equal(t, string(leftJSON), string(rightJSON))
}
