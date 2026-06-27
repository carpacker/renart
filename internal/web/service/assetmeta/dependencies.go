package assetmeta

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
)

// DependencyKey renders the normalized key for a dependency upstream:
//
//	a:<asset>#<mode>   for asset dependencies
//	u:<uri>#<mode>     for uri dependencies
//
// The asset/uri value keeps its original case; use dependencyMatchKey for
// case-insensitive comparison.
func DependencyKey(u pipeline.Upstream) string {
	mode := u.Mode
	prefix := "a"
	if strings.EqualFold(u.Type, "uri") {
		prefix = "u"
	}
	return prefix + ":" + strings.TrimSpace(u.Value) + "#" + mode.String()
}

// ParseDependencyKey reconstructs an Upstream from a key produced by
// DependencyKey. Unknown shapes fall back to an asset upstream in full mode.
func ParseDependencyKey(key string) pipeline.Upstream {
	key = strings.TrimSpace(key)
	upstreamType := "asset"
	rest := key
	switch {
	case strings.HasPrefix(key, "a:"):
		rest = key[2:]
	case strings.HasPrefix(key, "u:"):
		upstreamType = "uri"
		rest = key[2:]
	}
	value := rest
	mode := pipeline.UpstreamModeFull
	if hash := strings.LastIndex(rest, "#"); hash >= 0 {
		value = rest[:hash]
		mode = pipeline.MarshalUpstreamMode(rest[hash+1:])
	}
	return pipeline.Upstream{Type: upstreamType, Value: strings.TrimSpace(value), Mode: mode}
}

// DependencyMatchKey normalizes a dependency key (from DependencyKey) for
// case-insensitive identity comparison: prefix + lowercased value, mode dropped.
func DependencyMatchKey(key string) string { return dependencyMatchKey(key) }

// dependencyMatchKey normalizes a key so two dependencies that differ only by
// case or mode compare equal on identity (prefix + lowercased value).
func dependencyMatchKey(key string) string {
	prefix := "a"
	rest := key
	if strings.HasPrefix(key, "a:") || strings.HasPrefix(key, "u:") {
		prefix = key[:1]
		rest = key[2:]
	}
	if hash := strings.LastIndex(rest, "#"); hash >= 0 {
		rest = rest[:hash]
	}
	return prefix + ":" + strings.ToLower(strings.TrimSpace(rest))
}

func isAssetUpstream(u pipeline.Upstream) bool {
	return u.Type == "" || strings.EqualFold(u.Type, "asset")
}

// DependencyReconcileInput carries everything ReconcileDependencies needs.
type DependencyReconcileInput struct {
	AssetName string
	// Inferred are the dependencies freshly derived from the asset's SQL.
	Inferred []pipeline.Upstream
	// Current are the asset's current upstreams (asset + non-asset).
	Current []pipeline.Upstream
	// Prev is the asset's existing provenance.
	Prev RenartMeta
}

