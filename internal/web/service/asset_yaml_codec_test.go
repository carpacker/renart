package service

import (
	"strings"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

func TestPersistYAMLAssetDefinitionPreservesLoadConfigSibling(t *testing.T) {
	fs := afero.NewMemMapFs()
	definition := `name: example.thing
run: thing.sling.yml
type: load
`
	if err := afero.WriteFile(fs, "/p/assets/thing.asset.yml", []byte(definition), 0o644); err != nil {
		t.Fatal(err)
	}
	replication := "source: duckdb-default\ntarget: duckdb-default\nstreams:\n  s1:\n    object: x\n"
	if err := afero.WriteFile(fs, "/p/assets/thing.sling.yml", []byte(replication), 0o644); err != nil {
		t.Fatal(err)
	}

	asset := &pipeline.Asset{
		Name:           "example.thing",
		Type:           "load",
		DefinitionFile: pipeline.TaskDefinitionFile{Path: "/p/assets/thing.asset.yml"},
		ExecutableFile: pipeline.ExecutableFile{Path: "/p/assets/thing.sling.yml"},
		Upstreams:      []pipeline.Upstream{{Type: "asset", Value: "example.upstream", Mode: pipeline.UpstreamModeFull}},
		Materialization: pipeline.Materialization{
			Type:     pipeline.MaterializationTypeTable,
			Strategy: pipeline.MaterializationStrategyMerge,
		},
	}

	if err := persistYAMLAssetDefinition(fs, asset); err != nil {
		t.Fatalf("persist: %v", err)
	}

	// The replication config (the executable sibling) must be untouched.
	got, _ := afero.ReadFile(fs, "/p/assets/thing.sling.yml")
	if string(got) != replication {
		t.Errorf("sling replication config was modified:\n%s", got)
	}

	// The definition got the new managed fields and kept `run`.
	def, _ := afero.ReadFile(fs, "/p/assets/thing.asset.yml")
	var parsed map[string]any
	if err := yaml.Unmarshal(def, &parsed); err != nil {
		t.Fatalf("definition not valid yaml: %v", err)
	}
	if parsed["run"] != "thing.sling.yml" {
		t.Errorf("run pointer dropped:\n%s", def)
	}
	if _, ok := parsed["depends"]; !ok {
		t.Errorf("depends not written:\n%s", def)
	}
	if _, ok := parsed["materialization"]; !ok {
		t.Errorf("materialization not written:\n%s", def)
	}
}

func TestMergeYAMLAssetDefinitionPreservesUnmanagedKeys(t *testing.T) {
	// An API asset whose file carries a nested `parameters` spec, a `run`
	// pointer and an unknown key alongside renart-managed columns.
	existing := `name: example.my_api_asset_2
type: api
run: my_api_asset_2.api.yml
parameters:
  url: https://example.com/api
  response:
    fields:
      - id
      - name
custom_key: keep-me
columns:
  - name: id
    type: INTEGER
`
	asset := &pipeline.Asset{
		Name: "example.my_api_asset_2",
		Type: "api",
		ExecutableFile: pipeline.ExecutableFile{Path: "/x/my_api_asset_2.asset.yml"},
		Columns: []pipeline.Column{
			{Name: "id", Type: "INTEGER"},
			{Name: "email", Type: "VARCHAR"},
		},
		Owner: "data@example.com",
		Meta:  pipeline.EmptyStringMap{"renart_v": "1"},
	}

	merged, err := mergeYAMLAssetDefinition([]byte(existing), asset)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(merged, &parsed); err != nil {
		t.Fatalf("merged output not valid yaml: %v\n%s", err, merged)
	}

	// Unmanaged content survives untouched.
	if _, ok := parsed["parameters"]; !ok {
		t.Errorf("parameters spec was dropped:\n%s", merged)
	}
	if parsed["run"] != "my_api_asset_2.api.yml" {
		t.Errorf("run pointer was dropped, got %v:\n%s", parsed["run"], merged)
	}
	if parsed["custom_key"] != "keep-me" {
		t.Errorf("unknown key was dropped:\n%s", merged)
	}

	// Managed content reflects the asset.
	if parsed["owner"] != "data@example.com" {
		t.Errorf("owner not written: %v", parsed["owner"])
	}
	cols, ok := parsed["columns"].([]any)
	if !ok || len(cols) != 2 {
		t.Fatalf("expected 2 columns, got %v:\n%s", parsed["columns"], merged)
	}
}

func TestMergeYAMLAssetDefinitionManagesLoadFlatParameters(t *testing.T) {
	// A flat-parameter Load asset: renart owns `parameters`, so an edited
	// source_table must be written while unrelated keys survive.
	existing := `name: example.move_users
type: load
parameters:
  source_connection: postgres_prod
  source_table: public.users
  destination_connection: duckdb_default
  destination_table: public.users
  mode: full-refresh
custom_key: keep-me
`
	asset := &pipeline.Asset{
		Name:           "example.move_users",
		Type:           "load",
		ExecutableFile: pipeline.ExecutableFile{Path: "/x/move_users.asset.yml"},
		Parameters: pipeline.EmptyStringMap{
			"source_connection":      "postgres_prod",
			"source_table":           "public.customers", // edited
			"destination_connection": "duckdb_default",
			"destination_table":      "public.users",
			"mode":                   "incremental", // edited
		},
	}

	merged, err := mergeYAMLAssetDefinition([]byte(existing), asset)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	var parsed struct {
		Parameters map[string]string `yaml:"parameters"`
		CustomKey  string            `yaml:"custom_key"`
	}
	if err := yaml.Unmarshal(merged, &parsed); err != nil {
		t.Fatalf("invalid yaml: %v\n%s", err, merged)
	}
	if parsed.Parameters["source_table"] != "public.customers" {
		t.Errorf("source_table not updated: %q\n%s", parsed.Parameters["source_table"], merged)
	}
	if parsed.Parameters["mode"] != "incremental" {
		t.Errorf("mode not updated: %q", parsed.Parameters["mode"])
	}
	if parsed.CustomKey != "keep-me" {
		t.Errorf("unmanaged key dropped:\n%s", merged)
	}
}

func TestMergeYAMLAssetDefinitionClearsRemovedKeys(t *testing.T) {
	existing := `name: example.thing
type: load
run: thing.sling.yml
owner: old@example.com
tags:
  - daily
depends:
  - upstream.one
`
	// The asset no longer carries owner/tags/depends.
	asset := &pipeline.Asset{
		Name:           "example.thing",
		Type:           "load",
		ExecutableFile: pipeline.ExecutableFile{Path: "/x/thing.sling.yml"},
	}

	merged, err := mergeYAMLAssetDefinition([]byte(existing), asset)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(merged, &parsed); err != nil {
		t.Fatalf("invalid yaml: %v", err)
	}
	for _, key := range []string{"owner", "tags", "depends"} {
		if _, ok := parsed[key]; ok {
			t.Errorf("expected %q to be cleared:\n%s", key, merged)
		}
	}
	// The run pointer must survive even though it is unmanaged.
	if parsed["run"] != "thing.sling.yml" {
		t.Errorf("run pointer was dropped:\n%s", merged)
	}
	if !strings.Contains(string(merged), "name: example.thing") {
		t.Errorf("name missing:\n%s", merged)
	}
}
