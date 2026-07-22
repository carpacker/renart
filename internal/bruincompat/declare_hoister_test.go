package bruincompat

import (
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/require"
)

func TestDeclareHoister(t *testing.T) {
	t.Parallel()

	hoister := NewDeclareHoister()
	tests := []struct {
		name      string
		input     string
		assetType pipeline.AssetType
		want      string
		wantError bool
	}{
		{name: "no declare is a no-op", input: "SELECT 1; SELECT 2;", assetType: pipeline.AssetTypeBigqueryQuery, want: "SELECT 1; SELECT 2;"},
		{name: "already ordered is verbatim", input: "DECLARE x INT64;\nSELECT 1;", assetType: pipeline.AssetTypeBigqueryQuery, want: "DECLARE x INT64;\nSELECT 1;"},
		{name: "late declare", input: "SET x = 1;\nDECLARE y INT64;\nSELECT 1;", assetType: pipeline.AssetTypeBigqueryQuery, want: "DECLARE y INT64;\nSET x = 1;\nSELECT 1;"},
		{name: "declare in string", input: "SELECT 'declare bankruptcy' AS msg;", assetType: pipeline.AssetTypeBigqueryQuery, want: "SELECT 'declare bankruptcy' AS msg;"},
		{name: "semicolon in string", input: "SET separator = ';';\nDECLARE y INT64;", assetType: pipeline.AssetTypeBigqueryQuery, want: "DECLARE y INT64;\nSET separator = ';';"},
		{name: "declare in begin block", input: "SET x = 1;\nBEGIN\n  DECLARE y INT64;\n  SELECT y;\nEND;", assetType: pipeline.AssetTypeBigqueryQuery, want: "SET x = 1;\nBEGIN\n  DECLARE y INT64;\n  SELECT y;\nEND;"},
		{name: "case in begin block", input: "SET x = 1;\nBEGIN\n  SELECT CASE WHEN x>0 THEN 'a' ELSE 'b' END;\n  DECLARE y INT64;\nEND;", assetType: pipeline.AssetTypeBigqueryQuery, want: "SET x = 1;\nBEGIN\n  SELECT CASE WHEN x>0 THEN 'a' ELSE 'b' END;\n  DECLARE y INT64;\nEND;"},
		{name: "leading comment", input: "SET x = 1;\n-- setup\nDECLARE y INT64;", assetType: pipeline.AssetTypeBigqueryQuery, want: "-- setup\nDECLARE y INT64;\nSET x = 1;"},
		{name: "array casing", input: "SET x = 1;\nDECLARE distinct_keys array<STRING>;", assetType: pipeline.AssetTypeBigqueryQuery, want: "DECLARE distinct_keys array<STRING>;\nSET x = 1;"},
		{name: "begin transaction no-op", input: "DECLARE distinct_keys array<STRING>;\nBEGIN TRANSACTION;\nSELECT 1;\nCOMMIT TRANSACTION;", assetType: pipeline.AssetTypeBigqueryQuery, want: "DECLARE distinct_keys array<STRING>;\nBEGIN TRANSACTION;\nSELECT 1;\nCOMMIT TRANSACTION;"},
		{name: "hoist past begin transaction", input: "BEGIN TRANSACTION;\nSET x = 1;\nDECLARE y INT64;\nCOMMIT TRANSACTION;", assetType: pipeline.AssetTypeBigqueryQuery, want: "DECLARE y INT64;\nBEGIN TRANSACTION;\nSET x = 1;\nCOMMIT TRANSACTION;"},
		{name: "unsupported asset", input: "SET x = 1;\nDECLARE y INT64;", assetType: pipeline.AssetTypePython, want: "SET x = 1;\nDECLARE y INT64;", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := hoister.HoistDeclares(tt.input, tt.assetType)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDeclareHoisterList(t *testing.T) {
	t.Parallel()

	hoister := NewDeclareHoister()
	t.Run("no declare is a no-op", func(t *testing.T) {
		input := []string{"SELECT 1", "SELECT 2"}
		got, err := hoister.HoistDeclaresList(input, pipeline.AssetTypeBigqueryQuery)
		require.NoError(t, err)
		require.Equal(t, input, got)
	})
	t.Run("late declare is hoisted verbatim", func(t *testing.T) {
		input := []string{"SET x = 1", "DECLARE y INT64", "SELECT 1"}
		got, err := hoister.HoistDeclaresList(input, pipeline.AssetTypeBigqueryQuery)
		require.NoError(t, err)
		require.Equal(t, []string{"DECLARE y INT64", "SET x = 1", "SELECT 1"}, got)
	})
}
