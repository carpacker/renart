package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gofrs/flock"

	"renart/internal/clientapi"
)

var errWorkspaceAlreadyServed = errors.New("workspace is already served by another Renart process")

// workspaceServerLease prevents two long-lived Renart runtimes from opening
// the same workspace. The lease is deliberately per workspace: one web
// process may hold several leases for the projects it opens lazily.
type workspaceServerLease struct {
	lock     *flock.Flock
	closeErr error
	close    sync.Once
}

// acquireRuntimeWorkspaceLease leaves embedded CLI execution alone. Embedded
// commands are short-lived SQLite clients, not competing web-server owners.
func acquireRuntimeWorkspaceLease(ctx context.Context, cfg serverConfig) (*workspaceServerLease, error) {
	if cfg.headless {
		return nil, nil
	}
	return acquireWorkspaceServerLease(ctx, cfg.workspaceRoot)
}

func acquireWorkspaceServerLease(ctx context.Context, workspaceRoot string) (*workspaceServerLease, error) {
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace server lock root: %w", err)
	}
	stateDir := filepath.Join(root, ".renart")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace state directory for server lock: %w", err)
	}

	lockPath := filepath.Join(stateDir, "server.lock")
	fileLock := flock.New(lockPath)
	locked, err := fileLock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("acquire workspace server lock %q: %w", lockPath, err)
	}
	if !locked {
		return nil, workspaceAlreadyServedError(root, lockPath, discoverLiveWorkspaceServer(ctx, root))
	}

	lease := &workspaceServerLease{lock: fileLock}
	// server.json predates the lease. Checking it after acquiring the lock
	// catches a live older Renart process that does not know about server.lock;
	// stale discovery files are ignored by Discover's bounded health check.
	if existing := discoverLiveWorkspaceServer(ctx, root); existing != nil {
		_ = lease.Close()
		return nil, workspaceAlreadyServedError(root, lockPath, existing)
	}
	return lease, nil
}

func discoverLiveWorkspaceServer(ctx context.Context, workspaceRoot string) *clientapi.ServerFile {
	info, err := clientapi.ReadServerFile(workspaceRoot)
	if err != nil || info == nil {
		return nil
	}
	client, err := clientapi.Discover(ctx, workspaceRoot)
	if err != nil || client == nil {
		return nil
	}
	return info
}

func workspaceAlreadyServedError(workspaceRoot, lockPath string, existing *clientapi.ServerFile) error {
	details := make([]string, 0, 3)
	if existing != nil {
		if existing.PID > 0 {
			details = append(details, fmt.Sprintf("PID %d", existing.PID))
		}
		if address := strings.TrimSpace(existing.BaseURL); address != "" {
			details = append(details, address)
		}
		if !existing.StartedAt.IsZero() {
			details = append(details, "started "+existing.StartedAt.Format("2006-01-02 15:04:05Z07:00"))
		}
	}

	owner := "another Renart server"
	if len(details) > 0 {
		owner += " (" + strings.Join(details, ", ") + ")"
	}
	return fmt.Errorf(
		"%w: %s already has workspace %q open; use that server or stop it before starting another one (lock %q; a leftover unlocked file is harmless)",
		errWorkspaceAlreadyServed,
		owner,
		workspaceRoot,
		lockPath,
	)
}

func (l *workspaceServerLease) Close() error {
	if l == nil || l.lock == nil {
		return nil
	}
	l.close.Do(func() {
		l.closeErr = l.lock.Unlock()
	})
	return l.closeErr
}
