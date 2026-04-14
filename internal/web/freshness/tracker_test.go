package freshness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordMaterialization(t *testing.T) {
	tr := New()
	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	tr.RecordMaterialization("my_asset", ts, "succeeded")

	got := tr.Get("my_asset")
	require.NotNil(t, got)
	assert.Equal(t, ts, *got.MaterializedAt)
	assert.Equal(t, "succeeded", got.MaterializedStatus)
}

func TestRecordMaterialization_Overwrites(t *testing.T) {
	tr := New()
	ts1 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC)

	tr.RecordMaterialization("a", ts1, "failed")
	tr.RecordMaterialization("a", ts2, "succeeded")

	got := tr.Get("a")
	require.NotNil(t, got)
	assert.Equal(t, ts2, *got.MaterializedAt)
	assert.Equal(t, "succeeded", got.MaterializedStatus)
}

func TestGetAll(t *testing.T) {
	tr := New()
	ts := time.Now().UTC()

	tr.RecordMaterialization("a", ts, "succeeded")
	tr.RecordMaterialization("b", ts, "failed")

	all := tr.GetAll()
	assert.Len(t, all, 2)
	assert.Equal(t, "succeeded", all["a"].MaterializedStatus)
	assert.Equal(t, "failed", all["b"].MaterializedStatus)
}

func TestGet_Unknown(t *testing.T) {
	tr := New()
	assert.Nil(t, tr.Get("nonexistent"))
}

func TestLoadFromRunLogs(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs", "my-pipeline")
	require.NoError(t, os.MkdirAll(runsDir, 0o755))

	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	entry := map[string]any{
		"state": []map[string]string{
			{"name": "asset_a", "status": "succeeded"},
			{"name": "asset_b", "status": "failed"},
		},
		"timestamp": ts.Format(time.RFC3339Nano),
	}
	data, _ := json.Marshal(entry)
	require.NoError(t, os.WriteFile(filepath.Join(runsDir, "2025_06_01_12_00_00.json"), data, 0o644))

	tr := New()
	require.NoError(t, tr.LoadFromRunLogs(dir))

	a := tr.Get("asset_a")
	require.NotNil(t, a)
	assert.Equal(t, "succeeded", a.MaterializedStatus)
	assert.True(t, ts.Equal(*a.MaterializedAt))

	b := tr.Get("asset_b")
	require.NotNil(t, b)
	assert.Equal(t, "failed", b.MaterializedStatus)
}

func TestLoadFromRunLogs_LatestWins(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs", "pipe")
	require.NoError(t, os.MkdirAll(runsDir, 0o755))

	ts1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	write := func(name string, ts time.Time, status string) {
		entry := map[string]any{
			"state":     []map[string]string{{"name": "x", "status": status}},
			"timestamp": ts.Format(time.RFC3339Nano),
		}
		data, _ := json.Marshal(entry)
		require.NoError(t, os.WriteFile(filepath.Join(runsDir, name), data, 0o644))
	}

	write("2025_01_01.json", ts1, "failed")
	write("2025_06_01.json", ts2, "succeeded")

	tr := New()
	require.NoError(t, tr.LoadFromRunLogs(dir))

	x := tr.Get("x")
	require.NotNil(t, x)
	// The latest file is 2025_06_01 → status "succeeded"
	assert.Equal(t, "succeeded", x.MaterializedStatus)
}

func TestLoadFromRunLogs_MissingDir(t *testing.T) {
	tr := New()
	assert.NoError(t, tr.LoadFromRunLogs("/tmp/nonexistent-dir-1234567890"))
}

func TestRecordContentChange(t *testing.T) {
	tr := New()
	ts := time.Date(2025, 7, 1, 9, 0, 0, 0, time.UTC)

	tr.RecordContentChange("my_asset", ts)

	got := tr.Get("my_asset")
	require.NotNil(t, got)
	assert.Equal(t, ts, *got.ContentChangedAt)
	// Materialization fields unset.
	assert.Nil(t, got.MaterializedAt)
	assert.Empty(t, got.MaterializedStatus)
}

func TestRecordContentChange_CoexistsWithMaterialization(t *testing.T) {
	tr := New()
	matTS := time.Date(2025, 7, 1, 8, 0, 0, 0, time.UTC)
	contentTS := time.Date(2025, 7, 1, 9, 0, 0, 0, time.UTC)

	tr.RecordMaterialization("a", matTS, "succeeded")
	tr.RecordContentChange("a", contentTS)

	got := tr.Get("a")
	require.NotNil(t, got)
	assert.Equal(t, matTS, *got.MaterializedAt)
	assert.Equal(t, "succeeded", got.MaterializedStatus)
	assert.Equal(t, contentTS, *got.ContentChangedAt)
}
