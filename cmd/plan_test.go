package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"renart/internal/clientapi"
	"renart/internal/web/service"
)

func TestPlanDelegatesStructuredRequestToDiscoveredServer(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".bruin.yml"), "environments: {}\n")
	var plannedPath string
	var plannedRequest service.PipelinePlanRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/health":
			fmt.Fprintf(w, `{"status":"ok","version":"test","workspace_root":%q}`, root)
		case r.URL.Path == "/api/workspace":
			fmt.Fprint(w, `{
  "pipelines":[{"id":"cGlwZQ","uuid":"pipeline-uuid","name":"marts","path":"marts","assets":[
    {"id":"YXNzZXQ","name":"mart.orders","type":"duckdb.sql","path":"marts/assets/orders.sql","upstreams":[]}
  ]}],
  "connections":{},"selected_environment":"dev","errors":[],"updated_at":"2026-07-17T00:00:00Z","metadata":{}
}`)
		case strings.HasSuffix(r.URL.Path, "/plan"):
			plannedPath = r.URL.Path
			if err := json.NewDecoder(r.Body).Decode(&plannedRequest); err != nil {
				t.Errorf("decode request: %v", err)
			}
			fmt.Fprint(w, `{
  "id":"plan-id","status":"ready","pipeline_id":"cGlwZQ","pipeline_uuid":"pipeline-uuid","pipeline_name":"marts",
  "source":{"kind":"snapshot","version_id":"deployment-7","merkle_root":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
  "context":{"environment":"prod","start_date":"2026-07-16T00:00:00Z","end_date":"2026-07-17T00:00:00Z","execution_time":"2026-07-17T12:00:00Z","requested_full_refresh":false,"full_refresh":false,"backfill":false,"sensor_mode":"once","variables_digest":"vars","variable_provenance":[],"configuration_digest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","configuration_fidelity":"exact","destructive":false},
  "readiness":{"code_checks":{"assets":[],"summary":{}},"blockers":[],"warnings":[]},
  "selection":{"mode":"all"},"assets":[],"execution_units":[],
  "summary":{"assets":0,"execution_units":0,"stages":0,"destructive_operations":0,"blockers":0,"warnings":0}
}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	requirePlanServerFile(t, root, server.URL)

	var output bytes.Buffer
	app := Root("test")
	app.Writer = &output
	err := app.Run(context.Background(), []string{
		"renart", "plan", "--workspace", root, "--json", "--all", "--env", "prod",
		"--source", "snapshot", "--snapshot", "deployment-7",
		"--execution-time", "2026-07-17T12:00:00Z", "marts",
	})
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	if plannedPath != "/api/pipelines/cGlwZQ/plan" {
		t.Fatalf("planned path = %q", plannedPath)
	}
	if plannedRequest.Selection.Mode != service.PipelinePlanSelectionAll || plannedRequest.Source.VersionID != "deployment-7" {
		t.Fatalf("planned request = %+v", plannedRequest)
	}
	if plannedRequest.Environment != "prod" || plannedRequest.ExecutionTime != "2026-07-17T12:00:00Z" {
		t.Fatalf("planned context = %+v", plannedRequest)
	}
	if !strings.Contains(output.String(), `"id": "plan-id"`) {
		t.Fatalf("structured plan was not printed: %s", output.String())
	}
}

func TestPlanLocalUsesSharedPlanner(t *testing.T) {
	root, _ := writeRenderCLIWorkspace(t)
	mustWrite(t, filepath.Join(root, "marts", "pipeline.yml"), `
id: 11111111-1111-4111-8111-111111111111
name: marts
default_connections:
  duckdb: duckdb-default
`)
	var output bytes.Buffer
	app := Root("test")
	app.Writer = &output
	err := app.Run(context.Background(), []string{
		"renart", "plan", "--workspace", root, "--local", "--json", "--all",
		"--execution-time", "2026-07-17T12:00:00Z", "marts",
	})
	if err != nil {
		t.Fatalf("local plan failed: %v\n%s", err, output.String())
	}
	var plan service.PipelinePlan
	if err := json.Unmarshal(output.Bytes(), &plan); err != nil {
		t.Fatalf("decode plan: %v\n%s", err, output.String())
	}
	if plan.PipelineName != "marts" || plan.Selection.Mode != service.PipelinePlanSelectionAll {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if len(plan.Assets) != 1 || plan.Assets[0].Name != "mart.orders" {
		t.Fatalf("unexpected planned assets: %+v", plan.Assets)
	}
}

func TestPlanSelectionAndSourceValidation(t *testing.T) {
	pipelineTarget := runTarget{kind: "pipeline"}
	assetTarget := runTarget{kind: "asset"}
	assetTarget.asset.Name = "mart.orders"

	selection, err := planSelection(pipelineTarget, false, false, false, "", false)
	if err != nil || selection.Mode != service.PipelinePlanSelectionNeeded {
		t.Fatalf("needed selection = %+v, %v", selection, err)
	}
	selection, err = planSelection(assetTarget, false, true, true, "", false)
	if err != nil || selection.Scope != "asset_with_upstreams_and_downstreams" {
		t.Fatalf("asset selection = %+v, %v", selection, err)
	}
	if _, err := planSelection(pipelineTarget, false, true, false, "", false); err == nil {
		t.Fatal("pipeline upstream selection should fail")
	}
	selection, err = planSelection(pipelineTarget, false, false, false, "tag:daily", true)
	if err != nil || selection.Mode != service.PipelinePlanSelectionSelectorNeeded || selection.Selector != "tag:daily" {
		t.Fatalf("needed selector selection = %+v, %v", selection, err)
	}
	if _, err := planSelection(pipelineTarget, true, false, false, "tag:daily", false); err == nil {
		t.Fatal("--all with --selector should fail")
	}
	if _, err := planSelection(assetTarget, false, false, false, "tag:daily", false); err == nil {
		t.Fatal("asset target with --selector should fail")
	}
	source, err := planSource("snapshot", "version-1")
	if err != nil || source.Kind != service.PipelinePlanSourceSnapshot || source.VersionID != "version-1" {
		t.Fatalf("snapshot source = %+v, %v", source, err)
	}
	if _, err := planSource("working-tree", "version-1"); err == nil {
		t.Fatal("working-tree source with snapshot should fail")
	}
}

func requirePlanServerFile(t *testing.T, root, baseURL string) {
	t.Helper()
	if err := clientapi.WriteServerFile(root, clientapi.ServerFile{
		PID: os.Getpid(), BaseURL: baseURL, APIBaseURL: baseURL + "/api",
		WorkspaceRoot: root, Version: "test", StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}
