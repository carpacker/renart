package matlog

import (
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
)

func TestIntervalAwareRecognizesAPIExecutionWindowsWithoutClaimingReplaySafety(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{
		Type: pipeline.AssetType("api"),
		ExecutableFile: pipeline.ExecutableFile{Content: `parameters:
  request:
    params:
      updated_since: "{{ start_timestamp }}"
      updated_before: "{{- end_timestamp }}"`},
	}

	assert.True(t, IntervalAware(asset))
	assert.False(t, BackfillSafe(asset))
}

func TestBackfillSafeRecognizesWindowedAPIMergeWithPrimaryKey(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{
		Type:    pipeline.AssetType("api"),
		Columns: []pipeline.Column{{Name: "id", PrimaryKey: true}},
		ExecutableFile: pipeline.ExecutableFile{Content: `parameters:
  request:
    params:
      updated_since: "{{ start_timestamp }}"`},
		Materialization: pipeline.Materialization{
			Type:     pipeline.MaterializationTypeTable,
			Strategy: pipeline.MaterializationStrategyMerge,
		},
	}

	assert.True(t, IntervalAware(asset))
	assert.True(t, BackfillSafe(asset))
}

func TestIntervalAwareDoesNotTreatAPIPaginationCursorAsExecutionWindow(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{
		Type: pipeline.AssetType("api"),
		ExecutableFile: pipeline.ExecutableFile{Content: `parameters:
  pagination:
    type: cursor
    cursor_path: additional_data.next_cursor`},
	}

	assert.False(t, IntervalAware(asset))
}

func TestIntervalAwareLeavesReplaceTableAssetsUnchanged(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{
		Type:           pipeline.AssetType("duckdb.sql"),
		ExecutableFile: pipeline.ExecutableFile{Content: `select '{{ start_timestamp }}'`},
	}

	assert.False(t, IntervalAware(asset))
}

func TestIntervalAwareUsesExecutionContractNotDormantKeys(t *testing.T) {
	t.Parallel()

	load := &pipeline.Asset{
		Type: pipeline.AssetType("load"),
		Materialization: pipeline.Materialization{
			Type:           pipeline.MaterializationTypeTable,
			Strategy:       pipeline.MaterializationStrategyAppend,
			IncrementalKey: "updated_at",
		},
	}
	assert.False(t, IntervalAware(load), "Sling max-key state is not the Renart run window")

	appendSQL := &pipeline.Asset{
		Type: pipeline.AssetTypeDuckDBQuery,
		Materialization: pipeline.Materialization{
			Type:           pipeline.MaterializationTypeTable,
			Strategy:       pipeline.MaterializationStrategyAppend,
			IncrementalKey: "updated_at",
		},
	}
	assert.False(t, IntervalAware(appendSQL), "append does not guarantee replay-safe windows")

	timeIntervalSQL := &pipeline.Asset{
		Type: pipeline.AssetTypeDuckDBQuery,
		Materialization: pipeline.Materialization{
			Type:     pipeline.MaterializationTypeTable,
			Strategy: pipeline.MaterializationStrategyTimeInterval,
		},
	}
	assert.True(t, IntervalAware(timeIntervalSQL))
	assert.True(t, BackfillSafe(timeIntervalSQL))
}
