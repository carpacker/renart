package pybroker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/bruin-data/bruin/pkg/query"

	"renart/internal/web/runstate"
)

func testResult() *query.QueryResult {
	return &query.QueryResult{
		Columns:     []string{"id", "name", "score", "won", "played_at"},
		ColumnTypes: []string{"BIGINT", "VARCHAR", "DOUBLE", "BOOLEAN", "TIMESTAMP"},
		Rows: [][]any{
			{int64(1), "magnus", 0.92, true, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
			{int64(2), "hikaru", nil, false, nil},
		},
	}
}

func startTestBroker(t *testing.T, cfg Config) *Broker {
	t.Helper()
	if cfg.RunQuery == nil {
		cfg.RunQuery = func(context.Context, string, string) (*query.QueryResult, error) {
			return testResult(), nil
		}
	}
	if cfg.DefaultConnection == "" {
		cfg.DefaultConnection = "duckdb-default"
	}
	broker, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	t.Cleanup(broker.Close)
	return broker
}

func brokerQuery(t *testing.T, broker *Broker, token, accept, sql string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"sql": sql})
	req, err := http.NewRequest(http.MethodPost, broker.URL+"/v1/query", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestQueryArrowRoundTrip(t *testing.T) {
	broker := startTestBroker(t, Config{})

	resp := brokerQuery(t, broker, broker.Token, "", "select * from chess_games")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/vnd.apache.arrow.stream" {
		t.Fatalf("unexpected content type %q", got)
	}

	reader, err := ipc.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("failed to open arrow stream: %v", err)
	}
	defer reader.Release()

	schema := reader.Schema()
	wantTypes := map[string]arrow.DataType{
		"id":        arrow.PrimitiveTypes.Int64,
		"name":      arrow.BinaryTypes.String,
		"score":     arrow.PrimitiveTypes.Float64,
		"won":       arrow.FixedWidthTypes.Boolean,
		"played_at": arrow.FixedWidthTypes.Timestamp_us,
	}
	for name, want := range wantTypes {
		fields, ok := schema.FieldsByName(name)
		if !ok || len(fields) != 1 {
			t.Fatalf("missing column %q in schema %v", name, schema)
		}
		if !arrow.TypeEqual(fields[0].Type, want) {
			t.Fatalf("column %q: got type %v, want %v", name, fields[0].Type, want)
		}
	}

	rows := int64(0)
	for reader.Next() {
		rows += reader.Record().NumRows()
	}
	if rows != 2 {
		t.Fatalf("expected 2 rows, got %d", rows)
	}
}

