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

func TestRenderLocalJSONUsesSharedReadOnlyService(t *testing.T) {
	root, assetPath := writeRenderCLIWorkspace(t)
	var output bytes.Buffer
	app := Root("test")
	app.Writer = &output

	err := app.Run(context.Background(), []string{
		"renart", "render", "--workspace", root, "--local", "--json",
		"--execution-time", "2026-07-16T12:00:00Z", "mart.orders",
	})
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	var result service.AssetRenderResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode render output: %v\n%s", err, output.String())
	}
	if result.Asset.Name != "mart.orders" || result.Status != service.AssetRenderStatusPartial {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Asset.Target.Kind != "unknown" || result.Asset.Target.Fidelity != service.AssetRenderFidelityRuntimeOnly {
		t.Fatalf("unexpected non-materialized target: %+v", result.Asset.Target)
	}
	if len(result.Stages) != 2 || result.Stages[0].Kind != "compiled_query" || result.Stages[1].Kind != "execution_sql" {
		t.Fatalf("unexpected stages: %+v", result.Stages)
	}
	if !strings.Contains(result.Stages[0].Content, "2026-07-16T12:00:00") {
		t.Fatalf("execution context was not rendered: %s", result.Stages[0].Content)
	}
	if got, err := resolveRenderAssetPath(context.Background(), root, root, assetPath); err != nil || got != "marts/assets/orders.sql" {
		t.Fatalf("path resolution = %q, %v", got, err)
	}
}

func TestRenderDelegatesToDiscoveredServer(t *testing.T) {
	root, _ := writeRenderCLIWorkspace(t)
	var renderedPath string
	var renderedRequest service.AssetRenderRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/health":
			fmt.Fprintf(w, `{"status":"ok","version":"test","workspace_root":%q}`, root)
		case strings.HasPrefix(r.URL.Path, "/api/assets/") && strings.HasSuffix(r.URL.Path, "/render"):
			renderedPath = r.URL.Path
			if err := json.NewDecoder(r.Body).Decode(&renderedRequest); err != nil {
				t.Errorf("decode request: %v", err)
			}
			fmt.Fprint(w, `{
                  "status":"ok",
                  "provenance":{"source":{"kind":"working_tree","pipeline_path":"marts/pipeline.yml","merkle_root":"abcdef012345"},"pipeline":"marts","context":{"environment":"production","start_date":"2026-07-15T00:00:00Z","end_date":"2026-07-16T00:00:00Z","execution_time":"2026-07-16T12:00:00Z","run_id":"renart-render-preview","requested_full_refresh":true,"full_refresh":true,"variables_digest":"vars","configuration_digest":"config"}},
                  "asset":{"name":"mart.orders","type":"duckdb.sql"},
                  "stages":[{"kind":"execution_sql","language":"sql","content":"SELECT 1","status":"ok","fidelity":"exact"}],
                  "issues":[],"redactions":[]
                }`)
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

	var output bytes.Buffer
	app := Root("test")
	app.Writer = &output
	err := app.Run(context.Background(), []string{
		"renart", "render", "--workspace", root, "--json", "--env", "production",
		"--full-refresh", "marts/assets/orders.sql",
	})
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	expectedID := service.EncodeID("marts/assets/orders.sql")
	if renderedPath != "/api/assets/"+expectedID+"/render" {
		t.Fatalf("rendered path = %q", renderedPath)
	}
	if renderedRequest.Environment != "production" || !renderedRequest.FullRefresh {
		t.Fatalf("render request = %+v", renderedRequest)
	}
	if !strings.Contains(output.String(), `"content": "SELECT 1"`) {
		t.Fatalf("delegated result was not printed: %s", output.String())
	}
}

func TestRenderSnapshotDelegatesThroughPipelineOwnedEndpoint(t *testing.T) {
	root, _ := writeRenderCLIWorkspace(t)
	var renderedPath string
	var renderedRequest service.PipelineAssetRenderRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/health":
			fmt.Fprintf(w, `{"status":"ok","version":"test","workspace_root":%q}`, root)
		case r.URL.Path == "/api/workspace":
			fmt.Fprintf(w, `{
				"root":%q,
				"pipelines":[{
					"id":"pipeline-id",
					"name":"marts",
					"path":"marts/pipeline.yml",
					"assets":[{
						"id":"asset-id",
						"name":"mart.orders",
						"type":"duckdb.sql",
						"path":"marts/assets/orders.sql"
					}]
				}]
			}`, root)
		case r.URL.Path == "/api/pipelines/pipeline-id/assets/render":
			renderedPath = r.URL.Path
			if err := json.NewDecoder(r.Body).Decode(&renderedRequest); err != nil {
				t.Errorf("decode request: %v", err)
			}
			fmt.Fprint(w, `{
				"status":"ok",
				"provenance":{"source":{"kind":"snapshot","pipeline_path":"marts/pipeline.yml","version_id":"snapshot-7","merkle_root":"abcdef012345"},"pipeline":"marts","context":{}},
				"asset":{"name":"mart.orders","type":"duckdb.sql"},
				"stages":[{"kind":"execution_sql","language":"sql","content":"SELECT 1","status":"ok","fidelity":"exact"}],
				"issues":[],"redactions":[]
			}`)
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

	var output bytes.Buffer
	app := Root("test")
	app.Writer = &output
	err := app.Run(context.Background(), []string{
		"renart", "render", "--workspace", root, "--json",
		"--snapshot", "snapshot-7", "mart.orders",
	})
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if renderedPath != "/api/pipelines/pipeline-id/assets/render" {
		t.Fatalf("rendered path = %q", renderedPath)
	}
	if renderedRequest.AssetName != "mart.orders" ||
		renderedRequest.Source.Kind != service.PipelinePlanSourceSnapshot ||
		renderedRequest.Source.VersionID != "snapshot-7" {
		t.Fatalf("render request = %+v", renderedRequest)
	}
	if !strings.Contains(output.String(), `"version_id": "snapshot-7"`) {
		t.Fatalf("snapshot result was not printed: %s", output.String())
	}
}

