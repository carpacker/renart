package notebook

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"renart/internal/web/fingerprint"
)

// CellFingerprint hashes a cell's ID-resolved physical form: referenced
// logical names are rewritten to stable cell IDs (and external refs to
// their stable import names) before canonicalization, and the cell's own
// display name never enters the hash (its physical target is named by ID).
//
// Consequence — invariant 1: renaming a cell changes no fingerprint, so the
// staleness service sees nothing and no recompute can occur. The proof is
// the permanent regression test TestRenameChangesNoFingerprint.
//
// Name resolution uses the same identifier splice as the rename engine
// (never an AST re-print), so it never fails on templated SQL — a logical
// name left unresolved would silently break rename-invariance.
func CellFingerprint(nb *Notebook, cell *Cell) string {
	mapping := make(map[string]string, len(nb.Cells)+len(cell.ExternalRefs))
	for _, sibling := range nb.Cells {
		if sibling.ID == cell.ID {
			continue
		}
		mapping[strings.ToLower(sibling.Asset.Name)] = CellObjectName(sibling.ID)
	}
	for _, ref := range cell.ExternalRefs {
		mapping[strings.ToLower(ref)] = ImportObjectName(ref)
	}

	resolved := spliceIdentifiers(cell.Asset.ExecutableFile.Content, mapping)

	// CanonicalSQL strips comments (including @viz directives, invariant 3)
	// and collapses whitespace, so formatting and presentation never move
	// the fingerprint.
	canonical := fingerprint.CanonicalSQL(resolved)

	upstreamIDs := make([]string, 0, len(cell.Asset.Upstreams))
	for _, upstream := range cell.Asset.Upstreams {
		if sibling := nb.CellByName(upstream.Value); sibling != nil {
			upstreamIDs = append(upstreamIDs, sibling.ID)
		}
	}
	sort.Strings(upstreamIDs)
	externals := append([]string{}, cell.ExternalRefs...)
	sort.Strings(externals)

	hasher := sha256.New()
	hasher.Write([]byte("nbcell\x00"))
	hasher.Write([]byte(canonical))
	hasher.Write([]byte("\x00deps\x00"))
	hasher.Write([]byte(strings.Join(upstreamIDs, ",")))
	hasher.Write([]byte("\x00ext\x00"))
	hasher.Write([]byte(strings.Join(externals, ",")))
	return "nb1:" + hex.EncodeToString(hasher.Sum(nil))
}

// NotebookFingerprints returns the fingerprint of every cell, keyed by cell
// ID. Used by the staleness path and the rename-invariance regression test.
func NotebookFingerprints(nb *Notebook) map[string]string {
	result := make(map[string]string, len(nb.Cells))
	for _, cell := range nb.Cells {
		result[cell.ID] = CellFingerprint(nb, cell)
	}
	return result
}
