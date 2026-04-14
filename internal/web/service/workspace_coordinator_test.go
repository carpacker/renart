package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPathContains(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		eventPath string
		assetPath string
		expected  bool
	}{
		{name: "exact match", eventPath: "assets/a.sql", assetPath: "assets/a.sql", expected: true},
		{name: "directory contains asset", eventPath: "assets", assetPath: "assets/a.sql", expected: true},
		{name: "pipeline manifest affects assets dir", eventPath: "pipelines/orders/pipeline.yml", assetPath: "pipelines/orders/assets/a.sql", expected: true},
		{name: "schema file affects sibling asset", eventPath: "pipelines/orders/assets/schema.yml", assetPath: "pipelines/orders/assets/a.sql", expected: true},
		{name: "unrelated file", eventPath: "other/x.sql", assetPath: "assets/a.sql", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, PathContains(tt.eventPath, tt.assetPath))
		})
	}
}

func TestStripAssetContent(t *testing.T) {
	t.Parallel()

	state := WorkspaceState{
		Pipelines: []WorkspacePipeline{{
			ID:   "p1",
			Name: "orders",
			Assets: []WorkspaceAsset{{
				ID:      "a1",
				Name:    "marts.orders",
				Path:    "assets/orders.sql",
				Content: "select * from raw.orders",
			}},
		}},
	}

	stripped := StripAssetContent(state)

	assert.Equal(t, "", stripped.Pipelines[0].Assets[0].Content)
	assert.Equal(t, "select * from raw.orders", state.Pipelines[0].Assets[0].Content)
}

func TestStripAssetContentKeepingIDs(t *testing.T) {
	t.Parallel()

	state := WorkspaceState{
		Pipelines: []WorkspacePipeline{{
			Assets: []WorkspaceAsset{
				{ID: "keep", Content: "select 1"},
				{ID: "drop", Content: "select 2"},
			},
		}},
	}

	stripped := StripAssetContentKeepingIDs(state, []string{"keep"})

	assert.Equal(t, "select 1", stripped.Pipelines[0].Assets[0].Content)
	assert.Equal(t, "", stripped.Pipelines[0].Assets[1].Content)
}

func TestWorkspaceCoordinatorWatcherSuppression(t *testing.T) {
	t.Parallel()

	coord := NewWorkspaceCoordinator(WorkspaceCoordinatorDependencies{})

	assert.False(t, coord.IsWatcherSuppressed("assets/orders.sql"))
	coord.SuppressWatcherFor("assets/orders.sql")
	assert.True(t, coord.IsWatcherSuppressed("assets/orders.sql"))

	coord.recentServerWritesMu.Lock()
	coord.recentServerWrites["assets/orders.sql"] = time.Now().Add(-4 * time.Second)
	coord.recentServerWritesMu.Unlock()

	assert.False(t, coord.IsWatcherSuppressed("assets/orders.sql"))
}

func TestWorkspaceCoordinatorFindInspectIDs(t *testing.T) {
	t.Parallel()

	coord := NewWorkspaceCoordinator(WorkspaceCoordinatorDependencies{})
	coord.SetState(WorkspaceState{
		Pipelines: []WorkspacePipeline{{
			Assets: []WorkspaceAsset{
				{ID: "a", Name: "a", Path: "assets/a.sql"},
				{ID: "b", Name: "b", Path: "assets/b.sql", Upstreams: []string{"a"}},
				{ID: "c", Name: "c", Path: "assets/c.sql", Upstreams: []string{"b"}},
			},
		}},
	})

	assert.Equal(t, []string{"a", "b"}, coord.FindMaterializationInspectIDs("a"))
	assert.Equal(t, []string{"a"}, coord.FindDirectlyChangedAssetIDs("assets/a.sql"))
	assert.Equal(t, "b", coord.FindAssetNameByID("b"))
	assert.Equal(t, "", coord.FindAssetNameByID("missing"))
}
