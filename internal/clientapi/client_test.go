package clientapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiscoverNoFile(t *testing.T) {
	client, err := Discover(context.Background(), t.TempDir())
	if client != nil || err != nil {
		t.Fatalf("expected (nil, nil) without a server.json, got (%v, %v)", client, err)
	}
}

func TestDiscoverStaleFile(t *testing.T) {
	root := t.TempDir()
	// A server that no longer exists: nothing listens on the recorded port.
	if err := WriteServerFile(root, ServerFile{
		PID:           999999,
		BaseURL:       "http://127.0.0.1:1",
		APIBaseURL:    "http://127.0.0.1:1/api",
		WorkspaceRoot: root,
		StartedAt:     time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	client, err := Discover(context.Background(), root)
	if client != nil {
		t.Fatal("expected no client for a stale server.json")
	}
	if err == nil {
		t.Fatal("expected a diagnostic error for a stale server.json")
	}
	// Falling back to embedded mode must be fast.
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("discovery took %s; must fail fast on a dead server", elapsed)
	}
}

func TestDiscoverAndStream(t *testing.T) {
	root := t.TempDir()

	var sawToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/health":
			fmt.Fprintf(w, `{"status":"ok","version":"9.9.9","workspace_root":%q,"project_id":"p1"}`, root)
		case r.URL.Path == "/api/workspace":
			fmt.Fprint(w, `{"pipelines":[{"id":"cGlwZQ","name":"marts","path":"marts","assets":[]}],"connections":{},"selected_environment":"default","errors":[],"updated_at":"2026-07-11T00:00:00Z","metadata":{}}`)
		case strings.HasPrefix(r.URL.Path, "/api/assets/") && strings.HasSuffix(r.URL.Path, "/materialize/stream"):
			sawToken = r.Header.Get("Authorization")
			if r.URL.Query().Get("scope") != "asset" {
				t.Errorf("scope not forwarded: %q", r.URL.Query().Get("scope"))
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: start\ndata: {\"operation\":{}}\n\n")
			fmt.Fprint(w, "event: output\ndata: {\"chunk\":\"line one\\n\"}\n\n")
			fmt.Fprint(w, "event: output\ndata: {\"chunk\":\"line two\\n\"}\n\n")
			fmt.Fprint(w, "event: done\ndata: {\"status\":\"ok\",\"error\":\"\",\"exit_code\":0}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := WriteServerFile(root, ServerFile{
		PID:           1,
		BaseURL:       server.URL,
		APIBaseURL:    server.URL + "/api",
		WorkspaceRoot: root,
		Token:         "sekrit",
		StartedAt:     time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	client, err := Discover(context.Background(), root)
	if err != nil || client == nil {
		t.Fatalf("Discover: (%v, %v)", client, err)
	}
	if client.ServerVersion != "9.9.9" {
		t.Errorf("ServerVersion = %q", client.ServerVersion)
	}

	state, err := client.Workspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Pipelines) != 1 || state.Pipelines[0].Name != "marts" {
		t.Errorf("workspace not decoded: %+v", state.Pipelines)
	}

	var chunks []string
	done, err := client.MaterializeAssetStream(context.Background(), "YXNzZXQ", mustValues("scope=asset"), func(chunk string) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "ok" {
		t.Errorf("done.Status = %q", done.Status)
	}
	if len(chunks) != 2 || chunks[0] != "line one\n" {
		t.Errorf("chunks = %q", chunks)
	}
	if sawToken != "Bearer sekrit" {
		t.Errorf("token not sent: %q", sawToken)
	}
}

func TestDiscoverRejectsWrongWorkspace(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"status":"ok","version":"1","workspace_root":%q}`, other)
	}))
	defer server.Close()

	if err := WriteServerFile(root, ServerFile{
		PID: 1, BaseURL: server.URL, APIBaseURL: server.URL + "/api",
		WorkspaceRoot: root, StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	client, err := Discover(context.Background(), root)
	if client != nil {
		t.Fatal("must not delegate to a server serving a different workspace")
	}
	if err == nil || !strings.Contains(err.Error(), "serves") {
		t.Errorf("unexpected error: %v", err)
	}
}

func mustValues(encoded string) map[string][]string {
	values := map[string][]string{}
	for _, pair := range strings.Split(encoded, "&") {
		key, value, _ := strings.Cut(pair, "=")
		values[key] = append(values[key], value)
	}
	return values
}
