package service

import (
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
)

func TestResolveSlingSourceAssetBySourceTable(t *testing.T) {
	source := &pipeline.Asset{
		Name:    "raw.users",
		Columns: []pipeline.Column{{Name: "id", Type: "INTEGER"}, {Name: "email", Type: "VARCHAR"}},
	}
	other := &pipeline.Asset{Name: "raw.orders"}
	sling := &pipeline.Asset{
		Name:       "staging.users",
		Type:       "sling",
		Parameters: pipeline.EmptyStringMap{"source_table": "raw.users"},
	}
	pl := &pipeline.Pipeline{Assets: []*pipeline.Asset{source, other, sling}}

	got := resolveSlingSourceAsset(pl, sling)
	if got != source {
		t.Fatalf("expected source raw.users, got %+v", got)
	}
}

func TestResolveSlingSourceAssetBySingleUpstream(t *testing.T) {
	source := &pipeline.Asset{
		Name:    "raw.events",
		Columns: []pipeline.Column{{Name: "ts", Type: "TIMESTAMP"}},
	}
	sling := &pipeline.Asset{
		Name:      "staging.events",
		Type:      "sling",
		Upstreams: []pipeline.Upstream{{Type: "asset", Value: "raw.events"}},
	}
	pl := &pipeline.Pipeline{Assets: []*pipeline.Asset{source, sling}}

	if got := resolveSlingSourceAsset(pl, sling); got != source {
		t.Fatalf("expected source raw.events, got %+v", got)
	}
}

func TestInferSlingColumnsFromUpstream(t *testing.T) {
	source := &pipeline.Asset{
		Name:    "raw.users",
		Columns: []pipeline.Column{{Name: "id", Type: "INTEGER"}, {Name: "", Type: "skip"}, {Name: "email", Type: "VARCHAR"}},
	}
	sling := &pipeline.Asset{
		Name:       "staging.users",
		Type:       "sling",
		Parameters: pipeline.EmptyStringMap{"source_table": "raw.users"},
	}
	pl := &pipeline.Pipeline{Assets: []*pipeline.Asset{source, sling}}

	svc := &AssetService{}
	columns, apiErr := svc.inferSlingColumnsFromUpstream(pl, sling)
	if apiErr != nil {
		t.Fatalf("unexpected error: %+v", apiErr)
	}
	if len(columns) != 2 {
		t.Fatalf("expected 2 columns (blank-name skipped), got %d: %+v", len(columns), columns)
	}
	if columns[0].Name != "id" || columns[1].Name != "email" {
		t.Errorf("unexpected columns: %+v", columns)
	}
}

func TestInferSlingColumnsFromUpstreamNoSource(t *testing.T) {
	sling := &pipeline.Asset{Name: "staging.users", Type: "sling"}
	pl := &pipeline.Pipeline{Assets: []*pipeline.Asset{sling}}
	svc := &AssetService{}
	if _, apiErr := svc.inferSlingColumnsFromUpstream(pl, sling); apiErr == nil {
		t.Fatal("expected an error when no source asset can be resolved")
	}
}
