// Package assetmeta implements renart's asset provenance model: the compact,
// bruin-compatible record of which parts of an asset's Bruin definition renart
// generated (from SQL) versus what the user authored or overrode.
//
// The model mirrors §6 of architecture/renart-asset-editing-concept.md, but is
// stored as flat string keys under the asset's `meta` map rather than as a
// nested `meta.renart` block: bruin parses `meta` as map[string]string
// (pkg/pipeline/yaml.go), so a nested block would not round-trip through bruin
// and would break upstream `bruin` CLI parsing. The flat keys keep every
// committed asset file readable by, and loadable through, plain bruin.
//
// Only *exceptions* are stored. Inferred dependencies and columns are not
// listed; the file's real `depends:`/`columns:` plus these keys are enough to
// reconstruct intent on the next reconcile:
//
//	renart_v         schema version
//	renart_g         generator version
//	renart_sig_deps  checksum of the renart-managed dependency projection
//	renart_sig_cols  checksum of the renart-managed column projection
//	renart_dep_add   manual dependencies (keys: a:<asset>#<mode> / u:<uri>#<mode>)
//	renart_dep_drop  inferred dependencies the user suppressed (same key form)
//	renart_col_add   manual columns (names)
//	renart_col_drop  inferred columns the user omitted (names)
//	renart_col_own   generated columns whose fields the user now owns
//	                 (col:field|field;col2:field)
//	renart_col_map   rename memory (e:<exprhash>:col); optional
//
// All functions in this package are pure: they operate on values, never touch
// the filesystem, and are exhaustively unit-tested.
package assetmeta

import (
	"sort"
	"strconv"
	"strings"
)

// Flat meta keys. Stable strings — changing one is a file-format migration.
const (
	KeyVersion   = "renart_v"
	KeyGenerator = "renart_g"
	KeySigDeps   = "renart_sig_deps"
	KeySigCols   = "renart_sig_cols"
	KeyDepAdd    = "renart_dep_add"
	KeyDepDrop   = "renart_dep_drop"
	KeyColAdd    = "renart_col_add"
	KeyColDrop   = "renart_col_drop"
	KeyColOwn    = "renart_col_own"
	KeyColMap    = "renart_col_map"

	// KeyLegacyInferredUpstreams is the pre-provenance key that listed the
	// inferred (renart-managed) upstream names directly. It is read for
	// back-compat and cleared once an asset is migrated to the new model.
	KeyLegacyInferredUpstreams = "renart_inferred_upstreams"
)

// Schema and generator versions written into freshly reconciled assets.
const (
	SchemaVersion    = 1
	GeneratorVersion = 1
)

// allManagedKeys lists every key this package owns, so Apply can clear stale
// values that no longer apply.
var allManagedKeys = []string{
	KeyVersion, KeyGenerator, KeySigDeps, KeySigCols,
	KeyDepAdd, KeyDepDrop, KeyColAdd, KeyColDrop, KeyColOwn, KeyColMap,
	KeyLegacyInferredUpstreams,
}

// RenartMeta is the decoded provenance for a single asset.
type RenartMeta struct {
	Version   int
	Generator int

	SigDeps string
	SigCols string

	// DepAdd / DepDrop hold dependency keys (a:<asset>#<mode> / u:<uri>#<mode>).
	DepAdd  []string
	DepDrop []string

	// ColAdd / ColDrop hold column names (original case preserved).
	ColAdd  []string
	ColDrop []string

	// ColOwn maps a column name to the generated fields the user now owns
	// (e.g. {"order_total": {"type"}}).
	ColOwn map[string][]string

	// ColMap is optional rename memory: expression hash -> column name.
	ColMap map[string]string

	// LegacyInferred holds names from the deprecated renart_inferred_upstreams
	// key, used only to interpret a not-yet-migrated asset's current upstreams.
	LegacyInferred []string
}

// Parse decodes provenance from an asset's meta map. A nil/empty map yields a
// zero RenartMeta (Version 0), which callers treat as "never reconciled".
func Parse(meta map[string]string) RenartMeta {
	m := RenartMeta{
		Version:   atoiSafe(meta[KeyVersion]),
		Generator: atoiSafe(meta[KeyGenerator]),
		SigDeps:   strings.TrimSpace(meta[KeySigDeps]),
		SigCols:   strings.TrimSpace(meta[KeySigCols]),
		DepAdd:    splitList(meta[KeyDepAdd]),
		DepDrop:   splitList(meta[KeyDepDrop]),
		ColAdd:    splitList(meta[KeyColAdd]),
		ColDrop:   splitList(meta[KeyColDrop]),
		ColOwn:    decodeOwn(meta[KeyColOwn]),
		ColMap:    decodeMap(meta[KeyColMap]),

		LegacyInferred: splitList(meta[KeyLegacyInferredUpstreams]),
	}
	return m
}

