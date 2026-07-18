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
	"time"

	"github.com/gofrs/flock"

	"renart/internal/clientapi"
)

var errWorkspaceAlreadyServed = errors.New("workspace is already served by another Renart process")

const workspaceExecutionLeaseRetryDelay = 25 * time.Millisecond

// workspaceServerLease prevents two long-lived Renart runtimes from opening
// the same workspace. The lease is deliberately per workspace so independent
// server processes can still serve different repositories at the same time.
type workspaceServerLease struct {
	primary       *flock.Flock
	compatibility *flock.Flock
	closeErr      error
	close         sync.Once
}

// workspaceExecutionCoordinator protects physical-target claim recovery from
// racing a live execution. Every non-dry execution holds both files shared;
// startup recovery holds them exclusively while it converts orphaned active
// claims to dirty. The primary file lives outside the worktree so `git clean`
// cannot silently split the lock domain. The compatibility file keeps current
// processes coordinated when their per-user runtime directory differs.
type workspaceExecutionCoordinator struct {
	primaryPath       string
	compatibilityPath string
}

type heldWorkspaceExecutionLease struct {
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

func newWorkspaceExecutionCoordinator(workspaceRoot string) (*workspaceExecutionCoordinator, error) {
	root, err := canonicalWorkspaceRoot(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace execution lock root: %w", err)
	}
	primaryPath, err := workspaceExecutionLeasePath(root)
	if err != nil {
		return nil, err
	}
	return &workspaceExecutionCoordinator{
		primaryPath:       primaryPath,
		compatibilityPath: filepath.Join(root, ".renart", "execution.lock"),
	}, nil
}

// AcquireShared implements service.ExecutionLease. A separate flock instance
// is opened for each execution so concurrent runs in one server remain distinct
// shared holders and an exclusive reconciler cannot enter until all finish.
func (c *workspaceExecutionCoordinator) AcquireShared(ctx context.Context) (func() error, error) {
	held, err := c.acquire(ctx, true)
	if err != nil {
		return nil, err
	}
	return held.Close, nil
}

func (c *workspaceExecutionCoordinator) acquireExclusive(ctx context.Context) (func() error, error) {
	held, err := c.acquire(ctx, false)
	if err != nil {
		return nil, err
	}
	return held.Close, nil
}

func (c *workspaceExecutionCoordinator) acquire(ctx context.Context, shared bool) (*heldWorkspaceExecutionLease, error) {
	if c == nil {
		return nil, errors.New("workspace execution coordinator is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	primary, compatibility, err := c.newLocks()
	if err != nil {
		return nil, err
	}

	var locked bool
	if shared {
		locked, err = primary.TryRLockContext(ctx, workspaceExecutionLeaseRetryDelay)
	} else {
		locked, err = primary.TryLockContext(ctx, workspaceExecutionLeaseRetryDelay)
	}
	if err != nil {
		return nil, fmt.Errorf("acquire primary workspace execution lease %q: %w", c.primaryPath, err)
	}
	if !locked {
		return nil, fmt.Errorf("acquire primary workspace execution lease %q: lock was not acquired", c.primaryPath)
	}
	held := &heldWorkspaceExecutionLease{primary: primary}

	if shared {
		locked, err = compatibility.TryRLockContext(ctx, workspaceExecutionLeaseRetryDelay)
	} else {
		locked, err = compatibility.TryLockContext(ctx, workspaceExecutionLeaseRetryDelay)
	}
	if err != nil {
		_ = held.Close()
		return nil, fmt.Errorf("acquire compatibility workspace execution lease %q: %w", c.compatibilityPath, err)
	}
	if !locked {
		_ = held.Close()
		return nil, fmt.Errorf("acquire compatibility workspace execution lease %q: lock was not acquired", c.compatibilityPath)
	}
	held.compatibility = compatibility
	return held, nil
}

// tryAcquireExclusive is used by short-lived embedded CLI runtimes. It never
// waits behind a live executor: failure to acquire means that executor owns the
// active claims and reconciliation must be skipped. Once a crashed process has
// released its OS locks, the next embedded invocation can acquire and repair.
func (c *workspaceExecutionCoordinator) tryAcquireExclusive() (func() error, bool, error) {
	if c == nil {
		return nil, false, errors.New("workspace execution coordinator is unavailable")
	}
	primary, compatibility, err := c.newLocks()
	if err != nil {
		return nil, false, err
	}
	locked, err := primary.TryLock()
	if err != nil {
		return nil, false, fmt.Errorf("acquire primary workspace execution lease %q: %w", c.primaryPath, err)
	}
	if !locked {
		return nil, false, nil
	}
	held := &heldWorkspaceExecutionLease{primary: primary}
	locked, err = compatibility.TryLock()
	if err != nil {
		_ = held.Close()
		return nil, false, fmt.Errorf("acquire compatibility workspace execution lease %q: %w", c.compatibilityPath, err)
	}
	if !locked {
		_ = held.Close()
		return nil, false, nil
	}
	held.compatibility = compatibility
	return held.Close, true, nil
}

func (c *workspaceExecutionCoordinator) newLocks() (*flock.Flock, *flock.Flock, error) {
	if err := os.MkdirAll(filepath.Dir(c.primaryPath), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create workspace execution lease directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(c.compatibilityPath), 0o755); err != nil {
		return nil, nil, fmt.Errorf("create workspace execution compatibility directory: %w", err)
	}
	return flock.New(c.primaryPath), flock.New(c.compatibilityPath), nil
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

func workspaceExecutionLeasePath(canonicalRoot string) (string, error) {
	base, err := workspaceServerLeaseDir()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(filepath.ToSlash(canonicalRoot)))
	return filepath.Join(base, hex.EncodeToString(digest[:])+".execution.lock"), nil
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

func (l *heldWorkspaceExecutionLease) Close() error {
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