// ReconcileDependencies implements §7/§9: it computes the asset's final
// upstreams from inferred dependencies, manual additions, and suppressions, and
// returns updated provenance.
//
//	final = (inferred − d.drop) ∪ d.add
//
// Non-asset upstreams (uri/etc. that are not tracked) are preserved verbatim.
// A current asset dependency that renart did not infer and did not previously
// track as inferred is adopted as a manual add (d.add) — this is how an
// external/manual edit survives (§9 "adopt unknown dependency as d.add").
func ReconcileDependencies(in DependencyReconcileInput) (final []pipeline.Upstream, next RenartMeta) {
	assetMatch := strings.ToLower(strings.TrimSpace(in.AssetName))

	// Index inferred dependencies by match key (drop self-references).
	inferredByKey := make(map[string]pipeline.Upstream)
	inferredOrder := make([]string, 0, len(in.Inferred))
	for _, u := range in.Inferred {
		if strings.TrimSpace(u.Value) == "" || strings.ToLower(strings.TrimSpace(u.Value)) == assetMatch {
			continue
		}
		mk := dependencyMatchKey(DependencyKey(u))
		if _, ok := inferredByKey[mk]; ok {
			continue
		}
		inferredByKey[mk] = u
		inferredOrder = append(inferredOrder, mk)
	}

	dropSet := make(map[string]struct{})
	for _, k := range in.Prev.DepDrop {
		dropSet[dependencyMatchKey(k)] = struct{}{}
	}

	// legacyTracked: names the deprecated key marked as renart-inferred. Used
	// only to decide whether a current asset upstream is manual on a not-yet
	// migrated asset.
	legacyTracked := make(map[string]struct{})
	for _, name := range in.Prev.LegacyInferred {
		legacyTracked[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}

	// Start manual adds from existing provenance.
	manualByKey := make(map[string]pipeline.Upstream)
	manualOrder := make([]string, 0)
	addManual := func(u pipeline.Upstream) {
		mk := dependencyMatchKey(DependencyKey(u))
		if _, ok := manualByKey[mk]; ok {
			return
		}
		manualByKey[mk] = u
		manualOrder = append(manualOrder, mk)
	}
	initialManual := make(map[string]struct{}, len(in.Prev.DepAdd))
	for _, key := range in.Prev.DepAdd {
		up := ParseDependencyKey(key)
		addManual(up)
		initialManual[dependencyMatchKey(DependencyKey(up))] = struct{}{}
	}

	// trustInference decides what to do with a current asset dependency that is
	// no longer inferred and is not a recorded manual add. prevManaged is the
	// projection renart last wrote as inferred (every asset upstream that is not
	// manual). When its checksum matches the recorded signature, renart's
	// projection is intact, so inference is authoritative: such a dependency is
	// stale inferred and must be dropped. Only when the projection was changed
	// outside renart (checksum mismatch, or no signature yet) do we adopt the
	// unknown dependency as a manual add (§9 "adopt unknown dependency as d.add").
	prevManaged := make([]pipeline.Upstream, 0)
	for _, u := range in.Current {
		if !isAssetUpstream(u) {
			continue
		}
		mk := dependencyMatchKey(DependencyKey(u))
		if _, isManual := initialManual[mk]; isManual {
			continue
		}
		prevManaged = append(prevManaged, u)
	}
	trustInference := in.Prev.SigDeps != "" && DependencyProjectionHash(prevManaged) == in.Prev.SigDeps

	// Preserve non-asset upstreams; adopt unknown manual asset upstreams.
	nonAsset := make([]pipeline.Upstream, 0)
	for _, u := range in.Current {
		if !isAssetUpstream(u) {
			nonAsset = append(nonAsset, u)
			continue
		}
		value := strings.TrimSpace(u.Value)
		if value == "" || strings.ToLower(value) == assetMatch {
			continue
		}
		mk := dependencyMatchKey(DependencyKey(u))
		if _, isInferred := inferredByKey[mk]; isInferred {
			continue // managed by inference
		}
		if _, isLegacy := legacyTracked[strings.ToLower(value)]; isLegacy {
			continue // previously inferred under the legacy model; let inference own it
		}
		if _, dropped := dropSet[mk]; dropped {
			continue
		}
		if _, alreadyManual := initialManual[mk]; !alreadyManual && trustInference {
			continue // renart wrote this as inferred; the SQL dropped it → stale, drop it
		}
		// Unknown to inference, not stale → user-authored. Keep it as a manual add.
		addManual(u)
	}

	// Build final upstreams: non-asset, then inferred (minus drops, minus
	// anything overridden by a manual add), then manual adds. Both groups are
	// sorted for deterministic, reviewable diffs (§19): inferred before manual.
	sort.Strings(inferredOrder)
	sort.Strings(manualOrder)
	final = make([]pipeline.Upstream, 0, len(nonAsset)+len(inferredOrder)+len(manualOrder))
	final = append(final, nonAsset...)

	managedKeys := make([]string, 0, len(inferredOrder))
	for _, mk := range inferredOrder {
		if _, dropped := dropSet[mk]; dropped {
			continue
		}
		managedKeys = append(managedKeys, mk)
		if _, overridden := manualByKey[mk]; overridden {
			continue // manual add carries the authoritative form (e.g. symbolic)
		}
		final = append(final, normalizeAssetUpstream(inferredByKey[mk]))
	}
	for _, mk := range manualOrder {
		final = append(final, manualByKey[mk])
	}

	// Updated provenance.
	next = in.Prev
	next.Version = SchemaVersion
	next.Generator = GeneratorVersion
	next.LegacyInferred = nil // migrated

	depAdd := make([]string, 0, len(manualOrder))
	for _, mk := range manualOrder {
		depAdd = append(depAdd, DependencyKey(manualByKey[mk]))
	}
	next.DepAdd = depAdd

	// Keep only drops that still refer to a currently-inferred dependency;
	// a drop for a dependency the SQL no longer references is obsolete.
	depDrop := make([]string, 0, len(in.Prev.DepDrop))
	for _, key := range in.Prev.DepDrop {
		if _, ok := inferredByKey[dependencyMatchKey(key)]; ok {
			depDrop = append(depDrop, key)
		}
	}
	next.DepDrop = depDrop

	next.SigDeps = DependencyProjectionHash(managedKeysToUpstreams(managedKeys, inferredByKey))
	return final, next
}

func normalizeAssetUpstream(u pipeline.Upstream) pipeline.Upstream {
	out := u
	if out.Type == "" {
		out.Type = "asset"
	}
	return out
}

func managedKeysToUpstreams(keys []string, byKey map[string]pipeline.Upstream) []pipeline.Upstream {
	out := make([]pipeline.Upstream, 0, len(keys))
	for _, k := range keys {
		out = append(out, byKey[k])
	}
	return out
}

// DependencyProjectionHash returns a stable checksum of the renart-managed
// dependency projection (§9 sig.d). Order-independent: the input is canonicalized
// (sorted, lowercased keys) before hashing, so the same set always hashes the
// same regardless of ordering.
func DependencyProjectionHash(managed []pipeline.Upstream) string {
	if len(managed) == 0 {
		return ""
	}
	keys := make([]string, 0, len(managed))
	for _, u := range managed {
		keys = append(keys, dependencyMatchKey(DependencyKey(u)))
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return hex.EncodeToString(sum[:])[:16]
}
