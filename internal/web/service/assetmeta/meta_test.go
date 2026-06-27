package assetmeta

import (
	"reflect"
	"testing"
)

func TestParseApplyRoundTrip(t *testing.T) {
	original := map[string]string{
		"owner":      "data-team", // non-renart key must survive
		KeyVersion:   "1",
		KeyGenerator: "1",
		KeySigDeps:   "abc123",
		KeySigCols:   "def456",
		KeyDepAdd:    "a:analytics.date_spine#symbolic",
		KeyDepDrop:   "a:scratch.tmp#full",
		KeyColAdd:    "loaded_at",
		KeyColDrop:   "debug_rank",
		KeyColOwn:    "order_total:type",
		KeyColMap:    "e:9cc83f4a:order_total",
	}

	parsed := Parse(original)
	if parsed.Version != 1 || parsed.Generator != 1 {
		t.Fatalf("version/generator not parsed: %+v", parsed)
	}
	if parsed.SigDeps != "abc123" || parsed.SigCols != "def456" {
		t.Fatalf("sigs not parsed: %+v", parsed)
	}
	if !reflect.DeepEqual(parsed.DepAdd, []string{"a:analytics.date_spine#symbolic"}) {
		t.Fatalf("dep_add: %#v", parsed.DepAdd)
	}
	if !reflect.DeepEqual(parsed.ColOwn, map[string][]string{"order_total": {"type"}}) {
		t.Fatalf("col_own: %#v", parsed.ColOwn)
	}
	if parsed.ColMap["e:9cc83f4a"] != "order_total" {
		t.Fatalf("col_map: %#v", parsed.ColMap)
	}

	applied := parsed.Apply(map[string]string{"owner": "data-team"})
	for _, key := range []string{KeyVersion, KeySigDeps, KeyDepAdd, KeyColOwn, KeyColMap} {
		if applied[key] == "" {
			t.Fatalf("apply dropped %s: %#v", key, applied)
		}
	}
	if applied["owner"] != "data-team" {
		t.Fatalf("apply lost non-renart key: %#v", applied)
	}
}

func TestApplyClearsEmptyAndLegacy(t *testing.T) {
	// A meta carrying only the legacy key and an empty provenance should come
	// back with no renart keys at all (migrated away / nothing to store).
	meta := map[string]string{KeyLegacyInferredUpstreams: "analytics.orders"}
	out := RenartMeta{}.Apply(meta)
	if out != nil {
		if _, ok := out[KeyLegacyInferredUpstreams]; ok {
			t.Fatalf("legacy key not cleared: %#v", out)
		}
	}
}

func TestApplyEmptyReturnsNil(t *testing.T) {
	if got := (RenartMeta{}).Apply(nil); got != nil {
		t.Fatalf("expected nil meta for empty provenance, got %#v", got)
	}
}

func TestJoinListDeterministic(t *testing.T) {
	a := joinList([]string{"b", "a", "C", "a"})
	b := joinList([]string{"C", "a", "b"})
	if a != b {
		t.Fatalf("joinList not order-independent: %q vs %q", a, b)
	}
	if a != "a,b,C" {
		t.Fatalf("joinList not sorted case-insensitively: %q", a)
	}
}

func TestDecodeOwnMultiple(t *testing.T) {
	own := decodeOwn("order_total:type|description;customer_id:type")
	if !reflect.DeepEqual(own["order_total"], []string{"description", "type"}) {
		t.Fatalf("own order_total: %#v", own["order_total"])
	}
	if !reflect.DeepEqual(own["customer_id"], []string{"type"}) {
		t.Fatalf("own customer_id: %#v", own["customer_id"])
	}
	// round-trip is stable
	if got := encodeOwn(own); decodeOwn(got)["order_total"][0] != "description" {
		t.Fatalf("own round-trip unstable: %q", got)
	}
}