// Apply writes the provenance back into a copy of meta, setting non-empty keys
// and clearing every managed key that has no value (so absent provenance writes
// nothing and the legacy key is dropped once migrated). Non-renart keys in the
// input are preserved untouched. Returns nil when the result is empty, so the
// asset's meta serializes as absent rather than `meta: {}`.
func (m RenartMeta) Apply(meta map[string]string) map[string]string {
	out := make(map[string]string, len(meta)+len(allManagedKeys))
	for k, v := range meta {
		out[k] = v
	}
	for _, k := range allManagedKeys {
		delete(out, k)
	}

	set := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	if m.Version > 0 {
		set(KeyVersion, strconv.Itoa(m.Version))
	}
	if m.Generator > 0 {
		set(KeyGenerator, strconv.Itoa(m.Generator))
	}
	set(KeySigDeps, m.SigDeps)
	set(KeySigCols, m.SigCols)
	set(KeyDepAdd, joinList(m.DepAdd))
	set(KeyDepDrop, joinList(m.DepDrop))
	set(KeyColAdd, joinList(m.ColAdd))
	set(KeyColDrop, joinList(m.ColDrop))
	set(KeyColOwn, encodeOwn(m.ColOwn))
	set(KeyColMap, encodeMap(m.ColMap))

	if len(out) == 0 {
		return nil
	}
	return out
}

// --- encoding helpers ---

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		if _, ok := seen[strings.ToLower(v)]; ok {
			continue
		}
		seen[strings.ToLower(v)] = struct{}{}
		out = append(out, v)
	}
	return out
}

// joinList renders a comma list with stable (sorted, case-insensitive) order so
// the committed value is deterministic regardless of input order.
func joinList(values []string) string {
	cleaned := splitList(strings.Join(values, ","))
	sort.SliceStable(cleaned, func(i, j int) bool {
		return strings.ToLower(cleaned[i]) < strings.ToLower(cleaned[j])
	})
	return strings.Join(cleaned, ",")
}

// decodeOwn parses "col:field|field;col2:field" into a map.
func decodeOwn(raw string) map[string][]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make(map[string][]string)
	for _, entry := range strings.Split(raw, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		col, fieldsRaw, ok := strings.Cut(entry, ":")
		col = strings.TrimSpace(col)
		if !ok || col == "" {
			continue
		}
		fields := make([]string, 0)
		seen := make(map[string]struct{})
		for _, f := range strings.Split(fieldsRaw, "|") {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			if _, dup := seen[f]; dup {
				continue
			}
			seen[f] = struct{}{}
			fields = append(fields, f)
		}
		if len(fields) == 0 {
			continue
		}
		sort.Strings(fields)
		out[col] = fields
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func encodeOwn(own map[string][]string) string {
	if len(own) == 0 {
		return ""
	}
	cols := make([]string, 0, len(own))
	for col := range own {
		cols = append(cols, col)
	}
	sort.Strings(cols)
	parts := make([]string, 0, len(cols))
	for _, col := range cols {
		fields := append([]string(nil), own[col]...)
		sort.Strings(fields)
		parts = append(parts, col+":"+strings.Join(fields, "|"))
	}
	return strings.Join(parts, ";")
}

// decodeMap parses "e:hash:col;e:hash2:col2" into exprhash -> column.
func decodeMap(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make(map[string]string)
	for _, entry := range strings.Split(raw, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// Split from the right: the column name has no ':' but the hash key may.
		idx := strings.LastIndex(entry, ":")
		if idx <= 0 || idx == len(entry)-1 {
			continue
		}
		key := strings.TrimSpace(entry[:idx])
		col := strings.TrimSpace(entry[idx+1:])
		if key == "" || col == "" {
			continue
		}
		out[key] = col
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func encodeMap(mapping map[string]string) string {
	if len(mapping) == 0 {
		return ""
	}
	keys := make([]string, 0, len(mapping))
	for k := range mapping {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+":"+mapping[k])
	}
	return strings.Join(parts, ";")
}

func atoiSafe(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0
	}
	return n
}
