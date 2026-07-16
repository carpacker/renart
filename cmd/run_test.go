package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
	"renart/internal/clientapi"
	"renart/internal/web/model"
)

// TestRunClientMode drives `renart run` end to end against a fake server:
// discovery via .renart/server.json, workspace-based target resolution, the
// SSE stream, and the success exit path.
func TestRunClientMode(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/.bruin.yml", "environments: {}\n")

	var streamedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/health":
			fmt.Fprintf(w, `{"status":"ok","version":"test","workspace_root":%q}`, root)
		case r.URL.Path == "/api/workspace":
			fmt.Fprint(w, `{
				"pipelines": [{
					"id": "cGlwZQ", "name": "marts", "path": "marts",
					"assets": [{"id": "b3JkZXJz", "name": "mart.orders", "type": "duckdb.sql",
						"path": "marts/assets/orders.sql", "content": "", "upstreams": [], "is_materialized": false}]
				}],
				"connections": {}, "selected_environment": "default",
				"errors": [], "updated_at": "2026-07-11T00:00:00Z", "metadata": {}
			}`)
		case strings.HasSuffix(r.URL.Path, "/materialize/stream"):
			streamedPath = r.URL.Path
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: output\ndata: {\"chunk\":\"Running mart.orders\\n\"}\n\n")
			fmt.Fprint(w, "event: done\ndata: {\"status\":\"ok\",\"error\":\"\",\"exit_code\":0}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := clientapi.WriteServerFile(root, clientapi.ServerFile{
		PID: os.Getpid(), BaseURL: server.URL, APIBaseURL: server.URL + "/api",
		WorkspaceRoot: root, Version: "test", StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	err := Root("test").Run(context.Background(), []string{
		"renart", "run", "--workspace", root, "--quiet", "mart.orders",
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if streamedPath != "/api/assets/b3JkZXJz/materialize/stream" {
		t.Errorf("streamed wrong target: %q", streamedPath)
	}
}

func TestRunClientModeKeepsFullRefreshWindowOutOfBackfill(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/.bruin.yml", "environments: {}\n")

	var fullRefresh, backfill, startDate, endDate string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/health":
			fmt.Fprintf(w, `{"status":"ok","version":"test","workspace_root":%q}`, root)
		case r.URL.Path == "/api/workspace":
			fmt.Fprint(w, `{
				"pipelines": [{"id": "cGlwZQ", "name": "marts", "path": "marts", "assets": []}],
				"connections": {}, "selected_environment": "default",
				"errors": [], "updated_at": "2026-07-11T00:00:00Z", "metadata": {}
			}`)
		case strings.HasSuffix(r.URL.Path, "/materialize/stream"):
			fullRefresh = r.URL.Query().Get("full_refresh")
			backfill = r.URL.Query().Get("backfill")
			startDate = r.URL.Query().Get("start_date")
			endDate = r.URL.Query().Get("end_date")
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: done\ndata: {\"status\":\"ok\",\"error\":\"\",\"exit_code\":0}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := clientapi.WriteServerFile(root, clientapi.ServerFile{
		PID: os.Getpid(), BaseURL: server.URL, APIBaseURL: server.URL + "/api",
		WorkspaceRoot: root, Version: "test", StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	err := Root("test").Run(context.Background(), []string{
		"renart", "run", "--workspace", root, "--quiet", "marts", "--full-refresh",
		"--start-date", "2026-07-12T00:00:00Z", "--end-date", "2026-07-13T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if fullRefresh != "true" {
		t.Fatalf("expected full_refresh=true, got %q", fullRefresh)
	}
	if backfill != "" {
		t.Fatalf("expected a windowed full refresh not to set backfill, got %q", backfill)
	}
	if startDate != "2026-07-12T00:00:00Z" || endDate != "2026-07-13T00:00:00Z" {
		t.Fatalf("expected the full refresh window to be preserved, got %q - %q", startDate, endDate)
	}
}

func TestRunClientModeRefreshesStaleUpstreamsBeforeAsset(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/.bruin.yml", "environments: {}\n")

	var postPaths []string
	var upstreamOf, environment, refreshStart, refreshEnd, materializeBackfill string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/health":
			fmt.Fprintf(w, `{"status":"ok","version":"test","workspace_root":%q}`, root)
		case r.URL.Path == "/api/workspace":
			fmt.Fprint(w, `{
				"pipelines": [{
					"id": "cGlwZQ", "uuid": "pipeline-uuid", "name": "marts", "path": "marts",
					"assets": [
						{"id": "cmF3", "name": "raw.orders", "type": "duckdb.sql",
						 "path": "marts/assets/raw.sql", "content": "", "upstreams": [], "is_materialized": false},
						{"id": "b3JkZXJz", "name": "mart.orders", "type": "duckdb.sql",
						 "path": "marts/assets/orders.sql", "content": "", "upstreams": ["raw.orders"], "is_materialized": false}
					]
				}],
				"connections": {}, "selected_environment": "default",
				"errors": [], "updated_at": "2026-07-11T00:00:00Z", "metadata": {}
			}`)
		case strings.HasSuffix(r.URL.Path, "/build-stale/stream"):
			postPaths = append(postPaths, r.URL.Path)
			upstreamOf = r.URL.Query().Get("upstream_of")
			environment = r.URL.Query().Get("environment")
			refreshStart = r.URL.Query().Get("start")
			refreshEnd = r.URL.Query().Get("end")
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: output\ndata: {\"chunk\":\"Building raw.orders\\n\"}\n\n")
			fmt.Fprint(w, "event: done\ndata: {\"status\":\"ok\",\"error\":\"\",\"exit_code\":0}\n\n")
		case strings.HasSuffix(r.URL.Path, "/materialize/stream"):
			postPaths = append(postPaths, r.URL.Path)
			materializeBackfill = r.URL.Query().Get("backfill")
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: done\ndata: {\"status\":\"ok\",\"error\":\"\",\"exit_code\":0}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := clientapi.WriteServerFile(root, clientapi.ServerFile{
		PID: os.Getpid(), BaseURL: server.URL, APIBaseURL: server.URL + "/api",
		WorkspaceRoot: root, Version: "test", StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	err := Root("test").Run(context.Background(), []string{
		"renart", "run", "--workspace", root, "--quiet", "--refresh-upstreams",
		"--start-date", "2026-07-12T00:00:00Z", "--end-date", "2026-07-13T00:00:00Z",
		"mart.orders",
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	wantPaths := []string{
		"/api/pipelines/cGlwZQ/build-stale/stream",
		"/api/assets/b3JkZXJz/materialize/stream",
	}
	if !slices.Equal(postPaths, wantPaths) {
		t.Fatalf("expected upstream refresh before asset run %v, got %v", wantPaths, postPaths)
	}
	if upstreamOf != "mart.orders" {
		t.Fatalf("expected upstream_of=mart.orders, got %q", upstreamOf)
	}
	if environment != "default" {
		t.Fatalf("expected selected environment default, got %q", environment)
	}
	if refreshStart != "2026-07-12T00:00:00Z" || refreshEnd != "2026-07-13T00:00:00Z" {
		t.Fatalf("expected refresh window to match the asset run, got %q - %q", refreshStart, refreshEnd)
	}
	if materializeBackfill != "true" {
		t.Fatalf("expected the ordinary explicit asset window to remain a backfill, got %q", materializeBackfill)
	}
}

func TestRunClientModeStopsWhenUpstreamRefreshFails(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/.bruin.yml", "environments: {}\n")

	materializeRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/health":
			fmt.Fprintf(w, `{"status":"ok","version":"test","workspace_root":%q}`, root)
		case r.URL.Path == "/api/workspace":
			fmt.Fprint(w, `{
				"pipelines": [{
					"id": "cGlwZQ", "uuid": "pipeline-uuid", "name": "marts", "path": "marts",
					"assets": [{"id": "b3JkZXJz", "name": "mart.orders", "type": "duckdb.sql",
						"path": "marts/assets/orders.sql", "content": "", "upstreams": ["raw.orders"], "is_materialized": false}]
				}],
				"connections": {}, "selected_environment": "default",
				"errors": [], "updated_at": "2026-07-11T00:00:00Z", "metadata": {}
			}`)
		case strings.HasSuffix(r.URL.Path, "/build-stale/stream"):
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: done\ndata: {\"status\":\"error\",\"error\":\"raw.orders failed\",\"exit_code\":1,\"output\":\"upstream output\"}\n\n")
		case strings.HasSuffix(r.URL.Path, "/materialize/stream"):
			materializeRequests++
			http.Error(w, "target must not run", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := clientapi.WriteServerFile(root, clientapi.ServerFile{
		PID: os.Getpid(), BaseURL: server.URL, APIBaseURL: server.URL + "/api",
		WorkspaceRoot: root, Version: "test", StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	app := Root("test")
	exitCode := -1
	app.ExitErrHandler = func(_ context.Context, _ *cli.Command, err error) {
		var coder cli.ExitCoder
		if errors.As(err, &coder) {
			exitCode = coder.ExitCode()
		}
	}
	_ = app.Run(context.Background(), []string{
		"renart", "run", "--workspace", root, "--quiet", "--refresh-upstreams", "mart.orders",
	})
	if exitCode != 1 {
		t.Fatalf("expected upstream refresh failure to exit 1, got %d", exitCode)
	}
	if materializeRequests != 0 {
		t.Fatalf("expected target not to run after refresh failure, got %d requests", materializeRequests)
	}
}

// TestRunClientModeFailureExitCode asserts a failed run surfaces exit code 1.
func TestRunClientModeFailureExitCode(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/.bruin.yml", "environments: {}\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/health":
			fmt.Fprintf(w, `{"status":"ok","version":"test","workspace_root":%q}`, root)
		case r.URL.Path == "/api/workspace":
			fmt.Fprint(w, `{
				"pipelines": [{"id": "cGlwZQ", "name": "marts", "path": "marts", "assets": []}],
				"connections": {}, "selected_environment": "default",
				"errors": [], "updated_at": "2026-07-11T00:00:00Z", "metadata": {}
			}`)
		case strings.HasSuffix(r.URL.Path, "/materialize/stream"):
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: done\ndata: {\"status\":\"error\",\"error\":\"boom\",\"exit_code\":1}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := clientapi.WriteServerFile(root, clientapi.ServerFile{
		PID: os.Getpid(), BaseURL: server.URL, APIBaseURL: server.URL + "/api",
		WorkspaceRoot: root, Version: "test", StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	// urfave/cli's default handler os.Exit()s on ExitCoder errors; capture
	// the code instead so the test observes the contract (failed run → 1).
	app := Root("test")
	var exitCode = -1
	app.ExitErrHandler = func(_ context.Context, _ *cli.Command, err error) {
		var coder cli.ExitCoder
		if errors.As(err, &coder) {
			exitCode = coder.ExitCode()
		}
	}
	_ = app.Run(context.Background(), []string{
		"renart", "run", "--workspace", root, "--quiet", "marts",
	})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1 for a failed run, got %d", exitCode)
	}
}

func TestResolveRunTarget(t *testing.T) {
	state := model.WorkspaceState{Pipelines: []model.Pipeline{
		{ID: "p1", Name: "marts", Path: "marts", Assets: []model.Asset{
			{ID: "a1", Name: "mart.orders", Path: "marts/assets/orders.sql"},
			{ID: "a2", Name: "mart.customers", Path: "marts/assets/customers.sql"},
		}},
		{ID: "p2", Name: "raw", Path: "raw", Assets: []model.Asset{
			{ID: "a3", Name: "raw.orders", Path: "raw/assets/orders.sql"},
		}},
	}}
	root := "/ws"

	if got, err := resolveRunTarget(state, root, "marts", root); err != nil || got.kind != "pipeline" || got.pipeline.ID != "p1" {
		t.Errorf("pipeline by name: %+v, %v", got, err)
	}
	if got, err := resolveRunTarget(state, root, "mart.orders", root); err != nil || got.kind != "asset" || got.asset.ID != "a1" {
		t.Errorf("asset by name: %+v, %v", got, err)
	}
	if got, err := resolveRunTarget(state, root, "marts/assets/orders.sql", root); err != nil || got.asset.ID != "a1" {
		t.Errorf("asset by workspace-relative path: %+v, %v", got, err)
	}
	if got, err := resolveRunTarget(state, root, "assets/orders.sql", "/ws/marts"); err != nil || got.asset.ID != "a1" {
		t.Errorf("asset by cwd-relative path: %+v, %v", got, err)
	}
	if got, err := resolveRunTarget(state, root, "", "/ws/raw/assets"); err != nil || got.kind != "pipeline" || got.pipeline.ID != "p2" {
		t.Errorf("pipeline from cwd: %+v, %v", got, err)
	}
	if _, err := resolveRunTarget(state, root, "nope", root); err == nil {
		t.Error("expected error for unknown target")
	}
}
