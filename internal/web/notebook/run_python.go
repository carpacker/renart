package notebook

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// runPython executes a Python cell. The cell's materialize() dataframe is
// written into a throwaway DuckDB file, then copied into the session as
// cell_<id> via ATTACH — this sidesteps a write-lock conflict that would arise
// if the Python operator tried to open the session file directly while the
// runner holds it open.
func (r *Runner) runPython(ctx context.Context, session *Session, nb *Notebook, cell *Cell, result CellRunResult, startedAt time.Time) CellRunResult {
	if strings.TrimSpace(cell.Asset.ExecutableFile.Content) == "" {
		result.Error = "cell is empty"
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}

	if r.PythonMaterializer == nil {
		result.Error = "Python cell execution is not available in this environment"
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}

	tmpDir, err := os.MkdirTemp("", "renart-nbpy-")
	if err != nil {
		result.Error = "failed to allocate Python workspace: " + err.Error()
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}
	defer os.RemoveAll(tmpDir)
	destPath := filepath.Join(tmpDir, "out.duckdb")
	const srcTable = "output"

	// Export the cell's upstream siblings into a DuckDB file under their logical
	// names, so the Python can read them (via RENART_NOTEBOOK_INPUTS).
	inputsPath, exportErr := r.exportUpstreamInputs(ctx, session, nb, cell, filepath.Join(tmpDir, "inputs.duckdb"))
	if exportErr != nil {
		result.Error = normalizeDuckDBError(exportErr)
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}

	logs, materializeErr := r.PythonMaterializer(ctx, cell, destPath, srcTable, inputsPath)
	result.Logs = logs
	if materializeErr != nil {
		result.Error = normalizePythonError(materializeErr)
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}

	object := quoteIdent(result.ObjectName)
	// Drop any existing object under this name (by its actual type) before
	// recreating it, mirroring the SQL path.
	if existingType, typeErr := session.objectType(ctx, result.ObjectName); typeErr == nil && existingType != "" {
		dropKind := "view"
		if existingType == "BASE TABLE" || existingType == "LOCAL TEMPORARY" {
			dropKind = "table"
		}
		if err := session.Exec(ctx, fmt.Sprintf("drop %s if exists %s", dropKind, object)); err != nil {
			result.Error = normalizeDuckDBError(err)
			result.DurationMS = time.Since(startedAt).Milliseconds()
			return result
		}
	}

	batch := fmt.Sprintf(
		"attach %s as __renart_py (read_only); create table %s as select * from __renart_py.main.%s; detach __renart_py;",
		sqlStringLiteral(destPath), object, quoteIdent(srcTable))
	if err := session.Exec(ctx, batch); err != nil {
		result.Error = normalizeDuckDBError(err)
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}
	result.Materialized = "table"

	preview, err := session.Query(ctx, fmt.Sprintf("select * from %s limit %d", object, r.previewLimit()))
	if err != nil {
		result.Error = normalizeDuckDBError(err)
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}
	result.Columns = preview.Columns
	result.Rows = normalizeRows(preview.Rows)

	if count, countErr := session.Query(ctx, fmt.Sprintf("select count(*) from %s", object)); countErr == nil && len(count.Rows) == 1 && len(count.Rows[0]) == 1 {
		result.TotalRows = toInt64(count.Rows[0][0])
	}

	result.Status = CellRunOK
	result.DurationMS = time.Since(startedAt).Milliseconds()
	return result
}

// exportUpstreamInputs writes the cell's upstream sibling tables into a DuckDB
// file under their logical names. Returns "" (no file) when the cell has no
// materialized upstream siblings. Siblings whose session object does not exist
// yet are skipped (the cell can still run; the Python just won't see them).
func (r *Runner) exportUpstreamInputs(ctx context.Context, session *Session, nb *Notebook, cell *Cell, inputsPath string) (string, error) {
	declared := map[string]struct{}{}
	for _, upstream := range cell.Asset.Upstreams {
		if upstream.Type == "asset" {
			declared[strings.ToLower(upstream.Value)] = struct{}{}
		}
	}

	statements := []string{fmt.Sprintf("attach %s as __inputs;", sqlStringLiteral(inputsPath))}
	exported := 0
	for _, sibling := range nb.Cells {
		if sibling.ID == cell.ID {
			continue
		}
		if _, ok := declared[strings.ToLower(sibling.Asset.Name)]; !ok {
			continue
		}
		objectType, _ := session.objectType(ctx, CellObjectName(sibling.ID))
		if objectType == "" {
			continue
		}
		statements = append(statements, fmt.Sprintf(
			"create or replace table __inputs.%s as select * from %s;",
			quoteIdent(sibling.Asset.Name), quoteIdent(CellObjectName(sibling.ID))))
		exported++
	}
	if exported == 0 {
		return "", nil
	}
	statements = append(statements, "detach __inputs;")
	if err := session.Exec(ctx, strings.Join(statements, " ")); err != nil {
		return "", err
	}
	return inputsPath, nil
}

func normalizePythonError(err error) string {
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "python cell failed"
	}
	return message
}
