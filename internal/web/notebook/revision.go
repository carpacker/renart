package notebook

import (
	"crypto/sha256"
	"encoding/hex"
)

// ContentRevision returns an opaque revision for an exact cell-file snapshot.
// It is intentionally content-derived rather than process-local: revisions
// remain valid across server restarts and also detect edits made directly on
// disk. Clients use it as an optimistic-concurrency precondition when saving.
func ContentRevision(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
