package service

import (
	"context"
	"encoding/json"
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

func TestAPIAwareCreatorInfersAPIAssetWithoutExplicitType(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/ws/example/assets/example/another_asset.asset.yml"
	content := `parameters:
  request:
    url: https://api.weather.gov/alerts
    method: GET
  response:
    records_path: ""
  pagination:
    type: next_url
    max_pages: 10
`
	require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o644))

	asset, err := apiAwareYamlTaskCreator(fs)(path)
	require.NoError(t, err)
	require.NotNil(t, asset)
	assert.Equal(t, pipeline.AssetType(apiAssetType), asset.Type)
	assert.Equal(t, content, asset.ExecutableFile.Content)
}

func TestWorkspaceServiceLoadsAPIShapedAssetWithoutExplicitType(t *testing.T) {
	workspaceRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	pipelineRoot := filepath.Join(workspaceRoot, "example")
	assetsRoot := filepath.Join(pipelineRoot, "assets", "example")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: example\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "another_asset.asset.yml"), []byte(`parameters:
  request:
    url: https://api.weather.gov/alerts
    method: GET
  response:
    records_path: ""
  pagination:
    type: next_url
    max_pages: 10
`), 0o644))

	service := NewWorkspaceService(workspaceRoot, "")
	state, err := service.ComputeState(context.Background())
	require.NoError(t, err)
	assert.Empty(t, state.Errors)
	require.Len(t, state.Pipelines, 1)
	require.Len(t, state.Pipelines[0].Assets, 1)
	asset := state.Pipelines[0].Assets[0]
	assert.Equal(t, apiAssetType, asset.Type)
	assert.Equal(t, "example/assets/example/another_asset.asset.yml", asset.Path)
	assert.Empty(t, asset.ParseError)
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

func TestInferAPIAssetSamplesResponseColumnsAndRecordPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/search", r.URL.Path)
		_, _ = w.Write([]byte(`{"data":[{"id":1,"active":true,"created_at":"2026-07-09T08:00:00Z"}]}`))
	}))
	defer server.Close()

	workspaceRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "people.asset.yml"), []byte(`name: analytics.people
type: api

parameters:
  request:
    url: `+server.URL+`/search
  response:
    records_path: data
`), 0o644))

	resolver := NewWorkspaceResolver(workspaceRoot, func(ctx context.Context, pipelinePath string) (*pipeline.Pipeline, error) {
		return NewRenartPipelineBuilder(afero.NewOsFs()).CreatePipelineFromPath(ctx, pipelinePath, pipeline.WithMutate())
	})
	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:    workspaceRoot,
		ResolveAssetByID: resolver.ResolveAssetByID,
	})

	status, body, apiErr := service.InferAPIAsset(context.Background(), EncodeID("analytics/assets/people.asset.yml"))
	require.Nil(t, apiErr)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "data", body["records_path"])
	assert.Equal(t, 1, body["records_count"])

	paths := body["records_paths"].([]apiSampleRecordsPath)
	assert.True(t, containsSampleRecordsPath(paths, "data"))

	columns := body["columns"].([]WorkspaceColumn)
	byName := map[string]string{}
	for _, column := range columns {
		byName[column.Name] = column.Type
	}
	assert.Equal(t, "boolean", byName["active"])
	assert.Equal(t, "timestamp", byName["created_at"])
	assert.Equal(t, "integer", byName["id"])
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

