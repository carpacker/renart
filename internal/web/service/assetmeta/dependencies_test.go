package assetmeta

import (
	"sort"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
)

func asset(name string) pipeline.Upstream {
	return pipeline.Upstream{Type: "asset", Value: name, Mode: pipeline.UpstreamModeFull}
}

func upstreamValues(ups []pipeline.Upstream) []string {
	out := make([]string, 0, len(ups))
	for _, u := range ups {
		out = append(out, u.Value)
	}
	sort.Strings(out)
	return out
}

func TestDependencyKeyRoundTrip(t *testing.T) {
	u := pipeline.Upstream{Type: "asset", Value: "analytics.date_spine", Mode: pipeline.UpstreamModeSymbolic}
	key := DependencyKey(u)
	if key != "a:analytics.date_spine#symbolic" {
		t.Fatalf("key: %q", key)
	}
	back := ParseDependencyKey(key)
	if back.Value != u.Value || back.Mode != u.Mode || back.Type != "asset" {
		t.Fatalf("round-trip: %+v", back)
	}
}

func TestReconcileDependenciesBasic(t *testing.T) {
	// inferred raw.orders + raw.customers, no prior provenance.
	final, next := ReconcileDependencies(DependencyReconcileInput{
		AssetName: "analytics.orders",
		Inferred:  []pipeline.Upstream{asset("raw.orders"), asset("raw.customers")},
		Current:   nil,
		Prev:      RenartMeta{},
	})
	if got := upstreamValues(final); !equalStrings(got, []string{"raw.customers", "raw.orders"}) {
		t.Fatalf("final deps: %v", got)
	}
	if next.SigDeps == "" {
		t.Fatalf("expected a dependency signature")
	}
	if len(next.DepAdd) != 0 || len(next.DepDrop) != 0 {
		t.Fatalf("expected no add/drop exceptions: %+v", next)
	}
}

func TestReconcileDependenciesManualAddPreserved(t *testing.T) {
	// User added analytics.date_spine manually (not inferred). It must survive
	// reconciliation and be recorded in d.add.
	prev := RenartMeta{DepAdd: []string{"a:analytics.date_spine#symbolic"}}
	final, next := ReconcileDependencies(DependencyReconcileInput{
		AssetName: "analytics.orders",
		Inferred:  []pipeline.Upstream{asset("raw.orders")},
		Current: []pipeline.Upstream{
			asset("raw.orders"),
			{Type: "asset", Value: "analytics.date_spine", Mode: pipeline.UpstreamModeSymbolic},
		},
		Prev: prev,
	})
	if !equalStrings(upstreamValues(final), []string{"analytics.date_spine", "raw.orders"}) {
		t.Fatalf("final: %v", upstreamValues(final))
	}
	if len(next.DepAdd) != 1 || next.DepAdd[0] != "a:analytics.date_spine#symbolic" {
		t.Fatalf("dep_add not preserved: %+v", next.DepAdd)
	}
	// the symbolic mode must be preserved on the final upstream
	for _, u := range final {
		if u.Value == "analytics.date_spine" && u.Mode != pipeline.UpstreamModeSymbolic {
			t.Fatalf("manual dep lost its mode: %+v", u)
		}
	}
}

func TestReconcileDependenciesAdoptUnknownManual(t *testing.T) {
	// A dependency present in the file but neither inferred nor previously
	// tracked is adopted as a manual add (§9).
	final, next := ReconcileDependencies(DependencyReconcileInput{
		AssetName: "analytics.orders",
		Inferred:  []pipeline.Upstream{asset("raw.orders")},
		Current:   []pipeline.Upstream{asset("raw.orders"), asset("raw.fraud_rules")},
		Prev:      RenartMeta{},
	})
	if !equalStrings(upstreamValues(final), []string{"raw.fraud_rules", "raw.orders"}) {
		t.Fatalf("final: %v", upstreamValues(final))
	}
	if len(next.DepAdd) != 1 || next.DepAdd[0] != "a:raw.fraud_rules#full" {
		t.Fatalf("unknown dep not adopted: %+v", next.DepAdd)
	}
}

func TestReconcileDependenciesDropSuppressesInferred(t *testing.T) {
	// raw.customers is inferred but the user dropped it; it must not reappear.
	prev := RenartMeta{DepDrop: []string{"a:raw.customers#full"}}
	final, next := ReconcileDependencies(DependencyReconcileInput{
		AssetName: "analytics.orders",
		Inferred:  []pipeline.Upstream{asset("raw.orders"), asset("raw.customers")},
		Current:   []pipeline.Upstream{asset("raw.orders")},
		Prev:      prev,
	})
	if !equalStrings(upstreamValues(final), []string{"raw.orders"}) {
		t.Fatalf("dropped dep resurfaced: %v", upstreamValues(final))
	}
	if len(next.DepDrop) != 1 {
		t.Fatalf("drop not retained while still inferred: %+v", next.DepDrop)
	}
}

