package service

import "testing"

func TestParseLoadDiscoverStreamsJSON(t *testing.T) {
	// The `-o json` form sling emits, prefixed with an ANSI-coloured log line.
	output := "" +
		"\x1b[90m2:56AM\x1b[0m \x1b[31mWRN\x1b[0m could not parse DEBUGINFOD_URLS\n" +
		`{"fields":["#","Database","Schema","Name","Type"],"rows":[[1,"20631","main","smoke_users","table"],[2,"20631","public","orders","table"],[3,"20631","main","smoke_users","table"]]}` + "\n"

	streams := parseLoadDiscoverStreams(output)
	if len(streams) != 2 {
		t.Fatalf("expected 2 streams (deduped), got %d: %+v", len(streams), streams)
	}
	want := []struct{ name, schema string }{
		{"main.smoke_users", "main"},
		{"public.orders", "public"},
	}
	for i, w := range want {
		if streams[i].Name != w.name || streams[i].Schema != w.schema {
			t.Errorf("stream %d = %+v, want %v", i, streams[i], w)
		}
	}
}

func TestParseLoadDiscoverStreamsQualifiedName(t *testing.T) {
	// When Name is already schema-qualified, it is not double-prefixed.
	output := `{"fields":["Name"],"rows":[["analytics.users"]]}`
	streams := parseLoadDiscoverStreams(output)
	if len(streams) != 1 || streams[0].Name != "analytics.users" || streams[0].Schema != "analytics" {
		t.Fatalf("unexpected: %+v", streams)
	}
}

func TestParseLoadDiscoverStreamsEmpty(t *testing.T) {
	if got := parseLoadDiscoverStreams("6:24PM INF nothing here\n"); len(got) != 0 {
		t.Errorf("expected no streams, got %+v", got)
	}
}

func TestLoadFileStreamURI(t *testing.T) {
	cases := []struct{ root, path, want string }{
		{"/ws", "/abs/data.csv", "file:///abs/data.csv"},
		{"/ws", "data/in.csv", "file:///ws/data/in.csv"},
		{"/ws", "s3://bucket/key.csv", "s3://bucket/key.csv"},
		{"/ws", "file:///already.csv", "file:///already.csv"},
	}
	for _, c := range cases {
		if got := loadFileStreamURI(c.root, c.path); got != c.want {
			t.Errorf("loadFileStreamURI(%q,%q) = %q, want %q", c.root, c.path, got, c.want)
		}
	}
}

func TestLoadSourceTargetArgsLocalFile(t *testing.T) {
	executor := &HybridBruinExecutor{workspaceRoot: "/ws"}

	srcArgs, err := executor.loadSourceArgs(nil, loadRunParams{SourceConnection: "local", SourceTable: "data/in.csv"})
	if err != nil {
		t.Fatalf("local source: %v", err)
	}
	if len(srcArgs) != 2 || srcArgs[0] != "--src-stream" || srcArgs[1] != "file:///ws/data/in.csv" {
		t.Errorf("local source args = %v", srcArgs)
	}

	tgtArgs, err := executor.loadTargetArgs(nil, loadRunParams{DestinationConnection: "LOCAL", DestinationObject: "/out/result.csv"})
	if err != nil {
		t.Fatalf("local target: %v", err)
	}
	if len(tgtArgs) != 2 || tgtArgs[0] != "--tgt-object" || tgtArgs[1] != "file:///out/result.csv" {
		t.Errorf("local target args = %v", tgtArgs)
	}

	// A local source with no path is an error (no bruin connection to fall back on).
	if _, err := executor.loadSourceArgs(nil, loadRunParams{SourceConnection: "local"}); err == nil {
		t.Error("expected error for local source without a file path")
	}
}

func TestLoadConnectionCategory(t *testing.T) {
	cases := map[string]string{
		"postgres":  LoadCategoryDatabase,
		"Postgres":  LoadCategoryDatabase,
		"snowflake": LoadCategoryDatabase,
		"duckdb":    LoadCategoryDatabase,
		"s3":        LoadCategoryStorage,
		"gcs":       LoadCategoryStorage,
		"sftp":      LoadCategoryFile,
		"stripe":    "",
		"notion":    "",
		"":          "",
	}
	for connType, want := range cases {
		if got := loadConnectionCategory(connType); got != want {
			t.Errorf("loadConnectionCategory(%q) = %q, want %q", connType, got, want)
		}
	}
}
