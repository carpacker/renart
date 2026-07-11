package service

import (
	"context"
	"encoding/json"
	"math/big"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/bruin-data/bruin/pkg/scheduler"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestBruinFileExecutionLogSinkWritesBruinCompatibleQueryLog(t *testing.T) {
	fs := afero.NewMemMapFs()
	workspaceRoot := "/repo"
	now := time.Date(2026, 5, 26, 12, 34, 56, 789000000, time.UTC)
	startedAt := now.Add(-2 * time.Second)
	sink := NewBruinFileExecutionLogSinkWithFS(workspaceRoot, fs, func() time.Time { return now })

	rat := big.NewRat(1234500, 10000)
	err := sink.SaveQueryLog(context.Background(), QueryLogRecord{
		Query:               "select * from users",
		QueryStartTimestamp: startedAt,
		Connection:          "duckdb-default",
		Result: &query.QueryResult{
			Columns: []string{"id", "amount"},
			Rows: [][]interface{}{
				{int64(1), rat},
			},
		},
		Asset:       "pipeline/assets/users.sql",
		Environment: "dev",
		Limit:       100,
	})
	require.NoError(t, err)

	data, err := afero.ReadFile(fs, filepath.Join(workspaceRoot, "logs", "queries", "query_1779798896789.json"))
	require.NoError(t, err)

	expected, err := json.MarshalIndent(bruinQueryLog{
		Query:               "select * from users",
		QueryStartTimestamp: startedAt,
		Timestamp:           now,
		Connection:          "duckdb-default",
		Success:             true,
		Columns:             []string{"id", "amount"},
		Rows: [][]interface{}{
			{int64(1), json.Number("123.45")},
		},
		Asset:       "pipeline/assets/users.sql",
		Environment: "dev",
		Limit:       100,
	}, "", "  ")
	require.NoError(t, err)
	assert.JSONEq(t, string(expected), string(data))
}

func TestBruinFileExecutionLogSinkWritesBruinCompatibleQueryErrorLog(t *testing.T) {
	fs := afero.NewMemMapFs()
	workspaceRoot := "/repo"
	now := time.Date(2026, 5, 26, 12, 34, 56, 789000000, time.UTC)
	sink := NewBruinFileExecutionLogSinkWithFS(workspaceRoot, fs, func() time.Time { return now })

	err := sink.SaveQueryLog(context.Background(), QueryLogRecord{
		Query:               "select * from missing",
		QueryStartTimestamp: now.Add(-time.Second),
		Connection:          "duckdb-default",
		Error:               assert.AnError,
		Environment:         "dev",
	})
	require.NoError(t, err)

	data, err := afero.ReadFile(fs, filepath.Join(workspaceRoot, "logs", "queries", "query_1779798896789.json"))
	require.NoError(t, err)

	var log bruinQueryLog
	require.NoError(t, json.Unmarshal(data, &log))
	assert.False(t, log.Success)
	assert.Equal(t, assert.AnError.Error(), log.Error)
	assert.Empty(t, log.Columns)
	assert.Empty(t, log.Rows)
}

func TestBruinFileExecutionLogSinkDelegatesRunLogsToBruinScheduler(t *testing.T) {
	fs := afero.NewMemMapFs()
	workspaceRoot := "/repo"
	foundPipeline := &pipeline.Pipeline{
		Name: "analytics",
		Assets: []*pipeline.Asset{
			{Name: "raw.users", Type: "duckdb.sql"},
			{Name: "mart.users", Type: "duckdb.sql"},
		},
	}
	runID := "2026_05_26_12_34_56"
	runConfig := &scheduler.RunConfig{Environment: "dev", Output: "plain"}
	cmdline := []string{"renart", "run", "analytics"}

	renartScheduler := scheduler.NewScheduler(zap.NewNop().Sugar(), foundPipeline, runID)
	renartScheduler.MarkAll(scheduler.Succeeded)
	sink := NewBruinFileExecutionLogSinkWithFS(workspaceRoot, fs, time.Now)
	require.NoError(t, sink.SaveRunLog(context.Background(), RunLogRecord{
		Pipeline:  foundPipeline,
		Scheduler: renartScheduler,
		RunConfig: runConfig,
		RunID:     runID,
		Cmdline:   cmdline,
	}))

	bruinScheduler := scheduler.NewScheduler(zap.NewNop().Sugar(), foundPipeline, runID)
	bruinScheduler.MarkAll(scheduler.Succeeded)
	bruinStatePath := filepath.Join(workspaceRoot, "expected", "logs", "runs", foundPipeline.Name)
	require.NoError(t, bruinScheduler.SavePipelineState(fs, cmdline, runConfig, "", 0, runID, bruinStatePath))

	renartState := readPipelineStateForTest(t, fs, filepath.Join(workspaceRoot, "logs", "runs", foundPipeline.Name, runID+".json"))
	bruinState := readPipelineStateForTest(t, fs, filepath.Join(bruinStatePath, runID+".json"))
	normalizePipelineStateForTest(renartState)
	normalizePipelineStateForTest(bruinState)
	assert.Equal(t, bruinState, renartState)
}

func readPipelineStateForTest(t *testing.T, fs afero.Fs, path string) *scheduler.PipelineState {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	var state scheduler.PipelineState
	require.NoError(t, json.Unmarshal(data, &state))
	return &state
}

func normalizePipelineStateForTest(state *scheduler.PipelineState) {
	state.TimeStamp = time.Time{}
	sort.Slice(state.State, func(i, j int) bool {
		return state.State[i].Name < state.State[j].Name
	})
}