func TestReconcileDependenciesDropsStaleInferredWhenSigIntact(t *testing.T) {
	// raw.orders was inferred and renart recorded its projection signature.
	// The SQL no longer references it (inferred is now empty), and the manual
	// dep is preserved. Because the signature matches the current managed
	// projection, raw.orders is recognized as stale inferred and dropped rather
	// than adopted as manual.
	prev := RenartMeta{
		DepAdd:  []string{"a:analytics.manual_seed#full"},
		SigDeps: DependencyProjectionHash([]pipeline.Upstream{asset("raw.orders")}),
	}
	final, next := ReconcileDependencies(DependencyReconcileInput{
		AssetName: "analytics.customers",
		Inferred:  nil,
		Current:   []pipeline.Upstream{asset("raw.orders"), asset("analytics.manual_seed")},
		Prev:      prev,
	})
	if !equalStrings(upstreamValues(final), []string{"analytics.manual_seed"}) {
		t.Fatalf("stale inferred dep not dropped: %v", upstreamValues(final))
	}
	if len(next.DepAdd) != 1 || next.DepAdd[0] != "a:analytics.manual_seed#full" {
		t.Fatalf("manual dep not preserved / stale dep wrongly adopted: %+v", next.DepAdd)
	}
}

func TestReconcileDependenciesAdoptsUnknownWhenSigMismatched(t *testing.T) {
	// Same shape, but the recorded signature does not match the current managed
	// projection (an external edit), so the unknown dependency is adopted as
	// manual instead of dropped.
	prev := RenartMeta{
		SigDeps: DependencyProjectionHash([]pipeline.Upstream{asset("something.else")}),
	}
	final, next := ReconcileDependencies(DependencyReconcileInput{
		AssetName: "analytics.customers",
		Inferred:  nil,
		Current:   []pipeline.Upstream{asset("raw.orders")},
		Prev:      prev,
	})
	if !equalStrings(upstreamValues(final), []string{"raw.orders"}) {
		t.Fatalf("unknown dep on sig mismatch should be kept: %v", upstreamValues(final))
	}
	if len(next.DepAdd) != 1 || next.DepAdd[0] != "a:raw.orders#full" {
		t.Fatalf("unknown dep not adopted as manual: %+v", next.DepAdd)
	}
}

func TestReconcileDependenciesObsoleteDropRemoved(t *testing.T) {
	// A drop for a dependency the SQL no longer references is obsolete and
	// should be cleaned out.
	prev := RenartMeta{DepDrop: []string{"a:raw.customers#full"}}
	_, next := ReconcileDependencies(DependencyReconcileInput{
		AssetName: "analytics.orders",
		Inferred:  []pipeline.Upstream{asset("raw.orders")},
		Current:   []pipeline.Upstream{asset("raw.orders")},
		Prev:      prev,
	})
	if len(next.DepDrop) != 0 {
		t.Fatalf("obsolete drop retained: %+v", next.DepDrop)
	}
}

func TestReconcileDependenciesLegacyInferredNotAdopted(t *testing.T) {
	// An asset migrating from the legacy renart_inferred_upstreams key: a
	// current upstream that the legacy key marked as inferred must not be
	// mistaken for a manual add, even if it is momentarily not re-inferred.
	prev := RenartMeta{LegacyInferred: []string{"raw.customers"}}
	_, next := ReconcileDependencies(DependencyReconcileInput{
		AssetName: "analytics.orders",
		Inferred:  []pipeline.Upstream{asset("raw.orders")},
		Current:   []pipeline.Upstream{asset("raw.orders"), asset("raw.customers")},
		Prev:      prev,
	})
	if len(next.DepAdd) != 0 {
		t.Fatalf("legacy-inferred dep wrongly adopted as manual: %+v", next.DepAdd)
	}
	if len(next.LegacyInferred) != 0 {
		t.Fatalf("legacy key not migrated away: %+v", next.LegacyInferred)
	}
}

func TestReconcileDependenciesNonAssetPreserved(t *testing.T) {
	uri := pipeline.Upstream{Type: "uri", Value: "s3://bucket/data"}
	final, _ := ReconcileDependencies(DependencyReconcileInput{
		AssetName: "analytics.orders",
		Inferred:  []pipeline.Upstream{asset("raw.orders")},
		Current:   []pipeline.Upstream{uri},
		Prev:      RenartMeta{},
	})
	found := false
	for _, u := range final {
		if u.Type == "uri" && u.Value == "s3://bucket/data" {
			found = true
		}
	}
	if !found {
		t.Fatalf("non-asset upstream dropped: %+v", final)
	}
}

func TestDependencyProjectionHashStable(t *testing.T) {
	a := DependencyProjectionHash([]pipeline.Upstream{asset("raw.orders"), asset("raw.customers")})
	b := DependencyProjectionHash([]pipeline.Upstream{asset("raw.customers"), asset("raw.orders")})
	if a != b {
		t.Fatalf("hash not order-independent: %q vs %q", a, b)
	}
	if a == DependencyProjectionHash([]pipeline.Upstream{asset("raw.orders")}) {
		t.Fatalf("hash collision for different sets")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
