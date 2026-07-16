package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
// the same workspace. The lease is deliberately per workspace so independent
// server processes can still serve different repositories at the same time.
type workspaceServerLease struct {
	primary       *flock.Flock
	compatibility *flock.Flock
	closeErr      error
	close         sync.Once
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
	root, err := canonicalWorkspaceRoot(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace server lock root: %w", err)
	}
	primaryPath, err := workspaceServerLeasePath(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(primaryPath), 0o700); err != nil {
		return nil, fmt.Errorf("create workspace lease directory: %w", err)
	}

	primary := flock.New(primaryPath)
	locked, err := primary.TryLock()
	if err != nil {
		return nil, fmt.Errorf("acquire workspace server lease %q: %w", primaryPath, err)
	}
	compatibilityPath := filepath.Join(root, ".renart", "server.lock")
	if !locked {
		return nil, workspaceAlreadyServedError(root, primaryPath, compatibilityPath, discoverLiveWorkspaceServer(ctx, root))
	}

	lease := &workspaceServerLease{primary: primary}
	stateDir := filepath.Join(root, ".renart")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		_ = lease.Close()
		return nil, fmt.Errorf("create workspace state directory for server lock: %w", err)
	}

	// Keep taking the original in-worktree lock so current servers still
	// coordinate with older Renart versions and with users whose per-user
	// runtime directories differ. The primary lease above is authoritative for
	// current versions: unlike this compatibility file, `git clean` cannot
	// unlink it while a server is running.
	compatibility := flock.New(compatibilityPath)
	locked, err = compatibility.TryLock()
	if err != nil {
		_ = lease.Close()
		return nil, fmt.Errorf("acquire compatibility workspace server lock %q: %w", compatibilityPath, err)
	}
	if !locked {
		_ = lease.Close()
		return nil, workspaceAlreadyServedError(root, primaryPath, compatibilityPath, discoverLiveWorkspaceServer(ctx, root))
	}
	lease.compatibility = compatibility

	// server.json predates the lease. Checking it after acquiring the lock
	// catches a live older Renart process that does not know about server.lock;
	// stale discovery files are ignored by Discover's bounded health check.
	if existing := discoverLiveWorkspaceServer(ctx, root); existing != nil {
		_ = lease.Close()
		return nil, workspaceAlreadyServedError(root, primaryPath, compatibilityPath, existing)
	}
	return lease, nil
}

func canonicalWorkspaceRoot(workspaceRoot string) (string, error) {
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	return filepath.Clean(root), nil
}

func workspaceServerLeasePath(canonicalRoot string) (string, error) {
	base, err := workspaceServerLeaseDir()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(filepath.ToSlash(canonicalRoot)))
	return filepath.Join(base, hex.EncodeToString(digest[:])+".lock"), nil
}

func workspaceServerLeaseDir() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("RENART_WORKSPACE_LOCK_DIR")); configured != "" {
		if !filepath.IsAbs(configured) {
			return "", fmt.Errorf("RENART_WORKSPACE_LOCK_DIR must be an absolute path")
		}
		return filepath.Clean(configured), nil
	}
	if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" && filepath.IsAbs(runtimeDir) {
		return filepath.Join(runtimeDir, "renart", "workspace-leases"), nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve workspace lease directory: %w", err)
	}
	return filepath.Join(cacheDir, "renart", "workspace-leases"), nil
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

func workspaceAlreadyServedError(workspaceRoot, leasePath, compatibilityPath string, existing *clientapi.ServerFile) error {
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
		"%w: %s already has workspace %q open; use that server or stop it before starting another one (lease %q; compatibility lock %q; leftover unlocked files are harmless)",
		errWorkspaceAlreadyServed,
		owner,
		workspaceRoot,
		leasePath,
		compatibilityPath,
	)
}

func (l *workspaceServerLease) Close() error {
	if l == nil {
		return nil
	}
	l.close.Do(func() {
		var closeErrors []error
		if l.compatibility != nil {
			closeErrors = append(closeErrors, l.compatibility.Unlock())
		}
		if l.primary != nil {
			closeErrors = append(closeErrors, l.primary.Unlock())
		}
		l.closeErr = errors.Join(closeErrors...)
	})
	return l.closeErr
}