func TestWriteAPIAssetCSVAllowsNullOptionalOpenAPIFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi.yaml":
			_, _ = w.Write([]byte(`openapi: 3.0.3
paths:
  /alerts:
    get:
      responses:
        "200":
          content:
            application/json:
              schema:
                type: object
                properties:
                  features:
                    type: array
                    items:
                      type: object
                      properties:
                        id:
                          type: string
                        properties:
                          type: object
                          properties:
                            description:
                              type: string
`))
		case "/alerts":
			_, _ = w.Write([]byte(`{"features":[{"id":"alert-1","properties":{"description":null}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	csvPath := filepath.Join(t.TempDir(), "alerts.csv")
	spec := nativeAPISpec{
		OpenAPI:  nativeAPIOpenAPI{URL: server.URL + "/openapi.yaml"},
		Request:  nativeAPIRequest{URL: server.URL + "/alerts", Method: http.MethodGet},
		Response: nativeAPIResponse{RecordsPath: "features"},
	}
	count, err := writeAPIAssetCSV(context.Background(), jinja.NewRendererWithYesterday("quickstart", "test"), spec, csvPath, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	content, err := os.ReadFile(csvPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "properties")
	assert.Contains(t, string(content), `""description"":null`)
}

func TestWriteAPIAssetCSVSupportsRequestBodyAndAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "true", r.URL.Query().Get("include"))

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "Ada", body["name"])

		_, _ = w.Write([]byte(`{"data":[{"id":1,"name":"Ada"}]}`))
	}))
	defer server.Close()

	spec := nativeAPISpec{
		Request: nativeAPIRequest{
			URL:    server.URL + "/search",
			Method: http.MethodPost,
			Params: map[string]any{"include": true},
			Body: map[string]any{
				"name": "{{ item }}",
			},
		},
		Iterate: nativeAPIIterate{Over: []string{"Ada"}},
		Auth:    nativeAPIAuth{Type: "bearer", Token: "test-token"},
		Response: nativeAPIResponse{
			RecordsPath: "data",
			Fields:      map[string]string{"id": "id", "name": "name"},
		},
	}

	csvPath := filepath.Join(t.TempDir(), "people.csv")
	count, err := writeAPIAssetCSV(context.Background(), jinja.NewRendererWithYesterday("quickstart", "test"), spec, csvPath, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	content, err := os.ReadFile(csvPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "id,name")
	assert.Contains(t, string(content), "1,Ada")
}

func TestWriteAPIAssetCSVPaginatesByPageNumber(t *testing.T) {
	requestedPages := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		requestedPages = append(requestedPages, page)
		switch page {
		case "1":
			_, _ = w.Write([]byte(`{"data":[{"id":1}],"pagination":{"has_next_page":true}}`))
		case "2":
			_, _ = w.Write([]byte(`{"data":[{"id":2}],"pagination":{"has_next_page":false}}`))
		default:
			http.Error(w, "unexpected page", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	spec := nativeAPISpec{
		Request:    nativeAPIRequest{URL: server.URL + "/items"},
		Response:   nativeAPIResponse{RecordsPath: "data", Fields: map[string]string{"id": "id"}},
		Pagination: nativeAPIPagination{Type: "page_number", PageParam: "page", StartPage: 1, HasMorePath: "pagination.has_next_page", MaxPages: 5},
	}

	csvPath := filepath.Join(t.TempDir(), "items.csv")
	count, err := writeAPIAssetCSV(context.Background(), jinja.NewRendererWithYesterday("quickstart", "test"), spec, csvPath, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, []string{"1", "2"}, requestedPages)
	content, err := os.ReadFile(csvPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "1")
	assert.Contains(t, string(content), "2")
}

func TestWriteAPIAssetCSVPaginatesByLinkHeader(t *testing.T) {
	serverURL := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/items" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Query().Get("page") {
		case "":
			w.Header().Set("Link", `<`+serverURL+`/items?page=2>; rel="next"`)
			_, _ = w.Write([]byte(`{"items":[{"id":1}]}`))
		case "2":
			_, _ = w.Write([]byte(`{"items":[{"id":2}]}`))
		default:
			http.Error(w, "unexpected page", http.StatusBadRequest)
		}
	}))
	serverURL = server.URL
	defer server.Close()

	spec := nativeAPISpec{
		Request:    nativeAPIRequest{URL: server.URL + "/items"},
		Response:   nativeAPIResponse{RecordsPath: "items", Fields: map[string]string{"id": "id"}},
		Pagination: nativeAPIPagination{Type: "next_url", NextURLHeader: "Link", MaxPages: 5},
	}

	csvPath := filepath.Join(t.TempDir(), "items.csv")
	count, err := writeAPIAssetCSV(context.Background(), jinja.NewRendererWithYesterday("quickstart", "test"), spec, csvPath, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	content, err := os.ReadFile(csvPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "1")
	assert.Contains(t, string(content), "2")
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

func containsSampleRecordsPath(paths []apiSampleRecordsPath, want string) bool {
	for _, path := range paths {
		if path.Path == want {
			return true
		}
	}
	return false
}
