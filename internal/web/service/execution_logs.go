package service

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/git"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/bruin-data/bruin/pkg/scheduler"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
)

type ExecutionLogSink interface {
	SaveQueryLog(context.Context, QueryLogRecord) error
	SaveRunLog(context.Context, RunLogRecord) error
}

type QueryLogRecord struct {
	Query               string
	QueryStartTimestamp time.Time
	Connection          string
	Result              *query.QueryResult
	Error               error
	Asset               string
	Environment         string
	Limit               int64
	Timeout             int
	Description         string
}

type RunLogRecord struct {
	Pipeline  *pipeline.Pipeline
	Scheduler *scheduler.Scheduler
	RunConfig *scheduler.RunConfig
	RunID     string
	Cmdline   []string
}

type NoopExecutionLogSink struct{}

func (NoopExecutionLogSink) SaveQueryLog(context.Context, QueryLogRecord) error { return nil }
func (NoopExecutionLogSink) SaveRunLog(context.Context, RunLogRecord) error     { return nil }

type BruinFileExecutionLogSink struct {
	workspaceRoot string
	fs            afero.Fs
	now           func() time.Time
}

func NewBruinFileExecutionLogSink(workspaceRoot string) *BruinFileExecutionLogSink {
	return &BruinFileExecutionLogSink{
		workspaceRoot: workspaceRoot,
		fs:            afero.NewOsFs(),
		now:           time.Now,
	}
}

func NewBruinFileExecutionLogSinkWithFS(workspaceRoot string, fs afero.Fs, now func() time.Time) *BruinFileExecutionLogSink {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	if now == nil {
		now = time.Now
	}
	return &BruinFileExecutionLogSink{workspaceRoot: workspaceRoot, fs: fs, now: now}
}

type bruinQueryLog struct {
	Query               string          `json:"query"`
	QueryStartTimestamp time.Time       `json:"query_start_timestamp"`
	Timestamp           time.Time       `json:"timestamp"`
	Connection          string          `json:"connection"`
	Success             bool            `json:"success"`
	Columns             []string        `json:"columns,omitempty"`
	Rows                [][]interface{} `json:"rows,omitempty"`
	Error               string          `json:"error,omitempty"`
	Asset               string          `json:"asset,omitempty"`
	Environment         string          `json:"environment,omitempty"`
	Limit               int64           `json:"limit,omitempty"`
	Timeout             int             `json:"timeout,omitempty"`
	Description         string          `json:"description,omitempty"`
}

func (s *BruinFileExecutionLogSink) SaveQueryLog(_ context.Context, record QueryLogRecord) error {
	if s == nil {
		return nil
	}
	if err := s.ensureGitignore("logs/queries"); err != nil {
		return errors.Wrap(err, "failed to add logs/queries to .gitignore")
	}

	timestamp := s.now()
	entry := bruinQueryLog{
		Query:               record.Query,
		QueryStartTimestamp: record.QueryStartTimestamp,
		Timestamp:           timestamp,
		Connection:          record.Connection,
		Success:             record.Error == nil,
		Asset:               record.Asset,
		Environment:         record.Environment,
		Limit:               record.Limit,
		Timeout:             record.Timeout,
		Description:         record.Description,
	}
	if record.Error != nil {
		entry.Error = record.Error.Error()
	} else if record.Result != nil {
		entry.Columns = record.Result.Columns
		entry.Rows = formatBruinQueryLogRowsForJSON(record.Result.Rows)
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return errors.Wrap(err, "failed to marshal query log to JSON")
	}

	logPath := filepath.Join(s.workspaceRoot, "logs", "queries", "query_"+formatUnixMilli(timestamp)+".json")
	if err := s.fs.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return errors.Wrap(err, "failed to create logs/queries directory")
	}
	if err := afero.WriteFile(s.fs, logPath, data, 0o600); err != nil {
		return errors.Wrap(err, "failed to write query log file")
	}
	return nil
}

func (s *BruinFileExecutionLogSink) SaveRunLog(_ context.Context, record RunLogRecord) error {
	if s == nil || record.Scheduler == nil || record.Pipeline == nil || record.RunConfig == nil || record.RunID == "" {
		return nil
	}
	if err := s.ensureGitignore("logs/runs"); err != nil {
		return errors.Wrap(err, "failed to add logs/runs to .gitignore")
	}
	statePath := filepath.Join(s.workspaceRoot, "logs", "runs", record.Pipeline.Name)
	return record.Scheduler.SavePipelineState(s.fs, record.Cmdline, record.RunConfig, record.RunID, statePath)
}

func (s *BruinFileExecutionLogSink) ensureGitignore(pattern string) error {
	if s == nil {
		return nil
	}
	if _, ok := s.fs.(*afero.OsFs); !ok {
		return nil
	}
	return git.EnsureGivenPatternIsInGitignore(s.fs, s.workspaceRoot, pattern)
}

func newRenartRunID() string {
	if runID := os.Getenv("BRUIN_RUN_ID"); runID != "" {
		return runID
	}
	return time.Now().Format("2006_01_02_15_04_05")
}

func formatUnixMilli(t time.Time) string {
	return strconv.FormatInt(t.UnixMilli(), 10)
}

func formatBruinQueryLogRowsForJSON(rows [][]interface{}) [][]interface{} {
	formattedRows := make([][]interface{}, len(rows))
	for rowIdx, row := range rows {
		formattedRow := make([]interface{}, len(row))
		for colIdx, cell := range row {
			formattedRow[colIdx] = formatQueryCellForJSON(cell)
		}
		formattedRows[rowIdx] = formattedRow
	}
	return formattedRows
}

func formatQueryCellForJSON(cell interface{}) interface{} {
	switch v := cell.(type) {
	case *big.Rat:
		if v == nil {
			return nil
		}
		return json.Number(formatBigRatAsDecimal(v))
	case big.Rat:
		vcopy := v
		return json.Number(formatBigRatAsDecimal(&vcopy))
	default:
		return cell
	}
}

func formatBigRatAsDecimal(rat *big.Rat) string {
	if rat == nil {
		return ""
	}
	return trimDecimalString(rat.FloatString(38))
}

func trimDecimalString(value string) string {
	if !strings.Contains(value, ".") {
		return value
	}
	trimmed := strings.TrimRight(strings.TrimRight(value, "0"), ".")
	if trimmed == "" || trimmed == "-" {
		return "0"
	}
	return trimmed
}
