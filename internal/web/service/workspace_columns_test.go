package service

import (
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceColumnConversionPreservesForeignKeys(t *testing.T) {
	nullable := false
	input := []pipeline.Column{{
		Name:       "user_id",
		Type:       "INTEGER",
		Nullable:   pipeline.DefaultTrueBool{Value: &nullable},
		PrimaryKey: true,
		ForeignKey: &pipeline.ColumnReference{Table: "analytics.users", Column: "id"},
	}}

	modelColumns := PipelineColumnsToModelColumns(input)
	require.Len(t, modelColumns, 1)
	require.NotNil(t, modelColumns[0].ForeignKey)
	assert.Equal(t, "analytics.users", modelColumns[0].ForeignKey.Table)
	assert.Equal(t, "id", modelColumns[0].ForeignKey.Column)

	roundTrip := ModelColumnsToPipelineColumns(modelColumns)
	require.Len(t, roundTrip, 1)
	require.NotNil(t, roundTrip[0].ForeignKey)
	assert.Equal(t, input[0].ForeignKey, roundTrip[0].ForeignKey)
	assert.Equal(t, input[0].PrimaryKey, roundTrip[0].PrimaryKey)
	require.NotNil(t, roundTrip[0].Nullable.Value)
	assert.False(t, *roundTrip[0].Nullable.Value)
}