func TestResolveRenderAssetPathRejectsAmbiguousNames(t *testing.T) {
	root, _ := writeRenderCLIWorkspace(t)
	secondPipeline := filepath.Join(root, "other")
	mustWrite(t, filepath.Join(secondPipeline, "pipeline.yml"), "name: other\ndefault_connections:\n  duckdb: duckdb-default\n")
	mustWrite(t, filepath.Join(secondPipeline, "assets", "orders.sql"), `
/* @bruin
name: mart.orders
type: duckdb.sql
@bruin */
select 2
`)

	_, err := resolveRenderAssetPath(context.Background(), root, root, "mart.orders")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous-name error, got %v", err)
	}
}

func TestResolveRenderAssetPathRejectsAbsolutePathOutsideWorkspace(t *testing.T) {
	root, _ := writeRenderCLIWorkspace(t)
	outsidePath := filepath.Join(t.TempDir(), "outside.sql")
	mustWrite(t, outsidePath, "select 1\n")

	_, err := resolveRenderAssetPath(context.Background(), root, root, outsidePath)
	if err == nil || !strings.Contains(err.Error(), "outside the workspace") || strings.Contains(err.Error(), "no asset named") {
		t.Fatalf("expected an outside-workspace path error, got %v", err)
	}
}

func TestPrintAssetRenderResultUsesDistinctQualityCheckLabels(t *testing.T) {
	var output bytes.Buffer
	blocking := true
	printAssetRenderResult(&output, service.AssetRenderResult{
		Status: service.AssetRenderStatusOK,
		Provenance: service.AssetRenderProvenance{
			Source: service.AssetRenderSource{Kind: "working_tree", MerkleRoot: "abcdef012345"},
			Context: service.AssetRenderContext{
				StartDate: "2026-07-15T00:00:00Z",
				EndDate:   "2026-07-16T00:00:00Z",
			},
		},
		Asset: service.AssetRenderAsset{Name: "mart.orders", Type: "duckdb.sql"},
		Stages: []service.AssetRenderStage{
			{
				Kind:          "check",
				Label:         "order_id · not_null",
				Language:      "sql",
				Content:       "SELECT count(*) FROM mart.orders WHERE order_id IS NULL",
				Status:        service.AssetRenderStageStatusOK,
				Fidelity:      service.AssetRenderFidelityExact,
				CheckKind:     "column",
				CheckName:     "not_null",
				CheckColumn:   "order_id",
				CheckBlocking: &blocking,
			},
		},
		Issues:     []service.AssetRenderIssue{},
		Redactions: []service.AssetRenderRedaction{},
	})

	if !strings.Contains(output.String(), "order_id · not_null [exact]") {
		t.Fatalf("named check stage was not printed distinctly:\n%s", output.String())
	}
}

func TestPrintAssetRenderResultShowsValueFreeContextProvenance(t *testing.T) {
	var output bytes.Buffer
	printAssetRenderResult(&output, service.AssetRenderResult{
		Status: service.AssetRenderStatusOK,
		Provenance: service.AssetRenderProvenance{
			Source: service.AssetRenderSource{Kind: "working_tree", MerkleRoot: "abcdef012345"},
			Context: service.AssetRenderContext{
				Environment:           "production",
				StartDate:             "2026-07-15T00:00:00Z",
				EndDate:               "2026-07-16T00:00:00Z",
				ConfigurationDigest:   "configuration-digest",
				ConfigurationFidelity: "exact",
				VariablesDigest:       "variables-digest",
				VariableProvenance: []service.AssetRenderVariableProvenance{
					{Name: "region", Source: "pipeline_default"},
					{Name: "threshold", Source: "run_override"},
				},
			},
		},
		Asset: service.AssetRenderAsset{
			Name:        "mart.orders",
			Type:        "duckdb.sql",
			Fingerprint: "v2:asset-fingerprint",
			Target: service.AssetRenderTarget{
				Kind:     "relation",
				Object:   "mart.orders",
				Identity: "physical-target-digest",
				Fidelity: service.AssetRenderFidelityExact,
			},
		},
	})

	printed := output.String()
	if !strings.Contains(printed, "Configuration: configur [exact]") {
		t.Fatalf("configuration provenance missing: %s", printed)
	}
	if !strings.Contains(printed, "Variables: variable · region (pipeline default), threshold (run override)") {
		t.Fatalf("variable provenance missing: %s", printed)
	}
	if !strings.Contains(printed, "Asset fingerprint: v2:asset") {
		t.Fatalf("asset fingerprint missing: %s", printed)
	}
	if !strings.Contains(printed, "Target: mart.orders physical [exact]") {
		t.Fatalf("physical target identity missing: %s", printed)
	}
	if strings.Contains(printed, "secret-value") {
		t.Fatalf("variable values must not be printed: %s", printed)
	}
}

func writeRenderCLIWorkspace(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, ".bruin.yml"), `
default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: duckdb-default
          path: local.db
`)
	mustWrite(t, filepath.Join(root, "marts", "pipeline.yml"), `
name: marts
default_connections:
  duckdb: duckdb-default
`)
	assetPath := filepath.Join(root, "marts", "assets", "orders.sql")
	mustWrite(t, assetPath, `
/* @bruin
name: mart.orders
type: duckdb.sql
@bruin */
select '{{ execution_timestamp }}' as execution_time
`)
	return root, assetPath
}
