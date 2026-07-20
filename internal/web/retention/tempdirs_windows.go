//go:build windows

package retention

import "os"

func ownedByCurrentUser(os.FileInfo) bool {
	// Windows temp roots are scoped by the current user's environment. The
	// allowlist and non-symlink checks remain the primary boundary there.
	return true
}
