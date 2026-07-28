//go:build windows

package secretstore

import "golang.org/x/sys/windows"

func replaceLocalVaultFile(source, destination string) error {
	return windows.Rename(source, destination)
}
