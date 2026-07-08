package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIAssetOpenAPIColumnsInferTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/openapi.yaml", r.URL.Path)
		_, _ = w.Write([]byte(`openapi: 3.0.3
paths:
  /player/{username}:
    get:
      responses:
        "200":
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: object
                    properties:
                      username:
                        type: string
                      rating:
                        type: integer
                      active:
                        type: boolean
`))
	}))
	defer server.Close()

	asset := &pipeline.Asset{
		Name: "quickstart.players",
		Type: pipeline.AssetType(apiAssetType),
		ExecutableFile: pipeline.ExecutableFile{Content: `type: api

parameters:
  openapi:
    url: ` + server.URL + `/openapi.yaml
  request:
    url: https://api.example.com/player/{{ username }}
    method: GET
  response:
    records_path: data
`},
	}

	columns := apiResponseFieldColumns(context.Background(), asset)
	require.Len(t, columns, 3)
	byName := map[string]string{}
	for _, column := range columns {
		byName[column.Name] = column.Type
	}
	assert.Equal(t, "boolean", byName["active"])
	assert.Equal(t, "integer", byName["rating"])
	assert.Equal(t, "string", byName["username"])
}

// An API asset's `parameters:` is a nested request/response spec, which bruin's
// stock YAML reader (parameters = map[string]string) can't parse. The api-aware
// creator must still load the workbench-managed fields (columns, owner, …) from
// the file so edits like dropping a column round-trip instead of being masked by
// fresh inference in the workspace preview.
func TestAPIAwareCreatorLoadsFileColumnsDespiteNestedParameters(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/ws/analytics/assets/weather.asset.yml"
	content := `type: api
parameters:
  request:
    url: https://api.weather.gov/alerts
    method: GET
  response:
    records_path: ".features"
owner: data@company.com
columns:
  - name: id
    type: string
  - name: geometry
    type: json
meta:
  renart_col_drop: geometry
`
	require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o644))

	asset, err := apiAwareYamlTaskCreator(fs)(path)
	require.NoError(t, err)

	names := make([]string, 0, len(asset.Columns))
	for _, column := range asset.Columns {
		names = append(names, column.Name)
	}
	assert.ElementsMatch(t, []string{"id", "geometry"}, names, "file columns must load through the nested parameters spec")
	assert.Equal(t, "data@company.com", asset.Owner)
}

// When an API asset carries no declared columns, the workspace preview falls back
// to spec inference — but a column the user explicitly dropped must not reappear.
func TestAPIInferredColumnsForDisplayRespectsDrops(t *testing.T) {
	asset := &pipeline.Asset{
		Type: pipeline.AssetType(apiAssetType),
		Meta: pipeline.EmptyStringMap{"renart_col_drop": "b"},
		ExecutableFile: pipeline.ExecutableFile{Content: `type: api
parameters:
  request:
    url: https://api.example.com/x
  response:
    fields:
      a: string
      b: string
      c: string
`},
	}

	names := make([]string, 0)
	for _, column := range apiInferredColumnsForDisplay(context.Background(), asset) {
		names = append(names, column.Name)
	}
	assert.ElementsMatch(t, []string{"a", "c"}, names, "dropped column must not reappear via inference fallback")
}

func TestWriteAPIAssetCSVValidatesResponseAgainstOpenAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi.yaml":
			_, _ = w.Write([]byte(`openapi: 3.0.3
paths:
  /player/{username}:
    get:
      responses:
        "200":
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: object
                    required: [username, rating]
                    properties:
                      username:
                        type: string
                      rating:
                        type: integer
`))
		case "/player/Hikaru":
			_, _ = w.Write([]byte(`{"data":{"username":"Hikaru","rating":"not-an-integer"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	spec := nativeAPISpec{
		OpenAPI:  nativeAPIOpenAPI{URL: server.URL + "/openapi.yaml"},
		Request:  nativeAPIRequest{URL: server.URL + "/player/Hikaru", Method: http.MethodGet},
		Response: nativeAPIResponse{RecordsPath: "data"},
	}
	_, err := writeAPIAssetCSV(context.Background(), jinja.NewRendererWithYesterday("quickstart", "test"), spec, filepath.Join(t.TempDir(), "players.csv"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api response does not match OpenAPI schema")
	assert.Contains(t, err.Error(), "$.data.rating expected integer")
}

func TestHybridBruinExecutorRunsAPIAssetThroughLoadWithBruinTargetConnection(t *testing.T) {
	workspaceRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	fakeUv := filepath.Join(workspaceRoot, "fake-uv")
	require.NoError(t, os.WriteFile(fakeUv, []byte("#!/bin/sh\nprintf 'uv %s\\n' \"$*\"\n"), 0o755))
	t.Setenv("RENART_UV_BINARY", fakeUv)
	t.Setenv("RENART_SLING_BINARY", "")
	t.Setenv("SLING_BINARY", "")
	t.Setenv("RENART_SLING_PACKAGE", "sling-test-package")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/player/Hikaru", r.URL.Path)
		_, _ = w.Write([]byte(`{"username":"Hikaru","name":"Hikaru Nakamura"}`))
	}))
	defer server.Close()

	pipelineRoot := filepath.Join(workspaceRoot, "quickstart")
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineRoot, "assets/quickstart"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".bruin.yml"), []byte("environments:\n  default:\n    connections:\n      duckdb:\n        - name: duckdb-default\n          path: duckdb-files/chess.duckdb\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: quickstart\ndefault_connections:\n  duckdb: duckdb-default\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "assets/quickstart/players.asset.yml"), []byte(`type: api

parameters:
  request:
    url: `+server.URL+`/player/{{ username }}
    method: GET

  iterate:
    as: username
    over:
      - Hikaru

  response:
    fields:
      username: username
      name: name
`), 0o644))

	executor := NewHybridBruinExecutor(
		workspaceRoot,
		"bruin",
		nil,
		func() *pipeline.Builder {
			return NewRenartPipelineBuilder(afero.NewOsFs())
		},
	)
	output, err := executor.RunAsset(context.Background(), RunAssetRequest{AssetPath: "quickstart/assets/quickstart/players.asset.yml", Environment: "default"}, nil)
	require.NoError(t, err)
	assert.Contains(t, string(output), "Fetched 1 records from API asset quickstart.players")
	assert.Contains(t, string(output), "uv tool run --no-config --python 3.11 --from sling-test-package sling run --src-stream file://")
	assert.Contains(t, string(output), "--tgt-conn duckdb:///")
	assert.Contains(t, string(output), "/duckdb-files/chess.duckdb")
	assert.Contains(t, string(output), "--tgt-object quickstart.players")
	assert.NotContains(t, string(output), "--mode full-refresh")
}
