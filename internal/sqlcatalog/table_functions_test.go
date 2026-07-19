package sqlcatalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDuckDBTableFunctionColumns(t *testing.T) {
	t.Parallel()

	rangeColumns, ok := DuckDBTableFunctionColumns(" RANGE ")
	require.True(t, ok)
	assert.Equal(t, []Column{{Name: "range", Type: "BIGINT"}}, rangeColumns)

	seriesColumns, ok := DuckDBTableFunctionColumns("generate_series")
	require.True(t, ok)
	assert.Equal(t, []Column{{Name: "generate_series", Type: "BIGINT"}}, seriesColumns)

	_, ok = DuckDBTableFunctionColumns("read_csv")
	assert.False(t, ok)

	rangeColumns[0].Type = "VARCHAR"
	freshColumns, ok := DuckDBTableFunctionColumns("range")
	require.True(t, ok)
	assert.Equal(t, "BIGINT", freshColumns[0].Type)
}