func TestQueryJSONFallbackAndAuth(t *testing.T) {
	broker := startTestBroker(t, Config{})

	if resp := brokerQuery(t, broker, "", "", "select 1"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing token must be rejected, got %d", resp.StatusCode)
	}
	if resp := brokerQuery(t, broker, "wrong", "", "select 1"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong token must be rejected, got %d", resp.StatusCode)
	}

	resp := brokerQuery(t, broker, broker.Token, "application/json", "select * from chess_games")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var payload struct {
		Columns []string `json:"columns"`
		Rows    [][]any  `json:"rows"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Columns) != 5 || len(payload.Rows) != 2 {
		t.Fatalf("unexpected payload %+v", payload)
	}
}

func TestQueryValidateSQLRejection(t *testing.T) {
	broker := startTestBroker(t, Config{
		ValidateSQL: func(sql string) error {
			if strings.Contains(strings.ToLower(sql), "drop") {
				return errors.New("only read-only queries are allowed")
			}
			return nil
		},
	})

	resp := brokerQuery(t, broker, broker.Token, "", "drop table chess_games")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not_read_only") {
		t.Fatalf("expected not_read_only error, got %s", body)
	}
}

func TestQueryWaitsForInFlightMaterialization(t *testing.T) {
	registry := runstate.NewRegistry()
	upstreamRun := registry.BeginRun("other-run", "default", []string{"chess_games"})
	defer upstreamRun.End()
	finish := upstreamRun.BeginTask("chess_games")

	executed := make(chan struct{}, 1)
	logs := &bytes.Buffer{}
	broker := startTestBroker(t, Config{
		Context:  ContextDocument{Asset: "player_stats", Environment: "default", RunID: "my-run"},
		Registry: registry,
		UsedTables: func(string) ([]string, error) {
			return []string{"chess_games"}, nil
		},
		RunQuery: func(context.Context, string, string) (*query.QueryResult, error) {
			executed <- struct{}{}
			return testResult(), nil
		},
		Log: logs,
	})

	done := make(chan *http.Response, 1)
	go func() {
		done <- brokerQuery(t, broker, broker.Token, "application/json", "select * from chess_games")
	}()

	select {
	case <-executed:
		t.Fatal("query must not execute while the upstream is in flight")
	case <-time.After(150 * time.Millisecond):
	}

	finish(nil)
	select {
	case resp := <-done:
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 after upstream finished, got %d", resp.StatusCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("query never completed after the upstream finished")
	}
	if !strings.Contains(logs.String(), "waiting for chess_games") {
		t.Fatalf("expected a waiting log line, got %q", logs.String())
	}
}

func TestQueryFailsWhenAwaitedUpstreamFails(t *testing.T) {
	registry := runstate.NewRegistry()
	upstreamRun := registry.BeginRun("other-run", "default", []string{"chess_games"})
	defer upstreamRun.End()
	finish := upstreamRun.BeginTask("chess_games")

	broker := startTestBroker(t, Config{
		Context:    ContextDocument{Asset: "player_stats", Environment: "default", RunID: "my-run"},
		Registry:   registry,
		UsedTables: func(string) ([]string, error) { return []string{"chess_games"}, nil },
	})

	go func() {
		time.Sleep(50 * time.Millisecond)
		finish(errors.New("upstream exploded"))
	}()

	resp := brokerQuery(t, broker, broker.Token, "", "select * from chess_games")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "upstream exploded") {
		t.Fatalf("expected the upstream error to surface, got %s", body)
	}
}

func TestQueryRejectsSameRunPendingReference(t *testing.T) {
	registry := runstate.NewRegistry()
	run := registry.BeginRun("my-run", "default", []string{"player_stats", "leaderboard"})
	defer run.End()
	// player_stats is running (that's us); leaderboard is planned later.
	finishSelf := run.BeginTask("player_stats")
	defer finishSelf(nil)

	broker := startTestBroker(t, Config{
		Context:    ContextDocument{Asset: "player_stats", Environment: "default", RunID: "my-run"},
		Registry:   registry,
		UsedTables: func(string) ([]string, error) { return []string{"leaderboard"}, nil },
	})

	resp := brokerQuery(t, broker, broker.Token, "", "select * from leaderboard")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "declare it in this asset's depends") {
		t.Fatalf("expected the ordering guidance, got %s", body)
	}
}

func TestUndeclaredDependencyLint(t *testing.T) {
	logs := &bytes.Buffer{}
	broker := startTestBroker(t, Config{
		Context:           ContextDocument{Asset: "player_stats", Environment: "default", RunID: "my-run"},
		KnownAssets:       []string{"chess_games", "player_stats"},
		DeclaredUpstreams: []string{},
		UsedTables: func(string) ([]string, error) {
			return []string{"chess_games", "main.chess_games", "not_an_asset"}, nil
		},
		Log: logs,
	})

	resp := brokerQuery(t, broker, broker.Token, "", "select * from chess_games")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(logs.String(), "without declaring it in depends") {
		t.Fatalf("expected the lint note, got %q", logs.String())
	}
	if strings.Contains(logs.String(), "not_an_asset") {
		t.Fatalf("non-asset tables must not be linted, got %q", logs.String())
	}
}

func TestContextEndpoint(t *testing.T) {
	start := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	broker := startTestBroker(t, Config{
		Context: ContextDocument{
			StartDate:   start,
			Pipeline:    "chess",
			Asset:       "player_stats",
			RunID:       "run-42",
			FullRefresh: true,
			Vars:        map[string]any{"region": "eu"},
		},
	})

	req, _ := http.NewRequest(http.MethodGet, broker.URL+"/v1/context", nil)
	req.Header.Set("Authorization", "Bearer "+broker.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var doc ContextDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if !doc.StartDate.Equal(start) || doc.Pipeline != "chess" || !doc.FullRefresh || doc.Vars["region"] != "eu" {
		t.Fatalf("unexpected context document %+v", doc)
	}
}

func TestNormalizeRef(t *testing.T) {
	cases := map[string]string{
		"Chess_Games":            "chess_games",
		`"main"."Chess_Games"`:   "main.chess_games",
		"db.main.chess_games":    "main.chess_games",
		" `main`.`chess_games` ": "main.chess_games",
	}
	for input, want := range cases {
		if got := normalizeRef(input); got != want {
			t.Errorf("normalizeRef(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestArrowZeroRowsCarriesSchema(t *testing.T) {
	var buf bytes.Buffer
	err := writeQueryResultArrow(&buf, &query.QueryResult{
		Columns:     []string{"id"},
		ColumnTypes: []string{"BIGINT"},
		Rows:        nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := ipc.NewReader(&buf)
	if err != nil {
		t.Fatalf("zero-row stream must still open: %v", err)
	}
	defer reader.Release()
	if len(reader.Schema().Fields()) != 1 {
		t.Fatalf("unexpected schema %v", reader.Schema())
	}
}

func TestArrowMixedColumnPromotesToString(t *testing.T) {
	var buf bytes.Buffer
	err := writeQueryResultArrow(&buf, &query.QueryResult{
		Columns: []string{"v"},
		Rows:    [][]any{{int64(1)}, {"two"}, {map[string]any{"k": "v"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := ipc.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Release()
	if !arrow.TypeEqual(reader.Schema().Field(0).Type, arrow.BinaryTypes.String) {
		t.Fatalf("mixed column must promote to string, got %v", reader.Schema().Field(0).Type)
	}
	if !reader.Next() {
		t.Fatal("expected one record batch")
	}
	col, ok := reader.Record().Column(0).(*array.String)
	if !ok {
		t.Fatalf("expected a string column, got %T", reader.Record().Column(0))
	}
	if got := col.Value(2); got != `{"k":"v"}` {
		t.Fatalf("structured value must be JSON-encoded, got %q", got)
	}
}
