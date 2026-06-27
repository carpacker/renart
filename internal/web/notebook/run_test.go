package notebook

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	duck "github.com/bruin-data/bruin/pkg/duckdb"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/bruin-data/bruin/pkg/sqlparser"
	"github.com/spf13/afero"
)

type stubFetcher struct {
	duckPath string
	columns  []string
	rows     [][]any
	fetches  int
}

func (s *stubFetcher) LocalDuckDBPath(_ context.Context, _ string) (string, bool) {
	return s.duckPath, s.duckPath != ""
}

func (s *stubFetcher) Fetch(_ context.Context, _ string, limit int) ([]string, [][]any, error) {
	s.fetches++
	rows := s.rows
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return s.columns, rows, nil
}

func realRenameTables(t *testing.T) RenameTablesFunc {
	t.Helper()
	parser, err := sqlparser.NewSQLParserCached()
	if err != nil {
		t.Fatalf("sql parser unavailable: %v", err)
	}
	t.Cleanup(func() { _ = parser.Close() })
	return func(sql, dialect string, mapping map[string]string) (string, error) {
		return parser.RenameTables(sql, dialect, mapping)
	}
}

func loadRunFixture(t *testing.T, files map[string]string) *Notebook {
	t.Helper()
	fs := afero.NewOsFs()
	dir := filepath.Join(t.TempDir(), "revenue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := afero.WriteFile(fs, filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	loader := NewLoader(fs, pipeline.CreateTaskFromFileComments(fs), fakeUsedTables)
	nb, err := loader.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return nb
}

func TestRunCellsExecutesDAGInSession(t *testing.T) {
	nb := loadRunFixture(t, map[string]string{
		ManifestFileName: "id: 11111111-0000-0000-0000-000000000001\nblocks:\n  - cell: aaaa1111\n  - cell: bbbb2222\n",
		"base.sql":       "/* @bruin\nid: aaaa1111\ntype: duckdb.sql\n@bruin */\nselect 1 as amount union all select 2 as amount\n",
		"doubled.sql":    "/* @bruin\nid: bbbb2222\ntype: duckdb.sql\n@bruin */\nselect amount * 2 as amount from base\n",
	})

	runner := &Runner{
		Store:        NewSessionStore(filepath.Join(t.TempDir(), "sessions")),
		RenameTables: realRenameTables(t),
	}

	results, err := runner.RunCells(context.Background(), nb, TopoOrder(nb), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, result := range results {
		if result.Status != CellRunOK {
			t.Fatalf("cell %s failed: %s", result.Name, result.Error)
		}
	}

	doubled := results[1]
	if doubled.Name != "doubled" {
		t.Fatalf("expected doubled second, got %q", doubled.Name)
	}
	if doubled.TotalRows != 2 || len(doubled.Rows) != 2 {
		t.Fatalf("unexpected doubled result: total=%d rows=%v", doubled.TotalRows, doubled.Rows)
	}
	if !strings.Contains(doubled.RewrittenSQL, "cell_aaaa1111") {
		t.Fatalf("sibling reference not rewritten to machine name: %q", doubled.RewrittenSQL)
	}
	if doubled.Materialized != "view" {
		t.Fatalf("expected view by default, got %q", doubled.Materialized)
	}
}

func TestRunCellsIsRepeatable(t *testing.T) {
	nb := loadRunFixture(t, map[string]string{
		ManifestFileName: "id: 11111111-0000-0000-0000-0000000000a1\nblocks:\n  - cell: aaaa1111\n",
		"greeting.sql":   "/* @bruin\nid: aaaa1111\ntype: duckdb.sql\n@bruin */\nselect 'hello' as greeting, 42 as answer\n",
	})

	runner := &Runner{
		Store:        NewSessionStore(filepath.Join(t.TempDir(), "sessions")),
		RenameTables: realRenameTables(t),
	}

	// Run the same cell three times — a view-backed cell must re-run
	// cleanly (DuckDB's typed DROP must not trip on the existing view).
	for attempt := 1; attempt <= 3; attempt++ {
		results, err := runner.RunCells(context.Background(), nb, TopoOrder(nb), RunOptions{})
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if results[0].Status != CellRunOK {
			t.Fatalf("attempt %d failed: %s", attempt, results[0].Error)
		}
		if results[0].TotalRows != 1 {
			t.Fatalf("attempt %d: unexpected rows %d", attempt, results[0].TotalRows)
		}
	}

	// Switching to @materialize(table) on a previously-view cell also works.
	if err := os.WriteFile(nb.Cells[0].Path, []byte("/* @bruin\nid: aaaa1111\ntype: duckdb.sql\n@bruin */\n-- @materialize(table)\nselect 'hello' as greeting\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reloaded := loadRunFixtureReload(t, nb)
	results, err := runner.RunCells(context.Background(), reloaded, TopoOrder(reloaded), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != CellRunOK || results[0].Materialized != "table" {
		t.Fatalf("view→table switch failed: %+v", results[0])
	}

	// Error results carry empty (non-null) columns/rows for the UI.
	broken := loadRunFixture(t, map[string]string{
		ManifestFileName: "id: 11111111-0000-0000-0000-0000000000a2\nblocks:\n  - cell: bbbb2222\n",
		"bad.sql":        "/* @bruin\nid: bbbb2222\ntype: duckdb.sql\n@bruin */\nselect * from nonexistent_thing_xyz\n",
	})
	brokenResults, err := runner.RunCells(context.Background(), broken, TopoOrder(broken), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if brokenResults[0].Status != CellRunError {
		t.Fatalf("expected error, got %+v", brokenResults[0])
	}
	if brokenResults[0].Columns == nil || brokenResults[0].Rows == nil {
		t.Fatalf("error result must have non-nil columns/rows: %+v", brokenResults[0])
	}
}

func loadRunFixtureReload(t *testing.T, nb *Notebook) *Notebook {
	t.Helper()
	fs := afero.NewOsFs()
	loader := NewLoader(fs, pipeline.CreateTaskFromFileComments(fs), fakeUsedTables)
	reloaded, err := loader.Load(nb.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return reloaded
}

func TestRunPythonCellCapturesLogs(t *testing.T) {
	pythonCell := "\"\"\" @bruin\nid: aaaa1111\nclass: notebook\ntype: python\n@bruin \"\"\"\n\n\ndef materialize():\n    return None\n"

	build := func() *Notebook {
		return loadRunFixture(t, map[string]string{
			ManifestFileName: "id: 11111111-0000-0000-0000-000000000009\nblocks:\n  - cell: aaaa1111\n",
			"analysis.py":    pythonCell,
		})
	}

	// Success path: the captured stdout/stderr rides along on the result.
	okRunner := &Runner{
		Store: NewSessionStore(filepath.Join(t.TempDir(), "sessions")),
		PythonMaterializer: func(ctx context.Context, _ *Cell, destPath, destTable, _ string) (string, error) {
			client, err := duck.NewClient(duck.Config{Path: destPath})
			if err != nil {
				return "", err
			}
			defer client.Close()
			if err := client.RunQueryWithoutResult(ctx, &query.Query{Query: fmt.Sprintf("create table %s as select 42 as answer", destTable)}); err != nil {
				return "", err
			}
			return "hello from stdout", nil
		},
	}
	okResults, err := okRunner.RunCells(context.Background(), build(), TopoOrder(build()), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if okResults[0].Status != CellRunOK {
		t.Fatalf("expected ok, got %+v", okResults[0])
	}
	if okResults[0].Logs != "hello from stdout" {
		t.Fatalf("expected captured logs, got %q", okResults[0].Logs)
	}

	// Failure path: logs are still attached so a traceback is visible.
	errRunner := &Runner{
		Store: NewSessionStore(filepath.Join(t.TempDir(), "sessions")),
		PythonMaterializer: func(context.Context, *Cell, string, string, string) (string, error) {
			return "partial output\nTraceback: boom", fmt.Errorf("python cell failed")
		},
	}
	errResults, err := errRunner.RunCells(context.Background(), build(), TopoOrder(build()), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if errResults[0].Status != CellRunError {
		t.Fatalf("expected error, got %+v", errResults[0])
	}
	if errResults[0].Logs != "partial output\nTraceback: boom" {
		t.Fatalf("expected logs on failure, got %q", errResults[0].Logs)
	}
}

func TestRunCellsBlocksDownstreamOfFailure(t *testing.T) {
	nb := loadRunFixture(t, map[string]string{
		ManifestFileName: "id: 11111111-0000-0000-0000-000000000002\nblocks:\n  - cell: aaaa1111\n  - cell: bbbb2222\n",
		"broken.sql":     "/* @bruin\nid: aaaa1111\ntype: duckdb.sql\n@bruin */\nselect * from this_table_does_not_exist\n",
		"child.sql":      "/* @bruin\nid: bbbb2222\ntype: duckdb.sql\n@bruin */\nselect count(*) from broken\n",
	})

	// "this_table_does_not_exist" resolves as an external ref; without a
	// fetcher the import fails, which is the error path we want.
	runner := &Runner{
		Store:        NewSessionStore(filepath.Join(t.TempDir(), "sessions")),
		RenameTables: realRenameTables(t),
	}

	results, err := runner.RunCells(context.Background(), nb, TopoOrder(nb), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != CellRunError {
		t.Fatalf("expected broken to error, got %+v", results[0])
	}
	if results[1].Status != CellRunBlocked {
		t.Fatalf("expected child blocked, got %+v", results[1])
	}
}

func TestRunCellsMaterializeDirectivePinsTable(t *testing.T) {
	nb := loadRunFixture(t, map[string]string{
		ManifestFileName: "id: 11111111-0000-0000-0000-000000000003\nblocks:\n  - cell: aaaa1111\n",
		"pinned.sql":     "/* @bruin\nid: aaaa1111\ntype: duckdb.sql\n@bruin */\n-- @materialize(table)\nselect 42 as answer\n",
	})

	store := NewSessionStore(filepath.Join(t.TempDir(), "sessions"))
	runner := &Runner{Store: store, RenameTables: realRenameTables(t)}

	results, err := runner.RunCells(context.Background(), nb, TopoOrder(nb), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != CellRunOK || results[0].Materialized != "table" {
		t.Fatalf("expected pinned table, got %+v", results[0])
	}

	// The object is a real table in the session DB.
	session, err := store.Open(nb.UUID)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.Query(context.Background(), "select table_type from information_schema.tables where table_name = 'cell_aaaa1111'")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || !strings.Contains(strings.ToUpper(fmt.Sprintf("%v", result.Rows[0][0])), "TABLE") {
		t.Fatalf("expected base table, got %v", result.Rows)
	}
}

func TestImportGenericFetchIsCachedAndCapped(t *testing.T) {
	nb := loadRunFixture(t, map[string]string{
		ManifestFileName: "id: 11111111-0000-0000-0000-000000000004\nblocks:\n  - cell: aaaa1111\n",
		"reader.sql":     "/* @bruin\nid: aaaa1111\ntype: duckdb.sql\n@bruin */\nselect sum(amount) as total from marts.orders\n",
	})

	fetcher := &stubFetcher{
		columns: []string{"id", "amount", "note"},
		rows: [][]any{
			{float64(1), float64(10.5), "a"},
			{float64(2), float64(20), nil},
			{float64(3), float64(30), "c"},
		},
	}
	runner := &Runner{
		Store:        NewSessionStore(filepath.Join(t.TempDir(), "sessions")),
		RenameTables: realRenameTables(t),
		Fetcher:      fetcher,
		ImportRowCap: 2,
	}

	results, err := runner.RunCells(context.Background(), nb, TopoOrder(nb), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != CellRunOK {
		t.Fatalf("run failed: %s", results[0].Error)
	}
	if len(results[0].Imports) != 1 {
		t.Fatalf("expected one import, got %+v", results[0].Imports)
	}
	imported := results[0].Imports[0]
	if imported.Complete {
		t.Fatalf("expected truncated import (cap 2), got %+v", imported)
	}
	if imported.RowCount != 2 {
		t.Fatalf("expected 2 imported rows, got %d", imported.RowCount)
	}
	// 10.5 + 20 = 30.5 over the capped rows.
	if fmt.Sprintf("%v", results[0].Rows[0][0]) != "30.5" {
		t.Fatalf("unexpected total: %v", results[0].Rows)
	}

	// Second run reuses the cache.
	if _, err := runner.RunCells(context.Background(), nb, TopoOrder(nb), RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if fetcher.fetches != 1 {
		t.Fatalf("expected cached import, fetches=%d", fetcher.fetches)
	}

	// RefreshImports forces a re-fetch.
	if _, err := runner.RunCells(context.Background(), nb, TopoOrder(nb), RunOptions{RefreshImports: true}); err != nil {
		t.Fatal(err)
	}
	if fetcher.fetches != 2 {
		t.Fatalf("expected refresh to re-fetch, fetches=%d", fetcher.fetches)
	}
}

func TestImportViaAttachFastPath(t *testing.T) {
	// Build a source DuckDB file that plays the role of the pipeline's
	// warehouse.
	sourcePath := filepath.Join(t.TempDir(), "warehouse.duckdb")
	source, err := duck.NewClient(duck.Config{Path: sourcePath})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := source.RunQueryWithoutResult(ctx, &query.Query{Query: "create schema marts; create table marts.orders as select 7 as amount union all select 8"}); err != nil {
		t.Fatal(err)
	}
	source.Close()

	nb := loadRunFixture(t, map[string]string{
		ManifestFileName: "id: 11111111-0000-0000-0000-000000000005\nblocks:\n  - cell: aaaa1111\n",
		"reader.sql":     "/* @bruin\nid: aaaa1111\ntype: duckdb.sql\n@bruin */\nselect sum(amount) as total from marts.orders\n",
	})

	fetcher := &stubFetcher{duckPath: sourcePath}
	runner := &Runner{
		Store:        NewSessionStore(filepath.Join(t.TempDir(), "sessions")),
		RenameTables: realRenameTables(t),
		Fetcher:      fetcher,
	}

	results, err := runner.RunCells(ctx, nb, TopoOrder(nb), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != CellRunOK {
		t.Fatalf("run failed: %s", results[0].Error)
	}
	if fetcher.fetches != 0 {
		t.Fatalf("expected attach fast path, but generic fetch ran %d times", fetcher.fetches)
	}
	if len(results[0].Imports) != 1 || !results[0].Imports[0].Complete || results[0].Imports[0].RowCount != 2 {
		t.Fatalf("unexpected import record: %+v", results[0].Imports)
	}
	if fmt.Sprintf("%v", results[0].Rows[0][0]) != "15" {
		t.Fatalf("unexpected total: %v", results[0].Rows)
	}
}

func TestSessionStoreRemoveAndSweep(t *testing.T) {
	store := NewSessionStore(filepath.Join(t.TempDir(), "sessions"))

	session, err := store.Open("nb-active")
	if err != nil {
		t.Fatal(err)
	}
	session.Close()
	session, err = store.Open("nb-deleted")
	if err != nil {
		t.Fatal(err)
	}
	session.Close()

	removed, err := store.Sweep(map[string]bool{"nb-active": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "nb-deleted" {
		t.Fatalf("unexpected sweep result: %v", removed)
	}
	if _, statErr := os.Stat(store.DBPath("nb-deleted")); !os.IsNotExist(statErr) {
		t.Fatal("deleted notebook session file still exists")
	}
	if _, statErr := os.Stat(store.DBPath("nb-active")); statErr != nil {
		t.Fatal("active notebook session file removed by sweep")
	}

	if err := store.Remove("nb-active"); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(store.DBPath("nb-active")); !os.IsNotExist(statErr) {
		t.Fatal("Remove left the session file behind")
	}
}
