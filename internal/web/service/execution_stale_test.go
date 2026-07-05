package service

import (
	"reflect"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
)

func stalePipeline(assets ...*pipeline.Asset) *pipeline.Pipeline {
	return &pipeline.Pipeline{Assets: assets}
}

func staleAsset(name string, upstreams ...string) *pipeline.Asset {
	asset := &pipeline.Asset{Name: name}
	for _, upstream := range upstreams {
		asset.Upstreams = append(asset.Upstreams, pipeline.Upstream{Type: "asset", Value: upstream})
	}
	return asset
}

func planNames(steps []stalePlanStep) []string {
	names := make([]string, 0, len(steps))
	for _, step := range steps {
		names = append(names, step.asset.Name)
	}
	return names
}

func TestOrderStalePlanBuildsUpstreamsFirst(t *testing.T) {
	// Declaration order is deliberately anti-topological: c depends on b
	// depends on a, but the plan and pipeline list them downstream-first.
	parsed := stalePipeline(
		staleAsset("c", "b"),
		staleAsset("b", "a"),
		staleAsset("a"),
		staleAsset("fresh_leaf", "a"),
	)
	plan := []StaleAssetPlan{{AssetName: "c"}, {AssetName: "a"}, {AssetName: "b"}}

	steps, unknown := orderStalePlan(parsed, plan)
	if len(unknown) != 0 {
		t.Fatalf("expected no unknown assets, got %v", unknown)
	}
	if got, want := planNames(steps), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected topological order %v, got %v", want, got)
	}
}

func TestOrderStalePlanReportsUnknownAndKeepsCycles(t *testing.T) {
	parsed := stalePipeline(
		staleAsset("x", "y"),
		staleAsset("y", "x"),
	)
	plan := []StaleAssetPlan{{AssetName: "x"}, {AssetName: "gone"}, {AssetName: "y"}}

	steps, unknown := orderStalePlan(parsed, plan)
	if !reflect.DeepEqual(unknown, []string{"gone"}) {
		t.Fatalf("expected unknown [gone], got %v", unknown)
	}
	// Cyclic assets never reach indegree zero but must still be built, in
	// declaration order.
	if got, want := planNames(steps), []string{"x", "y"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected cycle members %v, got %v", want, got)
	}
}

func TestFailedUpstreamForWalksTransitively(t *testing.T) {
	parsed := stalePipeline(
		staleAsset("a"),
		staleAsset("b", "a"),
		staleAsset("c", "b"),
		staleAsset("d"),
	)
	failed := map[string]bool{"a": true}

	if got := failedUpstreamFor(parsed.Assets[2], parsed, failed); got != "a" {
		t.Fatalf("expected transitive failed upstream a, got %q", got)
	}
	if got := failedUpstreamFor(parsed.Assets[3], parsed, failed); got != "" {
		t.Fatalf("expected no failed upstream for d, got %q", got)
	}
}
