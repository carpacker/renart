//go:build unix

package service

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func pathsSameFilesystem(first, second string) (bool, error) {
	first = existingFilesystemAncestor(first)
	second = existingFilesystemAncestor(second)
	var firstStat unix.Stat_t
	if err := unix.Stat(first, &firstStat); err != nil {
		return false, fmt.Errorf("stat %s: %w", first, err)
	}
	var secondStat unix.Stat_t
	if err := unix.Stat(second, &secondStat); err != nil {
		return false, fmt.Errorf("stat %s: %w", second, err)
	}
	return firstStat.Dev == secondStat.Dev, nil
}

func existingFilesystemAncestor(path string) string {
	path = filepath.Clean(path)
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}
