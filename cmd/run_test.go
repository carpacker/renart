package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
