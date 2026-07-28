//go:build !windows

package secretstore

import "os"

func replaceLocalVaultFile(source, destination string) error {
	return os.Rename(source, destination)
}
