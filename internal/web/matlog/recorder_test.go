package matlog

import (
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
)

func TestIntervalAwareRecognizesAPIExecutionWindows(t *testing.T) {
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
