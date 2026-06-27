package service

import "testing"

func TestParseSlingDiscoverStreamsJSON(t *testing.T) {
	// The `-o json` form sling emits, prefixed with an ANSI-coloured log line.
	output := "" +
		"\x1b[90m2:56AM\x1b[0m \x1b[31mWRN\x1b[0m could not parse DEBUGINFOD_URLS\n" +
		`{"fields":["#","Database","Schema","Name","Type"],"rows":[[1,"20631","main","smoke_users","table"],[2,"20631","public","orders","table"],[3,"20631","main","smoke_users","table"]]}` + "\n"

	streams := parseSlingDiscoverStreams(output)
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

func TestParseSlingDiscoverStreamsQualifiedName(t *testing.T) {
	// When Name is already schema-qualified, it is not double-prefixed.
	output := `{"fields":["Name"],"rows":[["analytics.users"]]}`
	streams := parseSlingDiscoverStreams(output)
	if len(streams) != 1 || streams[0].Name != "analytics.users" || streams[0].Schema != "analytics" {
		t.Fatalf("unexpected: %+v", streams)
	}
}

func TestParseSlingDiscoverStreamsEmpty(t *testing.T) {
	if got := parseSlingDiscoverStreams("6:24PM INF nothing here\n"); len(got) != 0 {
		t.Errorf("expected no streams, got %+v", got)
	}
}

func TestSlingFileStreamURI(t *testing.T) {
	cases := []struct{ root, path, want string }{
		{"/ws", "/abs/data.csv", "file:///abs/data.csv"},
		{"/ws", "data/in.csv", "file:///ws/data/in.csv"},
		{"/ws", "s3://bucket/key.csv", "s3://bucket/key.csv"},
		{"/ws", "file:///already.csv", "file:///already.csv"},
	}
	for _, c := range cases {
		if got := slingFileStreamURI(c.root, c.path); got != c.want {
			t.Errorf("slingFileStreamURI(%q,%q) = %q, want %q", c.root, c.path, got, c.want)
		}
	}
}

func TestSlingSourceTargetArgsLocalFile(t *testing.T) {
	executor := &HybridBruinExecutor{workspaceRoot: "/ws"}

	srcArgs, err := executor.slingSourceArgs(nil, slingRunParams{SourceConnection: "local", SourceTable: "data/in.csv"})
	if err != nil {
		t.Fatalf("local source: %v", err)
	}
	if len(srcArgs) != 2 || srcArgs[0] != "--src-stream" || srcArgs[1] != "file:///ws/data/in.csv" {
		t.Errorf("local source args = %v", srcArgs)
	}

	tgtArgs, err := executor.slingTargetArgs(nil, slingRunParams{DestinationConnection: "LOCAL", DestinationTable: "/out/result.csv"})
	if err != nil {
		t.Fatalf("local target: %v", err)
	}
	if len(tgtArgs) != 2 || tgtArgs[0] != "--tgt-object" || tgtArgs[1] != "file:///out/result.csv" {
		t.Errorf("local target args = %v", tgtArgs)
	}

	// A local source with no path is an error (no bruin connection to fall back on).
	if _, err := executor.slingSourceArgs(nil, slingRunParams{SourceConnection: "local"}); err == nil {
		t.Error("expected error for local source without a file path")
	}
}

func TestSlingConnectionCategory(t *testing.T) {
	cases := map[string]string{
		"postgres":  SlingCategoryDatabase,
		"Postgres":  SlingCategoryDatabase,
		"snowflake": SlingCategoryDatabase,
		"duckdb":    SlingCategoryDatabase,
		"s3":        SlingCategoryStorage,
		"gcs":       SlingCategoryStorage,
		"sftp":      SlingCategoryFile,
		"stripe":    "",
		"notion":    "",
		"":          "",
	}
	for connType, want := range cases {
		if got := slingConnectionCategory(connType); got != want {
			t.Errorf("slingConnectionCategory(%q) = %q, want %q", connType, got, want)
		}
	}
}
