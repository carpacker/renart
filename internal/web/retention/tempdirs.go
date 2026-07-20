// Package retention contains bounded cleanup helpers for Renart's local
// operational state. Durable database pruning remains owned by the stores that
// understand their reference graphs.
package retention

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var temporaryDirectoryPrefixes = []string{
	"renart-api-asset-",
	"renart-nbpy-",
	"renart-plan-",
	"renart-pymat-",
	"renart-recovered-snapshot-",
	"renart-schedule-vars-",
	"renart-snapshot-",
	"renart-working-tree-plan-",
}

// CleanupTemporaryDirectories removes only allowlisted, same-user directories
// left before this server process started. The process-start guard is
// deliberately conservative: a long-running operation can keep an old temp
// directory for its full lifetime, and a later Renart process will collect it.
func CleanupTemporaryDirectories(
	tempRoot string,
	olderThan time.Time,
	processStartedAt time.Time,
) (int, error) {
	if strings.TrimSpace(tempRoot) == "" {
		return 0, errors.New("temporary directory root is required")
	}
	if olderThan.IsZero() || processStartedAt.IsZero() {
		return 0, errors.New("temporary directory cleanup cutoffs are required")
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		return 0, fmt.Errorf("read temporary directory root: %w", err)
	}

	removed := 0
	var cleanupErrors []error
	for _, entry := range entries {
		if !hasTemporaryDirectoryPrefix(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(tempRoot, entry.Name())
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("inspect %s: %w", path, err))
			continue
		}
		if !info.IsDir() || !ownedByCurrentUser(info) ||
			!info.ModTime().Before(olderThan) || !info.ModTime().Before(processStartedAt) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove %s: %w", path, err))
			continue
		}
		removed++
	}
	return removed, errors.Join(cleanupErrors...)
}

func hasTemporaryDirectoryPrefix(name string) bool {
	for _, prefix := range temporaryDirectoryPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
